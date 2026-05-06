---
date: 2026-05-01
topic: opsmask-auto-trigger
focus: How can Claude Code, Codex CLI, and Cursor be made to automatically trigger OpsMask when investigating logs or command outputs?
mode: repo-grounded
---

# Ideation: OpsMask Auto-Trigger by AI Agents

## Grounding Context

### Codebase
OpsMask is a Go binary exposing both a CLI and an MCP server (single binary, ~23k LOC across `cli`, `mcpsrv`, `engine`, `exec`, `config`, `detect`, `store`, `runtime`).

- **MCP tools:** `mask_text`, `detect_text`, `exec`, `mapping_stats`, `list_detectors`
- **MCP resource:** token-only `mapping` snapshot (no plaintext)
- **CLI verbs:** `mask` (streaming stdin/file → masked stdout), `unmask` (TTY-only — refuses non-TTY stdout), `exec -- cmd` (sentinel-aware argv, re-masks output), `mcp serve`, `config trust`
- **Audit:** `~/.config/opsmask/exec.log` + `mcp_calls.jsonl`
- **Engine:** streams line-by-line. No ambient hooks; every masking point is explicit.

### Pain points
1. Agents must remember to call `mask_text` first
2. Naive subprocess in agent code bypasses masking
3. TTY-only `unmask` blocks programmatic restoration
4. Low discoverability — nothing in a fresh repo announces "route logs through me"

### Output-rewriting capability matrix (the lever)
| Surface | Codex v0.124.0 | Claude Code | Cursor 1.7 |
|---|---|---|---|
| Pre-rewrite tool input | parsed, fails open | shipping (`updatedInput`) | unknown |
| Post-rewrite built-in (Bash/Read) | none | shipping (`updatedBuiltinToolOutput`) | not documented |
| Post-rewrite MCP output | parsed, fails open | shipping (`updatedMCPToolOutput`) | shipping (`updated_mcp_tool_output`) |
| Block-as-substitute | shipping | shipping | shipping |

