package mcpsrv

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/ching-kuo/opsmask/internal/exec"
)

// McpCallRecord is the lean per-call audit record written to mcp_calls.jsonl.
// Source is always "mcp"; readers normalize for forward compatibility.
//
// args_summary carries only sizes and booleans, never raw content — the
// audit log MUST not become a side-channel for the plaintext that
// mask_text/exec receive.
type McpCallRecord struct {
	Ts              time.Time      `json:"ts"`
	Source          string         `json:"source"`
	Tool            string         `json:"tool"`
	ArgsSummary     map[string]any `json:"args_summary,omitempty"`
	OK              bool           `json:"ok"`
	ErrClass        string         `json:"err_class,omitempty"`
	ResultSizeBytes int            `json:"result_size_bytes"`
	DurationMs      int64          `json:"duration_ms"`
}

// AuditFile is the production AuditWriter that appends to mcp_calls.jsonl.
//
// Open semantics match exec.log: O_APPEND|O_CREATE|O_WRONLY|O_CLOEXEC, mode
// 0600. POSIX guarantees line-sized appends are atomic across processes, so a
// long-lived MCP server can share the file with concurrent CLI invocations.
type AuditFile struct {
	mu sync.Mutex
	f  *os.File
}

// OpenAuditWriter resolves the audit directory (via exec.AuditDir), creates
// it with mode 0700 if missing, and opens mcp_calls.jsonl in append-only mode.
func OpenAuditWriter() (*AuditFile, error) {
	dir, err := exec.EnsureAuditDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "mcp_calls.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open mcp audit log: %w", err)
	}
	if info, err := f.Stat(); err == nil && info.Mode().Perm()&0o077 != 0 {
		_ = f.Close()
		return nil, fmt.Errorf("%s must not be group/world accessible", path)
	}
	return &AuditFile{f: f}, nil
}

// Write appends a single record as a JSON line. The mutex serializes encode
// + write within a single process; POSIX append-mode atomicity covers
// cross-process safety for line-sized writes.
func (a *AuditFile) Write(rec McpCallRecord) error {
	if a == nil || a.f == nil {
		return fmt.Errorf("audit writer is nil")
	}
	if rec.Ts.IsZero() {
		rec.Ts = time.Now()
	}
	if rec.Source == "" {
		rec.Source = exec.SourceMCP
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	_, err = a.f.Write(append(line, '\n'))
	return err
}

// Close flushes pending data and releases the file handle. Safe to call
// multiple times; subsequent calls are no-ops.
func (a *AuditFile) Close() error {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.f == nil {
		return nil
	}
	err := a.f.Close()
	a.f = nil
	return err
}
