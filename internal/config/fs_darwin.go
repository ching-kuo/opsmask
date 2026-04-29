//go:build darwin

package config

import (
	"fmt"
	"strings"
	"syscall"
)

func rejectNetworkFS(path string) error {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return err
	}
	var b []byte
	for _, c := range st.Fstypename[:] {
		if c == 0 {
			break
		}
		b = append(b, byte(c))
	}
	fs := strings.ToLower(string(b))
	switch fs {
	case "nfs", "smbfs", "fusefs", "webdav":
		return fmt.Errorf("%s is on unsupported network/sync filesystem %s", path, fs)
	default:
		return nil
	}
}