Sources: [code.claude.com/docs/en/hooks](https://code.claude.com/docs/en/hooks), [cursor.com/docs/hooks](https://cursor.com/docs/hooks), [developers.openai.com/codex/hooks](https://developers.openai.com/codex/hooks)

### Project-convention surfaces
- **Claude Code:** `CLAUDE.md`, project `.claude/` dir, custom skills, custom subagents
- **Codex:** `AGENTS.md` discovery chain (`~/.codex/AGENTS.override.md` > `~/.codex/AGENTS.md` > `<git-root>/AGENTS.md`), Skills (SKILL.md + scripts), `[agents]` subagents in `config.toml`, MCP auto-load via `[mcp_servers.*]`
- **Cursor:** `.cursorrules`, `.cursor/mcp.json`

### Competitive landscape
- **Talon** ([github.com/dativo-io/talon](https://github.com/dativo-io/talon)) — closest competitor: MCP proxy + OPA + Presidio. Gap: MCP-only, misses native shell.
- **agentsh** ([agentsh.org](https://www.agentsh.org/)) — shell shim, blocks not masks
- **LLM Guard Vault** ([github.com/protectai/llm-guard](https://github.com/protectai/llm-guard)) — Python lib round-trip, closest analog to OpsMask's mapping resource
- **Cloudflare AI Gateway** ([cloudflare.com/agents-week/updates](https://www.cloudflare.com/agents-week/updates/)) — network-layer interception
- **Gap:** no tool combines (a) hook-native into Claude Code/Cursor/Gemini, (b) round-trip vault via MCP resource, (c) project-level conventions

## Ranked Ideas

### 1. Claude Code dual-hook stack (PreToolUse + PostToolUse + Stop)
**Description:** A `PreToolUse` hook on `Bash` returns `updatedInput` rewriting the command to `opsmask exec -- <cmd>`. A `PostToolUse` hook on `Bash`/`Read`/`Grep` returns `updatedBuiltinToolOutput` after running the bytes through `mask`. The same surface (`updatedMCPToolOutput`) covers MCP tool results from other servers. A `Stop` hook does a final `detect_text` sweep on the transcript. All wired in `~/.claude/settings.json` with one config block.
**Warrant:** `direct:` matrix entry — Claude Code ships `updatedInput` (PreToolUse), `updatedBuiltinToolOutput` AND `updatedMCPToolOutput` (PostToolUse). Source: [code.claude.com/docs/en/hooks](https://code.claude.com/docs/en/hooks)
**Rationale:** This is the only fully shipping output-rewrite path today. Every other host either drops to MCP-only (Cursor) or fail-open (Codex). The matrix says ship this first.
**Downsides:** Per-host wiring; must follow hook contract changes; Stop-hook can't undo a leaked turn — only the final transcript. Hooks fire as separate JSON-over-stdio scripts which adds latency on hot paths.
**Confidence:** 90%
**Complexity:** Low
**Status:** Unexplored

### 2. Codex block-and-redirect (turn the lossy primitive into a forcing function)
**Description:** Codex `PostToolUse` only honors `decision:"block"` + `reason`. Lean into it: when a Bash command would emit secrets, return `decision:"block"` with a `reason` that *is* the masked stdout — formatted to look like a successful tool result — plus a one-line "re-issue future commands via `opsmask exec --`" hint. The model reads it as cleaned output and adopts the prefix on the next turn. Codex parity without waiting for upstream `updatedMCPToolOutput` to ship.
**Warrant:** `direct:` "Only `decision:'block'` + reason works (lossy)" from the matrix — synthesize the replacement payload to make lossy effectively lossless. Source: [developers.openai.com/codex/hooks](https://developers.openai.com/codex/hooks)
**Rationale:** The Codex parity gap is the single biggest blocker for cross-host adoption. This idea closes it today using only the primitive Codex actually honors.
**Downsides:** Model interprets a block-shaped message as a refusal, not a clean tool result — some prompt engineering needed to make it land. Brittle if Codex tightens the block-reason contract upstream.
**Confidence:** 70%
**Complexity:** Medium
**Status:** Unexplored

### 3. MCP-native steering (server `instructions` field + per-tool `description`)
**Description:** Pack the OpsMask MCP server's init `instructions` field with operational policy ("Before reading any file, command output, or log that may contain secrets, route it through `mask_text` or `exec`. Treat any token of the form `<<OPSMASK:...>>` as opaque"). Mirror the directive in every tool's `description` field. Cursor honors MCP fully; Codex auto-loads `[mcp_servers.*]` from `~/.codex/config.toml`; Claude Code reads tool descriptions during planning. One text edit in the binary propagates to all three hosts with zero per-project setup.
**Warrant:** `direct:` MCP `instructions` field shipping ([github.blog/changelog/2025-10-29](https://github.blog/changelog/2025-10-29-github-mcp-server-now-comes-with-server-instructions-better-tools-and-more/)); `reasoned:` MCP descriptions are loaded into agent context at connection time across all three clients — turning them into a passive automation channel.
**Rationale:** Edit one string in the binary; behavior steers across every connected agent in every project. Highest leverage-to-effort ratio in the survivor set.
**Downsides:** Persuasion-grade, not enforcement; Codex doesn't document `instructions` honoring (one of the gaps the Codex research surfaced); models can ignore prose under pressure.
**Confidence:** 75%
**Complexity:** Low
**Status:** Unexplored

### 4. Tool-disguise subagent (own the model's natural tool-selection)
**Description:** Ship official packages so the model's mental model of "the tool to read logs" *is* OpsMask: a Claude Code subagent named `read-logs`, a Codex skill named `inspect-output`, a Cursor MCP server named `log-reader`. Each wraps `mask_text` + `detect_text` + `exec`. Project rules text reinforces ("delegate `read these logs / debug this trace / look at this output` to `read-logs`"). Models route by name affinity, not by remembering an external tool name.
**Warrant:** `direct:` skills/subagents shipping in all three hosts. Codex subagents docs: [developers.openai.com/codex/subagents](https://developers.openai.com/codex/subagents); Codex skills: [developers.openai.com/codex/skills](https://developers.openai.com/codex/skills).
**Rationale:** Agents pick tools by name affinity during planning. Removing the "OpsMask is a separate thing" signal makes it the path of least resistance, not a discipline tax.
**Downsides:** Name conflicts with users' existing subagents/skills; description-poisoning if the subagent's prompt drifts; needs three slightly different package shapes maintained in lockstep.
**Confidence:** 80%
**Complexity:** Medium
**Status:** Unexplored

### 5. Sentinels as credential resolver (input-side reframe)
**Description:** Reframe OpsMask: not a post-hoc guard but the *only* way an agent references secrets. Project rules instruct: "write `<<SECRET:db_prod_url>>` instead of literal credentials; `opsmask exec` resolves them at exec boundary." The agent literally cannot construct a working command without going through OpsMask, so any output containing those secrets is naturally tokenized.
**Warrant:** `direct:` sentinel + vault round-trip already shipping in `exec`; `external:` LLM Guard Vault round-trip pattern ([github.com/protectai/llm-guard](https://github.com/protectai/llm-guard))
**Rationale:** Inverts the threat model: leaks become impossible by construction for the credential path, not by detection. Detection (`detect_text`) can stay narrowed to PII.
**Downsides:** Input-side only — covers credentials *the agent injects*, but does not retroactively mask outputs from commands the agent constructs without sentinels (e.g., `cat /etc/secrets/raw.env`). Pair with Idea 1 for full coverage. Requires user/team discipline to register secrets as sentinels first.
**Confidence:** 65%
**Complexity:** High
**Status:** Unexplored

### 6. LLM-egress proxy mode (universal fallback)
**Description:** Ship `opsmask gateway` — an HTTPS proxy that users point `ANTHROPIC_BASE_URL` / `OPENAI_BASE_URL` / Cursor's API base at. The proxy masks outbound prompts (regardless of which tool produced them) and unmasks inbound tool-call arguments. Single chokepoint covers Codex's fail-open hooks, Cursor's MCP-only rewrite, and any future agent that bypasses MCP entirely. Use as fallback for environments where hook setup is impractical.
**Warrant:** `external:` Cloudflare AI Gateway pattern ([cloudflare.com/agents-week/updates](https://www.cloudflare.com/agents-week/updates/)); `direct:` all three agents respect `*_BASE_URL` env vars.
**Rationale:** Defeats every "agent forgot to mask" failure mode at the network layer. Works uniformly across Claude Code / Codex / Cursor / future agents without per-tool integration.
**Downsides:** Extra network hop; HTTPS-MITM operational burden (cert trust); latency cost; doesn't catch tool *output* before it enters the agent's context window — only before it leaves the agent for the model. Belt-and-suspenders, not primary.
**Confidence:** 60%
**Complexity:** High
**Status:** Unexplored

## Recommended Sequencing

If you want one thing to ship first: **Idea 1 + Idea 3 in tandem.** Idea 1 is the only fully shipping output-rewrite path today (Claude Code) and is low-complexity; Idea 3 costs almost nothing, propagates to all three hosts, and steers behavior even where hooks fail open. **Idea 2** is the right Codex-specific add until upstream fixes the fail-open. **Idea 4** is the medium-term coverage play. **Idea 5** is the most architecturally interesting reframe but answers an adjacent question (input-side construction). **Idea 6** is the pessimist's universal fallback.

## Rejection Summary

| # | Idea | Reason Rejected |
|---|------|-----------------|
| F1.1 / F2.3 | Shell aliases for `cat`/`tail`/`kubectl` | Below floor on its own; agents may not source rc files; fold into onboarding script |
| F1.3 | Unmask daemon socket for trusted local clients | Friction-fix for round-trip, not a *trigger* mechanism — answers a different question |
| F1.4 | `opsmask doctor` startup probe | Useful as supporting infra, not a standalone step-function move |
| F1.5 | Pre-flight `detect_text` budget | Sub-flavor of Idea 1; same hook surface |
| F1.7 | Streaming-mask FIFO | Ritual-shaped; agents follow tools more reliably than rituals |
| F1.8 | `OPSMASK_STRICT=1` mode | Narrow audience (CI/unattended); supporting feature, not the trigger |
| F2.4 | Rename MCP `exec` → `bash` | Fights tool conventions; Idea 4 captures the steering benefit better |
| F2.6 | Audit-log auto-enrollment (writes rules into projects) | Privacy/ergonomics risk > value |
| F2.7 | Stop-hook retroactive scan | Folded into Idea 1 |
| F2.8 | Codex sandbox profile / `SHELL` override | Fragile across OS variants; Idea 2 is cleaner |
| F3.1 | OpsMask as the default shell | Same outcome as Idea 4 at higher install cost |
| F3.3 | Mask the intent (call-site classifier) | Not actionable as written; partially covered by hook-time `detect_text` |
| F3.4 | Per-session/PTY context | Far from current arch; expensive |
| F3.6 | Make unmasked output unusable | Folded into Idea 5 / strict mode |
| F3.8 | OpsMask owns the agent process | Talon-shaped rewrite; expensive vs incremental hooks |
| F4.1 | MCP `mask-tool` capability spec | Speculative governance dependency |
| F4.4 | Homebrew bundle | Distribution; not transformative on its own |
| F4.5 / F6.7 | Compliance product / cloud audit | Off-topic — this is about adoption mechanics, not product positioning |
| F4.6 | Default in `CLAUDE.md`/`AGENTS.md` templates | Distribution sub-tactic |
| F4.7 | Skill marketplace anchor tenant | Speculative; marketplaces immature |
| F4.8 | Pre-commit hook for repo hygiene | Off-topic — extends audience but doesn't trigger AI agents |
| F4.3 / F6.8 | Cross-session / team vault | Compounding move, but solves persistence not triggering |
| F5.1-5.8 | Cross-domain analogies (customs, pasteurization, etc.) | Reinforce existing moves; not new ideas |
| F5.7 | NFPA fire-load tool taxonomy | Folded into Idea 1 (the list of "high-risk tools" is what hooks intercept) |
| F5.9 | HMAC seal on outputs | Folded into Idea 5 / supporting integrity primitive |
| F6.1 | No-vault-no-boot wrapper | Heavy-handed; user-hostile if vault unlocks fail |
| F6.3 | OpsMask owns global config | Sub-tactic of distribution |
| F6.4 / F6.9 | Auto-install on first agent run / `opsmask agent-init` | Speculative on agent willingness/permissions |
