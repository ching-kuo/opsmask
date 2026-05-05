---
title: "refactor: Hostname detector backed by Public Suffix List"
type: refactor
status: active
date: 2026-05-05
origin: docs/brainstorms/2026-05-05-hostname-detector-public-suffix-list-requirements.md
---

# refactor: Hostname detector backed by Public Suffix List

## Summary

Replace the Hostname rule's `nonFQDNTLDs` denylist + `validHostname` Check
with a Public Suffix List (PSL) backed Check that accepts a candidate iff
PSL recognizes its suffix (ICANN-managed or privately-managed) or its
last label sits in a small RFC-reserved internal-TLD allowlist
(`local`, `internal`, `lan`, `home`, `localhost`, `arpa`, `corp`,
`intranet`, `test`). A short fixed list of high-frequency code/log
ccTLD collisions (`go`, `py`, `rs`, `sh`, `md`) is rejected after PSL
acceptance to keep source-file paths from masking. A new trust-gated
config block (`detectors.hostname.internal_tlds`) lets self-hosters
extend the internal allowlist for organization-specific suffixes.

---

## Problem Frame

The current `validHostname` Check is a curated 80+ entry denylist of
last-label tokens (file extensions, framework conventions, OpenStack
Python module names). Each integration-test report against a new
ecosystem produces a fresh round of false positives that need new
denylist entries — the maintenance trajectory is whack-a-mole. PSL is
already maintained by Mozilla and shipped via `golang.org/x/net`
(already an indirect dep), and structurally distinguishes "registered
network suffix" from "arbitrary dotted lowercase identifier" without
per-ecosystem updates.

See origin: `docs/brainstorms/2026-05-05-hostname-detector-public-suffix-list-requirements.md`.

---

## Requirements

- R1. Hostname matches must be backed by a structural PSL check that
  does not require per-ecosystem maintenance to stay precise.
- R2. PSL is the source of truth for registered suffixes; both ICANN-
  managed and privately-managed suffixes count (so private suffixes
  like `s3.amazonaws.com` and `appspot.com` are recognized without
  special casing).
- R3. Internal/private TLDs not in PSL are recognized via a fixed
  default set: `local`, `internal`, `lan`, `home`, `localhost`,
  `arpa`, `corp`, `intranet`, `test`.
- R4. Project config (`.opsmask/config.yaml`) accepts an additional
  list of internal TLDs that extend the default set when the
  trusted-project gate is satisfied (same gate as `exec` config —
  user-wide and `--config` overrides cannot widen masking).
- R5. The config field is additive only; no way to *exclude* TLDs
  from masking.
- R6. The current `validHostname` Check, `nonFQDNTLDs` map, and
  associated tests are removed entirely. PSL + internal-TLD allowlist
  subsumes them.
- R7. The existing Hostname regex (lowercase, 3+ labels, max-24-char
  TLD) is retained as a fast pre-filter. PSL evaluation runs in the
  rule's `Check` slot only.

**Origin acceptance examples** (all carried forward):

- AE1 (R1, R2): `nova.api.openstack.wsgi` → rejected (`wsgi` not in PSL, not internal).
- AE2 (R1, R2): `api.example.com` → masked (`com` is ICANN-managed).
- AE3 (R3): `worker-1.cluster.local` → masked (`local` is RFC-reserved internal).
- AE4 (R4): with trusted project config `internal_tlds: [acme]`,
  `db-1.prod.acme` → masked. *Note on origin AE4*: the brainstorm
  used `db-1.acme` (2 labels) as the example, but R7 retains the
  3+ label regex prefilter, so 2-label inputs never reach the
  Check. The plan reinterprets AE4 with a 3-label fixture
  (`db-1.prod.acme`), which preserves the product intent — masking
  works for project-defined internal TLDs in real operational
  hostnames — without changing R7. Bare 2-label `<host>.<tld>` is
  out of scope for the Hostname rule and was already so under the
  current denylist behavior.
- AE5 (R4): same 3-label input, no trusted config (or `--config` only)
  → NOT masked.
- AE6 (R1): greedy regex match `nova.api.openstack.compute.versions` → rejected.

---

## Scope Boundaries

- Context-anchored hostname detection (URLs, emails, `host=foo` k/v) is
  not added — the user explicitly opted to keep a generic hostname rule.
- DNS resolution / network-side validation is out of scope.
- Auto-refresh tooling for PSL beyond `go get -u golang.org/x/net` is
  out of scope.
- The existing Hostname regex is not changed — PSL is added to the
  Check slot only (R7).
- Configuration to *exclude* TLDs from masking is not added (R5).
- The two unselected approaches from brainstorm (bundled IANA root
  list, structural inversion via 4+ pure-alpha labels) remain rejected.

### Deferred to Follow-Up Work

- `opsmask explain hostname <input>` subcommand for operator
  debugging — useful but not required for v1. Closes the
  observability gap noted in System-Wide Impact.
- Consider promoting the trust-gated `detectors` block to other
  detectors (e.g., k8s noun extensions) once a second consumer needs
  it. For now, only `detectors.hostname.internal_tlds` is wired.
