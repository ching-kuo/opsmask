---
title: Claude Code Bash PreToolUse Hook (v0)
type: feat
status: active
date: 2026-05-02
origin: docs/brainstorms/2026-05-02-claude-code-bash-hook-requirements.md
---

# feat: Claude Code Bash PreToolUse Hook (v0)

## Summary

Ship a Claude Code `PreToolUse` hook that wraps non-skip-list `Bash` tool calls through a new `internal/exec` bypass entry point so command output is masked before reaching the agent context. Distribute via `opsmask install claude-code` / `opsmask uninstall claude-code` subcommands that opt a single project in (gitignored personal default, committable team-shared via `--team-shared`). Implementation has seven units: env-marker recursion guard, bypass exec entry point with `SourceHook` audit, skip-list/tokenizer package, hidden `claude-code-hook` JSON event handler, installer core with project detection and shim writer, the install/uninstall CLI subcommands, and the documentation pass.

---

## Problem Frame

OpsMask already provides masked execution via `opsmask exec`, but every wrap requires the user or agent to invoke it explicitly. AI coding agents in Claude Code reach for the `Bash` tool directly when investigating logs, and the raw bytes land in agent context before any masking can happen — see `docs/brainstorms/2026-05-02-claude-code-bash-hook-requirements.md` Problem Frame for the full motivation. Among the three coding-agent hosts surveyed in the ideation doc, only Claude Code currently ships a fully-implemented output-rewriting primitive at `PreToolUse`, which is the lever this plan targets.

The plan-time complication: the existing `exec.Orchestrate` path is policy-gated — it hard-denies `bash` at Layer A, requires `config trust` plus `exec.enabled: true`, and the default scope blocks `cat`/`tail`/`grep`/`journalctl`. Wrapping arbitrary agent-issued Bash commands through it as written is impossible. This plan introduces a sibling entry point that bypasses those gates while preserving the streaming-mask, audit, and process-group machinery `exec.Run` already provides.

---

## Requirements

Origin requirements R1-R17 from `docs/brainstorms/2026-05-02-claude-code-bash-hook-requirements.md` are carried forward verbatim. Key plan-affecting items:

- R1, R3, R5, R15 — hook coverage and skip-list semantics (drives U3, U4)
- R4 — wrap through OpsMask masked-execution path (drives U2 — the bypass entry point)
- R6, R7 — fail-closed with notification (drives the shim shape in U5)
- R8-R13, R16 — install/uninstall CLI surface and project detection (drives U5, U6)
- R17 — audit pass-throughs (drives U2, U4 audit-write paths)

**Origin actors:** A1 (OpsMask adopter), A2 (Claude Code), A3 (OpsMask binary)
**Origin flows:** F1 (first-time project opt-in), F2 (masked Bash invocation), F3 (fail-closed when OpsMask is unavailable)
**Origin acceptance examples:** AE1 (covers R5 — pipeline wrap), AE2 (R3 — skip-list match), AE3 (R6/R7 — fail-closed notification), AE4 (R8/R10 — first-run interactive prompt), AE5 (R9 — `$HOME` refusal), AE6 (R12 — uninstall preserves other content), AE7 (R16 — non-interactive flag), AE8 (R15 — recursion short-circuit)

---

## Scope Boundaries

- `PostToolUse` output masking, `Stop` transcript sweep, Read/Grep/MCP-tool-output coverage — deferred until v0 Bash-only path is validated in real use (see origin "Scope Boundaries").
- Codex CLI and Cursor integrations — separate brainstorms (Ideas 2 and 3 from the ideation doc).
- Subagent/skill packaging variant of OpsMask, LLM egress proxy, sentinel-as-credential reframe — separate brainstorms.
- Distribution as a Claude Code plugin (marketplace artifact); `--global` install flag.
- Per-project skip-list config file — v1 evolution path (origin R14), not part of this plan.
- Per-project customization of which verbs are wrapped beyond the v0 baked-in list.

### Deferred to Follow-Up Work

- **Idea 3 (MCP `instructions` field steering)** — separate brainstorm. The companion ideation paired Idea 1 with Idea 3; this plan is Idea 1 alone. Rationale captured in origin Open Questions.
- **Hook-script integrity binding** — v1 may extend `Trust(path)` to cover `<project>/.claude/settings.json` and the per-project shim at `<project>/.claude/opsmask-hook.sh`. v0 accepts the same project-write trust boundary that already governs `.opsmask/config.yaml` (see Key Technical Decisions).
- **Settings layering verification** — implementer confirms Claude Code's actual merge semantics for `.claude/settings.json` vs `.claude/settings.local.json` against current docs (see Open Questions / Deferred to Implementation). v0 implementation is robust to either rule because U5 scans both files.

---

## Context & Research

### Relevant Code and Patterns

**CLI structure (cobra-based, one file per subcommand):**
- `cmd/opsmask/main.go:13-22` — entry point; calls `cli.RewriteArgs(...)` then `cli.NewRoot(version)` then `Execute()`.
- `internal/cli/root.go:48-62` — `NewRoot` assembly. New top-level subcommand groups attach via `root.AddCommand(...)`.
- `internal/cli/root.go:65` — `RewriteArgs` known-commands map. **Must add `install`, `uninstall`, and `claude-code-hook`** or the `mask` shim eats them.
- `internal/cli/mcp.go:9-39` — subcommand-group precedent (`mcp` with `mcp serve` child). Mirror this exactly for `install` and `uninstall`.
- `internal/cli/exec.go:15-73` — cobra command shape with flags. Mirror for `--personal` / `--team-shared`.
- `internal/cli/root.go:18-46, 100-102` — `UsageError` / `CodeError` / `userErr` helpers and exit-code mapping. Use `CodeError{Code: 125, ...}` for runtime refusals to match exec conventions.

**Existing exec orchestration (the path being bypassed):**
- `internal/exec/orchestrate.go:18-30` — `Err*` constants. `ErrPolicyDenied` is what `bash` would hit at Layer A.
- `internal/exec/orchestrate.go:102-225` — `Orchestrate` pipeline. Bypass entry point reuses `Preflight()`, `BuildEnv()`, `Run()` but skips `Resolve` / `EvaluatePolicy` / trust+enabled gates.
- `internal/exec/run.go:42-127` — `Run` subprocess machinery. Manual `os.Pipe()`, `setProcessGroup`, dual-stream `engine.Process` masking with shared `pseudo.Allocator`. Reuse end-to-end.
- `internal/exec/run.go:67-73` + `internal/exec/fd_unix.go` — `CloseOnExecAll` invariant on fds ≥ 3. Do not regress.
- `internal/exec/env.go` — `BuildEnv(scope, cfg, extra)` with hardcoded hard-deny set (`BASH_ENV`, `LD_PRELOAD`, `BASH_FUNC_*`, etc.). Bypass entry point calls with `config.ScopeFreeform` so realistic dev-env passes through, hard-denies still strip.
- `internal/exec/denybase/denybase.go:12-26` — Layer A list. Confirms `bash`, `sh`, `zsh`, etc. are denied for the regular path (must remain so).

**Audit primitives:**
- `internal/exec/auditlog.go:21-22` — `SourceCLI = "cli"`, `SourceMCP = "mcp"`. **Add `SourceHook = "hook"`** here.
- `internal/exec/auditlog.go:60-62` — `WriteRecord` validates `Source` against the enum. Add `"hook"` to the validator.
- `internal/exec/auditlog.go:42-77` — `Record` schema, `NewRecord` constructor, `WriteRecord` write path. POSIX append mode + `O_CLOEXEC`, mode `0600`. Concurrency-safe at line size via kernel append atomicity.
- `internal/exec/auditlog.go:151-186` — `encodeRecord` truncation. Free for the bypass path; just sets `Truncated: true`.
- AST drift test for `Record` literal construction sites — see U2 below; the analyzer must learn that `internal/cchook/` and `internal/exec/orchestrate_hook.go` (or wherever) are sanctioned construction sites alongside `internal/cli/exec.go` and `internal/mcpsrv/`.

**Engine, runtime, config:**
- `internal/engine/engine.go:36` — `Process(ctx, r, w, rules, alloc, opts)` masking entry point. Allocator-shared, mutex-protected via `pseudo.Allocator.CommitPlans`. The bypass path calls this via `Run`'s existing dual-stream goroutines.
- `internal/runtime/runtime.go:38-83` — `runtime.New` constructs `*Env` with store, allocator, rules, trusted-config bool. Bypass mode reuses this but explicitly ignores `Loaded.Untrusted` / `Cfg.Enabled`.
- `internal/runtime/runtime.go:66-80` — `--config` override warning + strip pattern. Bypass mode mirrors the same defense (do not let `--config` enable `exec.enabled` via the hook path).
- `internal/config/trust.go:16-37` — `Trust(path)` / `IsTrusted(path)` API. Path-agnostic, keyed by realpath. Available for v1 hook-script binding without API changes.
- `internal/exec/auditlog.go:117` — `OPSMASK_AUDIT_DIR` env-var precedent. New `OPSMASK_EXEC_CHILD` recursion marker matches this naming convention.

**Test patterns (plain `go test`, table-driven, no testify):**
- `internal/cli/root_test.go:8-27` — table-driven test shape.
- `internal/cli/config_test.go:141-151` — `executeCLI(t, args, stdin)` harness for cobra integration tests.
- `internal/cli/exec_characterization_test.go:16-117` — characterization-test pattern. Mirror for U2 (regression on Layer-A deny for CLI/MCP-source).
- `t.TempDir()` + `t.Setenv(...)` + `t.Chdir(...)` — filesystem mocking pattern used throughout. No `afero`.
- Race detection mandatory per existing plan precedent (`docs/plans/2026-04-30-001-feat-mcp-server-plan.md:333,500`).

**Existing skill at `skill/opsmask/`:**
- `skill/opsmask/SKILL.md` — discipline-based agent guidance for opsmask CLI/MCP. Contains four phrases protected by `skill/opsmask/skill_contract_test.go:15-21` that any edit must preserve.
- New reference doc at `skill/opsmask/references/claude-code-hook.md` describes hook-active behavior (sentinels in Bash output, R17 audit silence). Original "Shells are rejected" text remains correct for `opsmask exec`; not modified.

**Plan-style precedent:**
- `docs/plans/2026-04-30-001-feat-mcp-server-plan.md` — the only existing plan in the repo. Frontmatter shape, section ordering, test-scenario shape, characterization-test pattern (its U4) all referenced here.

### Institutional Learnings

`docs/solutions/` does not exist. Institutional memory lives in `CHANGELOG.md`, `docs/REMAINING_RISKS.md`, and prior plans/brainstorms/ideation. Eleven directly load-bearing learnings surfaced (see ce-learnings-researcher output, summarized below):

