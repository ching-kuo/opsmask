package cchook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"

	maskexec "github.com/ching-kuo/opsmask/internal/exec"
	"github.com/ching-kuo/opsmask/internal/install"
)

type Event struct {
	Cwd           string         `json:"cwd"`
	HookEventName string         `json:"hook_event_name"`
	ToolName      string         `json:"tool_name"`
	ToolInput     map[string]any `json:"tool_input"`
}

type HandlerEnv struct {
	Executable string
	Secret     []byte
}

func Handle(in io.Reader, out io.Writer, env HandlerEnv) error {
	var ev Event
	if err := json.NewDecoder(in).Decode(&ev); err != nil {
		return writeRefuse(out, "OpsMask hook could not parse Claude Code hook input: "+err.Error())
	}
	if ev.ToolName != "Bash" {
		return writeEmpty(out)
	}
	command, _ := ev.ToolInput["command"].(string)
	if command == "" {
		return writeEmpty(out)
	}
	if maskexec.IsExecChild() {
		return writeEmpty(out)
	}
	cwd := ev.Cwd
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	top, err := install.ResolveProjectToplevel(cwd)
	if err != nil {
		return writeRefuse(out, err.Error())
	}
	registered, err := install.IsRegistered(top)
	if err != nil {
		return writeRefuse(out, "OpsMask hook registry could not be read: "+err.Error())
	}
	if !registered {
		return writeRefuse(out, "OpsMask hook fired in a project that was not opted in via `opsmask install claude-code`. Refusing.")
	}
	if skip, verb := Match(command); skip {
		rec := maskexec.NewRecord(maskexec.SourceHook)
		rec.Cwd = cwd
		rec.Executable = verb
		rec.Argv = []string{command}
		_ = maskexec.WritePassThroughRecord(rec)
		return writeEmpty(out)
	}
	secret := env.Secret
	if len(secret) == 0 {
		secret, err = LoadSecret()
		if err != nil {
			return writeRefuse(out, "OpsMask hook secret could not be loaded: "+err.Error())
		}
	}
	exe := env.Executable
	if exe == "" {
		exe, err = os.Executable()
		if err != nil {
			return writeRefuse(out, "OpsMask executable could not be resolved: "+err.Error())
		}
		exe, err = filepath.EvalSymlinks(exe)
		if err != nil {
			return writeRefuse(out, "OpsMask executable symlink could not be resolved: "+err.Error())
		}
	}
	sig := Sign(secret, top, command)
	wrapped := install.ShellQuote(exe) + " claude-code-exec --sig " + sig + " -- " + install.ShellQuote(command)
	return writeRewrite(out, ev.ToolInput, wrapped)
}

func Sign(secret []byte, projectToplevel, command string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(projectToplevel))
	mac.Write([]byte{0})
	mac.Write([]byte(command))
	return hex.EncodeToString(mac.Sum(nil))
}

func Verify(secret []byte, projectToplevel, command, sig string) bool {
	got, err := hex.DecodeString(sig)
	if err != nil {
		return false
	}
	want, err := hex.DecodeString(Sign(secret, projectToplevel, command))
	if err != nil {
		return false
	}
	return hmac.Equal(got, want)
}

func writeEmpty(out io.Writer) error {
	_, err := io.WriteString(out, "{}\n")
	return err
}

func writeRewrite(out io.Writer, original map[string]any, command string) error {
	updated := make(map[string]any, len(original)+1)
	for k, v := range original {
		updated[k] = v
	}
	updated["command"] = command
	return json.NewEncoder(out).Encode(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "allow",
			"permissionDecisionReason": "OpsMask hook rewrote command for masked execution",
			"updatedInput":             updated,
		},
	})
}

func writeRefuse(out io.Writer, reason string) error {
	return json.NewEncoder(out).Encode(map[string]any{
		"continue":   false,
		"stopReason": reason,
		"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "deny",
			"permissionDecisionReason": reason,
		},
	})
}
