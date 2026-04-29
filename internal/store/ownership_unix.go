//go:build !windows

package store

import (
	"fmt"
	"os"
	"syscall"
)

func platformFileOwnerOK(path string, info os.FileInfo) error {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("cannot verify owner for %s", path)
	}
	if int(st.Uid) != os.Getuid() {
		return fmt.Errorf("%s must be owned by current uid", path)
	}
	return nil
}
