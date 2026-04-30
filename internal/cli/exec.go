package cli

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ching-kuo/opsmask/internal/config"
	maskexec "github.com/ching-kuo/opsmask/internal/exec"
	"github.com/spf13/cobra"
)

func newExec(opts *Options) *cobra.Command {
	var timeout string
	cmd := &cobra.Command{
		Use:   "exec [--timeout duration] -- <command> [args...]",
		Short: "Resolve opsmask sentinels into a read-only follow-up command and re-mask output",
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
			cwd, _ := os.Getwd()
			cfg := rt.Loaded.ProjectExec

			if cfg.Scope == config.ScopeFreeform {
				fmt.Fprintf(cmd.ErrOrStderr(), "opsmask exec: scope=freeform; allow-list=%d entries; deny-opt-outs=%d\n", len(cfg.Allow), len(cfg.DenyOptOut))
			}

			runCtx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			runtime := maskexec.Runtime{
				Store:     rt.Store,
				Alloc:     rt.Alloc,
				Rules:     rt.Rules,
				Untrusted: rt.Loaded.Untrusted,
				Cfg:       cfg,
			}
			result, err := maskexec.Orchestrate(runCtx, runtime, args, maskexec.OrchestrateOptions{
				Source:  maskexec.SourceCLI,
				Cwd:     cwd,
				Timeout: timeout,
				// CLI does not refuse scope=freeform+empty-allow; only the MCP
				// path applies that tightening. Local users who wrote a trusted
				// freeform config knew what they were doing.
				RefuseScopeOpen: false,
				Stdin:           os.Stdin,
				Stdout:          cmd.OutOrStdout(),
				Stderr:          cmd.ErrOrStderr(),
			})
			if err != nil {
				printOrchestrateError(cmd, err)
				if errors.Is(err, maskexec.ErrAuditUnwritable) {
					return CodeError{Code: 125}
				}
				return CodeError{Code: 125}
			}
			if result.ExitCode != 0 {
				return CodeError{Code: result.ExitCode}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&timeout, "timeout", "", "maximum child runtime (for example 30s, 2m)")
	return cmd
}

func printOrchestrateError(cmd *cobra.Command, err error) {
	stderr := cmd.ErrOrStderr()
	switch {
	case errors.Is(err, maskexec.ErrAuditUnwritable):
		fmt.Fprintf(stderr, "exec refused: audit log unwritable: %v\n", err)
	case errors.Is(err, maskexec.ErrUntrusted):
		fmt.Fprintln(stderr, "exec disabled: project .opsmask/config.yaml is untrusted; run `opsmask config trust`")
	case errors.Is(err, maskexec.ErrDisabled):
		fmt.Fprintln(stderr, "exec disabled in this project (set exec.enabled: true in trusted .opsmask/config.yaml)")
	case errors.Is(err, maskexec.ErrTimeoutParse):
		fmt.Fprintf(stderr, "%v\n", err)
	case errors.Is(err, maskexec.ErrResolve):
		fmt.Fprintln(stderr, "resolution failed")
	case errors.Is(err, maskexec.ErrPolicyDenied):
		fmt.Fprintf(stderr, "exec rejected: %v\n", err)
	case errors.Is(err, maskexec.ErrScopeOpen):
		fmt.Fprintln(stderr, "exec refused: scope=freeform with empty allow-list (MCP)")
	default:
		fmt.Fprintf(stderr, "exec error: %v\n", err)
	}
}
