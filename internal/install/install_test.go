package install

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallAndUninstallClaudeCodePersonal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	project := gitProject(t)
	top, err := ResolveProjectToplevel(project)
	if err != nil {
		t.Fatal(err)
	}
	res, err := InstallClaudeCode(top, "/bin/opsmask", ModePersonal)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(res.SettingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"name": "opsmask"`) || !strings.Contains(string(body), `"matcher": "Bash"`) {
		t.Fatalf("settings = %s", body)
	}
	if _, err := os.Stat(res.ShimPath); err != nil {
		t.Fatal(err)
	}
	if !hasHook(res.SettingsPath) {
		t.Fatal("hook not detected after install")
	}
	if _, err := InstallClaudeCode(top, "/bin/opsmask", ModePersonal); err == nil {
		t.Fatal("expected idempotent second install to refuse")
	}
	if _, err := UninstallClaudeCode(top); err != nil {
		t.Fatal(err)
	}
	if hasHook(res.SettingsPath) {
		t.Fatal("hook still present after uninstall")
	}
}

func gitProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	return dir
}
