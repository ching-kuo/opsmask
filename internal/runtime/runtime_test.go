package runtime_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ching-kuo/opsmask/internal/runtime"
)

func TestNewRuntimeProducesUsableEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Chdir(t.TempDir())

	mappingDir := t.TempDir()
	if err := os.Chmod(mappingDir, 0o700); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	mapping := filepath.Join(mappingDir, "mapping.sqlite")
	env, err := runtime.New(runtime.Options{Mapping: mapping})
	if err != nil {
		t.Fatalf("runtime.New: %v", err)
	}
	t.Cleanup(func() { _ = env.Close() })

	if env.Store == nil {
		t.Fatal("Env.Store is nil")
	}
	if env.Alloc == nil {
		t.Fatal("Env.Alloc is nil")
	}
	if len(env.Rules) == 0 {
		t.Fatal("Env.Rules is empty (expected built-in detectors)")
	}
}

func TestCloseOnNilReceiverIsSafe(t *testing.T) {
	var env *runtime.Env
	if err := env.Close(); err != nil {
		t.Fatalf("nil Close returned error: %v", err)
	}
}