- An MCP `detector_config` view that surfaces per-rule effective
  configuration (collision set, internal TLDs) — only if operators
  ask for it.
- A fuzz target for `HostnameCheckFor` over PSL-shaped inputs to
  guard against future library-behavior drift on edge cases.

---

## Context & Research

### Relevant Code and Patterns

- `internal/detect/rules/builtin.go:41` — Hostname rule spec; regex
  unchanged.
- `internal/detect/registry.go:33-61` — `BuiltinRules` wires the
  Hostname rule's `Check` to `validHostname`. The new Check needs to
  be parameterizable on the merged internal-TLD set (default + project
  config), so wiring will move from a static function reference to a
  closure constructor.
- `internal/detect/registry.go:213-265` — `nonFQDNTLDs` and
  `validHostname` are deleted in this plan.
- `internal/detect/hostname_tld_test.go` — existing table-driven test
  is rewritten against the PSL contract.
- `internal/config/config.go:23-28` — `Config` struct; `Detectors`
  block is added at the same level as `Exec`.
- `internal/config/config.go:143-218` — `Loaded.ProjectExec` and the
  trust-gating logic in `Load`. The new `detectors` block is loaded
  only from trusted project paths, mirroring `ProjectExec`. User-wide
  and `--config` `detectors` blocks are warned and ignored.
- `internal/runtime/runtime.go:56-83` — current wiring builds
  `builtins` first, then appends `loaded.Rules`. The Hostname rule's
  Check is rebound here after config is loaded so the configured
  internal TLDs reach the Check.

### Institutional Learnings

- Trust-gating is established (`exec` block + `IsTrusted`); reuse the
  same anchor (`.opsmask/config.yaml` discovered via
  `findProjectConfig`). User-wide and `--config` exec blocks are
  ignored with a warning — the new `detectors` block uses the same
  treatment.
- Hostname regex precision was tightened in two prior iterations
  (lowercase + 3+ labels, last-label denylist). PSL replaces the
  second; the first is retained as fast pre-filter.

### External References

- `https://pkg.go.dev/golang.org/x/net/publicsuffix` — `PublicSuffix(d)
  → (suffix, icann)`. For unrecognized inputs the function falls back
  to the last label of the input with `icann=false`. Multi-label
  private suffixes (e.g., `cloudfront.net`, `appspot.com`,
  `s3.amazonaws.com`) return the multi-label suffix with `icann=false`.
- `https://publicsuffix.org/list/` — PSL data, embedded in
  `golang.org/x/net/publicsuffix` and updated with each release of the
  module.

---

## Key Technical Decisions

- **PSL recognition test: `icann == true || strings.Contains(suffix, ".")`.**
  ICANN-managed suffixes (`com`, `co.uk`, `py`, `rs`) return
  `icann=true`. Privately-managed multi-label suffixes
  (`s3.amazonaws.com`, `appspot.com`) return `icann=false` but with a
  multi-label `suffix`. Unrecognized inputs return the bare last label
  with `icann=false` — these fall through to the internal-TLD
  allowlist check. Verified empirically against
  `golang.org/x/net@v0.53.0` (`publicsuffix/list.go:73-140`).
- **Predicate safety relies on the regex prefilter.** Direct calls to
  `publicsuffix.PublicSuffix` on inputs like IP literals (`1.2.3.4`),
  trailing-dot domains (`a.b.com.`), or punycode IDN labels behave
  unevenly (the godoc explicitly marks IDN/case behavior as TODO at
  `publicsuffix/list.go:48-49`). The Hostname regex
  (`\b(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.){2,}[a-z]{2,24}\b`)
  excludes these classes before `Check` runs, so the production path
  is safe. The Check helper's contract documents this dependency, and
  unit tests assert defensive behavior on edge cases passed directly.
- **Default internal-TLD set: RFC-reserved only.** `local`, `internal`,
  `lan`, `home`, `localhost`, `arpa`, `corp`, `intranet`, `test`.
  Carried in source (no config required) so out-of-the-box behavior
  matches the existing test suite for `cluster.local` /
  `compute.internal` cases.
- **Narrow code/log ccTLD-collision rejection (compatibility
  exception).** A fixed five-entry set — `go`, `py`, `rs`, `sh`,
  `md` — is rejected after PSL acceptance. This set is framed
  explicitly as a compatibility exception, NOT a general extension
  point: it preserves the fixture-validated rejection behavior the
  current denylist offers for these specific labels, where
  `pkg.subpkg.go` / `some.module.py` / `some.module.rs` /
  `path.to.script.sh` / `notes.subdir.md` are dominant code/log
  artifacts that would otherwise mask under PSL. The set is closed
  by default. Additions require a failing integration fixture
  reproducing the false-positive in a real test report — not just
  prose evidence — and a CHANGELOG entry naming the ecosystem and
  the fixture. Removals from this set require explicit operator
  signoff because each entry was originally added for fixture-level
  regression coverage. Framework names, OpenStack module
  conventions, and other non-ccTLD noise are NOT eligible — those
  are rejected by PSL absence and need no maintenance. The set is
  documented in source with rationale per entry. This narrows the
  R6 deletion: the whack-a-mole `nonFQDNTLDs` map and
  `validHostname` function are removed, while five high-confidence
  collisions survive as a separate, bounded compatibility shim.
