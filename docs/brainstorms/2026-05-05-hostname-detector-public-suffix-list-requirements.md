---
date: 2026-05-05
topic: hostname-detector-public-suffix-list
---

# Hostname Detector: Public Suffix List Precision

## Summary

Replace the Hostname rule's curated bad-TLD denylist with a Public Suffix
List check (via `golang.org/x/net/publicsuffix`), so the rule structurally
distinguishes registered network suffixes from arbitrary dotted code. A
small RFC-reserved internal-TLD set covers operational suffixes
(`.local`, `.internal`, `.arpa`, etc.); a project config field lets
self-hosters add organization-specific internal TLDs.

---

## Problem Frame

LLM coding agents see masked output that pollutes their context window
with sentinels for things that are not network identifiers. The current
hostname rule fires on three classes of dotted lowercase 3+ label
identifiers it cannot structurally distinguish from FQDNs: Python module
logger names in OpenStack/Kolla logs (`nova.api.openstack.wsgi`,
`keystone.server.flask`, `neutron.plugins.ml2`), software class reprs
(`<nova.api.openstack.compute.versions.Versions object>`), and dot-
separated log filenames in `ls` output (`haproxy_latest.api.log`,
`openvswitch.ovs-vswitchd.log`).

Three iterative fixes have been applied today: a regex tightening
(lowercase + 3+ labels), a small bad-TLD denylist for file extensions,
and an extension covering Python module-name conventions. Each round
addressed the false positives in the most recent test report and produced
a fresh round in the next ecosystem. The denylist is now ~80 entries and
trending toward whack-a-mole. New ecosystems (Java packages, Rust crate
paths, Erlang modules, custom internal naming conventions) will produce
new false positives that need new entries.

The carrying cost is now visible: every test report risks adding code
churn to a detection rule that should be stable. The cost falls on
maintainers who must judge each requested addition, on operators who
discover false positives in unfamiliar ecosystems, and on agents who
work with degraded log readability until the list catches up.

---

## Requirements

**Detection precision**
- R1. Hostname matches must be backed by a structural test that does not
  require per-ecosystem maintenance to stay precise. The test must
  recognize a candidate as a hostname only when its suffix is a
  registered public suffix or a recognized internal/private suffix.
- R2. The Public Suffix List (PSL) is the source of truth for registered
  public suffixes. Both ICANN-managed and privately-managed suffixes
  count (so cloud-provider private TLDs like `s3.amazonaws.com` work
  without special-casing).
- R3. Internal/private TLDs not in PSL are recognized via a fixed
  default set covering RFC-reserved names: `local`, `internal`, `lan`,
  `home`, `localhost`, `arpa`, `corp`, `intranet`, `test`. Real
  operational hostnames (`worker-1.cluster.local`,
  `db-1.us-east-2.compute.internal`) keep masking out of the box.

**Configurability**
- R4. Project config (`.opsmask/config.yaml`) accepts an additional list
  of internal TLDs that extends the default RFC-reserved set. Any TLD
  named in this list is treated as a recognized internal suffix when
  the trusted-project gate is satisfied (same gate as `exec` config —
  user-wide config and `--config` overrides cannot enable detection of
  arbitrary additional TLDs).
- R5. The config field is additive only. There is no way to *exclude*
  TLDs (e.g., to forcibly mask `.com`); inclusion-only matches the
  hostname rule's masking semantics and keeps the surface small.

**Migration**
- R6. The current `validHostname` Check (bad-TLD denylist:
  `nonFQDNTLDs` map plus the function) is removed entirely. PSL +
  internal-TLD allowlist subsumes it; carrying both creates two
  sources of truth.
- R7. The existing Hostname regex (lowercase, 3+ labels, max-24-char
  TLD position) is retained as a fast pre-filter. PSL evaluation runs
  in the rule's `Check` slot so unrelated bytes never reach the PSL
  library.

---

## Acceptance Examples

- AE1. **Covers R1, R2.** Given input `nova.api.openstack.wsgi`, when
  the engine processes it, the candidate is rejected (no mask token)
  because PSL does not recognize `wsgi` as a public suffix and `wsgi`
  is not in the internal-TLD set.
- AE2. **Covers R1, R2.** Given input `api.example.com`, when the
  engine processes it, the candidate is masked because PSL recognizes
  `com` as an ICANN-managed public suffix.
- AE3. **Covers R3.** Given input `worker-1.cluster.local`, when the
  engine processes it (no project config customizing internal TLDs),
  the candidate is masked because `local` is in the default
  RFC-reserved internal set.
- AE4. **Covers R4.** Given a trusted project config containing
  `detectors: { hostname: { internal_tlds: [acme] } }` and input
  `db-1.acme`, when the engine processes it, the candidate is masked
  because `acme` is in the project's extended internal set.
