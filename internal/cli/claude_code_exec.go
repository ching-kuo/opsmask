package cli

import (
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/ching-kuo/opsmask/internal/cchook"
	maskexec "github.com/ching-kuo/opsmask/internal/exec"
	"github.com/spf13/cobra"
)

func newClaudeCodeExec(opts *Options) *cobra.Command {
	var sig, timeout string
	cmd := &cobra.Command{
		Use:    "claude-code-exec --sig <hex> -- <command>",
		Hidden: true,
		Args: func(cmd *cobra.Command, args []string) error {
			if sig == "" {
				return userErr("claude-code-exec requires --sig")
			}
			if len(args) == 0 {
				return userErr("claude-code-exec requires a command after --")
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
			runCtx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			runtime := maskexec.Runtime{
				Store:     rt.Store,
				Alloc:     rt.Alloc,
				Rules:     rt.Rules,
				Untrusted: rt.Loaded.Untrusted,
				Cfg:       rt.Loaded.ProjectExec,
			}
			result, err := cchook.RunWrapped(runCtx, runtime, sig, strings.Join(args, " "), cchook.WrappedOptions{
				Cwd:     cwd,
				Timeout: timeout,
				Stdin:   os.Stdin,
				Stdout:  cmd.OutOrStdout(),
				Stderr:  cmd.ErrOrStderr(),
			})
			if err != nil {
				printOrchestrateError(cmd, err)
				return CodeError{Code: 125}
			}
			if result.ExitCode != 0 {
				return CodeError{Code: result.ExitCode}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&sig, "sig", "", "hook signature")
	cmd.Flags().StringVar(&timeout, "timeout", "", "maximum child runtime")
	return cmd
}
