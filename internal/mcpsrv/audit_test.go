package mcpsrv_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/ching-kuo/opsmask/internal/mcpsrv"
)

func setMcpAuditDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Setenv("OPSMASK_AUDIT_DIR", dir)
	return dir
}

func TestOpenAuditWriterRoundtrip(t *testing.T) {
	dir := setMcpAuditDir(t)
	w, err := mcpsrv.OpenAuditWriter()
	if err != nil {
		t.Fatalf("OpenAuditWriter: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	for i := 0; i < 100; i++ {
		err := w.Write(mcpsrv.McpCallRecord{
			Tool:            "mask_text",
			OK:              true,
			ResultSizeBytes: 42,
			ArgsSummary:     map[string]any{"size": 42, "ascii": false},
		})
		if err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	body, err := os.ReadFile(filepath.Join(dir, "mcp_calls.jsonl"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	count := bytes.Count(body, []byte("\n"))
	if count != 100 {
		t.Fatalf("got %d records, want 100", count)
	}
	// Spot-check the first record decodes cleanly with source=mcp.
	first, _, _ := bytes.Cut(body, []byte("\n"))
	var rec mcpsrv.McpCallRecord
	if err := json.Unmarshal(first, &rec); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if rec.Source != "mcp" {
		t.Fatalf("Source = %q, want mcp", rec.Source)
	}
}

func TestOpenAuditWriterEnforcesPrivateMode(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Setenv("OPSMASK_AUDIT_DIR", dir)
	if _, err := mcpsrv.OpenAuditWriter(); err == nil {
		t.Fatal("expected error for world-readable audit dir")
	}
}

func TestOpenAuditWriterFileMode(t *testing.T) {
	dir := setMcpAuditDir(t)
	w, err := mcpsrv.OpenAuditWriter()
	if err != nil {
		t.Fatalf("OpenAuditWriter: %v", err)
	}
	defer w.Close()
	if err := w.Write(mcpsrv.McpCallRecord{Tool: "ping"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "mcp_calls.jsonl"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("mcp_calls.jsonl mode = %o, want private", info.Mode().Perm())
	}
}

func TestMcpCallsLogMultiProcessAppend(t *testing.T) {
	if os.Getenv("OPSMASK_MCP_AUDIT_CHILD") == "1" {
		mcpAuditChild()
		return
	}
	if runtime.GOOS == "windows" {
		t.Skip("multi-process append semantics differ on Windows")
	}
	dir := setMcpAuditDir(t)
	const procs, perProc = 4, 100

	var wg sync.WaitGroup
	for i := 0; i < procs; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			cmd := exec.Command(os.Args[0], "-test.run=TestMcpCallsLogMultiProcessAppend", "-test.v=false")
			cmd.Env = append(os.Environ(),
				"OPSMASK_MCP_AUDIT_CHILD=1",
				"OPSMASK_AUDIT_DIR="+dir,
				fmt.Sprintf("OPSMASK_AUDIT_TAG=p%d", idx),
				fmt.Sprintf("OPSMASK_AUDIT_COUNT=%d", perProc),
			)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Errorf("child %d: %v\n%s", idx, err, out)
			}
		}(i)
	}
	wg.Wait()

	body, err := os.ReadFile(filepath.Join(dir, "mcp_calls.jsonl"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var lines, valid int
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		lines++
		var rec mcpsrv.McpCallRecord
		if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
			t.Errorf("line %d not valid JSON: %v", lines, err)
			continue
		}
		valid++
	}
	if want := procs * perProc; lines != want {
		t.Fatalf("got %d lines, want %d", lines, want)
	}
	if valid != lines {
		t.Fatalf("got %d valid JSON lines out of %d", valid, lines)
	}
}

func mcpAuditChild() {
	count := 0
	fmt.Sscanf(os.Getenv("OPSMASK_AUDIT_COUNT"), "%d", &count)
	tag := os.Getenv("OPSMASK_AUDIT_TAG")
	w, err := mcpsrv.OpenAuditWriter()
	if err != nil {
		fmt.Fprintf(os.Stderr, "open: %v\n", err)
		os.Exit(1)
	}
	defer w.Close()
	for i := 0; i < count; i++ {
		if err := w.Write(mcpsrv.McpCallRecord{Tool: tag, OK: true, ResultSizeBytes: i}); err != nil {
			fmt.Fprintf(os.Stderr, "write: %v\n", err)
			os.Exit(1)
		}
	}
}
