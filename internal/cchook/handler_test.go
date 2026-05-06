package cchook

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ching-kuo/opsmask/internal/install"
)

// clearExecChild unsets OPSMASK_EXEC_CHILD for the duration of a test. The
// var leaks in when the test suite runs inside an opsmask-exec subprocess
// tree (e.g. when the Claude Code hook wraps `go test`); without this guard
// `Handle` short-circuits via IsExecChild and tests asserting against the
// rewrite/refuse envelopes see an empty `{}` response.
func clearExecChild(t *testing.T) {
	t.Helper()
	prev, had := os.LookupEnv("OPSMASK_EXEC_CHILD")
	os.Unsetenv("OPSMASK_EXEC_CHILD")
	t.Cleanup(func() {
		if had {
			os.Setenv("OPSMASK_EXEC_CHILD", prev)
		}
	})
}

func TestHandleRewritesNonSkipBashCommand(t *testing.T) {
	clearExecChild(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	project := gitProject(t)
	top, err := install.ResolveProjectToplevel(project)
	if err != nil {
		t.Fatal(err)
	}
	if err := install.RegisterInstall(top); err != nil {
		t.Fatal(err)
	}
	secret := bytes.Repeat([]byte{1}, 32)
	input := `{"cwd":` + quote(project) + `,"tool_name":"Bash","tool_input":{"command":"cat /etc/hosts"}}`
	var out bytes.Buffer
	if err := Handle(strings.NewReader(input), &out, HandlerEnv{Executable: "/bin/opsmask", Secret: secret}); err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	hso, ok := decoded["hookSpecificOutput"].(map[string]any)
	if !ok {
		t.Fatalf("missing hookSpecificOutput: %s", out.String())
	}
	if hso["permissionDecision"] != "allow" {
		t.Fatalf("permissionDecision = %v, want allow", hso["permissionDecision"])
	}
	updated := hso["updatedInput"].(map[string]any)
	command := updated["command"].(string)
	if !strings.Contains(command, "claude-code-exec --sig ") || !strings.Contains(command, "cat /etc/hosts") {
		t.Fatalf("rewrite command = %q", command)
	}
}

func TestHandleRefusesUnregisteredProject(t *testing.T) {
	clearExecChild(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	project := gitProject(t)
	input := `{"cwd":` + quote(project) + `,"tool_name":"Bash","tool_input":{"command":"cat /etc/hosts"}}`
	var out bytes.Buffer
	if err := Handle(strings.NewReader(input), &out, HandlerEnv{Executable: "/bin/opsmask", Secret: bytes.Repeat([]byte{1}, 32)}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"continue":false`) || !strings.Contains(out.String(), "not opted in") {
		t.Fatalf("response = %s", out.String())
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

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
