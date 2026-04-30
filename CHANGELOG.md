# Changelog

## Unreleased

### Added

- **MCP server (`opsmask mcp serve`).** Stdio Model Context Protocol server
  exposing five tools (`mask_text`, `detect_text`, `exec`, `mapping_stats`,
  `list_detectors`) and one resource template (`opsmask://mapping/{type}`)
  to LLM clients (Claude Desktop, Claude Code, Cursor, Copilot). Uses the
  official `modelcontextprotocol/go-sdk`. README ships paste-ready config
  snippets for each client.
- **`exec` over MCP** with one MCP-specific tightening: refuses when the
  policy would otherwise grant unrestricted execution
  (`scope: freeform` paired with an empty allow-list). Freeform plus a
  real allow-list remains a legitimate workflow.
- **`mcp_calls.jsonl` audit stream** alongside `exec.log` for non-exec
  MCP tool invocations. Counts and sizes only; never content. Multi-process
  POSIX-append safe like the existing `exec.log`.
- **`Source` field on `exec.Record`** with a `NewRecord(source)` constructor
  and runtime rejection of records with unset/invalid sources. CLI records
  carry `"source":"cli"`; MCP exec calls carry `"source":"mcp"`.
- **`Store.Stats(ctx)` method** returning per-type pseudonym counts in a
  single read query.
- **Side-effect-free `detect.Scan`** path used by `detect_text` to scan
  text without persisting any pseudonyms.

### Security

- **`exec` audit log now writes a pre-execution record.** Every `exec`
  invocation (CLI or MCP) writes two JSON-Lines records to `exec.log`:
  a `"starting"` record before `Run()` is invoked and a final outcome
  record after it returns. Closes a TOCTOU window where `Preflight`
  succeeded but the post-execution `WriteRecord` could fail (disk full
  mid-run), leaving an already-executed subprocess unaudited. The
  `"starting"` record carries argv, scope, and policy match; the final
  record adds `exit_code`, `duration_ms`, and `error_class`. Forensic
  reconstruction is preserved even if the final write fails.
- **`unmask` is intentionally not exposed as an MCP tool.** Plaintext
  never leaves the human's TTY through MCP. This is a deliberate
  divergence from CloakMCP and similar tools that ship `unpack` over
  MCP. README documents the rationale.
- **MCP mapping resource never returns plaintext or HMAC bytes** — only
  sentinel tokens and byte lengths. Cross-store correlation via HMAC
  equality is impossible by construction; the contract is verified by
  multi-encoding tests (raw, hex, base64 std/url/raw).
- **Engine cancellation** propagates request context through
  `engine.Process` at four sites (probe loop, outer loop, processSegment,
  maskChunk) so a client disconnect aborts in-flight masking. Documented
  caveat: `bufio.Reader.ReadSlice` and `io.Writer.Write` cannot be
  ctx-cancelled mid-call; MCP tool handlers always pass `*bytes.Buffer`
  writers so this limitation does not apply on the MCP path.
- **Audit asymmetry by design.** `exec.log` writes fail closed (no
  un-audited subprocess execution); `mcp_calls.jsonl` writes fail open
  (high-DOS surface, lower forensic value per call). The asymmetry is
  documented in `docs/REMAINING_RISKS.md` and the failure status is
  logged only to the server's stderr — never via MCP, so it cannot
  serve as an attack-success oracle.

### Changed

- **Race fix in `internal/exec.Run`.** Replaced `cmd.StdoutPipe`/
  `cmd.StderrPipe` with manually-managed `os.Pipe` instances. The Go
  standard library's `cmd.Wait` closes pipes returned by `StdoutPipe`/
  `StderrPipe` immediately on process exit, racing concurrent reader
  goroutines that drain output for `engine.Process`. The manual pipes
  stay open until the readers see EOF.
- **`internal/runtime` package** now hosts the `Env` / `New` / `Close`
  API previously inlined as unexported helpers in `internal/cli`. The
  CLI keeps a thin alias for source compatibility; `internal/mcpsrv`
  consumes the same construction path.
