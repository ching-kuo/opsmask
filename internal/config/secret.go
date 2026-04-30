package config

import (
	"crypto/rand"
	"os"
	"path/filepath"
)

func EnsureSecret() ([]byte, error) {
	path, err := userConfigPath("user_secret")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if err := requirePrivate(filepath.Dir(path)); err != nil {
		return nil, err
	}
	if err := rejectNetworkFS(filepath.Dir(path)); err != nil {
		return nil, err
	}
	unlock, err := lockSecret(path + ".lock")
	if err != nil {
		return nil, err
	}
	defer unlock()
	if b, err := os.ReadFile(path); err == nil {
		if err := requirePrivate(path); err != nil {
			return nil, err
		}
		return b, nil
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if b, readErr := os.ReadFile(path); readErr == nil {
			return b, requirePrivate(path)
		}
		return nil, err
	}
	if _, err := f.Write(secret); err != nil {
		_ = f.Close()
		return nil, err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	if d, err := os.Open(filepath.Dir(path)); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return secret, nil
}

func userConfigPath(name string) (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "opsmask", name), nil
}
