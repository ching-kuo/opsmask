package corpus

import (
	"strings"
	"testing"
)

func TestCompareEmptyOnMatch(t *testing.T) {
	if got := Compare([]byte("hello\n"), []byte("hello\n")); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

// TestCompareReportsMissingToken simulates the AE2-shaped failure path
// without corrupting any committed scenario.
func TestCompareReportsMissingToken(t *testing.T) {
	expected := []byte("user ⟪opsmask:email:*⟫ logged in\n")
	got := []byte("user alice@example.com logged in\n")
	d := Compare(expected, got)
	if d == "" {
		t.Fatal("expected non-empty diff")
	}
	if !strings.Contains(d, "-user ⟪opsmask:email:*⟫") {
		t.Fatalf("diff missing expected line marker:\n%s", d)
	}
	if !strings.Contains(d, "+user alice@example.com") {
		t.Fatalf("diff missing got line marker:\n%s", d)
	}
}
