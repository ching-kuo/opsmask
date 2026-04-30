package mcpsrv

import (
	"bytes"
	"context"
	"encoding/json"
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
//
// ErrorCode carries a stable string discriminant on failure (one of the
// EXEC_* / INVALID_* / INPUT_TOO_LARGE constants below). Clients should
// branch on ErrorCode rather than parsing the SDK's text-content error
// message — the message wording may change but the code will not.
type ExecOutput struct {
	ExitCode   int            `json:"exit_code"`
	Stdout     string         `json:"stdout"`
	Stderr     string         `json:"stderr"`
	DurationMs int64          `json:"duration_ms"`
	Masked     int            `json:"masked"`
	Destroyed  int            `json:"destroyed"`
	ByType     map[string]int `json:"by_type,omitempty"`
	ErrorCode  string         `json:"error_code,omitempty"`
}

// Stable error codes returned in ExecOutput.ErrorCode and the SDK error
// text. Clients should match against these constants rather than parsing
// the human-readable message that may follow.
const (
	ErrCodeInvalidArgs        = "INVALID_ARGS"
	ErrCodeInputTooLarge      = "INPUT_TOO_LARGE"
	ErrCodeInvalidTimeout     = "INVALID_TIMEOUT"
	ErrCodeAuditUnwritable    = "EXEC_AUDIT_UNWRITABLE"
	ErrCodeUntrusted          = "EXEC_UNTRUSTED"
	ErrCodeDisabled           = "EXEC_DISABLED"
	ErrCodeScopeOpenRefused   = "EXEC_SCOPE_OPEN_REFUSED"
	ErrCodeResolveFailed      = "EXEC_RESOLVE_FAILED"
	ErrCodePolicyDenied       = "EXEC_POLICY_DENIED"
	ErrCodeInternal           = "EXEC_INTERNAL"
)

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
		if err != nil {
			// Build the error result manually so structuredContent (carrying
			// ExecOutput.ErrorCode) survives. Returning a Go error to the SDK
			// produces a text-only error response with no structured payload,
			// forcing clients to string-parse — that's what we are fixing.
			return execErrorResult(out, err), out, nil
		}
		return nil, out, nil
	})
}

func handleExec(ctx context.Context, rt *runtime.Env, in ExecInput, caps Caps) (ExecOutput, error) {
	if len(in.Argv) == 0 {
		return execFailure(ErrCodeInvalidArgs, "argv is empty")
	}
	totalArgvBytes := 0
	for _, a := range in.Argv {
		totalArgvBytes += len(a)
	}
	if totalArgvBytes > caps.MaxTextBytes {
		return execFailure(ErrCodeInputTooLarge, fmt.Sprintf("argv=%d bytes exceeds cap=%d", totalArgvBytes, caps.MaxTextBytes))
	}
	if in.Timeout != "" {
		// Validate before invoking Orchestrate so we surface a stable code.
		if _, err := time.ParseDuration(in.Timeout); err != nil {
			return execFailure(ErrCodeInvalidTimeout, err.Error())
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
		code := classifyOrchestrateError(err)
		out.ErrorCode = code
		return out, errors.New(code)
	}
	return out, nil
}

// execFailure returns an ExecOutput with ErrorCode populated and a Go error
// whose Error() string is the stable code. The SDK marks the result as an
// error and serializes both the code (in structured content) and the human
// message (in text content); clients should branch on ErrorCode.
func execFailure(code, detail string) (ExecOutput, error) {
	if detail == "" {
		return ExecOutput{ErrorCode: code}, errors.New(code)
	}
	return ExecOutput{ErrorCode: code}, fmt.Errorf("%s: %s", code, detail)
}

// classifyOrchestrateError maps a typed Orchestrate error to the stable
// ErrCode* constant. Any unknown wrapping falls through to ErrCodeInternal.
func classifyOrchestrateError(err error) string {
	switch {
	case errors.Is(err, maskexec.ErrAuditUnwritable):
		return ErrCodeAuditUnwritable
	case errors.Is(err, maskexec.ErrUntrusted):
		return ErrCodeUntrusted
	case errors.Is(err, maskexec.ErrDisabled):
		return ErrCodeDisabled
	case errors.Is(err, maskexec.ErrScopeOpen):
		return ErrCodeScopeOpenRefused
	case errors.Is(err, maskexec.ErrTimeoutParse):
		return ErrCodeInvalidTimeout
	case errors.Is(err, maskexec.ErrResolve):
		return ErrCodeResolveFailed
	case errors.Is(err, maskexec.ErrPolicyDenied):
		return ErrCodePolicyDenied
	}
	return ErrCodeInternal
}

func errClassExec(err error, out ExecOutput) string {
	if out.ErrorCode != "" {
		return out.ErrorCode
	}
	if err != nil {
		return classifyOrchestrateError(err)
	}
	if out.ExitCode != 0 {
		return "non_zero"
	}
	return ""
}

// execErrorResult constructs a CallToolResult that carries both human-
// readable text content (the error message) and structured content
// (ExecOutput, including ErrorCode). The SDK marshals the typed output via
// json so clients receive a stable `error_code` field they can branch on.
func execErrorResult(out ExecOutput, err error) *mcp.CallToolResult {
	structured, mErr := json.Marshal(out)
	if mErr != nil {
		structured = []byte("{}")
	}
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
		StructuredContent: json.RawMessage(structured),
	}
}

func truncateString(s string, cap int) string {
	if cap <= 0 || len(s) <= cap {
		return s
	}
	return s[:cap]
}