- **Trust gate: identical to `exec` placement.** Detector config is
  parsed in `Config` by both `Load` and `LoadFile`. `Load` copies
  `cfg.Detectors` into `Loaded.ProjectDetectors` only on the trusted
  project branch (untrusted project: not loaded; user-wide: ignored
  with a warning emitted via the existing `stderr` callback in
  `config.go:154-217`). `LoadFile` populates `Loaded.ProjectDetectors`
  the same way it populates `ProjectExec`, and the warn-and-ignore
  for `--config` happens in `runtime.New` — symmetrical to the
  existing exec handling at `runtime.go:66-79`. `LoadFile`'s API is
  not changed.
- **Config schema: `detectors.hostname.internal_tlds: [...]`.** Nested
  shape leaves room for future per-detector knobs (e.g.,
  `detectors.k8s.*`) without flattening into top-level keys. Mirrors
  the existing `exec` block's nested shape.
- **Constructor API over rebinding by search.** `internal/detect`
  exposes `HostnameCheckFor(extra []string) func([]byte) bool`.
  `BuiltinRules` calls `HostnameCheckFor(nil)` for the default Check.
  `runtime.New` rebuilds the Hostname rule (or its Check field) from
  the loaded project additions before `Env.Rules` is published. No
  package mutates rule slices after publication; no concurrent-read
  hazard exists because `Env.Rules` is constructed fully before the
  return.
- **Residual ccTLD-collision exposure beyond the five-entry set is
  accepted.** The five-entry compatibility set (`go`, `py`, `rs`,
  `sh`, `md`) covers the dominant code/log false positives. PSL
  marks many *other* ccTLDs that occasionally appear as trailing
  labels in dotted lowercase identifiers (`.do`, `.in`, `.is`,
  `.it`, `.me`, `.so`, `.to`, `.tv`, `.cc`, `.gg`, `.fm`, `.am`,
  `.pm`, `.re`, `.id`, `.vc`, etc.). These outside-the-set ccTLDs
  are NOT rejected — adding them would re-create the whack-a-mole
  pattern. Operators who hit a high-volume case in their
  environment cannot opt those labels out via `internal_tlds`
  (which is additive, not subtractive); the workaround is a
  project-defined `regex_rules` entry that masks the affected
  paths. Documented as a residual in REMAINING_RISKS.md, with
  CHANGELOG explicitly distinguishing the rejected five from the
  accepted residue.

---

## Open Questions

### Resolved During Planning

- **PSL Check normalization** (origin deferred): The Hostname regex
  guarantees lowercase a-z0-9 input bounded by `\b`. Trailing dots are
  not possible in the captured bytes. No normalization needed beyond
  `string(b)`. (See AE6: greedy match has no trailing punctuation.)
- **Config schema location** (origin deferred): `detectors.hostname.
  internal_tlds`. Nested under a new `detectors` top-level block,
  mirroring `exec`. Leaves room for future per-detector configuration
  without rewriting the schema.
- **PSL semantics for private suffixes**: Verified empirically — the
  combined check `icann || strings.Contains(suffix, ".")` covers both
  ICANN and multi-label private suffixes correctly.

### Deferred to Implementation

- Whether to keep `golang.org/x/net` as `// indirect` in `go.mod` or
  promote to direct require: depends on `go mod tidy` output after
  the import is added. Resolve by running `go mod tidy` post-edit.
- Exact warning text wording when user-wide or `--config`
  `detectors` blocks are detected — match the style of the existing
  `exec` warning in `runtime.go:78`.

---

## Implementation Units

- U1. **PSL-backed Hostname Check + default internal-TLD set + ccTLD-collision rejection**

**Goal:** Replace `validHostname` (denylist) with a PSL-driven Check
that uses the RFC-reserved default internal-TLD set and a narrow
five-entry ccTLD-collision rejection. Delete `nonFQDNTLDs` and
`validHostname`. Expose `detect.HostnameCheckFor(extra []string)
func([]byte) bool` so runtime can supply project additions. Wire
`BuiltinRules` to call `HostnameCheckFor(nil)` for the default.
Update `internal/detect/hostname_tld_test.go` to assert the new
contract.

**Requirements:** R1, R2, R3, R6, R7

**Dependencies:** None.

**Files:**
- Modify: `internal/detect/registry.go` (delete `nonFQDNTLDs` and
  `validHostname`; add `defaultInternalTLDs` map,
  `codeLogCcTLDCollisions` map (`go`, `py`, `rs`, `sh`, `md`); add
  the exported `HostnameCheckFor(extra []string) func([]byte) bool`
  constructor; add the exported `ReservedTLDStatus(label string)
  (TLDStatus, reason string)` helper plus the `TLDStatus` type with
  values `TLDFree`, `TLDDefaultInternal`, `TLDCollision`; wire
  `BuiltinRules` to call `HostnameCheckFor(nil)` for the Hostname
  rule).
