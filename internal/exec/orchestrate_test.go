package exec_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ching-kuo/opsmask/internal/config"
	"github.com/ching-kuo/opsmask/internal/detect"
	maskexec "github.com/ching-kuo/opsmask/internal/exec"
	"github.com/ching-kuo/opsmask/internal/pseudo"
	"github.com/ching-kuo/opsmask/internal/store"
)

func newOrchestrateRuntime(t *testing.T, cfg config.ExecConfig, untrusted bool) maskexec.Runtime {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	st, err := store.OpenSQLite(filepath.Join(dir, "mapping.sqlite"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rules, err := detect.BuiltinRules()
	if err != nil {
		t.Fatalf("BuiltinRules: %v", err)
	}
	return maskexec.Runtime{
		Store:     st,
		Alloc:     pseudo.New([]byte("test-secret"), st),
		Rules:     rules,
		Untrusted: untrusted,
		Cfg:       cfg,
	}
}

func setOrchestrateAuditDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Setenv("OPSMASK_AUDIT_DIR", dir)
	return dir
}

func TestOrchestrateUntrusted(t *testing.T) {
	dir := setOrchestrateAuditDir(t)
	rt := newOrchestrateRuntime(t, config.ExecConfig{Enabled: true, Scope: config.ScopeReadOnly}, true)
	res, err := maskexec.Orchestrate(context.Background(), rt, []string{"kubectl", "get", "pods"}, maskexec.OrchestrateOptions{
		Source: maskexec.SourceCLI,
		Stdout: io_discard(), Stderr: io_discard(),
	})
	if !errors.Is(err, maskexec.ErrUntrusted) {
		t.Fatalf("err = %v, want ErrUntrusted", err)
	}
	if res.ExitCode != 125 {
		t.Fatalf("exit = %d, want 125", res.ExitCode)
	}
	assertAuditClass(t, dir, "untrusted")
}

func TestOrchestrateDisabled(t *testing.T) {
	dir := setOrchestrateAuditDir(t)
	rt := newOrchestrateRuntime(t, config.ExecConfig{Enabled: false, Scope: config.ScopeReadOnly}, false)
	_, err := maskexec.Orchestrate(context.Background(), rt, []string{"kubectl", "get", "pods"}, maskexec.OrchestrateOptions{
		Source: maskexec.SourceCLI,
		Stdout: io_discard(), Stderr: io_discard(),
	})
	if !errors.Is(err, maskexec.ErrDisabled) {
		t.Fatalf("err = %v, want ErrDisabled", err)
	}
	assertAuditClass(t, dir, "disabled")
}

func TestOrchestrateScopeOpenRefusedForMCP(t *testing.T) {
	dir := setOrchestrateAuditDir(t)
	cfg := config.ExecConfig{Enabled: true, Scope: config.ScopeFreeform}
	rt := newOrchestrateRuntime(t, cfg, false)
	_, err := maskexec.Orchestrate(context.Background(), rt, []string{"echo", "hi"}, maskexec.OrchestrateOptions{
		Source: maskexec.SourceMCP, RefuseScopeOpen: true,
		Stdout: io_discard(), Stderr: io_discard(),
	})
	if !errors.Is(err, maskexec.ErrScopeOpen) {
		t.Fatalf("err = %v, want ErrScopeOpen", err)
	}
	assertAuditClass(t, dir, "scope_open_refused")
}

func TestOrchestrateScopeOpenAllowedWithEntries(t *testing.T) {
	setOrchestrateAuditDir(t)
	cfg := config.ExecConfig{
		Enabled: true, Scope: config.ScopeFreeform,
		Allow: []config.AllowEntry{{
			Name:     "echo",
			Elements: []*regexp.Regexp{regexp.MustCompile("^echo$")},
			AnyTail:  true,
		}},
	}
	rt := newOrchestrateRuntime(t, cfg, false)
	res, err := maskexec.Orchestrate(context.Background(), rt, []string{"echo", "hi"}, maskexec.OrchestrateOptions{
		Source:          maskexec.SourceMCP,
		RefuseScopeOpen: true,
		Stdin:           strings.NewReader(""),
		Stdout:          &bytes.Buffer{},
		Stderr:          &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit = %d, want 0", res.ExitCode)
	}
}

func TestOrchestrateScopeOpenAllowedForCLI(t *testing.T) {
	// CLI does not refuse scope=freeform+empty-allow even though MCP does.
	setOrchestrateAuditDir(t)
	cfg := config.ExecConfig{Enabled: true, Scope: config.ScopeFreeform}
	rt := newOrchestrateRuntime(t, cfg, false)
	_, err := maskexec.Orchestrate(context.Background(), rt, []string{"echo", "hi"}, maskexec.OrchestrateOptions{
		Source:          maskexec.SourceCLI,
		RefuseScopeOpen: false,
		Stdin:           strings.NewReader(""),
		Stdout:          &bytes.Buffer{},
		Stderr:          &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("err = %v, want nil for CLI freeform", err)
	}
}

func TestOrchestrateInvalidTimeout(t *testing.T) {
	setOrchestrateAuditDir(t)
	cfg := config.ExecConfig{Enabled: true, Scope: config.ScopeReadOnly}
	rt := newOrchestrateRuntime(t, cfg, false)
	_, err := maskexec.Orchestrate(context.Background(), rt, []string{"echo", "hi"}, maskexec.OrchestrateOptions{
		Source: maskexec.SourceCLI, Timeout: "not-a-duration",
		Stdout: io_discard(), Stderr: io_discard(),
	})
	if !errors.Is(err, maskexec.ErrTimeoutParse) {
		t.Fatalf("err = %v, want ErrTimeoutParse", err)
	}
}

func TestOrchestratePolicyDenied(t *testing.T) {
	setOrchestrateAuditDir(t)
	cfg := config.ExecConfig{Enabled: true, Scope: config.ScopeReadOnly}
	rt := newOrchestrateRuntime(t, cfg, false)
	_, err := maskexec.Orchestrate(context.Background(), rt, []string{"cat", "/etc/passwd"}, maskexec.OrchestrateOptions{
		Source: maskexec.SourceCLI,
		Stdout: io_discard(), Stderr: io_discard(),
	})
	if !errors.Is(err, maskexec.ErrPolicyDenied) {
		t.Fatalf("err = %v, want ErrPolicyDenied", err)
	}
}

func TestOrchestrateInvalidSource(t *testing.T) {
	setOrchestrateAuditDir(t)
	cfg := config.ExecConfig{Enabled: true, Scope: config.ScopeReadOnly}
	rt := newOrchestrateRuntime(t, cfg, false)
	_, err := maskexec.Orchestrate(context.Background(), rt, []string{"echo"}, maskexec.OrchestrateOptions{
		Source: "evil",
		Stdout: io_discard(), Stderr: io_discard(),
	})
	if err == nil || !strings.Contains(err.Error(), "invalid source") {
		t.Fatalf("expected invalid-source error, got %v", err)
	}
}

func TestOrchestrateAuditSourceForMCP(t *testing.T) {
	dir := setOrchestrateAuditDir(t)
	cfg := config.ExecConfig{
		Enabled: true, Scope: config.ScopeFreeform,
		Allow: []config.AllowEntry{{
			Name:     "echo",
			Elements: []*regexp.Regexp{regexp.MustCompile("^echo$")},
			AnyTail:  true,
		}},
	}
	rt := newOrchestrateRuntime(t, cfg, false)
	_, err := maskexec.Orchestrate(context.Background(), rt, []string{"echo", "hi"}, maskexec.OrchestrateOptions{
		Source:          maskexec.SourceMCP,
		RefuseScopeOpen: true,
		Stdin:           strings.NewReader(""),
		Stdout:          &bytes.Buffer{},
		Stderr:          &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("Orchestrate: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "exec.log"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var rec maskexec.Record
	if err := json.Unmarshal(bytes.TrimSpace(body), &rec); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if rec.Source != "mcp" {
		t.Fatalf("Source = %q, want mcp", rec.Source)
	}
}

func io_discard() *bytes.Buffer { return &bytes.Buffer{} }

func assertAuditClass(t *testing.T, dir, want string) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(dir, "exec.log"))
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	if !bytes.Contains(body, []byte(`"error_class":"`+want+`"`)) {
		t.Fatalf("audit log = %s, want error_class=%q", string(body), want)
	}
}
