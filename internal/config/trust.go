package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type TrustEntry struct {
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	TrustedAt int64  `json:"trusted_at"`
}

func Trust(path string) error {
	real, sum, err := HashFile(path)
	if err != nil {
		return err
	}
	entries, _ := readTrust()
	entries[real] = TrustEntry{Path: real, SHA256: sum, TrustedAt: time.Now().Unix()}
	return writeTrust(entries)
}

func IsTrusted(path string) (bool, error) {
	real, sum, err := HashFile(path)
	if err != nil {
		return false, err
	}
	entries, err := readTrust()
	if err != nil {
		return false, err
	}
	e, ok := entries[real]
	return ok && e.SHA256 == sum, nil
}

func readTrust() (map[string]TrustEntry, error) {
	path, err := userConfigPath("trust.json")
	if err != nil {
		return nil, err
	}
	if !fileExists(path) {
		return map[string]TrustEntry{}, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m map[string]TrustEntry
	if err := json.Unmarshal(b, &m); err != nil {
		_ = os.Rename(path, path+".bak")
		return map[string]TrustEntry{}, nil
	}
	return m, nil
}

func writeTrust(m map[string]TrustEntry) error {
	path, err := userConfigPath("trust.json")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}
