package detect

import "testing"

func TestHostnamePublicSuffixCheck(t *testing.T) {
	allRules, err := BuiltinRules()
	if err != nil {
		t.Fatalf("BuiltinRules: %v", err)
	}
	var hostRule Rule
	for _, r := range allRules {
		if r.Name == "Hostname" {
			hostRule = r
			break
		}
	}
	if hostRule.Regex == nil {
		t.Fatal("Hostname rule not found")
	}
	for _, input := range []string{
		"nova.api.openstack.wsgi",
		"keystone.server.flask",
		"neutron.api.wsgi",
		"neutron.plugins.ml2",
		"nova.api.openstack.compute.versions",
		"latest.api.log",
		"nova.api.log",
		"a.b.c.json",
		"foo.bar.yaml",
		"some.module.py",
		"pkg.subpkg.go",
		"some.module.rs",
		"path.to.script.sh",
		"notes.subdir.md",
		"cmd.Flags",
		"package.json",
	} {
		if ms := FindMatches([]Rule{hostRule}, []byte(input)); len(ms) != 0 {
			t.Fatalf("Hostname unexpectedly matched %q -> %q", input, ms[0].Value)
		}
	}
	for _, tc := range []struct {
		input string
		want  string
	}{
		{"api.example.com", "api.example.com"},
		{"node-04.cluster.local", "node-04.cluster.local"},
		{"db-1.us-east-2.compute.internal", "db-1.us-east-2.compute.internal"},
		{"connect to mail.example.org now", "mail.example.org"},
		{"bucket.s3.amazonaws.com", "bucket.s3.amazonaws.com"},
		{"app.py.example.com", "app.py.example.com"},
	} {
		ms := FindMatches([]Rule{hostRule}, []byte(tc.input))
		if len(ms) != 1 || string(ms[0].Value) != tc.want {
			t.Fatalf("Hostname on %q: got %d matches (first=%q), want 1 match %q",
				tc.input, len(ms), valueOrEmpty(ms), tc.want)
		}
	}

	greedy := "<nova.api.openstack.compute.versions.Versions object at 0x7f12>"
	if ms := FindMatches([]Rule{hostRule}, []byte(greedy)); len(ms) != 0 {
		t.Fatalf("Hostname unexpectedly matched greedy module path %q", ms[0].Value)
	}
}

func valueOrEmpty(ms []Match) string {
	if len(ms) == 0 {
		return ""
	}
	return string(ms[0].Value)
}
