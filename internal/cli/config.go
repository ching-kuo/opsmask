package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ching-kuo/llm-mask/internal/config"
	"github.com/spf13/cobra"
)

func newConfig() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage trusted config",
		RunE: func(cmd *cobra.Command, args []string) error {
			path := filepath.Join(".llm-mask", "config.yaml")
			if _, err := os.Stat(path); err != nil {
				if !os.IsNotExist(err) {
					return err
				}
				return promptInitConfig(cmd)
			}
			return printConfigStatus(cmd, path)
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "init",
		Short: "Initialize .llm-mask/config.yaml in the current directory",
		RunE: func(cmd *cobra.Command, args []string) error {
			created, err := initProjectFiles()
			if err != nil {
				return err
			}
			printInitSummary(cmd, created)
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "trust",
		Short: "Trust the current project .llm-mask/config.yaml",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := filepath.Abs(filepath.Join(".llm-mask", "config.yaml"))
			if err != nil {
				return err
			}
			created, err := initProjectFiles()
			if err != nil {
				return err
			}
			if len(created) > 0 {
				printInitSummary(cmd, created)
			}
			if err := config.Trust(path); err != nil {
				return err
			}
			lit, re, deny, err := config.SummarizeFile(path)
			if err != nil {
				return err
			}
			real, sum, _ := config.HashFile(path)
			fmt.Fprintf(cmd.ErrOrStderr(), "trusted %s sha256=%s rules: literals=%d regex_rules=%d deny_list=%d caps: rules<=%d regex_bytes<=%d groups<=%d match_span<=%d matches_per_chunk<=%d\n",
				real, sum, lit, re, deny, config.MaxConfigRules, config.MaxRegexPatternSize, config.MaxRegexGroups, config.MaxMatchSpan, config.MaxMatchesPerChunk)
			return nil
		},
	})
	return cmd
}

func promptInitConfig(cmd *cobra.Command) error {
	fmt.Fprint(cmd.ErrOrStderr(), "No .llm-mask/config.yaml found. Initialize now? [y/N]: ")
	answer, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil && err != io.EOF {
		return err
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer != "y" && answer != "yes" {
		fmt.Fprintln(cmd.ErrOrStderr(), "not initialized; run `llm-mask config init` or `llm-mask init` when ready")
		return nil
	}
	created, err := initProjectFiles()
	if err != nil {
		return err
	}
	printInitSummary(cmd, created)
	fmt.Fprintln(cmd.ErrOrStderr(), "edit .llm-mask/config.yaml, then run `llm-mask config trust` to enable it")
	return nil
}

func printConfigStatus(cmd *cobra.Command, path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	lit, re, deny, err := config.SummarizeFile(abs)
	if err != nil {
		return err
	}
	ok, err := config.IsTrusted(abs)
	if err != nil {
		return err
	}
	trust := "untrusted"
	if ok {
		trust = "trusted"
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "%s is %s; rules: literals=%d regex_rules=%d deny_list=%d\n", abs, trust, lit, re, deny)
	if !ok {
		fmt.Fprintln(cmd.ErrOrStderr(), "run `llm-mask config trust` to enable this project config")
	}
	return nil
}
