package corpus

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateScenarioName(t *testing.T) {
	good := []string{
		"k8s-secret-yaml-multidoc",
		"abc",
		"a1b",
		"foo-bar",
		"123-abc",
	}
	for _, n := range good {
		t.Run("ok/"+n, func(t *testing.T) {
			if err := ValidateScenarioName(n); err != nil {
				t.Fatalf("expected ok, got %v", err)
			}
		})
	}

	bad := []string{
		"",        // empty
		"..",      // traversal
		"../foo",  // traversal
		"foo/bar", // separator
		`foo\bar`, // backslash separator
		"-foo",    // leading hyphen
		"foo-",    // trailing hyphen
		"FOO",     // uppercase
		"foo_bar", // underscore
		"foo bar", // whitespace
		"a",       // too short (1)
		"ab",      // too short (2)
		"aa",      // too short (2) - the '+' quantifier rejects this
	}
	for _, n := range bad {
		t.Run("bad/"+n, func(t *testing.T) {
			if err := ValidateScenarioName(n); err == nil {
				t.Fatalf("expected error for %q, got nil", n)
			}
		})
	}
}

func TestScenarioPathContainment(t *testing.T) {
	root := t.TempDir()
	got, err := ScenarioPath(root, "valid-name")
	if err != nil {
		t.Fatalf("ScenarioPath: %v", err)
	}
	abs, _ := filepath.Abs(root)
	if !strings.HasPrefix(got, abs) {
		t.Fatalf("expected %q under %q", got, abs)
	}
	rel, err := filepath.Rel(abs, got)
	if err != nil || rel != "valid-name" {
		t.Fatalf("rel = %q (err=%v), want valid-name", rel, err)
	}
}

