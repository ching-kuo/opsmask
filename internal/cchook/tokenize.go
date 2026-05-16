package cchook

import (
	"os"
	osexec "os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

var localeValueRE = regexp.MustCompile(`^[A-Za-z0-9_.@-]+$`)

func Match(command string) (bool, string) {
	argv, ok := commandArgv(command)
	if !ok || len(argv) == 0 {
		return false, ""
	}
	verb := filepath.Base(argv[0])
	args := argv[1:]
	switch verb {
	case "ls", "pwd", "cd", "true", "false":
		return true, verb
	case "git":
		return matchGitStatus(args), verb
	case "vim", "vi", "nvim":
		return len(args) == 0 || (len(args) == 1 && args[0] == "-R"), verb
	case "nano", "less", "more", "man", "top", "htop":
		return len(args) == 0, verb
	case "opsmask":
		return sameExecutable(argv[0]), verb
	default:
		return false, ""
	}
}

func commandArgv(command string) ([]string, bool) {
	if hasDisqualifyingMeta(command) {
		return nil, false
	}
	tokens, ok := splitShellWords(command)
	if !ok {
		return nil, false
	}
	for len(tokens) > 0 {
		name, val, isAssign := strings.Cut(tokens[0], "=")
		if !isAssign || name == "" {
			break
		}
		if name != "LANG" && !strings.HasPrefix(name, "LC_") {
			return nil, false
		}
		if !localeValueRE.MatchString(val) {
			return nil, false
		}
		tokens = tokens[1:]
	}
	return tokens, true
}

func hasDisqualifyingMeta(s string) bool {
	if strings.Contains(s, "$(") {
		return true
	}
	for _, r := range s {
		switch r {
		case '|', '&', ';', '`', '>', '<', '\\':
			return true
		}
	}
	return false
}

func splitShellWords(s string) ([]string, bool) {
	var out []string
	var b strings.Builder
	var quote rune
	inWord := false
	for _, r := range s {
		if quote != 0 {
			if r == quote {
				quote = 0
				continue
			}
			b.WriteRune(r)
			continue
		}
		switch r {
		case '\'', '"':
			quote = r
			inWord = true
		case ' ', '\t', '\n', '\r':
			if inWord {
				out = append(out, b.String())
				b.Reset()
				inWord = false
			}
		default:
			b.WriteRune(r)
			inWord = true
		}
	}
	if quote != 0 {
		return nil, false
	}
	if inWord {
		out = append(out, b.String())
	}
	return out, true
}

func matchGitStatus(args []string) bool {
	if len(args) == 0 || args[0] != "status" {
		return false
	}
	for _, a := range args[1:] {
		switch a {
		case "--short", "-s", "-b", "--branch", "--ahead-behind", "--no-renames", "--porcelain":
			continue
		case "--porcelain=v1", "--porcelain=v2":
			continue
		default:
			return false
		}
	}
	return true
}

var (
	selfExeOnce sync.Once
	selfExePath string
	selfExeOK   bool
)

func resolvedSelf() (string, bool) {
	selfExeOnce.Do(func() {
		exe, err := os.Executable()
		if err != nil {
			return
		}
		real, err := filepath.EvalSymlinks(exe)
		if err != nil {
			return
		}
		selfExePath = real
		selfExeOK = true
	})
	return selfExePath, selfExeOK
}

func sameExecutable(name string) bool {
	path, err := osexec.LookPath(name)
	if err != nil {
		return false
	}
	got, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	self, ok := resolvedSelf()
	if !ok {
		return false
	}
	return got == self
}
