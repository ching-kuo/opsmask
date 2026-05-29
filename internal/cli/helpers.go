package cli

import (
	"github.com/ching-kuo/opsmask/internal/runtime"
)

// runtimeEnv is the CLI-local alias for the shared runtime.Env. Sibling CLI
// files build their runtime via the buildRuntime helper, which forwards to
// internal/runtime so the MCP server can construct the same graph.
type runtimeEnv = runtime.Env

func buildRuntime(opts *Options) (*runtimeEnv, error) {
	return runtime.New(runtime.Options{
		Mapping: opts.Mapping,
		Config:  opts.Config,
	})
}
