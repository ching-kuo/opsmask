//go:build linux

package config

import (
	"fmt"
	"syscall"
)

func rejectNetworkFS(path string) error {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return err
	}
	switch uint64(st.Type) {
	case 0x6969, 0x517B, 0x65735546:
		return fmt.Errorf("%s is on unsupported network/sync filesystem", path)
	default:
		return nil
	}
}
