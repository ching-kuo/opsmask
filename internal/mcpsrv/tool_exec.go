package mcpsrv

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	maskexec "github.com/ching-kuo/opsmask/internal/exec"
	"github.com/ching-kuo/opsmask/internal/runtime"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ExecInput is the input schema for the exec tool.
type ExecInput struct {
	Argv    []string `json:"argv" jsonschema:"command and arguments to run"`
	Timeout string   `json:"timeout,omitempty" jsonschema:"optional timeout duration like 30s or 2m"`
}

// ExecOutput mirrors the relevant subset of OrchestrateResult plus the
// re-masked stdout/stderr captured by the engine.
type ExecOutput struct {
	ExitCode   int            `json:"exit_code"`
	Stdout     string         `json:"stdout"`
	Stderr     string         `json:"stderr"`
	DurationMs int64          `json:"duration_ms"`
	Masked     int            `json:"masked"`
	Destroyed  int            `json:"destroyed"`
	ByType     map[string]int `json:"by_type,omitempty"`
}

func registerExecTool(srv *mcp.Server, rt *runtime.Env, audit AuditWriter, caps Caps) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "exec",
		Description: "Run a follow-up command. Honors project allow-list, deny-base, and re-masks output.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ExecInput) (*mcp.CallToolResult, ExecOutput, error) {
		start := time.Now()
		out, err := handleExec(ctx, rt, in, caps)
		writeAudit(audit, McpCallRecord{
			Tool:            "exec",
			ArgsSummary:     map[string]any{"argv_len": len(in.Argv), "timeout": in.Timeout},
			OK:              err == nil && out.ExitCode == 0,
			ErrClass:        errClassExec(err, out),
			ResultSizeBytes: len(out.Stdout) + len(out.Stderr),
			DurationMs:      time.Since(start).Milliseconds(),
		})
		return nil, out, err
	})
}

func handleExec(ctx context.Context, rt *runtime.Env, in ExecInput, caps Caps) (ExecOutput, error) {
	if len(in.Argv) == 0 {
		return ExecOutput{}, errors.New("INVALID_ARGS: argv is empty")
	}
	totalArgvBytes := 0
	for _, a := range in.Argv {
		totalArgvBytes += len(a)
	}
	if totalArgvBytes > caps.MaxTextBytes {
		return ExecOutput{}, fmt.Errorf("INPUT_TOO_LARGE: argv=%d bytes exceeds cap=%d", totalArgvBytes, caps.MaxTextBytes)
	}
	if in.Timeout != "" {
		// Validate before invoking Orchestrate so we surface a stable code.
		if _, err := time.ParseDuration(in.Timeout); err != nil {
			return ExecOutput{}, fmt.Errorf("INVALID_TIMEOUT: %v", err)
		}
	}

	// Bounded in-memory writers — the cancellation contract requires this.
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	rtEnv := maskexec.Runtime{
		Store:     rt.Store,
		Alloc:     rt.Alloc,
		Rules:     rt.Rules,
		Untrusted: rt.Loaded.Untrusted,
		Cfg:       rt.Loaded.ProjectExec,
	}
	res, err := maskexec.Orchestrate(ctx, rtEnv, in.Argv, maskexec.OrchestrateOptions{
		Source:          maskexec.SourceMCP,
		Timeout:         in.Timeout,
		RefuseScopeOpen: true,
		Stdin:           strings.NewReader(""),
		Stdout:          stdout,
		Stderr:          stderr,
	})
	out := ExecOutput{
		ExitCode:   res.ExitCode,
		Stdout:     truncateString(stdout.String(), caps.MaxExecOutputBytes),
		Stderr:     truncateString(stderr.String(), caps.MaxExecOutputBytes),
		DurationMs: res.Duration.Milliseconds(),
		Masked:     res.Masked,
		Destroyed:  res.Destroyed,
		ByType:     res.ByType,
	}
	if err != nil {
		return out, mapOrchestrateError(err)
	}
	return out, nil
}

func mapOrchestrateError(err error) error {
	switch {
	case errors.Is(err, maskexec.ErrAuditUnwritable):
		return errors.New("EXEC_AUDIT_UNWRITABLE")
	case errors.Is(err, maskexec.ErrUntrusted):
		return errors.New("EXEC_UNTRUSTED")
	case errors.Is(err, maskexec.ErrDisabled):
		return errors.New("EXEC_DISABLED")
	case errors.Is(err, maskexec.ErrScopeOpen):
		return errors.New("EXEC_SCOPE_OPEN_REFUSED")
	case errors.Is(err, maskexec.ErrTimeoutParse):
		return errors.New("INVALID_TIMEOUT")
	case errors.Is(err, maskexec.ErrResolve):
		return errors.New("EXEC_RESOLVE_FAILED")
	case errors.Is(err, maskexec.ErrPolicyDenied):
		return errors.New("EXEC_POLICY_DENIED")
	}
	return fmt.Errorf("EXEC_INTERNAL: %v", err)
}

func errClassExec(err error, out ExecOutput) string {
	if err != nil {
		return errClass(err)
	}
	if out.ExitCode != 0 {
		return "non_zero"
	}
	return ""
}

func truncateString(s string, cap int) string {
	if cap <= 0 || len(s) <= cap {
		return s
	}
	return s[:cap]
}
