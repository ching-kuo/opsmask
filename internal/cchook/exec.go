package cchook

import (
	"context"
	"fmt"
	"io"
	"os"

	maskexec "github.com/ching-kuo/opsmask/internal/exec"
	"github.com/ching-kuo/opsmask/internal/install"
)

type WrappedOptions struct {
	Cwd     string
	Timeout string
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
}

func RunWrapped(ctx context.Context, rt maskexec.Runtime, sig, command string, opts WrappedOptions) (maskexec.HookResult, error) {
	cwd := opts.Cwd
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	top, err := install.ResolveProjectToplevel(cwd)
	if err != nil {
		return maskexec.HookResult{}, err
	}
	registered, err := install.IsRegistered(top)
	if err != nil {
		return maskexec.HookResult{}, err
	}
	if !registered {
		return maskexec.HookResult{}, fmt.Errorf("OpsMask hook fired in a project that was not opted in via `opsmask install claude-code`. Refusing")
	}
	secret, err := LoadSecret()
	if err != nil {
		return maskexec.HookResult{}, err
	}
	if !Verify(secret, top, command, sig) {
		return maskexec.HookResult{}, fmt.Errorf("invalid Claude Code hook signature")
	}
	return maskexec.OrchestrateHook(ctx, rt, command, maskexec.HookOptions{
		Cwd:     cwd,
		Timeout: opts.Timeout,
		Stdin:   opts.Stdin,
		Stdout:  opts.Stdout,
		Stderr:  opts.Stderr,
	})
}
