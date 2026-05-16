package rules

import "github.com/ching-kuo/opsmask/internal/policy"

type Spec struct {
	Name, Type, Pattern string
	Policy              policy.Policy
	Keywords            []string
	MaxMatchSpan        int
	SubMatch            int // if >0, use this capture group index as the match bounds
	MinEntropy          float64
}

// Builtins returns compact, high-precision Go RE2 patterns.
//
// Common secret/token rules are derived from the Gitleaks default configuration
// unless marked as a local opsmask extension. See docs/DETECTOR_RULES.md for
// the pinned upstream revision and porting rationale.
func Builtins() []Spec {
	return []Spec{
		// \b fails after YAML/JSON escape-sequence letters (n, t, r…) that immediately precede an IP.
		// Prefix is anchor/start-of-line, an escape-sequence letter (\n, \t, \r), or a non-word, non-digit
		// punctuation character — but NOT bare letters/underscore so we don't match `host10.0.0.1`.
		{Name: "IPv4", Type: "ip4", Pattern: `(?:^|\\[ntr]|[^0-9A-Za-z_])((?:25[0-5]|2[0-4]\d|1?\d?\d)(?:\.(?:25[0-5]|2[0-4]\d|1?\d?\d)){3})\b`, Policy: policy.Pseudonymize, Keywords: []string{"."}, MaxMatchSpan: 64, SubMatch: 1},
		{Name: "IPv6", Type: "ip6", Pattern: `\b(?:[0-9A-Fa-f]{1,4}(?::[0-9A-Fa-f]{1,4}){7}|(?:[0-9A-Fa-f]{1,4}(?::[0-9A-Fa-f]{1,4})*)?::(?:[0-9A-Fa-f]{1,4}(?::[0-9A-Fa-f]{1,4})*)?)\b`, Policy: policy.Pseudonymize, Keywords: []string{"::", ":"}, MaxMatchSpan: 128},
		{Name: "MAC", Type: "mac", Pattern: `\b[0-9A-Fa-f]{2}(?::[0-9A-Fa-f]{2}){5}\b`, Policy: policy.Pseudonymize, Keywords: []string{":"}, MaxMatchSpan: 64},
		{Name: "UUID", Type: "uuid", Pattern: `\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}\b`, Policy: policy.Pseudonymize, Keywords: []string{"-"}, MaxMatchSpan: 64},
		{Name: "HexID", Type: "hex_id", Pattern: `\b[0-9a-fA-F]{32,128}\b`, Policy: policy.Pseudonymize, MaxMatchSpan: 256},
		{Name: "Email", Type: "email", Pattern: `\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`, Policy: policy.Pseudonymize, Keywords: []string{"@"}, MaxMatchSpan: 320},
		// Lowercase + 3+ labels excludes source dot-notation (`cmd.Flags`)
		// and 2-label file extensions (`package.json`); 2-label public
		// domains are still covered by Email/PasswordURL.
		{Name: "Hostname", Type: "hostname", Pattern: `\b(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.){2,}[a-z]{2,24}\b`, Policy: policy.Pseudonymize, Keywords: []string{"."}, MaxMatchSpan: 256},
		k8s("KubernetesNamespace", "k8snamespace", `(?:namespaces?|ns|namespace)`, "namespace", "Namespace", "ns"),
		k8s("KubernetesPod", "k8spod", `pods?`, "pod", "Pod", "Pods"),
		k8s("KubernetesDeployment", "k8sdeployment", `deploy(?:ments?|ment)?`, "deploy", "deployment", "Deployment"),
		k8s("KubernetesService", "k8sservice", `(?:services?|svc)`, "svc", "service", "Service"),
		k8s("KubernetesReplicaSet", "k8sreplicaset", `(?:rs|replicasets?)`, "rs", "replicaset"),
		k8s("KubernetesDaemonSet", "k8sdaemonset", `(?:ds|daemonsets?)`, "ds", "daemonset"),
		k8s("KubernetesStatefulSet", "k8sstatefulset", `(?:sts|statefulsets?)`, "sts", "statefulset"),
		k8s("KubernetesConfigMap", "k8sconfigmap", `(?:cm|configmaps?)`, "cm", "configmap"),
		k8s("KubernetesSecret", "k8ssecret", `secrets?`, "secret", "Secret"),
		k8s("KubernetesIngress", "k8singress", `(?:ing|ingress(?:es)?)`, "ing", "ingress"),
		k8s("KubernetesPVC", "k8spvc", `pvc`, "pvc"),
		k8s("KubernetesPV", "k8spv", `pv`, "pv"),
		k8s("KubernetesNode", "k8snode", `nodes?`, "node", "Node"),
		k8s("KubernetesJob", "k8sjob", `jobs?`, "job", "Job"),
		k8s("KubernetesCronJob", "k8scronjob", `cronjobs?`, "cronjob", "CronJob"),
		k8s("KubernetesHPA", "k8shpa", `hpa`, "hpa"),
		k8s("KubernetesPDB", "k8spdb", `pdb`, "pdb"),
		k8s("KubernetesServiceAccount", "k8sserviceaccount", `(?:sa|serviceaccounts?)`, "sa", "serviceaccount"),
		// Gitleaks-derived common secret/token rules. Regexes are translated to
		// opsmask specs with SubMatch selecting the secret value when the
		// upstream rule includes surrounding delimiter context.
		secret("JWT", "jwt", `\b(ey[A-Za-z0-9]{17,}\.ey[A-Za-z0-9/_-]{17,}\.(?:[A-Za-z0-9/_-]{10,}={0,2})?)(?:[^A-Za-z0-9/_-]|$)`, []string{"ey"}, 4096, 1, 3),
		secret("PEMPrivateKey", "pem_private_key", `(?i)-----BEGIN[ A-Z0-9_-]{0,100}PRIVATE KEY(?: BLOCK)?-----[\s\S-]{64,}?KEY(?: BLOCK)?-----`, []string{"-----BEGIN", "-----begin"}, 4096, 0, 0),
		secret("AWSAccessKey", "aws_key", `\b((?:A3T[A-Z0-9]|AKIA|ASIA|ABIA|ACCA)[A-Z2-7]{16})\b`, []string{"A3T", "AKIA", "ASIA", "ABIA", "ACCA"}, 64, 1, 3),
		secret("GitHubAppToken", "github_token", `(?:ghu|ghs)_[0-9A-Za-z]{36}`, []string{"ghu_", "ghs_"}, 128, 0, 3),
		secret("GitHubFineGrainedPAT", "github_token", `github_pat_\w{82}`, []string{"github_pat_"}, 128, 0, 3),
		secret("GitHubOAuthToken", "github_token", `gho_[0-9A-Za-z]{36}`, []string{"gho_"}, 128, 0, 3),
		secret("GitHubPAT", "github_token", `ghp_[0-9A-Za-z]{36}`, []string{"ghp_"}, 128, 0, 3),
		secret("GitHubRefreshToken", "github_token", `ghr_[0-9A-Za-z]{36}`, []string{"ghr_"}, 128, 0, 3),
		secret("GitLabPAT", "gitlab_token", `glpat-[\w-]{20}`, []string{"glpat-"}, 128, 0, 3),
		secret("GitLabRoutablePAT", "gitlab_token", `\bglpat-[0-9A-Za-z_-]{27,300}\.[0-9a-z]{2}[0-9a-z]{7}\b`, []string{"glpat-"}, 320, 0, 4),
		secret("GitLabRunnerToken", "gitlab_token", `glrt-[0-9A-Za-z_-]{20}`, []string{"glrt-"}, 128, 0, 3),
		secret("GitLabCICDJobToken", "gitlab_token", `glcbt-[0-9A-Za-z]{1,5}_[0-9A-Za-z_-]{20}`, []string{"glcbt-"}, 128, 0, 3),
		secret("SlackAppToken", "slack_token", `(?i)xapp-\d-[A-Z0-9]+-\d+-[a-z0-9]+`, []string{"xapp", "XAPP"}, 320, 0, 2),
		secret("SlackBotToken", "slack_token", `xoxb-[0-9]{10,13}-[0-9]{10,13}[A-Za-z0-9-]*`, []string{"xoxb"}, 320, 0, 3),
		secret("SlackConfigToken", "slack_token", `(?i)xoxe\.xox[bp]-\d-[A-Z0-9]{163,166}`, []string{"xoxe.xoxb-", "xoxe.xoxp-"}, 320, 0, 2),
		secret("SlackRefreshToken", "slack_token", `(?i)xoxe-\d-[A-Z0-9]{146}`, []string{"xoxe-"}, 320, 0, 2),
		secret("SlackLegacyToken", "slack_token", `xox[os]-\d+-\d+-\d+-[A-Fa-f0-9]+`, []string{"xoxo", "xoxs"}, 320, 0, 2),
		secret("SlackLegacyWorkspaceToken", "slack_token", `xox[ar]-(?:\d-)?[0-9A-Za-z]{8,48}`, []string{"xoxa", "xoxr"}, 128, 0, 2),
		secret("SlackUserToken", "slack_token", `xox[pe](?:-[0-9]{10,13}){3}-[A-Za-z0-9-]{28,34}`, []string{"xoxp-", "xoxe-"}, 320, 0, 2),
		secret("SlackWebhookURL", "slack_token", `(?:https?://)?hooks\.slack\.com/(?:services|workflows|triggers)/[A-Za-z0-9+/]{43,56}`, []string{"hooks.slack.com"}, 320, 0, 0),
		// Fixed-length tokens: trailing delimiter excludes only the alphanumeric
		// charset (not `_`/`-`). The fixed body length already prevents greedy
		// over-extension, so a hyphen-prefixed adjacent token (e.g. `sk-...-rotated`)
		// must still be recognized as terminating the secret. See observation 2717
		// in memory.
		secret("OpenAIKey", "openai_key", `\b(sk-(?:proj|svcacct|admin)-(?:[A-Za-z0-9_-]{74}|[A-Za-z0-9_-]{58})T3BlbkFJ(?:[A-Za-z0-9_-]{74}|[A-Za-z0-9_-]{58})|sk-[A-Za-z0-9]{20}T3BlbkFJ[A-Za-z0-9]{20})(?:[^A-Za-z0-9]|$)`, []string{"T3BlbkFJ"}, 320, 1, 3),
		secret("AnthropicAdminKey", "anthropic_key", `\b(sk-ant-admin01-[A-Za-z0-9_-]{93}AA)(?:[^A-Za-z0-9]|$)`, []string{"sk-ant-admin01"}, 256, 1, 0),
		secret("AnthropicAPIKey", "anthropic_key", `\b(sk-ant-api03-[A-Za-z0-9_-]{93}AA)(?:[^A-Za-z0-9]|$)`, []string{"sk-ant-api03"}, 256, 1, 0),
		secret("StripeAccessToken", "stripe_key", `\b((?:sk|rk)_(?:test|live|prod)_[A-Za-z0-9]{10,99})(?:[^A-Za-z0-9]|$)`, []string{"sk_test", "sk_live", "sk_prod", "rk_test", "rk_live", "rk_prod"}, 160, 1, 2),
		secret("GCPAPIKey", "gcp_api_key", `\b(AIza[\w-]{35})(?:[^A-Za-z0-9]|$)`, []string{"AIza"}, 128, 1, 4),
		secret("TwilioAPIKey", "twilio_key", `\b(SK[0-9A-Fa-f]{32})\b`, []string{"SK"}, 64, 1, 3),
		secret("NPMAccessToken", "npm_token", `\b(npm_[A-Za-z0-9]{36})(?:[^A-Za-z0-9]|$)`, []string{"npm_"}, 64, 1, 3),
		secret("PyPIUploadToken", "pypi_token", `\b(pypi-AgEIcHlwaS5vcmc[A-Za-z0-9_-]{50,})(?:[^A-Za-z0-9_-]|$)`, []string{"pypi-AgEIcHlwaS5vcmc"}, 4096, 1, 3),
		secret("SendGridAPIKey", "sendgrid_key", `\b(SG\.[A-Za-z0-9_-]{16,32}\.[A-Za-z0-9_-]{16,64})(?:[^A-Za-z0-9_-]|$)`, []string{"SG."}, 256, 1, 3),
		secret("DigitalOceanPAT", "digitalocean_token", `\b(dop_v1_[a-f0-9]{64})(?:[^A-Za-z0-9]|$)`, []string{"dop_v1_"}, 128, 1, 3),
		secret("DigitalOceanOAuth", "digitalocean_token", `\b(doo_v1_[a-f0-9]{64})(?:[^A-Za-z0-9]|$)`, []string{"doo_v1_"}, 128, 1, 3),
		secret("DigitalOceanRefresh", "digitalocean_token", `\b(dor_v1_[a-f0-9]{64})(?:[^A-Za-z0-9]|$)`, []string{"dor_v1_"}, 128, 1, 3),
		secret("LinearAPIKey", "linear_token", `\b(lin_api_[A-Za-z0-9]{40})(?:[^A-Za-z0-9]|$)`, []string{"lin_api_"}, 128, 1, 3),
		secret("PostmanAPIKey", "postman_key", `\b(PMAK-[a-fA-F0-9]{24}-[a-fA-F0-9]{34})(?:[^A-Za-z0-9]|$)`, []string{"PMAK-"}, 128, 1, 3),

		// Local opsmask extensions for LLM-bound log masking gaps not covered by
		// the curated Gitleaks-derived baseline.
		{Name: "PasswordURL", Type: "password_url", Pattern: `\b[a-zA-Z][a-zA-Z0-9+.-]*://[^\s:/@]+:[^\s/@]+@[^\s]+`, Policy: policy.Destroy, Keywords: []string{"://"}, MaxMatchSpan: 4096},
		{Name: "GCPServiceAccount", Type: "gcp_sa", Pattern: `"type"\s*:\s*"service_account"`, Policy: policy.Destroy, Keywords: []string{"service_account"}, MaxMatchSpan: 1024},
		{Name: "StripePublishableKey", Type: "stripe_publishable_key", Pattern: `\b(pk_(?:test|live|prod)_[A-Za-z0-9]{10,99})(?:[^A-Za-z0-9]|$)`, Policy: policy.Destroy, Keywords: []string{"pk_test", "pk_live", "pk_prod"}, MaxMatchSpan: 160, SubMatch: 1, MinEntropy: 2},
		{Name: "StripeWebhookSecret", Type: "stripe_webhook_secret", Pattern: `\b(whsec_[A-Za-z0-9]{16,})(?:[^A-Za-z0-9]|$)`, Policy: policy.Destroy, Keywords: []string{"whsec_"}, MaxMatchSpan: 256, SubMatch: 1, MinEntropy: 2},
		// MinEntropy: 2 keeps real base62 Stripe IDs (always >= ~5 bits/char) while
		// rejecting low-entropy app-local IDs that share a prefix such as
		// `tok_aaaaaaaaaaaaaa` or `ba_xxxxxxxxxxxxxx`.
		{Name: "StripeObjectID", Type: "stripe_id", Pattern: `\b(?:ch|cus|pi|sub|in|re|evt|pm|prod|price|seti|ba|card|src|tok|txn)_[A-Za-z0-9]{14,}\b`, Policy: policy.Pseudonymize, Keywords: []string{"ch_", "cus_", "pi_", "sub_", "in_", "re_", "evt_", "pm_", "prod_", "price_", "seti_", "ba_", "card_", "src_", "tok_", "txn_"}, MaxMatchSpan: 64, MinEntropy: 2},
	}
}

