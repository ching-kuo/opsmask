package cli

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ching-kuo/llm-mask/internal/config"
	maskexec "github.com/ching-kuo/llm-mask/internal/exec"
	"github.com/spf13/cobra"
)

func newExec(opts *Options) *cobra.Command {
	var timeout string
	cmd := &cobra.Command{
		Use:   "exec [--timeout duration] -- <command> [args...]",
		Short: "Resolve llm-mask sentinels into a read-only follow-up command and re-mask output",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return userErr("exec requires a command after --")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := buildRuntime(opts)
			if err != nil {
				return CodeError{Code: 125, Err: err}
			}
			defer rt.Close()
			// Preflight the audit log before doing anything else so we never run a child
			// process that we cannot record. Without an audit trail the safety contract
			// of `exec` is broken; fail closed instead.
			if err := maskexec.Preflight(); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "exec refused: audit log unwritable: %v\n", err)
				return CodeError{Code: 125}
			}
			cfg := rt.loaded.ProjectExec
			cwd, _ := os.Getwd()
			rec := maskexec.Record{
				Ts:         time.Now(),
				Cwd:        cwd,
				Argv:       append([]string(nil), args...),
				Scope:      string(cfg.Scope),
				Executable: args[0],
			}
			defer func() {
				if werr := maskexec.WriteRecord(rec); werr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: audit log write failed: %v\n", werr)
				}
			}()

			fail := func(code int, class string) error {
				rec.ExitCode = code
				rec.ErrorClass = class
				return CodeError{Code: code}
			}
			if rt.loaded.Untrusted {
				fmt.Fprintln(cmd.ErrOrStderr(), "exec disabled: project .llm-mask/config.yaml is untrusted; run `llm-mask config trust`")
				return fail(125, "untrusted")
			}
			if !cfg.Enabled {
				fmt.Fprintln(cmd.ErrOrStderr(), "exec disabled in this project (set exec.enabled: true in trusted .llm-mask/config.yaml)")
				return fail(125, "disabled")
			}
			d := cfg.DefaultTimeout
			if timeout != "" {
				d, err = time.ParseDuration(timeout)
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "invalid --timeout %q: %v\n", timeout, err)
					return fail(125, "wrapper")
				}
			}
			resolved, err := maskexec.Resolve(cmd.Context(), args, func(typ, idx string) ([]byte, bool, error) {
				return rt.store.Lookup(cmd.Context(), typ, idx)
			})
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "resolution failed")
				rec.DenyMatch = err.Error()
				return fail(125, "resolve")
			}
			decision := maskexec.EvaluatePolicy(resolved, cfg)
			rec.AllowMatch = decision.AllowMatch
			rec.DenyMatch = decision.DenyMatch
			if !decision.Allowed {
				fmt.Fprintf(cmd.ErrOrStderr(), "exec rejected: %s\n", decision.Reason)
				return fail(125, decision.ErrorClass)
			}
			env := maskexec.BuildEnv(cfg.Scope, cfg, nil)
			rec.EnvAllowCount = len(env.Env)
			rec.EnvDenyCount = env.DenyCount
			rec.DenyOptOut = cfg.DenyOptOut
			if cfg.Scope == config.ScopeFreeform {
				fmt.Fprintf(cmd.ErrOrStderr(), "llm-mask exec: scope=freeform; allow-list=%d entries; deny-opt-outs=%d\n", len(cfg.Allow), len(cfg.DenyOptOut))
			}
			runCtx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			run := maskexec.Run(runCtx, resolved, maskexec.RunOptions{
				Env: env.Env, Stdin: os.Stdin, Stdout: cmd.OutOrStdout(), Stderr: cmd.ErrOrStderr(),
				Timeout: d, Rules: rt.rules, Alloc: rt.alloc,
			})
			rec.ExitCode = run.ExitCode
			rec.DurationMs = run.Duration.Milliseconds()
			rec.ErrorClass = run.ErrorClass
			if run.ExitCode != 0 {
				return CodeError{Code: run.ExitCode}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&timeout, "timeout", "", "maximum child runtime (for example 30s, 2m)")
	return cmd
}
