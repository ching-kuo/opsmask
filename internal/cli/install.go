package cli

import (
	"fmt"
	"os"

	"github.com/ching-kuo/opsmask/internal/cchook"
	"github.com/ching-kuo/opsmask/internal/install"
	"github.com/spf13/cobra"
)

func newInstall(opts *Options) *cobra.Command {
	cmd := &cobra.Command{Use: "install", Short: "Install host integrations"}
	cmd.AddCommand(newInstallClaudeCode(opts))
	return cmd
}

func newInstallClaudeCode(opts *Options) *cobra.Command {
	var teamShared, yes bool
	cmd := &cobra.Command{
		Use:   "claude-code",
		Short: "Install the Claude Code Bash PreToolUse hook",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := buildRuntime(opts)
			if err != nil {
				return CodeError{Code: 125, Err: fmt.Errorf("runtime init failed; run `opsmask init` in this project first if needed: %w", err)}
			}
			_ = rt.Close()
			if err := cchook.EnsureSecret(); err != nil {
				return CodeError{Code: 125, Err: err}
			}
			mode := install.ModePersonal
			if teamShared {
				mode = install.ModeTeamShared
				if !yes {
					fmt.Fprintln(cmd.ErrOrStderr(), "Team-shared Claude Code hooks fail closed for teammates or CI runners without OpsMask installed. Press Enter to continue or Ctrl-C to abort.")
					var one [1]byte
					_, _ = os.Stdin.Read(one[:])
				}
			}
			res, err := install.InstallClaudeCode("", "", mode)
			if err != nil {
				return CodeError{Code: 125, Err: err}
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "installed Claude Code hook at %s\n", res.SettingsPath)
			return nil
		},
	}
	cmd.Flags().BoolVar(&teamShared, "team-shared", false, "write committable .claude/settings.json instead of local settings")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation prompts")
	return cmd
}