func secret(name, typ, pattern string, keywords []string, maxSpan, subMatch int, minEntropy float64) Spec {
	return Spec{
		Name:         name,
		Type:         typ,
		Pattern:      pattern,
		Policy:       policy.Destroy,
		Keywords:     keywords,
		MaxMatchSpan: maxSpan,
		SubMatch:     subMatch,
		MinEntropy:   minEntropy,
	}
}

// k8s builds a resource-name detector with three precision properties:
//
//  1. Only the noun (e.g. `nodes?`) is case-insensitive; the resource-name
//     body is restricted to RFC 1123 DNS-subdomain lowercase. Avoids the
//     false positive on tabular column headers like `NODE   NOMINATED` where
//     the previous wholly-case-insensitive pattern would treat `NOMINATED`
//     as a node name.
//
//  2. The prefix anchor excludes `-` and other word characters so the noun
//     cannot match as a hyphen-suffix of a larger identifier. Closes a
//     false positive where a resource named `nginx-ingress` followed by a
//     class token (e.g. `nginx-ingress nginx`) would partial-match starting
//     at the embedded `ingress`, producing visually confusing output like
//     `nginx-[[opsmask:k8singress:…]]`.
//
//  3. Only the resource-name body is captured (SubMatch 1), so the noun and
//     surrounding separator/quote are preserved in the masked output:
//     `configmap "<name>" not found` becomes
//     `configmap "[[opsmask:k8sconfigmap:…]]" not found`, not
//     `[[opsmask:k8sconfigmap:…]]" not found`. The noun is not sensitive,
//     and keeping it visible preserves the error's diagnostic context.
//
// `\n` is naturally covered by `[^A-Za-z0-9_-]`, so the pattern works at the
// start of any line in a multi-line chunk without needing `(?m)`. The
// inter-token separator is `[ \t]+` (not `\s+`) so a YAML newline cannot
// bridge `kind: Secret` to the next key. The trailing assertion
// `(?:[^.A-Za-z0-9_-]|$)` rejects names followed by `.` so dotted hostnames
// inside paths (e.g. `kubernetes.io`) do not capture.
func k8s(name, typ, nouns string, keywords ...string) Spec {
	return Spec{
		Name:         name,
		Type:         typ,
		Pattern:      `(?:^|[^A-Za-z0-9_-])(?i:` + nouns + `)(?:/|[ \t]+(?:named[ \t]+)?["']?)([a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?)(?:[^.A-Za-z0-9_-]|$)`,
		Policy:       policy.Pseudonymize,
		Keywords:     keywords,
		MaxMatchSpan: 160,
		SubMatch:     1,
	}
}
