package exec

import (
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/ching-kuo/llm-mask/internal/config"
	"github.com/ching-kuo/llm-mask/internal/exec/denybase"
)

var (
	baselineOnce     sync.Once
	baselineReadOnly []config.AllowEntry
	baselineInvestg  []config.AllowEntry
	jqFilterPrefixRe = regexp.MustCompile(`^([.@$\[]|\(|if\b|def\b|select\b|map\b|with_entries\b|del\b)`)
	// sedExecRes matches GNU sed's substitution-execute flag (`s<delim>...<delim>...<delim>[flags]e`)
	// for the most common delimiters. RE2 lacks backreferences, so we enumerate
	// the delimiter characters explicitly. Each entry permits backslash-escapes
	// and any flag chars before the trailing `e`.
	sedExecRes = []*regexp.Regexp{
		regexp.MustCompile(`(?:^|[\s;])s/(?:\\.|[^/\\])*/(?:\\.|[^/\\])*/[gIpmMd0-9 ]*e`),
		regexp.MustCompile(`(?:^|[\s;])s\|(?:\\.|[^|\\])*\|(?:\\.|[^|\\])*\|[gIpmMd0-9 ]*e`),
		regexp.MustCompile(`(?:^|[\s;])s#(?:\\.|[^#\\])*#(?:\\.|[^#\\])*#[gIpmMd0-9 ]*e`),
		regexp.MustCompile(`(?:^|[\s;])s,(?:\\.|[^,\\])*,(?:\\.|[^,\\])*,[gIpmMd0-9 ]*e`),
	}
)

type PolicyDecision struct {
	Allowed    bool
	AllowMatch string
	ErrorClass string
	DenyMatch  string
	Reason     string
}

func EvaluatePolicy(argv []string, cfg config.ExecConfig) PolicyDecision {
	if len(argv) == 0 {
		return PolicyDecision{ErrorClass: "not_in_allow_list", Reason: "empty argv"}
	}
	if d := denyMatch(argv, cfg); d != "" {
		layer := "deny_layer_a"
		if strings.HasPrefix(d, "layer_b:") {
			layer = "deny_layer_b"
			d = strings.TrimPrefix(d, "layer_b:")
		} else if strings.HasPrefix(d, "layer_c:") {
			layer = "deny_layer_c"
			d = strings.TrimPrefix(d, "layer_c:")
		}
		return PolicyDecision{ErrorClass: layer, DenyMatch: d, Reason: "command denied by hard safety policy"}
	}
	base := BaselineAllow(cfg.Scope)
	entries := make([]config.AllowEntry, 0, len(base)+len(cfg.Allow))
	entries = append(entries, base...)
	entries = append(entries, cfg.Allow...)
	if cfg.Scope == config.ScopeFreeform && len(entries) == 0 {
		return PolicyDecision{Allowed: true}
	}
	for _, ent := range entries {
		if matchAllow(ent, argv) {
			name := ent.Name
			if name == "" {
				name = "project"
			}
			return PolicyDecision{Allowed: true, AllowMatch: name}
		}
	}
	return PolicyDecision{ErrorClass: "not_in_allow_list", Reason: "command is not permitted by exec scope/allow-list"}
}

func BaselineAllow(scope config.ExecScope) []config.AllowEntry {
	baselineOnce.Do(func() {
		baselineReadOnly = []config.AllowEntry{
			allow("baseline:kubectl-readonly", "^kubectl$", "^(get|describe|logs|events|top|version|api-resources|api-versions|explain|cluster-info|auth)$", "..."),
			allow("baseline:dig", "^dig$", "..."),
			allow("baseline:nslookup", "^nslookup$", "..."),
			allow("baseline:host", "^host$", "..."),
			allowFunc("baseline:jq-stdin", jqStdinOnly),
			allow("baseline:echo", "^echo$", "..."),
			allow("baseline:date", "^date$", "..."),
			allow("baseline:env", "^env$"),
		}
		baselineInvestg = append([]config.AllowEntry{}, baselineReadOnly...)
		baselineInvestg = append(baselineInvestg,
			allow("baseline:kubectl-investigate", "^kubectl$", "^(get|describe|logs|events|top|version|api-resources|api-versions|explain|cluster-info|auth|rollout|config|diff)$", "..."),
			allow("baseline:helm-read", "^helm$", "^(get|list|history|show|status|env|version|template)$", "..."),
			allow("baseline:aws-read", "^aws$", "^(s3|ec2|iam|sts|logs|cloudwatch|describe.*|get.*|list.*)$", "..."),
			allow("baseline:gcloud-read", "^gcloud$", "^.*$", "^(describe|list|get-.*|export)$", "..."),
			allow("baseline:az-read", "^az$", "^.*$", "^(show|list|export)$", "..."),
			allow("baseline:docker-read", "^docker$", "^(ps|images|inspect|logs|history|version|info|top|stats|events)$", "..."),
			allow("baseline:git-read", "^git$", "^(log|show|diff|status|branch|ls-files|ls-tree|cat-file|rev-parse)$", "..."),
			allow("baseline:network-read", "^(ping|traceroute|mtr|netstat|ss|lsof)$", "..."),
			allow("baseline:file-readers", "^(cat|head|tail|less|more|grep|awk|sed)$", "..."),
		)
	})
	switch scope {
	case config.ScopeInvestigate:
		return baselineInvestg
	case config.ScopeFreeform:
		return nil
	default:
		return baselineReadOnly
	}
}

