package detect

import (
	"bytes"
	"testing"
)

// Exercises the full production path for IPv4 detection: BuiltinRules ->
// FindMatches -> ruleFindAll -> scanWindows -> SubMatch index extraction. This
// guards against bugs in the SubMatch wiring that the rules-package regex-only
// tests would not catch (keyword window, offset arithmetic, byte slicing).
func TestFindMatchesIPv4SubMatchPath(t *testing.T) {
	rules, err := BuiltinRules()
	if err != nil {
		t.Fatalf("BuiltinRules: %v", err)
	}
	var ipv4 Rule
	for _, r := range rules {
		if r.Name == "IPv4" {
			ipv4 = r
			break
		}
	}
	if ipv4.Regex == nil {
		t.Fatal("IPv4 rule not found")
	}
	if ipv4.SubMatch != 1 {
		t.Fatalf("IPv4 SubMatch = %d, want 1", ipv4.SubMatch)
	}

	for _, tc := range []struct {
		name  string
		input string
		want  []string
	}{
		{"yaml_escaped_hosts", `"10.0.0.38\n10.0.0.47\t10.0.0.57"`, []string{"10.0.0.38", "10.0.0.47", "10.0.0.57"}},
		{"plain_with_space", "host: 10.0.0.1 port: 80", []string{"10.0.0.1"}},
		{"newline_separated", "ip1=10.0.0.1\nip2=10.0.0.2", []string{"10.0.0.1", "10.0.0.2"}},
		{"letter_prefix_no_match", "host10.0.0.1", nil},
		{"underscore_prefix_no_match", "_10.0.0.1", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ms := FindMatches([]Rule{ipv4}, []byte(tc.input))
			got := make([]string, 0, len(ms))
			for _, m := range ms {
				got = append(got, string(m.Value))
				if !bytes.Equal(m.Value, []byte(tc.input)[m.Start:m.End]) {
					t.Fatalf("Value/[Start:End] mismatch: %q vs %q", m.Value, []byte(tc.input)[m.Start:m.End])
				}
			}
			if len(got) != len(tc.want) {
				t.Fatalf("matches = %v, want %v", got, tc.want)
			}
			for i, v := range tc.want {
				if got[i] != v {
					t.Fatalf("matches[%d] = %q, want %q", i, got[i], v)
				}
			}
		})
	}
}
