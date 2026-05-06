---
date: 2026-05-06
topic: detection-corpus
scope: piece-a-only
related: docs/ideation/2026-05-06-internal-readiness-ideation.md
---

# Detection Corpus + CI Regression Gate (Piece A)

## Summary

OpsMask gains an internal detection-regression test corpus under `testdata/corpus/`. Each scenario is an `input.txt` paired with an `expected.txt` golden output. A new test target runs every scenario through the masking engine and fails CI on any diff. Two CLI subcommands (`opsmask corpus add` and `opsmask corpus accept`) keep authoring and golden-updates ergonomic, so observed misses become regression-guarded with one command. Public benchmark (Piece B) and adversarial leaderboard (Piece C) are explicitly out of scope; this is internal CI only.

The corpus closes the loop that produced commits `98d0b84` (K8s YAML cross-line false positive) and `ccc678c` (PSL/FQDN refactor): both fixed real misses but landed without regression-guards beyond unit tests, leaving room for the same shape of bug to recur.

---

## Problem Frame

Today, OpsMask's detection quality is verified by:
- Unit tests in each detector package (synthetic, narrow).
- A manual canary run after detector changes.
- The maintainer's memory of past regressions.

Two recent commits (`98d0b84`, `ccc678c`) fixed regressions that slipped past unit tests precisely because unit tests don't represent real-shaped agent inputs. A `kubectl get pod -o yaml` dump or a multi-document `kubeconfig` is structurally different from the strings that exercise individual detectors. There is no fixture in the repo today that would have caught either bug as a regression test, and there is no convention for how a new "this should have been masked" case becomes a permanent guard.

The corpus is the convention. It does not replace unit tests; it adds a layer above them where every sample is a real-shape representative of an agent input class, and CI fails the moment any detector regresses against any sample.

---

## Actors

- **A1. OpsMask maintainer** — adds samples, accepts golden updates, reviews regression failures.
- **A2. CI** — runs the corpus test on every push; blocks merges on regression.
- **A3. OpsMask binary** — gains `corpus add` and `corpus accept` subcommands; existing engine internals reused unchanged.

---

## Key Flows

- **F1. Adding a new sample from an observed miss.**
  - **Trigger:** Maintainer notices the engine missed (or false-positived) something during dogfooding or after an incident.
  - **Steps:** (1) Save the offending input to a file. (2) Run `opsmask corpus add <file> --scenario <name>`. (3) Tool runs engine, prints engine output, asks "is this correct? [y/n/e]". (4) On `y` → writes `input.txt` and `expected.txt`. On `n` → tool exits without writing; user edits expected by hand and writes both files manually (the case where the current engine is wrong). On `e` → opens `$EDITOR` on the proposed expected so user can correct it before save.
  - **Outcome:** A scenario directory exists; CI now guards against regression on this case.

- **F2. Detector change passes corpus.**
  - **Trigger:** Maintainer modifies a detector and runs `go test ./...`.
  - **Steps:** Corpus test runs every scenario, diffs engine output against `expected.txt` (after token canonicalization, see R6). Zero diffs → pass.
  - **Outcome:** Change merges normally.

- **F3. Detector change breaks a scenario (regression).**
  - **Trigger:** Same as F2, but a scenario's diff is non-empty.
  - **Steps:** (1) Test fails with a unified diff showing what changed. (2) Maintainer reviews. (3) If the change was a regression → fix the detector, re-run. (4) If the change was an intentional improvement (e.g., new detector class, a previously-flagged FP no longer flagged) → run `opsmask corpus accept <scenario>` to refresh `expected.txt`. The accept is committed alongside the detector change so PR reviewers see the golden delta.
  - **Outcome:** Either a regression was caught and fixed, or a deliberate improvement landed with explicit golden-update review.

