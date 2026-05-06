package corpus

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCorpusRootFromPackageDir(t *testing.T) {
	root, err := CorpusRoot()
	if err != nil {
		t.Fatalf("CorpusRoot: %v", err)
	}
	if !filepath.IsAbs(root) {
		t.Fatalf("expected absolute path, got %q", root)
	}
	if !strings.HasSuffix(filepath.ToSlash(root), "/testdata/corpus") {
		t.Fatalf("unexpected suffix: %q", root)
	}
}

func TestCorpusRootFromDeepSubdir(t *testing.T) {
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	want, err := CorpusRoot()
	if err != nil {
		t.Fatalf("CorpusRoot baseline: %v", err)
	}
	// Move into an existing subdirectory deeper in the repo.
	deep := filepath.Join(orig, "..", "engine")
	if err := os.Chdir(deep); err != nil {
		t.Skipf("cannot chdir to %s: %v", deep, err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
	got, err := CorpusRoot()
	if err != nil {
		t.Fatalf("CorpusRoot from deep: %v", err)
	}
	if got != want {
		t.Fatalf("root differs by cwd: deep=%q baseline=%q", got, want)
	}
}

func TestCorpusRootOutsideModule(t *testing.T) {
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
	_, err = CorpusRoot()
	if !errors.Is(err, ErrNoModule) {
		t.Fatalf("expected ErrNoModule, got %v", err)
	}
}