func allow(name string, pats ...string) config.AllowEntry {
	ent := config.AllowEntry{Name: name}
	if len(pats) > 0 && (pats[len(pats)-1] == "..." || pats[len(pats)-1] == "…") {
		ent.AnyTail = true
		pats = pats[:len(pats)-1]
	}
	for _, p := range pats {
		ent.Elements = append(ent.Elements, regexp.MustCompile(p))
	}
	return ent
}

func allowFunc(name string, fn func([]string) bool) config.AllowEntry {
	return config.AllowEntry{Name: name, MatchFunc: fn}
}

func matchAllow(ent config.AllowEntry, argv []string) bool {
	if ent.MatchFunc != nil {
		return ent.MatchFunc(argv)
	}
	if len(argv) < len(ent.Elements) {
		return false
	}
	if !ent.AnyTail && len(argv) != len(ent.Elements) {
		return false
	}
	for i, re := range ent.Elements {
		if !re.MatchString(argv[i]) || re.FindString(argv[i]) != argv[i] {
			return false
		}
	}
	return true
}

func jqStdinOnly(argv []string) bool {
	if len(argv) < 2 || argv[0] != "jq" {
		return false
	}
	positionals := 0
	for i := 1; i < len(argv); i++ {
		a := argv[i]
		switch a {
		case "--null-input", "-n", "--slurp", "-s", "--raw-input", "-R", "--raw-output", "-r", "--compact-output", "-c", "--tab", "-t", "--sort-keys", "-S", "--ascii-output", "-a", "-e", "--exit-status", "-j", "--join-output":
			continue
		case "--arg", "--argjson":
			i += 2
			if i >= len(argv) {
				return false
			}
			continue
		default:
			positionals++
			if positionals > 1 {
				return false
			}
			if !jqFilterPrefixRe.MatchString(a) {
				return false
			}
		}
	}
	return positionals == 1
}

