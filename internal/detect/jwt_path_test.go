package detect

import (
	"bytes"
	"testing"
)

const fixtureJWT = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJ1c2VyXzkxMjM3IiwiaWF0IjoxNzQ2MDAwMDAwfQ.s9bF2qLk-7T0GhM3xJpW1aNcQ8YvR2zIeKwUHfVbTjA"

func TestFindMatchesJWTProductionPath(t *testing.T) {
	rules, err := BuiltinRules()
	if err != nil {
		t.Fatalf("BuiltinRules: %v", err)
	}
	jwt := mustRule(t, rules, "JWT")

	for _, tc := range []struct {
		name  string
		input string
	}{
		{"standalone", fixtureJWT},
		{"bearer_lower", "bearer " + fixtureJWT},
		{"bearer_upper", "Bearer " + fixtureJWT},
		{"authorization_header", "Authorization: Bearer " + fixtureJWT},
		{"token_assignment", "token=" + fixtureJWT},
		{"long_log_line", "2026-04-28T13:15:01Z ERROR auth: invalid bearer token " + fixtureJWT + " from 203.0.113.42"},
		{"line_start", fixtureJWT + " rejected"},
		{"line_end", "rejected " + fixtureJWT},
		{"json_value", `{"authorization":"Bearer ` + fixtureJWT + `"}`},
		{"trailing_comma", "token=" + fixtureJWT + ", more"},
		{"trailing_paren", "(" + fixtureJWT + ")"},
		{"trailing_bracket", "[" + fixtureJWT + "]"},
		{"trailing_brace", "{" + fixtureJWT + "}"},
		{"url_query_amp", "https://x?token=" + fixtureJWT + "&id=42"},
		{"trailing_equals", "jwt=" + fixtureJWT + "=other"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ms := FindMatches([]Rule{jwt}, []byte(tc.input))
			if len(ms) != 1 {
				t.Fatalf("matches = %d (%v), want 1", len(ms), values(ms))
			}
			if got := string(ms[0].Value); got != fixtureJWT {
				t.Fatalf("match = %q, want fixture JWT", got)
			}
			if !bytes.Equal(ms[0].Value, []byte(tc.input)[ms[0].Start:ms[0].End]) {
				t.Fatalf("Value/[Start:End] mismatch: %q vs %q", ms[0].Value, []byte(tc.input)[ms[0].Start:ms[0].End])
			}
		})
	}
}

func TestFindMatchesJWTRejectsDottedNonJWT(t *testing.T) {
	rules, err := BuiltinRules()
	if err != nil {
		t.Fatalf("BuiltinRules: %v", err)
	}
	jwt := mustRule(t, rules, "JWT")

	for _, input := range []string{
		"eyJub3QiOiJqd3QifQ.eyJub19jbGFpbSI6dHJ1ZX0.signaturepart",
		"eyJhbGciOiJIUzI1NiJ9.bm90LWpzb24.signaturepart",
		"eyJhbGciOiJIUzI1NiJ9.eyJub19jbGFpbSI6dHJ1ZX0.signaturepart",
		"not.a.jwt",
	} {
		t.Run(input, func(t *testing.T) {
			if ms := FindMatches([]Rule{jwt}, []byte(input)); len(ms) != 0 {
				t.Fatalf("matches = %v, want none", values(ms))
			}
		})
	}
}

func mustRule(t *testing.T, rules []Rule, name string) Rule {
	t.Helper()
	for _, r := range rules {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("rule %s not found", name)
	return Rule{}
}

func values(ms []Match) []string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, string(m.Value))
	}
	return out
}