- **F4. Adding a sample for a *known miss* (TDD shape).**
  - **Trigger:** Maintainer wants to fix a miss test-first.
  - **Steps:** (1) Hand-write `input.txt`. (2) Hand-write `expected.txt` representing what the engine *should* produce. (3) Run corpus test → fails (engine differs from desired expected). (4) Fix the detector. (5) Re-run → passes.
  - **Outcome:** The regression-guard is in place before the fix lands; no risk of the test being shaped to match a buggy engine.

---

## Requirements

**Corpus structure**

- R1. The corpus lives under `testdata/corpus/<scenario-name>/` with `input.txt` and `expected.txt` per scenario. `<scenario-name>` is kebab-case, descriptive (e.g., `k8s-secret-yaml-multidoc`, `kubeconfig-aws-eks`, `journalctl-systemd`).
- R2. Each scenario MAY include a `README.md` describing where the sample came from, what bug or class it guards against, and any sanitization performed. Required for scenarios derived from real (sanitized) data; optional for purely synthetic samples.
- R3. `expected.txt` is the engine output after masking, with token IDs canonicalized per R6. It is committed to the repo and reviewed in PRs.

**Engine integration**

- R4. The masking engine exposes a callable primitive that takes a string and returns the masked string, usable from a test context without spawning a subprocess. (Likely already exists in `internal/exec` or `internal/detect`; planning will confirm and extract a stable function signature if needed.)
- R5. The corpus test (`internal/corpus_test.go` or similar) discovers all `testdata/corpus/<scenario>/` directories at test time, runs the engine on each `input.txt`, canonicalizes tokens, and diffs against `expected.txt`. A non-empty diff fails the test with a readable unified diff in the failure message.

**Token canonicalization**

- R6. Engine output tokens are canonicalized before diff: a concrete token like `<<HOSTNAME_a3f9>>` becomes `<<HOSTNAME_*>>`. Canonicalization is per-class — same canonicalized form for every token of the same class, regardless of order or HMAC output. The expected file is stored in canonicalized form.
- R7. Canonicalization preserves token *count* and *class*. A scenario where `host-a` and `host-b` both get masked produces `<<HOSTNAME_*>>` twice, in their original positions. A regression that masks only one of them fails the diff.

**Authoring and acceptance tooling**

- R8. `opsmask corpus add <file> --scenario <name> [--note "..."]` runs the engine on the input, prints proposed expected (canonicalized), and prompts for confirmation. On confirm, writes `input.txt` and canonicalized `expected.txt` under `testdata/corpus/<name>/`, and writes a `README.md` if `--note` was given.
- R9. `opsmask corpus accept <scenario|--all>` regenerates `expected.txt` from the current engine output for the named scenario (or all scenarios). Refuses if the working tree has uncommitted changes to the scenario directory unless `--force` is passed. The accept produces a diff that the user commits explicitly — the command never auto-commits.
- R10. `opsmask corpus list` prints scenario names, sample size in bytes, and the date `expected.txt` was last accepted (from git, not filesystem).
- R11. The corpus subcommands are excluded from the existing exec-trust gate — they read project files but do not execute commands or modify state outside `testdata/corpus/`.

**CI integration**

- R12. The corpus test runs as part of `go test ./...` — no separate CI job, no separate Make target. A failure blocks merge identically to any other test failure.
- R13. The test reports per-scenario pass/fail with the scenario name in the test name (e.g., `TestCorpus/k8s-secret-yaml-multidoc`) so CI output points directly to the failing scenario.

**Bootstrap content**

- R14. Initial bootstrap includes ≥10 scenarios covering, at minimum: K8s secret YAML (single doc), K8s YAML multi-document (regression for `98d0b84`), kubeconfig with AWS EKS auth, `kubectl get pods -o yaml`, a generic FQDN sample (regression for `ccc678c`), an IPv4 + IPv6 sample, an OpenStack UUID sample, a `.env` file, `journalctl` output, and an SSH command output. Bootstrap is a separate PR after the engine integration lands.

---

## Acceptance Examples

