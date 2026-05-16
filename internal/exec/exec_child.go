package exec

import (
	"os"
	"strings"
)

const execChildEnv = "OPSMASK_EXEC_CHILD"

// IsExecChild reports whether the current process is inside a process tree
// spawned by opsmask exec. Presence matters; the value is informational only.
func IsExecChild() bool {
	_, ok := os.LookupEnv(execChildEnv)
	return ok
}

func injectExecChild(env []string) []string {
	prefix := execChildEnv + "="
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			continue
		}
		out = append(out, kv)
	}
	return append(out, prefix+"1")
}
