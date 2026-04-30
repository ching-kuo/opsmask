package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMcpServeStdinClose checks that `opsmask mcp serve` exits cleanly when
// stdin closes (common when the client process is terminated). The SDK's
// stdio transport returns io.EOF, which is the expected terminating error.
func TestMcpServeStdinClose(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	cwd := t.TempDir()
	if err := os.Chmod(cwd, 0o700); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	auditDir := t.TempDir()
	if err := os.Chmod(auditDir, 0o700); err != nil {
		t.Fatalf("chmod audit: %v", err)
	}
	t.Setenv("OPSMASK_AUDIT_DIR", auditDir)
	t.Chdir(cwd)

	mapping := filepath.Join(cwd, "mapping.sqlite")

	root := NewRoot("test")
	root.SetArgs([]string{"--mapping", mapping, "mcp", "serve"})
	root.SetIn(bytes.NewReader(nil))
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	root.SetContext(ctx)

	err := root.Execute()
	if err == nil {
		return
	}
	// EOF and ctx-cancellation are both clean terminations from this side.
	if errStr := err.Error(); strings.Contains(errStr, "EOF") || errStr == "" {
		return
	}
	if err == context.DeadlineExceeded {
		t.Fatal("mcp serve hung past context deadline on closed stdin")
	}
	t.Fatalf("unexpected mcp serve error: %v", err)
}

// TestMcpServeRegistration ensures the mcp subcommand is reachable from the
// root and the rewriter does not promote it to a positional `mask` argument.
func TestMcpServeRegistration(t *testing.T) {
	out := RewriteArgs([]string{"mcp", "serve"})
	if got := strings.Join(out, " "); got != "mcp serve" {
		t.Fatalf("RewriteArgs(mcp serve)=%q, want unchanged", got)
	}
	root := NewRoot("test")
	if cmd, _, err := root.Find([]string{"mcp", "serve"}); err != nil || cmd == nil {
		t.Fatalf("mcp serve not registered: cmd=%v err=%v", cmd, err)
	}
}
