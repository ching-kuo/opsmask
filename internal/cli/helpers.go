package cli

import (
	"fmt"
	"os"

	"github.com/ching-kuo/opsmask/internal/config"
	"github.com/ching-kuo/opsmask/internal/detect"
	"github.com/ching-kuo/opsmask/internal/pseudo"
	"github.com/ching-kuo/opsmask/internal/store"
)

type runtimeEnv struct {
	store  store.Store
	alloc  *pseudo.Allocator
	rules  []detect.Rule
	loaded config.Loaded
}

func buildRuntime(opts *Options) (*runtimeEnv, error) {
	path, err := store.ResolveMapping("", opts.Mapping)
	if err != nil {
		return nil, err
	}
	st, err := store.OpenSQLite(path)
	if err != nil {
		return nil, err
	}
	secret, err := config.EnsureSecret()
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	builtins, err := detect.BuiltinRules()
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	loaded, err := config.Load("", func(s string) { fmt.Fprintln(os.Stderr, s) }, true)
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	if opts.Config != "" {
		explicit, err := config.LoadFile(opts.Config)
		if err != nil {
			_ = st.Close()
			return nil, err
		}
		loaded.Rules = append(loaded.Rules, explicit.Rules...)
		loaded.DenyList = append(loaded.DenyList, explicit.DenyList...)
		// Trust is anchored to the project's .opsmask/config.yaml (see config.IsTrusted).
		// An arbitrary --config file cannot satisfy that gate, so its exec block must
		// not enable command execution. Warn loudly and ignore the exec settings.
		if explicit.ProjectExec.Enabled {
			fmt.Fprintf(os.Stderr, "warning: exec settings in --config %s are ignored; exec must be enabled via trusted .opsmask/config.yaml\n", opts.Config)
		}
	}
	rules := append(builtins, loaded.Rules...)
	return &runtimeEnv{store: st, alloc: pseudo.New(secret, st), rules: rules, loaded: loaded}, nil
}

func (r *runtimeEnv) Close() {
	if r != nil && r.store != nil {
		_ = r.store.Close()
	}
}
