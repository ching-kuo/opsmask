package detect

import "testing"

// TestFindMatchesTrailingDelimiterAudit exercises every Destroy rule against
// the punctuation contexts that real logs produce: assignments, query strings,
// JSON envelopes, list separators, and end-of-line. The original JWT regression
// (memory observation 2715) and the OpenAI hyphen-termination report (memory
// observation 2717) both come from rules whose trailing-delimiter group
// excluded part of the token's own charset; this test guards every rule of
// that shape so the same class of bug cannot reappear in a future port.
func TestFindMatchesTrailingDelimiterAudit(t *testing.T) {
	rules, err := BuiltinRules()
	if err != nil {
		t.Fatalf("BuiltinRules: %v", err)
	}

	// Tokens use a varied 36-char body because MinEntropy=3 is the floor for
	// most Gitleaks-derived rules; a monocharacter fixture would be rejected
	// regardless of regex correctness.
	body36 := "aB1cD2eF3gH4iJ5kL6mN7oP8qR9sT0uVwXyz"
	body82 := body36 + body36 + body36[:10] // 82 chars
	body93 := body36 + body36 + body36[:21] // 93 chars
	cases := []struct {
		rule  string
		token string
	}{
		{"JWT", fixtureJWT},
		{"AWSAccessKey", "AKIAIOSFODNN7EXAMPLE"},
		{"GitHubPAT", "ghp_" + body36},
		{"GitHubAppToken", "ghu_" + body36},
		{"GitHubOAuthToken", "gho_" + body36},
		{"GitHubRefreshToken", "ghr_" + body36},
		{"GitHubFineGrainedPAT", "github_pat_" + body82},
		{"GitLabPAT", "glpat-" + "aB1cD2eF3gH4iJ5kL6mN"},
		{"GitLabRunnerToken", "glrt-aB1cD2eF3gH4iJ5kL6mN"},
		{"SlackBotToken", "xoxb-" + "123456789012-987654321098-aB1cD2eF3gH4iJ5kL6mN"},
		{"OpenAIKey", "sk-" + body36[:20] + "T3BlbkFJ" + body36[16:]},
		{"AnthropicAPIKey", "sk-ant-api03-" + body93 + "AA"},
		{"StripeAccessToken", "sk_live_" + "51HmvK2EpZ8qWnBxRzLcA0vYtJfNd"},
		{"StripePublishableKey", "pk_live_" + "51HmvK2EpZ8qWnBxRzLcA0vYtJfNd"},
		{"StripeWebhookSecret", "whsec_" + "1aB2cD3eF4gH5iJ6kL7mN8oP"},
		{"GCPAPIKey", "AIzaSy" + body36[3:36]},
		{"TwilioAPIKey", "SK" + "0123456789abcdef0123456789ABCDEF"},
		{"NPMAccessToken", "npm_" + body36},
		{"SendGridAPIKey", "SG." + body36[:22] + "." + body36 + body36[:7]},
		{"DigitalOceanPAT", "dop_v1_fedcba9876543210" + "0123456789abcdef" + "fedcba9876543210" + "0123456789abcdef"},
		{"DigitalOceanOAuth", "doo_v1_fedcba9876543210" + "0123456789abcdef" + "fedcba9876543210" + "0123456789abcdef"},
		{"DigitalOceanRefresh", "dor_v1_fedcba9876543210" + "0123456789abcdef" + "fedcba9876543210" + "0123456789abcdef"},
		{"LinearAPIKey", "lin_api_" + body36 + body36[:4]},
		{"PostmanAPIKey", "PMAK-" + "1a2b3c4d5e6f0987abcdef01-fedcba9876543210abcdef0123456789ab"},
	}

	contexts := []struct {
		name string
		fmt  func(token string) string
	}{
		{"plain", func(t string) string { return t }},
		{"space_prefix", func(t string) string { return "prefix " + t + " suffix" }},
		{"trailing_comma", func(t string) string { return "a=" + t + ", next" }},
		{"trailing_paren", func(t string) string { return "(" + t + ")" }},
		{"trailing_bracket", func(t string) string { return "[" + t + "]" }},
		{"trailing_brace", func(t string) string { return "{" + t + "}" }},
		{"trailing_semicolon", func(t string) string { return "v=" + t + "; more" }},
		{"trailing_quote", func(t string) string { return `"` + t + `"` }},
		{"trailing_single_quote", func(t string) string { return "'" + t + "'" }},
		{"url_query_amp", func(t string) string { return "https://x?token=" + t + "&id=42" }},
		{"trailing_equals", func(t string) string { return "k=" + t + "=other" }},
		{"line_end", func(t string) string { return "value: " + t }},
		{"json_value", func(t string) string { return `{"k":"` + t + `"}` }},
		{"yaml_value", func(t string) string { return "token: " + t + "\nnext: line" }},
	}

	for _, tc := range cases {
		rule := mustRule(t, rules, tc.rule)
		for _, ctx := range contexts {
			input := ctx.fmt(tc.token)
			t.Run(tc.rule+"/"+ctx.name, func(t *testing.T) {
				ms := FindMatches([]Rule{rule}, []byte(input))
				if len(ms) == 0 {
					t.Fatalf("no match for %s in %q", tc.rule, input)
				}
				if got := string(ms[0].Value); got != tc.token {
					t.Fatalf("match = %q, want %q (input %q)", got, tc.token, input)
				}
			})
		}
	}
}

