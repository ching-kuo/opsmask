package cchook

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const secretName = "hook_secret"

func EnsureSecret() error {
	path, err := secretPath()
	if err != nil {
		return err
	}
	if _, err := loadSecretFile(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(hex.EncodeToString(raw)+"\n"), 0o600)
}

func LoadSecret() ([]byte, error) {
	path, err := secretPath()
	if err != nil {
		return nil, err
	}
	return loadSecretFile(path)
}

func secretPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "opsmask", secretName), nil
}

func loadSecretFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode().Perm() != 0o600 {
		return nil, fmt.Errorf("%s must have mode 0600", path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	raw, err := hex.DecodeString(strings.TrimSpace(string(body)))
	if err != nil {
		return nil, fmt.Errorf("decode hook secret: %w", err)
	}
	if len(raw) != 32 {
		return nil, fmt.Errorf("hook secret must decode to 32 bytes")
	}
	return raw, nil
}
