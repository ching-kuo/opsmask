//go:build windows

package config

func lockSecret(path string) (func(), error) {
	return func() {}, nil
}
