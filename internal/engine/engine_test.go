package engine

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ching-kuo/llm-mask/internal/detect"
	"github.com/ching-kuo/llm-mask/internal/pseudo"
	"github.com/ching-kuo/llm-mask/internal/store"
)

func TestProcessMasksIdentifiersAndDestroysSecrets(t *testing.T) {
	st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "mapping.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rules, err := detect.BuiltinRules()
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	stats, err := Process(context.Background(),
		strings.NewReader("ip 10.0.0.1 user alice@example.com key AKIAIOSFODNN7EXAMPLE\n"),
		&out, rules, pseudo.New([]byte("01234567890123456789012345678901"), st), Options{ASCIITokens: true})
	if err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "[[llm-mask:ip4:") || !strings.Contains(s, "[[llm-mask:email:") || !strings.Contains(s, "[REDACTED_AWS_KEY]") {
		t.Fatalf("unexpected output: %s", s)
	}
	if stats.Masked != 2 || stats.Destroyed != 1 {
		t.Fatalf("stats=%+v", stats)
	}
}

func TestProcessMasksAcrossStreamingBoundary(t *testing.T) {
	st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "mapping.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rules, err := detect.BuiltinRules()
	if err != nil {
		t.Fatal(err)
	}
	prefix := strings.Repeat("a", 16<<20-6) + " user "
	input := prefix + "alice@example.com\n"
	var out bytes.Buffer
	_, err = Process(context.Background(), strings.NewReader(input), &out, rules, pseudo.New([]byte("01234567890123456789012345678901"), st), Options{ASCIITokens: true, MaxLine: len(input) + 1})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "[[llm-mask:email:") {
		t.Fatalf("email spanning boundary was not masked")
	}
	if strings.Contains(out.String(), "alice@example.com") {
		t.Fatalf("raw email leaked across boundary")
	}
}

func TestTokenFormUsesFirst8KiB(t *testing.T) {
	st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "mapping.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rules, err := detect.BuiltinRules()
	if err != nil {
		t.Fatal(err)
	}
	input := strings.Repeat("a", 100) + "☃" + strings.Repeat("b", tokenProbe) + " alice@example.com\n"
	var out bytes.Buffer
	_, err = Process(context.Background(), strings.NewReader(input), &out, rules, pseudo.New([]byte("01234567890123456789012345678901"), st), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "⟪llm-mask:email:") {
		t.Fatalf("expected unicode token based on first 8KiB, got %q", out.String()[len(out.String())-80:])
	}
}

func TestProcessMasksKnownGapFixture(t *testing.T) {
	st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "mapping.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rules, err := detect.BuiltinRules()
	if err != nil {
		t.Fatal(err)
	}
	input := []byte(strings.Join([]string{
		"auth: invalid bearer token eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJ1c2VyXzkxMjM3IiwiaWF0IjoxNzQ2MDAwMDAwfQ.s9bF2qLk-7T0GhM3xJpW1aNcQ8YvR2zIeKwUHfVbTjA from 10.0.0.1",
		"billing: api_key=sk_live_" + "51HmvK2EpZ8qWnBxRzLcA0vYtJfNd",
		"stripe charge ch_3PkXbZ8FwL2JqK1A0vNcRtYu",
	}, "\n"))
	var out bytes.Buffer
	_, err = Process(context.Background(), bytes.NewReader(input), &out, rules, pseudo.New([]byte("01234567890123456789012345678901"), st), Options{ASCIITokens: true})
	if err != nil {
		t.Fatal(err)
	}
	s := out.String()
	for _, leaked := range []string{
		"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
		"sk_live_" + "51HmvK2EpZ8qWnBxRzLcA0vYtJfNd",
		"ch_3PkXbZ8FwL2JqK1A0vNcRtYu",
	} {
		if strings.Contains(s, leaked) {
			t.Fatalf("fixture leaked %q in:\n%s", leaked, s)
		}
	}
	for _, want := range []string{"[REDACTED_JWT]", "[REDACTED_STRIPE_KEY]", "[[llm-mask:stripe_id:"} {
		if !strings.Contains(s, want) {
			t.Fatalf("fixture output missing %q in:\n%s", want, s)
		}
	}
}