- Modify: `internal/detect/hostname_tld_test.go` (replace fixtures —
  see Test scenarios).
- Add: `internal/detect/hostname_psl_helper_test.go` (direct unit
  tests for `HostnameCheckFor` edge cases bypassing the regex —
  see Test scenarios).
- Modify: `go.mod`, `go.sum` (promote `golang.org/x/net` to direct;
  run `go mod tidy`).

**Approach:**
- Add import `golang.org/x/net/publicsuffix` in `registry.go`.
- Define `defaultInternalTLDs = map[string]bool{...}` with the nine
  RFC-reserved entries from R3.
- Define `codeLogCcTLDCollisions = map[string]bool{"go": true,
  "py": true, "rs": true, "sh": true, "md": true}` with a comment
  documenting the bounded charter (see Key Technical Decisions:
  "Narrow code/log ccTLD-collision rejection") and per-entry
  rationale.
- Define the `TLDStatus` type as `type TLDStatus int` with three
  exported constants in `iota` order: `TLDFree`, `TLDDefaultInternal`,
  `TLDCollision`.
- Implement `ReservedTLDStatus(label string) (TLDStatus, reason string)`:
  - If `defaultInternalTLDs[label]` → return
    `(TLDDefaultInternal, "RFC-reserved internal TLD")`.
  - Else if `codeLogCcTLDCollisions[label]` → return
    `(TLDCollision, "compatibility-exception ccTLD: <label>")`
    with the reason text shaped so `internal/config` can embed it
    in a parse error pointing operators at `regex_rules`.
  - Else → return `(TLDFree, "")`.
  - The function does no normalization (caller is expected to pass
    a lowercase RFC-1123 label); behavior on other inputs is
    undefined.
- Define `HostnameCheckFor(extra []string) func([]byte) bool`:
  - Build `extras := map[string]bool{}` from the slice.
  - Closure body, given a candidate `b`:
    - **Preflight defensive rejections** (guard against direct
      callers bypassing the regex prefilter; production rule path
      already rejects these via the regex):
      - If `len(b) == 0` → reject (empty input).
      - If `bytes.IndexByte(b, '.') < 0` → reject (no dot, can't be
        a multi-label hostname).
      - If `b[len(b)-1] == '.'` → reject (trailing dot — PSL
        library's behavior is implementation-detail per
        `publicsuffix/list.go:48-49`).
      - If `netip.ParseAddr(string(b))` succeeds → reject (IP
        literal returns a dotted suffix from PSL that would
        otherwise spuriously satisfy the `strings.Contains(s, ".")`
        branch).
    - `s, icann := publicsuffix.PublicSuffix(string(b))`
    - If `icann || strings.Contains(s, ".")`: PSL recognized.
      - If the *last label* of `b` is in `codeLogCcTLDCollisions`
        AND the suffix `s` equals that last label (i.e., the
        recognition is via a single-label ICANN ccTLD that
        collides with a known code/log extension), reject.
      - Otherwise accept.
    - Otherwise (`s` is a single-label fallback): accept iff
      `defaultInternalTLDs[s] || extras[s]`.
- The collision check is structured to fire only for single-label
  collisions (`some.module.py` → suffix `py`), not for paths like
  `app.py.example.com` where PSL recognized via `com`.
- `BuiltinRules` wires `r.Check = detect.HostnameCheckFor(nil)` for
  the Hostname rule.
- Delete `nonFQDNTLDs` and the old `validHostname`.
- Document on the constructor that the predicate is sound only on
  inputs satisfying the Hostname regex (lowercase a-z0-9 labels, 3+
  labels, last label 2-24 chars, bounded by `\b`); direct callers
  outside the rule pipeline must enforce the same shape.

**Patterns to follow:**
- `validK8sName` in `registry.go:199-211` — same Check signature
  (`func([]byte) bool`).
- The hostname rule's existing Check is the right slot (see
  `BuiltinRules` lines 55-57); preserve that wiring shape.

**Test scenarios (`hostname_tld_test.go` — full pipeline via FindMatches; covers AE1, AE2, AE3, AE6 from origin. AE4 is covered in U3 via runtime config; AE5 is split between U2 (config-layer trust gate) and U3 (`--config` runtime gate)):**
- Happy path: `api.example.com` → masked. **Covers AE2.**
- Happy path: `node-04.cluster.local` → masked (RFC-reserved).
  **Covers AE3.**
- Happy path: `db-1.us-east-2.compute.internal` → masked.
- Happy path: `mail.example.org` embedded in surrounding prose →
  masked, with surrounding bytes preserved.
- Happy path: PSL private multi-label suffix —
  `bucket.s3.amazonaws.com` → masked (`s3.amazonaws.com` is private
  in PSL). **Covers R2.**
- Edge case: `nova.api.openstack.wsgi` → rejected (single-label `wsgi`
  is not in PSL, not internal). **Covers AE1.**
