---
title: "feat: Add MCP server for plug-and-play LLM client integration"
type: feat
status: completed
date: 2026-04-30
---

# feat: Add MCP server for plug-and-play LLM client integration

## Summary

Ship a Model Context Protocol server as a new `opsmask mcp serve` subcommand of the existing Go binary. The server exposes the project's masking, detection, follow-up `exec`, and observability capabilities to MCP clients (Claude Code, Cursor, Copilot) over stdio JSON-RPC. The strategic divergence from CloakMCP is that `unmask` is deliberately **not** an MCP tool — preserving OpsMask's TTY-gated reversibility. `exec` **is** exposed, which is the project's headline differentiator and the primary reason to ship MCP at all.

The plan proceeds in two refactor phases (runtime relocation, exec orchestration extraction, detect-only engine path) before any MCP tool handlers are written. This sequencing prevents CLI/MCP behavior drift and respects the side-effect semantics of `engine.Process`.

---

## Problem Frame

Per `docs/competitive-analysis.md`, "no MCP server" is the single biggest external-perception gap vs. the closest competitor (CloakMCP). Today, integrating OpsMask with an LLM client requires the bundled Claude skill prompt, which works only on Claude Code and requires the user to have read and trusted the skill. MCP is the broader emerging standard; without it, OpsMask is invisible to Cursor/Copilot users and friction-heavy for Claude Code users compared to the `pipx install cloakmcp; cloak install --profile hardened` flow that CloakMCP ships today. Shipping MCP also lets us put our differentiating capabilities (`exec`, K8s-aware detectors, TTY-gated unmask) directly in front of agents, where the value is realized.

---

## Requirements

- R1. New `opsmask mcp serve` subcommand starts an MCP server on stdio that conforms to spec revision 2025-03-26 or later, using the official `modelcontextprotocol/go-sdk`.
- R2. The server reuses `engine.Process`, `internal/store`, `internal/detect`, and `internal/exec` capabilities through importable wrappers — no parallel re-implementation. Runtime construction currently sitting in `internal/cli` is relocated to an importable package so `internal/mcpsrv` can call it directly.
- R3. `unmask` is not exposed as an MCP tool, prompt, or resource. Plaintext never leaves the human's TTY through MCP.
- R4. `exec` is exposed as an MCP tool that honors every existing CLI gate (config trust binding, allow-list, deny-list, env shaping, audit preflight, output re-masking) and adds two MCP-specific tightenings: refuse when the policy would otherwise grant unrestricted execution (`cfg.Scope == freeform` AND project allow-list is empty), and tag audit records with `source: "mcp"`.
- R5. The five tools (`mask_text`, `detect_text`, `exec`, `mapping_stats`, `list_detectors`) and one resource template (`mapping/{type}`) are the v1 surface. Tool descriptions stay tight to keep the per-call schema budget under 4 KiB total.
- R6. Distribution path documented end-to-end: GoReleaser config emits `opsmask` binary; README carries Claude Desktop / Claude Code / Cursor config snippets with absolute-path guidance.
- R7. All MCP tool calls write a JSONL audit record. Mask/detect/observability calls record only counts and sizes to a new `mcp_calls.jsonl` stream; `exec` calls reuse the existing `exec.log` audit stream with the added `source` field. Both files use identical open semantics (`O_APPEND|O_CREATE|O_WRONLY|O_CLOEXEC`, mode 0600) so multi-process append works.
- R8. Every long-running tool (`exec`, large `mask_text`) propagates the MCP request context through to subprocess control, store calls, and audit finalization. A client disconnect or cancellation must terminate the subprocess (process-group SIGTERM → SIGKILL grace), interrupt the engine's read/detect/write loop, and finalize the audit record. **Today `engine.Process` only checks ctx at `alloc.CommitPlans`; the outer loop at `internal/engine/engine.go:71-97` and `processSegment` do not. U3 closes this gap before MCP tools that depend on it ship.**

---

## Scope Boundaries

- HTTP / Streamable-HTTP / SSE transports (stdio only for v1; revisit if remote-host use cases emerge).
- OAuth / authentication (stdio is local-only by spec; auth is moot).
- `unmask` exposure in any MCP form (tool, resource, or prompt).
- New detector rules, new policy primitives, or schema changes to the SQLite mapping store.
- `.mcpb` desktop-extension packaging (planned for a follow-up once the format stabilizes across clients).
- Encryption-at-rest for the mapping store (separate roadmap item; tracked in `docs/competitive-analysis.md` "Where OpsMask is behind").
- Tool result compression / RAG-MCP-style schema deduplication (worth revisiting if telemetry shows token bloat in practice).
- MCP resource subscriptions / change notifications (v1 contract is read-on-demand snapshots only; advertised capabilities will not include subscriptions).

### Deferred to Follow-Up Work

- Remote MCP hosting (Streamable HTTP transport, OAuth) — separate plan once a real use case surfaces.
- `.mcpb` desktop extension package — separate plan after format stabilizes across Claude / Cursor / Copilot.
- Mapping vault encryption-at-rest — tracked separately; orthogonal to MCP.
- MCP resource subscriptions — only if a real client-side caching pattern emerges.

---

## Context & Research

### Relevant Code and Patterns

- `cmd/opsmask/main.go` — entry point; `cli.RewriteArgs` plus `cli.NewRoot`. Adding a subcommand follows the existing `newMask` / `newExec` shape.
- `internal/cli/root.go:60` — `root.AddCommand(...)` is where the new `mcp` group attaches.
- `internal/cli/helpers.go:13` — `runtimeEnv` is **unexported**; `internal/cli/helpers.go:20` `buildRuntime` is package-private. U1 relocates both into `internal/runtime` so external callers (`internal/mcpsrv`) can construct the same runtime without re-implementing.
- `internal/cli/mask.go:39` — `engine.Process(ctx, in, out, rules, alloc, opts)` is the masking primitive. **It is not pure** — at `internal/engine/engine.go:129` it calls `alloc.CommitPlans` which writes to the SQLite store. The MCP `mask_text` tool inherits this side effect (intended). The MCP `detect_text` tool must not — see Key Technical Decisions for the dedicated detect-only path.
- `internal/cli/exec.go:26` — the CLI exec command. The orchestration body (preflight → trust → enabled → scope → resolve → policy → env → run → audit) is currently inlined in `newExec` and must be extracted before the MCP exec tool is written; otherwise the two will drift.
- `internal/exec/run.go:29` — `Run` returns `RunResult{ExitCode, Duration, ErrorClass}` only; `internal/exec/run.go:128` `maskStream` discards `engine.Process` stats. U4 refactors `Run` to also return masked/destroyed/by_type stats so the MCP exec tool can surface them.
- `internal/exec/auditlog.go:16` — `Record` does not have a `Source` field today. U2 adds it with `json:"source,omitempty"`; readers normalize empty to `"cli"`. CLI call sites must be updated to set `Source: "cli"` explicitly via a new constructor.
- `internal/exec/auditlog.go:78-95` — audit dir convention: `OPSMASK_AUDIT_DIR` env var, fallback `os.UserConfigDir() + /opsmask`. The new `mcp_calls.jsonl` writer reuses `auditDir()` (export it from the package or duplicate the resolver) so both audit streams land in the same directory with consistent permissions.
- `internal/exec/policy.go:53-69` — `EvaluatePolicy` baseline + freeform behavior. **Critical detail at line 56-58**: when `cfg.Scope == ScopeFreeform AND len(entries) == 0` the function returns `Allowed: true` for everything. When freeform has any allow entries (project- or baseline-supplied), the function falls through to entry matching. This is the cut-line for the MCP-specific refusal: refuse only when the policy would grant unrestricted execution, not when freeform is paired with a real allow-list.
- `internal/store/store.go:18` — `Store` interface has `Lookup, Insert, InsertBatch, List, Prune, Close`. **No `Stats()` method exists.** U3 adds `Stats(ctx) (StoreStats, error)` to the interface and updates every implementation (`internal/store/sqlite.go`) and any test doubles.
- `internal/store/concurrency_multiprocess_test.go` — proves concurrent **subprocess insert** safety only. Does not prove concurrent CLI mask + long-lived MCP serve workloads, reads-during-writes, or context cancellation under contention. U1 adds the missing coverage.
- `internal/detect/rules/builtin.go:19` — `Builtins()` returns the spec list that `list_detectors` walks.
- `internal/detect/codec.go` — sentinel inert-escape decode/encode; tests must cover both Unicode (`⟪…⟫`) and ASCII (`--MASK--…--`) token forms.

