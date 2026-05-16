package install

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Mode string

const (
	ModePersonal   Mode = "personal"
	ModeTeamShared Mode = "team-shared"
)

type Result struct {
	ProjectToplevel string
	SettingsPath    string
	ShimPath        string
}

func InstallClaudeCode(projectToplevel, opsmaskPath string, mode Mode) (Result, error) {
	if projectToplevel == "" {
		var err error
		projectToplevel, err = ResolveProjectToplevel("")
		if err != nil {
			return Result{}, err
		}
	}
	if opsmaskPath == "" {
		exe, err := os.Executable()
		if err != nil {
			return Result{}, err
		}
		opsmaskPath, err = filepath.EvalSymlinks(exe)
		if err != nil {
			return Result{}, err
		}
	}
	claudeDir := filepath.Join(projectToplevel, ".claude")
	if err := os.MkdirAll(claudeDir, 0o700); err != nil {
		return Result{}, err
	}
	settings := filepath.Join(claudeDir, "settings.local.json")
	if mode == ModeTeamShared {
		settings = filepath.Join(claudeDir, "settings.json")
	}
	if existing, ok := findExistingHook(projectToplevel); ok {
		return Result{}, fmt.Errorf("already installed at %s", existing)
	}
	shim := filepath.Join(claudeDir, "opsmask-hook.sh")
	if err := os.WriteFile(shim, []byte(shimScript(opsmaskPath)), 0o700); err != nil {
		return Result{}, err
	}
	if err := addHook(settings, shim); err != nil {
		_ = os.Remove(shim)
		return Result{}, err
	}
	if mode == ModePersonal {
		_ = ensureGitignore(projectToplevel)
	}
	if err := RegisterInstall(projectToplevel); err != nil {
		_ = removeHookOrErr(settings)
		_ = os.Remove(shim)
		return Result{}, fmt.Errorf("register install: %w", err)
	}
	return Result{ProjectToplevel: projectToplevel, SettingsPath: settings, ShimPath: shim}, nil
}

func removeHookOrErr(path string) error {
	root, err := readSettings(path)
	if err != nil {
		return err
	}
	hooks, _ := root["hooks"].(map[string]any)
	arr, _ := hooks["PreToolUse"].([]any)
	out := arr[:0]
	for _, item := range arr {
		if m, ok := item.(map[string]any); ok && m["name"] == "opsmask" {
			continue
		}
		out = append(out, item)
	}
	hooks["PreToolUse"] = out
	return writeSettings(path, root)
}

func UninstallClaudeCode(projectToplevel string) (Result, error) {
	if projectToplevel == "" {
		var err error
		projectToplevel, err = ResolveProjectToplevel("")
		if err != nil {
			return Result{}, err
		}
	}
	var changed string
	for _, settings := range settingsPaths(projectToplevel) {
		ok, err := removeHook(settings)
		if err != nil {
			return Result{}, fmt.Errorf("remove hook from %s: %w", settings, err)
		}
		if ok {
			changed = settings
		}
	}
	shim := filepath.Join(projectToplevel, ".claude", "opsmask-hook.sh")
	if err := os.Remove(shim); err != nil && !os.IsNotExist(err) {
		return Result{}, fmt.Errorf("remove shim: %w", err)
	}
	if err := Unregister(projectToplevel); err != nil {
		return Result{}, err
	}
	if changed == "" {
		return Result{}, fmt.Errorf("OpsMask Claude Code hook is not installed in this project")
	}
	return Result{ProjectToplevel: projectToplevel, SettingsPath: changed, ShimPath: shim}, nil
}

func settingsPaths(projectToplevel string) []string {
	return []string{
		filepath.Join(projectToplevel, ".claude", "settings.local.json"),
		filepath.Join(projectToplevel, ".claude", "settings.json"),
	}
}

func findExistingHook(projectToplevel string) (string, bool) {
	for _, p := range settingsPaths(projectToplevel) {
		if hasHook(p) {
			return p, true
		}
	}
	return "", false
}

func hasHook(path string) bool {
	root, err := readSettings(path)
	if err != nil {
		return false
	}
	arr, _ := root["hooks"].(map[string]any)["PreToolUse"].([]any)
	for _, item := range arr {
		if m, ok := item.(map[string]any); ok && m["name"] == "opsmask" {
			return true
		}
	}
	return false
}

func addHook(path, shim string) error {
	root, err := readSettings(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
		root["hooks"] = hooks
	}
	arr, _ := hooks["PreToolUse"].([]any)
	arr = append(arr, map[string]any{
		"name":    "opsmask",
		"matcher": "Bash",
		"hooks": []any{map[string]any{
			"type":    "command",
			"command": ShellQuote(shim),
		}},
	})
	hooks["PreToolUse"] = arr
	return writeSettings(path, root)
}

func removeHook(path string) (bool, error) {
	root, err := readSettings(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	hooks, _ := root["hooks"].(map[string]any)
	arr, _ := hooks["PreToolUse"].([]any)
	if len(arr) == 0 {
		return false, nil
	}
	out := arr[:0]
	changed := false
	for _, item := range arr {
		if m, ok := item.(map[string]any); ok && m["name"] == "opsmask" {
			changed = true
			continue
		}
		out = append(out, item)
	}
	if !changed {
		return false, nil
	}
	hooks["PreToolUse"] = out
	if err := writeSettings(path, root); err != nil {
		return false, err
	}
	return true, nil
}

func readSettings(path string) (map[string]any, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return map[string]any{}, err
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return map[string]any{}, nil
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, err
	}
	if root == nil {
		root = map[string]any{}
	}
	return root, nil
}

func writeSettings(path string, root map[string]any) error {
	body, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".settings.*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(append(body, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func shimScript(opsmaskPath string) string {
	json := `{"continue":false,"stopReason":"OpsMask binary is unavailable; reinstall or uninstall the OpsMask Claude Code hook."}`
	return "#!/bin/sh\nif [ ! -x " + ShellQuote(opsmaskPath) + " ]; then\n  printf '%s\\n' '" + json + "'\n  exit 0\nfi\nexec " + ShellQuote(opsmaskPath) + " claude-code-hook\n"
}

func ensureGitignore(projectToplevel string) error {
	path := filepath.Join(projectToplevel, ".gitignore")
	body, _ := os.ReadFile(path)
	if strings.Contains(string(body), ".claude/settings.local.json") {
		return nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if len(body) > 0 && !strings.HasSuffix(string(body), "\n") {
		if _, err := f.WriteString("\n"); err != nil {
			return err
		}
	}
	_, err = f.WriteString(".claude/settings.local.json\n")
	return err
}

func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
