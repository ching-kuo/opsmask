package exec

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/ching-kuo/opsmask/internal/config"
	"github.com/ching-kuo/opsmask/internal/detect"
	"github.com/ching-kuo/opsmask/internal/pseudo"
	"github.com/ching-kuo/opsmask/internal/store"
)

// OrchestrateError is the typed-error category surfaced to the caller after a
// refusal. CLI maps each value to a distinct exit-2-class stderr message; the
// MCP exec tool maps each value to a stable JSON-RPC error code.
type OrchestrateError string

const (
	ErrAuditUnwritable OrchestrateError = "EXEC_AUDIT_UNWRITABLE"
	ErrUntrusted       OrchestrateError = "EXEC_UNTRUSTED"
	ErrDisabled        OrchestrateError = "EXEC_DISABLED"
	ErrScopeOpen       OrchestrateError = "EXEC_SCOPE_OPEN_REFUSED"
	ErrTimeoutParse    OrchestrateError = "EXEC_TIMEOUT_INVALID"
	ErrResolve         OrchestrateError = "EXEC_RESOLVE_FAILED"
	ErrPolicyDenied    OrchestrateError = "EXEC_POLICY_DENIED"
)

func (e OrchestrateError) Error() string { return string(e) }

// wrappedOrchestrate carries an OrchestrateError plus a diagnostic detail
// string. errors.Is unwraps to the typed kind; callers should match on the
// kind, not the message.
type wrappedOrchestrate struct {
	kind   OrchestrateError
	detail string
}

func (w *wrappedOrchestrate) Error() string {
	if w.detail == "" {
		return string(w.kind)
	}
	return string(w.kind) + ": " + w.detail
}
func (w *wrappedOrchestrate) Unwrap() error { return w.kind }

func wrapErr(kind OrchestrateError, format string, a ...any) error {
	return &wrappedOrchestrate{kind: kind, detail: fmt.Sprintf(format, a...)}
}

// OrchestrateOptions is the per-call input. Stdin/Stdout/Stderr are owned by
// the caller — the orchestrator never inherits os.Stdout/os.Stderr defaults.
// MCP handlers MUST pass *bytes.Buffer instances so engine.Process never
// blocks on a network or pipe writer that ctx cancellation cannot interrupt.
type OrchestrateOptions struct {
	Source  string // "cli" or "mcp"; passed straight into the audit Record
	Cwd     string // recorded in the audit log
	Timeout string // optional; parsed via time.ParseDuration
	// RefuseScopeOpen rejects calls when policy would grant unrestricted
	// execution (scope=freeform with no allow-list entries). MCP sets this
	// true; CLI sets it false because a local human who wrote a trusted
	// freeform config has already accepted that surface.
	RefuseScopeOpen bool
	Stdin           io.Reader
	Stdout          io.Writer
	Stderr          io.Writer
}

// OrchestrateResult is the per-call output. Audit holds the record that was
// (or would have been) written; the caller can inspect it for telemetry.
type OrchestrateResult struct {
	ExitCode   int
	Duration   time.Duration
	ErrorClass string
	Masked     int
	Destroyed  int
	ByType     map[string]int
	Audit      Record
}

// Runtime is the minimal subset of runtime.Env that Orchestrate needs.
// internal/runtime is forbidden from importing internal/exec to avoid a cycle;
// instead the orchestrator depends on this small interface, which the runtime
// package's Env satisfies structurally.
type Runtime struct {
	Store     store.Store
	Alloc     *pseudo.Allocator
	Rules     []detect.Rule
	Untrusted bool
	Cfg       config.ExecConfig
}

