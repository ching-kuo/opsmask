package exec

import (
	"os"
	"strings"

	"github.com/ching-kuo/opsmask/internal/config"
)

type EnvResult struct {
	Env       []string
	DenyCount int
}

func BuildEnv(scope config.ExecScope, cfg config.ExecConfig, environ []string) EnvResult {
	if environ == nil {
		environ = os.Environ()
	}
	allow := baselineEnvAllow(scope)
	for _, v := range cfg.EnvAllow {
		allow[v] = true
	}
	for _, v := range cfg.EnvDeny {
		delete(allow, v)
	}
	var out []string
	denyCount := 0
	for _, kv := range environ {
		name, _, ok := strings.Cut(kv, "=")
		if !ok || name == "" {
			continue
		}
		if hardDenyEnv(name) {
			denyCount++
			continue
		}
		if allow[name] || strings.HasPrefix(name, "LC_") {
			out = append(out, kv)
			continue
		}
		denyCount++
	}
	return EnvResult{Env: out, DenyCount: denyCount}
}

func baselineEnvAllow(scope config.ExecScope) map[string]bool {
	names := []string{"PATH", "HOME", "USER", "LOGNAME", "TMPDIR", "LANG", "TERM", "KUBECONFIG", "AWS_PROFILE", "AWS_REGION", "AWS_DEFAULT_REGION", "AWS_CONFIG_FILE", "AWS_SHARED_CREDENTIALS_FILE", "GOOGLE_APPLICATION_CREDENTIALS", "CLOUDSDK_CONFIG", "CLOUDSDK_CORE_PROJECT", "GOOGLE_CLOUD_PROJECT", "AZURE_CONFIG_DIR", "AZURE_TENANT_ID", "AZURE_SUBSCRIPTION_ID", "HELM_CACHE_HOME", "HELM_CONFIG_HOME", "HELM_DATA_HOME", "HELM_NAMESPACE", "HELM_KUBECONTEXT", "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy"}
	if scope == config.ScopeInvestigate || scope == config.ScopeFreeform {
		names = append(names, "SSH_AUTH_SOCK", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "DOCKER_HOST", "DOCKER_CONFIG", "DOCKER_CERT_PATH")
	}
	out := make(map[string]bool, len(names))
	for _, n := range names {
		out[n] = true
	}
	return out
}

func hardDenyEnv(name string) bool {
	if strings.HasPrefix(name, "BASH_FUNC_") || strings.HasPrefix(name, "GIT_CONFIG_") {
		return true
	}
	switch name {
	case "BASH_ENV", "ENV", "ZDOTDIR", "SHELLOPTS", "BASHOPTS", "FPATH",
		"LD_PRELOAD", "LD_LIBRARY_PATH", "DYLD_INSERT_LIBRARIES", "DYLD_LIBRARY_PATH", "DYLD_FRAMEWORK_PATH", "DYLD_FALLBACK_LIBRARY_PATH", "DYLD_FALLBACK_FRAMEWORK_PATH",
		"PYTHONPATH", "PYTHONSTARTUP", "PYTHONHOME", "NODE_PATH", "NODE_OPTIONS", "RUBYOPT", "RUBYLIB", "PERL5OPT", "PERL5LIB",
		"JAVA_TOOL_OPTIONS", "_JAVA_OPTIONS", "JDK_JAVA_OPTIONS", "GIT_SSH_COMMAND", "GIT_SSH", "GIT_ASKPASS", "SSH_ASKPASS", "GIT_EXTERNAL_DIFF", "GIT_PAGER",
		"KUBECTL_EXTERNAL_DIFF", "KUBE_EDITOR", "EDITOR", "VISUAL", "PAGER", "LESS", "LESSOPEN", "LESSCLOSE", "MANPAGER", "CURL_HOME", "NETRC", "WGETRC":
		return true
	default:
		return false
	}
}
