package corpus

import (
	"strings"
	"testing"
)

func TestUnifiedDiffEmptyOnEqual(t *testing.T) {
	if got := UnifiedDiff([]byte("abc\n"), []byte("abc\n")); got != "" {
		t.Fatalf("expected empty diff, got %q", got)
	}
	if got := UnifiedDiff(nil, nil); got != "" {
		t.Fatalf("expected empty diff for nil/nil, got %q", got)
	}
}

func TestUnifiedDiffSingleLineChange(t *testing.T) {
	expected := "alpha\nbeta\ngamma\n"
	got := "alpha\nBETA\ngamma\n"
	d := UnifiedDiff([]byte(expected), []byte(got))
	if d == "" {
		t.Fatal("expected non-empty diff")
	}
	if !strings.Contains(d, "--- expected\n") || !strings.Contains(d, "+++ got\n") {
		t.Fatalf("missing headers:\n%s", d)
	}
	if !strings.Contains(d, "-beta\n") || !strings.Contains(d, "+BETA\n") {
		t.Fatalf("missing change markers:\n%s", d)
	}
	if !strings.Contains(d, " alpha\n") || !strings.Contains(d, " gamma\n") {
		t.Fatalf("missing context lines:\n%s", d)
	}
	if !strings.Contains(d, "@@") {
		t.Fatalf("missing hunk header:\n%s", d)
	}
}

func TestUnifiedDiffMultipleHunks(t *testing.T) {
	// Build inputs with two distant changes separated by enough context
	// that buildHunks splits them.
	exp := "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\nm\nn\no\np\n"
	got := "A\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\nm\nn\no\nP\n"
	d := UnifiedDiff([]byte(exp), []byte(got))
	if d == "" {
		t.Fatalf("expected non-empty diff")
	}
	headers := strings.Count(d, "@@")
	// Each hunk produces one "@@" line, formatted as `@@ ... @@`. Each hunk
	// header therefore contains "@@" twice (open + close), giving 2 per hunk.
	if headers < 4 {
		t.Fatalf("expected >=2 hunks (>=4 @@ markers), got %d:\n%s", headers, d)
	}
}

func TestUnifiedDiffAddedLine(t *testing.T) {
	exp := "a\nb\nc\n"
	got := "a\nb\nNEW\nc\n"
	d := UnifiedDiff([]byte(exp), []byte(got))
	if !strings.Contains(d, "+NEW\n") {
		t.Fatalf("expected +NEW line:\n%s", d)
	}
}

func TestUnifiedDiffRemovedLine(t *testing.T) {
	exp := "a\nb\nc\n"
	got := "a\nc\n"
	d := UnifiedDiff([]byte(exp), []byte(got))
	if !strings.Contains(d, "-b\n") {
		t.Fatalf("expected -b line:\n%s", d)
	}
}

func TestUnifiedDiffTrailingNewlineDifference(t *testing.T) {
	exp := "a\nb\n"
	got := "a\nb"
	d := UnifiedDiff([]byte(exp), []byte(got))
	if d == "" {
		t.Fatalf("trailing-newline difference should produce non-empty diff")
	}
}
