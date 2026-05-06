---
date: 2026-05-02
topic: claude-code-bash-hook
---

# Claude Code Bash PreToolUse Hook (v0)

## Summary

OpsMask ships a Claude Code `PreToolUse` hook that wraps every non-trivial `Bash` tool call through OpsMask's masked-execution path so command output is masked before it enters the agent context. Distribution is an `opsmask install claude-code` subcommand that opts a single project in (default off everywhere else) and asks on first run whether to install personal (gitignored) or team-shared (committable). Failure mode is fail-closed with a user notification.

---

## Problem Frame

OpsMask already exposes a CLI (`mask`, `unmask`, `exec`) and an MCP server with mapping resources, but every masking action requires the user or agent to explicitly route output through it. In practice an AI coding agent investigating logs in Claude Code reaches for `Bash` directly — `cat /var/log/app.log`, `kubectl logs ...`, `journalctl ...` — and the raw bytes land in the agent's context window before any masking can happen. Discipline-based masking ("remember to call `mask_text` first") fails the moment the agent is mid-investigation.

Among the three major coding-agent hosts surveyed in the ideation doc, only Claude Code ships a fully-implemented output-rewriting primitive at the `PreToolUse` boundary today. That asymmetry is the lever for the first integration: the host that ships the cleanest mechanism gets the cleanest hook, validated in a small surface area, before any cross-host parity work.

---

## Actors

- A1. **OpsMask adopter** — a developer who has installed OpsMask and uses Claude Code; wants masked Bash output for specific projects (e.g. an infra-investigate workflow), default off elsewhere.
- A2. **Claude Code** — the agent host; fires `PreToolUse` hooks before executing tool calls and honors `updatedInput` rewrites.
- A3. **OpsMask binary** — provides the `exec` execution path used by the hook, and the new `install claude-code` / `uninstall claude-code` subcommands.

---

## Key Flows

- F1. **First-time project opt-in**
  - **Trigger:** Adopter runs `opsmask install claude-code` from inside a project directory.
  - **Actors:** A1, A3
  - **Steps:** (1) Installer verifies `opsmask` is on PATH and a Claude Code settings location is detectable. (2) Installer prompts the user to choose personal (gitignored) or team-shared (committable). (3) Installer writes the hook config block to the chosen settings file. (4) Installer prints confirmation including the file it wrote and the next-step verb summary.
  - **Outcome:** That project is opted in; subsequent Claude Code sessions in the project run the masking hook.
  - **Covered by:** R8, R9, R10, R11

- F2. **Masked Bash invocation**
  - **Trigger:** Claude Code agent issues a `Bash` tool call inside an opted-in project.
  - **Actors:** A2, A3
  - **Steps:** (1) The `PreToolUse` hook fires. (2) The hook compares the command against the v0 skip-list. (3) Skip-list match → command passes through unmodified. (4) Otherwise → hook returns `updatedInput` rewriting the command through OpsMask's masked-execution path. (5) Bash runs the rewritten command; output is masked. (6) Audit record lands in the existing OpsMask exec audit log.
  - **Outcome:** Agent receives masked output; audit log records the call.
  - **Covered by:** R1, R3, R4, R5

- F3. **Fail-closed when OpsMask is unavailable**
  - **Trigger:** Claude Code agent issues a non-skip-list Bash call in an opted-in project, but `opsmask` is missing or the hook script crashes.
  - **Actors:** A2, A3
  - **Steps:** (1) The hook fires. (2) The hook detects the failure. (3) The hook signals refusal to Claude Code. (4) Bash does not run. (5) The user sees a notification identifying that the OpsMask hook is enabled and what's wrong.
  - **Outcome:** No leak — the command never ran. The user is notified and can fix the underlying installation.
  - **Covered by:** R6, R7

---

## Requirements

**Hook coverage and behavior**

