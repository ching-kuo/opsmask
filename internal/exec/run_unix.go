//go:build !windows

package exec

import (
	"os/exec"
	"syscall"
)

func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func signalGroup(pid int, sig syscall.Signal) {
	_ = syscall.Kill(-pid, sig)
}
