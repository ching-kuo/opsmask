package rules

import (
	"regexp"
	"testing"

	"github.com/ching-kuo/opsmask/internal/policy"
)

func findSpec(t *testing.T, name string) Spec {
	t.Helper()
	for _, s := range Builtins() {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("rule %s not found", name)
	return Spec{}
}

func compile(t *testing.T, name string) *regexp.Regexp {
	t.Helper()
	return regexp.MustCompile(findSpec(t, name).Pattern)
}

func TestIPv6RuleRequiresEightGroupsOrCompression(t *testing.T) {
	re := compile(t, "IPv6")
	for _, tc := range []struct {
		name  string
		input string
		want  bool
	}{
		{"timestamp_three_groups", "16:23:37", false},
		{"timestamp_in_log_line", "2026-04-13 16:23:37.573 INFO", false},
		{"compressed_short", "2403:8ec0::100", true},
		{"compressed_with_zone_neighbor", " fe80::1 ", true},
		{"full_eight_groups", "2001:0db8:0000:0000:0000:ff00:0042:8329", true},
		{"compressed_double_only_no_word_anchor", " :: ", false},
		{"link_local", "fe80::1ff:fe23:4567:890a", true},
		{"four_groups_no_compression", "1:2:3:4", false},
		{"random_hex_runs", "abcd:ef01:2345:6789:abcd:ef01:2345:6789", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := re.MatchString(tc.input)
			if got != tc.want {
				t.Fatalf("MatchString(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestIPv4EscapeSequencePrefixSubMatch(t *testing.T) {
	spec := findSpec(t, "IPv4")
	if spec.SubMatch != 1 {
		t.Fatalf("IPv4 SubMatch = %d, want 1", spec.SubMatch)
	}
	re := regexp.MustCompile(spec.Pattern)
	for _, tc := range []struct {
		name     string
		input    string
		wantSub1 string
	}{
		{"plain", "10.0.0.1", "10.0.0.1"},
		{"space_prefix", " 10.0.0.1 ", "10.0.0.1"},
		{"newline_escape_prefix", `\n10.0.0.1 `, "10.0.0.1"},
		{"tab_escape_prefix", `\t10.0.0.1 `, "10.0.0.1"},
		{"yaml_hosts_value", `"10.0.0.1\n10.0.0.2\t10.0.0.3"`, "10.0.0.1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := re.FindStringSubmatch(tc.input)
			if m == nil || m[1] == "" {
				t.Fatalf("no submatch 1 in %q, want %q", tc.input, tc.wantSub1)
			}
			if m[1] != tc.wantSub1 {
				t.Fatalf("submatch 1 = %q, want %q", m[1], tc.wantSub1)
			}
		})
	}
}

func TestIPv4AllEscapesInYAMLHosts(t *testing.T) {
	re := regexp.MustCompile(findSpec(t, "IPv4").Pattern)
	input := `"10.0.0.38\n10.0.0.47\t10.0.0.57"`
	matches := re.FindAllStringSubmatch(input, -1)
	var ips []string
	for _, m := range matches {
		if len(m) > 1 && m[1] != "" {
			ips = append(ips, m[1])
		}
	}
	want := []string{"10.0.0.38", "10.0.0.47", "10.0.0.57"}
	if len(ips) != len(want) {
		t.Fatalf("got %v, want %v", ips, want)
	}
	for i, ip := range ips {
		if ip != want[i] {
			t.Fatalf("ips[%d] = %q, want %q", i, ip, want[i])
		}
	}
}

func TestHexIDRuleMatchesLongHex(t *testing.T) {
	re := compile(t, "HexID")
	for _, tc := range []struct {
		name  string
		input string
		want  bool
	}{
		{"openstack_tenant_id", "2fbc86f895ed4ef1ad036b7e4a068b50", true},
		{"sha1_git_hash", "356a192b7913b04c54574d18c28d46e6395428ab", true},
		{"sha256_hash", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", true},
		{"too_short_md5_minus_one", "2fbc86f895ed4ef1ad036b7e4a068b5", false},
		{"git_short_sha", "deadbeef", false},
		{"uuid_with_hyphens_no_match", "672a7d06-da75-2fe9-91ad-036b7e4a068b", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := re.MatchString(tc.input)
			if got != tc.want {
				t.Fatalf("MatchString(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestGitleaksDerivedSecretRules(t *testing.T) {
	for _, tc := range []struct {
		rule  string
		input string
	}{
		{"JWT", fixtureJWTForRules()},
		{"AWSAccessKey", "AKIAIOSFODNN7EXAMPLE"},
		{"GitHubPAT", "ghp_" + "0123456789abcdefABCDEF0123456789abcd"},
		{"GitHubFineGrainedPAT", "github_pat_" + repeat("a", 82)},
		{"GitLabPAT", "glpat-" + "0123456789abcdefghij"},
		{"SlackBotToken", "xoxb-" + "123456789012-123456789012-abcdefghijklmnop"},
		{"StripeAccessToken", "sk_live_" + "51HmvK2EpZ8qWnBxRzLcA0vYtJfNd"},
		{"GCPAPIKey", "AIza" + repeat("a", 35)},
		{"TwilioAPIKey", "SK" + "0123456789abcdef0123456789ABCDEF"},
		{"PEMPrivateKey", "-----BEGIN PRIVATE KEY-----\n" + repeat("a", 80) + "\n-----END PRIVATE KEY-----"},
		{"NPMAccessToken", "npm_" + repeat("a", 36)},
		{"PyPIUploadToken", "pypi-AgEIcHlwaS5vcmc" + repeat("a", 60)},
		{"SendGridAPIKey", "SG." + repeat("a", 22) + "." + repeat("b", 43)},
		{"DigitalOceanPAT", "dop_v1_" + repeat("a", 64)},
		{"DigitalOceanOAuth", "doo_v1_" + repeat("a", 64)},
		{"DigitalOceanRefresh", "dor_v1_" + repeat("a", 64)},
		{"LinearAPIKey", "lin_api_" + repeat("a", 40)},
		{"PostmanAPIKey", "PMAK-" + repeat("a", 24) + "-" + repeat("b", 34)},
	} {
		t.Run(tc.rule, func(t *testing.T) {
			if !compile(t, tc.rule).MatchString(tc.input) {
				t.Fatalf("%s did not match %q", tc.rule, tc.input)
			}
		})
	}
}

func TestStripeLocalExtensions(t *testing.T) {
	for _, tc := range []struct {
		rule  string
		input string
	}{
		{"StripePublishableKey", "pk_live_" + "51HmvK2EpZ8qWnBxRzLcA0vYtJfNd"},
		{"StripeWebhookSecret", "whsec_" + "1234567890abcdef1234567890"},
		{"StripeObjectID", "ch_3PkXbZ8FwL2JqK1A0vNcRtYu"},
		{"StripeObjectID", "cus_3PkXbZ8FwL2JqK1A0vNcRtYu"},
		{"StripeObjectID", "price_3PkXbZ8FwL2JqK1A0vNcRtYu"},
		{"StripeObjectID", "seti_3PkXbZ8FwL2JqK1A0vNcRtYu"},
		{"StripeObjectID", "ba_3PkXbZ8FwL2JqK1A0vNcRtYu"},
		{"StripeObjectID", "card_3PkXbZ8FwL2JqK1A0vNcRtYu"},
		{"StripeObjectID", "src_3PkXbZ8FwL2JqK1A0vNcRtYu"},
		{"StripeObjectID", "tok_3PkXbZ8FwL2JqK1A0vNcRtYu"},
		{"StripeObjectID", "txn_3PkXbZ8FwL2JqK1A0vNcRtYu"},
	} {
		t.Run(tc.rule+"/"+tc.input[:4], func(t *testing.T) {
			if !compile(t, tc.rule).MatchString(tc.input) {
				t.Fatalf("%s did not match %q", tc.rule, tc.input)
			}
		})
	}

	for _, input := range []string{
		"ch_short",
		"in_progress",
		"re_queued",
		"prod_environment",    // common infra prefix; underscore inside body breaks the [A-Za-z0-9]{14,} run
		"prod_useast_primary", // 14+ chars total but underscore splits the body run
		"price_default",       // app-local price tier, body too short
	} {
		if compile(t, "StripeObjectID").MatchString(input) {
			t.Fatalf("StripeObjectID unexpectedly matched %q", input)
		}
	}

	// Boundary: 14 base62 chars exactly should match (off-by-one guard).
	if !compile(t, "StripeObjectID").MatchString("prod_abcdefghijklmn") {
		t.Fatalf("StripeObjectID did not match 14-char boundary value")
	}
}

// TestDestroyRulesAreRegisteredAsBuiltinSecretTypes guards against future
// contributors adding a Destroy-policy built-in without registering its Type
// in policy.BuiltinSecretTypes(). Without that registration, user config
// could silently downgrade a credential rule to Pseudonymize.
func TestDestroyRulesAreRegisteredAsBuiltinSecretTypes(t *testing.T) {
	secretTypes := policy.BuiltinSecretTypes()
	for _, s := range Builtins() {
		if s.Policy != policy.Destroy {
			continue
		}
		if !secretTypes[s.Type] {
			t.Errorf("Destroy rule %q has Type %q not registered in policy.BuiltinSecretTypes()", s.Name, s.Type)
		}
	}
}

func fixtureJWTForRules() string {
	return "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJ1c2VyXzkxMjM3IiwiaWF0IjoxNzQ2MDAwMDAwfQ.s9bF2qLk-7T0GhM3xJpW1aNcQ8YvR2zIeKwUHfVbTjA"
}

func repeat(s string, n int) string {
	out := ""
	for range n {
		out += s
	}
	return out
}
