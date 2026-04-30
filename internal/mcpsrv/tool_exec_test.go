package mcpsrv_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ching-kuo/opsmask/internal/config"
	maskexec "github.com/ching-kuo/opsmask/internal/exec"
	"github.com/ching-kuo/opsmask/internal/mcpsrv"
	mcpruntime "github.com/ching-kuo/opsmask/internal/runtime"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// configuredRuntime returns an MCP server already wired against a runtime
// whose ProjectExec config matches `cfg`. Audit dir lives in a tmp dir.
func configuredServer(t *testing.T, cfg config.ExecConfig, untrusted bool) (*mcp.ClientSession, *mcpruntime.Env, func()) {
	t.Helper()
	auditDir := t.TempDir()
	if err := os.Chmod(auditDir, 0o700); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Setenv("OPSMASK_AUDIT_DIR", auditDir)

	rt := newTestRuntime(t)
	rt.Loaded.ProjectExec = cfg
	rt.Loaded.Untrusted = untrusted

	srv := mcpsrv.NewServerWithCaps(rt, nil, mcpsrv.DefaultCaps())
	clientT, serverT := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

	go func() { _ = srv.Run(ctx, serverT) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	sess, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		cancel()
		t.Fatalf("Connect: %v", err)
	}
	return sess, rt, func() { _ = sess.Close(); cancel() }
}

func callExec(t *testing.T, sess *mcp.ClientSession, argv []string) (*mcp.CallToolResult, mcpsrv.ExecOutput) {
	t.Helper()
	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "exec",
		Arguments: map[string]any{"argv": argv},
	})
	if err != nil {
		t.Fatalf("CallTool exec: %v", err)
	}
	var out mcpsrv.ExecOutput
	if res.StructuredContent != nil {
		raw, _ := json.Marshal(res.StructuredContent)
		_ = json.Unmarshal(raw, &out)
	}
	return res, out
}

func TestExecRefusesUntrusted(t *testing.T) {
	cfg := config.ExecConfig{Enabled: true, Scope: config.ScopeReadOnly}
	sess, _, cleanup := configuredServer(t, cfg, true)
	defer cleanup()
	res, out := callExec(t, sess, []string{"echo", "hi"})
	if !res.IsError {
		t.Fatal("expected error result")
	}
	if out.ErrorCode != mcpsrv.ErrCodeUntrusted {
		t.Fatalf("ErrorCode = %q, want %q", out.ErrorCode, mcpsrv.ErrCodeUntrusted)
	}
	body, _ := json.Marshal(res.Content)
	if !strings.Contains(string(body), "EXEC_UNTRUSTED") {
		t.Fatalf("body = %s, want EXEC_UNTRUSTED", body)
	}
}

func TestExecRefusesDisabled(t *testing.T) {
	cfg := config.ExecConfig{Enabled: false, Scope: config.ScopeReadOnly}
	sess, _, cleanup := configuredServer(t, cfg, false)
	defer cleanup()
	res, out := callExec(t, sess, []string{"echo", "hi"})
	if !res.IsError || out.ErrorCode != mcpsrv.ErrCodeDisabled {
		t.Fatalf("ErrorCode = %q want %q", out.ErrorCode, mcpsrv.ErrCodeDisabled)
	}
}

func TestExecRefusesEmptyArgv(t *testing.T) {
	cfg := config.ExecConfig{Enabled: true, Scope: config.ScopeReadOnly}
	sess, _, cleanup := configuredServer(t, cfg, false)
	defer cleanup()
	res, out := callExec(t, sess, []string{})
	if !res.IsError || out.ErrorCode != mcpsrv.ErrCodeInvalidArgs {
		t.Fatalf("ErrorCode = %q want %q", out.ErrorCode, mcpsrv.ErrCodeInvalidArgs)
	}
}

