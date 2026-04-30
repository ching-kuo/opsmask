# Custom detector cookbook

Built-in detectors cover common credentials and debug-useful infrastructure
identifiers. They intentionally do **not** try to guess every application-local
identifier shape, because names like `user_123`, `acct_abc`, `order_99`, and
`tenant_…` are deployment-specific and false-positive prone.

Use trusted project `regex_rules` for those values.

## Recommended default: pseudonymize app IDs

Prefer `policy: pseudonymize` for identifiers that help debugging. The LLM sees
a stable sentinel, so repeated references remain correlatable, while the raw
value stays local.

```yaml
# .opsmask/config.yaml
literals: []
regex_rules:
  - name: app-user-id
    type: app_user
    pattern: '\buser_\d+\b'
    policy: pseudonymize
  - name: app-order-id
    type: app_order
    pattern: '\border_[A-Za-z0-9]+\b'
    policy: pseudonymize
  - name: app-tenant-id
    type: app_tenant
    pattern: '\btenant_[0-9a-f-]{8,}\b'
    policy: pseudonymize
deny_list: []
exec:
  enabled: false
  scope: read-only
```

After editing config, trust it:

```sh
opsmask config trust
```

Trust is bound to the config path and file hash. Any edit requires running
`opsmask config trust` again before project rules apply.

Example:

```sh
printf 'user user_91237 placed order_X7Q9\n' | opsmask mask
```

Expected shape:

```text
user ⟪opsmask:app_user:...⟫ placed ⟪opsmask:app_order:...⟫
```

## Other common project shapes

```yaml
regex_rules:
  - name: internal-account
    type: app_account
    pattern: '\bacct_[A-Za-z0-9_-]{6,}\b'
    policy: pseudonymize
  - name: request-id
    type: request_id
    pattern: '\b(?:req|trace)_[A-Za-z0-9_-]{8,}\b'
    policy: pseudonymize
  - name: ticket-id
    type: ticket_id
    pattern: '\b[A-Z]{2,10}-\d{2,}\b'
    policy: pseudonymize
```

## When to use destroy

Use `policy: destroy` when the value is a credential or when correlation itself
is unsafe or unnecessary.

```yaml
regex_rules:
  - name: legacy-session-cookie
    type: legacy_session
    pattern: '\blegacy_session_[A-Za-z0-9_-]{24,}\b'
    policy: destroy
```

Destroyed values are replaced with `[REDACTED_<TYPE>]` and cannot be unmasked.

## Notes

- Keep patterns as specific as possible.
- Start with pseudonymization for debug identifiers.
- Use test fixtures with realistic logs before trusting broad regexes.
- Do not add app-specific IDs to `internal/detect/rules/builtin.go`; built-ins
  should remain provider/common-format rules or broadly useful debug
  identifiers.
