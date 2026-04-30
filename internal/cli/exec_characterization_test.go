package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Characterization tests for the CLI `exec` subcommand. These existed in
// spirit only (one trust-related test) before U4; the table here pins the
// observable contract — every refusal class produces the right stderr
// fragment and exit signal — so the orchestrator extraction proves
// behavior-preserving.

func newCharProject(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	cwd := t.TempDir()
	if err := os.Chmod(cwd, 0o700); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Chdir(cwd)
	return cwd
}

func writeProjectConfig(t *testing.T, cwd, body string) {
	t.Helper()
	dir := filepath.Join(cwd, ".opsmask")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestExecRefusesUntrusted(t *testing.T) {
	cwd := newCharProject(t)
	auditDir := t.TempDir()
	if err := os.Chmod(auditDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPSMASK_AUDIT_DIR", auditDir)
	writeProjectConfig(t, cwd, "exec:\n  enabled: true\n  scope: read-only\n")

	stderr, err := executeCLI(t, []string{"exec", "--", "kubectl", "get", "pods"}, "")
	if err == nil {
		t.Fatal("expected error for untrusted config")
	}
	if !strings.Contains(stderr, "untrusted") {
		t.Fatalf("stderr = %q, want untrusted message", stderr)
	}
}

func TestExecRefusesDisabled(t *testing.T) {
	cwd := newCharProject(t)
	auditDir := t.TempDir()
	if err := os.Chmod(auditDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPSMASK_AUDIT_DIR", auditDir)
	writeProjectConfig(t, cwd, "exec:\n  enabled: false\n  scope: read-only\n")
	if _, err := executeCLI(t, []string{"config", "trust"}, ""); err != nil {
		t.Fatalf("trust: %v", err)
	}

	stderr, err := executeCLI(t, []string{"exec", "--", "kubectl", "get", "pods"}, "")
	if err == nil {
		t.Fatal("expected error for disabled exec")
	}
	if !strings.Contains(stderr, "exec disabled in this project") {
		t.Fatalf("stderr = %q, want disabled message", stderr)
	}
}

func TestExecRefusesEmptyArgv(t *testing.T) {
	newCharProject(t)
	stderr, err := executeCLI(t, []string{"exec"}, "")
	if err == nil {
		t.Fatal("expected error for empty argv")
	}
	if !strings.Contains(stderr, "exec requires") && !strings.Contains(err.Error(), "exec requires") {
		t.Fatalf("stderr = %q err = %v, want argv-required message", stderr, err)
	}
}

func TestExecPolicyDeniedWritesAudit(t *testing.T) {
	cwd := newCharProject(t)
	auditDir := t.TempDir()
	if err := os.Chmod(auditDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPSMASK_AUDIT_DIR", auditDir)
	writeProjectConfig(t, cwd, "exec:\n  enabled: true\n  scope: read-only\n")
	if _, err := executeCLI(t, []string{"config", "trust"}, ""); err != nil {
		t.Fatalf("trust: %v", err)
	}

	stderr, err := executeCLI(t, []string{"exec", "--", "cat", "/etc/passwd"}, "")
	if err == nil {
		t.Fatal("expected policy denial")
	}
	if !strings.Contains(stderr, "exec rejected") {
		t.Fatalf("stderr = %q, want exec rejected", stderr)
	}
	body, err := os.ReadFile(filepath.Join(auditDir, "exec.log"))
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	if !strings.Contains(string(body), `"source":"cli"`) {
		t.Fatalf("audit body missing source=cli: %s", string(body))
	}
}
