package rules

import "testing"

func TestHostnameRule(t *testing.T) {
	re := compile(t, "Hostname")
	for _, input := range []string{"api.example.com", "db-1.us-east-2.compute.internal", "node-04.cluster.local"} {
		if !re.MatchString(input) {
			t.Fatalf("hostname rule did not match %q", input)
		}
	}
	// Negative cases:
	// - 1.2.3.4 / 2026-04-28: numeric-only TLD or hyphen-separated date.
	// - cmd.Flags / strings.HasPrefix: Go dot-notation; uppercase + 2 labels.
	// - exec.go / config.go / package.json / worker.yaml: source-file paths;
	//   2 labels even when fully lowercase.
	// - example.com: 2-label public domain — intentionally not matched here;
	//   it would be caught by the Email or PasswordURL rule when relevant.
	for _, input := range []string{
		"1.2.3.4",
		"2026-04-28",
		"cmd.Flags",
		"cmd.Flags()",
		"strings.HasPrefix",
		"fmt.Errorf",
		"exec.go",
		"config.go",
		"package.json",
		"worker.yaml",
		"example.com",
	} {
		if re.MatchString(input) {
			t.Fatalf("hostname rule unexpectedly matched %q", input)
		}
	}
}

// Note: 3+ label dot-paths like `nova.api.openstack.wsgi` or
// `haproxy_latest.api.log` (after the underscore truncates LHS to
// `latest.api.log`) DO match the hostname regex — distinguishing them
// from real FQDNs requires knowing the TLD position. The rejection
// happens via the Public Suffix List backed Hostname Check wired in
// detect.BuiltinRules; that path is exercised in
// internal/detect/hostname_tld_test.go where the registered Rule
// (Regex + Check) is available.

func TestKubernetesPodRule(t *testing.T) {
	re := compile(t, "KubernetesPod")
	for _, input := range []string{"pod/api-7d4f-xyz", "kubectl get pod nginx-abc-123", "Pods named \"queue-worker-77\""} {
		if !re.MatchString(input) {
			t.Fatalf("k8s pod rule did not match %q", input)
		}
	}
	for _, input := range []string{"api-7d4f-xyz", "kubectl logs api"} {
		if re.MatchString(input) {
			t.Fatalf("k8s pod rule unexpectedly matched %q", input)
		}
	}
}

func TestKubernetesResourceSpecificRules(t *testing.T) {
	for _, tc := range []struct {
		rule  string
		input string
	}{
		{"KubernetesNamespace", "namespace/demo-safe"},
		{"KubernetesDeployment", "deployment/api"},
		{"KubernetesService", "svc/web"},
		{"KubernetesNode", "node/worker-1"},
		{"KubernetesNode", "Node worker-1"},
		{"KubernetesNode", "NODE worker-1"},
		{"KubernetesServiceAccount", "serviceaccount/default"},
	} {
		t.Run(tc.rule+"/"+tc.input, func(t *testing.T) {
			if !compile(t, tc.rule).MatchString(tc.input) {
				t.Fatalf("%s did not match %q", tc.rule, tc.input)
			}
		})
	}
}

// TestKubernetesNodeRejectsTabularHeaders guards against the false positive
// where `kubectl get pods -o wide` column headers `NODE   NOMINATED` were
// matched as `node + name=NOMINATED`. Resource names in Kubernetes are
// RFC 1123 DNS subdomains (lowercase only), so an uppercase token following
// the noun is never a real name.
func TestKubernetesNodeRejectsTabularHeaders(t *testing.T) {
	re := compile(t, "KubernetesNode")
	for _, input := range []string{
		"NODE   NOMINATED",
		"NODE   NOMINATED NODE",
		"Node   READINESS",
		"node   READY",
	} {
		if re.MatchString(input) {
			t.Fatalf("KubernetesNode rule unexpectedly matched %q", input)
		}
	}
}

