// Package runtime constructs the shared dependency graph (mapping store,
// pseudonym allocator, detector rules, loaded config) used by both the CLI
// commands and the MCP server. It exists so external callers can build the
// same runtime without re-implementing the wiring in internal/cli.
package runtime

import (
	"fmt"
	"io"
	"os"

	"github.com/ching-kuo/opsmask/internal/config"
	"github.com/ching-kuo/opsmask/internal/detect"
	"github.com/ching-kuo/opsmask/internal/pseudo"
	"github.com/ching-kuo/opsmask/internal/store"
)

// Options configures runtime construction. Empty values fall back to
// defaults: the canonical mapping path under the user's config dir, no
// explicit --config override, and stderr for warnings.
type Options struct {
	Mapping string
	Config  string
	Warn    io.Writer
}

// Env is the shared dependency graph. Fields are exported so external
// packages can read them without accessor boilerplate.
type Env struct {
	Store  store.Store
	Alloc  *pseudo.Allocator
	Rules  []detect.Rule
	Loaded config.Loaded
}

// New opens the mapping store, loads project config, and assembles the
// runtime. The caller owns the returned Env and must call Close.
func New(opts Options) (*Env, error) {
	warn := opts.Warn
	if warn == nil {
		warn = os.Stderr
	}
	path, err := store.ResolveMapping("", opts.Mapping)
	if err != nil {
		return nil, fmt.Errorf("resolve mapping path: %w", err)
	}
	st, err := store.OpenSQLite(path)
	if err != nil {
		return nil, fmt.Errorf("open mapping store: %w", err)
	}
	secret, err := config.EnsureSecret()
	if err != nil {
		_ = st.Close()
		return nil, fmt.Errorf("ensure secret: %w", err)
	}
	builtins, err := detect.BuiltinRules()
	if err != nil {
		_ = st.Close()
		return nil, fmt.Errorf("load builtin rules: %w", err)
	}
	loaded, err := config.Load("", func(s string) { fmt.Fprintln(warn, s) }, true)
	if err != nil {
		_ = st.Close()
		return nil, fmt.Errorf("load project config: %w", err)
	}
	if opts.Config != "" {
		explicit, err := config.LoadFile(opts.Config)
		if err != nil {
			_ = st.Close()
			return nil, fmt.Errorf("load --config %s: %w", opts.Config, err)
		}
		loaded.Rules = append(loaded.Rules, explicit.Rules...)
		loaded.DenyList = append(loaded.DenyList, explicit.DenyList...)
		// Trust is anchored to the project's .opsmask/config.yaml (see config.IsTrusted).
		// An arbitrary --config file cannot satisfy that gate, so its exec block must
		// not enable command execution. Warn loudly and ignore the exec settings.
		if explicit.ProjectExec.Enabled {
			fmt.Fprintf(warn, "warning: exec settings in --config %s are ignored; exec must be enabled via trusted .opsmask/config.yaml\n", opts.Config)
		}
	}
	rules := append(builtins, loaded.Rules...)
	return &Env{Store: st, Alloc: pseudo.New(secret, st), Rules: rules, Loaded: loaded}, nil
}

// Close releases the mapping store. Safe to call on a nil receiver.
func (e *Env) Close() error {
	if e == nil || e.Store == nil {
		return nil
	}
	return e.Store.Close()
}