### External References

- MCP transport spec 2025-03-26: https://modelcontextprotocol.io/specification/2025-03-26/basic/transports — stdio is the recommended posture for local CLI wrappers; clients MUST support it.
- MCP security best practices: https://modelcontextprotocol.io/docs/tutorials/security/security_best_practices — "Use the stdio transport to limit access to just the MCP client" for locally-running servers.
- Official Go SDK `modelcontextprotocol/go-sdk` (v1.5.0, April 2026): https://github.com/modelcontextprotocol/go-sdk — Anthropic+Google co-maintained.
- CloakMCP: https://github.com/ovitrac/CloakMCP — exposes `cloak_pack_text`, `cloak_unpack_text`, `cloak_pack_dir`, `cloak_unpack_dir`, `cloak_scan_text`, `cloak_vault_stats` on stdio. Notably exposes `unpack` unguarded; we deliberately diverge.
- Invariant Labs MCP tool poisoning research: https://invariantlabs.ai/blog/mcp-security-notification-tool-poisoning-attacks
- OWASP MCP01 (token mismanagement and secret exposure): https://owasp.org/www-project-mcp-top-10/2025/MCP01-2025-Token-Mismanagement-and-Secret-Exposure
- Reference Go MCP server: https://github.com/github/github-mcp-server — GoReleaser, Homebrew, Claude Desktop config snippet patterns.
- Tool-bloat research: https://www.atlassian.com/blog/developer/mcp-compression-preventing-tool-bloat-in-ai-agents — recommends ≤10–15 tools per agent context.

---

## Key Technical Decisions

