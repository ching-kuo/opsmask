package exec

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/ching-kuo/opsmask/internal/config"
)

type HookOptions struct {
	Cwd     string
	Timeout string
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
}

type HookResult = OrchestrateResult

// OrchestrateHook runs a Claude Code hook command through bash while reusing
// sentinel resolution, environment shaping, process-group handling, masking,
// and audit logging. It intentionally skips the regular exec policy gates.
func OrchestrateHook(ctx context.Context, rt Runtime, command string, opts HookOptions) (HookResult, error) {
	if command == "" {
		return HookResult{}, wrapErr(ErrPolicyDenied, "empty command")
	}
	if err := Preflight(); err != nil {
		return HookResult{}, &wrappedOrchestrate{kind: ErrAuditUnwritable, detail: err.Error()}
	}

	unresolved := []string{"bash", "-c", command}
	rec := NewRecord(SourceHook)
	rec.Cwd = opts.Cwd
	rec.Argv = append([]string(nil), unresolved...)
	rec.Scope = string(config.ScopeFreeform)
	rec.Executable = "bash"
	res := HookResult{Audit: rec}

	finalize := func(class string, exitCode int) {
		res.Audit.ErrorClass = class
		res.Audit.ExitCode = exitCode
		res.ErrorClass = class
		res.ExitCode = exitCode
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

	resolved, err := Resolve(ctx, unresolved, func(typ, idx string) ([]byte, bool, error) {
		return rt.Store.Lookup(ctx, typ, idx)
	})
	if err != nil {
		res.Audit.DenyMatch = err.Error()
		finalize("resolve", 125)
		_ = WriteRecord(res.Audit)
		return res, wrapErr(ErrResolve, "%s", err.Error())
	}

	env := BuildEnv(config.ScopeFreeform, rt.Cfg, nil)
	res.Audit.EnvAllowCount = len(env.Env)
	res.Audit.EnvDenyCount = env.DenyCount
	res.Audit.DenyOptOut = rt.Cfg.DenyOptOut

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
	if werr := WriteRecord(res.Audit); werr != nil {
		return res, fmt.Errorf("audit write failed: %w", werr)
	}
	return res, nil
}
