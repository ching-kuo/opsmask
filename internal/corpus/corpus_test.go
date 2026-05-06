package corpus

import (
	"context"
	"os"
	"testing"
)

// TestCorpus walks testdata/corpus/, runs every scenario through the
// production engine pipeline, canonicalizes, and diffs against the
// committed expected.txt golden. Per-scenario sub-tests give callers
// `go test -run 'TestCorpus/<name>'` filtering and per-scenario CI failure
// attribution.
func TestCorpus(t *testing.T) {
	root, err := CorpusRoot()
	if err != nil {
		t.Fatalf("CorpusRoot: %v", err)
	}
	scenarios, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(scenarios) == 0 {
		t.Run("empty", func(t *testing.T) {})
		return
	}
	for _, sc := range scenarios {
		sc := sc
		t.Run(sc.Name, func(t *testing.T) {
			input, err := os.ReadFile(sc.InputPath)
			if err != nil {
				t.Fatalf("read input: %v", err)
			}
			expected, err := os.ReadFile(sc.ExpectedPath)
			if err != nil {
				t.Fatalf("read expected: %v", err)
			}
			masked, err := RunMask(context.Background(), input)
			if err != nil {
				t.Fatalf("RunMask: %v", err)
			}
			canon := Canonicalize(masked)
			if diff := Compare(expected, canon); diff != "" {
				t.Fatalf("scenario %q diff:\n%s", sc.Name, diff)
			}
		})
	}
}