- R1. The hook intercepts `Bash` tool calls in Claude Code via `PreToolUse` and rewrites them so command output is masked before it reaches the agent context.
- R2. The hook covers `Bash` only in v0. `Read`, `Grep`, MCP-tool outputs, and `Stop` transcript sweeps are out of scope.
- R3. Commands matching the v0 skip-list pass through unmodified. The skip-list covers (a) trivial filesystem-state commands that take no user-controlled data through stdin or argv expansion (`ls`, `pwd`, `cd`, `true`, `false`, argument-less `git status`), and (b) interactive TTY commands (`vim`, `less`, `top`, `htop`, `nano`). Commands that take arbitrary expandable arguments (`echo`, `test`, `[`) are **not** in the skip-list — `echo $SECRET` and `test ... && echo $secret` would otherwise bypass masking. Skip-list match requires the entire command be a pure skip-listed verb plus its safe arguments — any pipe, subshell (`$(...)`, `` `...` ``), redirect (`>`, `<`, `>>`), or compound separator (`;`, `&&`, `||`, `&`) disqualifies and the command is wrapped.
- R4. Commands not in the skip-list are wrapped through OpsMask's existing masked-execution path, which both masks output and writes to OpsMask's existing exec audit log.
- R5. The wrap composes with shell features (pipes, subshells, redirects, env vars, here-docs). The hook does not parse the command into pieces or classify it by leftmost verb beyond the skip-list match.

**Failure handling**

- R6. When `opsmask` is not on PATH or the hook script crashes, the hook fails closed: the Bash command must not run.
- R7. On fail-closed, the user receives a notification that identifies the hook is enabled, what failed, and how to address it.

**Distribution and project opt-in**

- R8. OpsMask exposes an `opsmask install claude-code` subcommand that opts a single project in. Default state for any unconfigured project is hook-not-active; nothing is written to global Claude Code settings.
- R9. The installer must be run from inside a project directory. A project directory is detected by `git rev-parse --show-toplevel` succeeding and returning a path other than `$HOME`. Running it elsewhere (no git toplevel, or toplevel equals `$HOME`) prints a guidance error and exits non-zero without writing anything.
- R10. On first run inside a project, the installer asks (Y/n style or labeled choice) whether to install personal (gitignored, `<project>/.claude/settings.local.json`) or team-shared (committable, `<project>/.claude/settings.json`). On subsequent runs, if a hook block already exists in either settings file, the installer prints `already installed at <path>` and exits zero — re-runs are idempotent and do not re-prompt. Persisting the personal-vs-shared choice across genuinely-fresh re-runs is deferred to v1.
- R11. The installer verifies that `opsmask` is on PATH and that a Claude Code settings location is detectable before writing. Otherwise it prints a setup error and refuses.
- R12. `opsmask uninstall claude-code` is the symmetric inverse: it removes the hook block from whichever settings file the installer wrote it to.
- R13. v0 ships neither a `--global` install flag nor distribution as a Claude Code plugin.

**Recursion and audit**

- R15. The hook short-circuits (passes through unmodified) when invoked inside an `opsmask exec` child process, detected via the `OPSMASK_EXEC_CHILD=1` environment marker that `opsmask exec` sets in spawned processes. The `opsmask` binary itself is also added to the v0 skip-list. This prevents recursion when the agent runs `opsmask` CLI commands directly (e.g. `opsmask mask-text`, `opsmask exec -- foo`).
- R16. The installer accepts non-interactive flags `--personal` and `--team-shared` that skip the first-run prompt and write to the corresponding settings file. In a non-TTY context with neither flag, the installer fails with guidance rather than defaulting silently.
- R17. The hook records every skip-list pass-through (matched verb plus the raw command, redacted of any plaintext sentinels) to the existing exec audit log so silent over-allow is auditable post-hoc.

**Evolution path**

- R14. v0 ships a hardcoded skip-list. v1 introduces a project-level skip-list config (e.g. a file under `.opsmask/`) without breaking v0 installations. v0's design must permit the v1 evolution as an additive change.

