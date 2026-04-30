package store

import (
	"fmt"
	"os"
	"path/filepath"
)

func EnsurePrivateDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s must not be group/world accessible; run opsmask init in the intended project", dir)
	}
	if err := platformFileOwnerOK(dir, info); err != nil {
		return err
	}
	return nil
}

func ResolveMapping(cwd, explicit string) (string, error) {
	if explicit != "" {
		dir := filepath.Dir(explicit)
		if err := EnsurePrivateDir(dir); err != nil {
			return "", err
		}
		return explicit, nil
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	for {
		dir := filepath.Join(cwd, ".opsmask")
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			if err := EnsurePrivateDir(dir); err != nil {
				return "", err
			}
			return filepath.Join(dir, "mapping.sqlite"), nil
		}
		next := filepath.Dir(cwd)
		if next == cwd {
			break
		}
		cwd = next
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(configDir, "opsmask")
	if err := EnsurePrivateDir(dir); err != nil {
		return "", err
	}
	return filepath.Join(dir, "mapping.sqlite"), nil
}