- **Shared exec orchestration** lives in `internal/exec.Orchestrate`.
  CLI `exec` and MCP `exec` go through the same code path; the MCP
  caller opts into the scope-open refusal via `RefuseScopeOpen: true`.

## v0.1.0

### Changed

- **Renamed project from `llm-mask` to `OpsMask`.** This is a breaking change
  with no compatibility shim:
  - Module path: `github.com/ching-kuo/llm-mask` → `github.com/ching-kuo/opsmask`.
  - Binary: `llm-mask` → `opsmask`.
  - Sentinel wire format: `⟪llm-mask:T:I⟫` / `[[llm-mask:T:I]]` →
    `⟪opsmask:T:I⟫` / `[[opsmask:T:I]]`. Reports and mapping stores produced
    by older versions cannot be unmasked with the new binary without manual
    sentinel rewriting.
  - Inert escape: `[LLM_MASK_ESCAPED_SENTINEL:…]` →
    `[OPSMASK_ESCAPED_SENTINEL:…]`.
  - Environment variables: `LLM_MASK_AUDIT_DIR`, `LLM_MASK_STORE_CHILD`,
    `LLM_MASK_STORE_PATH` → `OPSMASK_AUDIT_DIR`, `OPSMASK_STORE_CHILD`,
    `OPSMASK_STORE_PATH`.
  - Project config directory: `.llm-mask/` → `.opsmask/`. Existing projects
    must rename the directory and re-run `opsmask config trust`.
  - User config / audit log directory: `~/.config/llm-mask/` →
    `~/.config/opsmask/`.

### Added

- Gitleaks-derived common secret/token rule baseline pinned to upstream commit
  `8863af47d64c3681422523e36837957c74d4af4b`. Covers GitHub app/OAuth/PAT/fine-
  grained/refresh tokens, GitLab PAT/runner/CI job tokens, Slack app/bot/user/
  config/refresh/legacy/webhook shapes, OpenAI keys with the `T3BlbkFJ` marker,
  Anthropic admin and API keys, Stripe access tokens, GCP API keys, and Twilio
  API keys. Local extensions for Stripe publishable keys, webhook secrets, and
  pseudonymized resource IDs (`stripe_id`).
- JWT structural validator now requires both a JSON header with `alg`/`typ` and
  a JSON payload with at least one common registered claim
  (`sub`/`iss`/`aud`/`exp`/`nbf`/`iat`/`jti`), which closes the bearer-token
  false negative for payloads that only carry `sub`/`iat`.
- `MinEntropy` rule field with Shannon-entropy gating to filter low-entropy
  near-misses on high-cardinality secret formats.
- Additional Gitleaks-derived rules: npm access tokens, PyPI upload Macaroons,
  SendGrid API keys, DigitalOcean PAT/OAuth/refresh tokens, Linear API keys,
  and Postman access keys.
- Stripe object-ID coverage extended to `seti_`, `ba_`, `card_`, `src_`, `tok_`,
  and `txn_` prefixes alongside the existing charge/customer/intent set.
- `docs/DETECTOR_RULES.md` (rule sourcing, attribution, update procedure),
  `docs/CUSTOM_DETECTORS.md` (project-specific `regex_rules` cookbook),
  `docs/REMAINING_RISKS.md`, and `docs/THIRD_PARTY_NOTICES.md`.

### Security

- Common secret rules now use a negative-charset trailing delimiter
  (`(?:[^A-Za-z0-9_-]|$)` and variants) instead of a small punctuation
  whitelist, so JWTs and Stripe / OpenAI / Anthropic / GCP keys are still
  destroyed when followed by `,`, `)`, `]`, `}`, `&`, or `=`.
- Fixed-length token rules (OpenAI, Anthropic, GCP API key) now use the
  alphanumeric-only trailing delimiter `(?:[^A-Za-z0-9]|$)`. Excluding `-`
  and `_` from delimiters was overly restrictive — the body length already
  prevents greedy over-extension, and a hyphen-prefixed adjacent token (e.g.
  `key=sk-...-rotated`) was previously dropped from detection.
