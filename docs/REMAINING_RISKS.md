# Remaining risks

Last updated: 2026-04-30

## MCP attack surface

- **Tool poisoning.** A malicious agent can craft `exec` or `mask_text`
  inputs designed to coerce a confused-deputy decision. The existing
  allow-list, deny-base, and scope-open refusal close the most direct
  paths; the residual risk is pattern complexity (a benign-looking
  argv that exploits a path bug in a downstream tool). Audit log
  preserves the full unresolved argv for forensics.
- **Token-result poisoning.** Subprocess output that contains
  adversarial sentinel-shaped strings is neutralized by the engine's
  inert-escape pass before re-masking. Plaintext that survives masking
  still flows back; this is the same surface as CLI exec, not an
  MCP-specific risk.
- **Mixed-process SQLite contention.** A long-lived MCP server can
  share `mapping.sqlite` with concurrent CLI invocations. The mixed
  long-lived + short-lived test in
  `internal/store/concurrency_multiprocess_mixed_test.go` covers the
  representative case but cannot enumerate every scheduler interleaving;
  production users may surface edge cases the test does not exercise.
- **Audit-write failure (non-exec).** `mcp_calls.jsonl` writes are
  fail-open: a non-exec MCP tool call (`mask_text`, `detect_text`,
  `mapping_stats`, `list_detectors`, mapping resource read) succeeds
  even when the audit append fails. The failure is logged to the
  server's stderr only; no MCP tool surface exposes audit-failure
  status. Operators who require fail-closed semantics for bulk masking
  should monitor the launching client's stderr capture (systemd
  journal, etc.).
- **MCP supply chain.** Single-binary distribution via signed
  GoReleaser artifacts mitigates typo-squat risk; OpsMask is not
  published to npm or pip.

## Detector sourcing and updates

This file captures known follow-up risks after the Gitleaks-derived detector
baseline and masking-gap fixes.

## Detector sourcing and updates

- Gitleaks-derived rules are pinned and curated, not automatically synced.
  Future upstream changes will not apply until deliberately reviewed and
  ported.
- Automatic download during `opsmask init` is intentionally avoided to preserve
  the no-network/default-reproducible trust model. If online updates are added,
  they should be explicit, pinned, hash-recorded, and trust-gated.
- Gitleaks rules are designed primarily for repository scanning. Some rules may
  need further tuning for streaming logs to avoid false positives or false
  negatives.

## Detector semantics

- Not every Gitleaks feature is fully modeled. `keywords`, `secretGroup`, and
  selected entropy thresholds are represented, but upstream allowlists and some
  repository-context assumptions are reviewed manually.
- Entropy thresholds can suppress low-entropy test/example-looking tokens. This
  reduces false positives but may miss deliberately low-entropy or unusual real
  secrets.
- JWT detection is structural, not cryptographic. It validates JWT-like header
  and payload shape but does not verify signatures or token expiry.

## Coverage gaps by design

- Application-specific identifiers remain custom config rules. Built-ins do not
  attempt to guess every `user_*`, `order_*`, `tenant_*`, `acct_*`, `req_*`, or
  `trace_*` convention.
- `StripeObjectID` covers the broad Stripe prefix set
  (`ch_|cus_|pi_|sub_|in_|re_|evt_|pm_|prod_|price_|seti_|ba_|card_|src_|tok_|txn_`)
  with a 14-char base62 body and `MinEntropy: 2`. The entropy floor rejects
  monocharacter app-local IDs sharing the prefix, but high-entropy app-local
  shapes (e.g. an internal `tok_<random alphanumeric>` token) can still
  pseudonymize through this rule. Pseudonymization is reversible per
  `user_secret`, so the worst case is mapping pollution rather than data loss;
  applications that use these prefixes for non-Stripe IDs should add an
  earlier-priority custom rule or rename the prefix.
- General non-debug PII filtering is future work. OpenAI Privacy Filter or
  similar model-based typed destruction is not integrated yet.
- High-entropy generic string detection remains out of scope until there is a
  separate false-positive strategy for hashes, build IDs, commit SHAs, and other
  benign high-entropy strings.

## Verification gaps

- `go test ./...`, `go vet ./...`, `make lint`, and fixture leak checks passed
  during implementation.
- Optional static tools were not installed in the environment, so `make lint`
  skipped `staticcheck`, `govulncheck`, and `gosec`.
- Fixture coverage is representative, not exhaustive. Additional provider-token
  fixtures should be added as new rule families are ported.
