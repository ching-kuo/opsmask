---
title: "feat: Detection Corpus + CI Regression Gate (Piece A)"
type: feat
status: completed
date: 2026-05-06
origin: docs/brainstorms/2026-05-06-detection-corpus-requirements.md
---

# feat: Detection Corpus + CI Regression Gate (Piece A)

## Summary

Add an internal detection-regression test corpus under `testdata/corpus/` driven by a new `internal/corpus` test harness and three new `opsmask corpus` CLI subcommands (`add`, `accept`, `list`). The harness walks each `<scenario>/input.txt`, masks it through the existing `engine.Process` pipeline, canonicalizes tokens (`[[opsmask:hostname:abc123]]` → `[[opsmask:hostname:*]]`), and diffs against the committed `expected.txt` golden. The corpus runs as part of `go test ./...` — no separate CI job. Bootstrap content (≥10 scenarios) lands in a second PR so reviewers can scrutinize each scenario individually; this plan delivers the tooling.

---

## Problem Frame

Recent commits `98d0b84` (K8s YAML cross-line false positive) and `ccc678c` (PSL/FQDN refactor) fixed real regressions, but neither shipped with a real-shape regression guard — only narrower unit tests in their respective detector packages. The next regression of the same class can recur. Unit tests don't catch this because they exercise individual detectors against synthetic strings, not detector-set behavior on representative agent inputs (`kubectl get pod -o yaml`, `kubeconfig`, `journalctl` dumps).

---

## Requirements

- R1. The corpus lives under `testdata/corpus/<scenario>/` with `input.txt` + `expected.txt` (+ optional `README.md`). Carried from origin R1-R3.
- R2. The masking engine exposes a callable primitive usable from a test context without a subprocess. Carried from origin R4.
- R3. A new test target discovers all scenarios, runs the engine, canonicalizes tokens, and diffs against `expected.txt`. Carried from origin R5-R7.
- R4. Token canonicalization is test-only (engine output unchanged in production). Carried from origin Key Decisions.
- R5. `opsmask corpus add <file> --scenario <name> [--note "..."]` runs the engine on input, prints proposed canonicalized expected, prompts y/n/e, and writes the scenario directory on confirm. Carried from origin R8.
- R6. `opsmask corpus accept <scenario|--all>` regenerates `expected.txt` and never auto-commits; refuses on an uncommitted target unless `--force`. Carried from origin R9.
- R7. `opsmask corpus list` prints scenario names with metadata. Carried from origin R10.
- R8. The corpus subcommands do not invoke `internal/exec/orchestrate` (the trust-gated arbitrary-command executor) and write no persistent state outside `testdata/corpus/`. Fixed-purpose subprocesses (`git status --porcelain`, `git log -1`, `$EDITOR`) are explicitly allowed for the documented UX. Carried from origin R11.
- R9. The corpus test runs as part of `go test ./...` with per-scenario sub-test names. Carried from origin R12-R13.
- R10. Bootstrap corpus (≥10 scenarios covering K8s YAML multidoc, kubeconfig, FQDN, IPv4+IPv6, OpenStack UUID, `.env`, journalctl, SSH output, and the two recent regression cases). Carried from origin R14.

**Origin actors:** A1 (maintainer), A2 (CI), A3 (OpsMask binary)
**Origin flows:** F1 (add new sample), F2 (detector change passes), F3 (detector change breaks), F4 (TDD shape — add sample for known miss)
**Origin acceptance examples:** AE1-AE6 carried; mapped to U-IDs in test scenarios below.

---

## Scope Boundaries

- Public benchmark / `opsmask-bench` (Piece B).
- Adversarial leaderboard (Piece C).
- Performance benchmarks against the corpus.
- Synthetic data generation pipeline.
- Image / binary content samples.
- Hook-level integration tests (Bash hook, future Read hook end-to-end).
- Mapping-store assertions on corpus runs (text output only).

### Deferred to Follow-Up Work

- **Bootstrap content (R10 / U5).** Separate PR after the tooling lands. Per origin Key Decisions ("Bootstrap is a separate PR"), this lets reviewers scrutinize each scenario's sanitization individually without drowning the tooling PR.

---

## Context & Research

### Relevant Code and Patterns

- **Engine entry:** `internal/engine/engine.go` — `Process(ctx, reader, writer, rules, allocator, opts) (Stats, error)`. Stream-based but composable with `bytes.Buffer`.
- **Existing test invocation pattern:** `internal/engine/engine_test.go:15` (`TestProcessMasksIdentifiersAndDestroysSecrets`) — sets up `store.OpenSQLite(t.TempDir())`, calls `detect.BuiltinRules()`, instantiates `pseudo.New(secret, st)`, runs `Process` against a `bytes.Buffer`. This is the closest existing pattern for the corpus harness.
- **Token format:** `[[opsmask:<class>:<id>]]` (e.g., `[[opsmask:ip4:abc123]]`, `[[opsmask:email:def456]]`); destroyed form `[REDACTED_<KIND>]` (e.g., `[REDACTED_AWS_KEY]`).
- **Runtime bootstrap:** `internal/cli/helpers.go:12` — `buildRuntime(opts) (*runtimeEnv, error)` returns `Rules`, `Alloc`, `Loaded.DenyList`, `Close()`.
- **CLI subcommand pattern:** `internal/cli/mask.go` (one cobra command per file, registered via `internal/cli/root.go`). New `corpus` parent command with `add`/`accept`/`list` children mirrors this.
- **Existing testdata layout:** `testdata/{config, logs, reports, store}/` — none currently used for engine regression. New `testdata/corpus/` is greenfield in this directory.

### Institutional Learnings

- `docs/solutions/` does not exist. No prior learnings to apply.

### External References

- None used. Local patterns are sufficient.

---

## Key Technical Decisions

