package exec

import (
	"context"
	"fmt"
	"strings"

	"github.com/ching-kuo/llm-mask/internal/detect"
)

type LookupFunc func(typ, index string) ([]byte, bool, error)

type ResolveError struct {
	Tokens []string
	Reason string
}

func (e ResolveError) Error() string {
	if e.Reason != "" {
		return e.Reason
	}
	return "sentinel resolution failed: " + strings.Join(e.Tokens, ", ")
}

func Resolve(ctx context.Context, argv []string, lookup LookupFunc) ([]string, error) {
	if len(argv) == 0 {
		return nil, ResolveError{Reason: "empty argv after --"}
	}
	resolved := make([]string, len(argv))
	var failed []string
	var firstErr error
	for i, arg := range argv {
		out := detect.TokenRegexp().ReplaceAllFunc([]byte(arg), func(m []byte) []byte {
			if ctx.Err() != nil {
				firstErr = ctx.Err()
				failed = append(failed, string(m))
				return m
			}
			tok, ok := detect.ParseToken(m)
			if !ok {
				failed = append(failed, string(m))
				return m
			}
			real, found, err := lookup(tok.Type, tok.Index)
			if err != nil {
				firstErr = err
				failed = append(failed, string(m))
				return m
			}
			if !found || len(real) == 0 {
				failed = append(failed, string(m))
				return m
			}
			return real
		})
		resolved[i] = string(out)
	}
	if len(failed) > 0 {
		if firstErr != nil {
			return nil, ResolveError{Tokens: failed, Reason: fmt.Sprintf("sentinel resolution failed: %v", firstErr)}
		}
		return nil, ResolveError{Tokens: failed}
	}
	normalizeKubectlNamespaceArgs(resolved)
	return resolved, nil
}

func normalizeKubectlNamespaceArgs(argv []string) {
	if len(argv) < 3 || argv[0] != "kubectl" {
		return
	}
	for i := 1; i < len(argv); i++ {
		arg := argv[i]
		switch arg {
		case "-n", "--namespace":
			if i+1 < len(argv) {
				argv[i+1] = bareKubernetesNamespace(argv[i+1])
				i++
			}
		default:
			if strings.HasPrefix(arg, "--namespace=") {
				argv[i] = "--namespace=" + bareKubernetesNamespace(strings.TrimPrefix(arg, "--namespace="))
			}
		}
	}
}

func bareKubernetesNamespace(s string) string {
	for _, prefix := range []string{"namespace/", "namespaces/", "ns/"} {
		if strings.HasPrefix(s, prefix) {
			return strings.TrimPrefix(s, prefix)
		}
	}
	return s
}
