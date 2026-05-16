package cli

import (
	"fmt"

	"github.com/ching-kuo/opsmask/internal/install"
	"github.com/spf13/cobra"
)

func newUninstall(opts *Options) *cobra.Command {
	cmd := &cobra.Command{Use: "uninstall", Short: "Uninstall host integrations"}
	cmd.AddCommand(newUninstallClaudeCode(opts))
	return cmd
}

func newUninstallClaudeCode(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "claude-code",
		Short: "Uninstall the Claude Code Bash PreToolUse hook",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := install.UninstallClaudeCode("")
			if err != nil {
				return CodeError{Code: 125, Err: err}
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "uninstalled Claude Code hook from %s\n", res.SettingsPath)
			return nil
		},
	}
	_ = opts
	return cmd
}