- **Reuse `engine.Process` as-is; do not add a new `MaskString` wrapper.** `Process` already works against `bytes.Buffer` (see `engine_test.go:15`); a string-in/string-out wrapper would duplicate the streaming-aware boundary handling for marginal ergonomic benefit. The corpus harness can compose `Process` directly with `strings.NewReader` and `bytes.Buffer`.
- **Canonicalization is a regex on the engine output, not a flag on the engine.** Preserves R4 (engine unchanged in production). Regex captures token class (`[[opsmask:hostname:...]]`) and replaces ID with `*`, preserving count and ordering. Destroyed form `[REDACTED_<KIND>]` is already deterministic and needs no canonicalization.
- **Per-scenario sub-tests via `t.Run`.** Gives `go test -run 'TestCorpus/k8s-secret-yaml-multidoc'` filtering and per-scenario CI failure attribution (origin R13).
- **Two-step accept (`corpus accept` writes file; user reviews diff and commits explicitly).** Origin Key Decisions chose this over `-update-golden` flag style. The plan honors that: `accept` writes only; the user runs `git diff testdata/corpus/` and commits manually.
- **`internal/corpus/` as a new package** (not under `internal/engine/`). Keeps corpus tooling logically separate from the engine — corpus is test infrastructure, not engine internals. Matches the convention of single-responsibility packages already in `internal/`.
- **CLI registers `corpus` as a parent command with `add`/`accept`/`list` children** (cobra subcommand pattern), one file: `internal/cli/corpus.go`. Mirrors the size and shape of existing single-file subcommands like `mask.go`, `unmask.go`.
- **Corpus-specific runtime composition, not `cli.buildRuntime`.** Per origin R11 (no persistent state outside `testdata/corpus/`), `buildRuntime` is unsuitable — it opens the project's persistent SQLite mapping store and writes pseudonym entries during `engine.Process`. Instead, `internal/corpus/runner.go` exposes `RunMask(ctx, input []byte) ([]byte, error)` which internally: creates an ephemeral directory via `tmpDir, err := os.MkdirTemp("", "opsmask-corpus-*")`, opens the store via `st, err := store.OpenSQLite(filepath.Join(tmpDir, "mapping.sqlite"))`, defers cleanup as `defer func() { st.Close(); os.RemoveAll(tmpDir) }()` (close-before-remove order matters on Windows/macOS where open handles block directory removal), instantiates `pseudo.New(fixedTestSecret, st)`, calls `detect.BuiltinRules()`, and runs `engine.Process` against a `bytes.Buffer`. Both `TestCorpus` and `corpus add`/`accept` route through `RunMask` — no divergence between test and CLI codepaths.
- **Builtin rules only in v1.** The corpus runner uses `detect.BuiltinRules()` exclusively. Project-level config (`.opsmask/config.yaml`) and custom detectors are not loaded. This is an explicit boundary so future config-aware corpus modes are an additive change. Documented so downstream changes cannot silently widen the test surface.
- **Canonicalization regex sourced from `detect.TokenRegexp()`.** The engine emits two token forms (`⟪opsmask:…⟫` Unicode default, `[[opsmask:…]]` ASCII) plus an internal inert-escape form; `TokenRegexp()` is the single source of truth for token shape. The canonicalizer reuses it (preserving class capture, replacing the ID capture with `*`) so future token-shape changes flow through automatically.
- **Corpus root resolution via `go.mod`-walk helper.** A shared `CorpusRoot()` in `internal/corpus` walks up from the current working directory until it finds `go.mod`, then returns `<repo>/testdata/corpus`. Used by both the test harness (cwd is the test package directory) and the CLI (cwd is wherever the user invoked `opsmask corpus …`). Hardcoded `../../testdata/corpus` is rejected — fragile across run contexts.
- **Path-escape defense via `filepath.Rel` containment check.** Scenario name validator enforces `^[a-z0-9][a-z0-9-]+[a-z0-9]$` (kebab-case, length ≥3 — note `+` not `*` so the inner character class must consume at least one character). After joining with the corpus root and `filepath.Clean`, a `filepath.Rel(corpusRoot, scenarioPath)` check confirms the relative path doesn't begin with `..` or escape the root. String-prefix checks are explicitly rejected as subtly fragile (symlinks, trailing slashes).
- **Atomic file writes via temp-file + close + rename.** Both `corpus add` and `corpus accept` write `expected.txt` (and `input.txt` for `add`) via `os.CreateTemp(scenarioDir, ".tmp-*")`, write, `Close`, then `os.Rename` to the final name. Standard Go atomic-write idiom; an interrupted run leaves no partial file.
- **`accept --all` is two-phase.** Phase 1 enumerates targets, validates, and runs the dirty-tree check on every target; any phase-1 failure aborts before any write. Phase 2 writes. No partial mutation of the corpus tree.
- **Unified-diff failure messages via in-package helper.** A small (~40 LOC) hand-rolled hunked-diff helper in `internal/corpus/diff.go` produces standard `--- expected / +++ got` output with `+`/`-`/` ` line markers and surrounding context. No new dependency. Helper is unit-tested with in-memory inputs so the failure-path is exercised without corrupting any committed golden.
- **R8 framing clarified.** Origin R11's "no command execution" means corpus commands do not invoke `internal/exec/orchestrate` (the trust-gated arbitrary-command executor). Fixed-purpose subprocesses for `git status --porcelain`, `git log -1`, and `$EDITOR` are explicitly allowed: scoped, non-user-controlled, and necessary for the documented UX. The exec trust gate is unaffected.

---

## Open Questions

### Resolved During Planning

