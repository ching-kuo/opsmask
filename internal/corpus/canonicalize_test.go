package corpus

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ching-kuo/opsmask/internal/detect"
)

func TestCanonicalize(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "unicode single token",
			in:   "host=⟪opsmask:hostname:0123456789abcdef⟫\n",
			want: "host=⟪opsmask:hostname:*⟫\n",
		},
		{
			name: "ascii single token",
			in:   "host=[[opsmask:hostname:0123456789abcdef]]\n",
			want: "host=[[opsmask:hostname:*]]\n",
		},
		{
			name: "two distinct unicode tokens preserve positions",
			in:   "a ⟪opsmask:hostname:0123456789abcdef⟫ b ⟪opsmask:hostname:fedcba9876543210⟫ c",
			want: "a ⟪opsmask:hostname:*⟫ b ⟪opsmask:hostname:*⟫ c",
		},
		{
			name: "mixed unicode and ascii",
			in:   "x⟪opsmask:ip4:0123456789abcdef⟫y[[opsmask:email:fedcba9876543210]]z",
			want: "x⟪opsmask:ip4:*⟫y[[opsmask:email:*]]z",
		},
		{
			name: "adjacent tokens no whitespace",
			in:   "⟪opsmask:hostname:0123456789abcdef⟫⟪opsmask:hostname:fedcba9876543210⟫",
			want: "⟪opsmask:hostname:*⟫⟪opsmask:hostname:*⟫",
		},
		{
			name: "multiline preserves structure",
			in:   "line1 ⟪opsmask:hostname:0123456789abcdef⟫\nline2 ⟪opsmask:ip4:fedcba9876543210⟫\n",
			want: "line1 ⟪opsmask:hostname:*⟫\nline2 ⟪opsmask:ip4:*⟫\n",
		},
		{
			name: "destroyed form unchanged",
			in:   "key=[REDACTED_AWS_KEY]\n",
			want: "key=[REDACTED_AWS_KEY]\n",
		},
		{
			name: "empty input",
			in:   "",
			want: "",
		},
		{
			name: "no tokens passes through",
			in:   "plain text with no tokens at all\n",
			want: "plain text with no tokens at all\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Canonicalize([]byte(tc.in))
			if string(got) != tc.want {
				t.Fatalf("Canonicalize:\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}

// TestCanonicalizeCoversAllBuiltinClasses asserts the canonicalizer regex
// character class covers every type produced by detect.BuiltinRules. A new
// detector with an unrecognized class character would surface here.
func TestCanonicalizeCoversAllBuiltinClasses(t *testing.T) {
	rules, err := detect.BuiltinRules()
	if err != nil {
		t.Fatalf("BuiltinRules: %v", err)
	}
	seen := map[string]bool{}
	for _, r := range rules {
		if seen[r.Type] {
			continue
		}
		seen[r.Type] = true
		// Build a synthetic ascii-form token for each type.
		tok := "[[opsmask:" + r.Type + ":0123456789abcdef]]"
		got := string(Canonicalize([]byte(tok)))
		want := "[[opsmask:" + r.Type + ":*]]"
		if got != want {
			t.Errorf("type %q did not canonicalize: got=%q want=%q", r.Type, got, want)
		}
		// Same for unicode form.
		utok := "⟪opsmask:" + r.Type + ":0123456789abcdef⟫"
		ugot := string(Canonicalize([]byte(utok)))
		uwant := "⟪opsmask:" + r.Type + ":*⟫"
		if ugot != uwant {
			t.Errorf("type %q unicode did not canonicalize: got=%q want=%q", r.Type, ugot, uwant)
		}
	}
	if len(seen) == 0 {
		t.Fatal("BuiltinRules returned zero rules")
	}
}

// Sanity: token regex still matches the canonicalized output's literal
// "*" placeholder positions are NOT matched as tokens (id requires 16 hex).
func TestCanonicalizeOutputNotReMatchable(t *testing.T) {
	in := []byte("x ⟪opsmask:hostname:0123456789abcdef⟫ y")
	out := Canonicalize(in)
	if bytes.Contains(out, []byte("0123456789abcdef")) {
		t.Fatalf("canonicalized output still contains original id: %s", out)
	}
	if !strings.Contains(string(out), ":*⟫") {
		t.Fatalf("expected wildcard form, got %s", out)
	}
}
