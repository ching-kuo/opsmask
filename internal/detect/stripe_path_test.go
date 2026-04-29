package detect

import "testing"

func TestFindMatchesStripeRulesProductionPath(t *testing.T) {
	rules, err := BuiltinRules()
	if err != nil {
		t.Fatalf("BuiltinRules: %v", err)
	}
	stripeKey := mustRule(t, rules, "StripeAccessToken")
	webhook := mustRule(t, rules, "StripeWebhookSecret")
	objectID := mustRule(t, rules, "StripeObjectID")

	for _, tc := range []struct {
		name  string
		rule  Rule
		input string
		want  string
	}{
		{"secret_key", stripeKey, "api_key=sk_live_" + "51HmvK2EpZ8qWnBxRzLcA0vYtJfNd from 192.0.2.115", "sk_live_" + "51HmvK2EpZ8qWnBxRzLcA0vYtJfNd"},
		{"restricted_key", stripeKey, "key=rk_test_" + "51HmvK2EpZ8qWnBxRzLcA0vYtJfNd", "rk_test_" + "51HmvK2EpZ8qWnBxRzLcA0vYtJfNd"},
		{"webhook_secret", webhook, "stripe whsec_" + "1234567890abcdef1234567890", "whsec_" + "1234567890abcdef1234567890"},
		{"object_id", objectID, "stripe charge ch_3PkXbZ8FwL2JqK1A0vNcRtYu refused", "ch_3PkXbZ8FwL2JqK1A0vNcRtYu"},
		{"key_paren", stripeKey, "(sk_live_" + "51HmvK2EpZ8qWnBxRzLcA0vYtJfNd)", "sk_live_" + "51HmvK2EpZ8qWnBxRzLcA0vYtJfNd"},
		{"key_trailing_comma", stripeKey, "api_key=sk_live_" + "51HmvK2EpZ8qWnBxRzLcA0vYtJfNd,next", "sk_live_" + "51HmvK2EpZ8qWnBxRzLcA0vYtJfNd"},
		{"key_url_amp", stripeKey, "?key=sk_live_" + "51HmvK2EpZ8qWnBxRzLcA0vYtJfNd&id=42", "sk_live_" + "51HmvK2EpZ8qWnBxRzLcA0vYtJfNd"},
		{"key_bracket", stripeKey, "[sk_live_" + "51HmvK2EpZ8qWnBxRzLcA0vYtJfNd]", "sk_live_" + "51HmvK2EpZ8qWnBxRzLcA0vYtJfNd"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ms := FindMatches([]Rule{tc.rule}, []byte(tc.input))
			if len(ms) != 1 {
				t.Fatalf("matches = %d (%v), want 1", len(ms), values(ms))
			}
			if got := string(ms[0].Value); got != tc.want {
				t.Fatalf("match = %q, want %q", got, tc.want)
			}
		})
	}
}