- **Engine API change needed?** No. `engine.Process` signature is verified suitable as-is — accepts `io.Reader`/`io.Writer`, takes `[]detect.Rule` + `*pseudo.Allocator`, returns `Stats`. Composes cleanly with `bytes.Buffer`. The runner and tests will enforce the contract going forward; no separate doc-only unit needed.
- **Where does `internal/corpus` live and what does it export?** New package under `internal/`. Exports: `RunMask(ctx context.Context, input []byte) ([]byte, error)` (encapsulates ephemeral store + builtin rules + fixed secret + `engine.Process`), `Canonicalize(masked []byte) []byte`, `Discover(root string) ([]Scenario, error)`, `CorpusRoot() (string, error)`, `ValidateScenarioName(name string) error`, plus the unified-diff helper. Test file lives alongside in `internal/corpus/corpus_test.go`.
- **How does the corpus runner load rules + allocator without persisting state?** `RunMask` creates an ephemeral SQLite store with `os.MkdirTemp("", "opsmask-corpus-*")` + `store.OpenSQLite`, defers `os.RemoveAll`, calls `detect.BuiltinRules()`, instantiates `pseudo.New(fixedTestSecret, st)`. The fixed secret is irrelevant for goldens because canonicalization wildcards IDs. No project state is touched.

### Deferred to Implementation

- The canonicalizer regex is sourced from `detect.TokenRegexp()`; the implementer wires the capture-group rewrite (preserve class, replace ID with `*`). Class enumeration for U2's coverage tests reads from `detect.BuiltinRules()` (or directly from `internal/detect/rules/builtin.go`), not `registry.go` — `registry.go` does not enumerate concrete classes.
- `corpus accept --all`'s uncommitted check shells `git status --porcelain testdata/corpus/<scenario>/`. Pure-Go go-git is rejected as heavyweight; `git` is a dev-environment assumption.
- Behavior when `expected.txt` exists but `input.txt` is missing (corrupt scenario): hard-fail with a clear message naming the scenario.
- `corpus add` `e` (edit) flow uses `$EDITOR` on a temp file; on editor exit the tool re-reads the temp file as the proposed expected and re-prompts.

---

## High-Level Technical Design

> *Directional guidance for review, not implementation specification.*

```
testdata/corpus/                    # bootstrap content lives here (separate PR)
└── <scenario-name>/
    ├── input.txt                   # raw input (the masking engine reads this)
    ├── expected.txt                # canonicalized engine output (golden)
    └── README.md                   # optional: source, sanitization, what regression it guards

internal/corpus/                    # new package
├── canonicalize.go                 # Canonicalize via detect.TokenRegexp() — covers ⟪…⟫ and [[…]]
├── runner.go                       # RunMask: ephemeral store, builtin rules, fixed secret
├── root.go                         # CorpusRoot via go.mod-walk from cwd
├── validate.go                     # ValidateScenarioName + filepath.Rel containment
├── diff.go                         # UnifiedDiff hand-rolled hunked diff (~40 LOC)
├── discover.go                     # Discover + Scenario struct
├── compare.go                      # Compare(expected, got) → diff string (pure)
└── corpus_test.go                  # TestCorpus driver

internal/cli/
└── corpus.go                       # cobra: opsmask corpus { add | accept | list }
                                    # routes through corpus.RunMask, never cli.buildRuntime
```

**Test flow per scenario:**

1. `CorpusRoot()` walks up from cwd to find `go.mod`, returns `<root>/testdata/corpus`.
2. `Discover(root)` returns `[]Scenario{Name, InputPath, ExpectedPath}`.
3. For each scenario: `t.Run(scenario.Name, …)`.
4. Inside sub-test: read `input.txt`; `RunMask(ctx, input)` (ephemeral store + builtin rules + fixed secret); `Canonicalize(out)`; `Compare(expected, canonicalized)`; non-empty diff → `t.Fatalf` with the unified diff.

**`corpus add` flow:** parse args → `ValidateScenarioName` → `CorpusRoot` → refuse if scenario dir exists → `RunMask` on input → `Canonicalize` → print proposed expected → prompt y/n/e → on y, atomic-write (`CreateTemp` + `Close` + `Rename`) `<scenario>/input.txt` and `<scenario>/expected.txt` (and `README.md` if `--note`).

**`corpus accept` flow (two-phase):**
- *Phase 1 (preflight):* enumerate targets (one named or all via `Discover`); validate each has `input.txt`; run `git status --porcelain` per scenario; collect dirty scenarios; if any dirty and not `--force`, abort with the list — no writes occur.
- *Phase 2 (writes):* `RunMask` → `Canonicalize` → atomic-write `expected.txt` per target. Never invokes git for committing.

---

## Implementation Units

*U1 was removed during plan review (Codex consensus): a standalone doc-only unit added no value over the runtime contract enforced by U3's `RunMask` and tests. Verification of `engine.Process` suitability is recorded in Open Questions / Resolved During Planning.*

---

- U2. **Implement token canonicalizer + corpus runner + path utilities**

**Goal:** Pure function that takes masked engine output and returns a form where token IDs are replaced with `*` while preserving token class, count, and position. Destroyed-secret tokens (`[REDACTED_<KIND>]`) pass through unchanged (already deterministic).

**Goal:** Foundation primitives consumed by U3 and U4: token canonicalizer (covers both `⟪opsmask:…⟫` Unicode and `[[opsmask:…]]` ASCII forms via `detect.TokenRegexp()`); ephemeral-store `RunMask` runner; `CorpusRoot()` helper that walks up to find `go.mod`; `ValidateScenarioName` with kebab-case rule + `filepath.Rel` containment check; unified-diff helper.

**Requirements:** R2, R3, R4, R8

**Dependencies:** None

**Files:**
- Create: `internal/corpus/canonicalize.go` + `internal/corpus/canonicalize_test.go`
- Create: `internal/corpus/runner.go` + `internal/corpus/runner_test.go`
- Create: `internal/corpus/root.go` + `internal/corpus/root_test.go`
- Create: `internal/corpus/validate.go` + `internal/corpus/validate_test.go`
- Create: `internal/corpus/diff.go` + `internal/corpus/diff_test.go`

