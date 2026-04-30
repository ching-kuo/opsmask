package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	configpkg "github.com/ching-kuo/opsmask/internal/config"
)

func TestConfigTrustCreatesMissingProjectConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	cwd := t.TempDir()
	t.Chdir(cwd)

	stderr, err := executeCLI(t, []string{"config", "trust"}, "")
	if err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(cwd, ".opsmask", "config.yaml")
	info, err := os.Stat(cfg)
	if err != nil {
		t.Fatalf("config.yaml was not created: %v", err)
	}
	body, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"literals: []", "regex_rules: []", "deny_list: []", "exec:", "enabled: false", "scope: read-only"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("config.yaml = %q, want actual %s entry", body, want)
		}
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("config.yaml permissions = %o, want private", info.Mode().Perm())
	}
	dirInfo, err := os.Stat(filepath.Dir(cfg))
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm()&0o077 != 0 {
		t.Fatalf(".opsmask permissions = %o, want private", dirInfo.Mode().Perm())
	}
	ok, err := configpkg.IsTrusted(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("newly created config was not trusted")
	}
	if !strings.Contains(stderr, "created [") || !strings.Contains(stderr, "trusted ") {
		t.Fatalf("stderr = %q, want creation and trust summary", stderr)
	}
}

func TestConfigCommandPromptsToInitialize(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	cwd := t.TempDir()
	t.Chdir(cwd)

	stderr, err := executeCLI(t, []string{"config"}, "yes\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cwd, ".opsmask", "config.yaml")); err != nil {
		t.Fatalf("config.yaml was not created: %v", err)
	}
	if !strings.Contains(stderr, "Initialize now?") || !strings.Contains(stderr, "opsmask config trust") {
		t.Fatalf("stderr = %q, want first-run initialization prompt and trust hint", stderr)
	}
}

func TestConfigTrustRepairsCommentOnlyProjectConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	cwd := t.TempDir()
	t.Chdir(cwd)
	if err := os.Mkdir(".opsmask", 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(cwd, ".opsmask", "config.yaml")
	if err := os.WriteFile(cfg, []byte("# literals: []\n# regex_rules: []\n# deny_list: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	stderr, err := executeCLI(t, []string{"config", "trust"}, "")
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"literals: []", "regex_rules: []", "deny_list: []", "exec:", "enabled: false", "scope: read-only"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("config.yaml = %q, want repaired %s entry", body, want)
		}
	}
	if !strings.Contains(stderr, "created [") || !strings.Contains(stderr, "trusted ") {
		t.Fatalf("stderr = %q, want repair and trust summary", stderr)
	}
}

// --config must NOT enable exec, since trust is bound to .opsmask/config.yaml
// (path-anchored hash). An LLM-reachable bypass would defeat the trust gate.
func TestExecConfigOverrideCannotEnableExec(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	cwd := t.TempDir()
	t.Chdir(cwd)

	overrideDir := filepath.Join(cwd, "override")
	if err := os.MkdirAll(overrideDir, 0o700); err != nil {
		t.Fatal(err)
	}
	override := filepath.Join(overrideDir, "config.yaml")
	body := []byte("exec:\n  enabled: true\n  scope: freeform\n")
	if err := os.WriteFile(override, body, 0o600); err != nil {
		t.Fatal(err)
	}

	stderr, err := executeCLI(t, []string{"--config", override, "exec", "--", "kubectl", "get", "pods"}, "")
	// exec must be refused, since the override cannot satisfy the project trust gate.
	if err == nil {
		t.Fatal("expected exec to fail when --config tries to enable exec; got success")
	}
	if !strings.Contains(stderr, "exec disabled") && !strings.Contains(stderr, "exec must be enabled via trusted") {
		t.Fatalf("stderr = %q, want disabled-exec or trust-warning message", stderr)
	}
	_ = configpkg.Loaded{} // keep import used
}

func executeCLI(t *testing.T, args []string, stdin string) (string, error) {
	t.Helper()
	var stderr bytes.Buffer
	root := NewRoot("test")
	root.SetArgs(args)
	root.SetErr(&stderr)
	root.SetOut(&bytes.Buffer{})
	root.SetIn(strings.NewReader(stdin))
	err := root.Execute()
	return stderr.String(), err
}