// Orchestrate runs the full exec pipeline: audit preflight, untrusted/disabled
// checks, optional scope-open refusal, sentinel resolution, policy evaluation,
// env shaping, subprocess execution. Audit record is finalized and written
// regardless of which stage produced the outcome.
//
// On any pre-Run refusal the result includes an Audit record and one of the
// typed OrchestrateError values via errors.Is. On Run errors the subprocess
// exit code propagates through OrchestrateResult.ExitCode.
func Orchestrate(ctx context.Context, rt Runtime, argv []string, opts OrchestrateOptions) (OrchestrateResult, error) {
	if len(argv) == 0 {
		return OrchestrateResult{}, wrapErr(ErrPolicyDenied, "empty argv")
	}
	if opts.Source != SourceCLI && opts.Source != SourceMCP {
		return OrchestrateResult{}, fmt.Errorf("orchestrate: invalid source %q", opts.Source)
	}
	if err := Preflight(); err != nil {
		return OrchestrateResult{}, &wrappedOrchestrate{kind: ErrAuditUnwritable, detail: err.Error()}
	}

	rec := NewRecord(opts.Source)
	rec.Cwd = opts.Cwd
	rec.Argv = append([]string(nil), argv...)
	rec.Scope = string(rt.Cfg.Scope)
	rec.Executable = argv[0]
	res := OrchestrateResult{Audit: rec}

	finalize := func(class string, exitCode int) {
		res.Audit.ErrorClass = class
		res.Audit.ExitCode = exitCode
		res.ErrorClass = class
		res.ExitCode = exitCode
	}

	if rt.Untrusted {
		finalize("untrusted", 125)
		_ = WriteRecord(res.Audit)
		return res, ErrUntrusted
	}
	if !rt.Cfg.Enabled {
		finalize("disabled", 125)
		_ = WriteRecord(res.Audit)
		return res, ErrDisabled
	}

	timeout := rt.Cfg.DefaultTimeout
	if opts.Timeout != "" {
		d, err := time.ParseDuration(opts.Timeout)
		if err != nil {
			finalize("wrapper", 125)
			_ = WriteRecord(res.Audit)
			return res, wrapErr(ErrTimeoutParse, "invalid timeout %q: %v", opts.Timeout, err)
		}
		timeout = d
	}

	// MCP-specific tightening: refuse when policy would grant unrestricted
	// execution. Per policy.go:56-58, that is `Scope == freeform AND merged
	// allow-list is empty`. A freeform scope paired with a real allow-list is
	// a legitimate workflow and stays allowed even with RefuseScopeOpen=true.
	if opts.RefuseScopeOpen && rt.Cfg.Scope == config.ScopeFreeform {
		merged := len(rt.Cfg.Allow) + len(BaselineAllow(rt.Cfg.Scope))
		if merged == 0 {
			finalize("scope_open_refused", 125)
			_ = WriteRecord(res.Audit)
			return res, ErrScopeOpen
		}
	}

	resolved, err := Resolve(ctx, argv, func(typ, idx string) ([]byte, bool, error) {
		return rt.Store.Lookup(ctx, typ, idx)
	})
	if err != nil {
		res.Audit.DenyMatch = err.Error()
		finalize("resolve", 125)
		_ = WriteRecord(res.Audit)
		return res, wrapErr(ErrResolve, "%s", err.Error())
	}

	decision := EvaluatePolicy(resolved, rt.Cfg)
	res.Audit.AllowMatch = decision.AllowMatch
	res.Audit.DenyMatch = decision.DenyMatch
	if !decision.Allowed {
		finalize(decision.ErrorClass, 125)
		_ = WriteRecord(res.Audit)
		return res, &wrappedOrchestrate{kind: ErrPolicyDenied, detail: decision.Reason}
	}

	env := BuildEnv(rt.Cfg.Scope, rt.Cfg, nil)
	res.Audit.EnvAllowCount = len(env.Env)
	res.Audit.EnvDenyCount = env.DenyCount
	res.Audit.DenyOptOut = rt.Cfg.DenyOptOut

	// Fail-closed pre-execution audit. Preflight only proves the file was
	// openable when Orchestrate started; an actual write may still fail
	// (disk full, permission flip, file deletion). Writing the
	// "starting" record now refuses to run the subprocess if the audit
	// stream is broken at the moment we'd need it. The post-execution
	// record below records the outcome.
	starting := res.Audit
	starting.ErrorClass = "starting"
	if werr := WriteRecord(starting); werr != nil {
		finalize("audit_write_failed", 125)
		return res, &wrappedOrchestrate{kind: ErrAuditUnwritable, detail: werr.Error()}
	}

	run := Run(ctx, resolved, RunOptions{
		Env:     env.Env,
		Stdin:   opts.Stdin,
		Stdout:  opts.Stdout,
		Stderr:  opts.Stderr,
		Timeout: timeout,
		Rules:   rt.Rules,
		Alloc:   rt.Alloc,
	})

	res.ExitCode = run.ExitCode
	res.Duration = run.Duration
	res.ErrorClass = run.ErrorClass
	res.Masked = run.Masked
	res.Destroyed = run.Destroyed
	res.ByType = run.ByType
	res.Audit.ExitCode = run.ExitCode
	res.Audit.DurationMs = run.Duration.Milliseconds()
	res.Audit.ErrorClass = run.ErrorClass
	// Post-execution record may fail (disk filled mid-Run); the
	// pre-execution "starting" record is already on disk so forensic
	// reconstruction stays possible. Surface the failure as a soft error.
	if werr := WriteRecord(res.Audit); werr != nil {
		return res, fmt.Errorf("audit write failed: %w", werr)
	}
	return res, nil
}