**Approach:**
- **Canonicalizer:** `Canonicalize(masked []byte) []byte` reuses `detect.TokenRegexp()` (which already covers both Unicode and ASCII token forms). Replacement preserves the class capture group and rewrites the ID capture group to `*`, preserving bracket/delimiter form. Destroyed form `[REDACTED_<KIND>]` is deterministic and passes through; the engine's internal inert-escape form is not produced in normal Process output (engine-internal only) and does not need canonicalization for v1.
- **`RunMask` runner:** creates ephemeral store via `os.MkdirTemp("", "opsmask-corpus-*")` + `store.OpenSQLite`, defers `os.RemoveAll`; calls `detect.BuiltinRules()`; instantiates `pseudo.New(fixedTestSecret, st)`; runs `engine.Process(ctx, bytes.NewReader(input), &out, rules, alloc, engine.Options{})` and returns `out.Bytes()`. Single composition shared by harness and CLI.
- **`CorpusRoot()`:** walks up from `os.Getwd()` looking for `go.mod`; on find, returns `<root>/testdata/corpus`. Errors if not found within reasonable depth (e.g., 8 levels) so accidental invocation outside the repo fails fast with a readable error.
- **`ValidateScenarioName(name string)`:** regex `^[a-z0-9][a-z0-9-]+[a-z0-9]$` (note `+` not `*` — enforces length ≥3 since the inner class must match at least one character; the leading and trailing anchors contribute the other two). Reject names containing `..` or path separators. After joining with the corpus root and `filepath.Clean`, `filepath.Rel(corpusRoot, scenarioPath)` must succeed and the result must not begin with `..` or be absolute.
- **Diff helper:** `UnifiedDiff(expected, got []byte) string` — small hand-rolled hunked-diff producing standard `--- expected / +++ got` headers and `+`/`-`/` ` line markers with three lines of surrounding context. ~40 LOC. No external dependency.

**Execution note:** Test-first. Each helper is small and pure; TDD surfaces edge-case decisions (multi-line, adjacent tokens, kebab-edge inputs, identical inputs producing empty diff) cheaply.

**Patterns to follow:**
- Table-driven test layout in `internal/detect/trailing_delimiter_test.go`.
- Engine setup pattern in `internal/engine/engine_test.go:15` (for `RunMask` internals).

**Test scenarios:**

*Canonicalizer:*
- *Happy path:* `⟪opsmask:hostname:0123456789abcdef⟫` → `⟪opsmask:hostname:*⟫` (Unicode form, default engine output)
- *Happy path:* `[[opsmask:hostname:0123456789abcdef]]` → `[[opsmask:hostname:*]]` (ASCII form, used when `Options{ASCIITokens:true}`)
- *Happy path:* Two distinct hostnames in the same input both canonicalize, preserving positions
- *Happy path:* Mixed Unicode + ASCII tokens in same buffer both canonicalize independently
- *Edge case:* Adjacent tokens with no whitespace canonicalize independently
- *Edge case:* Multi-line input preserves line structure
- *Edge case:* Destroyed form `[REDACTED_AWS_KEY]` passes through unchanged
- *Edge case:* Empty input → empty output
- *Edge case:* Input with no tokens passes through byte-for-byte
- *Class coverage:* One case per token class enumerated by `detect.BuiltinRules()` — asserts the regex character set covers all classes produced today

*Runner:*
- *Happy path:* `RunMask` on input containing a hostname produces output containing one canonicalizable token; running twice with two different process instances (fresh ephemeral stores each time) yields the same canonicalized output (proves seed-independence)
- *Cleanup:* `RunMask` does not leave temp directories behind after return (assert via `os.MkdirTemp` parent has no opsmask-corpus-* entries owned by this process after the call)
- *Cleanup on panic:* `defer os.RemoveAll` runs even if `engine.Process` returns an error

*CorpusRoot:*
- *Happy path:* Called from `internal/corpus/` (test cwd) returns the repo's `testdata/corpus` absolute path
- *Happy path:* Called from a deep subdirectory of the repo returns the same absolute path
- *Happy path (CLI integration):* `CorpusRoot()` invoked from `internal/cli/corpus_test.go` (different cwd, different package — covered explicitly in U4 tests) resolves to the same `testdata/corpus` absolute path. This proves both packages share one root resolution, not two different conventions.
- *Failure:* Called from `os.TempDir()` (no go.mod within reach) returns an error naming the missing `go.mod`

*ValidateScenarioName:*
- *Happy path:* `"k8s-secret-yaml-multidoc"` accepted
- *Failure:* `".."`, `"../foo"`, `"foo/bar"`, `"-foo"`, `"foo-"`, `""`, `"FOO"`, `"foo_bar"`, `"a"`, `"ab"`, `"aa"` all rejected with named errors (length-2 inputs `aa`/`ab` fail because the regex requires length ≥3)
- *Containment:* After joining and cleaning `<corpusRoot>/<name>`, `filepath.Rel` must yield a relative path that does not start with `..`

*Diff helper:*
- *Happy path:* Identical inputs return empty string
- *Happy path:* Single-line difference produces a hunk with one `-` and one `+` line, plus surrounding context
- *Edge case:* Multi-hunk diff (two distant changes in a long file) produces two hunks with `@@` headers
- *Edge case:* Trailing newline differences are visible in output

**Verification:**
- `go test -race ./internal/corpus/...` passes for U2 files.
- Manual: invoke `Canonicalize` on `engine_test.go`'s expected outputs across two test runs with different allocator secrets — same canonicalized result.

---

- U3. **Implement corpus discovery, comparison helper, the `TestCorpus` test harness, and the corpus README**

