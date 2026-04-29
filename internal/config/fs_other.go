//go:build !linux && !darwin

package config

func rejectNetworkFS(path string) error { return nil }