- **AE1. Covers R1, R3, R5, R6.** Given the corpus contains `testdata/corpus/k8s-secret-yaml-multidoc/` with valid `input.txt` and `expected.txt`, when `go test ./...` runs against the current engine, the corpus test passes with zero diff.
- **AE2. Covers R5, R7.** Given a detector change accidentally drops detection of one hostname in a multi-hostname sample, when CI runs, the corpus test fails with a diff showing one `<<HOSTNAME_*>>` token replaced by the original hostname; the failure message names the scenario.
- **AE3. Covers R8.** Given an empty `testdata/corpus/`, when the maintainer runs `opsmask corpus add ./fixtures/leaked-output.txt --scenario kubectl-get-pods --note "from incident X, sanitized"`, the tool prints proposed expected, the user types `y`, and the tool writes `testdata/corpus/kubectl-get-pods/{input.txt, expected.txt, README.md}` with the note in the README.
- **AE4. Covers R9.** Given a deliberate detector improvement (a new detector class catching a previously-missed pattern), when the maintainer runs `opsmask corpus accept --all` and inspects `git diff testdata/corpus/`, the diff shows the previously-unmasked spans now wrapped in tokens; the maintainer commits the accept alongside the detector change.
- **AE5. Covers R6.** Given a scenario whose `expected.txt` contains `<<HOSTNAME_*>>` and the engine produces `<<HOSTNAME_a3f9>>` (or `<<HOSTNAME_7b21>>` on a different machine), the diff is empty after canonicalization; the test passes regardless of HMAC seed.
- **AE6. Covers F4.** Given a known miss (`opsmask corpus add` would write the *current buggy* expected, which is wrong), when the maintainer hand-writes `input.txt` and `expected.txt` (with the desired correct masking) and runs the corpus test, the test fails — confirming the regression-guard is in place. After the detector fix lands, the same test passes.

---

## Success Criteria

- A future regression of the same class as `98d0b84` or `ccc678c` is caught by `go test ./...` before merge, not by manual canary or user incident.
- Adding a new sample from an observed miss takes ≤2 minutes (one `corpus add` invocation + edit if needed + commit).
- Bootstrap corpus (R14) lands within the same PR sequence as the tooling, so the CI gate is meaningful from day one (not "added but empty").
- `expected.txt` files are reviewable diffs in PRs — a reviewer can see what changed in detection behavior without running the engine themselves.
- The corpus contributes to detection-quality evidence the project can cite later (when Piece B / public benchmark becomes warranted), without requiring rework.

---

## Scope Boundaries

- **Piece B (public benchmark `opsmask-bench`).** Out of scope; revisit when external visibility, contributor traffic, or comparative recall claims against Presidio/LLM Guard/etc. become motivating.
- **Piece C (adversarial leaderboard).** Out of scope.
- **Performance benchmarks.** The corpus checks correctness, not speed. Latency/throughput regression tests are separate work.
- **Synthetic data generation pipeline.** Bootstrap and ongoing sample addition are manual or semi-manual via `corpus add`. A generator that produces N synthetic kubeconfigs from templates is plausible v2 work but not v1.
- **Image / binary content.** Corpus samples are text only. Read coverage's image-passthrough behavior (per the Read hook brainstorm) is tested separately, not via this corpus.
- **Cross-tool integration tests.** The corpus exercises the masking engine directly. Hook-level integration tests (Bash hook, Read hook end-to-end) are separate and may share fixtures with the corpus opportunistically.
- **Mapping-store assertions.** The corpus tests masked output text only. Assertions about mapping-store contents (which tokens map to which values, persistence semantics) are separate test scope.

---

## Key Decisions

