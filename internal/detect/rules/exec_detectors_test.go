package rules

import "testing"

func TestHostnameRule(t *testing.T) {
	re := compile(t, "Hostname")
	for _, input := range []string{"api.example.com", "db-1.us-east-2.compute.internal", "node-04.cluster.local"} {
		if !re.MatchString(input) {
			t.Fatalf("hostname rule did not match %q", input)
		}
	}
	for _, input := range []string{"1.2.3.4", "2026-04-28"} {
		if re.MatchString(input) {
			t.Fatalf("hostname rule unexpectedly matched %q", input)
		}
	}
}

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
		{"KubernetesServiceAccount", "serviceaccount/default"},
	} {
		t.Run(tc.rule, func(t *testing.T) {
			if !compile(t, tc.rule).MatchString(tc.input) {
				t.Fatalf("%s did not match %q", tc.rule, tc.input)
			}
		})
	}
}