// TestFindMatchesFixedLengthHyphenAdjacent guards the specific report from
// memory observation 2717: a fixed-length secret followed by a hyphen-prefixed
// token must still be detected. The trailing-delimiter group originally
// excluded `-` and `_` for OpenAI/Anthropic/GCP rules; with fixed body length
// that exclusion is unnecessary and silently dropped real keys whose adjacent
// log token started with `-`.
func TestFindMatchesFixedLengthHyphenAdjacent(t *testing.T) {
	rules, err := BuiltinRules()
	if err != nil {
		t.Fatalf("BuiltinRules: %v", err)
	}
	body36 := "aB1cD2eF3gH4iJ5kL6mN7oP8qR9sT0uVwXyz"

	for _, tc := range []struct {
		name  string
		rule  string
		token string
	}{
		{"openai_legacy", "OpenAIKey", "sk-aB1cD2eF3gH4iJ5kL6mN" + "T3Blb" + "kFJ7oP8qR9sT0uVwXyZ012a"},
		{"anthropic_api", "AnthropicAPIKey", "sk-ant-api03-" + body36 + body36 + body36[:21] + "AA"},
		{"gcp_api_key", "GCPAPIKey", "AIzaSy" + body36[3:36]},
		// GCP body permits underscore and hyphen via [\w-]; cover the case where
		// the body itself contains hyphens AND the adjacent log token starts with
		// a hyphen — this is the most complex boundary case the trailing-delimiter
		// fix introduces.
		{"gcp_api_key_hyphen_in_body", "GCPAPIKey", "AIzaSy" + "-aB1cD-2eF3gH-4iJ5kL-6mN7oP-8qR9s"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rule := mustRule(t, rules, tc.rule)
			input := "key=" + tc.token + "-rotated"
			ms := FindMatches([]Rule{rule}, []byte(input))
			if len(ms) != 1 {
				t.Fatalf("matches = %d, want 1 in %q", len(ms), input)
			}
			if got := string(ms[0].Value); got != tc.token {
				t.Fatalf("match = %q, want %q", got, tc.token)
			}
		})
	}
}

// TestFindMatchesPyPIBoundary verifies the variable-length PyPI Macaroon body
// terminates at a non-`[A-Za-z0-9_-]` character and does not silently absorb
// adjacent underscore-prefixed log words. This guards the only new rule whose
// body permits underscore and hyphen, where over-extension is the failure
// mode the trailing-delimiter audit cannot otherwise prove safe.
func TestFindMatchesPyPIBoundary(t *testing.T) {
	rules, err := BuiltinRules()
	if err != nil {
		t.Fatalf("BuiltinRules: %v", err)
	}
	pypi := mustRule(t, rules, "PyPIUploadToken")
	body36 := "aB1cD2eF3gH4iJ5kL6mN7oP8qR9sT0uVwXyz"
	token := "pypi-AgEIcHlwaS5vcmc" + body36 + body36[:14]
	input := "token=" + token + ",metadata=foo"
	ms := FindMatches([]Rule{pypi}, []byte(input))
	if len(ms) != 1 {
		t.Fatalf("matches = %d, want 1 in %q", len(ms), input)
	}
	if got := string(ms[0].Value); got != token {
		t.Fatalf("match = %q, want %q", got, token)
	}
}

// TestFindMatchesStripeObjectIDLowEntropyNegatives asserts that the
// MinEntropy: 2 floor on StripeObjectID rejects monocharacter and other
// low-entropy app-local IDs that share a Stripe prefix. Without the floor,
// a sentinel like `tok_aaaaaaaaaaaaaa` (14 'a's) would pseudonymize and
// pollute output for users whose application happens to use the same prefix.
func TestFindMatchesStripeObjectIDLowEntropyNegatives(t *testing.T) {
	rules, err := BuiltinRules()
	if err != nil {
		t.Fatalf("BuiltinRules: %v", err)
	}
	rule := mustRule(t, rules, "StripeObjectID")
	for _, input := range []string{
		"tok_aaaaaaaaaaaaaa",
		"ba_xxxxxxxxxxxxxxxx",
		"src_yyyyyyyyyyyyyyyyy",
	} {
		t.Run(input, func(t *testing.T) {
			ms := FindMatches([]Rule{rule}, []byte(input))
			if len(ms) != 0 {
				t.Fatalf("matches = %v, want none for low-entropy input %q", values(ms), input)
			}
		})
	}
}