- **Pair format (`input.txt` + `expected.txt`) over span format (`input.txt` + JSON span list).** Pair format is human-readable, diff-friendly in PRs, and matches how the maintainer thinks about masking ("show me what the agent sees"). Span format is more rigorous (separates recall from FP, gives precise positions) but harder to author and review by hand. Revisit if the corpus grows past ~50 samples and pair-format diffs become noisy.
- **Token canonicalization at diff time, not at engine time.** The engine still produces concrete tokens (HMAC output) so token stability across Bash/Read holds for production use. Canonicalization is a test-only transformation (`<<HOSTNAME_[a-f0-9]+>>` → `<<HOSTNAME_*>>`). This keeps the engine unchanged and the test independent of HMAC seed and run order.
- **Two-step accept (run, review diff, commit) instead of auto-update.** A flag like `go test -update-golden` is convenient but turns golden review into a rubber-stamp. `corpus accept` produces a diff the user inspects and commits explicitly — the same review discipline as code changes.
- **Bootstrap is a separate PR.** The tooling can land first as infrastructure; the bootstrap content lands second so reviewers can scrutinize each scenario individually without drowning the tooling PR.
- **Corpus tooling is in `cmd/opsmask` not a separate `cmd/opsmask-corpus`.** Maintainer workflow: one binary, one set of subcommands. The corpus operations are read-only against the project, so they don't conflict with the exec trust gate.
- **No `corpus remove`.** Removing a scenario is `git rm -r testdata/corpus/<name>`. Adding a `corpus remove` subcommand has no ergonomic benefit and creates a slightly-confusing audit story (a scenario removed via the tool vs. via git).

---

## Dependencies / Assumptions

- **D1.** The masking engine has (or can expose with minimal refactor) a string-in / string-out function suitable for test invocation. Verified informally against the existence of `internal/exec` and `internal/detect`; planning will confirm the signature.
- **D2.** Token format is stable enough that `<<HOSTNAME_[a-f0-9]+>>` is a safe canonicalization regex. If the token format ever changes (e.g., to include a position counter), the canonicalization regex updates with it.
- **D3.** `go test` discovery via `filepath.Walk` over `testdata/corpus/` runs in a fraction of a second for bootstrap size; performance is not a concern at <100 scenarios. Revisit if the corpus crosses ~500 samples.
- **D4. Residual risk: golden refresh by accident.** A maintainer running `opsmask corpus accept --all` before reviewing the diff can land an incorrect engine change as the new ground truth. Mitigation is process: PR review of the `expected.txt` diff. Tooling could enforce no-uncommitted-changes (R9 already does for the targeted-scenario case) but `--all` is by design a power tool.
- **D5.** Real-data sanitization for samples derived from production output is the maintainer's responsibility — `corpus add` does not auto-sanitize. README per R2 documents what sanitization was done, so reviewers can verify.

---

## Outstanding Questions

### Resolve Before Planning

- (none)

### Deferred to Planning

- [Affects R4] The exact public function signature exposed by the engine for test consumption. The product decision is "tests can call it directly without subprocess"; the signature is for `ce-plan`.
- [Affects R6] Concrete canonicalization regex(es) for the current token shapes. Planning will enumerate token classes and produce the regex set.
- [Affects R8, R9] Exact CLI flag shapes (`--scenario` vs positional, `--all` vs explicit list). Pure UX choice for planning.
- [Affects R13] Whether the corpus test uses Go's `testing.T.Run` per scenario (giving per-scenario `-run` filtering) or a single test with subtest-like reporting. Planning detail.
- [Affects R14] Final bootstrap list of scenarios. The list in R14 is illustrative and minimum; the maintainer's `kubectl`/`ssh` history is the source for real-shape additions.

---

## Notes on what this brainstorm deliberately did not commit to

- A separate corpus repo, mirror, or release artifact. Everything lives in the main repo's `testdata/`.
- Coverage targets ("≥95% recall on corpus"). Recall is a property of the corpus + engine; setting a numeric target before bootstrap exists is premature.
- Integration with the future pre-send diff (#5). When #5 ships, observed-edit events become candidate `corpus add` inputs, but that integration is a future stitch, not v1 scope.
- Multi-language detection or non-English content. Bootstrap is English/ASCII-shaped infra output.
