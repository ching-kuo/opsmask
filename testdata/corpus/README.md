# Detection Regression Corpus

This directory holds end-to-end masking goldens for the OpsMask detection
engine. Each scenario is a directory containing:

```
<scenario-name>/
  input.txt         # raw input fed to engine.Process
  expected.txt      # canonicalized engine output (golden, byte-compared)
  README.md         # optional: source, sanitization notes, what regression it guards
```

The harness lives in `internal/corpus/`. It walks this directory, runs each
`input.txt` through `engine.Process` against `detect.BuiltinRules()` with an
ephemeral SQLite mapping store, canonicalizes the output (token IDs replaced
with `*` so allocator-secret changes don't churn goldens), and diffs against
`expected.txt`. Any diff fails the scenario's sub-test under `TestCorpus`.

## When to add a scenario

Add a scenario whenever you fix a real regression in the detection engine
that has a representative input shape (a kubeconfig, a `kubectl` dump, a
`journalctl` excerpt). Synthetic inputs are preferred; sanitized real data
is acceptable when the sanitization is documented in the scenario's
`README.md`.

## Workflow

```
opsmask corpus add <input-file> --scenario <name> [--note "..."]
# review proposed expected.txt; answer y/n/e
git diff testdata/corpus/<scenario>/
git add testdata/corpus/<scenario>/ && git commit
```

The CLI never auto-commits. After `corpus add` writes the directory, you
review `expected.txt` and commit explicitly.

## Updating goldens after intentional engine changes

```
opsmask corpus accept --all
git diff testdata/corpus/    # scrutinize each change
git commit
```

`accept` regenerates `expected.txt` for one scenario or all of them, but it
refuses to overwrite scenarios with uncommitted changes (use `--force` to
override). It never invokes `git add` or `git commit`.

## Listing scenarios

```
opsmask corpus list
```

Tab-separated output: name, input size, last-accept date (from git log).

## What lives here vs. what doesn't

The corpus runs as part of `go test ./...` - there is no separate CI job.
The `corpus` CLI commands write only inside this directory; they do not
touch the project's persistent SQLite mapping store. Subprocess use is
limited to fixed-purpose calls (`git status --porcelain`, `git log -1`,
`$EDITOR`); the trust-gated `opsmask exec` arbitrary-command path is not
involved.

## Naming

Scenario names are kebab-case (lowercase letters, digits, hyphens), length
3+. Underscore-prefixed names like `_smoke-hello` exist only as planning
artifacts created by hand; the `corpus add` validator rejects them so user
input cannot escape the corpus root.
