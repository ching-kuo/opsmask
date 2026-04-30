# Detector rule sourcing

`opsmask` treats the masker as the trust boundary: if a credential survives
`opsmask mask`, it may be sent to an LLM. Built-in rules therefore prioritize
high-confidence local detection with deterministic behavior.

## Source-of-truth model

Common secret/token detectors are derived from the Gitleaks default
configuration:

- Upstream: <https://github.com/gitleaks/gitleaks>
- Ruleset: `config/gitleaks.toml`
- Pinned revision reviewed for this pass:
  `8863af47d64c3681422523e36837957c74d4af4b`
- License: MIT, copyright Zachary Rice / Gitleaks contributors.
  See `docs/THIRD_PARTY_NOTICES.md`.

The rules are not fetched at runtime. `opsmask` ports a curated subset into
`internal/detect/rules/builtin.go` so masking remains local, reproducible, and
available without network access.

`opsmask init` also must not download rules. The project README promises no
network I/O, and detector updates are security-sensitive supply-chain changes.
If online refresh is added later, it should be an explicit command such as
`opsmask rules update --source gitleaks --ref <tag-or-sha>` that records the
upstream commit and SHA-256, writes a local rules file, and requires an explicit
trust step before activation.

## Porting policy

When porting a Gitleaks rule:

1. Use the Gitleaks regex as the upstream reference.
2. Keep Go/RE2 compatibility.
3. Map Gitleaks `keywords` to `Rule.Keywords` prefilters.
4. Use `SubMatch` when the upstream regex includes surrounding delimiter context
   and the captured secret should be the actual redaction span.
5. Map Gitleaks entropy thresholds to `MinEntropy` when they materially reduce
   false positives.
6. Review allowlists manually. Repository-scanning allowlists do not always fit
   streaming log masking.
7. When the upstream regex anchors the secret with a trailing-delimiter group,
   port it as a negative character class that excludes only the token's own
   charset (for example `(?:[^A-Za-z0-9_-]|$)`), not a small whitelist of
   delimiters. A whitelist will silently miss tokens followed by common log
   punctuation such as `,`, `)`, `]`, `}`, or `&`.
   - **Exception for fixed-length tokens.** When the body uses an exact `{N}`
     length quantifier (e.g. OpenAI proj keys at 74 or 58, Anthropic API keys
     at 93, GCP API keys at 35), greedy extension cannot occur, so excluding
     `-` and `_` from the trailing-delimiter class is unnecessary and silently
     drops keys followed by hyphen-prefixed adjacent tokens (`key=sk-…-rotated`).
     Use `(?:[^A-Za-z0-9]|$)` for these rules.
8. Add regression tests for the `opsmask` fixture and representative token
   shapes before claiming support.

## Current Gitleaks-derived families

The current curated baseline covers these common secret families:

- JWTs.
- PEM private keys.
- AWS-style access keys.
- GitHub app/OAuth/PAT/fine-grained/refresh tokens.
- GitLab PAT/runner/job token shapes.
- Slack app/bot/user/config/legacy/webhook tokens.
- OpenAI API keys.
- Anthropic API keys.
- Stripe secret/restricted access tokens.
- Google/GCP API keys.
- Twilio API keys.
- npm registry access tokens (`npm_…`).
- PyPI upload Macaroons (`pypi-AgEIcHlwaS5vcmc…`).
- SendGrid API keys (`SG.<id>.<secret>`).
- DigitalOcean PAT, OAuth, and refresh tokens (`dop_v1_…`, `doo_v1_…`,
  `dor_v1_…`).
- Linear API keys (`lin_api_…`).
- Postman access keys (`PMAK-…`).

All of these use `Destroy` policy: they are redacted and are not stored in the
mapping database.

## Local `opsmask` extensions

Some rules are intentionally local because `opsmask` protects LLM-bound logs,
not only source repositories:

- `password_url`: URL user-password credentials.
- `gcp_sa`: service-account JSON discriminator.
- `stripe_webhook_secret`: Stripe webhook signing secrets.
- `stripe_publishable_key`: publishable Stripe keys are not secret credentials,
  but they are account-linked and not needed by the LLM.
- `stripe_id`: Stripe resource IDs are pseudonymized, not destroyed, so incident
  analysis can correlate repeated billing objects without revealing the raw ID.
- Hostname, IP, email, Kubernetes, UUID, MAC, and long hex ID pseudonymizers are
  debug-oriented `opsmask` identifiers rather than Gitleaks secret rules.

Application-specific IDs such as `user_123`, `order_abc`, and `tenant_…` remain
custom project rules. See `docs/CUSTOM_DETECTORS.md`.

## Deliberate non-goals

- No runtime download of Gitleaks config.
- No automatic init-time download of Gitleaks config.
- No blind import of every upstream rule without false-positive review.
- No provider API verification. Tools such as TruffleHog can verify credentials
  by contacting providers, but `opsmask mask` must remain local and offline.
- No model-based PII filtering in this pass. OpenAI Privacy Filter or similar
  typed-destroy PII minimization is a future opt-in feature, not part of the
  deterministic secret rules.

## Future update procedure

1. Pick and record a new upstream Gitleaks commit.
2. Diff `config/gitleaks.toml` against the current pinned revision.
3. Review changed rules for log false positives and Go/RE2 compatibility.
4. Port selected changes into `internal/detect/rules/builtin.go`.
5. Update tests, this document, and `README.md`.
6. Run:

   ```sh
   go test ./...
   go vet ./...
   ./bin/opsmask mask < skill/opsmask/evals/files/app.log
   ```

Future explicit updater design:

1. User runs `opsmask rules update --source gitleaks --ref <tag-or-sha>`.
2. The command downloads `config/gitleaks.toml`, verifies and records SHA-256,
   source URL, and upstream commit/tag.
3. The command writes under `.opsmask/rules/` or generates reviewed
   `.opsmask/config.yaml` rules.
4. The rules remain inactive until the user runs an explicit trust command.
5. Normal `opsmask init` remains offline.

Supplemental references for future research:

- GitHub Advanced Security custom pattern examples:
  <https://github.com/advanced-security/secret-scanning-custom-patterns>
- TruffleHog detector/verification catalog:
  <https://github.com/trufflesecurity/trufflehog>
- Yelp `detect-secrets` plugin catalog:
  <https://github.com/Yelp/detect-secrets>
