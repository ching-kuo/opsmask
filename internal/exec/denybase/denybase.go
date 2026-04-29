// Package denybase holds the canonical hard-deny basename set used by both
// internal/exec/policy.go (Layer A) and internal/config (deny-opt-out
// validation). It is a leaf package so both consumers can import it without
// cycles.
package denybase

var bases = map[string]struct{}{}

// names is the master Layer A deny list. Entries cover shells, debuggers,
// in-process REPLs, schedulers, remote-exec helpers, build/archive tools, and
// open-helpers. Add new entries here only.
var names = []string{
	"bash", "sh", "zsh", "dash", "fish", "ksh", "csh", "tcsh", "ash", "mksh", "busybox", "toybox",
	"gdb", "lldb", "radare2", "r2",
	"sqlite3", "psql", "mysql", "mariadb", "redis-cli", "mongo", "mongosh",
	"vim", "vi", "nvim", "emacs", "ex", "view", "nano",
	"python", "python2", "python3", "ipython",
	"node", "nodejs", "deno", "bun", "perl", "ruby", "lua", "php",
	"script", "expect", "osascript",
	"crontab", "at", "batch", "launchctl", "systemctl", "systemd-run", "service",
	"ssh", "scp", "rsync", "telnet", "nc", "ncat", "socat", "mosh",
	"make", "gmake", "cmake",
	"tar", "gtar", "bsdtar",
	"open", "xdg-open",
	"xargs",
}

func init() {
	for _, n := range names {
		bases[n] = struct{}{}
	}
}

func Contains(name string) bool {
	_, ok := bases[name]
	return ok
}

func Names() []string {
	out := make([]string, len(names))
	copy(out, names)
	return out
}