**Goal:** Walk `testdata/corpus/` at test time (resolved via U2's `CorpusRoot()`), run each scenario through `RunMask`, canonicalize, diff against `expected.txt`, and fail with a unified diff on mismatch. Per-scenario sub-tests via `t.Run`. Failure-path coverage uses an in-memory `Compare` helper unit-tested separately, never by corrupting committed goldens.

**Requirements:** R3, R9

**Dependencies:** U2

**Files:**
- Create: `internal/corpus/discover.go` + `internal/corpus/discover_test.go`
- Create: `internal/corpus/compare.go` + `internal/corpus/compare_test.go` (pure-function comparison: takes expected bytes + got bytes, returns empty string or unified diff via U2's diff helper)
- Create: `internal/corpus/corpus_test.go` (`TestCorpus` driver)
- Create: `testdata/corpus/_smoke-hello/{input.txt, expected.txt, README.md}` (tooling smoke scenario; bootstrap PR may keep or remove)
- Create: `testdata/corpus/README.md` (documents the corpus convention: structure, when to add a scenario, the `corpus add` → review → commit workflow)

**Approach:**
- `Discover(root string) ([]Scenario, error)`: uses `os.ReadDir` (no recursion needed; scenarios are direct children). `Scenario` fields: `Name`, `InputPath`, `ExpectedPath`. Hard-fails when `input.txt` or `expected.txt` is missing in a scenario directory, naming the offending scenario.
- `Compare(expected, got []byte) (diff string)`: returns the empty string on byte-equal match, otherwise the unified-diff string from U2's helper. Pure function, no I/O — exercised by `compare_test.go` with in-memory inputs covering both success and mismatch paths. This is what makes the failure-path test possible without touching any committed golden.
- `TestCorpus(t)`: calls `CorpusRoot()`, then `Discover`, then `t.Run(scenario.Name, …)` per scenario. Inside each sub-test: read `input.txt`, run `RunMask(ctx, input)`, `Canonicalize(out)`, compare to read-in `expected.txt` via `Compare`, `t.Fatalf` on non-empty diff with the diff in the failure message.
- When `testdata/corpus/` is empty (no scenarios discovered), `TestCorpus` emits one passing sub-test `TestCorpus/empty` and exits — avoids "no tests run" noise while bootstrap is pending.
- The smoke scenario `_smoke-hello/` lives at the repo-level `testdata/corpus/`. Its README marks it as a tooling smoke test, not a real regression case. Bootstrap PR (U5) decides whether to keep or remove it.
- The corpus README is a single screen of prose: scenario directory structure, what `<scenario>/README.md` should contain, the `opsmask corpus add` workflow, golden-update via `opsmask corpus accept`, and a note that `expected.txt` is reviewable in PRs.

**Execution note:** Test-first for `Compare` and `Discover`; the harness driver follows once those primitives are green.

**Patterns to follow:**
- `internal/engine/engine_test.go:15` for engine invocation shape (already encapsulated by U2's `RunMask`).
- `internal/cli/helpers.go:buildRuntime` is **explicitly NOT used** by U3 — `buildRuntime` opens persistent project state and would violate origin R11. The test composes everything via U2's `RunMask` instead.

**Test scenarios:**
- *Happy path / Covers AE1:* The `_smoke-hello` scenario passes with zero diff against current engine output.
- *Happy path / Covers AE5:* `Compare(canonicalized_run_a, canonicalized_run_b)` returns the empty string when both runs use different allocator secrets but identical inputs (proves canonicalization eliminates secret-dependent variation).
- *Failure path / Covers AE2 (via `compare_test.go`, NOT by corrupting any golden):* `Compare(expected_with_token, got_without_token)` returns a non-empty unified diff naming the missing token; the test asserts the diff contains the scenario-relevant line markers.
- *Discovery happy path:* `Discover` on a directory with one valid scenario returns one `Scenario` struct with correct paths.
- *Discovery edge case:* `Discover` on an empty directory returns empty slice, no error.
- *Discovery failure:* `Discover` on a scenario directory missing `input.txt` returns an error naming the scenario.
- *Discovery failure:* Same for missing `expected.txt`.
- *Sub-test naming:* `go test -run 'TestCorpus/_smoke-hello' ./internal/corpus/` executes only that scenario.
- *Empty corpus:* When `testdata/corpus/` exists but contains no scenario directories, `TestCorpus` passes with a single `TestCorpus/empty` sub-test.
- *Root resolution from test:* `TestCorpus` running from `internal/corpus/corpus_test.go` resolves the corpus root to the repo-level `testdata/corpus/` and finds `_smoke-hello`.

**Verification:**
- `go test -race ./internal/corpus/...` passes with `_smoke-hello` present.
- `go test -run 'TestCorpus/_smoke-hello' ./internal/corpus/` runs only that scenario.
- `compare_test.go` includes the failure-path case proving the diff helper produces readable output without corrupting any committed scenario.

---

- U4. **Implement `opsmask corpus add | accept | list` CLI subcommands**

**Goal:** Three subcommands under a `corpus` parent command. `add` is semi-interactive (y/n/e prompt). `accept` regenerates the golden but never commits. `list` enumerates scenarios with metadata.

**Requirements:** R5, R6, R7, R8

**Dependencies:** U2 (canonicalizer, RunMask, CorpusRoot, ValidateScenarioName, diff), U3 (Discover, Compare)

**Files:**
- Create: `internal/cli/corpus.go`
- Modify: `internal/cli/root.go` — register the `corpus` parent command AND add `"corpus": true` to the `RewriteArgs` known map (otherwise `opsmask corpus list` is rewritten to `opsmask mask corpus list` per `internal/cli/root.go:64-86`).
- Modify: `CONTRIBUTING.md` (or create if absent — confirm at code time) — add a one-paragraph pointer to `testdata/corpus/README.md` for "how to guard a detection bug fix against regression."
- Test: `internal/cli/corpus_test.go`
- Test: `internal/cli/root_test.go` — add a case asserting `RewriteArgs([]string{"corpus","list"})` and `RewriteArgs([]string{"corpus","add","./f"})` are unchanged.

**Approach:**
- Cobra parent command `corpus` with three children. One file mirroring the shape of `internal/cli/mask.go` (~200-250 LOC including subcommands).
- All three subcommands route engine invocation through U2's `corpus.RunMask` — they do **not** call `cli.buildRuntime`. This is what enforces origin R11 ("no persistent state outside testdata/corpus/").
- All scenario directory writes are atomic: write to `os.CreateTemp(scenarioDir, ".tmp-*")`, write content, `Close`, then `os.Rename` to the final path.
- All scenario names pass through `corpus.ValidateScenarioName` before any path is constructed; validation failures exit non-zero with a clear message naming the rule that was violated.

- **`add`:**
  - Args: positional `<file>`; flags `--scenario <name>` (required), `--note <text>` (optional).
  - Validates scenario name; resolves corpus root via `corpus.CorpusRoot()`; refuses if `testdata/corpus/<scenario>/` already exists (suggests `accept`).
  - Reads the input file, calls `corpus.RunMask`, canonicalizes the output, prints proposed expected to stderr, prompts y/n/e on stdin (TTY-aware: in non-TTY context, refuse rather than hang).
  - On `y`: creates the scenario dir, then atomically writes `input.txt` (copy of source), `expected.txt` (canonicalized), and `README.md` if `--note` provided.
  - On `n`: prints "not written" guidance and exits 0.
  - On `e`: writes proposed expected to a temp file, opens `$EDITOR` on it, re-reads after editor exits, re-prompts.

- **`accept` (two-phase):**
  - Args: positional `<scenario>` OR `--all` flag (mutually exclusive).
  - Flags: `--force` (override the uncommitted-changes guard).
  - **Phase 1 (preflight):** enumerate targets (one named scenario, or `Discover` all); for each: validate it has `input.txt`; run `git status --porcelain testdata/corpus/<scenario>/`; if non-empty and `--force` not set, fail the entire run with a list of dirty scenarios. No writes occur in phase 1.
  - **Phase 2 (writes):** only runs if phase 1 reported no errors. For each target: `RunMask` on `input.txt`, canonicalize, atomic-write `expected.txt`. Print a one-line "wrote: …" per scenario.
  - Never invokes `git add` or `git commit`. The user runs `git diff` and commits.

- **`list`:**
  - No args/flags in v1.
  - `Discover` the corpus; for each scenario, gather: scenario name, byte size of `input.txt`, last-accept date from `git log -1 --format=%cs -- testdata/corpus/<scenario>/expected.txt` (best-effort; falls back to `"(no git history)"` if git is unavailable or the scenario is uncommitted).
  - Tab-separated output, one scenario per line; empty corpus prints `"no scenarios"`.

- **R8 framing:** corpus commands do **not** invoke `internal/exec/orchestrate` (the trust-gated arbitrary-command executor). Fixed-purpose subprocesses (`git status --porcelain`, `git log -1`, `$EDITOR`) are explicitly allowed: scoped, non-user-controlled, and necessary for the documented UX. The exec trust gate is unaffected because these commands never touch `exec.enabled` and never construct a command from user input.

**Patterns to follow:**
- `internal/cli/mask.go` for cobra command shape and exit-code propagation (`cli.UsageError`, `cli.ExitCode`).
- `internal/cli/root.go` for registration and `RewriteArgs`.

**Test scenarios:**

*RewriteArgs (`internal/cli/root_test.go`):*
- *Happy path:* `RewriteArgs([]string{"corpus", "list"})` returns `["corpus", "list"]` unchanged.
- *Happy path:* `RewriteArgs([]string{"corpus", "add", "./fixture.txt", "--scenario", "x"})` is unchanged.
- *Regression:* `RewriteArgs([]string{"unknown-command"})` still gets prefixed with `mask` (proves the change is additive).

*CorpusRoot from CLI cwd (`internal/cli/corpus_test.go`):*
- *Integration:* Calling `corpus.CorpusRoot()` from a test in `internal/cli/` returns the same absolute `testdata/corpus` path as when called from `internal/corpus/`. Proves the go.mod-walk root helper is cwd-agnostic and both packages share one resolution.

*Scenario name validation:*
- *Failure path:* `corpus add ./f --scenario "../etc/passwd"` exits non-zero before any filesystem write.
- *Failure path:* `corpus add ./f --scenario "FOO"` exits non-zero (uppercase rejected).
- *Failure path:* `corpus add ./f --scenario "foo bar"` exits non-zero (whitespace rejected).

*Add:*
- *Happy path / Covers AE3:* `corpus add ./fixture.txt --scenario kubectl-test --note "from issue X"` with stdin `y` writes `testdata/corpus/kubectl-test/{input.txt, expected.txt, README.md}` (use a temp corpus root via test fixture).
- *Edge case:* `corpus add` against an existing scenario directory exits non-zero with a "use accept" message; no files modified.
- *Edge case:* `corpus add` with no `--scenario` flag exits non-zero with the cobra usage error.
- *Edge case:* `corpus add` with stdin `n` leaves the filesystem unchanged (assert by listing the parent dir before/after).
- *Edge case (non-TTY):* `corpus add` with no TTY and no `--yes` flag refuses rather than hanging.
- *Atomicity:* simulate write failure mid-write (e.g., disk-full) — the scenario directory contains no partial files (no `.tmp-*` left, no `expected.txt`).

*Accept:*
- *Happy path / Covers AE4:* `corpus accept <scenario>` with an unchanged working tree regenerates `expected.txt`; the new file content matches `Canonicalize(RunMask(input))`.
- *Failure path:* `corpus accept <scenario>` with uncommitted changes in that scenario's directory and no `--force` exits non-zero without writing.
- *Happy path:* `corpus accept --force <scenario>` with uncommitted changes proceeds.
- *Happy path:* `corpus accept --all` regenerates every scenario's expected; reports per-scenario.
- *Two-phase correctness:* `corpus accept --all` with two valid scenarios and one dirty (no `--force`) — fails the whole run; assert NEITHER valid scenario's `expected.txt` was modified (this is what the two-phase split exists to guarantee).
- *Edge case:* `corpus accept` with both a positional scenario AND `--all` exits non-zero (mutually exclusive flags).
- *Edge case (no git):* `corpus accept` with `git` not on PATH falls back to a clear error message (or proceeds with `--force`) — implementer choice, documented.

*List:*
- *Happy path:* `corpus list` with one committed scenario prints one tab-separated line: name, input size, last-accept date.
- *Happy path:* `corpus list` with one uncommitted scenario shows `"(no git history)"` for the date.
- *Edge case:* `corpus list` on empty corpus prints `"no scenarios"`.

*Integration:*
- `corpus add` followed by `corpus list` shows the new scenario.
- `corpus add` then run `go test ./internal/corpus/...` — newly added scenario passes the corpus harness (proves CLI and harness use the same engine composition via U2's `RunMask`).
- `corpus add` then `corpus accept` against the same scenario produces no diff (idempotent on identical input).

**Verification:**
- All tests above pass under `go test -race ./internal/cli/...`.
- Manual end-to-end: `opsmask corpus add ./fixture.txt --scenario test-foo --note "..."` (answer y), `opsmask corpus list` shows it, `go test ./...` passes, `git status` shows the new scenario as untracked, `git diff` after `git add` shows reviewable golden content.
- `opsmask corpus list` works without arguments (proves the RewriteArgs fix).

---

- U5. **Bootstrap corpus content (≥10 scenarios)**

**Goal:** Land ≥10 hand-curated scenarios covering the breadth listed in R10. Per origin Key Decisions, this is a separate PR after U1-U4 land.

**Requirements:** R10

**Dependencies:** U2, U3, U4 (all of the tooling PR must be merged first)

**Files:**
- Create: `testdata/corpus/k8s-secret-yaml-multidoc/{input.txt, expected.txt, README.md}` (regression for `98d0b84`)
- Create: `testdata/corpus/fqdn-public-suffix/{input.txt, expected.txt, README.md}` (regression for `ccc678c`)
- Create: `testdata/corpus/kubeconfig-aws-eks/{input.txt, expected.txt, README.md}`
- Create: `testdata/corpus/kubectl-get-pods-yaml/{input.txt, expected.txt, README.md}`
- Create: `testdata/corpus/ipv4-and-ipv6/{input.txt, expected.txt, README.md}`
- Create: `testdata/corpus/openstack-uuid/{input.txt, expected.txt, README.md}`
- Create: `testdata/corpus/dotenv-file/{input.txt, expected.txt, README.md}`
- Create: `testdata/corpus/journalctl-systemd/{input.txt, expected.txt, README.md}`
- Create: `testdata/corpus/ssh-remote-output/{input.txt, expected.txt, README.md}`
- Create: `testdata/corpus/k8s-secret-yaml-singledoc/{input.txt, expected.txt, README.md}`
- Optional remove: `testdata/corpus/_smoke-hello/` (the U3 smoke scenario; remove only if redundant with the bootstrap)

**Approach:**
- For each scenario: produce sanitized input (synthetic preferred; sanitized real data acceptable with sanitization documented in `README.md`). Use `opsmask corpus add` for each so the canonicalized expected is generated by the same path that future contributors will use.
- Each `README.md` follows a short standard shape: source (synthetic or sanitized real), what regression class it guards against, sanitization notes (if any).
- The two regression-anchored scenarios (K8s YAML multidoc, FQDN PSL) are constructed from the inputs in the failing pre-fix tests in those commits; sanitize host names but keep structural shape identical.

**Execution note:** Use `opsmask corpus add` for every scenario so the golden is generated by the same path future maintainers use; do not hand-write `expected.txt`. The TDD shape (F4) only applies when the engine is currently wrong about a scenario — none of the bootstrap scenarios are in that state.

**Patterns to follow:**
- For sanitization, hand-edit real data into representative synthetic equivalents (replace real hostnames with `customer-prod-01`-shaped synthetics, real IPs with RFC 5737 documentation ranges, etc.). **Do not** pre-mask with `opsmask mask` and then feed the result into `corpus add` — `engine.Process` invokes `InertEscape` on already-tokenized input, producing `[OPSMASK_ESCAPED_SENTINEL:…]` markers rather than stable canonical tokens, which would break the golden.
- The `input.txt` committed to the corpus is plaintext (or sanitized-plaintext) shaped like real agent output. The engine masks it during `corpus add` exactly once; the canonicalized result is the committed `expected.txt`.

**Test scenarios:**
- *Happy path:* After the PR lands, `go test ./internal/corpus/...` passes all ≥10 scenarios with zero diff.
- *Reviewability:* Each scenario's `expected.txt` is a clean diff in the PR (no spurious whitespace, deterministic ordering) — reviewer can read each one to spot incorrect masking before merge.

(Detector class coverage is exercised by U2's canonicalizer tests, not by bootstrap. R14's coverage target is real-shape representative scenarios named in R10, not exhaustive class enumeration.)

**Verification:**
- The PR review catches any incorrect ground truth.
- `go test ./...` passes after merge.
- `opsmask corpus list` reports ≥10 scenarios.

---

## System-Wide Impact

- **Interaction graph:** New `internal/corpus` package consumed only by its own tests and by `internal/cli/corpus.go`. No production code depends on it. `engine.Process` signature is consumed (read) but not modified. `internal/cli/root.go` gains one entry in `RewriteArgs`'s known map and one registered subcommand.
- **Error propagation:** Test failures surface through standard `testing.T.Fatalf` with U2's unified-diff helper formatting the message. CLI errors propagate via the existing cobra `RunE` + `cli.ExitCode(err)` pipeline; scenario-name validation failures use `cli.UsageError` for proper exit-code shape.
- **State lifecycle risks:** `RunMask` creates an ephemeral `os.MkdirTemp` SQLite store and `defer os.RemoveAll`s it on every invocation — both tests and CLI. No persistent state under `~/.config/opsmask/` or project `.opsmask/` is touched. CLI subcommand writes occur only inside `<corpusRoot>/<scenario>/` after `filepath.Rel` containment check; atomic write via `CreateTemp` + `Close` + `Rename` so an interrupted run leaves no partial files.
- **API surface parity:** None. The engine API is not changed. `cli.buildRuntime` is not extended; corpus commands deliberately do not use it (per R11).
- **Integration coverage:** U4's "Integration" test scenarios cross the CLI → corpus runner → engine → filesystem boundary; the `corpus add` → `go test ./internal/corpus/...` round-trip proves the CLI and harness share the same engine composition (would fail loudly if the two diverged).
- **Unchanged invariants:** `internal/engine/Process` signature is not modified. The existing `opsmask mask`, `opsmask exec`, and Claude Code hook paths are untouched. Token format produced for live workloads is unchanged (canonicalization is test-side only). The exec trust gate is unaffected (corpus commands never invoke `internal/exec/orchestrate`).

---

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| Token format changes (e.g., new class added) silently bypass canonicalizer | U2 sources its regex from `detect.TokenRegexp()` (single source of truth); a new class is automatically captured. Class-coverage test in U2 enumerates `detect.BuiltinRules()` classes and asserts each produces a canonicalizable token, so a regex character-set gap is caught. |
| `corpus accept` shells out to `git` and fails on systems without git on PATH | Document `git` as a dev-environment dependency. Fallback message names `git` explicitly. `--force` allows bypassing the dirty-tree check entirely. |
| Bootstrap PR ground-truth errors (a scenario's expected.txt encodes a wrong masking) ship as "correct" | PR review of each `expected.txt` is the gate. The per-scenario README forces the author to articulate what the scenario guards against. The `_smoke-hello` scenario in U3's PR ensures the harness is exercised before bootstrap content lands. |
| Future engine improvements that newly mask something previously unmasked break every scenario | Expected behavior. `opsmask corpus accept --all` regenerates; reviewer scrutinizes the resulting diff alongside the engine change. The two-phase accept makes this safe (no partial mutation on failure). |
| Bootstrap PR drowns reviewers with 10+ scenarios at once | Origin Key Decisions splits bootstrap from tooling; the bootstrap PR is reviewable scenario-by-scenario. The single-PR-per-brainstorm default is overridden here per the brainstorm's explicit choice. |
| Scenario name path-escape (e.g., `--scenario "../etc/passwd"`) | `ValidateScenarioName` enforces kebab-case regex; `filepath.Rel` containment check after path join rejects anything that escapes the corpus root. Tested with `..`, separators, absolute paths, empty, non-kebab. |
| `corpus add` interrupted mid-write leaves partial scenario | Atomic write via `os.CreateTemp` + `Close` + `os.Rename`. An interrupted run leaves at most a `.tmp-*` file, never a partial `input.txt` or `expected.txt`. |
| `accept --all` partial mutation when one target is dirty | Two-phase implementation: phase 1 validates all targets and runs dirty-tree check; phase 2 writes only if phase 1 reported zero errors. |
| Persistent project state created during corpus operations (violates origin R11) | `RunMask` uses `os.MkdirTemp` + `defer os.RemoveAll` for the SQLite store. CLI corpus commands route through `RunMask` exclusively — `cli.buildRuntime` is explicitly not used. |
| Engine produces Unicode tokens by default but plan canonicalizer assumed ASCII | Resolved during plan review: U2 canonicalizer reuses `detect.TokenRegexp()` which already covers both `⟪opsmask:…⟫` and `[[opsmask:…]]` forms. Test cases cover both. |
| `RewriteArgs` swallows the new `corpus` command | Resolved during plan review: U4 modifies `internal/cli/root.go` to add `"corpus": true` to the known map; `root_test.go` adds regression tests asserting `RewriteArgs` leaves `corpus` invocations unchanged. |

---

## Documentation / Operational Notes

- `testdata/corpus/README.md` is created in U3 (file list above), explaining scenario structure, the `corpus add` → review → commit workflow, and `corpus accept` for golden updates.
- `CONTRIBUTING.md` update is folded into U4 (file list above) — one paragraph pointing to `testdata/corpus/README.md`.
- No CI configuration changes — the corpus runs via plain `go test ./...` (origin R12). When a future CI workflow lands (tracked in `docs/ideation/2026-05-06-internal-readiness-ideation.md` survivor #3), it will run the corpus by virtue of running the standard test target.
- No CHANGELOG entry for U2-U4 alone (internal tooling, no user-visible CLI surface beyond the new `corpus` subcommand which is dev-facing). The bootstrap PR (U5) warrants a CHANGELOG line: "Add detection regression corpus under testdata/corpus/."
- Document subprocess use explicitly in `testdata/corpus/README.md`: corpus commands invoke `git status --porcelain`, `git log -1`, and `$EDITOR` — these are the only external commands these subcommands run, and they are scoped, fixed-purpose, and not user-controlled.

---

## Sources & References

- **Origin document:** `docs/brainstorms/2026-05-06-detection-corpus-requirements.md`
- **Related ideation:** `docs/ideation/2026-05-06-internal-readiness-ideation.md` (survivor #6)
- **Related code:**
  - `internal/engine/engine.go` (engine entry)
  - `internal/engine/engine_test.go` (test invocation pattern)
  - `internal/cli/mask.go` (cobra subcommand pattern)
  - `internal/cli/root.go` (registration)
  - `internal/cli/helpers.go` (`buildRuntime`)
  - `internal/detect/rules/builtin.go` (concrete detector rules — source for class enumeration in U2's coverage tests)
  - `internal/detect/codec.go` (`RenderToken` — token format authority; `TokenRegexp()` is the canonicalizer's regex source)
  - `internal/detect/registry.go` (rule registry plumbing — read-only reference; does not enumerate concrete classes)
- **Recent commits the corpus guards against regressing:**
  - `98d0b84` — "fix(detect): k8s detector no longer crosses YAML newlines or dotted paths"
  - `ccc678c` — "refactor(hostname): switch to Public Suffix List for hostname validation"
