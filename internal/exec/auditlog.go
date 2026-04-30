package exec

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ching-kuo/opsmask/internal/config"
)

const auditMaxLine = 4095

type Record struct {
	Ts            time.Time                `json:"ts"`
	Cwd           string                   `json:"cwd,omitempty"`
	Executable    string                   `json:"executable,omitempty"`
	Argv          []string                 `json:"argv,omitempty"`
	Scope         string                   `json:"scope,omitempty"`
	AllowMatch    string                   `json:"allow_match,omitempty"`
	DenyMatch     string                   `json:"deny_match,omitempty"`
	DenyOptOut    []config.DenyOptOutEntry `json:"deny_opt_out,omitempty"`
	EnvAllowCount int                      `json:"env_allow_count"`
	EnvDenyCount  int                      `json:"env_deny_count"`
	ExitCode      int                      `json:"exit_code"`
	DurationMs    int64                    `json:"duration_ms"`
	ErrorClass    string                   `json:"error_class,omitempty"`
	Truncated     bool                     `json:"truncated,omitempty"`
}

func WriteRecord(rec Record) error {
	if rec.Ts.IsZero() {
		rec.Ts = time.Now()
	}
	line, err := encodeRecord(rec)
	if err != nil {
		return err
	}
	f, _, err := openAuditLog()
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

// Preflight verifies the audit log can be created and written. It fails
// closed when the directory or file would reject WriteRecord, so callers
// can refuse to run a command that would leave no audit trail.
func Preflight() error {
	f, _, err := openAuditLog()
	if err != nil {
		return err
	}
	return f.Close()
}

func openAuditLog() (*os.File, string, error) {
	dir, err := auditDir()
	if err != nil {
		return nil, "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, "", err
	}
	if info, err := os.Stat(dir); err == nil && info.Mode().Perm()&0o077 != 0 {
		return nil, "", fmt.Errorf("%s must not be group/world accessible", dir)
	}
	path := filepath.Join(dir, "exec.log")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, "", err
	}
	if info, err := f.Stat(); err == nil && info.Mode().Perm()&0o077 != 0 {
		_ = f.Close()
		return nil, "", fmt.Errorf("%s must not be group/world accessible", path)
	}
	return f, path, nil
}

func auditDir() (string, error) {
	if x := os.Getenv("OPSMASK_AUDIT_DIR"); x != "" {
		return x, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "opsmask"), nil
}

func encodeRecord(rec Record) ([]byte, error) {
	b, err := json.Marshal(rec)
	if err != nil {
		return nil, err
	}
	if len(b)+1 <= auditMaxLine {
		return b, nil
	}
	rec.Truncated = true
	// Binary search for the largest argv prefix length that still fits.
	// fits(n) means rec with first n argv entries plus "…" still encodes within limit.
	full := rec.Argv
	lo, hi := 0, len(full)
	var best []byte
	for lo <= hi {
		mid := (lo + hi) / 2
		trial := make([]string, 0, mid+1)
		trial = append(trial, full[:mid]...)
		trial = append(trial, "…")
		rec.Argv = trial
		buf, err := json.Marshal(rec)
		if err != nil {
			return nil, err
		}
		if len(buf)+1 <= auditMaxLine {
			best = buf
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	if best != nil {
		return best, nil
	}
	return json.Marshal(Record{Ts: rec.Ts, ErrorClass: "audit_truncate_oversize", Truncated: true})
}
