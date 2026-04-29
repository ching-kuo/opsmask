//go:build windows

package store

import "os"

func platformFileOwnerOK(path string, info os.FileInfo) error {
	return nil
}