func TestExecRefusesScopeOpenForMCP(t *testing.T) {
	cfg := config.ExecConfig{Enabled: true, Scope: config.ScopeFreeform}
	sess, _, cleanup := configuredServer(t, cfg, false)
	defer cleanup()
	res, out := callExec(t, sess, []string{"echo", "hi"})
	if !res.IsError || out.ErrorCode != mcpsrv.ErrCodeScopeOpenRefused {
		t.Fatalf("ErrorCode = %q want %q", out.ErrorCode, mcpsrv.ErrCodeScopeOpenRefused)
	}
}

func TestExecRefusesPolicyDenial(t *testing.T) {
	cfg := config.ExecConfig{Enabled: true, Scope: config.ScopeReadOnly}
	sess, _, cleanup := configuredServer(t, cfg, false)
	defer cleanup()
	res, out := callExec(t, sess, []string{"cat", "/etc/passwd"})
	if !res.IsError || out.ErrorCode != mcpsrv.ErrCodePolicyDenied {
		t.Fatalf("ErrorCode = %q want %q", out.ErrorCode, mcpsrv.ErrCodePolicyDenied)
	}
}

func TestExecHappyPathWritesAudit(t *testing.T) {
	cfg := config.ExecConfig{Enabled: true, Scope: config.ScopeReadOnly}
	sess, _, cleanup := configuredServer(t, cfg, false)
	defer cleanup()

	res, out := callExec(t, sess, []string{"echo", "hello-world"})
	if res.IsError {
		body, _ := json.Marshal(res.Content)
		t.Fatalf("unexpected error: %s", body)
	}
	if out.ExitCode != 0 {
		t.Fatalf("exit = %d, want 0", out.ExitCode)
	}
	if !strings.Contains(out.Stdout, "hello-world") {
		t.Fatalf("stdout = %q, want hello-world", out.Stdout)
	}

	// Confirm exec.log got a record with source=mcp.
	auditDir := os.Getenv("OPSMASK_AUDIT_DIR")
	body, err := os.ReadFile(filepath.Join(auditDir, "exec.log"))
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	if !strings.Contains(string(body), `"source":"mcp"`) {
		t.Fatalf("audit log missing source=mcp: %s", body)
	}
	_ = maskexec.SourceMCP // keep import used
}

func TestExecRemasksOutput(t *testing.T) {
	cfg := config.ExecConfig{Enabled: true, Scope: config.ScopeReadOnly}
	sess, _, cleanup := configuredServer(t, cfg, false)
	defer cleanup()

	res, out := callExec(t, sess, []string{"echo", "the IP is 10.0.0.1"})
	if res.IsError {
		body, _ := json.Marshal(res.Content)
		t.Fatalf("unexpected error: %s", body)
	}
	if strings.Contains(out.Stdout, "10.0.0.1") {
		t.Fatalf("stdout still has plaintext: %q", out.Stdout)
	}
	if !strings.Contains(out.Stdout, "opsmask:") {
		t.Fatalf("stdout missing sentinel: %q", out.Stdout)
	}
	if out.Masked < 1 {
		t.Fatalf("Masked = %d, want >= 1", out.Masked)
	}
}

func TestExecInvalidTimeout(t *testing.T) {
	cfg := config.ExecConfig{Enabled: true, Scope: config.ScopeReadOnly}
	sess, _, cleanup := configuredServer(t, cfg, false)
	defer cleanup()
	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "exec",
		Arguments: map[string]any{"argv": []string{"echo", "hi"}, "timeout": "not-a-duration"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected error result")
	}
	var out mcpsrv.ExecOutput
	if res.StructuredContent != nil {
		raw, _ := json.Marshal(res.StructuredContent)
		_ = json.Unmarshal(raw, &out)
	}
	if out.ErrorCode != mcpsrv.ErrCodeInvalidTimeout {
		t.Fatalf("ErrorCode = %q, want %q", out.ErrorCode, mcpsrv.ErrCodeInvalidTimeout)
	}
}
