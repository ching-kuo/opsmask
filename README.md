# OpsMask

A local-only Go CLI that pseudonymizes secrets and identifiers before logs go
to an LLM, then restores them at a TTY when the report comes back.

- **Local only.** No telemetry, no network I/O, no daemon.
- **Deterministic.** The same value always maps to the same sentinel within a
  project, so correlated log lines stay correlated across reports.
- **Reversible — for you.** `unmask` runs at your terminal. The LLM never
  has a way to recover the original values.
- **Extensible.** Built-in detectors cover common cloud/SaaS secrets and
  infrastructure identifiers; project-local rules cover application IDs.

> **⚠️ Project status: early / under-tested.** This is a personal project
> that has only been exercised against the author's own workflows. It has
> **not** been validated at scale, against adversarial inputs, or in
> production environments. Detector coverage is incomplete by design —
> any new secret format that isn't in the rule set will pass through
> unmasked. Treat OpsMask as a **leakage-reduction aid**, never as a
> sole control protecting sensitive data. Review masked output before
> sending it to an LLM, especially during initial adoption. Bug reports
> and missed-detection samples are welcome via GitHub issues.

```text
                     ┌──────────────┐    masked text    ┌─────────┐
   raw logs ───────▶ │ opsmask mask │ ────────────────▶ │   LLM   │
                     └──────┬───────┘                   └────┬────┘
                            │ writes mapping                 │
                            ▼                                │ masked report
              <project>/.opsmask/mapping.sqlite              │ (sentinels echoed)
                            ▲                                │
                            │ reads mapping                  │
                     ┌──────┴───────┐                        │
   plaintext ◀───────│ opsmask      │ ◀──────────────────────┘
                (TTY)│  unmask      │
                     └──────────────┘
```

## When to use it

Use OpsMask whenever you want an LLM to help with **real production
data** that contains values you would not paste into a public chat:

- `kubectl logs`, `kubectl describe`, or `kubectl get -o ...` output.
- `journalctl`, syslog, or application logs from production.
- `ssh host 'tail …'`, `dmesg`, cloud provider audit logs.
- Crash dumps, stack traces, or support bundles with embedded credentials,
  emails, hostnames, or customer identifiers.

It is **not** the right tool for:

- Source code review, where line-level structure must stay readable.
- Documents where pseudonyms break meaning (e.g. legal contracts,
  human-readable narratives).
- Defending against a malicious LLM that ignores instructions to preserve
  sentinels — OpsMask is a leakage-reduction tool, not a sandbox.

## Prerequisites

- **Go 1.26+** to build from source.
- **A writable user-config directory.** The user-wide secret used to seed
  pseudonymization is generated on first run (mode `0600`) under
  `os.UserConfigDir()`: `~/.config/opsmask/user_secret` on Linux,
  `~/Library/Application Support/opsmask/user_secret` on macOS,
  `%AppData%\opsmask\user_secret` on Windows. Keep this directory on local
  disk — it is rejected on network filesystems.
- **A TTY for `unmask`.** `opsmask unmask` refuses to write to a
  non-terminal stdout to avoid accidentally piping plaintext into a file or
  another program. There is no bypass flag; this is intentional.
- **Optional: a git repository.** Required for `opsmask install claude-code`,
  not for plain `mask`/`unmask`.

## Install

Build from source (no signed binaries are published yet):

```sh
# from the repository root
go build -o ./bin/opsmask ./cmd/opsmask
sudo mv ./bin/opsmask /usr/local/bin/opsmask   # or any directory on PATH
```

On macOS, an unsigned binary built locally runs without Gatekeeper prompts.
If you copy the binary from another machine, clear the quarantine attribute:

```sh
xattr -d com.apple.quarantine /usr/local/bin/opsmask
```

## Smoke test (no cluster needed)

Confirm the install before wiring it into anything:

```sh
echo "alice@example.com from 10.0.0.1 hit api.example.com" | opsmask mask
```

Expected output (token indexes will differ):

