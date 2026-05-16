package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

type Options struct {
	Config  string
	Mapping string
	Verbose bool
}

type UsageError struct{ Err error }

func (e UsageError) Error() string { return e.Err.Error() }
func (e UsageError) Unwrap() error { return e.Err }

type CodeError struct {
	Code int
	Err  error
}

func (e CodeError) Error() string {
	if e.Err == nil {
		return ""
	}
	return e.Err.Error()
}
func (e CodeError) Unwrap() error { return e.Err }

func ExitCode(err error) int {
	var c CodeError
	if errors.As(err, &c) {
		return c.Code
	}
	var u UsageError
	if errors.As(err, &u) {
		return 2
	}
	return 1
}

func NewRoot(version string) *cobra.Command {
	opts := &Options{}
	root := &cobra.Command{
		Use:           "opsmask",
		Short:         "Mask log text before sending it to an LLM",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&opts.Config, "config", "", "config path override")
	root.PersistentFlags().StringVar(&opts.Mapping, "mapping", "", "mapping SQLite path override")
	root.PersistentFlags().BoolVar(&opts.Verbose, "verbose", false, "verbose diagnostics")
	root.AddCommand(newMask(opts), newUnmask(opts), newExec(opts), newInit(), newMapping(opts), newConfig(), newMcp(opts), newCorpus(), newInstall(opts), newUninstall(opts), newClaudeCodeHook(opts), newClaudeCodeExec(opts))
	return root
}

func RewriteArgs(args []string) []string {
	known := map[string]bool{"mask": true, "unmask": true, "exec": true, "init": true, "mapping": true, "config": true, "mcp": true, "corpus": true, "install": true, "uninstall": true, "claude-code-hook": true, "claude-code-exec": true, "completion": true, "help": true}
	if len(args) == 0 {
		return []string{"mask"}
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if known[a] || a == "-h" || a == "--help" || a == "--version" {
			return args
		}
		if a == "--config" || a == "--mapping" {
			i++
			continue
		}
		if strings.HasPrefix(a, "--config=") || strings.HasPrefix(a, "--mapping=") || a == "--verbose" {
			continue
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			continue
		}
		return append([]string{"mask"}, args...)
	}
	return append([]string{"mask"}, args...)
}

func openInput(args []string) (*os.File, func(), error) {
	if len(args) == 0 || args[0] == "-" {
		return os.Stdin, func() {}, nil
	}
	f, err := os.Open(args[0])
	if err != nil {
		return nil, nil, err
	}
	return f, func() { _ = f.Close() }, nil
}

func userErr(format string, args ...any) error {
	return UsageError{Err: fmt.Errorf(format, args...)}
}