- AE5. **Covers R4.** Given the same input `db-1.acme` but no
  trusted project config (or the config is supplied via `--config`
  rather than the project's anchored path), when the engine processes
  it, the candidate is NOT masked because the trust gate prevents
  arbitrary configs from enabling masking expansions.
- AE6. **Covers R1.** Given input
  `<nova.api.openstack.compute.versions.Versions object at 0x7f12>`,
  when the engine processes it, the longest greedy regex match
  (`nova.api.openstack.compute.versions`) is evaluated against PSL +
  internal allowlist; `versions` is not a registered suffix and the
  candidate is rejected.

---

## Success Criteria

- The OpenStack/Kolla integration test report's "Remaining (medium
  severity)" section becomes empty: Python logger names, class reprs,
  and dot-separated log filenames all pass through unmasked.
- Real operational hostname coverage in the existing test suite
  (`api.example.com`, `node-04.cluster.local`,
  `db-1.us-east-2.compute.internal`) is unchanged.
- Adding a new ecosystem that uses dotted lowercase namespaces (Java
  packages, Erlang modules, etc.) requires zero code changes — those
  identifiers are already rejected by PSL absence.
- The detect package's `nonFQDNTLDs` map and `validHostname` function
  are deleted; no `// TODO: also denylist X` comments remain.
- A self-hoster running OpsMask in a cluster using a non-RFC internal
  TLD (e.g., `.acme`) can opt that TLD into masking via project config
  without code changes.

---

## Scope Boundaries

- Context-anchored hostname detection (URLs, emails, `host=foo`
  key-value pairs) is not added. URL credential masking is already
  covered by the PasswordURL rule and email by the Email rule; the
  user explicitly opted to keep a generic hostname rule rather than
  drop it in favor of context-only matching.
- DNS resolution / network-side hostname validation is out of scope
  (would introduce side effects, latency, and a dependency on network
  reachability during masking).
- Auto-refresh tooling for PSL beyond what `go get -u
  golang.org/x/net` already provides is not in scope.
- The two unselected approaches — bundled IANA root-zone TLD list,
  structural inversion (4+ pure-alpha lowercase labels) — are
  rejected. PSL's private-suffix awareness gives strictly more
  precision than IANA-only; structural inversion folded under
  inspection because it doesn't replace the residual 3-label
  denylist need.
- The existing Hostname regex (lowercase, 3+ labels, max-24-char TLD)
  is not changed. PSL is added in the Check slot only.
- Configuration to *exclude* TLDs from the public/internal allowlist
  (forcing masking of e.g. `.com`) is not added. Inclusion-only
  matches the rule's structural intent and keeps the config surface
  small.

---

## Key Decisions

- **Public Suffix List over IANA-only or pure heuristic.** PSL covers
  both ICANN public suffixes (gTLDs, ccTLDs) and privately-managed
  suffixes that operators actually use (`s3.amazonaws.com`,
  `appspot.com`, `cloudfront.net`). IANA-only would still mask
  `bucket.s3.amazonaws.com` correctly via the `com` TLD but loses
  precision on pure-PSL identifiers; pure heuristics fold on the
  3-label tail.
- **`golang.org/x/net` dep is acceptable.** The repo already depends
  on `golang.org/x/term`, `golang.org/x/oauth2`, `golang.org/x/sys`,
  and `golang.org/x/exp`; adding `golang.org/x/net` is consistent
  with the existing footprint.
- **Configurable internal TLDs over fixed allowlist or unrestricted
  fallback.** Self-hosters with custom internal TLDs can opt them in
  per-project; default behavior (RFC-reserved set) is sane out of the
  box; no fallback to "mask anything that looks lowercase" because
  that re-introduces the false-positive class.
- **Trust-gated config.** The internal-TLD config field is anchored
  to the project's `.opsmask/config.yaml` (same trust gate as
  `exec.allow`), so user-wide config and `--config` overrides cannot
  silently widen what gets masked. Consistent with existing config
  precedent.

---

## Dependencies / Assumptions

- New module dependency: `golang.org/x/net/publicsuffix`. Stable Go
  team-maintained library; the same Mozilla PSL data ships across
  Go's networking stack.
- Assumption: `publicsuffix.PublicSuffix(domain)` returns `(suffix,
  icann)` and an empty / "" suffix only for inputs that are not
  recognized at all. Verified against the package docs at
  pkg.go.dev/golang.org/x/net/publicsuffix; the Check function reads
  both return values to keep ICANN-managed and privately-managed
  suffixes (both count as real network suffixes for our purposes).
- Assumption: PSL does not include the RFC-reserved internal suffixes
  (`local`, `internal`, etc.) — confirmed via the package's published
  data; that's why the small RFC default set is needed alongside.

---

## Outstanding Questions

### Deferred to Planning

- [Affects R4][Technical] Exact YAML config schema location:
  `detectors.hostname.internal_tlds: []` vs. a flatter shape like
  `hostname.internal_tlds: []`. Should mirror existing config
  conventions in `internal/config`.
- [Affects R7][Technical] Whether the PSL Check should normalize the
  candidate (lowercase already guaranteed by regex) or strip
  trailing dots. The library expects bare hostnames; the regex
  output should already be safe but worth confirming against the
  engine's chunking behavior.