1. **Layer A `bash` deny is path-normalized** (`internal/exec/policy.go:171, 297-299`) — bypass must be a separate entry point, not a policy carve-out. Confirmed.
2. **`exec` audit is `O_APPEND|O_CREATE|O_WRONLY|O_CLOEXEC`, mode `0600`** — POSIX append atomicity is the cross-process safety. Reuse via `OpenAppendLog("exec.log")`.
3. **`Source` field on `exec.Record` has runtime + AST-test enforcement** (CHANGELOG lines 22-23, MCP plan U2). New `SourceHook` requires updating the runtime validator. The AST drift test (`TestRecordLiteralASTDriftBlocksExternalConstruction` at `internal/exec/auditlog_test.go`) walks the module and flags any composite `Record{...}` literal **outside** `internal/exec/` — it has no allowlist to update. New code in `internal/exec/orchestrate_hook.go` is auto-allowed (same package); new code in `internal/cchook/` MUST use `exec.NewRecord(exec.SourceHook)` exclusively.
4. **`os.Pipe()` race fix** (CHANGELOG lines 80-85) — child stdout/stderr must use manual pipes, not `cmd.StdoutPipe`. Reuse `Run`, do not reinvent.
5. **SIGTERM-then-SIGKILL grace always escalates** (CHANGELOG line 218-219) — do not regress to a Ctrl-C-only-SIGTERM behavior.
6. **`--config <override>` cannot enable `exec.enabled`** (`internal/runtime/runtime.go:66-80`) — same defense applies to the hook path.
7. **`BuildEnv` is allow-list shaped, not deny-list** (`internal/exec/env.go:33-42`): everything not in `baselineEnvAllow(scope)` plus `cfg.EnvAllow` (or `LC_*` prefix) is dropped, regardless of the hard-deny set. Hard-deny strip is a defense-in-depth check on the allowed set. The `BASH_ENV`/`LD_PRELOAD`/`GIT_CONFIG_*` family is hard-denied (CHANGELOG lines 168-171). For the bypass mode, call `BuildEnv(config.ScopeFreeform, ...)` and inject `OPSMASK_EXEC_CHILD=1` **after** `BuildEnv` returns (since the marker isn't in the allow-list and would otherwise be filtered).
8. **`CloseOnExecAll` on fd ≥ 3 invariant** (CHANGELOG line 207-208, `internal/exec/run.go:67-73`) — preserved for free by reusing `Run`.
9. **`OPSMASK_*` env-var convention** (CHANGELOG lines 109-111) — new `OPSMASK_EXEC_CHILD` matches the `OPSMASK_AUDIT_DIR` naming convention. Note: `OPSMASK_STORE_CHILD` is referenced only in `internal/store/concurrency_multiprocess_test.go` as a test fixture using value comparison (`== "1"`) — it is not a production precedent. The plan uses `os.LookupEnv` for presence semantics regardless because presence is harder to spoof to falsey accidentally than value comparison.
10. **`README.md` Limitations + `docs/REMAINING_RISKS.md` are the canonical residual-risk surfaces** — every fail-mode asymmetry goes there. Seven entries needed (host bypass, team-shared DoS, shell-tokenization edges, silent skip-list pass-throughs, `$VAR` expansion in skip-listed commands, multi-hook chain semantics, hook_secret recovery).
11. **Trust binding extension to additional files is data-model-trivial** (`internal/config/trust.go:16-37` is path-agnostic) — v1 hook-script integrity work has a clean target.

### External References

- `code.claude.com/docs/en/hooks` — Claude Code PreToolUse hook contract: `updatedInput.command`, `decision: "block"`, `continue: false`, `stopReason`. Cited in origin doc capability matrix.
- Settings-layering merge semantics for `.claude/settings.json` vs `.claude/settings.local.json` — verify at implementation time via context7 MCP (deferred question, see Open Questions).

---

## Key Technical Decisions

- **Bypass exec via a sibling entry point reachable only through an HMAC-signed hidden subcommand, with git-toplevel binding and project-registry verification.** A new `internal/exec.OrchestrateHook(ctx, rt, command string, opts)` reuses `Preflight`, `BuildEnv(ScopeFreeform, ...)`, and `Run`, but skips `EvaluatePolicy` / `rt.Untrusted` / `rt.Cfg.Enabled`. The bypass is reachable **only** through a new hidden subcommand `opsmask claude-code-exec` (cobra `Hidden: true`, added to `RewriteArgs`). That subcommand requires a valid HMAC-SHA256 signature computed as `HMAC(secret, toplevel + "\x00" + command)` where `toplevel = ResolveProjectToplevel(os.Getwd())` — i.e., bound to the project's git-toplevel realpath at sign time. The secret lives at `~/.config/opsmask/hook_secret` (mode 0600, ownership = current user, **no env-var override**) generated once on first install. The hook handler (U4) reads the secret, computes the toplevel-bound sig, and emits `updatedInput.command = "<canonical-opsmask-path> claude-code-exec --sig <hex> -- <command>"` where `<canonical-opsmask-path>` is `os.Executable()` resolved via `filepath.EvalSymlinks` so the rewrite identifies the binary by realpath, not name. The hidden subcommand re-derives the secret, recomputes the HMAC against its own `ResolveProjectToplevel(os.Getwd())`, refuses with a clear error on mismatch. Binding to git-toplevel rather than raw cwd ensures a Claude Code session launched from a subdirectory of the registered project produces signatures the exec entry point can verify (closes codex F2). **No `--shell` flag is added to the public `exec` subcommand.**

  **What the HMAC scheme defends against and what it does not.** The scheme prevents naive or accidental direct invocation of `claude-code-exec` (e.g., a user typing the command at a shell, an attacker without same-UID access, a sig captured from one project replayed in another). It does **not** prevent a same-UID-malicious process from reading `~/.config/opsmask/hook_secret` and minting arbitrary toplevel-bound signatures — that threat boundary is the standard Unix same-UID trust assumption (identical to `~/.ssh/id_rsa`, `~/.aws/credentials`). Per-call nonces and time-bounded sigs are deferred to v1; toplevel binding is the v0 narrowing. Documented in REMAINING_RISKS so the defense scope is explicit, not implied.

  Resolves origin P0 #1; closes the doc-review finding that a public bypass flag would defeat `config trust`; closes codex F1 (HMAC overclaim) and F2 (secret-storage location).
- **Sentinel resolution preserved in `OrchestrateHook`, but plaintext stays out of the audit log.** The bypass calls `Resolve` over the wrapped command string before passing to bash, so `<<OPSMASK:...>>` sentinels in agent-issued commands resolve via `rt.Store.Lookup` — same as the regular path. Sentinel resolution is a separate concern from policy gating; skipping it would silently break the sentinel-as-credential workflow (origin Idea 5 builds on this). **The audit record stores the unresolved command (sentinel placeholders preserved)** — matching `internal/exec/orchestrate.go:113-117`'s pattern of recording argv before `Resolve` runs. Resolved plaintext flows into bash argv (inheriting the existing `/proc/<pid>/cmdline` residual-risk surface that's already documented for `opsmask exec` on multi-user hosts) but never into `exec.log`. Closes codex F4.
- **Recursion is prevented by canonical-binary identity check, not text-name match.** The skip-list entry for `opsmask` does NOT match by basename alone. The matcher: (i) tokenizes the command (per U3 rules), (ii) calls `exec.LookPath(argv[0])` to resolve the actual binary, (iii) calls `filepath.EvalSymlinks` on both that path and `os.Executable()`, (iv) matches only when the two realpaths are identical. A PATH-shadowed `opsmask` (e.g., a malicious project's `bin/opsmask`) does NOT pass through — it wraps. The wrap envelope itself uses the canonical absolute path (`os.Executable()` resolved) so Claude Code's re-issued command points at the verified opsmask binary; the second hook firing matches that identity check and passes through. The **secondary** mechanism is the `OPSMASK_EXEC_CHILD=1` env marker (presence semantics via `os.LookupEnv`), which `exec.Run` sets on every child process; it handles the case where a wrapped bash command itself spawns the verified opsmask as a grandchild (e.g., a script under `bash -c` calling `opsmask mask-text`). AE8's coverage explicitly spans both mechanisms. Closes codex F3 (text-match recursion guard).
- **Per-project shim location, not per-user; backed by a git-toplevel-keyed install registry that gates the bypass.** The installer writes the POSIX `sh` shim to `<project>/.claude/opsmask-hook.sh` rather than `~/.config/opsmask/hooks/`. This keeps the shim trust radius scoped to the project (matching `.opsmask/config.yaml`'s scope) and means `uninstall claude-code` can fully remove the shim alongside the settings-file edit. The installer ALSO writes the **git-toplevel realpath** (`filepath.EvalSymlinks(git rev-parse --show-toplevel)`) to a per-user registry at `~/.config/opsmask/hook_installs.json` (mode 0600). The hidden subcommands `claude-code-hook` and `claude-code-exec` verify on every invocation: (a) compute the current git-toplevel realpath via `git rev-parse --show-toplevel` against `os.Getwd()`, (b) check that toplevel is in the registry. They refuse with a clear error otherwise ("OpsMask hook fired in a project that was not opted in via `opsmask install claude-code`. Refusing."). Using git-toplevel rather than raw cwd ensures Claude Code sessions launched from a subdirectory of the project still resolve to the registered path. **Nested-repo behavior:** the **innermost** git toplevel wins (a submodule with its own `.git` registers separately from the outer repo). **No-git refusal (no fallback).** If `git rev-parse --show-toplevel` fails (no `.git/` reachable from cwd), both install AND hook invocation refuse with an actionable error: "OpsMask hook requires a git project. Run `git init` in this project, then re-run install." This eliminates the ambiguity between cwd-as-given and toplevel keys, removes a surface where two different invocations of the same non-git directory could resolve to different realpaths, and aligns install/runtime behavior on a single rule. Closes codex Round 3 B1 (non-git contradiction). Closes codex F2 (registry exact-cwd vs project-root) and F5 (malicious-project bypass-as-a-service).
- **Runtime-init prerequisite enforced at install.** `opsmask install claude-code` verifies that `runtime.New(...)` succeeds before writing any settings or shim files. If the project's mapping store, secret, or detector rules cannot be loaded, the installer prints an actionable error naming the fix (typically "run `opsmask init` in this project first"). This prevents the silent "every Bash call fails closed because runtime construction fails" experience an uninitialized project would otherwise produce. The hook handler (U4) also distinguishes runtime-construction failure from binary-missing in its fail-closed envelope.
- **Restricted skip-list match for interactive editors.** Editors (`vim`, `vi`, `nvim`, `nano`, `less`, `more`, `man`) match the skip-list only when invoked **bare** (zero arguments) or with safe read-only flags (`-R` for vim-family, no flags for less/more/nano/man). `vim -c 'read /etc/passwd' -c 'w /tmp/out' -c 'q'` does NOT match — it wraps. The skip-list table in U3 carries a per-verb argument predicate. This closes the file-read bypass surface flagged in doc-review.
- **Team-shared install confirmation includes the teammate-DoS warning.** `opsmask install claude-code --team-shared` prints, before any settings-file write, a warning naming the consequence explicitly: teammates and CI runners that clone the repo without OpsMask installed will hit fail-closed on every Bash call. The user must press Enter to confirm (or pass `--yes` to skip the prompt in scripted installs). Closes the doc-review finding that the teammate-DoS risk was documented in REMAINING_RISKS but absent from the install UX.
- **Identity statement: two operating modes.** This plan ships a deliberate two-mode product. **Mode A** (`opsmask exec`, `mask`, `unmask`, MCP) remains policy-gated, default-deny. **Mode B** (the `PreToolUse` hook + the `claude-code-exec` bypass entry point) is policy-bypassed for an explicitly opted-in project, with HMAC signing as the access control and audit logging as the safety net. The two modes have different threat models. Adopters opt into Mode B per-project; the README and skill reference page name both modes side-by-side. Closes the doc-review finding that the positioning shift was unnamed.
- **Personal install (`.claude/settings.local.json`, gitignored) by default.** Origin Key Decision preserved. `--team-shared` flag opts into committable `.claude/settings.json` after explicit user choice plus the warning above. The team-shared install creates a teammate-DoS risk — documented in `docs/REMAINING_RISKS.md` and surfaced at install time.
- **Idempotent install across both settings files.** Whether Claude Code merges `.local.json` and `.json` additively or `.local.json` overrides, the installer scans both files for an existing OpsMask hook block (identified by a stable `name: "opsmask"` field on the hook entry) and refuses with `"already installed at <path>"` if one exists in either. Same scan governs uninstall. v0 implementation is robust to either layering rule, deferring the verification to implementation.
- **Accept project-write trust boundary for v0 hook integrity; document explicitly.** Extending `Trust(path)` to cover the hook config file and per-project shim is a v1 evolution. The threat (project-write attacker replacing the shim or settings) is the same threat surface that already governs `.opsmask/config.yaml`'s rules — and the per-project shim location aligns the trust radius. The hook config is not LLM-writable from inside a Claude Code session unless the hook itself is misconfigured. Resolves origin P0 #2 in the conservative direction.
- **`SourceHook = "hook"` audit source.** Distinct from `SourceCLI` and `SourceMCP` so post-hoc audit-log analysis can attribute records. Used both for wrapped invocations (U2) and skip-list pass-throughs (U4). Skip-list pass-through records have an empty `ExitCode` and `DurationMs` since the command runs unwrapped — semantics documented in U2.
- **Pass-through audit volume control.** Skip-list pass-through records (R17) write to a separate file `pass_through.log` rather than `exec.log` so per-event audit volume in `exec.log` stays bounded for Mode A users. The pass-through writer applies the same POSIX append-mode semantics as `exec.log` and shares `OPSMASK_AUDIT_DIR`. Documented as the v0 mitigation for hot-loop audit growth flagged by doc-review; rotation/sampling deferred to v1.
- **Idea 3 (MCP `instructions` field steering) deferral rationale.** Idea 3 was the ideation's "highest leverage-to-effort" pick alongside Idea 1. It is being pursued as a separate parallel plan because (a) it requires no new architecture — just a string edit in `internal/mcpsrv/server.go` plus per-tool `description` updates, making it a poor fit for shared scope with this plan, and (b) the review pipeline for it is much shorter, so blocking the architecturally-loaded Idea 1 plan on a shared brainstorm review would slow both. Codex and Cursor adopters get Idea 3's coverage on its own track. The follow-up brainstorm filename will be `docs/brainstorms/<date>-mcp-instructions-steering-requirements.md`; this plan's CHANGELOG entry references it once landed. Closes the doc-review finding that Idea 3 was silently downgraded.

---

## Open Questions

### Resolved During Planning

- **How to bypass the existing `exec` policy/scope/trust gates without exposing a public bypass surface?** New sibling entry point `OrchestrateHook` reachable only through HMAC-signed hidden subcommand `claude-code-exec`. **No `--shell` flag on `exec`.** Resolves origin P0 #1 and the doc-review finding that any user-reachable bypass flag would defeat `config trust`.
- **Hook script integrity?** Accept the project-write trust boundary for v0 with the **per-project** shim location (matches `.opsmask/config.yaml` scope); document the residual surface in REMAINING_RISKS. v1 may extend `Trust(path)` to cover `.claude/settings.json` and `.claude/opsmask-hook.sh`. Resolves origin P0 #2.
- **Bash command-string tokenization rule for skip-list match?** Env-prefix-stripped argv[0] match plus zero shell-metacharacter check; **per-verb argument predicate** for editors and `git status` (closes the file-read bypass surface). Inline POSIX-style splitter in pure Go, no external dependency. Origin P1 #4.
- **Fail-closed notification when binary is missing?** Two-layer: per-project POSIX `sh` shim + opsmask diagnostic. Origin P1 #6.
- **Recursion prevention?** Two distinct mechanisms — primary: skip-list match on `opsmask` (R15, prevents wrap-recursion on Claude Code's re-issued top-level command); secondary: `OPSMASK_EXEC_CHILD=1` env marker (prevents grandchild recursion when wrapped bash itself spawns opsmask). Doc-review correction.
- **Sentinel resolution in `OrchestrateHook`?** Preserved. `Resolve` is a non-gating transform; skipping it would silently break sentinel-as-credential workflows. Doc-review correction.
- **Per-user vs per-project shim location?** Per-project (`<project>/.claude/opsmask-hook.sh`). Trust radius matches `.opsmask/config.yaml`'s scope; uninstall is fully symmetric. Doc-review correction.
- **Runtime-init prerequisite at install?** Verified by the installer; refuses with actionable error naming `opsmask init` if `runtime.New(...)` fails. Doc-review correction.
- **Bash-only framing softening?** Applied via origin doc edits (Summary, Problem Frame, Success Criterion); install confirmation message names the v0 limitation explicitly. Two-mode identity statement added to Key Technical Decisions.
- **Idea 3 (MCP `instructions` field steering)?** Separate parallel brainstorm with rationale (architectural disjunctness + shorter review pipeline). Codex/Cursor adopters get coverage on that track. Origin P2 #7.
- **v1 evolution path for Read/Grep/MCP coverage?** Out of scope; v0 install/config shape designed to be additive-compatible (`install claude-code` may grow `--tool=read|grep` flags; the hook block is one entry under `PreToolUse > Bash > matchers` and additional tools would add sibling entries). Origin P2 #8.
- **Multi-hook PreToolUse chain coexistence?** Installer detects, prompts user (chain / refuse / overwrite), surfaces the chain semantics in REMAINING_RISKS. Doc-review addition.

### Deferred to Implementation

- **Confirm Claude Code's settings-layering merge semantics for hook arrays.** Implementer fetches `code.claude.com/docs/en/hooks` via context7 MCP at implementation time. v0 implementation is robust to either rule because U5 scans both files. Origin P1 #3.
- **Concrete sentinel for "this is the OpsMask hook block".** Stable `name: "opsmask"` field on the hook entry. The exact JSON shape depends on Claude Code's parser tolerance for unknown fields — verified at U5 implementation time.
- **Exact JSON envelope shape for fail-closed responses.** Claude Code's hook contract has `continue: false` + `stopReason` plus optional `decision: "block"`. Verify the wrap envelope's `updatedInput.command` field name (some hook docs reference `hookSpecificOutput` wrapper for input modification — implementer confirms via context7 at U4 implementation).
- **Where to inject `OPSMASK_EXEC_CHILD=1` for both regular and bypass paths.** Must be injected **after** `BuildEnv` returns (since `BuildEnv` is allow-list-shaped and would otherwise filter the marker — note this differs from "not in hard-deny list" framing). Resolved during U1 implementation.
- **Whether to also propagate `OPSMASK_EXEC_CHILD` over child-of-child boundaries when bash itself sets it explicitly.** Default behavior of `exec.Cmd.Env` plus normal inheritance covers this; the deferred work is testing edge cases like `env -i ...` inside a wrapped command.

---

## High-Level Technical Design

> *This illustrates the intended approach and is directional guidance for review, not implementation specification. The implementing agent should treat it as context, not code to reproduce.*

**Sequence diagram — F2 (masked Bash invocation) end-to-end:**

```mermaid
sequenceDiagram
    participant CC as Claude Code
    participant Shim as <project>/.claude/opsmask-hook.sh
    participant Hook as opsmask claude-code-hook
    participant CCExec as opsmask claude-code-exec
    participant ExecH as OrchestrateHook
    participant Bash as bash -c
    participant Engine as engine.Process
    participant Audit1 as pass_through.log
    participant Audit2 as exec.log

    CC->>Shim: PreToolUse JSON event (stdin)
    Shim->>Shim: command -v opsmask
    alt binary missing
        Shim->>CC: JSON envelope {continue:false, stopReason:"..."} (stdout)
    else binary present
        Shim->>Hook: exec opsmask claude-code-hook (passes stdin)
        Hook->>Hook: Decode JSON, extract Bash.command; cwd = EvalSymlinks(Getwd())
        alt non-Bash tool
            Hook->>CC: JSON envelope {} (pass-through)
        else OPSMASK_EXEC_CHILD set (grandchild recursion)
            Hook->>CC: JSON envelope {} (pass-through)
        else cwd not in install registry
            Hook->>CC: JSON envelope {continue:false, stopReason:"...not opted in via opsmask install claude-code..."}
        else runtime.New fails
            Hook->>CC: JSON envelope {continue:false, stopReason:"...run opsmask init..."}
        else skip-list match (canonical-binary identity check for opsmask)
            Hook->>Audit1: WriteRecord(Source=hook, unresolved command, no ExitCode)
            Hook->>CC: JSON envelope {} (pass-through)
        else wrap path
            Hook->>Hook: toplevel = ResolveProjectToplevel(Getwd()); load hook_secret
            Hook->>Hook: sig = HMAC(secret, toplevel + "\x00" + command)
            Hook->>Hook: canonical-path = EvalSymlinks(Executable())
            Hook->>CC: JSON envelope {updatedInput.command:"<canonical-path> claude-code-exec --sig <hex> -- '<orig>'"}
            Note over CC: Claude Code re-issues — fresh process tree
            CC->>Shim: Re-issued command goes back through the shim (Bash hook fires again)
            Shim->>Hook: exec opsmask claude-code-hook (re-entry; cwd unchanged)
            Note over Hook: Skip-list match on opsmask via canonical-path identity → pass-through
            Hook->>Audit1: WriteRecord(Source=hook, "<canonical-path> claude-code-exec ...")
            Hook->>CC: JSON envelope {} (pass-through)
            CC->>CCExec: <canonical-path> claude-code-exec --sig <hex> -- '<orig>'
            CCExec->>CCExec: toplevel = ResolveProjectToplevel(Getwd()); registry check; load secret
            CCExec->>CCExec: expected = HMAC(secret, toplevel + "\x00" + command); hmac.Equal verify
            alt registry miss / sig mismatch
                CCExec->>CC: exit non-zero with diagnostic
            else valid
                CCExec->>ExecH: cchook.RunWrapped → OrchestrateHook(ctx, rt, command)
                ExecH->>Audit2: WriteRecord("starting", Source=hook, UNRESOLVED command)
                ExecH->>ExecH: Resolve sentinels → BuildEnv(ScopeFreeform) + inject OPSMASK_EXEC_CHILD=1
                ExecH->>Bash: spawn bash -c '<resolved>' (process group)
                Bash-->>Engine: stdout/stderr (manual os.Pipe)
                Engine-->>CC: masked bytes (sentinels)
                ExecH->>Audit2: WriteRecord(final, Source=hook, UNRESOLVED command, ExitCode, DurationMs)
            end
        end
    end
```

**Note on the diagram's re-entry path.** Claude Code re-issuing the wrap envelope produces a second PreToolUse hook invocation on the rewritten command. The handler matches `opsmask` in the skip-list (R15, primary recursion guard), writes a pass-through audit record, and returns an empty envelope so Claude Code runs `opsmask claude-code-exec` directly. The HMAC verification inside `claude-code-exec` is what actually gates the bypass. The `OPSMASK_EXEC_CHILD` env marker (U1) does NOT prevent this re-entry — it only prevents recursion when bash itself spawns a grandchild that re-invokes opsmask.

**Hook envelope grammar (sketch — exact field names verified against Claude Code docs at U4):**

```
PreToolUse-input  := { tool_name: "Bash", tool_input: { command: string, ... } }
HookResponse      := EmptyEnvelope | RewriteEnvelope | RefuseEnvelope
EmptyEnvelope     := {} or { continue: true }                  # pass-through
RewriteEnvelope   := { updatedInput: { command: string } }    # wrap
RefuseEnvelope    := { continue: false, stopReason: string }  # fail-closed
```

**Module map (Output Structure):**

```
internal/
  exec/
    orchestrate_hook.go        [U2] OrchestrateHook entry point (preserves Resolve)
    orchestrate_hook_test.go   [U2] tests incl. Layer A regression
    env.go                     [U1] OPSMASK_EXEC_CHILD injection (post-BuildEnv)
    run.go                     [U1] inject helper called before cmd.Start()
    exec_child.go              [U1] IsExecChild() helper
    exec_child_test.go         [U1] tests
    auditlog.go                [U2] SourceHook constant + WriteRecord validator
  cchook/
    skiplist.go                [U3] hardcoded skip-list table with per-verb predicates
    tokenize.go                [U3] inline shell splitter + metacharacter check (no external deps)
    tokenize_test.go           [U3] table-driven matching tests (~40 scenarios)
    handler.go                 [U4] JSON envelope handler (calls cchook.Match, signs wrap)
    handler_test.go            [U4] stdin/stdout JSON round-trip tests
    exec.go                    [U4] cchook.RunWrapped (HMAC verify + dispatch to OrchestrateHook)
    exec_test.go               [U4] HMAC verify tests + security regression
    secret.go                  [U4] hook_secret load/ensure
    secret_test.go             [U4] secret-file mode + creation tests
  install/
    install.go                 [U5] project detection + runtime-init prereq + settings write
    install_test.go            [U5] tempdir-based install/uninstall tests
    detect.go                  [U5] DetectProject + DetectSettings helpers
    shim.go                    [U5] per-project shim writer
    registry.go                [U4/U5] Register/Unregister/IsRegistered for hook_installs.json
    registry_test.go           [U4/U5] registry mode/owner/idempotency tests
  cli/
    install.go                 [U6] cobra wiring for install claude-code
    uninstall.go               [U6] cobra wiring for uninstall claude-code
    claude_code_hook.go        [U4] hidden claude-code-hook subcommand wiring
    claude_code_exec.go        [U4] hidden claude-code-exec subcommand wiring
    root.go                    [U6] AddCommand updates + RewriteArgs additions
                               [Adds: install, uninstall, claude-code-hook, claude-code-exec]
docs/
  REMAINING_RISKS.md           [U7] 5+ new entries (host bypass, team-shared DoS,
                                    tokenization, silent over-allow, $VAR expansion,
                                    multi-hook chain semantics, hook_secret recovery)
README.md                       [U7] new "Claude Code hook" section (two-mode framing)
CHANGELOG.md                    [U7] Unreleased entry
skill/opsmask/
  references/
    claude-code-hook.md        [U7] new reference page (acknowledges wrap visibility in tool-call UI)
```

**Files NOT modified (explicitly):**
- `internal/cli/exec.go` — no `--shell` flag is added. The bypass is reachable only through `claude-code-exec`.

---

## Implementation Units

- U1. **`OPSMASK_EXEC_CHILD` recursion-marker primitive**

**Goal:** Introduce the env-var marker that downstream code uses to detect "I am running inside an opsmask-spawned child" and short-circuit recursive masking.

**Requirements:** R15

**Dependencies:** None.

**Files:**
- Modify: `internal/exec/env.go` — add the marker injection after `BuildEnv` returns (the marker must not appear in the hard-deny list; verify the existing list's contents and add a code comment if there's any risk of accidental future stripping).
- Modify: `internal/exec/run.go` — call the injection helper before `cmd.Start()` so every child of `exec.Run` (regular and bypass) inherits it.
- Create: `internal/exec/exec_child.go` — exports `IsExecChild() bool` (presence-check via `os.LookupEnv("OPSMASK_EXEC_CHILD")`) and an internal `injectExecChild(env []string) []string` helper.
- Test: `internal/exec/exec_child_test.go` — table-driven tests on `IsExecChild` plus an integration test that spawns a child via `exec.Run` and asserts the marker is set in the child's environment.

**Approach:**
- Marker shape: presence-only. Value is `"1"` for human-readability but consumers MUST use `os.LookupEnv` (presence) rather than `os.Getenv == "1"` (value).
- Injection point: after `BuildEnv` returns the environment slice for the child but before the `exec.Cmd.Env` is set. This keeps the marker out of `BuildEnv`'s allow-list machinery (it's not user-supplied).
- Inheritance: bash itself does not strip the marker, so a `bash -c '<cmd>'` invocation that itself shells out further will preserve the marker — the recursion guard works transitively.

**Patterns to follow:**
- `OPSMASK_AUDIT_DIR` precedent at `internal/exec/auditlog.go:117` for env-var naming.
- `OPSMASK_STORE_CHILD` precedent in `internal/store/` for child-marker presence semantics.

**Test scenarios:**
- Happy path: `IsExecChild()` returns false in a fresh process; returns true when `OPSMASK_EXEC_CHILD=1` is set.
- Happy path: spawned child via `exec.Run` has `OPSMASK_EXEC_CHILD=1` in its environment (verified by the child writing its env to a temp file the parent reads).
- Edge case: marker with empty value (`OPSMASK_EXEC_CHILD=`) is treated as "set" by `IsExecChild` per `os.LookupEnv` presence semantics.
- Edge case: nested children (parent → child → grandchild) all see the marker.
- Edge case: hard-deny-set strip in `BuildEnv` does not strip `OPSMASK_EXEC_CHILD` — assertion that the marker survives a `BuildEnv` round-trip.
- Integration: full `exec.Run` invocation against a Go subprocess that prints `OPSMASK_EXEC_CHILD` to stdout; parent verifies "1" is captured.

**Verification:**
- `go test ./internal/exec/... -race -run ExecChild` passes.
- `git grep OPSMASK_EXEC_CHILD` shows the marker only in `env.go`, `run.go`, `exec_child.go`, the new tests, and the `cchook` package (after U4).

---

- U2. **`OrchestrateHook` bypass entry point + `SourceHook` audit**

**Goal:** Add the policy-bypassing exec entry point that the `claude-code-exec` hidden subcommand (U4) will dispatch to. Wire `SourceHook = "hook"` audit source through the runtime validator. Preserve `Resolve` so sentinels still resolve.

**Requirements:** R1, R4, R5, R17

**Dependencies:** U1.

**Execution note:** Start with characterization tests against the current `Orchestrate` to lock in Layer-A deny semantics for `Source: "cli"` and `Source: "mcp"` before introducing the new entry point. The bypass must not alter the existing path's behavior.

**Files:**
- Create: `internal/exec/orchestrate_hook.go` — exports `OrchestrateHook(ctx context.Context, rt Runtime, command string, opts HookOptions) (HookResult, error)`. All `Record{...}` literals at this site go through `NewRecord(SourceHook)` per project convention; the existing AST drift test at `internal/exec/auditlog_test.go` already sanctions everything inside `internal/exec/` so this file needs no analyzer change.
- Create: `internal/exec/orchestrate_hook_test.go` — bypass behavior, audit-record shape, regression test that the regular path still rejects `bash` from `cli`/`mcp` sources, and the `Resolve` round-trip for sentinels.
- Modify: `internal/exec/auditlog.go:21-22` — add `SourceHook = "hook"` constant.
- Modify: `internal/exec/auditlog.go:60-62` — extend `WriteRecord` validator to accept `"hook"`.
- Test: `internal/exec/auditlog_test.go` — add test for `SourceHook` validation (positive + negative).
- Note on AST drift: doc-review confirmed that the existing `TestRecordLiteralASTDriftBlocksExternalConstruction` walks the module and flags any composite `Record{...}` literal **outside** `internal/exec/`. New code in `internal/exec/orchestrate_hook.go` is auto-allowed (same package). New code in `internal/cchook/` (U3, U4) MUST use `exec.NewRecord(exec.SourceHook)` exclusively — no composite literals. No analyzer changes are needed.

**Approach:**
- Signature: `OrchestrateHook(ctx, rt, command string, opts HookOptions) (HookResult, error)` where `HookOptions` carries `Cwd`, `Timeout`, `Stdout`, `Stderr` (caller-owned writers — same convention as `Orchestrate`).
- Pipeline: `Preflight()` → **audit-write `"starting"` record with `Argv=["bash", "-c", command-as-received-with-sentinels-preserved]`** → run `Resolve` over the command string to substitute `<<OPSMASK:...>>` sentinels (Resolve happens AFTER audit so resolved plaintext never reaches `exec.log` — closes codex F4) → build `argv = []string{"bash", "-c", resolved}` for the actual exec → `BuildEnv(config.ScopeFreeform, rt.Cfg, nil)` (the env construction is allow-list-shaped — see `internal/exec/env.go:33-42` — so the `OPSMASK_EXEC_CHILD` marker is injected explicitly **after** `BuildEnv` returns, not relied upon to survive its allow-list pass) → `Run(ctx, argv, env, opts)` → **audit-write final record with the same unresolved Argv plus exit code and duration**. `Run` already streams output through `engine.Process` and handles process-group signals.
- **Skipped gates:** no `EvaluatePolicy`, no `rt.Untrusted` check, no `rt.Cfg.Enabled` check. **Preserved transforms:** `Preflight`, `Resolve` (sentinels resolve, but only into the bash subprocess argv — NOT into `exec.log`), `BuildEnv` (hard-deny strip still applies), `Run` (engine masking + process group + grace).
- **Audit record (load-bearing):** `Source=SourceHook`, `Argv=[]string{"bash", "-c", <unresolved-command>}` — sentinel placeholders preserved; the resolved plaintext does not appear in `exec.log`. `Scope=""` (n/a for bypass), `AllowMatch=""`, `DenyMatch=""`. The command shows up in `Argv[2]` and benefits from the existing 4095-byte truncation logic.
- **Process-list residual risk:** the resolved bash invocation's argv (`bash -c '<resolved>'`) is visible via `/proc/<pid>/cmdline` on multi-user hosts during execution. This is the same residual risk already documented for `opsmask exec` in `docs/REMAINING_RISKS.md`; the hook bypass inherits it. Documented as residual, not load-bearing for the v0 threat model (single-user developer machine).
- Regression invariant: a separate test invokes `Orchestrate` (the regular path) with `Source: SourceCLI` and `bash`-shaped argv; asserts it returns `ErrPolicyDenied` with `DenyMatch: "bash"`. Same with `SourceMCP`. Load-bearing assertion that Layer A is untouched.
- Cancellation: `Run` already handles `ctx.Done()` → SIGTERM → grace → SIGKILL on the process group. Bypass inherits this.

**Patterns to follow:**
- `internal/exec/orchestrate.go:102-225` — `Orchestrate` flow as the reference; bypass is the same shape minus the gate steps.
- `internal/cli/exec_characterization_test.go:16-117` — characterization-test pattern.
- MCP plan U2's AST analyzer pattern (referenced in research output) for `Record` literal construction sanctioning.

**Test scenarios:**
- Happy path: `OrchestrateHook(ctx, rt, "echo hi", opts)` runs `bash -c "echo hi"`, returns exit 0, captures `"hi\n"` to `opts.Stdout` (or its masked form if `hi` happens to match a sentinel — generally won't).
- Happy path: `OrchestrateHook(ctx, rt, "cat /etc/hosts", opts)` succeeds even though the regular `Orchestrate` would deny `cat` at `read-only` scope.
- Happy path: sentinel resolution — given a stored mapping `<<OPSMASK:NS:prod>>` → `production`, `OrchestrateHook(ctx, rt, "kubectl get pods --namespace <<OPSMASK:NS:prod>>", opts)` resolves the sentinel before bash runs; bash sees `kubectl get pods --namespace production`. Verifies sentinel-as-credential workflow is preserved.
- Edge case: empty command string → returns `ErrUsage` (or equivalent) cleanly without spawning anything.
- Edge case: command produces binary output → engine masks via `[REDACTED_BINARY]` per `internal/ioutil.ReplaceBinaryRuns`.
- Edge case: command emits >4095-byte argv (impossible since `command` is a single string; but the audit record's `Argv[2]` truncation is exercised) → `Truncated: true` in the record.
- Edge case: env-marker injection — child process `cmd.Env` contains `OPSMASK_EXEC_CHILD=1` after `BuildEnv` returns plus the explicit injection. Verified by spawning a Go-test subprocess that prints its env to stdout.
- Error path: subprocess crashes with non-zero exit → caller sees the exit code, audit record records `ExitCode: <n>`.
- Error path: audit log preflight fails (audit dir is `0500`) → `ErrAuditUnwritable`, no spawn.
- Error path: ctx cancelled mid-stream → child receives SIGTERM, then SIGKILL after grace; caller returns ctx.Err(); audit record records `DurationMs` of actual run.
- Error path: `Resolve` failure (sentinel references missing index) → returns the same error class the regular path returns; no spawn.
- Integration: `OrchestrateHook(..., "cat /var/log/app.log | grep error", ...)` produces masked output for the full pipeline. Covers AE1.
- Integration: `OrchestrateHook(..., "echo $PATH", ...)` — verify the freeform-scope env passes PATH through (vs read-only baseline).
- **Regression:** `Orchestrate(..., argv: ["bash", "-c", "true"], OrchestrateOptions{Source: SourceCLI})` returns `ErrPolicyDenied` with `DenyMatch: "bash"`. Same with `SourceMCP`. **Critical** — failure here means Layer A regressed.
- AST drift: `git grep -E 'exec\.Record\{' internal/cchook/` returns nothing — `internal/cchook/` uses `exec.NewRecord(exec.SourceHook)` exclusively. Any drift would cause the existing `TestRecordLiteralASTDriftBlocksExternalConstruction` to fail.

**Verification:**
- `go test ./internal/exec/... -race` passes including the regression on Layer A.
- The AST drift test sanctions `internal/exec/orchestrate_hook.go` and `internal/cchook/` and rejects new construction sites elsewhere.
- `grep -n SourceHook internal/exec/auditlog.go` shows the constant exported and the validator updated.

---

- U3. **Skip-list and tokenizer (`internal/cchook/`)**

**Goal:** Define the v0 skip-list contents and the matching rule (env-prefix stripping + metacharacter check). Provide a pure-function entry point the hook handler in U4 calls.

**Requirements:** R3, R5, R15

**Dependencies:** None (pure tokenization; no exec or audit dependencies).

**Files:**
- Create: `internal/cchook/skiplist.go` — exports `SkipList()` (the hardcoded list as a `map[string]struct{}` for O(1) lookup).
- Create: `internal/cchook/tokenize.go` — exports `Match(command string) (skip bool, verb string)` — the tokenizer + check.
- Test: `internal/cchook/tokenize_test.go` — table-driven scenarios.
- Test: `internal/cchook/skiplist_test.go` — assert the v0 contents (as a regression against accidental edits).

**Approach:**
- Skip-list contents (v0), each with a per-verb argument predicate:
  - Trivial filesystem-state (any args allowed):
    - `ls`, `pwd`, `cd`, `true`, `false`
  - `git` — match only when argv is exactly `git status` plus non-data-bearing flags from a small allow-list. Allowed bare flags: `--short`, `-s`, `-b`, `--branch`, `--ahead-behind`, `--no-renames`. Allowed value-flags: `--porcelain` (with optional `=<format>` value, where `<format>` is one of `v1`, `v2`; bare `--porcelain` is also allowed). Any other `git` subcommand, unrecognized flag, or unrecognized porcelain value → wrap. Closes codex Round 7 W1 (porcelain-value ambiguity).
  - Interactive TTY (**bare invocation or read-only flag only** — closes the file-read bypass surface):
    - `vim`, `vi`, `nvim` — match only with zero arguments OR a single `-R` flag. `vim -c '...'`, `vim file.txt`, `vim -e '...'` all wrap.
    - `nano`, `less`, `more`, `man`, `top`, `htop` — match only with zero arguments. `less file.txt`, `man secret-doc` wrap.
  - OpsMask itself: `opsmask` (any args), **but only when argv[0] resolves to the same binary as `os.Executable()`**. The matcher: tokenize → `exec.LookPath(argv[0])` → `filepath.EvalSymlinks(...)` → compare to `filepath.EvalSymlinks(os.Executable())`. Match only on realpath equality. A PATH-shadowed `opsmask` (malicious project's `bin/opsmask`) does NOT match — it wraps. Closes codex F3. R15.
- Tokenization rule (no external dependency — pure-Go inline splitter, ~30-40 lines):
  1. Bail out (no match) if the raw command string contains any of: `|`, `&`, `;`, `$(`, `` ` ``, `>`, `<`, `>>` — return `skip=false`.
  2. Split on ASCII whitespace (`strings.Fields`-style) with quote handling: a single-quoted or double-quoted span counts as one token. Unmatched quote → return `skip=false` (conservative).
  3. Strip leading env prefixes from a **narrow allow-list only**, with both name AND value validated. The name must match `^LC_[A-Z0-9_]*=` (locale variants) or exactly `LANG=`. The value (everything after `=` until next whitespace, after quote handling) must additionally match `^[A-Za-z0-9_.@-]+$` — the conservative locale-name grammar. **Values containing `:`, `/`, `\`, whitespace, quotes, `$`, backticks, or any shell metacharacter force `skip=false` (wrap)** — this defends against locale-aware library loading exploits where a value like `LC_ALL=en_US.UTF-8:/tmp/evil` passes a name-only check but the value carries a payload (closes codex Round 3 A2). **Any other env assignment — especially `PATH=...`, `SHELL=...`, `IFS=...`, `BASH_ENV=...`, `LD_*=...`, `DYLD_*=...` — forces `skip=false` (wrap)** regardless of value. The hook process resolves `argv[0]` via its own PATH for the canonical-binary identity check, but bash then executes the command with the modified PATH from the env-prefix; allowing arbitrary `PATH=...` prefixes opens a TOCTOU bypass where the canonical check passes but bash runs a different binary. Closes codex F1 (env-prefix bypass).
  4. Check the first non-assignment token (the effective argv[0]) against the skip-list table. Apply the per-verb argument predicate.
  5. For the `opsmask` skip-list entry only, additionally apply the canonical-binary identity check (resolve via `exec.LookPath`, `EvalSymlinks`, compare to `EvalSymlinks(os.Executable())`).
  6. If matched, return `skip=true, verb=<name>`.
- Conservative bias: if the tokenizer cannot parse the command (malformed quote, unmatched paren detected during the metacharacter scan, ambiguous form), return `skip=false` (wrap is the safe outcome).
- **No external shell-parser dependency.** The plan explicitly does NOT use `mvdan.cc/sh` or `github.com/google/shlex` (neither is in `go.mod` today, and adding ~40k lines of code for a conservative under-approximator is disproportionate). The inline splitter is the right scope.
- Match is not full POSIX shell parsing — it's an under-approximation that errs toward wrapping. Any time we're not 100% sure the command is a bare skip-listed verb, we wrap.

**Patterns to follow:**
- `internal/exec/policy.go` — the existing argv normalization (path basename, etc.) for inspiration on the shape of pure-function checks.

**Test scenarios:**
- Happy path: `"ls"` → match, verb=`ls`.
- Happy path: `"ls /tmp"` → match.
- Happy path: `"git status"` → match.
- Happy path: `"git status --porcelain"` → match.
- Happy path: `"opsmask mask-text --help"` → match only when `exec.LookPath("opsmask")` resolves to the same binary as `os.Executable()` (canonical-path identity check). R15.
- Edge: PATH-shadowed `opsmask` (malicious project's `./bin/opsmask` shim earlier in PATH) → no match (binary identity differs from running opsmask). Wraps via the bypass path, where the registry check + HMAC then refuse the malicious project anyway. Closes codex F3.
- Happy path: `"pwd"`, `"cd /tmp"`, `"true"`, `"false"` → match.
- Edge: `"\\ls"` → no match (escape disqualifies argv[0] match).
- Edge: `"LC_ALL=C ls"` → match (allow-listed locale prefix stripped, value passes grammar).
- Edge: `"LC_ALL=C LANG=en_US.UTF-8 ls /tmp"` → match (multiple allow-listed prefixes; both values pass grammar).
- Edge: `"LC_ALL=en_US.UTF-8 ls"` → match (dot is in the locale-value grammar).
- Edge: `"LC_ALL=en_US@euro ls"` → match (`@` is in the locale-value grammar — locale modifiers).
- Edge: `"LC_ALL=en_US.UTF-8:/tmp/evil ls"` → no match (colon disqualifies — closes codex Round 3 A2 locale-value smuggling).
- Edge: `"LC_ALL=../etc ls"` → no match (slash disqualifies).
- Edge: `"LC_ALL=$(whoami) ls"` → no match (`$(` already disqualifies at metacharacter scan, before grammar check).
- Edge: `"LC_ALL=foo;bar ls"` → no match (`;` already disqualifies at metacharacter scan).
- Edge: `"LANG=en_US.UTF-8 ls"` → match.
- Edge: `"LANG=foo bar ls"` → no match (whitespace in unquoted value parses as separate tokens; second token `bar` is not allow-listed → wrap).
- Edge: `"LANG='foo bar' ls"` → no match (quoted value has whitespace; locale grammar rejects).
- Edge: `"PATH=./bin:$PATH opsmask mask-text"` → no match (PATH= is NOT allow-listed; env-prefix bypass closed). Closes codex F1.
- Edge: `"PATH=/tmp ls"` → no match (PATH= forces wrap regardless of argv[0]).
- Edge: `"BASH_ENV=/tmp/x bash -c true"` → no match (BASH_ENV= forces wrap; defense in depth).
- Edge: `"FOO=bar ls"` → no match (any non-allow-listed env assignment forces wrap).
- Edge: `"IFS=$'\\n' ls"` → no match (IFS= forces wrap).
- Edge: `"nice -n 10 ls"` → no match (nice not in skip-list).
- Edge: `"echo hi"` → no match (echo removed from skip-list per origin doc edits).
- Edge: `"test -f foo"` → no match (test removed).
- Edge: `"git diff"` → no match (git is conditional on `status`).
- Edge: `"git log -p"` → no match (git is conditional on `status`).
- Edge: `"git status --porcelain=v2"` → match (porcelain is on the per-verb allow-list).
- Edge: `"git status -uall"` → no match (`-uall` not on the per-verb allow-list).
- Edge: `"vim"` → match (bare invocation).
- Edge: `"vim -R"` → match (read-only flag).
- Edge: `"vim file.txt"` → no match (file argument disqualifies — closes file-read bypass).
- Edge: `"vim -c 'read /etc/passwd' -c 'w /tmp/out' -c 'q'"` → no match (`-c` flag disqualifies — closes the doc-review-flagged bypass).
- Edge: `"less file.txt"` → no match (less requires bare invocation).
- Edge: `"man secret-doc"` → no match (man requires bare invocation).
- Edge: `""` → no match (empty command).
- Edge: `"   "` → no match (whitespace-only).
- Edge: `"ls; cat secret"` → no match (separator `;` disqualifies).
- Edge: `"ls | wc -l"` → no match (pipe disqualifies).
- Edge: `"ls > out.txt"` → no match (redirect disqualifies).
- Edge: `"ls && pwd"` → no match (`&&` includes `&`).
- Edge: `"echo $(cat /etc/passwd)"` → no match (subshell disqualifies, also echo not in list).
- Edge: `"ls `cat foo`"` → no match (backtick subshell).
- Edge: `"ls $HOME"` → match (`$` without `(` is a variable expansion; the metacharacter rule rejects `$(` specifically, allowing bare `$VAR` since the existing CLI/MCP `exec` resolves these without metachar concerns).
- Edge: `'malformed quote string` (unclosed quote) → no match (tokenizer error → conservative).
- Covers AE2: `"ls"` → skip-list match, verifies with U4 that no masking subprocess is spawned.
- Covers AE8 (partial): `"opsmask mask-text --help"` → skip-list match.

**Verification:**
- `go test ./internal/cchook/... -race` passes all scenarios.
- The skip-list contents test fails if anyone adds `echo` / `test` / `[` back without explicit comment + sign-off.

---

- U4. **Hidden subcommands `claude-code-hook` (JSON event handler) + `claude-code-exec` (HMAC-signed bypass entry point)**

**Goal:** Two new hidden cobra subcommands. `claude-code-hook` is the PreToolUse JSON event handler the project shim execs. `claude-code-exec` is the HMAC-gated bypass-execution entry point that Claude Code re-issues via `updatedInput`. Together they preserve masked streaming, audit logging, and process control while intentionally bypassing the `EvaluatePolicy` / trust / `Cfg.Enabled` gates for opted-in projects (see Key Technical Decisions for the two-mode framing).

**Requirements:** R1, R3, R6, R7, R15, R17

**Dependencies:** U1, U2, U3.

**Files:**
- Create: `internal/cchook/handler.go` — exports `Handle(in io.Reader, out io.Writer, env Env) error` (testable; no cobra dependency). `Env` carries the runtime, secret loader, and pass-through audit writer.
- Create: `internal/cchook/exec.go` — exports `RunWrapped(ctx, rt, sig, command string, opts) (HookResult, error)` — the entry point `claude-code-exec` calls. Verifies the HMAC, then dispatches to `internal/exec.OrchestrateHook` (U2). Refuses with a clear error on mismatch.
- Create: `internal/cchook/secret.go` — exports `LoadSecret() ([]byte, error)` and `EnsureSecret() error`. Reads `~/.config/opsmask/hook_secret` only (no `OPSMASK_AUDIT_DIR` or other env override — the secret is in the user-config namespace, not the audit namespace). On load, verify (a) file mode is exactly 0600, (b) file owner UID equals current process UID. Refuse on either violation. Generates 32 random bytes on first install if missing. Used by both the handler (to sign) and `RunWrapped` (to verify).
- Create: `internal/install/registry.go` — exports `RegisterInstall(projectToplevel string) error`, `Unregister(projectToplevel string) error`, `IsRegistered(projectToplevel string) (bool, error)`, `ResolveProjectToplevel(cwd string) (string, error)`, and the exported sentinel `var ErrNoGitToplevel = errors.New("opsmask: not inside a git project")`. `ResolveProjectToplevel` calls `git rev-parse --show-toplevel`; on failure (no git repo) it wraps the underlying error with `%w` and `ErrNoGitToplevel` so callers use `errors.Is(err, ErrNoGitToplevel)` to branch — **no cwd fallback**. On success it returns `filepath.EvalSymlinks(toplevel)`. Reads/writes `~/.config/opsmask/hook_installs.json` (mode 0600, owner-checked) which lists project toplevels (realpaths). **Atomic write contract:** every write goes through tempfile-in-same-dir + `fsync(file)` + `chmod 0600` + owner-UID re-check + `os.Rename` (atomic on POSIX same-filesystem). Plain `os.WriteFile` is forbidden in this file — closes codex Round 3 C1 (registry torn-write). **Concurrent-write contract:** load-modify-rename is wrapped in an advisory file lock on `~/.config/opsmask/hook_installs.json.lock` (acquired before load, released after rename) so two concurrent `RegisterInstall`/`Unregister` calls cannot lose updates. Closes codex Round 4 R4-3. Used by U5 (installer) to register/unregister and by U4 (hook handler + claude-code-exec) to gate execution.
- Create: `internal/cchook/handler_test.go`, `internal/cchook/exec_test.go`, `internal/cchook/secret_test.go`.
- Create: `internal/cli/claude_code_hook.go` — cobra wiring (`Hidden: true`, `RunE` calls `cchook.Handle(os.Stdin, os.Stdout, ...)`).
- Create: `internal/cli/claude_code_exec.go` — cobra wiring (`Hidden: true`, `RunE` parses `--sig` flag and trailing `-- <command>`, calls `cchook.RunWrapped(...)`).
- Modify: `internal/cli/root.go:48-62` — add `newClaudeCodeHook(opts)` and `newClaudeCodeExec(opts)` to `AddCommand`.
- Modify: `internal/cli/root.go:65` — add `claude-code-hook` and `claude-code-exec` to the `RewriteArgs` known map.
- Test: `internal/cli/claude_code_hook_test.go`, `internal/cli/claude_code_exec_test.go` — CLI integration tests via `executeCLI(t, args, stdin)`.

**Approach (handler):**
- JSON envelope (verified at implementation time against `code.claude.com/docs/en/hooks`):
  - Input: `{tool_name: "Bash", tool_input: {command: "<str>", ...}}`. Other tools → return empty envelope.
  - Output (skip / pass-through): `{}` or `{continue: true}`.
  - Output (wrap): `{updatedInput: {command: "opsmask claude-code-exec --sig <hex> -- <command>"}}` — the `--sig` value is the HMAC-SHA256 of the command computed against the project's loaded `hook_secret`. Exact envelope shape verified against current Claude Code docs at implementation time.
  - Output (fail-closed): `{continue: false, stopReason: "<diagnostic>"}`.
- Decision tree (in order):
  1. Decode stdin JSON. On error → fail-closed envelope with diagnostic + exit 0 (so Claude Code sees the response).
  2. If `tool_name != "Bash"` → empty envelope. (Defense in depth — the hook config should only target Bash.)
  3. If `IsExecChild()` (U1) → empty envelope. Secondary recursion guard for grandchild scenarios.
  4. **Project registry check.** Compute `toplevel = install.ResolveProjectToplevel(os.Getwd())` (git-toplevel realpath). If `errors.Is(err, install.ErrNoGitToplevel)` → fail-closed envelope: "OpsMask hook requires a git project. The current directory is not inside a git repository. Refusing." On any other resolve error → fail-closed envelope with the diagnostic. **Sentinel contract (closes codex Round 6 W6-2).** All callers outside `internal/install` MUST reference the package-qualified exported sentinel `install.ErrNoGitToplevel` and branch via `errors.Is` — never `==` (breaks across wraps) and never a locally redeclared error value. If `IsRegistered(toplevel)` is false → fail-closed envelope: "OpsMask hook fired in a project that was not opted in via `opsmask install claude-code`. Refusing." This blocks the malicious-`.claude/`-in-cloned-repo attack (codex F5) and handles Claude Code sessions launched from subdirectories of a registered project (codex F2). No-git fail-closed closes codex Round 3 B1.
  5. If `runtime.New(...)` fails → fail-closed envelope with a **distinct diagnostic** ("OpsMask hook is enabled but the project is not initialized. Run `opsmask init` in this project."). Different from binary-missing and registry-missing diagnostics.
  6. Run `cchook.Match(command)` (U3) — the canonical-binary identity check governs the `opsmask` skip-list match.
  7. On match: write `Source=hook` audit record to `pass_through.log` (R17 — separate file from `exec.log`, see Key Technical Decisions). Record stores the **unresolved** command (sentinel placeholders preserved). Return empty envelope.
  8. On no-match: load `hook_secret`, compute `toplevel = ResolveProjectToplevel(os.Getwd())` (already done in step 4 — reuse), compute `sig = HMAC-SHA256(secret, toplevel + "\x00" + command)` (binding to project toplevel, NOT raw cwd, so a subdirectory invocation under the same registered project produces the same sig), return wrap envelope with `command = "<canonical-opsmask-path> claude-code-exec --sig " + hexsig + " -- " + shellQuote(orig)` where `<canonical-opsmask-path>` is `filepath.EvalSymlinks(os.Executable())` so the rewrite identifies the binary by realpath.
- Fail-closed conditions: malformed JSON, missing `command` field, registry-not-found, runtime-init failure (each with a distinct diagnostic), audit-write failure on pass-through (R6), secret-load failure on wrap.
- `claude-code-hook` is `Hidden: true`.

**Approach (claude-code-exec):**
- Flag: `--sig <hex>` (required). Trailing positional after `--`: the command string.
- Steps:
  1. Resolve `toplevel = install.ResolveProjectToplevel(os.Getwd())` (git-toplevel realpath). If `errors.Is(err, install.ErrNoGitToplevel)` → exit non-zero with: "opsmask claude-code-exec: requires a git project. Refusing." On any other resolve error → exit non-zero with the diagnostic. (Same sentinel contract as the handler — package-qualified, `errors.Is`-only.)
  2. **Project registry check.** If `IsRegistered(toplevel)` is false → exit non-zero with: "opsmask claude-code-exec: this project is not opted in to the OpsMask hook. Refusing." Closes codex F5 at this entry point too (defense in depth alongside the handler's check at step 4).
  3. Load `hook_secret`. Refuse with clear error if missing (signals install mismatch).
  4. Compute `expected = HMAC-SHA256(secret, toplevel + "\x00" + command)`. Compare with `--sig` using `hmac.Equal` (constant-time).
  5. On mismatch → exit non-zero with: `"opsmask claude-code-exec: signature mismatch. This subcommand is reachable only through the Claude Code hook chain — direct invocation is not supported."`
  6. On match → call `cchook.RunWrapped(ctx, rt, command, opts)` which dispatches to `OrchestrateHook`. The hook orchestrator audits the **unresolved** command (sentinel placeholders preserved), then `Resolve`s, then runs bash. Streams output to `os.Stdout` / `os.Stderr`.
- `claude-code-exec` is `Hidden: true`. The registry check + HMAC + cwd binding together preserve masked streaming, audit, and process control while allowing a registered, HMAC-signed hook bypass — they do NOT preserve `EvaluatePolicy` / trust / enabled gates (those are intentionally skipped; see Key Technical Decisions for the two-mode framing). The Hidden flag is defense in depth (out of `--help`).
- Recovery if the secret is somehow lost (e.g., `~/.config/opsmask/hook_secret` deleted): the hook handler's wrap envelope will use a freshly-generated secret while `claude-code-exec` reads the (now-different) secret on disk → sig mismatch → user sees the diagnostic. They run `opsmask install claude-code` again to regenerate alignment. Documented in U7 / REMAINING_RISKS.

**Patterns to follow:**
- `internal/cli/mcp.go` for the cobra `RunE` shape.
- `internal/mcpsrv/` for JSON-over-stdio handling patterns.

**Test scenarios (handler):**
- Happy path: input `{tool_name:"Bash", tool_input:{command:"ls"}}` → output `{}` (or `{continue:true}`); pass-through audit at `pass_through.log` has one new `Source=hook` record with `Argv=["ls"]`. Covers AE2.
- Happy path: input `{tool_name:"Bash", tool_input:{command:"cat /var/log/app.log | grep error"}}` → output contains `updatedInput.command` rewriting to `opsmask claude-code-exec --sig <hex> -- 'cat /var/log/app.log | grep error'`. The `--sig` value validates against the loaded `hook_secret`. Covers AE1 (the wrap signal).
- Happy path: `tool_name:"Read"` → empty envelope (not Bash, pass-through).
- Happy path: `OPSMASK_EXEC_CHILD=1` set → empty envelope regardless of command. Covers AE8 secondary mechanism.
- Happy path: `cchook.Match` returns skip on `opsmask <anything>` → empty envelope. Covers AE8 primary mechanism (skip-list match on `opsmask`).
- Edge: missing `tool_input.command` field → fail-closed envelope with diagnostic.
- Edge: malformed JSON on stdin → fail-closed envelope with diagnostic.
- Edge: `tool_name` field absent → fail-closed envelope.
- Edge: stdin is empty → fail-closed envelope.
- Edge: command containing single quotes / shell-special chars / newlines is properly shell-quoted in `updatedInput.command` such that `claude-code-exec`'s `-- <command>` parses as a single argument.
- Edge: runtime-init failure (no mapping store) → fail-closed envelope with the **distinct diagnostic** naming `opsmask init` as the fix. Verify the message text differs from the binary-missing diagnostic.
- Edge: secret-load failure (file deleted) → fail-closed envelope with the diagnostic naming `opsmask install claude-code` as the fix.
- Error path: audit-log preflight fails (dir is `0500`) → fail-closed envelope (R6 invariant).
- Integration: `executeCLI(t, []string{"claude-code-hook"}, jsonInput)` round-trips correctly.

**Test scenarios (claude-code-exec):**
- Happy path: valid sig (computed with cwd binding) + matching command + cwd registered → spawns bash via `OrchestrateHook`, masked output reaches stdout, exit code 0.
- Happy path: command with sentinel `<<OPSMASK:...>>` → audited unresolved (sentinel preserved in `exec.log`), then resolved before bash runs.
- Edge: malformed `--sig` (not hex, wrong length) → exit non-zero with sig-mismatch diagnostic.
- Edge: missing `--sig` flag → exit 2 with `UsageError`.
- Edge: missing trailing `--` separator → exit 2 with `UsageError`.
- **Security regression — direct invocation:** call `claude-code-exec --sig 0000... -- 'rm -rf $HOME'` directly from a registered project (no hook chain, attacker chose the sig) → exits non-zero with sig-mismatch diagnostic. **Critical** — failure here means a user can bypass `config trust` from the CLI.
- **Security regression — wrong cwd:** generate a valid sig in `/project-A`, then run `claude-code-exec --sig <hex> -- '<cmd>'` from `/project-B` → exits non-zero with sig-mismatch diagnostic (cwd binding rejects replay across projects). Closes codex F1 same-project replay.
- **Security regression — unregistered project:** call `claude-code-exec` from a project NOT in `hook_installs.json` (with any sig) → exits non-zero with the registry-not-found diagnostic. Closes codex F5.
- **Security regression — wrong secret:** generate sig with secret-A, replace `~/.config/opsmask/hook_secret` with secret-B, then invoke → exits non-zero with sig-mismatch. Constant-time compare verified by mutation testing (flip one bit at a time and assert all positions reject).
- **Security regression — secret file integrity:** mode 0644 on `hook_secret` → `LoadSecret()` refuses; subcommand exits non-zero with mode-violation diagnostic. Wrong owner UID → same.
- Integration: full hook chain — handler emits wrap envelope (with cwd-bound sig and canonical opsmask path) → simulated Claude Code re-issue invokes `claude-code-exec` → bash runs → masked output captured.

**Test scenarios (secret):**
- Happy path: `EnsureSecret()` on a clean machine creates `~/.config/opsmask/hook_secret` with 32 random bytes, mode 0600, owner = current UID.
- Happy path: `LoadSecret()` returns the bytes after `EnsureSecret()` ran.
- Edge: parent directory doesn't exist → `EnsureSecret()` creates `~/.config/opsmask/` mode 0700.
- Edge: secret file mode is wider than 0600 (e.g., 0644) → `LoadSecret()` refuses with a clear error.
- Edge: secret file owner UID differs from current UID → `LoadSecret()` refuses with a clear error (defense against shared-machine misconfiguration).
- **Security regression:** `OPSMASK_AUDIT_DIR` set to point at a different location → secret loader IGNORES it. The secret is in the user-config namespace, not the audit namespace; closes codex F2 (env-redirected secret store).

**Test scenarios (registry):**
- Happy path: `RegisterInstall("/path/to/project")` writes `~/.config/opsmask/hook_installs.json` (mode 0600) via tempfile + fsync + rename. `IsRegistered("/path/to/project")` returns true; `IsRegistered("/other/path")` returns false.
- Happy path: registering then `Unregister(...)`'ing leaves the file with the entry removed (other entries preserved). Unregister also goes through tempfile + fsync + rename.
- Happy path: `RegisterInstall(...)` with a path that resolves to the same realpath as an existing entry (e.g., via symlink) → no duplicate; idempotent.
- Edge: registry file mode wider than 0600 → load refuses with a clear error.
- Edge: registry file doesn't exist (first install) → `RegisterInstall(...)` creates it with mode 0600.
- Edge: registry JSON corruption → load refuses (do not auto-recover; force user to inspect).
- Edge: `Unregister(...)` on a path not in the registry → no-op (idempotent), no error.
- **Atomic write — TestRegistry_PartialWrite_LeavesOriginalIntact (codex Round 3 C1):** simulate a write failure between tempfile-write and rename (e.g., disk-full on the tempfile, or a synthetic error injected before rename). Verify post-state: original file unchanged (or absent if first install), no `.tmp` orphans on success, `.tmp` cleaned up by deferred `os.Remove` on failure. Repeat for `Unregister` mid-write failure.
- **Atomic write — TestRegistry_OwnerUIDReverify:** after the tempfile is fsync'd but before rename, the owner UID re-check fires (defense against a TOCTOU race on `~/.config/opsmask/`). Mock `os.Stat` to return a different UID → write aborts, original file untouched, tempfile removed.
- **Concurrent write — TestRegistry_ConcurrentRegister_NoLostUpdate (codex Round 4 R4-3):** spawn two goroutines that call `RegisterInstall("/path/A")` and `RegisterInstall("/path/B")` simultaneously (`-race` flag). After both return, the registry contains BOTH paths. Without the advisory lock, one update would clobber the other. Repeat for concurrent `Register` + `Unregister`, and for two concurrent installs from different processes (use `t.Cleanup` and a shared lockfile path).
- **Concurrent write — TestRegistry_LockfileLeak (closes codex Round 5 W-2):** spawn a **subprocess** (`os/exec.Command(os.Args[0], "-test.run=TestRegistryLockSubprocess")` with an env marker) that acquires the advisory lock and `os.Exit(1)`s while holding it (or `syscall.Kill(os.Getpid(), syscall.SIGKILL)` to model a hard crash without graceful shutdown). The parent then attempts `RegisterInstall` and asserts it acquires the lock and completes within a bounded timeout (e.g., 5s). This actually exercises kernel FD-close cleanup across processes; a recovered goroutine panic would not, and an unrecovered one would terminate the test process. Documented in the test as the canonical form because the goroutine-panic model does not test the right thing.
- **`ResolveProjectToplevel` no-git error (codex Round 3 B1):** call from a tempdir with no `.git/` → returns `ErrNoGitToplevel`, NOT a cwd-realpath fallback.
- **`ResolveProjectToplevel` git worktree:** call from a worktree → returns the worktree's realpath (its own toplevel), NOT the main checkout.
- **`ResolveProjectToplevel` submodule:** call from inside a submodule → returns the submodule realpath (innermost), NOT the outer repo.
- **`ResolveProjectToplevel` symlinked path:** call from a symlinked project root → returns the realpath via `EvalSymlinks`.

**Verification:**
- `go test ./internal/cchook/... ./internal/cli/... -race` passes.
- `opsmask --help` does not list `claude-code-hook` or `claude-code-exec` (Hidden flag).
- `opsmask exec --help` does NOT show a `--shell` flag — the bypass path is not on `exec` at all.
- Manual smoke test: `echo '{"tool_name":"Bash","tool_input":{"command":"ls"}}' | opsmask claude-code-hook` returns `{}` and writes one record to `pass_through.log` (not `exec.log`).
- Manual security test: `opsmask claude-code-exec --sig deadbeef -- 'echo OWNED'` exits non-zero with sig-mismatch error and does not run the command.

---

- U5. **Installer core (`internal/install/`)**

**Goal:** Library functions that the CLI subcommand wraps: project detection, settings-file detection, hook-block merge/diff, shim writer, idempotency check, opsmask-binary verification.

**Requirements:** R8, R9, R10, R11, R12, R16

**Dependencies:** None (pure library; tests use `t.TempDir`).

**Files:**
- Create: `internal/install/install.go` — exports `Install(opts Options) (Result, error)` and `Uninstall(opts Options) (Result, error)`.
- Create: `internal/install/shim.go` — exports `WriteShim(path string) error` and the shim-script template.
- Create: `internal/install/detect.go` — exports `DetectProject(cwd string) (root string, err error)`, `DetectSettings(projectRoot string) (Detection, error)`.
- Test: `internal/install/install_test.go` — comprehensive table-driven tests against tempdirs.
- Test: `internal/install/shim_test.go` — verify shim contents and executability.
- Test: `internal/install/detect_test.go` — project detection edge cases.

**Approach:**
- Project detection (R9): the installer calls `install.ResolveProjectToplevel(cwd)`. If `errors.Is(err, install.ErrNoGitToplevel)` → reject with: "OpsMask hook requires a git project. Run `git init` in this project, then re-run install." Compare the returned toplevel to `os.UserHomeDir()`; if equal → reject with: "Refusing to install at $HOME — run `opsmask install claude-code` from inside a project." On any other error from `ResolveProjectToplevel` → propagate the diagnostic. No fallback to cwd. The `errors.Is` form is mandatory across package boundaries — a `==` comparison breaks if the error is wrapped. This aligns install-time and runtime non-git behavior on a single rule (closes codex Round 3 B1; closes codex Round 5 W-3).
- **Pre-install verification (R11 + new prerequisites):** before any file write, the installer runs four checks (registry write happens LAST, after success — see "Install ordering and rollback" below):
  1. `exec.LookPath("opsmask")` — binary on PATH.
  2. `runtime.New(rt.Options{ProjectRoot: <detected>})` — verifies the mapping store, secret file, and detector rules can be loaded. **If this fails, refuse with the actionable error: "OpsMask is installed but the project is not initialized. Run `opsmask init` in this project, then re-run install."** Closes the doc-review finding that an uninitialized project would silently fail-closed on every Bash call.
  3. `cchook.EnsureSecret()` — generates `~/.config/opsmask/hook_secret` if it does not exist (32 random bytes, mode 0600, owner-checked on every load). The secret is per-machine, not per-project; failures here abort install.
  4. Settings-file detection scan: both `<root>/.claude/settings.json` and `<root>/.claude/settings.local.json`. Parse each as JSON; look for an existing OpsMask hook block (sentinel: a stable `name: "opsmask"` field on the hook entry).
- **Install ordering and rollback (closes codex F3).** The installer commits state in this order — registry is the LAST step so a partial install never authorizes a project that doesn't have a working hook chain:
  1. Pre-install verification (above) — read-only checks; nothing on disk has changed yet.
  2. Resolve `toplevel = ResolveProjectToplevel(detected-project-root)` — the value that will be registered.
  3. Team-shared confirmation prompt (if `--team-shared` chosen and not `--yes`) — **if user declines, abort with no on-disk changes**.
  4. Multi-hook coexistence prompt (if existing PreToolUse Bash hook detected) — **if user picks "refuse to install", abort with no on-disk changes**.
  5. Write the shim atomically: tempfile → fsync → rename to `<project>/.claude/opsmask-hook.sh`. If write fails → abort, no further state changes.
  6. Write the settings-file hook block atomically (read existing JSON → modify in place → tempfile → fsync → rename to `<project>/.claude/settings.json` or `.local.json`). If write fails → roll back the shim from step 5 (delete it), then abort.
  7. **Only after steps 5 and 6 succeed, call `install.RegisterInstall(toplevel)`** to add the project to `~/.config/opsmask/hook_installs.json`. If this final step fails → roll back: delete the shim, restore the settings file from the pre-write copy preserved in memory, then surface the registry error.
  8. Print confirmation message (file paths, install mode, v0 limitation note).
- Idempotency (R10): if a hook block is found in either file, return `Result{Status: AlreadyInstalled, Path: <found-path>}`. Caller prints "already installed at <path>" and exits 0. The shim file is also re-checked for content drift; if it differs from the canonical version, the installer overwrites it and reports the refresh.
- Install (R10, R16): caller passes `Personal | TeamShared | Interactive`. For `Interactive` in a non-TTY → return error (caller surfaces guidance). For `Personal` → write to `.claude/settings.local.json`. For `TeamShared` → first print the teammate-DoS warning to stderr, prompt for confirmation (Enter), then write to `.claude/settings.json`. `--yes` flag bypasses the prompt for scripted installs. Either branch creates the `.claude/` dir if missing (mode 0755), creates the settings file if missing (mode 0644), merges the hook block under `hooks.PreToolUse[].matcher: "Bash"` plus a `hooks: [{type: "command", command: ".claude/opsmask-hook.sh"}]` entry (exact JSON shape verified against current Claude Code docs at implementation time).
- **Per-project shim location.** Shim writer (`shim.go`) writes the POSIX `sh` script to `<project>/.claude/opsmask-hook.sh` (NOT `~/.config/opsmask/hooks/`), mode `0755`. The trust radius is scoped to the project, matching `.opsmask/config.yaml`. Script template:
  ```sh
  #!/bin/sh
  set -e
  if ! command -v opsmask >/dev/null 2>&1; then
    printf '{"continue":false,"stopReason":"OpsMask hook is enabled in this project but `opsmask` is not on PATH. Install opsmask, or run `opsmask uninstall claude-code` in this project to disable."}\n'
    exit 2
  fi
  exec opsmask claude-code-hook "$@"
  ```
- The hook block in settings.json references the relative path `.claude/opsmask-hook.sh`. The installer prints both the settings-file path AND the shim path in confirmation, with a note that the user should add `.claude/opsmask-hook.sh` to `.gitignore` (personal install) or commit it (team-shared install) per their preference. The installer optionally appends to `.gitignore` itself for personal installs (with an explicit user prompt).
- **Multi-hook coexistence:** if the settings file already contains a `PreToolUse` hook on `Bash` from another tool, the installer warns and asks the user whether to (a) chain alongside the existing hook (default — Claude Code runs both in array order; OpsMask appends), (b) refuse to install (recommend resolving first), or (c) overwrite. Behavior on chain interaction (whether the second hook sees `updatedInput` from the first) is documented as a known interaction in REMAINING_RISKS — the user accepts the chain semantics by choosing (a). Closes the doc-review concern about multi-hook chains.
- Uninstall (R12, AE6): scan both settings files, find the hook block by sentinel, remove it, preserve unrelated keys and array order with semantic equivalence (object content, array order, and unrelated keys preserved; whitespace and JSON object key order may normalize through the load → modify → marshal cycle). **Also remove `<project>/.claude/opsmask-hook.sh`** so uninstall is fully symmetric (closes the doc-review residue concern). **Also resolve `toplevel = ResolveProjectToplevel(cwd)` and call `install.Unregister(toplevel)`** so the unregister key matches the install key (closes codex Round 4 R4-2). If `errors.Is(err, install.ErrNoGitToplevel)` → refuse with the same no-git diagnostic as install (closes codex Round 5 W-3). The `errors.Is` form is mandatory; do not compare with `==` since the error may be wrapped across package boundaries. The `~/.config/opsmask/hook_secret` is NOT removed by uninstall (it is per-machine, shared across projects, and harmless if left in place; removing it would force regeneration on the next install which is fine but unnecessary).
- Atomic write: settings, shim, and registry writes all go through `WriteFileAtomic` (defined above). No direct-write fallback. Closes codex Round 4 R4-5.

**Patterns to follow:**
- `internal/exec/auditlog.go:117-149` for `os.UserConfigDir()` + audit dir creation pattern (mode 0700; the shim dir uses 0755 since it must be readable+executable by the shim runner).
- **New atomic-write helper `internal/install/atomic.go`** (closes codex Round 4 R4-5): exports `WriteFileAtomic(path string, data []byte, mode os.FileMode) error` that performs same-directory `os.CreateTemp` + write + `f.Sync()` + `f.Chmod(mode)` + owner-UID re-check + `os.Rename`. On any error before rename, the helper deletes the tempfile via `defer os.Remove`. **Orphan-tempfile policy (closes codex Round 5 W-1).** A SIGKILL between tempfile-create and rename leaves an orphan that `defer` cannot clean up. The helper uses a stable prefix convention `.opsmask-atomic-*.tmp` so orphans are distinguishable. On every `RegisterInstall`, `Unregister`, and install-time settings/shim write, the caller first sweeps any matching orphans older than 60 seconds in the same directory (a startup-style cleanup, embedded into the helper itself: `WriteFileAtomic` runs the sweep before creating a new tempfile). Orphans within the 60-second window are left alone (might be a concurrent in-progress write protected by the advisory lock or another caller's transient state). The 60-second floor is documented as harmless: the orphan content is internal-only, mode 0600 / 0644 / 0755 per file class, and never readable by another user. **TOCTOU contract (closes codex Round 6 W6-1).** A sweep that fires while another writer is past the 60-second floor but still pre-rename can race and unlink that writer's tempfile. The contract: writers must treat `os.Rename → ENOENT` as a write failure (the tempfile vanished under them), leave the original target file intact, and surface the error so the caller invokes its normal rollback path (e.g., the install rollback chain in U5). Sweepers must treat `os.Remove → ENOENT` as a no-op (two sweepers racing to delete the same orphan is harmless). The advisory lock on `~/.config/opsmask/hook_installs.json.lock` only covers registry writes — settings and shim writes do not share a lock and therefore rely on this rename-ENOENT contract for cross-process safety. **No cross-filesystem fallback to direct `os.WriteFile`** — the registry, settings, and shim files all live under `<project>/.claude/` or `~/.config/opsmask/`, both of which are guaranteed to be on a single filesystem; cross-filesystem rename is not a real concern here, and the fallback would silently break atomicity. `internal/config/trust.go`'s plain `os.WriteFile` is **NOT** the pattern to copy. Tested by `TestWriteFileAtomic_*` (happy path, owner-mismatch, mid-write failure, mode enforcement, orphan-sweep-on-rewrite).

**Test scenarios:**
- Happy path: `Install(Options{Mode: Personal, Cwd: tempProject})` writes `<tempProject>/.claude/settings.local.json` with a valid hook block; the shim is at `<tempProject>/.claude/opsmask-hook.sh` and is executable; the project's realpath is added to `~/.config/opsmask/hook_installs.json`; the hook secret is generated at `~/.config/opsmask/hook_secret`.
- Happy path: `Install(Options{Mode: TeamShared, Cwd: tempProject, Yes: true})` writes to `<tempProject>/.claude/settings.json`. With `Yes: false` plus a non-TTY → returns "confirmation required" error.
- Happy path: team-shared install confirmation message contains the teammate-DoS warning text. Verified by inspecting the captured stderr output.
- Happy path: re-running install (any mode) returns `AlreadyInstalled` with the file path; no settings-file changes; shim file is checked for drift and refreshed if needed.
- Happy path: `Uninstall(...)` removes the hook block AND `<project>/.claude/opsmask-hook.sh` AND the project's git-toplevel realpath from `~/.config/opsmask/hook_installs.json`. Preserves other settings keys (semantic equivalence — content, array order, unrelated keys; whitespace and key order may normalize). Covers AE6.
- **Uninstall from subdirectory (codex Round 4 R4-2):** install at `/tmp/proj` (its toplevel), then run `Uninstall` from `/tmp/proj/sub/dir`. Verify post-state: hook block removed from settings, shim removed, AND `/tmp/proj` (toplevel realpath) is removed from the registry — NOT just `/tmp/proj/sub/dir`. Critical regression: failure here means the project remains registered after uninstall when invoked from a subdirectory.
- **Security regression — malicious project repro:** simulate a cloned repo with `.claude/settings.json` + `.claude/opsmask-hook.sh` already committed by an attacker (project NOT registered). Spawn `opsmask claude-code-hook` with a crafted JSON event from that cwd → fail-closed envelope referencing the registry-not-found diagnostic. **Critical** — failure here means the malicious-project-clones-OpsMask attack succeeds. Closes codex F5.
- **Rollback — TestInstall_ShimWriteFails_NoStateChange:** mock the shim-write step (step 5) to fail (e.g., readonly filesystem on `.claude/`). Verify post-state: no shim, settings file unchanged, registry does NOT contain the project, install exits non-zero with the shim-write error.
- **Rollback — TestInstall_SettingsWriteFails_RollsBackShim (codex Round 3 C2):** shim-write succeeds, settings-write fails mid-rename. Verify post-state: shim file does NOT exist (rolled back), settings file unchanged, registry does NOT contain the project, install exits non-zero with the settings-write error. Cleanup assertions explicit: `assert.NoFileExists(<project>/.claude/opsmask-hook.sh)`, `assert.JSONEqual(settingsBefore, settingsAfter)`, `assert.False(IsRegistered(toplevel))`.
- **Rollback — TestInstall_RegistryWriteFails_RollsBackShimAndSettings (codex Round 3 C2 + F3):** shim-write succeeds, settings-write succeeds, registry-write fails (e.g., simulate a permissions error on `~/.config/opsmask/`). Verify post-state: shim does NOT exist, settings file is restored to pre-write contents (from the in-memory snapshot), registry does NOT contain the project, install exits non-zero with the registry-write error. Cleanup assertions explicit: `assert.NoFileExists(...)`, `assert.JSONEqual(settingsBefore, settingsAfter)`, `assert.False(IsRegistered(toplevel))`. **Critical** — failure here means a project can end up registered without a working hook chain (or vice versa).
- **Atomic write — TestRegistry_PartialWrite_LeavesOriginalIntact (codex Round 3 C1):** simulate a write failure between tempfile-write and rename (e.g., disk-full on the tempfile, or a synthetic error injected before rename). Verify post-state: the original `~/.config/opsmask/hook_installs.json` is unchanged (or absent if first install), no `.tmp` left behind on success path, the half-written tempfile is cleaned up by deferred `os.Remove` on failure path. Repeat for `Unregister` mid-write failure.
- **Atomic write — TestRegistry_OwnerUIDReverify:** after the tempfile is fsync'd but before rename, verify the owner UID re-check fires (defense against TOCTOU on the config dir). Test by mocking `os.Stat` to return a different UID → write aborts, original file untouched.
- Subdirectory invocation: register a project at `/tmp/proj` (its git toplevel). Run the handler with `os.Getwd() == /tmp/proj/sub/dir`. The registry check resolves to `/tmp/proj` (git toplevel) and matches. Closes codex F2 (registry exact-cwd vs toplevel).
- **Non-git refusal at install (codex Round 3 B1):** install in a tempdir with no `.git/` → installer refuses with the no-git diagnostic ("requires a git project. Run `git init`..."). No shim, no settings, no registry entry written.
- **Non-git refusal at runtime:** spawn `opsmask claude-code-hook` from a tempdir with no `.git/` (with any registered project on the system) → fail-closed envelope with the no-git diagnostic. Spawn `opsmask claude-code-exec --sig <any> -- ls` from same → exit non-zero with the no-git diagnostic.
- **Git worktree:** create a project at `/tmp/main` with `.git`, then `git worktree add /tmp/wt branch-foo`. Install from `/tmp/main` → registers `/tmp/main` realpath. Install from `/tmp/wt` → `git rev-parse --show-toplevel` returns `/tmp/wt` (worktrees have their own toplevel), registers `/tmp/wt` realpath. Both keys coexist in the registry. Handler from `/tmp/main/sub` matches `/tmp/main`; handler from `/tmp/wt/sub` matches `/tmp/wt`. Verifies stable keying for git worktrees.
- **Submodule (independent install):** project `/tmp/outer` with submodule at `/tmp/outer/sub` (which has its own `.git/` file). `git rev-parse --show-toplevel` from `/tmp/outer/sub` returns `/tmp/outer/sub` (submodule toplevel). Outer install registers `/tmp/outer`; submodule install registers `/tmp/outer/sub` separately. Handler from `/tmp/outer/sub/x` resolves to the submodule, NOT the outer project. Verifies "innermost wins" (codex Round 3 B2).
- **Nested independent repo:** project `/tmp/outer` with `.git`, plus an unrelated nested repo at `/tmp/outer/vendored` with its own `.git`. Handler from `/tmp/outer/vendored` resolves to `/tmp/outer/vendored` (innermost wins). If only `/tmp/outer` is registered, handler from `/tmp/outer/vendored` fails-closed registry-not-found.
- **Detached worktree HEAD:** worktree at `/tmp/wt` with `git checkout --detach`. Install + handler resolve via `git rev-parse --show-toplevel`, which still returns `/tmp/wt` even with detached HEAD. Verifies detached-HEAD does not break resolution.
- **Symlinked project root:** create `/tmp/proj-real`, register it, then create `/tmp/proj-link → /tmp/proj-real`. Install or invocation from `/tmp/proj-link` resolves to `/tmp/proj-real` realpath via `EvalSymlinks` → matches the existing registry entry; no duplicate.
- Happy path: pre-install runtime verification — `EnsureSecret()` creates `~/.config/opsmask/hook_secret` (mode 0600) if missing.
- Edge: `.claude/` directory doesn't exist → installer creates it with mode 0755.
- Edge: settings file doesn't exist → installer creates it with `{}` then merges.
- Edge: settings file exists with other unrelated hooks (e.g., a user's existing `Stop` hook for a different tool) → installer adds Bash-PreToolUse without touching the other entries; uninstall reverses that exactly.
- Edge: settings file has a hook block in `.local.json` and another tool has one in `.json` → installer detects ours regardless of which file it's in.
- Edge: settings file already contains a different `PreToolUse` Bash hook → installer prompts user (chain / refuse / overwrite). Test all three branches.
- Edge: `Install(Options{Mode: Interactive, Cwd: $HOME})` → returns project-detection error. Covers AE5.
- Edge: `Install(...)` from outside any git repo → returns the no-git diagnostic ("requires a git project. Run `git init`...") — see also non-git refusal test above.
- Edge: `Install(...)` when `opsmask` binary is not on PATH (`t.Setenv("PATH", "")`) → returns binary-missing error before any file write.
- Edge: `Install(...)` when `runtime.New` fails (no mapping store; e.g., a project without `opsmask init` run) → returns the **distinct runtime-init-failure error** naming `opsmask init` as the fix. Closes the doc-review finding.
- Edge: shim file already exists with different contents (e.g., a previous opsmask version) → installer overwrites (atomic rename) since the shim is content-addressed by purpose.
- Edge: shim path is on a read-only filesystem → returns clear error.
- Edge: `<project>/.claude/` doesn't exist → installer creates it (mode 0755).
- Edge: `--yes` flag in non-TTY → bypasses confirmation prompt, install proceeds.
- Edge: `Uninstall(...)` when nothing is installed → returns `Result{Status: NothingToUninstall}` and exits 0.
- Edge: `Uninstall(...)` when only the shim is present (no settings hook block) → removes the shim, prints "stale shim removed". Inverse asymmetry handled cleanly.
- Error path: settings file is corrupted JSON → installer refuses with a clear error; no write attempted.
- Error path: partial-write crash mid-rename → atomic rename ensures the original file is intact.
- Error path: secret-file mode is wider than 0600 → `LoadSecret()` refuses; installer aborts with a clear message.
- Integration: full Install → read-back via `os.ReadFile` + JSON parse + structural assertion that the hook block is at the expected path under `hooks.PreToolUse[]` (verified against current Claude Code docs).

**Verification:**
- `go test ./internal/install/... -race` passes all scenarios.
- A manual smoke test in a tempdir: `Install(Mode: Personal)` → inspect the file → run `Uninstall` → inspect that the file is back to **semantic equivalence** with the original — same object content, same array order, all unrelated keys present. Whitespace and JSON object key order may normalize through the load → modify → marshal cycle; tests assert via `json.Unmarshal` + `reflect.DeepEqual` rather than byte equality (closes codex Round 4 R4-6).

---

- U6. **CLI subcommands: `install claude-code` and `uninstall claude-code`**

**Goal:** Cobra wiring for the install/uninstall flow. Mirrors `internal/cli/mcp.go` shape. Handles flag parsing, TTY detection, interactive prompting, and dispatch to U5.

**Requirements:** R8-R13, R16

**Dependencies:** U5.

**Files:**
- Create: `internal/cli/install.go` — `newInstall(opts) *cobra.Command` (group), `newInstallClaudeCode(opts) *cobra.Command` (child).
- Create: `internal/cli/uninstall.go` — same structure for uninstall.
- Test: `internal/cli/install_test.go` — `executeCLI(t, args, stdin)` integration tests.
- Test: `internal/cli/uninstall_test.go`.
- Modify: `internal/cli/root.go:48-62` — add `newInstall(opts), newUninstall(opts)` to `AddCommand`.
- Modify: `internal/cli/root.go:65` — add `install` and `uninstall` to the `RewriteArgs` known map.

**Approach:**
- `install` and `uninstall` are top-level groups with one child each (`claude-code`). Mirror the exact shape of `internal/cli/mcp.go:9-39`.
- Flags on `install claude-code`:
  - `--personal` (default for `Interactive` if TTY) → write to `.claude/settings.local.json`.
  - `--team-shared` → write to `.claude/settings.json`.
  - `--global` → reserved-but-unimplemented (R13: not shipped in v0); cobra returns "not yet implemented" if used.
- TTY detection via `internal/tty` (the existing helper used by `unmask`). In a non-TTY context with neither flag → return `UsageError` with guidance text.
- In a TTY context with neither flag → prompt with cobra's standard input read or via the `internal/tty` helper. Two-option choice: personal vs team-shared.
- Dispatch:
  1. Resolve cwd (use `os.Getwd`).
  2. Call `install.DetectProject(cwd)`. On error → user-facing message, exit 125.
  3. Call `install.Install(Options{...})`. On `AlreadyInstalled` → print message, exit 0. On error → print message, exit 125.
  4. On success → print confirmation: file path, install mode, and the v0 limitation note ("This wraps Bash output only; Read/Grep/MCP tool outputs remain unmasked. See README for details."). The note is the resolution of origin P1 #5 (Bash-only framing).
- `uninstall claude-code` is symmetric: detect, scan both settings files, remove the block, print summary.
- Both commands respect the global `--config` flag for consistency, but the install path itself doesn't read `.opsmask/config.yaml` (no exec gates here).

**Patterns to follow:**
- `internal/cli/mcp.go:9-39` — group + child wiring.
- `internal/cli/exec.go:15-73` — flag declaration shape.
- `internal/cli/config_test.go:141-151` — `executeCLI` test harness.

**Test scenarios:**
- Happy path: `install claude-code --personal` from a project tempdir → writes `.claude/settings.local.json`, prints confirmation. Covers AE7.
- Happy path: `install claude-code --team-shared` → writes `.claude/settings.json`. Covers AE7.
- Happy path: `install claude-code` in TTY without flags → prompts, accepts "personal", writes settings.local.json. Covers AE4.
- Edge: `install claude-code` non-TTY without flag → exit 2 with `UsageError` "specify --personal or --team-shared". Covers AE7's failure branch.
- Edge: `install claude-code` from `$HOME` → exit 125 with project-detection error. Covers AE5.
- Edge: `install claude-code` with both `--personal --team-shared` → exit 2 with mutual-exclusion error.
- Edge: re-run `install claude-code --personal` in already-installed project → exit 0 with "already installed at <path>".
- Happy path: `uninstall claude-code` after personal install → removes the block from settings.local.json, prints summary. Covers AE6.
- Happy path: `uninstall claude-code` after team-shared install → removes the block from settings.json.
- Edge: `uninstall claude-code` when nothing is installed → exit 0 with "nothing to uninstall" (idempotent).
- Integration: full install + opsmask claude-code-hook smoke test (the JSON envelope round-trip from U4 + the file-on-disk from U5).
- Integration: `RewriteArgs(["install", "claude-code", "--personal"])` returns the args verbatim (i.e., `install` is recognized as a known command, not shimmed to `mask`).

**Verification:**
- `go test ./internal/cli/... -race` passes.
- `opsmask install claude-code --help` lists the flags correctly.
- `opsmask install --help` lists `claude-code` as a subcommand.

---

- U7. **Documentation, CHANGELOG, REMAINING_RISKS, skill update**

**Goal:** Document the new feature, residual risks, and operational expectations so adopters and reviewers can understand what shipped without reading source.

**Requirements:** Documentation per origin Success Criteria; supports the user's CLAUDE.md post-implementation pipeline (docs step is non-skippable).

**Dependencies:** U1-U6 (documents what was actually built).

**Files:**
- Modify: `README.md` — add a new "Claude Code hook" section after the existing "MCP server" section. Cover: what it does, how to install (`opsmask install claude-code [--personal|--team-shared]`), how to uninstall, what gets masked (Bash only — explicit scope), what doesn't (Read/Grep/MCP), how fail-closed works.
- Modify: `CHANGELOG.md` — `Unreleased` block with entries under `Added` (install/uninstall subcommands, hook coverage), `Security` (new `SourceHook` audit + `OPSMASK_EXEC_CHILD` recursion guard, default-off promise, project-write trust boundary acceptance), `Changed` (none expected).
- Modify: `docs/REMAINING_RISKS.md` — nine new entries:
  1. **Host-side hook bypass.** Fail-closed is conditional on Claude Code invoking the hook; global `--no-hooks`, settings overrides, or future Claude Code changes are out of scope. OpsMask cannot detect or remediate from within the hook itself when the host bypasses it.
  2. **Team-shared install denies Bash for teammates without OpsMask.** Anyone cloning a repo with a committed `.claude/settings.json` hook block who lacks OpsMask hits fail-closed on every Bash call. Recommend per-user install for personal investigations; `--team-shared` install requires explicit confirmation.
  3. **Shell-tokenization edge cases.** The skip-list tokenizer is conservative under-approximation; `bash -c '<user-supplied>'`, env-prefixed, and time/nice-prefixed forms always wrap. Documented behavior, not a bug.
  4. **Skip-list silent over-allow.** A skip-listed verb later found to leak (e.g., a future `git status` flag that prints data) audits as a pass-through but is not prevented at runtime. Operators monitor `pass_through.log` post-hoc.
  5. **Skip-list `$VAR` expansion.** Skip-listed commands with bare `$VAR` expansions (e.g., `ls $LOG_DIR`) pass through unmasked; the expansion happens in bash, not the hook. Operators should review the skip-list contents against the filenames and path structures present in their environments.
  6. **Multi-hook PreToolUse chain semantics.** When OpsMask's hook coexists with another tool's `PreToolUse` Bash hook, Claude Code runs them in array order and the second hook may see the first's `updatedInput`. The installer warns and prompts on detection; behavior under chain interaction is governed by Claude Code's hook semantics.
  7. **Hook secret loss recovery.** If `~/.config/opsmask/hook_secret` is deleted, the installer regenerates it on next run, but existing installs in other projects continue to use the cached old-sig validation path until re-installed. The user-visible signal is a "signature mismatch" diagnostic from `claude-code-exec`. Recovery: run `opsmask install claude-code` again in each affected project.
  8. **Same-UID secret extraction is out of scope.** The hook secret at `~/.config/opsmask/hook_secret` is mode 0600 and owner-checked, but any same-UID process can read it (standard Unix trust boundary, identical to `~/.ssh/id_rsa`). The HMAC scheme prevents naive direct invocation of `claude-code-exec` and cross-project sig replay (cwd-bound), but not signing by a same-UID-malicious process that has read the secret. Users requiring stronger isolation should run OpsMask in a sandboxed UID. Codex F1 framing.
  9. **Resolved sentinel plaintext appears in `/proc/<pid>/cmdline`.** When a wrapped bash command is running, its argv (containing resolved secret values from sentinels) is visible to other same-UID processes via `/proc/<pid>/cmdline` on Linux. This is the same residual surface already documented for `opsmask exec` on multi-user hosts. The audit log itself stores the unresolved command (sentinel placeholders preserved) so this exposure is bounded to the bash subprocess lifetime. Codex F4 residual.
- Create: `skill/opsmask/references/claude-code-hook.md` — describes hook-active behavior to agents accurately: (a) sentinels appear in Bash output, (b) skip-list pass-throughs are audited at `pass_through.log` (not `exec.log`), (c) the wrap **IS visible in the Claude Code tool-call UI** as `opsmask claude-code-exec --sig <hex> -- '<orig>'` — this is intentional, the agent should not work around it or attempt to bypass via direct bash invocation, (d) when `<<OPSMASK:...>>` sentinels appear in resolved-form output, that is the wrap path delivering masked replacement. Does NOT modify existing SKILL.md (preserves the four required phrases per `skill/opsmask/skill_contract_test.go:15-21`).
- Modify: `skill/opsmask/SKILL.md` — single new bullet under "When OpsMask is installed in a project" referencing the new reference page. Verify all four protected phrases survive.

**Approach:**
- README section follows the existing tone. No emojis. ~150 lines target. Examples blocks for installation, fail-closed UX, audit-log inspection.
- CHANGELOG follows the existing style (`/Users/igene/Documents/personal_project/llm_mask/CHANGELOG.md` line shape).
- REMAINING_RISKS entries follow MCP-section's existing tone: 1-2 sentences, condition + operator guidance + monitoring hint.
- Skill reference page is short (~50 lines). Frames the agent's perspective: "When you see sentinels in Bash output and an audit record exists, the project has installed OpsMask's Bash hook. No action required from you."

**Patterns to follow:**
- README's existing "MCP server" section for tone/structure.
- `docs/REMAINING_RISKS.md`'s existing entries for shape.
- CHANGELOG's `2026-04-30` entries for format.

**Test scenarios:**
- Test expectation: none — pure documentation. Verification is a manual checklist (see Verification).

**Verification:**
- `go test ./skill/opsmask/... -race` passes — the contract test confirms the four required phrases still exist in SKILL.md.
- `grep -n "Claude Code hook" README.md` returns the new section heading.
- `grep -c "^##" CHANGELOG.md` shows the new `Unreleased` block.
- Manual scan of `docs/REMAINING_RISKS.md` confirms four new entries with the documented shape.
- Manual scan of `README.md`'s new section confirms the v0-Bash-only scope is named explicitly (origin P1 #5 resolution).

---

## System-Wide Impact

- **Interaction graph.** New control flow: Claude Code (host) → per-project POSIX shim (`<project>/.claude/opsmask-hook.sh`) → opsmask binary (`opsmask claude-code-hook`). Handler decides: pass-through (skip-list match — including `opsmask` for the re-issue chain) or wrap (signed `opsmask claude-code-exec --sig <hex> -- '<cmd>'`). Wrap envelope is consumed by Claude Code; Claude Code re-issues, which fires the hook chain again, where the skip-list matches `opsmask` and passes through to `claude-code-exec`. `claude-code-exec` verifies the HMAC signature and dispatches to `OrchestrateHook`. Bash runs inside `OrchestrateHook` via the existing `Run` machinery; output flows back through `engine.Process` to Claude Code's agent context. Audit records flow to two separate files: `pass_through.log` (skip-list pass-throughs from U4) and `exec.log` (wrapped invocations from U2 — same file the regular CLI/MCP exec writes to).
- **Error propagation.** Three failure boundaries: (1) shim → Claude Code (binary missing → JSON envelope with `continue: false`), (2) handler → Claude Code (malformed input or audit failure → fail-closed envelope), (3) bypass exec → handler (subprocess error → propagated as the bash exit code, not as a hook failure). Layer 3 keeps Claude Code's "tool call failed" UX consistent with running bash directly.
- **State lifecycle risks.** Audit-log writes to `~/.config/opsmask/exec.log` happen across multiple processes (regular CLI, MCP server, hook handler). POSIX `O_APPEND` line-sized writes are safe under PIPE_BUF — no new mutex required. Settings-file, shim, and registry writes all use the same explicit same-dir tempfile + fsync + chmod 0600 (registry) / 0644 (settings) / 0755 (shim) + owner-UID re-check + `os.Rename` pattern (defined in U5; trust.go's `os.WriteFile` is NOT inherited because it is not atomic — closes codex Round 4 R4-5). The shim file at `<project>/.claude/opsmask-hook.sh` is overwritten on each install; no stale-content concerns since the shim is content-addressed by purpose. Concurrent registry writes are serialized through an advisory lock on `~/.config/opsmask/hook_installs.json.lock` (closes codex Round 4 R4-3).
- **API surface parity.** This plan adds four new top-level CLI commands (`install`, `uninstall`, hidden `claude-code-hook`, hidden `claude-code-exec`). **No new flags on existing public subcommands.** The MCP server is not affected. The existing `mask`, `unmask`, `exec`, `mcp serve`, `config`, `init` surfaces remain bit-identical. The `RewriteArgs` known-commands map gains four entries.
- **Integration coverage.** Cross-layer scenarios that unit tests alone won't prove and require integration tests:
  - Full hook round-trip: tempdir project + `Install(Personal)` + spawn `opsmask claude-code-hook` subprocess with crafted JSON → verify wrap envelope contains a valid `--sig` → spawn `opsmask claude-code-exec --sig <hex> -- '<cmd>'` subprocess → verify masked output reaches the test's stdout buffer.
  - Direct-invocation refusal: spawn `opsmask claude-code-exec --sig 0000... -- 'rm -rf /'` directly (no hook chain) → verify exit non-zero, no command runs, sig-mismatch diagnostic on stderr.
  - Recursion guard end-to-end: same as above but the wrapped command itself invokes `opsmask mask-text` → verify the inner invocation does not re-wrap (handled by `IsExecChild()`).
  - Layer A regression: `Orchestrate` from `Source: SourceCLI` with `argv: ["bash", "-c", "true"]` → verify still returns `ErrPolicyDenied`.
  - Settings-layering robustness: write a hook block in `.claude/settings.json` AND a different hook block in `.claude/settings.local.json` → verify install detects ours regardless of file (R10 idempotency).
- **Cancellation.** New `OrchestrateHook` (U2) inherits `exec.Run`'s ctx-cancellation: `ctx.Done()` triggers SIGTERM on the process group, then SIGKILL after grace. The handler (U4) treats stdin-EOF and ctx-cancel symmetrically — both produce a fail-closed envelope on stdout. Documented caveat (carried from MCP plan): `bufio.Reader.ReadSlice` and `io.Writer.Write` cannot be ctx-cancelled mid-call; for hook latency this is bounded by Claude Code's hook-timeout config.
- **Unchanged invariants.**
  - `Orchestrate` (the regular path) continues to deny `bash` at Layer A for `SourceCLI` and `SourceMCP`. Regression test in U2 enforces this.
  - `WriteRecord` continues to reject records with unknown `Source` values; only `cli`, `mcp`, and the new `hook` are accepted.
  - The `OPSMASK_AUDIT_DIR` override and `~/.config/opsmask/` mode-0700 invariants are preserved.
  - The skill at `skill/opsmask/SKILL.md` retains the four phrases protected by `skill_contract_test.go:15-21`. The "Shells are rejected" guidance remains correct for `opsmask exec` (still hard-deny on bash for the regular path); the hook-active behavior is described in the new reference page.
  - `BuildEnv`'s hard-deny set (`BASH_ENV`, `LD_PRELOAD`, etc.) remains untouched. The bypass mode (`OrchestrateHook` invoked via `claude-code-exec`) reuses it; `OPSMASK_EXEC_CHILD` injection happens after `BuildEnv` returns since the marker is not in the allow-list.
  - File size limit (CLAUDE.md): every new file is ≤400 lines. The longest expected file is `internal/install/install.go` at ~350 lines.

---

## Risks & Dependencies

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| `OrchestrateHook` accidentally regresses Layer A on the regular path. | Medium | High | U2's regression test asserts `Orchestrate(SourceCLI, ["bash", ...])` still returns `ErrPolicyDenied`. Same with `SourceMCP`. Race-detector enabled. |
| `claude-code-exec` HMAC verification is bypassable (constant-time bug, secret exposure, replay). | Low | High | U4 uses `hmac.Equal` (constant-time). Secret file mode 0600, owner-UID checked, no env-var override. Sig is cwd-bound so cross-project replay refuses. Mutation testing on the verify path. Direct-invocation, cross-project, and unregistered-cwd security regression tests. Same-UID extraction documented as out of scope. |
| Hook secret recovery edge: user deletes `~/.config/opsmask/hook_secret`. | Low | Medium | Documented in REMAINING_RISKS. Recovery is `opsmask install claude-code` rerun in each affected project. |
| Malicious project ships `.claude/settings.json` + `.claude/opsmask-hook.sh` to abuse the user's installed OpsMask as a bypass-as-a-service. | Medium | High | `claude-code-hook` and `claude-code-exec` both verify cwd is in the per-user install registry (`~/.config/opsmask/hook_installs.json`) before honoring the hook chain. Refused otherwise with a clear diagnostic. Closes codex F5. |
| PATH-shadowed `opsmask` (malicious project's `bin/opsmask`) bypasses the recursion skip-list. | Low | High | Skip-list match for `opsmask` resolves argv[0] via `exec.LookPath` and `filepath.EvalSymlinks`, compares to `filepath.EvalSymlinks(os.Executable())`. Match only on realpath equality. Closes codex F3. |
| Audit log records resolved sentinel plaintext, leaking secrets into `exec.log`. | Medium before fix / Low after fix | High | Audit record stores the **unresolved** command (sentinel placeholders preserved); `Resolve` runs after the audit-write `"starting"` record and feeds only the bash subprocess argv. Closes codex F4. |
| Settings-layering rule differs from assumption, breaking team-shared installs silently. | Low | Medium | U5 implementation scans both files for an existing block; the install/uninstall flow works regardless of merge semantics. Implementer verifies actual rule via context7 at implementation time. |
| Skip-list tokenizer accepts an unsafe form (e.g., command with `\\` escape that hides `;`). | Medium | High | Tokenizer is conservative-by-construction (any tokenizer error → wrap). Comprehensive table-driven tests in U3 cover ~40 edge cases including the editor `-c` bypass. Pass-through audit (R17) makes any leak post-hoc-detectable via `pass_through.log`. |
| `OPSMASK_EXEC_CHILD` is stripped by some intermediate process (e.g., `sudo -E` not preserving env), causing grandchild recursion. | Low | Medium | Documented in REMAINING_RISKS. Primary recursion guard is the skip-list match on `opsmask` (R15), independent of the env marker. Worst case is excess `pass_through.log` records, not a leak. |
| Shim script crashes (e.g., missing `printf` builtin in unusual shells), causing fail-open. | Low | High | Shim uses POSIX-baseline `sh` constructs (`command -v`, `printf`, `exec`, `set -e`). Tested against `dash` and `bash`. |
| Pass-through audit (`pass_through.log`) fills the disk on hot agent loops. | Medium | Medium | Pass-throughs go to a separate file from `exec.log`, capped log-line size by existing 4095-byte truncation. v1 adds rotation/sampling; v0 documents the limit and points at `OPSMASK_AUDIT_DIR` for relocation. |
| Claude Code's hook envelope shape changes between versions. | Medium | Medium | U4 verifies envelope shape against current docs at implementation time. README documents the supported Claude Code version range. |
| Multi-hook chain: another tool's `PreToolUse` Bash hook coexists with OpsMask's. | Medium | Medium | Installer detects, prompts user (chain / refuse / overwrite), surfaces the chain semantics in REMAINING_RISKS. |
| User installs OpsMask hook in a Codex/Cursor session by mistake. | Low | Low | Hook config is Claude-Code-specific (lives in `.claude/`); other tools don't honor it. |
| Personal install's `.claude/settings.local.json` or `.claude/opsmask-hook.sh` is committed by accident. | Medium | Low | Most projects' default `.gitignore` ignores `*.local.json`; the installer prompts the user about gitignoring the shim. Confirmation message reminds the user to verify both. |
| Pre-existing project lacks `opsmask init`; install would silently break every Bash call. | High before fix / Low after fix | High | U5 verifies `runtime.New(...)` succeeds before writing any files. Refuses with actionable error naming `opsmask init` as the fix. Closes doc-review finding. |

---

## Documentation / Operational Notes

**Documentation impacts (all under U7 unit):**
- `README.md`: new "Claude Code hook" section ~150 lines (two-mode framing: Mode A policy-gated, Mode B HMAC-bypass).
- `CHANGELOG.md`: `Unreleased` block entries.
- `docs/REMAINING_RISKS.md`: nine new entries (see U7 list — host bypass, team-shared DoS, tokenization edges, silent skip-list over-allow, `$VAR` expansion, multi-hook chain, hook_secret recovery, same-UID extraction, `/proc/cmdline` exposure).
- `skill/opsmask/SKILL.md`: one bullet referencing the new reference page; preserves four protected phrases.
- `skill/opsmask/references/claude-code-hook.md`: new reference page describing hook-active behavior to agents (acknowledges wrap visibility in Claude Code tool-call UI).

**Operational notes:**
- `OPSMASK_AUDIT_DIR` env-var relocates the audit-log family. Hook-source wrap records (from `OrchestrateHook` final write) land in **`exec.log`** alongside CLI/MCP records. Skip-list pass-through records (R17, from the hook handler) land in a **separate `pass_through.log`** in the same audit dir, so per-event audit volume in `exec.log` stays bounded.
- The shim is **per-project** at `<project>/.claude/opsmask-hook.sh`, mode 0755 (must be executable). Overwritten on each install — no manual edits expected.
- Uninstall removes (a) the hook block from the project's `.claude/settings*.json`, (b) the per-project shim file at `<project>/.claude/opsmask-hook.sh`, AND (c) the project's git-toplevel realpath from `~/.config/opsmask/hook_installs.json`. Idempotent: re-running uninstall on an already-uninstalled project is a no-op. The per-machine `~/.config/opsmask/hook_secret` is NOT removed by uninstall (shared across projects).

### PR Shaping

- This is large-scoped enough to warrant a single feature branch and one PR; commits split per implementation unit (U1 through U7) for reviewability. The user's CLAUDE.md mandates the post-implementation pipeline (review → simplify → codex review → docs) before merge. Plan to run that pipeline after U7 completes.
- Conventional commit prefix: `feat:` for U1-U6, `docs:` for U7.

---

## Sources & References

- **Origin document:** `docs/brainstorms/2026-05-02-claude-code-bash-hook-requirements.md`
- **Companion ideation:** `docs/ideation/2026-05-01-opsmask-auto-trigger-ideation.md` (Idea 1 capability matrix and competitive landscape)
- **Plan-style precedent:** `docs/plans/2026-04-30-001-feat-mcp-server-plan.md`
- **Existing exec orchestration:** `internal/exec/orchestrate.go`, `internal/exec/run.go`, `internal/exec/policy.go`, `internal/exec/denybase/denybase.go`
- **Audit primitives:** `internal/exec/auditlog.go`
- **CLI structure:** `internal/cli/root.go`, `internal/cli/mcp.go`, `internal/cli/exec.go`
- **Trust binding:** `internal/config/trust.go`
- **Engine and runtime:** `internal/engine/engine.go`, `internal/runtime/runtime.go`
- **Existing skill:** `skill/opsmask/SKILL.md`, `skill/opsmask/skill_contract_test.go`
- **External:** `code.claude.com/docs/en/hooks` (verify current `updatedInput`/`continue`/`stopReason` envelope shape and settings-file merge semantics at implementation time)
