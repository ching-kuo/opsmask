package main

import (
	"fmt"
	"os"

	"github.com/ching-kuo/opsmask/internal/cli"
)

var version = "dev"

func main() {
	args := cli.RewriteArgs(os.Args[1:])
	root := cli.NewRoot(version)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		if err.Error() != "" {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(cli.ExitCode(err))
	}
}