---

## Acceptance Examples

- AE1. **Covers R5.** Given the project is opted in, when the agent runs `cat /var/log/app.log | grep error`, the hook wraps the entire piped command through OpsMask's masked-execution path; the agent receives masked output for the full pipeline rather than just the leftmost segment.
- AE2. **Covers R3.** Given the project is opted in, when the agent runs `ls`, the command passes through unmodified (skip-list match) and no masking subprocess is spawned.
- AE3. **Covers R6, R7.** Given the project is opted in but `opsmask` is not on PATH, when the agent runs `cat /etc/hosts`, the Bash call refuses to execute and the user sees a notification identifying that the OpsMask hook is enabled and the binary is missing.
- AE4. **Covers R8, R10.** Given a project that has not been opted in, when the user runs `opsmask install claude-code` inside it for the first time, the installer asks personal-vs-team-shared interactively before writing.
- AE5. **Covers R9.** Given the user runs `opsmask install claude-code` from outside any project (e.g. `$HOME`), the command prints an error and exits non-zero without modifying any settings file.
- AE6. **Covers R12.** Given a project has been opted in via a personal install, when the user runs `opsmask uninstall claude-code` in that project, the hook block is removed from `.claude/settings.local.json` and no other content in that file is altered.
- AE7. **Covers R16.** Given a non-TTY context (e.g. a devcontainer post-create script), when the user runs `opsmask install claude-code --personal` inside an opted-in project for the first time, the installer writes to `.claude/settings.local.json` without prompting. Running the same command without `--personal` or `--team-shared` in a non-TTY context fails with a guidance error.
- AE8. **Covers R15.** Given the project is opted in, when the agent runs `opsmask mask-text --help` (or any `opsmask` subcommand), the command passes through unmodified (skip-list match on `opsmask`). When a wrapped command spawns a child that re-invokes `opsmask`, the child detects `OPSMASK_EXEC_CHILD=1` and short-circuits without re-wrapping.

---

## Success Criteria

- An OpsMask adopter can opt a single project in with one command and immediately see masked Bash output in their next Claude Code session in that project, with nothing changed elsewhere.
- When the hook stops working, the user understands why and how to fix it without reading source code.
- Default-off semantics hold: a fresh clone of the OpsMask repo, or a project that has not run the installer, exhibits no Claude Code behavior change.
- `ce-plan` can pick up this document and produce a single-PR implementation without re-asking product-shape questions.
- In a representative test session of agent-driven log investigation, the fraction of secret-bearing bytes that reach agent context via `Bash` is measurably reduced (target: ≥ 95%) relative to a control session without the hook installed. This is the user-facing security outcome v0 must move; without it the install plumbing has shipped but the central problem from the Problem Frame has not.

---

## Scope Boundaries

- `PostToolUse` output masking (Bash, Read, Grep), `Stop` transcript sweep, MCP-output coverage — deferred until the v0 Bash-only path is validated in real use.
- Codex CLI and Cursor integrations — separate brainstorms (Ideas 2 and 3 from `docs/ideation/2026-05-01-opsmask-auto-trigger-ideation.md`).
- Subagent/skill packaging variant of OpsMask (Idea 4), LLM egress proxy (Idea 6), sentinel-as-credential reframe (Idea 5).
- Distribution as a Claude Code plugin (marketplace artifact); `--global` install flag.
- Project-level skip-list config file — this is the v1 evolution path (R14), not part of v0.
- Per-project customization of which verbs are wrapped beyond the v0 baked-in list.

---

## Key Decisions

