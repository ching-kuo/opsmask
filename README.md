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

```text
                ┌──────────────┐    masked     ┌─────────┐
   raw logs ──▶ │ opsmask mask │ ────────────▶ │   LLM   │
                └──────────────┘               └────┬────┘
                                                    │ masked report
                                  ┌──────────────┐  │
   plaintext ◀───── (TTY) ─────── │   opsmask    │ ◀┘
                                  │    unmask    │
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

## Install

Download a release archive containing the `opsmask` binary and the
companion skill, or build locally:

```sh
go build -o ./bin/opsmask ./cmd/opsmask
```

## Quick start

```sh
# 1. Initialize a project (one-time)
opsmask init
opsmask config trust

# 2. Mask logs before sending them to the LLM
kubectl logs deploy/api | opsmask mask --summary > masked.log

# 3. Paste masked.log into your LLM session, get a report back

# 4. Restore sentinels at your terminal (never inside the agent)
opsmask unmask < report.md
```

`unmask` refuses non-TTY stdout to reduce the chance of accidentally piping
plaintext somewhere durable.

### Round-trip example

Input:

```text
customer Example Corp from alice@example.com hit 10.0.0.1
```

After `opsmask mask`:

```text
customer Example Corp from ⟪opsmask:email:01af3c1d2b4e5f60⟫ hit ⟪opsmask:ip4:7c93a4ed1b209f88⟫
```

The LLM works on the masked text and must echo sentinels verbatim. Then
`unmask` reverses them locally.

## Commands

| Command | Purpose |
| --- | --- |
| `opsmask init` | Scaffold `.opsmask/` (mode `0700`) and a commented `config.yaml`. |
| `opsmask config` | Show current config status. |
| `opsmask config init` | Same scaffold as `init`, from the config command group. |
| `opsmask config trust` | Trust the current project config (path + SHA-256 bound). |
| `opsmask mask [file\|-]` | Mask stdin or a file. Flags: `--summary`, `--ascii-tokens`, `--max-line` (default `16MiB`). |
| `opsmask unmask [file\|-]` | Restore tokens. TTY-only. |
| `opsmask exec -- <cmd> [args...]` | Run a follow-up command with sentinels in argv; output is re-masked before it returns. |
| `opsmask mapping list [--type T] [--limit N]` | List pseudonyms (no plaintext). TTY-only. |
| `opsmask mapping prune --older-than <duration> [--type T]` | Delete old mapping rows. `--older-than` is required. |
| `opsmask mcp serve` | Run the Model Context Protocol server on stdio for LLM clients. |

Global flags: `--config <path>`, `--mapping <path>`, `--verbose`.

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

## Token forms

Three forms can appear in masked output. Agents must preserve them
character-for-character:

- Unicode sentinel: `⟪opsmask:<type>:<index>⟫` (default).
- ASCII fallback: `[[opsmask:<type>:<index>]]` (used when input is
  strict-ASCII or when `--ascii-tokens` is passed).
- Inert escape: `[OPSMASK_ESCAPED_SENTINEL:<base64url>]` (planted before
  masking when source text already looks like a sentinel; decoded back to
  literal bytes during `unmask`).

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

| Type | Policy | Matches |
| --- | --- | --- |
| `ip4` | Pseudonymize | Dotted-quad IPv4 with each octet 0–255. |
| `ip6` | Pseudonymize | Full or `::`-compressed IPv6. Three-group strings like log timestamps `16:23:37` are excluded. |
| `mac` | Pseudonymize | Six colon-separated hex bytes. |
| `uuid` | Pseudonymize | RFC 4122 v1–v5 with hyphens. |
| `hex_id` | Pseudonymize | Plain hex run of 32–128 chars (MD5/SHA/long IDs). |
| `email` | Pseudonymize | Standard `local@domain.tld` shape. |
| `hostname` | Pseudonymize | RFC-1123-ish hostnames/FQDNs (≥2 labels, alphabetic TLD). |
| `k8snamespace`, `k8spod`, `k8snode`, … | Pseudonymize | Kubernetes resource references with nearby resource nouns. |
| `jwt` | Destroy | JWT-shaped strings with valid header (`alg`/`typ`) and a common payload claim. |
| `pem_private_key` | Destroy | `-----BEGIN ... PRIVATE KEY-----` blocks. |
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

Hostname/FQDN and Kubernetes-resource detectors are precision-biased. For
project-specific shapes (`user_…`, `order_…`, `tenant_…`, etc.), add trusted
project rules — see [docs/CUSTOM_DETECTORS.md](docs/CUSTOM_DETECTORS.md).

## Configuration

Project config lives at `.opsmask/config.yaml`. It is **ignored until
trusted**:

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
```

The `deny_list` is an **audit canary**, not an enforcement boundary — a hit
after masking signals the rule set missed something it should have destroyed.

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

`exec` writes JSON-lines audit records to `~/.config/opsmask/exec.log`
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
reflection as an expected failure mode, and keep `~/.config/opsmask` and
project `.opsmask/` directories out of cloud-sync and shared-backup paths.

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

## License

[MIT](LICENSE).
