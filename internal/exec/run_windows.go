//go:build windows

package exec

import (
	"os/exec"
	"syscall"
)

func setProcessGroup(cmd *exec.Cmd)           {}
func signalGroup(pid int, sig syscall.Signal) {}