- **Blanket wrap rather than risky-verb classifier.** A known-coverage-matrix is a footgun for a masking tool; default-deny is safer than default-allow. The narrow skip-list is for ergonomics on commands with no plausible leak risk, not a coverage strategy.
- **`PreToolUse` rewrite rather than `PostToolUse` mask.** `PreToolUse` is fail-closed (no run if the hook breaks), reuses OpsMask's existing audit plumbing, and prevents output from ever entering the agent context unmasked. `PostToolUse` would be fail-open and would require new audit plumbing.
- **Personal-by-default install location.** Infra-investigate is a personal debugging practice. Committing the hook by default would impose masking on teammates who may not have OpsMask installed, triggering the fail-closed refusal for them. The interactive prompt makes the team-shared case explicit when actually intended.
- **v0 hardcodes the skip-list; v1 introduces project-level config.** Ships simple now, leaves a clean evolution path that does not break v0 installations.
- **No global install in v0.** Default-off-everywhere is the explicit user-facing promise. A global flag would create a third state (globally-on, project-on, project-off) that the v0 mental model does not need.

---

## Dependencies / Assumptions

- Claude Code's `PreToolUse` hook continues to honor `updatedInput.command` for the `Bash` tool. Source: `code.claude.com/docs/en/hooks` (cited in the ideation doc capability matrix).
- Claude Code's settings layering treats `<project>/.claude/settings.local.json` as overriding/merging with `<project>/.claude/settings.json` such that personal hook config is honored. To be confirmed against current Claude Code docs during planning.
- OpsMask's existing `exec` path already provides streaming masked execution and audit logging at `~/.config/opsmask/exec.log`. Verified against the README and `cmd/opsmask/main.go`.
- The set of "interactive TTY commands" in the v0 skip-list is reasonably stable in practice; planning will produce the concrete list. The risk of an interactive command being missing from the skip-list is that wrapping it could break the TTY; users would notice and report it.
- **The fail-closed guarantee is conditional on Claude Code invoking the `PreToolUse` hook on `Bash`.** Hook-bypass scenarios — hooks globally disabled by the user, an operator-applied `--no-hooks` flag, a future Claude Code change that skips hooks under some condition — are residual risks rather than enforced properties of this design. OpsMask cannot detect or remediate when the host bypasses the hook from within the hook itself. This bound is acknowledged explicitly so the security claim does not overreach.

---

## Outstanding Questions

### Resolve Before Planning

- (none)

### Deferred to Planning

- [Affects R5][Technical] The exact encoding of the rewrite (e.g., `opsmask exec -- bash -c '<original>'` vs another wrap shape that preserves shell features) is a planning detail. The product decision is captured in R5; the specific encoding is for `ce-plan`.
- [Affects R7][Technical] The exact text and exit-code shape of the fail-closed notification — the product decision is "user is notified clearly"; the specific message wording is a planning detail.
- [Affects R10][Needs research] Confirm Claude Code's settings layering and merging behavior for hooks across `.claude/settings.json` and `.claude/settings.local.json` against current docs; this is now load-bearing (see Deferred / Open Questions).
- [Affects R3][Technical] The concrete v0 skip-list contents — bullet-level enumeration of every verb covered, plus how "interactive" is detected (verb match only, or also a stdin-isatty check via `opsmask exec` itself).

---

## Deferred / Open Questions

### From 2026-05-02 review

These items surfaced during the ce-doc-review pass on this requirements doc and require user judgment before or during planning. They are scope-shifting decisions, not implementation details.

