package cli

import (
	"os"

	"github.com/ching-kuo/opsmask/internal/cchook"
	"github.com/spf13/cobra"
)

func newClaudeCodeHook(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:    "claude-code-hook",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cchook.Handle(os.Stdin, cmd.OutOrStdout(), cchook.HandlerEnv{})
		},
	}
	_ = opts
	return cmd
}