- `policy.BuiltinSecretTypes()` extended with `gitlab_token`, `stripe_key`,
  `stripe_publishable_key`, `stripe_webhook_secret`, `gcp_api_key`,
  `twilio_key`, `npm_token`, `pypi_token`, `sendgrid_key`,
  `digitalocean_token`, `linear_token`, and `postman_key` so user config
  cannot downgrade these to pseudonymize.

## v1.1.0 - 2026-04-28

### Added

- `opsmask exec` subcommand for sentinel-aware follow-up commands.
- Scope-tiered exec policy: `read-only`, `investigate`, and `freeform`.
- Hostname/FQDN and contextual Kubernetes resource-name detectors.
- JSON-lines exec audit log at `~/.config/opsmask/exec.log`.
- Tier-specific child environment allow-list with hard-deny stripping for
  interpreter startup and command-dispatch variables.
- Hard deny-list for shells, debuggers, REPLs, schedulers, remote-exec helpers,
  cluster/cloud mutation verbs, and known command-dispatch flags. `xargs` is in
  the hard deny-list because any command-bearing form is equivalent to
  arbitrary command construction.
- Streaming output re-masking: child stdout and stderr are piped through the
  masking engine line-by-line rather than buffered to memory, so large outputs
  do not balloon RSS and the agent sees masked output as it is produced.

### Changed

- `pseudo.Allocator` cache access is now mutex-protected for concurrent
  remasking paths.
- Config schema now supports a project-only `exec:` block with `scope`,
  `allow`, `allow_deny_opt_out`, `deny_opt_out`, `env_allow`, `env_deny`, and
  `default_timeout`.
- Legacy `exec.allow_shell` is rejected with a migration message pointing to
  `scope: freeform`.

### Security

- `exec` is disabled by default and user-wide config cannot enable it.
- `--config <file>` overrides cannot enable exec. Trust is anchored to the
  project's `.opsmask/config.yaml` (path-bound hash); an arbitrary `--config`
  path cannot satisfy the trust gate, so its `exec` block is ignored with a
  warning.
- `exec` preflights the audit log (`~/.config/opsmask/exec.log`) before
  spawning a child and refuses to run if the log is unwritable. Post-run write
  failures are surfaced to stderr so an exec invocation never leaves no audit
  trail silently.
- IPv4 detector tightened: prefix match no longer fires on word-adjacent
  letters (e.g. `host10.0.0.1`) while still matching after YAML/JSON escape
  letters (`\n`, `\t`, `\r`).
- Layer B `kubectl get/describe secret` denial now covers comma-joined resource
  lists (`pod,secret`), `secret/foo` and `secrets.v1` qualified forms, and any
  flag positioning of the namespace argument.
- Layer B `sed` execute-flag denial (`s/.../.../e`) uses precise patterns
  across common delimiters (`/`, `|`, `#`, `,`) instead of a substring search,
  so legitimate substitutions containing `/e` (e.g. `s/enabled/disabled/`) are
  no longer falsely rejected.
- Audit log file open now sets `O_CLOEXEC` per the documented invariant.
- Child processes inherit only stdin/stdout/stderr; other descriptors are
  marked close-on-exec before launch on POSIX systems.
- Child output is re-masked before it crosses back to agent-visible stdout or
  stderr.
- `argv[0]` is normalized via `filepath.Base` before all Layer B/C deny
  comparisons, so full-path invocations such as `/usr/bin/kubectl exec ...`
  cannot bypass command-family deny rules.
- Layer C dangerous-flag deny patterns for `curl`/`wget` are scoped to those
  binaries only; benign flags such as `dig -t`, `date -d`, `kubectl -f`, and
  `aws --output` are no longer falsely rejected. Generic dispatch flags
  (`--exec-path=`, `--rsh=`, `--checkpoint-action=exec=`, etc.) remain global.
- SIGTERM-on-cancel always escalates to SIGKILL after the kill-grace timeout,
  even when the wrapper is interrupted via Ctrl-C rather than `--timeout`.
