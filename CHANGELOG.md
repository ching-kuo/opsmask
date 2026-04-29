# Changelog

## Unreleased

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

- `llm-mask exec` subcommand for sentinel-aware follow-up commands.
- Scope-tiered exec policy: `read-only`, `investigate`, and `freeform`.
- Hostname/FQDN and contextual Kubernetes resource-name detectors.
- JSON-lines exec audit log at `~/.config/llm-mask/exec.log`.
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
  project's `.llm-mask/config.yaml` (path-bound hash); an arbitrary `--config`
  path cannot satisfy the trust gate, so its `exec` block is ignored with a
  warning.
- `exec` preflights the audit log (`~/.config/llm-mask/exec.log`) before
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
