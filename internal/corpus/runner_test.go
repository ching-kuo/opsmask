package corpus

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunMaskHostnameProducesToken(t *testing.T) {
	out, err := RunMask(context.Background(), []byte("user alice@example.com\n"))
	if err != nil {
		t.Fatalf("RunMask: %v", err)
	}
	if bytes.Contains(out, []byte("alice@example.com")) {
		t.Fatalf("raw email leaked: %s", out)
	}
	if !strings.Contains(string(Canonicalize(out)), "opsmask:email:*") {
		t.Fatalf("expected canonicalized email token, got: %s", out)
	}
}

func TestRunMaskCanonicalIsSeedIndependent(t *testing.T) {
	// Two independent calls share fixedTestSecret today, but the assertion
	// is that canonicalized output is identical across runs - which is the
	// real invariant the corpus harness depends on.
	in := []byte("ip 10.0.0.1 user alice@example.com\n")
	a, err := RunMask(context.Background(), in)
	if err != nil {
		t.Fatalf("RunMask a: %v", err)
	}
	b, err := RunMask(context.Background(), in)
	if err != nil {
		t.Fatalf("RunMask b: %v", err)
	}
	ca := Canonicalize(a)
	cb := Canonicalize(b)
	if !bytes.Equal(ca, cb) {
		t.Fatalf("canonicalized outputs differ across runs:\n a=%s\n b=%s", ca, cb)
	}
}

func TestRunMaskCleansTempDir(t *testing.T) {
	// Snapshot opsmask-corpus-* entries in TempDir before and after.
	before := countCorpusTempDirs(t)
	if _, err := RunMask(context.Background(), []byte("hello\n")); err != nil {
		t.Fatalf("RunMask: %v", err)
	}
	after := countCorpusTempDirs(t)
	if after > before {
		t.Fatalf("RunMask leaked temp directories: before=%d after=%d", before, after)
	}
}

func countCorpusTempDirs(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		t.Fatalf("ReadDir TempDir: %v", err)
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "opsmask-corpus-") {
			n++
		}
	}
	return n
}

func TestRunMaskCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := RunMask(ctx, []byte("hello\n"))
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

// TestRunMaskCleansTempDirOnError verifies the deferred cleanup runs even
// when engine.Process returns an error. Plan U2 explicitly requires this.
func TestRunMaskCleansTempDirOnError(t *testing.T) {
	before := countCorpusTempDirs(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := RunMask(ctx, []byte("hello\n"))
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	after := countCorpusTempDirs(t)
	if after > before {
		t.Fatalf("RunMask leaked temp directories on error path: before=%d after=%d", before, after)
	}
}

// Sanity: RunMask does not write anywhere under the repo's testdata/corpus.
// We can't easily inspect "no writes occurred" without more plumbing; we
// assert only that the call succeeds even when CWD is a temp dir with no
// repo nearby (proves RunMask is self-contained).
func TestRunMaskWorksFromArbitraryCwd(t *testing.T) {
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
	if _, err := RunMask(context.Background(), []byte("user alice@example.com\n")); err != nil {
		t.Fatalf("RunMask from %s: %v", filepath.Base(tmp), err)
	}
}
