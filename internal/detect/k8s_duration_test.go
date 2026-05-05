package detect

import "testing"

// TestKubernetesRulesRejectDurationTokens guards against the false positive
// where `kubectl get nodes -o wide` worker-row ROLES column literally
// contains `node` and the adjacent AGE column (`10h`, `5d`, `30m`, `1d2h`,
// `2y190d`, `1.5s`) gets tokenized as a node name. Pattern matching is
// structurally correct — `node   10h` is `noun + whitespace + name` — so the
// rejection happens via the `notDurationToken` Check wired on every k8s*
// rule, not via tightening the regex.
//
// This test exercises the registered Rule (Regex + Check), not the bare
// regex from the spec.
func TestKubernetesRulesRejectDurationTokens(t *testing.T) {
	allRules, err := BuiltinRules()
	if err != nil {
		t.Fatalf("BuiltinRules: %v", err)
	}
	var nodeRule Rule
	for _, r := range allRules {
		if r.Name == "KubernetesNode" {
			nodeRule = r
			break
		}
	}
	if nodeRule.Regex == nil {
		t.Fatal("KubernetesNode rule not found")
	}
	for _, input := range []string{
		"worker-1 Ready node 10h v1.28",
		"worker-1 Ready node 5d v1.28",
		"worker-1 Ready node 30m v1.28",
		"worker-1 Ready node 1d2h v1.28",
		"worker-1 Ready node 2y190d v1.28",
		"worker-1 Ready node 1.5s v1.28",
	} {
		if ms := FindMatches([]Rule{nodeRule}, []byte(input)); len(ms) != 0 {
			t.Fatalf("KubernetesNode unexpectedly matched %q -> %q", input, ms[0].Value)
		}
	}
	for _, tc := range []struct {
		input    string
		wantName string
	}{
		{"node worker-1", "worker-1"},
		{"node node-04", "node-04"},
		{"node 10-worker", "10-worker"},
		{"node/k8s-master-3", "k8s-master-3"},
	} {
		ms := FindMatches([]Rule{nodeRule}, []byte(tc.input))
		if len(ms) != 1 {
			t.Fatalf("KubernetesNode on %q: got %d matches, want 1", tc.input, len(ms))
		}
		if string(ms[0].Value) != tc.wantName {
			t.Fatalf("KubernetesNode on %q: got %q, want %q", tc.input, ms[0].Value, tc.wantName)
		}
	}
}