- **Use `modelcontextprotocol/go-sdk` (official) over `mark3labs/mcp-go`.** Framing: this is a maintainability and spec-alignment bet, not a maturity comparison — both SDKs implement current spec. The official SDK is co-maintained by Anthropic and Google with full spec-coverage intent and idiomatic Go (reflection-based schema, `context.Context` everywhere). For a security-sensitive tool aligned with Anthropic's ecosystem, the long-term spec-compliance bet wins; the community SDK has more raw stars and existing examples but adds a third-party-coupling risk.
- **Stdio transport only for v1.** The spec recommends stdio for local CLI wrappers; it eliminates DNS-rebinding, origin-header, and session-id surface entirely. CVE-2025-6514 (RCE via `mcp-remote`) demonstrates the cost of HTTP transport bugs.
- **`unmask` is not an MCP tool.** Returning plaintext to an agent's context window voids the TTY-gate, OpsMask's headline differentiator. CloakMCP exposes its `unpack` tools and accepts the leakage; we do not.
- **`exec` is an MCP tool.** It is the project's headline differentiator. Output is already re-masked by `engine.Process` before returning (verified at `internal/exec/run.go:128`), so plaintext does not leak. The existing trust-binding + allow-list + deny-base + env-shaping gates already constrain agent callers identically to human callers.
- **Refined MCP-specific exec tightening: refuse `exec` only when `cfg.Scope == freeform` AND the merged baseline + project allow-list is empty.** This is the precise condition where `EvaluatePolicy` returns `Allowed: true` unconditionally (`internal/exec/policy.go:56-58`). A freeform scope paired with an explicit allow-list is *more* restrictive than the baseline (it still goes through entry matching) and is a legitimate workflow that should remain available over MCP. The earlier blanket-refusal of all freeform scope was wrong.
- **Audit records get a `Source` field with runtime enforcement as the primary guard.** `internal/exec.Record` gains `Source string \`json:"source,omitempty"\``. A new constructor `NewRecord(source string) Record` is the sanctioned construction path. **Primary defense: `WriteRecord` rejects any record whose `Source` is not in `{"cli", "mcp"}`.** This rejection is the load-bearing guarantee — every audit record actually written has a valid source. Bare `Record{}` literal construction is silently downgraded to a write-time error rather than producing a malformed audit line. Decoders normalize the empty string to `"cli"` only when *reading* pre-MCP audit logs; never on write. **Secondary defense (drift prevention): an AST/type-aware analyzer test** (using `golang.org/x/tools/go/analysis` or equivalent) finds composite literals whose resolved type is `internal/exec.Record` regardless of import alias — the existing `maskexec.Record{...}` at `internal/cli/exec.go:41` would not be caught by a simple `git grep -E 'exec\.Record\{'`. The analyzer rejects new literal-construction sites outside `internal/exec` itself; the existing `internal/cli/exec.go` site is updated to `NewRecord` in U2 and added to the rejection list afterward.
- **Two audit streams, same directory, different failure semantics.** `exec.log` continues to receive `Record`-shaped lines; a new `mcp_calls.jsonl` receives lean MCP-call records (`{ts, tool, args_summary, ok, err_class, result_size_bytes, duration_ms, source: "mcp"}`). Both files are opened with `O_APPEND|O_CREATE|O_WRONLY|O_CLOEXEC`, mode 0600, and live in `AuditDir()` (the existing `OPSMASK_AUDIT_DIR` / `os.UserConfigDir()/opsmask` resolver, exported from `internal/exec` for shared use). Multi-process appends are safe under POSIX `O_APPEND` semantics for line-sized writes (single-line records well under PIPE_BUF on all supported platforms).
- **Failure semantics: `exec.log` writes fail closed; `mcp_calls.jsonl` writes fail open, degraded status NOT surfaced through MCP.** `exec.Preflight` already fails closed before any subprocess runs (`internal/cli/exec.go:35`), and U4's `Orchestrate` preserves this for every call site including MCP. **There is no relaxation of exec audit guarantees over MCP** — if `exec.log` is unwritable, `exec` tool calls return `EXEC_AUDIT_UNWRITABLE` and refuse to run the subprocess. The lean `mcp_calls.jsonl` for non-exec tools (`mask_text`, `detect_text`, `mapping_stats`, `list_detectors`, resource reads) uses fail-open semantics so an attacker who fills the audit directory cannot deny core detection/masking functionality, but the server logs every audit failure to its own stderr only. **Audit-failure state is not exposed through any MCP tool or resource** — exposing even a sticky boolean would let an attacker who controls fill capacity probe whether the attack succeeded. Operators who need machine-readable degraded-status can read the server's stderr stream (e.g., via the systemd journal or the launching MCP client's stderr capture). The asymmetry between exec and non-exec audit semantics is deliberate: subprocess execution is forensically critical and must not run un-audited; bulk masking has lower per-call forensic value and a higher denial-of-service surface.
- **Detect-only engine path.** `engine.Process` calls `alloc.CommitPlans` (`internal/engine/engine.go:129`) — it persists mappings as a side effect. The MCP `detect_text` tool must not persist. U3 adds a new function (placement: `engine.DetectOnly` or `detect.Scan` — choose during U3 implementation) that calls `detect.FindMatches` directly and aggregates counts without invoking the allocator. `mask_text` continues to use `engine.Process` and *does* persist mappings (correct — the agent calling `mask_text` then sending masked text to the LLM and receiving a masked report relies on the same project-deterministic mapping).
- **`Run` is refactored to return engine stats.** Today `internal/exec/run.go:128` discards `engine.Process` stats. U4 changes `RunResult` to include `Masked, Destroyed int` and `ByType map[string]int` aggregated across stdout and stderr, so the MCP exec tool can surface them. The CLI exec command ignores the new fields (existing behavior unchanged).
- **Shared exec orchestration extracted to `internal/exec`.** Today `internal/cli/exec.go:26-109` inlines the full flow. U4 extracts a function `internal/exec.Orchestrate(ctx, runtime, source, argv, opts)` that returns `(OrchestrateResult, error)`. The CLI `newExec` becomes a thin wrapper that prints stderr messages on refusals; the MCP exec tool calls the same function and converts errors to JSON-RPC error codes.
- **Runtime relocation: `internal/runtime` with explicit exported API.** U1 moves `runtimeEnv` (now `runtime.Env`) and `buildRuntime` (now `runtime.New`) into a new `internal/runtime` package. The `Env` struct exports its fields directly so `internal/mcpsrv` (and `internal/cli`) can read them without accessor boilerplate:

  ```
  // internal/runtime/runtime.go (signature sketch — directional, not implementation)
  type Env struct {
      Store  store.Store
      Alloc  *pseudo.Allocator
      Rules  []detect.Rule
      Loaded config.Loaded
  }
  func New(opts Options) (*Env, error)
  func (e *Env) Close() error
  ```

  `internal/cli/helpers.go` becomes a thin re-export (`type runtimeEnv = runtime.Env`; existing field access on `rt.store` / `rt.alloc` / `rt.rules` / `rt.loaded` becomes `rt.Store` / `rt.Alloc` / `rt.Rules` / `rt.Loaded` — this is a mechanical rename across the CLI package, covered by U1's "existing CLI tests pass" verification).

  `internal/runtime` imports `internal/store`, `internal/pseudo`, `internal/detect`, and `internal/config` only; it must not import `internal/cli` to avoid cycles. The CLI package imports `internal/runtime`, not the reverse.
- **`mapping_list` is an MCP Resource, not a Tool.** It is read-only state; resources are the right primitive. The v1 contract is read-on-demand snapshots only; resource subscriptions are not advertised in capabilities. Tokens only — no plaintext, no HMAC bytes, no anything that could enable cross-store correlation. Per-call rate-limit / max-limit clamp (default 50, max 500) is enforced in the resource handler.
- **Subcommand layout: `opsmask mcp serve`.** Keeps the `mcp` namespace open for follow-ups (`opsmask mcp tools list`, etc.) without polluting the top-level command set.
- **Tool descriptions stay terse (≤200 chars each).** Per Atlassian / RAG-MCP research, schema-budget bloat collapses agent selection accuracy.

---

## Open Questions

### Resolved During Planning

- **Which Go SDK?** Official `modelcontextprotocol/go-sdk`.
- **Which transport?** Stdio only.
- **Do we expose `unmask`?** No.
- **Do we expose `exec`?** Yes, with refined freeform refusal.
- **`mapping_list` as tool or resource?** Resource, read-on-demand only.
- **Where does runtime construction live?** New `internal/runtime` package.
- **Where does shared exec orchestration live?** New function in `internal/exec`.
- **How does `detect_text` avoid persisting?** Dedicated detect-only path using `detect.FindMatches`.
- **Where do MCP audit records live?** Same directory as `exec.log`, separate file `mcp_calls.jsonl`.

### Deferred to Implementation

- **Exact placement of the detect-only function** (`engine.DetectOnly` vs `detect.Scan`). Decide during U3 based on which existing helpers it can reuse.
- **Whether `detect_text` returns per-match offsets** as an opt-in `include_matches: true` parameter. Default to counts only; add offsets only if real-world use shows agents need them.
- **Whether to surface `internal/exec.auditDir()` as exported** (`AuditDir()`) vs duplicate the env-var/UserConfigDir resolver in `internal/mcpsrv`. Export is simpler; duplication is more isolated. Decide during U2.
- **Final tap repository URL for Homebrew** — publishing decision, not planning.

---

## High-Level Technical Design

> *This illustrates the intended approach and is directional guidance for review, not implementation specification. The implementing agent should treat it as context, not code to reproduce.*

```
                           ┌────────────────────────────┐
   stdin/stdout (JSON-RPC) │  opsmask mcp serve         │
   ◀─────────────────────▶ │                            │
                           │  ┌──────────────────────┐  │
                           │  │ mcp.Server (SDK)     │  │
                           │  │  - tools[5]          │  │
                           │  │  - resource[1]       │  │
                           │  │  capabilities:       │  │
                           │  │    subscribe=false   │  │
                           │  └──────────┬───────────┘  │
                           │             │              │
                           │  ┌──────────▼───────────┐  │
                           │  │ internal/runtime.New │  │  (relocated from cli/)
                           │  │  - store             │  │
                           │  │  - alloc             │  │
                           │  │  - rules             │  │
                           │  │  - loaded cfg        │  │
                           │  └──────────┬───────────┘  │
                           │             │              │
                           │  ┌──────────▼───────────┐  │
                           │  │ tool handlers        │  │
                           │  │  mask_text       ────┼──┼──▶ engine.Process (persists)
                           │  │  detect_text     ────┼──┼──▶ detect.FindMatches (no persist)
                           │  │  exec            ────┼──┼──▶ exec.Orchestrate (shared w/ CLI)
                           │  │  mapping_stats   ────┼──┼──▶ store.Stats (new method)
                           │  │  list_detectors  ────┼──┼──▶ rules.Builtins() + project rules
                           │  └──────────────────────┘  │
                           │  ┌──────────────────────┐  │
                           │  │ resource handler     │  │
                           │  │  mapping/{type}  ────┼──┼──▶ store.List(type, limit)
                           │  │   (no plaintext,     │  │
                           │  │    no HMAC, tokens   │  │
                           │  │    only)             │  │
                           │  └──────────────────────┘  │
                           │  ┌──────────────────────┐  │
                           │  │ audit                │  │
                           │  │  exec.log ←──────────┼──┼── exec.WriteRecord (Source set)
                           │  │  mcp_calls.jsonl ←───┼──┼── mcpsrv audit writer
                           │  └──────────────────────┘  │
                           └────────────────────────────┘
```

Tool I/O sketches (directional — final field names settle in implementation):

```
mask_text:
  in:  { text: string, ascii_tokens?: bool }
  out: { text: string, masked: int, destroyed: int, by_type: {string:int} }
  side effect: persists pseudonyms to mapping store (intended)

detect_text:
  in:  { text: string, include_matches?: bool }
  out: { count: int, by_type: {string:int}, matches?: [{type, start, end}] }
  side effect: none (uses detect.FindMatches without allocator commits)

exec:
  in:  { argv: [string], timeout?: string }
  out: { exit_code: int, stdout: string, stderr: string, duration_ms: int,
         masked: int, destroyed: int, by_type: {string:int} }
  errors: EXEC_UNTRUSTED | EXEC_DISABLED | EXEC_SCOPE_OPEN_REFUSED
        | EXEC_RESOLVE_FAILED | EXEC_POLICY_DENIED | EXEC_TIMEOUT | EXEC_CANCELLED
  cancellation: ctx propagated through Resolve, Run (process-group), audit finalize

mapping_stats:
  in:  {}
  out: { total: int, by_type: {string:int} }

list_detectors:
  in:  {}
  out: { detectors: [{name, type, policy}] }

resource: mapping/{type}
  uri:  opsmask://mapping/{type}?limit=N   (default 50, max 500)
  body: { type: string, entries: [{token, length}], truncated: bool }
        (no plaintext, no HMAC, no any cross-correlatable identifier)
  contract: read-on-demand snapshot only; subscriptions not advertised
```

---

## Implementation Units

- U1. **MCP server subcommand skeleton + runtime relocation**

**Goal:** Add the `opsmask mcp serve` cobra subcommand. Relocate `runtimeEnv` and `buildRuntime` from `internal/cli` to a new `internal/runtime` package with the exported `Env` API (see Key Technical Decisions) so external packages can construct the same runtime. Stand up an empty MCP server on stdio that responds to `initialize` and `ping` (no tools registered yet).

**Requirements:** R1, R2

**Dependencies:** none

**Files:**
- Create: `internal/runtime/runtime.go` — exports `Env`, `New(opts Options)`, `(*Env).Close()`. Move the body of `internal/cli/helpers.go:20`.
- Modify: `internal/cli/helpers.go` — becomes a thin re-export (`type runtimeEnv = runtime.Env`, `var buildRuntime = runtime.New` adapted to existing call sites) so the rest of `internal/cli` keeps compiling without churn.
- Create: `internal/cli/mcp.go` — registers `newMcp(opts)` returning a `cobra.Command` group with `serve` as the only child.
- Modify: `internal/cli/root.go:60` — add `newMcp(opts)` to `AddCommand` call.
- Create: `internal/mcpsrv/server.go` — exports `NewServer(rt *runtime.Env, w AuditWriter) *mcp.Server`. v1 registers no tools; subsequent units add them.
- Modify: `cmd/opsmask/main.go` — register `"mcp"` in `RewriteArgs` known-command map.
- Modify: `go.mod`, `go.sum` — add `github.com/modelcontextprotocol/go-sdk` pinned to a 1.x release.
- Test: `internal/runtime/runtime_test.go` — constructs a runtime against a temp mapping path, asserts the same shape today's CLI tests assert. Asserts `Env.Store`, `Env.Alloc`, `Env.Rules`, `Env.Loaded` are non-nil and externally accessible.
- Test: `internal/cli/mcp_test.go` — invokes `mcp serve` over a stdin/stdout pipe, completes the `initialize` handshake, asserts `capabilities.tools` is empty and `capabilities.resources.subscribe == false`.
- Test: `internal/mcpsrv/server_test.go` — uses the SDK's in-memory transport to exercise the server without subprocesses.

**Approach:**
- `internal/runtime` becomes the canonical place for "open the store, load the config, build the rule set, build the allocator." It exports a minimal `Options` mirroring today's `cli.Options`.
- `internal/cli/helpers.go` retains its old name and signature for source compatibility; under the hood it calls into `runtime.New`. Other CLI files (`mask.go`, `exec.go`, etc.) need no changes.
- `internal/mcpsrv` lives under `internal/` (private to the binary) — there is no public API contract for the MCP wiring.
- The empty server advertises `tools` and `resources` capabilities so subsequent units can register without bumping capabilities again. Subscription support is **not** advertised.

**Patterns to follow:**
- Subcommand registration mirrors `internal/cli/root.go:60`.
- `internal/store/concurrency_multiprocess_test.go` for the new mixed-process test shape.

**Test scenarios:**
- Happy path: `runtime.New(opts)` produces a working `Env` with non-nil `store, alloc, rules`.
- Happy path: `opsmask mcp serve < /dev/null` exits cleanly with code 0.
- Happy path: SDK in-memory transport completes `initialize`, returned capabilities show `tools` and `resources` advertised, `subscribe: false`.
- Edge case: malformed JSON-RPC payload → server replies with parse error and continues serving.
- Edge case: stdin close mid-session → server exits with code 0, no goroutine leaks.
- Edge case: shutdown order — close-tracking fakes for `runtime.Env` and the `mcp_calls.jsonl` audit writer assert both are closed (in that order: audit writer first to flush in-flight records, then runtime to release the SQLite store) when the server's main loop exits via stdin close, ctx cancellation, or signal. A goroutine count taken before and after the test confirms no leaks.
- Edge case: ensure `internal/cli` package still compiles and all existing tests pass after the relocation (sanity check).
- Edge case: `runtime.Env` field-rename sweep — every reference to the old unexported fields is updated. The sweep relies on the Go compiler (the rename forces compilation errors for any missed reference) plus a verification grep `git grep -nE '\.(store|alloc|rules|loaded)\b' internal/cli internal/mcpsrv -- '*.go'` (covers receiver-style access like `r.store` at the old `helpers.go:60`/`:63` *and* named-field literals like `runtimeEnv{store: ...}`). Matches outside `_test.go` files indicate missed sites.
- Error path: unable to open mapping store (read-only filesystem) → `runtime.New` returns a wrapped error; `mcp serve` exits with non-zero before the SDK accepts connections.

**Verification:**
- `go test ./internal/runtime/... ./internal/cli/... ./internal/mcpsrv/... ./internal/store/...` passes including the new mixed-process test.
- `go vet ./...` is clean.
- `git grep 'runtimeEnv'` shows only the alias in `helpers.go`.

---

- U2. **Audit primitives: `Source` field on `exec.Record`, `mcp_calls.jsonl` writer**

**Goal:** Extend `internal/exec.Record` with a `Source` field plus a constructor that requires it. Add an `internal/mcpsrv` audit writer for `mcp_calls.jsonl` with identical open semantics to the existing `exec.log`. Add multi-process append tests for both files.

**Requirements:** R7

**Dependencies:** U1

**Execution note:** Add the multi-process append tests before implementing the writer. Append safety is the load-bearing property of this unit.

**Files:**
- Modify: `internal/exec/auditlog.go`:
  - Add `Source string \`json:"source,omitempty"\`` to `Record`.
  - Add `func NewRecord(source string) Record` constructor that stamps `Ts` and `Source`.
  - **Harden `WriteRecord` to reject any record whose `Source` is not `"cli"` or `"mcp"`** — returns a wrapped error so call-site bypass via bare `Record{}` literal is detectable rather than silently writing a malformed audit line. The empty-string normalization contract applies only to *readers* of pre-MCP audit logs, never to writers.
  - Export `AuditDir() (string, error)` as a thin wrapper around the existing private `auditDir()` so `internal/mcpsrv` can resolve the same directory without duplicating the env-var-then-UserConfigDir logic.
- Modify: `internal/cli/exec.go:41` — replace `rec := maskexec.Record{...}` with `rec := maskexec.NewRecord("cli")` and field assignments. Existing CLI behavior unchanged.
- Create: `internal/mcpsrv/audit.go` — exports `OpenAuditWriter() (*AuditWriter, error)` that opens `auditDir()/mcp_calls.jsonl` with `O_APPEND|O_CREATE|O_WRONLY|O_CLOEXEC`, mode 0600, and provides `(*AuditWriter).Write(rec McpCallRecord) error`. The writer holds the open `*os.File` for the lifetime of the server; reopens are unnecessary because each Write is single-syscall append-with-newline.
- Test: `internal/exec/auditlog_test.go` — adds:
  - Roundtrip of a `Record{Source: "mcp"}`; reader normalizes pre-MCP empty-source lines to `"cli"`.
  - **Writer rejection**: `WriteRecord(Record{Source: ""})` returns an error and writes nothing; `WriteRecord(Record{Source: "unknown"})` returns an error and writes nothing.
  - **AST drift check**: a Go test using `golang.org/x/tools/go/analysis` (or hand-rolled `go/parser` + `go/types`) walks all packages under `./...` and finds composite literals whose resolved type is `github.com/ching-kuo/opsmask/internal/exec.Record`. Any match outside `internal/exec` itself fails the test. This catches `maskexec.Record{...}` and any future alias the simple `git grep` would miss. The runtime `WriteRecord` rejection remains the primary guard; this test is drift prevention.
  - Multi-process append test: 4 subprocesses each append 100 records to `exec.log`; reader counts 400 valid JSONL lines.
  - `AuditDir()` test: returns the value of `OPSMASK_AUDIT_DIR` when set; falls back to `os.UserConfigDir()/opsmask` otherwise; matches the directory used by `WriteRecord`.
- Create: `internal/mcpsrv/audit_test.go`:
  - Single-process happy path.
  - Multi-process append test: same shape as the exec test, against `mcp_calls.jsonl`.
  - File mode 0600 enforced (test fails if `os.Stat` shows wider perms).
  - `OpenAuditWriter` errors when `auditDir()` exists with mode 0755.

**Approach:**
- `NewRecord(source string) Record` is the only sanctioned way to construct a `Record` going forward. Tests assert the field is non-empty for any record that reaches `WriteRecord`.
- `mcp_calls.jsonl` record shape: `{ts, source: "mcp", tool, args_summary, ok, err_class, result_size_bytes, duration_ms}`. `args_summary` carries only sizes and booleans, never content.
- Identical open-flag semantics to `exec.log` — line-sized writes are atomic on append-mode files under POSIX (and on Windows the existing test infrastructure already accepts `WRITE`-mode append). The tests prove the contract on Linux/macOS; Windows-specific behavior is documented but not asserted.

**Patterns to follow:**
- `internal/exec/auditlog.go:67-93` for `openAuditLog` flag set and permission checks.

**Test scenarios:**
- Happy path: `NewRecord("cli")` and `NewRecord("mcp")` produce records with `Ts` set and `Source` populated.
- Happy path (mcp writer): 100 single-process writes produce 100 readable JSONL lines.
- Happy path (exec writer): existing tests still pass after `NewRecord` refactor.
- Edge case: 4 subprocesses × 100 appends each → 400 lines in `mcp_calls.jsonl`, all valid JSON, no truncation. Same for `exec.log`.
- Edge case: empty `Source` decodes to empty string in `Record`; readers default to `"cli"` (covered by a small helper test).
- Edge case: `OPSMASK_AUDIT_DIR` set to a writable temp dir → both files land there.
- Error path: `auditDir()` parent unwritable → `OpenAuditWriter` returns wrapped error.
- Error path: pre-existing file with mode 0644 → `OpenAuditWriter` refuses with the same permission-tightening logic as `openAuditLog`.

**Verification:**
- All existing `internal/exec` and `internal/cli/exec` tests pass.
- New audit tests pass under `go test -race ./...`.
- Manual inspection: a CLI exec invocation produces `exec.log` lines with `"source":"cli"`; a not-yet-implemented MCP exec call would produce lines with `"source":"mcp"` (asserted in U5).

---

- U3. **Detect-only engine path, engine cancellation, `Store.Stats()`, mixed-process SQLite coverage**

**Goal:** Three engine/store changes that are prerequisites for any MCP tool work. (1) Add the side-effect-free detection function for `detect_text`. (2) Add ctx checks to `engine.Process`'s outer loop and `processSegment` so cancellation actually fires for large `mask_text` calls. (3) Add `Store.Stats(ctx)` to the interface and implement for SQLite. Plus the mixed-process SQLite test that validates concurrent CLI + long-lived MCP server safety.

**Requirements:** R2, R5, R8

**Dependencies:** U1

**Execution note:** Test-first for the detect-only path and the cancellation path — write the persistence-absence and ctx-respect assertions before the code changes.

**Files:**
- Decide and create: either `internal/engine/detect_only.go` exporting `engine.DetectOnly(ctx, r io.Reader, rules []detect.Rule, opts engine.Options) (engine.Stats, error)` OR `internal/detect/scan.go` exporting `detect.Scan(ctx context.Context, b []byte, rules []Rule) (Stats, error)`. Default during implementation: place it under `internal/engine` if it benefits from the existing `Options.MaxLine` streaming logic, else under `internal/detect`. Whichever placement, the function honors ctx the same way the cancellation work below does.
- Modify: `internal/engine/engine.go` — add ctx checks at four sites:
  1. The token-form probe loop at `internal/engine/engine.go:53-69` — runs *before* the outer streaming loop and is the first place a long input gets read. Add `select { case <-ctx.Done(): return stats, ctx.Err() ; default: }` before each `ch.Next()`.
  2. The outer streaming loop at `internal/engine/engine.go:71-97` — same pattern before each `ch.Next()` call.
  3. `processSegment` at `internal/engine/engine.go:100` — early ctx check before `detect.InertEscape`.
  4. `maskChunk` at `internal/engine/engine.go:118` — early ctx check before `detect.FindMatches` (which is CPU-bound on large rule sets).
  Existing `alloc.CommitPlans(ctx, ...)` ctx propagation at `internal/engine/engine.go:129` is preserved.
- **Cancellation contract caveat (document, do not try to fix in this unit).** Two limitations of ctx cancellation in `engine.Process` cannot be closed by inserting `select` checks alone:
  - `Chunker.Next()` reads via `bufio.Reader.ReadSlice` at `internal/ioutil/chunker.go:55`. A ctx check placed *before* the call does not interrupt an in-flight read on a generic `io.Reader`. The reader itself must support `Close()` or deadlines for an in-flight read to abort. For MCP `mask_text` the input is a `strings.Reader` over an in-memory string, so the read is non-blocking — this limitation is irrelevant. For `exec` the input pipes are closed when the subprocess is signaled, which unblocks the read.
  - `w.Write` at `internal/engine/engine.go:107` can block on a slow writer (`os.Pipe`, terminal, `net.Conn` without deadline). Ctx checks bracketing the write do not interrupt an in-flight `Write`. **Resolution: every MCP-side caller of `engine.Process` writes into a bounded in-memory `bytes.Buffer` it owns, never a network or pipe writer.** This is a precondition documented on the MCP tool handlers in U5 and verified by U5 tests. The cancellation test in this unit uses `io.Discard` or a `bytes.Buffer` writer specifically; CI bounds (~50 ms) are valid against in-memory writers, not against arbitrary blocking writers.
- Modify: `internal/store/store.go:18` — add `Stats(ctx context.Context) (StoreStats, error)` and a `StoreStats struct { Total int; ByType map[string]int }`.
- Modify: `internal/store/sqlite.go` — implement `Stats` with a single `SELECT type, COUNT(*) FROM mappings GROUP BY type` query.
- Search and modify: every other `store.Store` implementation or test double in the codebase (`grep -r "store.Store" --include='*.go'`) — add `Stats` to each, even if the implementation is a no-op for tests that do not exercise it.
- Test: `internal/engine/detect_only_test.go` (or `internal/detect/scan_test.go`):
  - Asserts that calling the function does not write to a paired SQLite store (open the store, call detect-only, close, reopen, assert row count unchanged).
  - Same input through `engine.Process` writes rows; through the detect-only path does not.
- Test: `internal/engine/cancellation_test.go`:
  - Construct a 100 MiB synthetic input with no matches; cancel the ctx mid-call; assert `engine.Process` returns within ~50 ms with `ctx.Err()` set (not after consuming the entire input).
  - Same shape for the detect-only path.
  - Construct an input with a high match density; cancel mid-call; same assertion (covers the `maskChunk` ctx-check path).
- Test: `internal/store/sqlite_stats_test.go` — empty store, single-type store, multi-type store.
- Test: `internal/store/concurrency_multiprocess_mixed_test.go` — long-lived "MCP-style" goroutine performs a continuous mix of `Insert`/`Lookup`/`List` while a separate subprocess hammers `Insert`. Asserts no truncation collisions, no SQLite busy errors past the existing retry budget, all reads observe a consistent post-write state. Skipped on Windows where multi-process file locking semantics differ. (Moved here from U1 — this is a general store concurrency test, not MCP-skeleton work.)

**Approach:**
- `engine.Stats` already exists; the detect-only function returns the same shape so the MCP tool handler can be agnostic to which path produced the counts.
- The SQL query is read-only and uses the existing prepared-statement pattern.
- Test doubles get an additive change only — no behavior shift to existing tests.

**Patterns to follow:**
- `internal/detect.FindMatches` is the read-only primitive both paths share.
- `internal/store/sqlite.go` `List` query for cursor/iteration patterns.

**Test scenarios:**
- Happy path (detect-only): input with 2 IPs and 1 email → returns `Stats{Masked: 3, ByType: {ip4: 2, email: 1}}`.
- Happy path (no persistence): same input through detect-only → SQLite store row count unchanged.
- Happy path (engine.Process for contrast): same input through `engine.Process` → SQLite store row count increased by 3.
- Happy path (Stats): empty store → `{Total: 0, ByType: {}}`. Store with 3 IPs and 2 emails → `{Total: 5, ByType: {ip4: 3, email: 2}}`.
- Edge case: multi-line input through detect-only respects `MaxLine` if the chosen function placement honors it; if not, document the divergence.
- Edge case: input with no matches → `Stats{}` zero-valued.
- Cancellation: large input (100 MiB synthetic) with cancelled ctx → returns `ctx.Err()` within ~50 ms; goroutine count back to baseline within 100 ms.
- Cancellation: high-match-density input + cancelled ctx → same bound; ensures the `maskChunk` ctx-check path is exercised.
- Cancellation: detect-only on the same large input → returns within bound.
- Mixed-process: long-lived goroutine + subprocess writers run for 10s with no errors and consistent reads (see test file above).
- Error path: cancelled context during `Stats` → returns `ctx.Err()`.

**Verification:**
- `go test ./...` passes.
- `git grep -F 'store.Store'` shows every type that satisfies the interface implements `Stats`.
- The new detect-only function has zero references to `pseudo.Allocator`.

---

- U4. **Extract shared exec orchestration; capture engine stats from `Run`**

**Goal:** Pull the inlined orchestration out of `internal/cli/exec.go:26-109` into a shared `internal/exec.Orchestrate` function. Refactor `internal/exec.Run` to capture `engine.Process` stats from both stdout and stderr streams. CLI exec becomes a thin wrapper. No new feature ships in this unit — pure refactor.

**Requirements:** R2, R4 (groundwork)

**Dependencies:** U1, U3 (Stats shape pattern can inform engine-stats capture, though not strictly required)

**Execution note:** Characterization tests first. Capture today's CLI exec behavior (every refusal mode, scope mode, and happy path) as table-driven tests **before** refactoring. The refactor must leave every existing test green with no behavior change.

**Files:**
- Modify: `internal/exec/run.go` — `RunResult` gains `Masked, Destroyed int` and `ByType map[string]int`. `maskStream` is reworked to capture and combine stats from stdout and stderr (sync.Mutex around a shared accumulator, or per-stream accumulators merged after both wait-groups return).
- Create: `internal/exec/orchestrate.go` — exports `func Orchestrate(ctx context.Context, rt *runtime.Env, source string, argv []string, opts OrchestrateOptions) (OrchestrateResult, error)`. Encapsulates: preflight, untrusted check, enabled check, scope-open refusal hook (parameterized so MCP can opt into it; CLI does not), resolve, evaluate-policy, build-env, run, write-record. Returns a structured result the caller can format.
- Modify: `internal/cli/exec.go` — `newExec` becomes a thin wrapper that calls `Orchestrate("cli", ...)` and prints stderr messages on the error variants.
- Test: `internal/cli/exec_characterization_test.go` — table-driven tests capturing every existing refusal class and the happy path **before** the refactor (Execution note above). These run unit-level (in-process).
- Test: `internal/cli/exec_subprocess_characterization_test.go` — **subprocess-level** characterization: invoke the `opsmask` binary via `os/exec` for each refusal class and assert the full external contract (exit code, stderr text format, audit log line shape). Unit-level tests cannot catch shifts in stderr-vs-stdout routing, signal handling, or audit timing relative to subprocess lifecycle.
- Test: `internal/exec/orchestrate_test.go` — exercises the same matrix against the new function directly, plus the new `source` parameter routes to the right audit value.

**Approach:**
- `OrchestrateOptions` carries: `Timeout`, `RefuseScopeOpen bool` (true for MCP, false for CLI), `Stdout io.Writer`, `Stderr io.Writer`, and any additional knobs MCP may need. **The orchestrator MUST receive the writers from the caller** — it never inherits `os.Stdout`/`os.Stderr` defaults and never accepts SDK-supplied writers. This makes the writer-ownership boundary explicit at the API level, so a future MCP-handler bug cannot accidentally pipe subprocess output back to the user's terminal or to a network writer that defeats ctx cancellation.
- The "scope-open refusal hook" implements the precise condition from `internal/exec/policy.go:56-58`: `cfg.Scope == ScopeFreeform AND (len(cfg.Allow) + len(BaselineAllow(cfg.Scope))) == 0`. Since `BaselineAllow(ScopeFreeform)` returns no entries (verify during implementation), the condition simplifies to `Scope == Freeform AND len(cfg.Allow) == 0`. If baseline does add freeform entries, use the full union check.
- `RunResult.Masked/Destroyed/ByType` are aggregated across both streams. Race-safety: each goroutine writes into its own local stats; main thread merges after `streamWg.Wait()`. No mutex needed.
- CLI exec characterization tests exist before any production code moves, so the refactor is provably behavior-preserving.

**Patterns to follow:**
- `internal/exec/policy_test.go` for table-driven gate-behavior coverage.
- `internal/exec/run.go:118-130` for the existing maskStream shape.

**Test scenarios:**
- Characterization: all existing CLI exec test scenarios reproduce identical outcomes (exit codes, audit records, stderr text) before and after the refactor.
- Happy path: `Orchestrate(ctx, rt, "cli", argv, opts)` with read-only scope and an allow-listed kubectl command runs successfully; audit record has `source: "cli"`.
- Happy path: `Orchestrate(ctx, rt, "mcp", argv, opts)` same → audit record has `source: "mcp"`.
- Refusal: `RefuseScopeOpen: true` + freeform scope + empty project allow-list → returns a typed `ErrScopeOpen`; audit record has `error_class: "scope_open_refused"`.
- Allow path: `RefuseScopeOpen: true` + freeform scope + non-empty project allow-list + matching command → runs successfully (legitimate workflow preserved).
- Stats capture: subprocess emits 2 IPs on stdout and 1 email on stderr → `RunResult.Masked == 3, ByType{ip4:2, email:1}`.
- Cancellation: `ctx` cancelled mid-run → child process terminated within the kill-grace window; `OrchestrateResult` includes the audit-finalized state.

**Verification:**
- All existing CLI exec tests pass unchanged.
- New orchestrator test matrix passes.
- `internal/cli/exec.go` is < 30 lines (signal that the orchestration body actually moved).

---

- U5. **MCP tools: `mask_text`, `detect_text`, `mapping_stats`, `list_detectors`, plus `exec`**

**Goal:** Register the five MCP tools using the relocated runtime, the detect-only path, the orchestrator, and the audit writer. This is the unit where MCP behavior first becomes externally observable.

**Requirements:** R3, R4, R5, R7, R8

**Dependencies:** U1, U2, U3, U4

**Execution note:** Treat `exec` as the highest blast-radius tool. Implement the four lower-risk tools first; add `exec` last with table-driven failure-mode coverage before the happy path.

**Files:**
- Create: `internal/mcpsrv/tools_text.go` — registers `mask_text` (calls `engine.Process`) and `detect_text` (calls the U3 detect-only function).
- Create: `internal/mcpsrv/tools_observe.go` — registers `mapping_stats` (calls `store.Stats`) and `list_detectors` (returns `rules.Builtins()` plus project rules from the runtime config).
- Create: `internal/mcpsrv/tool_exec.go` — registers `exec`. **Constructs two fresh bounded `bytes.Buffer` instances for stdout and stderr** (size cap from server config, default 4 MiB each — see input-cap discussion below) and passes them as `OrchestrateOptions.Stdout` / `OrchestrateOptions.Stderr`. Calls `exec.Orchestrate(ctx, rt, "mcp", argv, opts)`. Maps the typed errors to JSON-RPC error codes. After the orchestrator returns, projects the buffers into the MCP tool result's `stdout` / `stderr` fields. The handler never accepts a writer from the SDK or the request context, and never writes to `os.Stdout` / `os.Stderr` — those are reserved for server-level logging only.
- Modify: `internal/mcpsrv/server.go` — wire up the registrations.
- Test: `internal/mcpsrv/tools_text_test.go`:
  - `mask_text`: input → output sentinels; **persistence assertion** that mappings are now in the store.
  - `detect_text`: input → counts; **non-persistence assertion** that the store row count is unchanged.
  - Audit: every call writes one `mcp_calls.jsonl` line.
- Test: `internal/mcpsrv/tools_observe_test.go`:
  - `mapping_stats`: empty/single-type/multi-type cases mirror U3 tests but through the MCP layer.
  - `list_detectors`: builtins present, project rules appended after builtins.
- Test: `internal/mcpsrv/handler_validation_test.go` — covers handler-level input validation that fires *before* `Orchestrate` or `engine.Process`:
  - `exec`: empty `argv` → `INVALID_ARGS`; `argv` byte size > cap → `INPUT_TOO_LARGE`; `timeout` not a valid Go duration string → `INVALID_TIMEOUT`.
  - `mask_text` / `detect_text`: input `text` > `--max-text-bytes` → `INPUT_TOO_LARGE` (no `engine.Process` call made; assertion via fake engine that records call counts).
  - All four caps (`--max-text-bytes`, `--max-exec-output-bytes`, plus per-tool implicit limits) are exercised at the boundary.
- Test: `internal/mcpsrv/tool_exec_test.go` — table-driven, covers:
  - Happy path: read-only scope, allow-listed argv with one sentinel → masked output, exit 0, `source: "mcp"` in `exec.log`.
  - `EXEC_UNTRUSTED`, `EXEC_DISABLED`, `EXEC_SCOPE_OPEN_REFUSED` (freeform + empty allow), `EXEC_RESOLVE_FAILED`, `EXEC_POLICY_DENIED`, `EXEC_TIMEOUT`.
  - Allow path: freeform + non-empty allow-list + matching command → succeeds.
  - Cancellation: client closes the in-memory transport mid-`exec` → handler ctx cancels → child process terminated within kill-grace; audit record has `error_class: "cancelled"` (or the ctx-cancellation class).
  - Large output: subprocess emits >1 MiB on stdout → handler accumulates without deadlock; result string is bounded (default cap, configurable in a follow-up).
  - Sentinel inert-escape (cold tokens): subprocess output contains a literal sentinel-shaped string with an index that does NOT exist in the store → inert-escape path neutralizes it → agent receives the inert form. **Test both Unicode (`⟪…⟫`) and ASCII (`--MASK--…--`) token forms.**
  - Sentinel inert-escape (hot tokens, same-store): subprocess output emits a sentinel matching a real entry in this run's mapping store (e.g., a tool that legitimately echoes a previously-masked value) → the inert-escape pass at `internal/engine/engine.go:101` runs **before** detection, so hot tokens are also neutralized to inert form rather than re-resolved. Assert this for both token forms. The agent never sees the plaintext for that hot token through `exec`.
  - `mask_text` called with already-masked input from this same store: input contains live sentinels for known mappings → output passes through `engine.Process` inert-escape path; sentinels are escaped, not re-resolved. Assert via the round-trip behavior expected from `internal/cli/unmask.go:38-42`'s comment about the inert-escape contract.
  - Subprocess non-zero exit: tool call succeeds; result includes exit_code and re-masked stderr.

**Approach:**
- Tool descriptions are ≤200 chars each; total schema budget verified by a static test that marshals capabilities and asserts byte length.
- Cancellation: each handler accepts the SDK-supplied `ctx`; pass it through to `engine.Process`, `store.Stats`, `detect.FindMatches`, and `exec.Orchestrate`. The orchestrator already propagates ctx through `Resolve`, `Run`, and audit finalization (per U4).
- **Engine and exec writers are in-memory `*bytes.Buffer` only, constructed by the MCP handler.** `mask_text`, `detect_text`, and `exec` handlers each construct fresh bounded `bytes.Buffer` instances and pass them through to `engine.Process` (text tools) or `OrchestrateOptions.Stdout`/`Stderr` (exec tool). The buffer is projected into the MCP tool result after the call returns. **No MCP handler ever inherits or forwards a writer supplied by the SDK, the transport, the request context, or `os` standard streams.** This is a hard precondition for the cancellation contract from U3 — in-flight `Write` calls on network/pipe/terminal writers cannot be ctx-cancelled. A test asserts the writer type observed by `engine.Process` (and by `Run` for exec) is `*bytes.Buffer`, not just `io.Writer`. A second test uses a fake `Run` that records the concrete writer types it receives and fails if anything other than `*bytes.Buffer` shows up.
- **Default size caps lowered from 16 MiB to 4 MiB.** The CLI's 16 MiB default is *per-line* for streaming masking; an MCP handler holds the *total* result in memory and returns it in one JSON-RPC payload. JSON-RPC stdio framing imposes its own limits, and 4 MiB is comfortably within any current MCP client's parsing budget while still handling realistic log-snippet sizes. Caps are configurable via `mcp serve --max-text-bytes` and `--max-exec-output-bytes` flags (default 4 MiB each); inputs/outputs exceeding the cap return `INPUT_TOO_LARGE` / `OUTPUT_TRUNCATED` error codes.
- The `exec` handler returns a structured tool result with content blocks rather than embedded JSON-as-text. Subprocess stdout/stderr already pass through `engine.Process` into in-memory buffers owned by the orchestrator (`internal/exec.Run` post-U4), so the in-memory-writer precondition holds for `exec` automatically.
- Audit writes are best-effort but logged: if the writer fails, the tool call still returns successfully (with a stderr log to the server's process stderr) — failing the call because audit failed would be a stronger guarantee but introduces a new DOS surface (kill the audit log → kill the tools). Trade-off recorded in the risk table.

**Patterns to follow:**
- `internal/cli/exec.go` (post-U4) for the thin orchestration call shape.
- `internal/exec/policy_test.go` for the table-driven matrix shape.

**Test scenarios:**
- (See file list above for the exhaustive scenario list per tool.)
- Schema budget: marshaled capabilities byte length < 4096.
- Manual smoke (one-time): from a Claude Desktop / Claude Code session, list tools, call `mask_text` with one IPv4 string, verify masked output and audit record.

**Verification:**
- `go test ./internal/mcpsrv/... ./internal/exec/...` passes including all cancellation/large-output/inert-escape scenarios.
- `go test -race ./...` passes.

---

- U6. **MCP resource: `mapping/{type}` read-on-demand**

**Goal:** Register the mapping resource template. Enforce the no-plaintext / no-HMAC contract through tests, not just code review.

**Requirements:** R5, R7

**Dependencies:** U1

**Files:**
- Create: `internal/mcpsrv/resource_mapping.go` — registers a resource template at URI `opsmask://mapping/{type}` with optional query string `?limit=N` (default 50, max 500). Body: `{type, entries: [{token, length}], truncated}`.
- Modify: `internal/mcpsrv/server.go` — wire the registration; explicitly *not* advertise subscription support.
- Test: `internal/mcpsrv/resource_mapping_test.go`:
  - Happy path: 10 IPv4 mappings → `?limit=5` returns 5 entries, `truncated: true`. `?limit=20` returns 10, `truncated: false`.
  - Edge: unknown type → empty entries, `truncated: false`.
  - Edge: limit clamp — `?limit=1000` → returns at most 500.
  - **No-plaintext test:** seed the store with three plaintext fixture values that are unique substrings (e.g., `"ZZZ-PLAIN-MARKER-1"`); marshal the resource body; assert no marker appears anywhere in the output bytes.
  - **No-HMAC test (multi-encoding):** seed the store; for each `Mapping.HMACFull` value, assert the output bytes do not contain the HMAC in any of these encodings: raw bytes, lowercase hex, uppercase hex, standard base64, raw standard base64 (no padding), URL-safe base64, raw URL-safe base64. `Mapping.HMACFull` is `[]byte` at `internal/store/store.go:13`; an attacker probing the resource for cross-store correlation would try every common encoding.
  - Capability assertion: server capabilities show `resources.subscribe == false`.

**Approach:**
- The resource handler reads via `store.List(ctx, type, limit)` and projects each `Mapping` into the safe `(token, length)` pair. The token is the sentinel form (`opsmask:type:index`), not the raw HMAC.
- The handler never logs or returns `Mapping.RealValue` or `Mapping.HMACFull`.

**Patterns to follow:**
- `internal/store/sqlite.go` `List` for the iteration shape.
- `internal/detect/codec.go` for the canonical token form.

**Test scenarios:**
- (See file list above.)

**Verification:**
- All resource tests pass.
- A grep for `RealValue` in `internal/mcpsrv/resource_mapping.go` returns nothing.

---

- U7. **Distribution: README, Claude Desktop / Cursor / Claude Code config, GoReleaser, CHANGELOG**

**Goal:** Move from "the binary builds" to "a user can plug it into their MCP client in two minutes." Document the divergence from CloakMCP explicitly.

**Requirements:** R6

**Dependencies:** U1, U5, U6 (so the documented surface matches reality)

**Files:**
- Modify: `README.md` — new "MCP server" section after "Commands". Includes the Claude Desktop config snippet (with `which opsmask` guidance), Cursor `mcp.json` snippet, the explicit "no `unmask` over MCP — by design" callout, and the tool list table.
- Modify: `docs/competitive-analysis.md` — mark item 1 in "Feature gaps to consider" as shipped; update the feature matrix's "LLM-side integration" row for OpsMask from "Claude skill" to "MCP server (5 tools, 1 resource)".
- Modify or create: `.goreleaser.yaml` — verify it builds for `linux-amd64`, `linux-arm64`, `darwin-amd64`, `darwin-arm64`, `windows-amd64`. Add Homebrew tap stanza guarded behind a release-only env var.
- Modify: `CHANGELOG.md` — entry for the MCP server feature, including the deliberate `unmask` exclusion as a security note.
- Modify: `docs/REMAINING_RISKS.md` — entry for the MCP attack surface (tool poisoning, agent-as-caller).
- Test: none (documentation and build configuration). Manual smoke verification in the verification section.

**Approach:**
- README "MCP server" section structure:
  1. Two-sentence what / why.
  2. Quickstart with `which opsmask` + paste-into-config snippet.
  3. Tool list table.
  4. "What is *not* exposed and why" callout (`unmask`, `init`, `config trust` — CLI-only by design).
  5. Pointer to `docs/CUSTOM_DETECTORS.md`.
- Goreleaser: confirm existing config (if any) is already producing all five platform binaries; if absent, mirror `github/github-mcp-server` patterns.

**Patterns to follow:**
- `github/github-mcp-server` `.goreleaser.yaml`.
- The existing `docs/competitive-analysis.md` table format.

**Test scenarios:**
- Test expectation: none — documentation and build configuration. Manual smoke:
  - `goreleaser build --snapshot --clean` produces all five platform binaries.
  - README config snippet works verbatim in a real Claude Desktop installation.
  - README snippet works verbatim in Cursor.
  - `git grep -F 'unmask'` in README shows the exclusion is documented.

**Verification:**
- A new user, following only the README, can install OpsMask, configure a single MCP client, and successfully call `mask_text` end-to-end.
- Competitive-analysis matrix reflects the shipped state.

---

## System-Wide Impact

- **Interaction graph:** New `internal/mcpsrv` package depends on `internal/runtime`, `internal/engine`, `internal/store`, `internal/detect/rules`, and `internal/exec`. `internal/runtime` is the new shared dependency between `internal/cli` and `internal/mcpsrv`. No reverse dependencies.
- **Error propagation:** MCP tool errors map to JSON-RPC error responses with stable string codes (`EXEC_*`). Internal Go errors are not surfaced verbatim to the agent (avoid leaking absolute paths or other host details).
- **State lifecycle risks:** Mapping store writes from `mask_text` calls compete with concurrent CLI `opsmask mask` invocations and the long-lived MCP server's own writes. The existing `internal/store/concurrency_multiprocess_test.go` proves multi-subprocess insert safety; **U1 adds the missing coverage** for mixed long-lived + short-lived writers and reads-during-writes. Audit-log appends across processes are protected by POSIX `O_APPEND` semantics for line-sized writes (verified by U2 tests).
- **API surface parity:** The CLI is the canonical surface; MCP is a strict subset (no `unmask`, no `init`, no `config trust`). When new CLI subcommands ship, decide explicitly whether to expose them via MCP — default to **no** unless they meet the agent-safe bar (no plaintext-leakage path, no admin-privilege escalation).
- **Integration coverage:** U5's `tool_exec_test.go` table covers the cross-layer scenario where the existing exec policy gates fire from the MCP entrypoint via the shared orchestrator. U4's characterization tests prove the CLI behavior is unchanged through the refactor.
- **Cancellation lifecycle:** MCP request ctx is propagated through every long-running call site (`engine.Process`, `detect.FindMatches`, `store.Stats`, `exec.Orchestrate`). On client disconnect, in-flight subprocesses receive process-group SIGTERM with a kill-grace fallback (existing `Run` behavior, exercised by U5 cancellation tests).
- **Unchanged invariants:** TTY-gate on `unmask`, config-trust path+SHA-256 binding, exec allow-list semantics, audit log preflight, sentinel inert-escape decoding. None are altered. The `Source` field on `exec.Record` and the new `Stats` method on `Store` are additive; readers/implementations that ignore them are unaffected.

---

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| Tool-poisoning attack coerces an agent to call `exec` with a malicious sentinel-bearing argv. | Existing allow-list and deny-base layers refuse the call. New scope-open refusal closes the residual case where freeform paired with an empty allow-list would have permitted everything. Audit log captures the attempt. |
| Schema budget bloat as we add more tools over time. | Hard cap: tool count stays ≤8 across all future iterations; tool descriptions stay ≤200 chars; tool result sizes are summarized (counts, not content) by default. U5 includes a static byte-length test on marshaled capabilities. |
| Token-result poisoning from `exec` (the subprocess returns adversarial content that the agent interprets as instructions). | `engine.Process` re-masks the output, so any embedded sentinels are decoded inertly; raw plaintext that survives masking still flows back. This is the same risk surface as the CLI today, not a new MCP-specific risk. Documented in README. |
| Official Go SDK API churn breaks the build. | Pin to a specific `1.x` release in `go.mod`; update on a deliberate cadence. |
| SQLite contention between long-lived MCP server and concurrent CLI processes. | U1 adds a mixed-process test covering exactly this case. The existing single-shape multi-subprocess test is preserved. **Risk rating: partially covered after U1 lands**; a real production user could still hit edge cases the test does not exercise. Document in `docs/REMAINING_RISKS.md`. |
| Audit-write failure causes silent forensic gap (`exec.log`). | **Fail closed.** `exec.Preflight` already refuses to run a subprocess when `exec.log` is unwritable; U4's `Orchestrate` preserves this for both CLI and MCP call paths. An attacker who fills the audit directory cannot cause `exec` to run un-audited; they can only cause the tool to refuse. |
| Audit-write failure causes silent forensic gap (`mcp_calls.jsonl`). | **Fail open; degraded status logged to server stderr only — never exposed via MCP.** Non-exec MCP tool calls (`mask_text`, `detect_text`, `mapping_stats`, `list_detectors`, resource reads) continue serving on audit-write failure. The server logs every failure to its own stderr (visible to the launching client's stderr capture, systemd journal, etc.). **No MCP tool or resource exposes any audit-failure signal — not a count, not a sticky boolean, not a delta.** Exposing even a coarse signal would let an attacker who controls fill capacity confirm the attack succeeded. The asymmetry between exec (fail-closed) and non-exec audit (fail-open, stderr-only) is deliberate: bulk masking has lower forensic value per call and a higher denial-of-service surface than subprocess execution. |
| MCP request cancellation leaves orphaned subprocesses. | `exec.Orchestrate` propagates ctx; existing `Run` already handles SIGTERM/SIGKILL grace. U5 explicitly tests client-disconnect mid-exec. |
| Resource subscription expectation if a client assumes change notifications. | Capabilities explicitly advertise `subscribe: false`. README documents the read-on-demand contract. |
| Users misconfigure their MCP client (relative path, wrong binary location). | README documents `which opsmask` and explicitly warns that `PATH` is unreliable in client subprocesses. |
| `unmask`-shaped feature requests from users who don't understand the divergence. | README has a dedicated callout. CHANGELOG explains the rationale. |
| MCP supply-chain attack (a malicious package masquerading as `opsmask`). | Single-binary distribution via signed GoReleaser artifacts; document the canonical install paths in README; do not publish to npm or pip where the typo-squat risk is highest. |
| `Source` field default leaks (a future call site forgets to set it, audit records get empty `source`). | Two layers of defense, both delivered in U2: (1) **Runtime enforcement** — `WriteRecord` rejects any record whose `Source` is not `"cli"` or `"mcp"`, so an empty source produces a write-time error rather than a malformed audit line. (2) **AST drift analyzer** — a Go test using `golang.org/x/tools/go/analysis` walks all packages and finds composite literals whose resolved type is `internal/exec.Record`; matches outside `internal/exec` itself fail the test, catching aliased construction (e.g., `maskexec.Record{...}`) that simple grep would miss. Reader normalization continues to treat empty as `"cli"` for pre-MCP audit logs only. |

---

## Documentation / Operational Notes

- README: new "MCP server" section (covered in U7).
- `docs/competitive-analysis.md`: update feature matrix; mark item 1 in "Feature gaps to consider" as shipped (covered in U7).
- `CHANGELOG.md`: entry for v1 MCP server, including the deliberate `unmask` exclusion.
- `docs/CUSTOM_DETECTORS.md`: add a one-liner noting that custom detectors flow through MCP identically once `config trust` is in place.
- `docs/REMAINING_RISKS.md`: entries for MCP attack surface, mixed-process SQLite contention, audit-write failure trade-off.
- Operational rollout: pre-1.0 binary, no feature flag needed. Monitor GitHub issues for the first 2 weeks for misconfiguration patterns.

### PR Shaping

The 7 implementation units do not have to land as 7 PRs. A reasonable shaping is:

- **PR 1 — pre-MCP refactor (U3 + U4).** Detect-only engine path, engine cancellation, `Store.Stats()`, mixed-process SQLite test, exec orchestration extraction, `Run` stats capture. Zero new external surface; all behavior-preserving for the CLI. Smallest review surface, biggest dependency-graph payoff.
- **PR 2 — MCP scaffolding (U1 + U2).** Runtime relocation, `mcp serve` skeleton, audit primitives, `Source` field. Adds the SDK dependency and the `mcp` subcommand but no tools yet.
- **PR 3 — MCP tools and resource (U5 + U6).** All five tools and the mapping resource. The user-facing feature lands here.
- **PR 4 — distribution (U7).** README, CHANGELOG, GoReleaser, competitive-analysis update.

This is a recommendation, not a requirement — single-PR delivery is fine for a solo maintainer; multi-PR is recommended if review is being shared.

---

## Sources & References

- Related code: `internal/cli/helpers.go:13`, `internal/cli/exec.go:26`, `internal/exec/run.go:29`, `internal/exec/auditlog.go:16`, `internal/exec/policy.go:53-69`, `internal/engine/engine.go:129`, `internal/store/store.go:18`, `internal/store/concurrency_multiprocess_test.go`, `internal/detect/rules/builtin.go:19`, `internal/detect/codec.go`
- Related docs: `docs/competitive-analysis.md`, `docs/CUSTOM_DETECTORS.md`, `docs/REMAINING_RISKS.md`
- External: MCP spec 2025-03-26, official Go SDK, CloakMCP repository, Invariant Labs and OWASP MCP01 attack research, GitHub MCP server reference implementation (URLs in Context & Research above)
