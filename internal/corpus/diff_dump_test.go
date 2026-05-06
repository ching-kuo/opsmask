package corpus

import (
	"strings"
	"testing"
)

func TestUnifiedDiffHunkHeadersAreAccurate(t *testing.T) {
	exp := "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\nm\nn\no\np\n"
	got := "A\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\nm\nn\no\nP\n"
	d := UnifiedDiff([]byte(exp), []byte(got))
	t.Logf("\n%s", d)
	// First hunk should start at line 1 with 4 lines on each side
	// (1 mutation + 3 trailing context). Second hunk should start
	// near line 13 (lines m, n, o, p) with 4 lines per side.
	if !strings.Contains(d, "@@ -1,4 +1,4 @@") {
		t.Errorf("missing first hunk header @@ -1,4 +1,4 @@:\n%s", d)
	}
	if !strings.Contains(d, "@@ -13,4 +13,4 @@") {
		t.Errorf("missing second hunk header @@ -13,4 +13,4 @@:\n%s", d)
	}
}