func denyMatch(argv []string, cfg config.ExecConfig) string {
	base := strings.ToLower(filepath.Base(argv[0]))
	if hardDeniedBase(base) && !optedOut(base, cfg) {
		return base
	}
	lower := make([]string, len(argv))
	lower[0] = base
	for i := 1; i < len(argv); i++ {
		lower[i] = strings.ToLower(argv[i])
	}
	if len(lower) >= 2 {
		switch lower[0] {
		case "kubectl":
			if in(lower[1], "exec", "cp", "port-forward", "debug", "attach", "run", "delete", "patch", "apply", "replace", "edit", "scale", "cordon", "drain", "uncordon", "annotate", "label", "create", "set", "expose") {
				return "layer_b:kubectl-" + lower[1]
			}
			if lower[1] == "auth" && len(lower) >= 3 && lower[2] == "reconcile" {
				return "layer_b:kubectl-auth-reconcile"
			}
			if lower[1] == "rollout" && len(lower) >= 3 && in(lower[2], "undo", "restart", "pause", "resume") {
				return "layer_b:kubectl-rollout-" + lower[2]
			}
			if (lower[1] == "get" || lower[1] == "describe") && len(lower) >= 3 {
				if mentionsKubectlSecret(lower[2:]) {
					return "layer_b:kubectl-get-secret"
				}
			}
			if lower[1] == "logs" {
				for _, a := range lower[2:] {
					if a == "--follow" || a == "-f" {
						return "layer_b:kubectl-logs-follow"
					}
				}
			}
		case "helm":
			if in(lower[1], "upgrade", "rollback", "uninstall", "install", "delete", "push") {
				return "layer_b:helm-" + lower[1]
			}
		case "aws":
			if len(lower) >= 3 && lower[1] == "s3" && in(lower[2], "cp", "mv", "rm", "sync") {
				return "layer_b:aws-s3-" + lower[2]
			}
			for _, a := range lower[1:] {
				if strings.HasPrefix(a, "delete-") || strings.HasPrefix(a, "create-") || strings.HasPrefix(a, "put-") || strings.HasPrefix(a, "update-") || strings.HasPrefix(a, "attach-") || strings.HasPrefix(a, "detach-") {
					return "layer_b:aws-mutation"
				}
			}
		case "gcloud", "az":
			for _, a := range lower[1:] {
				if in(a, "delete", "create", "update", "start", "stop") || strings.HasPrefix(a, "set-") || strings.HasPrefix(a, "add-") || strings.HasPrefix(a, "remove-") {
					return "layer_b:" + lower[0] + "-mutation"
				}
			}
		case "git":
			for i := 1; i < len(argv); i++ {
				la := strings.ToLower(argv[i])
				if strings.HasPrefix(la, "--exec-path=") {
					return "layer_b:git-exec-path"
				}
				if la == "-c" && i+1 < len(argv) {
					v := strings.ToLower(argv[i+1])
					if strings.HasPrefix(v, "alias.") && strings.Contains(v, "!") {
						return "layer_b:git-alias-shell"
					}
					if strings.HasPrefix(v, "core.sshcommand=") {
						return "layer_b:git-ssh-command"
					}
				}
			}
		case "docker":
			if in(lower[1], "run", "exec", "cp", "rm", "rmi", "kill", "stop", "restart") {
				return "layer_b:docker-" + lower[1]
			}
		case "env":
			if len(lower) > 1 {
				return "layer_b:env-dispatch"
			}
		case "find":
			for _, a := range lower[1:] {
				if in(a, "-exec", "-delete", "-fprintf") {
					return "layer_b:find-action"
				}
			}
		case "xargs":
			for _, a := range lower[1:] {
				if strings.HasPrefix(a, "-i") || strings.HasPrefix(a, "--replace") {
					return "layer_b:xargs-replace"
				}
				if !strings.HasPrefix(a, "-") {
					return "layer_b:xargs-command"
				}
			}
		case "awk":
			if strings.Contains(strings.Join(argv[1:], " "), "system(") {
				return "layer_b:awk-system"
			}
		case "sed":
			joined := strings.Join(argv[1:], " ")
			if strings.Contains(joined, "--eval-stdin") {
				return "layer_b:sed-exec"
			}
			for _, re := range sedExecRes {
				if re.MatchString(joined) {
					return "layer_b:sed-exec"
				}
			}
		}
	}
	isOutboundHTTP := base == "curl" || base == "wget"
	for _, a := range argv {
		if strings.HasPrefix(a, "--exec-path=") || strings.HasPrefix(a, "--exec=") || strings.HasPrefix(a, "--rsh=") || strings.HasPrefix(a, "--rcp=") ||
			strings.HasPrefix(a, "--checkpoint-action=exec=") || strings.HasPrefix(a, "--rmt-command=") || strings.HasPrefix(a, "--to-command=") ||
			strings.HasPrefix(a, "--use-compress-program=") || strings.HasPrefix(a, "--shell=") || strings.HasPrefix(a, "--shell-cmd=") {
			return "layer_c:dispatch-flag"
		}
		if isOutboundHTTP {
			if strings.HasPrefix(a, "--data") || a == "-d" || strings.HasPrefix(a, "--form") || a == "-F" ||
				strings.HasPrefix(a, "--upload-file") || a == "-T" ||
				a == "--config" || a == "-K" || a == "--netrc" || a == "-G" || a == "-o" ||
				strings.HasPrefix(a, "--output") || strings.HasPrefix(a, "--resolve") || strings.HasPrefix(a, "--connect-to") {
				return "layer_c:curl-dangerous-flag"
			}
		}
	}
	return ""
}

func hardDeniedBase(base string) bool {
	return denybase.Contains(base)
}

func optedOut(name string, cfg config.ExecConfig) bool {
	if cfg.Scope != config.ScopeFreeform || !cfg.AllowDenyOptOut {
		return false
	}
	for _, ent := range cfg.DenyOptOut {
		if ent.Name == name {
			return true
		}
	}
	return false
}

// Handles comma-separated resource lists (`pod,secret`) and `secret/foo`,
// `secrets.v1` qualified forms. Flag tokens are skipped so callers may pass
// the full tail after the verb without prefiltering.
func mentionsKubectlSecret(tokens []string) bool {
	for _, t := range tokens {
		if strings.HasPrefix(t, "-") {
			continue
		}
		for _, part := range strings.Split(t, ",") {
			if slash := strings.IndexByte(part, '/'); slash >= 0 {
				part = part[:slash]
			}
			if dot := strings.IndexByte(part, '.'); dot >= 0 {
				part = part[:dot]
			}
			if part == "secret" || part == "secrets" {
				return true
			}
		}
	}
	return false
}

func in(s string, xs ...string) bool {
	for _, x := range xs {
		if s == x {
			return true
		}
	}
	return false
}