// TestKubernetesRulesRejectHyphenSuffixNoun guards against the false positive
// where a resource whose own name *ends with* a noun (e.g. `nginx-ingress`,
// `app-configmap`) followed by a neighbor token would partial-match starting
// at the embedded noun, producing output like `nginx-[[opsmask:k8singress:…]]`.
// The prefix anchor `[^A-Za-z0-9_-]` excludes `-` so the noun must be a
// standalone word, not a hyphen-suffix.
func TestKubernetesRulesRejectHyphenSuffixNoun(t *testing.T) {
	for _, tc := range []struct {
		rule  string
		input string
	}{
		{"KubernetesIngress", "nginx-ingress nginx"},
		{"KubernetesIngress", "ladder-ingress class-default"},
		{"KubernetesConfigMap", "app-configmap data"},
		{"KubernetesConfigMap", "my-cm value"},
		{"KubernetesSecret", "tls-secret default"},
		{"KubernetesPod", "init-pod ready"},
	} {
		t.Run(tc.rule+"/"+tc.input, func(t *testing.T) {
			if compile(t, tc.rule).MatchString(tc.input) {
				t.Fatalf("%s rule unexpectedly matched %q", tc.rule, tc.input)
			}
		})
	}
}

// Guards two regressions: YAML newline separator and dotted-path tail.
func TestKubernetesRulesRejectYAMLAndPathSeparators(t *testing.T) {
	for _, tc := range []struct {
		rule  string
		input string
	}{
		{"KubernetesSecret", "kind: Secret\nmetadata:"},
		{"KubernetesSecret", "kind: Secret\n  metadata:"},
		{"KubernetesSecret", "/var/run/secrets/kubernetes.io/serviceaccount"},
		{"KubernetesConfigMap", "kind: ConfigMap\nmetadata:"},
		{"KubernetesNamespace", "kind: Namespace\nmetadata:"},
		{"KubernetesPod", "kind: Pod\nmetadata:"},
		{"KubernetesService", "/var/run/services/kubernetes.io/spec"},
	} {
		t.Run(tc.rule+"/"+tc.input, func(t *testing.T) {
			if compile(t, tc.rule).MatchString(tc.input) {
				t.Fatalf("%s rule unexpectedly matched %q", tc.rule, tc.input)
			}
		})
	}
}

// TestKubernetesRulesCaptureNameOnly ensures only the resource-name body is
// returned in SubMatch 1 (the engine replaces only that span). Verifies the
// noun and surrounding separator/quote are preserved in the masked output —
// e.g. `configmap "<name>" not found` becomes
// `configmap "[token]" not found`, not `[token]" not found`.
func TestKubernetesRulesCaptureNameOnly(t *testing.T) {
	for _, tc := range []struct {
		rule     string
		input    string
		wantName string
	}{
		{"KubernetesConfigMap", `configmap "my-app-config" not found`, "my-app-config"},
		{"KubernetesConfigMap", "configmap/my-app-config", "my-app-config"},
		{"KubernetesIngress", "ingress ladder-gateway", "ladder-gateway"},
		{"KubernetesIngress", "ingresses/ladder-gateway", "ladder-gateway"},
		{"KubernetesNode", "node/worker-1", "worker-1"},
		{"KubernetesNode", "Node worker-1", "worker-1"},
		{"KubernetesPod", `Pods named "queue-worker-77"`, "queue-worker-77"},
		{"KubernetesSecret", "secrets/my-app-secret", "my-app-secret"},
		{"KubernetesSecret", `secret "my-app-secret" not found`, "my-app-secret"},
		{"KubernetesPod", "pod/x", "x"},
	} {
		t.Run(tc.rule+"/"+tc.input, func(t *testing.T) {
			spec := findSpec(t, tc.rule)
			if spec.SubMatch != 1 {
				t.Fatalf("%s SubMatch = %d, want 1", tc.rule, spec.SubMatch)
			}
			m := compile(t, tc.rule).FindStringSubmatch(tc.input)
			if m == nil {
				t.Fatalf("%s did not match %q", tc.rule, tc.input)
			}
			if m[1] != tc.wantName {
				t.Fatalf("%s SubMatch 1 = %q, want %q (full match %q)", tc.rule, m[1], tc.wantName, m[0])
			}
		})
	}
}