- **[P0][Affects R4, Dependencies, AE1, AE3] Existing `opsmask exec` is policy-gated, not a transparent masker.** `bash` is hard-denied by `internal/exec/denybase` Layer A; `exec` is disabled by default and requires `opsmask config trust` plus `exec.enabled: true`; the default `read-only` scope blocks `cat`/`tail`/`grep`/`journalctl`. As written, every non-trivial Bash call in a fresh project refuses on R6 fail-closed — including AE1 itself. Pick the architectural path before ce-plan starts: (a) introduce a hook-only `opsmask exec --shell` mode that bypasses policy gates with an explicit user opt-in flag, (b) extend the installer to write/verify a permissive trusted exec config alongside the hook block (acknowledge the security trade-off), or (c) scope v0 explicitly to projects that already have freeform exec enabled. This blocks ce-plan from producing a coherent contract until resolved.
- **[P0][Affects F2, F3, Dependencies] Hook script integrity is unguarded.** The hook lives in a project file writable by anyone with project-write access (supply-chain compromise, malicious dependency, or a prior Bash session). The current `config trust` SHA-256 binding for `.opsmask/config.yaml` is not extended to the hook. Decide: extend the trust binding to include the hook script SHA-256 and have opsmask verify it on hook invocation, or accept and document explicitly in `docs/REMAINING_RISKS.md` that anyone with project-write access is implicitly trusted.
- **[P1][Affects R10, R12, AE6, Success Criteria] Settings layering invalidates the default-off promise for the team-shared install path.** Claude Code merges hook arrays additively across settings files — `.local.json` does not override `.claude/settings.json`. Teammates without OpsMask cloning a project with a committed team-shared install hit fail-closed on every Bash call. Resolve before planning: confirm the layering rule against current Claude Code docs; add a teammate-rescue path (e.g., the team-shared installer drops a `.claude/OPSMASK.md` rescue note and warns the installing user explicitly); decide whether to refuse a team-shared install when a `.claude/settings.json` hook block already exists.
- **[P1][Affects R3, R5] Bash command-string tokenization is undecidable as written.** Claude Code's `Bash` tool ships a single `command` string but R3's skip-list match implies argv tokenization while R5 says "the hook does not parse the command into pieces." Edge cases the doc does not resolve: `\ls`, `LC_ALL=C cat`, `bash -c '<orig>'`, `(cd /tmp && cat secret)`, leading whitespace, env-prefixed and time/nice-prefixed forms. The "exact rewrite encoding" is a product-shape decision — picking `opsmask exec -- bash -c '<original>'` vs. argv-only invocation determines whether shell features can compose at all. Resolve before planning.
- **[P1][Affects Summary, R2, Problem Frame] Bash-only ships a partial guarantee under framing implying a complete one.** Read/Grep open the same files as `cat`/`grep`. An adopter watching the agent route around Bash via Read sees raw secrets land in context — exactly the failure the Problem Frame describes. Decide: soften the Summary and Problem Frame to scope the v0 promise to "the Bash path" explicitly, or expand v0 to include Read at minimum.
- **[P1][Affects R6, R7, F3, AE3] Fail-closed notification cannot run when the binary is missing.** AE3 promises "user sees a notification" but the notification logic lives in the hook entry point; if `opsmask` is absent, the script that would emit the message does not run — only Claude Code's generic hook-error UX shows. Decide: have the installer write a thin shell shim (not a bare `opsmask` invocation) into the hook config so the shim can `printf` a diagnostic and exit non-zero even when the binary is absent, or accept the generic UX and revise R7/AE3 wording to reference it explicitly.
- **[P2][Affects Problem Frame, Scope Boundaries] Idea 3 (MCP `instructions` field steering) silently dropped.** The companion ideation explicitly recommended "Idea 1 + Idea 3 in tandem" and called Idea 3 the "highest leverage-to-effort ratio in the survivor set." Idea 3 is also the only piece of the recommended bundle that addresses Codex/Cursor adopters in v0. Add a one-line note to Scope Boundaries: either "Idea 3 is being pursued in parallel — see [link]" or "Idea 3 was rejected because [reason]."
- **[P2][Affects R14, Scope Boundaries] v1 evolution path covers the easy axis (skip-list config) not the load-bearing one (Read/Grep/MCP coverage).** R14 reassures on the easy axis while leaving the harder boundary silent. Decide: extend R14 to assert that the v0 install/config shape is additive-compatible with Read/Grep/MCP coverage — name the v0 decisions (install command name, single-hook-block shape, opt-in granularity) that may need revisiting at that boundary, or explicitly accept that the v0→v1-broader-coverage transition is not a constraint on v0 design.