- Edge case: greedy match on
  `<nova.api.openstack.compute.versions.Versions object at 0x7f12>` →
  the lowercase-only regex captures
  `nova.api.openstack.compute.versions`; Check rejects (`versions`
  single-label not in PSL/internal). **Covers AE6.**
- Edge case: `a.b.c.json`, `foo.bar.yaml` → rejected.
- Edge case: `keystone.server.flask`, `neutron.plugins.ml2` → rejected.
- Edge case: `latest.api.log`, `nova.api.log` → rejected.
- Code/log ccTLD-collision rejection (RETAINED from existing test):
  `pkg.subpkg.go`, `some.module.py`, `some.module.rs`,
  `path.to.script.sh`, `notes.subdir.md` → rejected (PSL accepts via
  ICANN ccTLD, but the five-entry collision filter rejects).
- Edge case (regex is the gatekeeper, not the Check):
  `cmd.Flags`, `package.json` (2-label) → never reach the Check
  because the regex requires 3+ labels.
- Negative coverage: `app.py.example.com` (3-label path containing a
  collision-set token but recognized via `com`) → masked — confirms
  the collision filter only fires for single-label ICANN matches.

**Test scenarios (`hostname_psl_helper_test.go` — direct helper, no regex):**
- IP literal: `1.2.3.4` passed directly → rejected (the helper
  documents the regex-prefilter dependency; this test asserts
  defensive behavior — a recognized public-suffix predicate should
  not fire on dotted-numeric input. Implementation: short-circuit
  if the candidate parses as `netip.Addr` before consulting PSL).
- Trailing dot: `api.example.com.` → rejected (the trailing-dot
  fallback is implementation-detail of the PSL library; defensive
  rejection guards against future library changes).
- Empty input: `""` → rejected.
- Single-label input: `localhost` → rejected (regex would never let
  this reach the Check, but the helper short-circuits inputs without
  at least one dot).
- Punycode IDN: `xn--nxasmq6b.example.com` (3-label, satisfies the
  Hostname regex) → masked. Confirms the helper does not break
  punycode-shaped ASCII input. (A 2-label `xn--example.com` would
  never reach the Check because the regex requires 3+ labels.)
- Configurable internal TLD (helper-direct, no regex prefilter):
  `HostnameCheckFor([]string{"acme"})` → `db-1.prod.acme` accepted;
  same helper with no extras → `db-1.prod.acme` rejected. (Bare
  2-label `db-1.acme` would never reach the Check in production —
  the 3-label regex excludes it. The helper is regex-shape-agnostic,
  so test inputs use 3-label forms to mirror production.)

**Verification:**
- `go test -race ./internal/detect/...` passes including the rewritten
  hostname test.
- `nonFQDNTLDs` and `validHostname` are deleted from
  `internal/detect/registry.go`.
- `go vet ./...` is clean.

---

- U2. **Config schema: `detectors.hostname.internal_tlds`**

**Goal:** Add a `Detectors` block to `config.Config` with
`Hostname.InternalTLDs []string`. Plumb into `config.Loaded` such that
project-trusted detectors land in `Loaded.ProjectDetectors`, while
user-wide and `--config` `detectors` blocks are warned and ignored.

**Requirements:** R4, R5

**Dependencies:** U1 (the Check constructor must already accept the
extra map).

**Files:**
- Modify: `internal/config/config.go` (add `Detectors`,
  `DetectorsConfig`, `HostnameDetectorConfig` types; extend `Loaded`
  with `ProjectDetectors DetectorsConfig`; mirror `ProjectExec`
  treatment in `Load` and `LoadFile`; emit warnings for user-wide and
  `--config` `detectors` blocks).
- Modify: `internal/config/config_test.go` (add cases — see Test
  scenarios).

**Approach:**
- New types in `internal/config/config.go`:
  ```
  type DetectorsConfig struct {
      Hostname HostnameDetectorConfig `yaml:"hostname"`
  }
  type HostnameDetectorConfig struct {
      InternalTLDs []string `yaml:"internal_tlds"`
  }
  ```
