package corpus

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverHappyPath(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "alpha", "input.txt"), "x")
	writeFile(t, filepath.Join(root, "alpha", "expected.txt"), "x")
	writeFile(t, filepath.Join(root, "bravo", "input.txt"), "y")
	writeFile(t, filepath.Join(root, "bravo", "expected.txt"), "y")
	got, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d scenarios, want 2", len(got))
	}
	if got[0].Name != "alpha" || got[1].Name != "bravo" {
		t.Fatalf("unexpected names: %+v", got)
	}
}

func TestDiscoverEmptyRootReturnsEmpty(t *testing.T) {
	root := t.TempDir()
	got, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty, got %+v", got)
	}
}

func TestDiscoverMissingRootReturnsEmpty(t *testing.T) {
	root := filepath.Join(t.TempDir(), "does-not-exist")
	got, err := Discover(root)
	if err != nil {
		t.Fatalf("expected nil err for missing root, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty slice, got %+v", got)
	}
}

func TestDiscoverMissingInputFails(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "broken", "expected.txt"), "x")
	_, err := Discover(root)
	if err == nil || !strings.Contains(err.Error(), "broken") || !strings.Contains(err.Error(), "input.txt") {
		t.Fatalf("expected error naming broken/input.txt, got %v", err)
	}
}

func TestDiscoverMissingExpectedFails(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "broken", "input.txt"), "x")
	_, err := Discover(root)
	if err == nil || !strings.Contains(err.Error(), "broken") || !strings.Contains(err.Error(), "expected.txt") {
		t.Fatalf("expected error naming broken/expected.txt, got %v", err)
	}
}

func TestDiscoverIgnoresHidden(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".hidden", "input.txt"), "x")
	got, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty (.hidden ignored), got %+v", got)
	}
}
