//go:build !windows

package exec

import (
	"os"
	"strconv"
	"syscall"
)

func CloseOnExecAll() {
	dir := "/proc/self/fd"
	if _, err := os.Stat(dir); err != nil {
		dir = "/dev/fd"
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, ent := range ents {
		fd, err := strconv.Atoi(ent.Name())
		if err != nil || fd < 3 {
			continue
		}
		syscall.CloseOnExec(fd)
	}
}
