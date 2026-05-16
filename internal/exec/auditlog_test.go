package exec_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	maskexec "github.com/ching-kuo/opsmask/internal/exec"
)

func setAuditDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Setenv("OPSMASK_AUDIT_DIR", dir)
	return dir
}

func TestNewRecordSetsSourceAndTimestamp(t *testing.T) {
	rec := maskexec.NewRecord(maskexec.SourceCLI)
	if rec.Source != "cli" {
		t.Fatalf("Source = %q, want cli", rec.Source)
	}
	if rec.Ts.IsZero() {
		t.Fatal("Ts is zero")
	}
}

func TestWriteRecordRejectsInvalidSource(t *testing.T) {
	dir := setAuditDir(t)
	cases := []struct {
		name string
		rec  maskexec.Record
	}{
		{"empty", maskexec.Record{Executable: "x"}},
		{"unknown", maskexec.Record{Source: "evil", Executable: "x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := maskexec.WriteRecord(tc.rec)
			if err == nil {
				t.Fatal("expected error for invalid Source")
			}
			if !strings.Contains(err.Error(), "invalid Record.Source") {
				t.Fatalf("unexpected error: %v", err)
			}
			// Must not have created the audit log.
			if _, err := os.Stat(filepath.Join(dir, "exec.log")); err == nil {
				t.Fatal("exec.log was written despite rejection")
			}
		})
	}
}

func TestWriteRecordRoundtrip(t *testing.T) {
	dir := setAuditDir(t)
	rec := maskexec.NewRecord(maskexec.SourceMCP)
	rec.Executable = "kubectl"
	rec.Argv = []string{"kubectl", "get", "pods"}
	rec.ExitCode = 0
	if err := maskexec.WriteRecord(rec); err != nil {
		t.Fatalf("WriteRecord: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "exec.log"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var decoded maskexec.Record
	if err := json.Unmarshal(bytes.TrimSpace(body), &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Source != "mcp" {
		t.Fatalf("Source = %q, want mcp", decoded.Source)
	}
}

func TestEnsureAuditDirHonorsEnv(t *testing.T) {
	dir := setAuditDir(t)
	got, err := maskexec.EnsureAuditDir()
	if err != nil {
		t.Fatalf("EnsureAuditDir: %v", err)
	}
	if got != dir {
		t.Fatalf("EnsureAuditDir = %q, want %q", got, dir)
	}
}

func TestEnsureAuditDirRefusesWideMode(t *testing.T) {
	dir := t.TempDir()
	// 0755 is wider than 0700 - the existing exec.log path rejects this and
	// EnsureAuditDir must do the same so the MCP path matches.
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Setenv("OPSMASK_AUDIT_DIR", dir)
	if _, err := maskexec.EnsureAuditDir(); err == nil {
		t.Fatal("expected error for world-readable audit dir")
	}
}

// TestExecLogMultiProcessAppend exercises POSIX O_APPEND atomicity across
// concurrent subprocess writers — proves the shared-directory guarantee that
// mcp_calls.jsonl piggy-backs on.
func TestExecLogMultiProcessAppend(t *testing.T) {
	if os.Getenv("OPSMASK_AUDIT_CHILD") == "1" {
		childAppend()
		return
	}
	if runtime.GOOS == "windows" {
		t.Skip("multi-process append semantics differ on Windows")
	}
	dir := setAuditDir(t)
	const procs, perProc = 4, 100

	var wg sync.WaitGroup
	for i := 0; i < procs; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			cmd := exec.Command(os.Args[0], "-test.run=TestExecLogMultiProcessAppend", "-test.v=false")
			cmd.Env = append(os.Environ(),
				"OPSMASK_AUDIT_CHILD=1",
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

	body, err := os.ReadFile(filepath.Join(dir, "exec.log"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var lines, valid int
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		lines++
		var rec maskexec.Record
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

func childAppend() {
	count := 0
	fmt.Sscanf(os.Getenv("OPSMASK_AUDIT_COUNT"), "%d", &count)
	tag := os.Getenv("OPSMASK_AUDIT_TAG")
	for i := 0; i < count; i++ {
		rec := maskexec.NewRecord(maskexec.SourceCLI)
		rec.Executable = tag
		rec.Argv = []string{tag, fmt.Sprintf("%d", i)}
		if err := maskexec.WriteRecord(rec); err != nil {
			fmt.Fprintf(os.Stderr, "write: %v\n", err)
			os.Exit(1)
		}
	}
}

// TestRecordLiteralASTDriftBlocksExternalConstruction is the drift-prevention
// guard required by the plan. It walks every Go file under the module root and
// flags any composite literal whose unqualified type name is "Record"
// constructed outside internal/exec. Any future call site that adds a
// `maskexec.Record{...}` literal — or any aliased import — fails this test
// and must use NewRecord instead.
//
// The check is intentionally syntax-level (not full type resolution) to keep
// the test self-contained without pulling go/types and its module loader. To
// prevent false positives on unrelated types named "Record" living in other
// packages, the test additionally verifies the import set of the offending
// file references internal/exec; files that import internal/exec and
// construct a "Record" literal are the ones we care about.
func TestRecordLiteralASTDriftBlocksExternalConstruction(t *testing.T) {
	root, err := moduleRoot()
	if err != nil {
		t.Fatalf("moduleRoot: %v", err)
	}
	var offenders []string
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			base := filepath.Base(path)
			switch base {
			case ".git", "vendor", "node_modules", "docs", "dist", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		// internal/exec is the package that owns the Record type and is allowed
		// to construct literals; the truncation fallback at encodeRecord uses
		// one. Skip the package directory.
		rel, _ := filepath.Rel(root, path)
		if strings.HasPrefix(rel, "internal/exec"+string(filepath.Separator)) {
			return nil
		}

		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if perr != nil {
			return nil // syntax errors are caught by `go build`; not our job
		}
		// Must import internal/exec (under any alias) for a Record{} literal
		// in this file to refer to maskexec.Record. Track three forms:
		// named alias (`al "..."`), default alias (`exec`), and dot
		// import (`. "..."`) which exposes Record as a bare identifier.
		execAlias := ""
		dotImported := false
		for _, imp := range f.Imports {
			if strings.Trim(imp.Path.Value, `"`) != "github.com/ching-kuo/opsmask/internal/exec" {
				continue
			}
			if imp.Name == nil {
				execAlias = "exec"
			} else if imp.Name.Name == "." {
				dotImported = true
			} else {
				execAlias = imp.Name.Name
			}
			break
		}
		if execAlias == "" && !dotImported {
			return nil
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok || lit.Type == nil {
				return true
			}
			switch t := lit.Type.(type) {
			case *ast.SelectorExpr:
				pkgIdent, ok := t.X.(*ast.Ident)
				if !ok {
					return true
				}
				if pkgIdent.Name == execAlias && t.Sel.Name == "Record" {
					pos := fset.Position(lit.Pos())
					offenders = append(offenders, fmt.Sprintf("%s:%d", pos.Filename, pos.Line))
				}
			case *ast.Ident:
				if dotImported && t.Name == "Record" {
					pos := fset.Position(lit.Pos())
					offenders = append(offenders, fmt.Sprintf("%s:%d (dot-import)", pos.Filename, pos.Line))
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(offenders) > 0 {
		t.Fatalf("Record composite literals outside internal/exec (use NewRecord):\n%s", strings.Join(offenders, "\n"))
	}
}

func moduleRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod found from %s", wd)
		}
		dir = parent
	}
}