- Add `Detectors DetectorsConfig` field to `Config`.
- Add `ProjectDetectors DetectorsConfig` field to `Loaded`.
- In `Load` (mirrors existing exec gate at `config.go:154-217`):
  - Parse the user-wide config as before. If `cfg.Detectors` is
    non-zero, append a warning ("user-wide detectors block is
    ignored; configure detectors via trusted project
    .opsmask/config.yaml") via the existing `stderr` callback. Do
    NOT populate `Loaded.ProjectDetectors`.
  - On the trusted project branch, copy `cfg.Detectors` into
    `Loaded.ProjectDetectors`.
  - On the untrusted project branch, do not populate (existing
    early-return covers this).
- In `LoadFile` (used by `--config`): populate
  `Loaded.ProjectDetectors` from the parsed config exactly the way
  `LoadFile` populates `Loaded.ProjectExec` today (no warning here —
  `LoadFile` has no `stderr` callback). The warn-and-ignore for
  `--config` lives in `runtime.New`, symmetric to the existing
  ExecConfig handling at `runtime.go:66-79`.
- Export a small validation helper from `internal/detect`:
  `ReservedTLDStatus(label string) (status detect.TLDStatus,
  reason string)` returning one of `TLDFree`, `TLDDefaultInternal`,
  `TLDCollision`. The `internal/config` package consumes only this
  exported helper — it does not reach into the unexported maps
  `defaultInternalTLDs` / `codeLogCcTLDCollisions` directly.
- Validation in a new `validateDetectorsConfig(*DetectorsConfig)`
  helper invoked from `parseConfig`:
  - Each TLD must match `^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`
    (RFC 1123 label, lowercase, max 63 chars, no dots).
  - Trim whitespace.
  - Reject duplicates and empty entries with a parse error so
    misconfigurations fail early rather than silently no-op.
  - For each entry, consult `detect.ReservedTLDStatus`:
    - `TLDDefaultInternal` → parse error: redundant, the label is
      already in the RFC-reserved default set.
    - `TLDCollision` → targeted parse error: the label collides
      with a fixed code/log compatibility-exception ccTLD
      (`go`/`py`/`rs`/`sh`/`md`) and cannot be overridden via
      `internal_tlds`. The error text explains why (so an operator
      who wants `.py` as a project internal TLD knows the override
      is intentionally disallowed and that the fix is to mask
      `.py` paths via a project-defined regex rule instead).
    - `TLDFree` → accepted.

**Patterns to follow:**
- `Exec` block in `internal/config/config.go:70-113` — nested struct
  with its own `UnmarshalYAML` for validation.
- Trust gating in `Load` lines 178-217 — same path discrimination
  (project trusted vs user-wide vs --config).
- Warning emission via the `stderr` callback (lines 169-176 for
  exec).

**Test scenarios:**
- Happy path: trusted project config with
  `detectors: { hostname: { internal_tlds: [acme, corp-internal] } }`
  → `Loaded.ProjectDetectors.Hostname.InternalTLDs` contains both.
- Trust gate: same config in user-wide path → warning emitted via
  `stderr` callback, `Loaded.ProjectDetectors` is the zero value.
  **Covers AE5.**
- Trust gate: same config loaded via `LoadFile` →
  `Loaded.ProjectDetectors` IS populated at the config layer (the
  warn-and-ignore lives in `runtime.New`, asserted in U3). This
  matches existing `ProjectExec` behavior in `LoadFile`.
- Trust gate: untrusted project config → no detectors loaded
  (existing untrusted branch already returns early).
- Validation: `internal_tlds: [Foo]` (uppercase) → parse error.
- Validation: `internal_tlds: ["a.b"]` (contains dot) → parse error.
- Validation: `internal_tlds: [""]` → parse error.
- Validation: `internal_tlds: [acme, acme]` → parse error
  (duplicates).
- Validation: `internal_tlds: [local]` (overlap with default) →
  parse error citing "label is already a default RFC-reserved
  internal TLD".
- Validation: `internal_tlds: [py]` (overlap with collision set) →
  targeted parse error citing the compatibility-exception ccTLD
  set, naming the colliding label, and pointing operators at
  project-defined `regex_rules` as the workaround for masking
  `.py`-suffix paths.
- Validation: empty `detectors:` block parses cleanly with zero
  values.

**Verification:**
- `go test -race ./internal/config/...` passes.
- Roundtrip: a trusted project YAML containing `detectors.hostname.
  internal_tlds: [acme]` produces the expected `Loaded` struct.

---

- U3. **Runtime wiring: trust-gated rebind of Hostname Check**

**Goal:** After `config.Load` returns in `runtime.New`, rebind the
Hostname rule's `Check` using the project-trusted `internal_tlds`
extension. Detector config supplied via `--config` is warned and
ignored — symmetric to the existing exec handling.

**Requirements:** R4, R5

**Dependencies:** U1 (constructor exported), U2 (config schema).

**Files:**
- Modify: `internal/runtime/runtime.go`:
  - After `loaded` is constructed, build the effective hostname
    extras as `loaded.ProjectDetectors.Hostname.InternalTLDs`.
  - When `opts.Config != ""`, mirror the existing exec warning at
    `runtime.go:77-79`: if `explicit.ProjectDetectors.Hostname.
    InternalTLDs` is non-empty, emit a warning and IGNORE it (do
    not merge into the effective extras).
  - Walk `builtins` by index (`for i := range builtins`), assigning
    `builtins[i].Check = detect.HostnameCheckFor(extras)` for the
    rule whose `Type == "hostname"`. A range-value loop would
    mutate a copy and silently drop the rebind — implementers must
    use index assignment.
  - Construct `Env.Rules` from the patched `builtins` and only then
    return — no rule slice is published before the rebind.
- Add: `internal/runtime/runtime_test.go`.
- Add: `internal/mcpsrv/tools_text_test.go` (or extend the existing
  one) to assert that an MCP `mask` call with a trusted project
  `internal_tlds: [acme]` masks `db-1.prod.acme`, while the same
  input via `--config` does not. Codex flagged that MCP impact is
  asserted but not tested; this closes that gap.

**Patterns to follow:**
- `runtime.New` already mutates `builtins` indirectly (via
  `BuiltinRules` returning a fresh slice) — keep the rebind local to
  `runtime.New`, do not export Check rebinding from any other
  package.

**Test scenarios:**
- Happy path: with no project config, runtime's hostname rule
  rejects `db-1.prod.acme` (single-label `acme` not in PSL, not in
  default internal set; passes the 3+ label regex prefilter so the
  Check fires). **Covers AE5.**
- Happy path: with trusted project config supplying
  `internal_tlds: [acme]`, runtime's hostname rule masks
  `db-1.prod.acme`. **Covers AE4 (reinterpreted as 3-label per
  Requirements section).**
- Trust gate: with the same config provided via `--config`,
  runtime's hostname rule still rejects `db-1.prod.acme` and a
  warning is written to the runtime's `warn` writer. **Covers AE5.**
- Two-runtime isolation: construct two `runtime.New` instances with
  different `internal_tlds` lists; assert each one's hostname Check
  reflects only its own additions (no shared state across runtimes).
- Integration: end-to-end masking via `engine.Mask` on
  `db-1.prod.acme accessed at 12:00` produces a sentinel only when
  the trusted project config is in place.
- MCP integration: the `mask` MCP tool reflects the project-trusted
  `internal_tlds` (because it consumes `rt.Rules`); the same call
  through a `--config`-only path does not.
- Cross-rule overlap: the masked output for `https://user:pw@api.foo.com`
  is the PasswordURL sentinel (not the Hostname sentinel embedded
  inside it) — confirms `nonOverlapping` ordering still wins after
  the Check change.
- Cross-rule overlap: `user@mail.example.org` produces a single
  Email sentinel covering the whole token; the embedded
  `mail.example.org` is not separately masked by the Hostname rule.

**Verification:**
- `go test -race ./internal/runtime/... ./internal/detect/...
  ./internal/config/...` passes.
- The rebind happens once per `runtime.New`; no per-chunk allocation
  regression on the hot path (the closure captures a `map[string]bool`
  by reference; same shape as the existing static check).

---

- U4. **Documentation: README, CHANGELOG, REMAINING_RISKS**

**Goal:** Document the PSL-based hostname detector, the new config
field, and the accepted ccTLD-collision regressions.

**Requirements:** Operator-facing aspects of R3, R4, R6.

**Dependencies:** U1, U2, U3 merged.

**Files:**
- Modify: `README.md` (document `detectors.hostname.internal_tlds`
  config field; mention PSL as the masking precision source; show a
  short YAML snippet for self-hosters with custom internal TLDs).
- Modify: `CHANGELOG.md` (entry under "Unreleased": describe the
  PSL switch; call out the `detectors` config schema; explicitly
  distinguish the **rejected** five-entry compatibility set
  (`.go`/`.py`/`.rs`/`.sh`/`.md`) from the **accepted** residual
  ccTLDs outside the set (`.do`/`.in`/`.is`/`.it`/`.me`/etc.). Name
  the operator workaround for residual cases: project-defined
  `regex_rules` to mask the affected paths).
- Modify: `docs/REMAINING_RISKS.md` (new bullet titled "Residual
  ccTLD masking on dotted-lowercase identifiers". Body describes:
  PSL-recognized ICANN ccTLDs outside the five-entry compatibility
  set will mask multi-label paths whose last label happens to be
  one of those ccTLDs — affects identifiers ending in
  `.do`/`.in`/`.is`/`.it`/`.me` etc. The five-entry set
  (`.go`/`.py`/`.rs`/`.sh`/`.md`) is explicitly rejected and is NOT
  affected. Operator workaround: project-defined `regex_rules` for
  the affected paths; PasswordURL/Email rules already context-anchor
  real network identifiers).
- Modify: `CLAUDE.md` (if it documents the hostname rule's behavior;
  otherwise no change).

**Patterns to follow:**
- Existing CHANGELOG entry style (see commits like
  `21aa0d0` / 2026-05-05 K8s false-positive fixes).
- REMAINING_RISKS.md's existing bulleted shape.

**Test scenarios:** *(documentation-only unit)*

Test expectation: none -- documentation-only changes; correctness is
verified by review and by ensuring code-level docs reference the
right symbol names after U1's deletions.

**Verification:**
- `git grep nonFQDNTLDs` and `git grep validHostname` return no hits.
- README's hostname-detector section names PSL and the
  `detectors.hostname.internal_tlds` field.
- CHANGELOG entry exists under "Unreleased".
- REMAINING_RISKS bullet exists for ccTLD collisions.

---

## System-Wide Impact

- **Interaction graph:** Hostname rule's Check is wired in two
  places: `BuiltinRules` (default — used in standalone tests) and
  `runtime.New` (project-config override — used by every CLI command
  and the MCP server through the shared runtime). Both inherit the
  project-trusted `internal_tlds` automatically.
- **Error propagation:** Config parse errors for `detectors.hostname.
  internal_tlds` surface at config load time and bubble up through
  `runtime.New` like existing `exec` validation errors — the user
  sees them on the next `opsmask` invocation, not at runtime.
- **State lifecycle risks:** None — the closure is bound once per
  runtime construction; no shared mutable state. Two runtimes with
  different configs hold independent Checks (asserted in U3 test).
- **API surface parity:** No public Go API changes outside
  `internal/` (the new `detect.HostnameCheckFor` is internal-only).
  The CLI surface gains one new YAML field; the trust-gate
  semantics are identical to the existing `exec` block.
- **MCP surface:** `list_detectors` already returns name/type/policy
  per rule, not internal Check state, so the new collision filter
  and `internal_tlds` are not surfaced through that tool. This is
  acceptable for v1 — the tool documents the active rule set, not
  per-rule configuration. A future enhancement could add a
  `detector_config` view if operators ask for it. `mask` /
  `mask_chunk` flow through `rt.Rules` and pick up the project
  internal_tlds automatically (asserted in U3 MCP test).
- **Binary footprint:** Promoting `golang.org/x/net` from indirect to
  direct adds the embedded PSL data table (~440 KB on disk in
  `golang.org/x/net@v0.53.0/publicsuffix/table.go`). The `net` module
  is already an indirect dependency; the binary growth is bounded
  to the PSL table itself plus the small `publicsuffix` package code.
  Acceptable at this scale.
- **Observability gap:** v1 has no operator-facing way to ask "why
  did the hostname Check accept/reject this input?". Deferred —
  surface a `opsmask explain hostname <input>` subcommand in a
  follow-up if operators need it. Documented as a deferred
  follow-up item.
- **Integration coverage:** `runtime_test.go` (U3) is the cross-layer
  test that proves config → rule wiring; unit tests in
  `detect/hostname_tld_test.go`, `detect/hostname_psl_helper_test.go`,
  and `config/config_test.go` cover the layer boundaries
  individually. MCP coverage in `mcpsrv/tools_text_test.go` (extended
  in U3) closes the cross-binary gap Codex flagged.
- **Unchanged invariants:** The Hostname regex pattern is unchanged
  (R7). The Hostname rule's `Type` is still `hostname`. The Policy
  is still `Pseudonymize`. Other detectors' Check wiring is
  untouched. `LoadFile`'s public signature is unchanged.

---

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| `golang.org/x/net/publicsuffix` private-suffix list shifts may change which inputs are recognized between Go module updates. | Pin via `go.mod`; same risk profile as any embedded-data dep. Documented in REMAINING_RISKS that PSL data refreshes with `go get -u`. |
| Beyond the five-entry compatibility set (`go`/`py`/`rs`/`sh`/`md`), residual ccTLD-collision masking on uncommon code/log suffixes (`.do`/`.in`/`.is`/`.it`/`.me`/etc.). | The five-entry set is **rejected** by `codeLogCcTLDCollisions` in U1 — those labels do NOT mask. The wider residue is accepted: documented in CHANGELOG and REMAINING_RISKS with explicit "rejected vs accepted" framing. Operator workaround for residual cases: project-defined `regex_rules`. The five-entry set is bounded by an explicit charter (U1) — additions require a failing integration fixture, not just prose evidence. |
| Trust-gating mistakes (silently honoring detectors from `--config` or user-wide) widen masking unexpectedly. | U2 tests user-wide warn-and-ignore and parse validation. U3 tests `--config` ignore at the `runtime.New` boundary, mirroring the existing `exec` trust gate. |
| PSL semantics on edge cases (IP literals, trailing dots, IDN) drift between Go module versions. | The Hostname regex prefilter excludes these classes before `Check` runs. The helper short-circuits IP literals and inputs without a dot defensively (asserted in `hostname_psl_helper_test.go`). |
| Closure rebind in `runtime.New` becomes stale if a future refactor caches builtins globally. | Rebind happens before `Env.Rules` is published. `BuiltinRules` always returns a fresh slice. The two-runtime isolation test in U3 guards against shared-state regressions. |
| PSL embedded data (~440 KB) inflates the binary. | Acceptable trade-off at current binary size. The data is immutable and deduplicated by the linker. Alternative (bundled IANA root list) was already rejected in the brainstorm. |

---

## Documentation / Operational Notes

- README gains a short "Configuring internal TLDs" subsection under
  the existing self-host section.
- CHANGELOG entry under "Unreleased" describes the PSL switch, new
  config field, and accepted ccTLD-collision regression.
- REMAINING_RISKS.md adds one bullet covering the ccTLD-collision
  trade-off and a note that PSL data refreshes with `go get -u
  golang.org/x/net`.

---

## Sources & References

- **Origin document:** `docs/brainstorms/2026-05-05-hostname-detector-public-suffix-list-requirements.md`
- Related code:
  - `internal/detect/registry.go` (Check wiring)
  - `internal/detect/rules/builtin.go` (Hostname spec)
  - `internal/config/config.go` (config schema, trust gate)
  - `internal/runtime/runtime.go` (runtime wiring)
- External docs:
  - `https://pkg.go.dev/golang.org/x/net/publicsuffix`
  - `https://publicsuffix.org/list/`