```text
⟪opsmask:email:01af3c1d…⟫ from ⟪opsmask:ip4:7c93a4ed…⟫ hit ⟪opsmask:hostname:b1d2e3f4…⟫
```

The pipeline above works **without** running `opsmask init` or `config trust` —
built-in detectors are always active. `init` and `config trust` only unlock
project-specific extensions (custom regex rules, internal TLDs, the `exec`
follow-up surface). See [Configuration](#configuration).

## Pick your integration

| Path | Use when | Start here |
| --- | --- | --- |
| **Plain CLI** | You drive the LLM via copy/paste or your own scripts. | [Quick start](#quick-start) |
| **Claude Code hook** | You use Claude Code and want non-trivial Bash output masked automatically. | [Claude Code Bash hook](#claude-code-bash-hook) |
| **MCP server** | You use Claude Desktop, Cursor, Copilot, or another MCP client. | [MCP server](#mcp-server) |

The three paths are independent — pick one to start, add others later.

## Quick start

```sh
# 1. (Optional) Initialize a project for custom rules and exec follow-up.
#    Skip this if you only need built-in detectors.
opsmask init
opsmask config trust

# 2. Mask logs before sending them to the LLM.
kubectl logs deploy/api | opsmask mask --summary > masked.log

# 3. Paste masked.log into your LLM session, get a report back as report.md.

# 4. Restore sentinels at your terminal (never inside the agent).
opsmask unmask < report.md
```

### Tell your LLM this

The LLM must echo sentinels **verbatim** (no paraphrasing, lowercasing, or
splitting) for `unmask` to restore them. If you use Claude Code, the
[`skill/opsmask`](skill/opsmask/SKILL.md) directory ships a Skill that
encodes this contract. For other clients, paste a system prompt like:

> The text I share has been pseudonymized by OpsMask. Tokens of the form
> `⟪opsmask:<type>:<index>⟫`, `[[opsmask:<type>:<index>]]`, and
> `[OPSMASK_ESCAPED_SENTINEL:...]` are placeholders for real values. Do not
> paraphrase, modify, lowercase, split, or "clean up" these tokens. Echo
> them character-for-character in your reply.

### Round-trip example

Input:

```text
customer Example Corp from alice@example.com hit 10.0.0.1
```

After `opsmask mask`:

```text
customer Example Corp from ⟪opsmask:email:01af3c1d2b4e5f60⟫ hit ⟪opsmask:ip4:7c93a4ed1b209f88⟫
```

`unmask` reverses the sentinels locally once the LLM's report comes back.

## Token forms

Three forms can appear in masked output. Agents must preserve them
character-for-character:

- **Unicode sentinel**: `⟪opsmask:<type>:<index>⟫` (default).
- **ASCII fallback**: `[[opsmask:<type>:<index>]]` — used when input is
  strict-ASCII or when `--ascii-tokens` is passed. Pick this when your LLM
  client mangles non-ASCII characters or your downstream tooling is not
  Unicode-clean.
- **Inert escape**: `[OPSMASK_ESCAPED_SENTINEL:<base64url>]` — planted before
  masking when source text already looks like a sentinel; decoded back to
  literal bytes during `unmask`.

## Commands

**Setup**

| Command | Purpose |
| --- | --- |
| `opsmask init` | Scaffold `.opsmask/` (mode `0700`) and a commented `config.yaml`. |
| `opsmask config` | Show current config status. |
| `opsmask config init` | Same scaffold as `init`, from the config command group. |
| `opsmask config trust` | Trust the current project config (path + SHA-256 bound). |

**Runtime**

| Command | Purpose |
| --- | --- |
| `opsmask mask [file\|-]` | Mask stdin or a file. Flags: `--summary`, `--ascii-tokens`, `--max-line` (default `16MiB`; raise it for very long single-line JSON or stack traces). |
| `opsmask unmask [file\|-]` | Restore tokens. TTY-only. |
| `opsmask exec -- <cmd> [args...]` | Run a follow-up command with sentinels in argv; output is re-masked before it returns. |

**Integrations**

| Command | Purpose |
| --- | --- |
| `opsmask install claude-code [--team-shared]` | Install the Claude Code Bash hook for the current git project. |
| `opsmask uninstall claude-code` | Remove the Claude Code hook from the current git project. |
| `opsmask mcp serve` | Run the Model Context Protocol server on stdio for LLM clients. |

**Admin**

| Command | Purpose |
| --- | --- |
| `opsmask mapping list [--type T] [--limit N]` | List pseudonyms (no plaintext). TTY-only. |
| `opsmask mapping prune --older-than <duration> [--type T]` | Delete old mapping rows. `--older-than` is required. |

Global flags: `--config <path>`, `--mapping <path>`, `--verbose`.

## State and storage

OpsMask keeps state in two places:

- **User-config directory** (Go's `os.UserConfigDir()` + `opsmask/`):
  `~/.config/opsmask/` on Linux, `~/Library/Application Support/opsmask/`
  on macOS, `%AppData%\opsmask\` on Windows. Holds `user_secret` (the
  32-byte HMAC key that seeds deterministic pseudonymization),
  `exec.log`, `pass_through.log`, and `mcp_calls.jsonl`. Back this
  directory up; keep it out of cloud sync (Dropbox, iCloud Drive, etc.).
- **`<project>/.opsmask/`** — per-project config (`config.yaml`) and the
  mapping store (`mapping.sqlite`). Add `.opsmask/` to `.gitignore`;
  never commit it.

Pseudonymization is deterministic per `user_secret`. Two machines with
different secrets produce different sentinels for the same input, even with
identical project config. Existing mapping rows already on disk remain
restorable by `unmask` regardless of the secret — `unmask` looks up by
token index — but if you lose `user_secret`, future masking on a fresh
project produces sentinels that differ from any prior run.

## Claude Code Bash hook

`opsmask install claude-code` opts the current git project into a Claude Code
`PreToolUse` hook for `Bash` calls. Non-trivial Bash commands are rewritten
through a hidden, signed `opsmask claude-code-exec` entry point so stdout and
stderr are masked before they can enter the agent context. The default install
writes `.claude/settings.local.json` and adds it to `.gitignore`; pass
`--team-shared` to write `.claude/settings.json` after accepting the teammate
fail-closed warning.

Trivial commands on a built-in skiplist (`ls`, `pwd`, `git status`, and
similar read-only Bash) pass through **unwrapped** and unmasked — their
output is logged to `pass_through.log` for audit but is not run through the
detector pipeline. Wrapped invocations are logged to `exec.log`. Treat the
hook as a safety net for production-shaped output, not as a guarantee that
every Bash result is masked.

Install bakes the resolved binary path into `.claude/opsmask-hook.sh`. If
you move or rename the `opsmask` binary after running `install claude-code`,
re-run `opsmask install claude-code` so the hook script picks up the new
path.

This is a deliberate second operating mode:

- `opsmask exec`, `mask`, `unmask`, and MCP remain policy-gated by project
  trust and `exec.enabled`.
- The Claude Code hook bypasses those policy gates only for a project that was
  explicitly registered by `opsmask install claude-code`. The bypass is gated
  by a per-user hook secret and git-toplevel-bound HMAC signature, and writes
  hook records to `exec.log` / `pass_through.log`.

The v0 hook covers Claude Code `Bash` output only. `Read`, `Grep`, MCP tool
outputs, and transcript sweeps are follow-up surfaces.

## MCP server

`opsmask mcp serve` exposes masking, detection, and follow-up `exec` to
LLM clients (Claude Desktop, Claude Code, Cursor, Copilot) over the
standard Model Context Protocol stdio transport. The server uses the
official `modelcontextprotocol/go-sdk`.

### Quickstart

Find the absolute path to your binary (clients launch the server as a
subprocess and `PATH` is unreliable in that context):

```sh
which opsmask
# /usr/local/bin/opsmask  (or wherever you installed it)
```

Add the snippet to your client's MCP config. For Claude Desktop on macOS
(`~/Library/Application Support/Claude/claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "opsmask": {
      "command": "/usr/local/bin/opsmask",
      "args": ["mcp", "serve"]
    }
  }
}
```

Cursor (`~/.cursor/mcp.json`):

```json
{
  "mcpServers": {
    "opsmask": {
      "command": "/usr/local/bin/opsmask",
      "args": ["mcp", "serve"]
    }
  }
}
```

Claude Code (`~/.config/claude-code/mcp.json` or per-project
`.claude/mcp.json`):

```json
{
  "mcpServers": {
    "opsmask": {
      "command": "/usr/local/bin/opsmask",
      "args": ["mcp", "serve"]
    }
  }
}
```

### Tools and resources

| Tool | Purpose |
| --- | --- |
| `mask_text` | Mask sensitive values in text using project detectors. Persists pseudonyms. |
| `detect_text` | Scan text for sensitive values without persisting. Returns counts by type. |
| `exec` | Run a follow-up command. Honors project allow-list, deny-base, re-masks output. |
| `mapping_stats` | Per-type counts of pseudonyms currently in the mapping store. |
| `list_detectors` | List active detector rules (built-ins + project rules). |

| Resource | URI |
| --- | --- |
| Mapping snapshot (token-only) | `opsmask://mapping/{type}?limit=N` |

### What is *not* exposed and why

`unmask`, `init`, and `config trust` are **CLI-only by design**. `unmask`
returns plaintext; exposing it as an MCP tool would put plaintext into
the agent's context window — voiding the project's headline guarantee.
`init` and `config trust` mutate trust anchors that must originate from
a human at a TTY.

The mapping resource returns sentinel tokens and byte lengths only —
**no plaintext, no HMAC bytes**. The resource handler is exercised by
`internal/mcpsrv/resource_mapping_test.go` against multiple HMAC
encodings (raw, hex, base64 std/url/raw) to enforce this contract.

### Audit

MCP tool calls write JSON-Lines records to two audit streams in the
same directory as `exec.log` (configurable via `OPSMASK_AUDIT_DIR`,
defaults to `~/.config/opsmask/`):

- `exec.log` — `exec` tool records, identical schema to CLI exec, plus
  a `"source": "mcp"` field. **Fail-closed**: subprocess execution
  refuses if the audit log is unwritable.
- `mcp_calls.jsonl` — lean records for non-exec tools (`mask_text`,
  `detect_text`, `mapping_stats`, `list_detectors`, resource reads).
  Contains call counts and sizes, never content. **Fail-open**: a
  bulk-masking call is not blocked by an unwritable audit, but the
  failure is logged to the server's stderr (visible to the launching
  client). No MCP tool surface exposes audit-failure status — exposing
  even a sticky boolean would create a denial-of-service oracle.

## What gets detected

Common secret/token detectors are derived from a pinned review of the
[Gitleaks](https://github.com/gitleaks/gitleaks) default configuration, with
local extensions for LLM-bound log masking gaps and debug-useful identifiers.
See [docs/DETECTOR_RULES.md](docs/DETECTOR_RULES.md) for sourcing,
attribution, and the procedure for keeping rules current.

Two policies apply:

- **Pseudonymize** — value is mapped to a stable token and remembered in the
  mapping store. `unmask` can restore it.
- **Destroy** — value is replaced with `[REDACTED_<TYPE>]` and not stored.
  `unmask` cannot recover it.

The detectors you will see most often:

| Type | Policy | Matches |
| --- | --- | --- |
| `ip4` | Pseudonymize | Dotted-quad IPv4 with each octet 0–255. |
| `ip6` | Pseudonymize | Full or `::`-compressed IPv6. Three-group strings like log timestamps `16:23:37` are excluded. |
| `email` | Pseudonymize | Standard `local@domain.tld` shape. |
| `hostname` | Pseudonymize | RFC-1123-ish hostnames/FQDNs whose suffix is recognized by the Public Suffix List or configured as internal. |
| `k8snamespace`, `k8spod`, `k8snode`, … | Pseudonymize | Kubernetes resource references with nearby resource nouns. |
| `jwt` | Destroy | JWT-shaped strings with valid header (`alg`/`typ`) and a common payload claim. |
| `pem_private_key` | Destroy | `-----BEGIN ... PRIVATE KEY-----` blocks. |

<details>
<summary>All other built-in detectors (cloud, SaaS, generic IDs)</summary>

| Type | Policy | Matches |
| --- | --- | --- |
| `mac` | Pseudonymize | Six colon-separated hex bytes. |
| `uuid` | Pseudonymize | RFC 4122 v1–v5 with hyphens. |
| `hex_id` | Pseudonymize | Plain hex run of 32–128 chars (MD5/SHA/long IDs). |
| `password_url` | Destroy | URLs with embedded `user:pass@host` credentials. |
| `aws_key` | Destroy | AWS access keys (`AKIA…`, `ASIA…`, `ABIA…`, `ACCA…`, `A3T…`). |
| `github_token` | Destroy | GitHub PATs and tokens (`ghp_`, `gho_`, `ghu_`, `ghs_`, `ghr_`, `github_pat_`). |
| `gitlab_token` | Destroy | GitLab token prefixes (`glpat-`, `glrt-`, `glcbt-`). |
| `slack_token` | Destroy | Slack app, bot, user, legacy, config, refresh, and webhook shapes. |
| `openai_key` | Destroy | OpenAI keys containing the `T3BlbkFJ` marker. |
| `anthropic_key` | Destroy | Anthropic `sk-ant-api03-…` and `sk-ant-admin01-…` keys. |
| `stripe_key`, `stripe_webhook_secret`, `stripe_publishable_key` | Destroy | Stripe secret/restricted, webhook signing, and publishable keys. |
| `stripe_id` | Pseudonymize | Stripe resource IDs (`ch_`, `cus_`, `pi_`, `sub_`, `evt_`, `pm_`, `prod_`, `price_`, `seti_`, `ba_`, `card_`, `src_`, `tok_`, `txn_`). |
| `gcp_api_key` | Destroy | Google/GCP API keys beginning with `AIza`. |
| `gcp_sa` | Destroy | JSON `"type": "service_account"` discriminator. |
| `twilio_key` | Destroy | Twilio API keys (`SK…`). |
| `npm_token` | Destroy | npm registry tokens (`npm_`). |
| `pypi_token` | Destroy | PyPI Macaroon upload tokens. |
| `sendgrid_key` | Destroy | SendGrid API keys (`SG.<id>.<secret>`). |
| `digitalocean_token` | Destroy | DigitalOcean PAT/OAuth/refresh (`dop_v1_`, `doo_v1_`, `dor_v1_`). |
| `linear_token` | Destroy | Linear API keys (`lin_api_`). |
| `postman_key` | Destroy | Postman access keys (`PMAK-`). |

</details>

Hostname/FQDN detection uses the Public Suffix List for registered ICANN and
private suffixes, plus a small default internal-TLD set (`local`, `internal`,
`lan`, `home`, `localhost`, `arpa`, `corp`, `intranet`, `test`).
Kubernetes-resource detectors are also precision-biased. For project-specific
shapes (`user_…`, `order_…`, `tenant_…`, etc.), add trusted project rules —
see [docs/CUSTOM_DETECTORS.md](docs/CUSTOM_DETECTORS.md).

## Configuration

Project config lives at `.opsmask/config.yaml`. **Built-in detectors run
without it.** A project config is only required to:

- Add custom `regex_rules` for application-specific IDs.
- Extend hostname masking with `detectors.hostname.internal_tlds`.
- Enable the `exec` follow-up surface.

The config file is **ignored until trusted**:

```sh
opsmask config trust
```

Trust is bound to the file's resolved path plus a SHA-256 of its contents.
Any edit requires a fresh `config trust`. Passing `--config <other-path>`
cannot satisfy the trust gate; security-critical settings (notably `exec:`)
are silently ignored when loaded from an override path.

Example:

```yaml
# .opsmask/config.yaml
literals: []
regex_rules:
  - name: app-user-id
    type: app_user
    pattern: '\buser_\d+\b'
    policy: pseudonymize
deny_list: []
exec:
  enabled: false
  scope: read-only
  default_timeout: 30s
  allow: []
  env_allow: []
  env_deny: []
detectors:
  hostname:
    internal_tlds: [acme]
```

The `deny_list` is an **audit canary**, not an enforcement boundary — a hit
after masking signals the rule set missed something it should have destroyed.

`detectors.hostname.internal_tlds` extends hostname masking for trusted
self-hosted suffixes such as `db-1.prod.acme`. It is additive only and is
honored only from a trusted project `.opsmask/config.yaml`; user-wide config
and `--config` overrides cannot widen hostname masking.

## Follow-up commands with `exec`

When investigating a masked entity, `opsmask exec` runs a read-only
follow-up such as `kubectl describe`, `dig`, or `nslookup` while keeping
plaintext out of the agent's context. The wrapper resolves sentinels in
argv locally, runs the child, and re-masks stdout/stderr before they return.

```sh
opsmask exec -- kubectl describe pod '⟪opsmask:k8spod:0123456789abcdef⟫'
opsmask exec -- nslookup '[[opsmask:hostname:0123456789abcdef]]'
```

`exec` is **disabled by default** and only enabled by a trusted project
config. Three scope tiers are available:

- `read-only` (default) — curated baseline: `kubectl get|describe|logs`
  (with secret and follow-mode carve-outs), DNS tools, stdin-only `jq`,
  `echo`, `date`, bare `env`.
- `investigate` — adds broader read-only SRE commands and arbitrary-path
  file readers: `cat`, `tail`, `grep`, `awk`, `sed`.
- `freeform` — explicit escape hatch. Any non-denied command can run unless
  the project allow-list constrains it. Shells, debuggers, REPLs, schedulers,
  remote-exec helpers, and known dispatch flags remain denied unless a named
  freeform deny opt-out is configured.

Project allow-list entries are per-argv-element regex sets:

```yaml
exec:
  enabled: true
  scope: investigate
  allow:
    - name: "curl-internal-prom"
      elements:
        - "^curl$"
        - "^-fsSL$"
        - "^https://prom\\.internal/.*$"
```

`curl` and `wget` are not in any baseline; allow them explicitly when needed.

`exec` writes JSON-lines audit records to the user-config directory's `exec.log` (`~/.config/opsmask/exec.log` on Linux; the platform-equivalent path elsewhere)
(mode `0600`). Each invocation writes two records: a `"starting"` record
before the child process runs (with argv, scope, policy match, env-shaping
counts) and a final record afterward (with `exit_code`, `duration_ms`,
`error_class`). The pre-execution record is the load-bearing forensic
anchor — if the audit log becomes unwritable between `Preflight` and
the final write, the subprocess refuses to run. If a write fails *after*
the subprocess has already executed (e.g. disk filled mid-Run), the
`"starting"` record on disk preserves reconstruction.

### Child environment shaping

Children do **not** inherit the full parent environment. `exec` builds a
tier-specific allow-list (`PATH`, `HOME`, locale vars, kube/cloud config
paths, proxy vars, tier-specific SRE vars) and strips interpreter startup and
command-dispatch variables (`BASH_ENV`, `LD_PRELOAD`, `PYTHONPATH`,
`NODE_OPTIONS`, `GIT_SSH_COMMAND`, `GIT_CONFIG_*`, `BASH_FUNC_*`, …).
Project `exec.env_allow` adds tool-specific variables; `exec.env_deny` always
wins.

## Troubleshooting

**`opsmask unmask` exits with `unmask refuses to write to non-TTY stdout`.**
Run `unmask` interactively. Redirecting to a file or pipe is intentionally
blocked — there is no flag to override it. If you need plaintext in a file,
copy from your terminal scrollback after the fact.

**`config trust` keeps rejecting the project.** Trust binds to the resolved
path and SHA-256 of the file. Re-running `config trust` after every edit is
expected. If you opened the project through a symlink, run `config trust`
from the resolved real path.

**Custom `regex_rules` are ignored.** Confirm two things: (1) the config is
at `.opsmask/config.yaml` (not passed via `--config`), and (2) you ran
`opsmask config trust` after the most recent edit. `--config` overrides
cannot satisfy the trust gate.

**The Claude Code hook is not firing.** Check `.claude/settings.local.json`
exists and contains an `opsmask` `PreToolUse` entry; reinstall with
`opsmask install claude-code` if missing. Pass-through events are logged to
`pass_through.log` in the user-config directory. If you moved or renamed the
`opsmask` binary after running `install`, re-run `opsmask install claude-code`
to refresh the binary path baked into `.claude/opsmask-hook.sh`.

**Mappings appear out of sync between machines.** `user_secret` is per
machine. Copy the `user_secret` file from one machine's user-config
directory to the other (mode `0600`) before any masking happens there.

**Removing OpsMask state.** To wipe everything: remove the user-config
directory (`~/.config/opsmask/` on Linux, `~/Library/Application Support/opsmask/`
on macOS, `%AppData%\opsmask\` on Windows) and any per-project `.opsmask/`
directories. To uninstall the Claude Code hook for one project:
`opsmask uninstall claude-code`. The binary itself can be deleted from
wherever you installed it on `PATH`.

## Limitations

OpsMask reduces leakage on the CLI pipe path. It does **not** protect:

- Screenshots, clipboard, or copy-paste that bypasses the CLI.
- Uploads to a chat UI's file picker that skip the masking pipeline.
- An agent that ignores the companion skill and rewrites or paraphrases
  sentinel tokens.
- Detection gaps. New secret formats appear constantly; the deny-list canary
  helps you notice misses, but it cannot substitute for review.

Pseudonymization is deterministic per `user_secret`. That property is
intentional — it lets correlated lines stay correlated — but it means an LLM
that echoes a sentinel back in plaintext form leaks information. Treat token
reflection as an expected failure mode, and keep the user-config directory
and project `.opsmask/` directories out of cloud-sync and shared-backup paths.

`exec` runs a real child process after resolving sentinels in argv. Scope
tiers and the hard deny-list reduce that surface, but they do not provide
filesystem sandboxing. On shared bastions or jump hosts, resolved argv may be
briefly visible to other local users via `/proc/<pid>/cmdline` unless the
host hides other users' processes (e.g. Linux `hidepid=2`). Do not enable
`exec` on multi-user hosts unless that exposure is acceptable.

For a full list of follow-up risks and detector-sourcing caveats, see
[docs/REMAINING_RISKS.md](docs/REMAINING_RISKS.md).

## Documentation

End-user documentation:

- [docs/DETECTOR_RULES.md](docs/DETECTOR_RULES.md) — detector sourcing,
  Gitleaks attribution, and rule-update procedure.
- [docs/CUSTOM_DETECTORS.md](docs/CUSTOM_DETECTORS.md) — project-specific
  `regex_rules` cookbook for application IDs.
- [docs/REMAINING_RISKS.md](docs/REMAINING_RISKS.md) — known limitations
  and follow-up risks.
- [docs/THIRD_PARTY_NOTICES.md](docs/THIRD_PARTY_NOTICES.md) — license
  attributions for derived rules.
- [docs/examples/kubernetes-safe-followup.md](docs/examples/kubernetes-safe-followup.md)
  — worked Kubernetes triage example.
- [BENCHMARKS.md](BENCHMARKS.md) — local benchmark numbers.
- [CHANGELOG.md](CHANGELOG.md) — release notes.
- [testdata/corpus/README.md](testdata/corpus/README.md) — how to guard
  a detection bug fix against regression with the corpus harness
  (`opsmask corpus add | accept | list`).

## License

[MIT](LICENSE).
