package runtime_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ching-kuo/opsmask/internal/config"
	"github.com/ching-kuo/opsmask/internal/engine"
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

func TestHostnameInternalTLDTrustGate(t *testing.T) {
	env := newRuntimeForTest(t, "", "")
	if hostnameMatches(env, "db-1.prod.acme") {
		t.Fatal("hostname matched unconfigured internal TLD")
	}

	projectRoot := writeTrustedProjectConfig(t, "detectors:\n  hostname:\n    internal_tlds: [acme]\n")
	env = newRuntimeForTest(t, projectRoot, "")
	if !hostnameMatches(env, "db-1.prod.acme") {
		t.Fatal("hostname did not match trusted project internal TLD")
	}

	configPath := writePrivateConfig(t, "detectors:\n  hostname:\n    internal_tlds: [acme]\n")
	var warn bytes.Buffer
	env = newRuntimeForTestWithOptions(t, runtime.Options{Config: configPath, Warn: &warn}, "")
	if hostnameMatches(env, "db-1.prod.acme") {
		t.Fatal("hostname matched internal TLD from --config")
	}
	if !strings.Contains(warn.String(), "detector settings in --config") {
		t.Fatalf("warning = %q", warn.String())
	}
}

func TestHostnameInternalTLDsAreRuntimeLocal(t *testing.T) {
	acmeRoot := writeTrustedProjectConfig(t, "detectors:\n  hostname:\n    internal_tlds: [acme]\n")
	acme := newRuntimeForTest(t, acmeRoot, "")

	corpRoot := writeTrustedProjectConfig(t, "detectors:\n  hostname:\n    internal_tlds: [corpbox]\n")
	corp := newRuntimeForTest(t, corpRoot, "")

	if !hostnameMatches(acme, "db-1.prod.acme") || hostnameMatches(acme, "db-1.prod.corpbox") {
		t.Fatal("acme runtime did not keep its hostname TLD configuration isolated")
	}
	if !hostnameMatches(corp, "db-1.prod.corpbox") || hostnameMatches(corp, "db-1.prod.acme") {
		t.Fatal("corpbox runtime did not keep its hostname TLD configuration isolated")
	}
}

func TestHostnameInternalTLDMasksThroughEngine(t *testing.T) {
	projectRoot := writeTrustedProjectConfig(t, "detectors:\n  hostname:\n    internal_tlds: [acme]\n")
	env := newRuntimeForTest(t, projectRoot, "")
	var out bytes.Buffer
	stats, err := engine.Process(context.Background(), strings.NewReader("db-1.prod.acme accessed at 12:00"), &out, env.Rules, env.Alloc, engine.Options{ASCIITokens: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats.ByType["hostname"] != 1 || strings.Contains(out.String(), "db-1.prod.acme") {
		t.Fatalf("masked=%d output=%q", stats.ByType["hostname"], out.String())
	}
}

func TestHostnameOverlapStillPrefersWiderRules(t *testing.T) {
	env := newRuntimeForTest(t, "", "")
	var out bytes.Buffer
	input := "login https://user:pw@api.foo.com and user@mail.example.org"
	stats, err := engine.Process(context.Background(), strings.NewReader(input), &out, env.Rules, env.Alloc, engine.Options{ASCIITokens: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats.ByType["password_url"] != 1 {
		t.Fatalf("password_url count = %d", stats.ByType["password_url"])
	}
	if stats.ByType["email"] != 1 {
		t.Fatalf("email count = %d", stats.ByType["email"])
	}
	if stats.ByType["hostname"] != 0 {
		t.Fatalf("embedded hostname count = %d", stats.ByType["hostname"])
	}
	if strings.Contains(out.String(), "api.foo.com") || strings.Contains(out.String(), "mail.example.org") {
		t.Fatalf("output retained embedded plaintext: %q", out.String())
	}
}

func newRuntimeForTest(t *testing.T, cwd string, configPath string) *runtime.Env {
	t.Helper()
	return newRuntimeForTestWithOptions(t, runtime.Options{Config: configPath}, cwd)
}

func newRuntimeForTestWithOptions(t *testing.T, opts runtime.Options, cwd string) *runtime.Env {
	t.Helper()
	home := t.TempDir()
	if cwd == "" {
		t.Setenv("HOME", home)
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
		t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
		cwd = t.TempDir()
	}
	t.Chdir(cwd)
	mappingDir := t.TempDir()
	if err := os.Chmod(mappingDir, 0o700); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	opts.Mapping = filepath.Join(mappingDir, "mapping.sqlite")
	env, err := runtime.New(opts)
	if err != nil {
		t.Fatalf("runtime.New: %v", err)
	}
	t.Cleanup(func() { _ = env.Close() })
	return env
}

func hostnameMatches(env *runtime.Env, input string) bool {
	for _, r := range env.Rules {
		if r.Type == "hostname" {
			return len(r.Regex.FindAllIndex([]byte(input), -1)) > 0 && r.Check([]byte(input))
		}
	}
	return false
}

func writeTrustedProjectConfig(t *testing.T, body string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	root := t.TempDir()
	opsmaskDir := filepath.Join(root, ".opsmask")
	if err := os.Mkdir(opsmaskDir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(opsmaskDir, "config.yaml")
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := config.Trust(cfg); err != nil {
		t.Fatal(err)
	}
	return root
}

func writePrivateConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfg
}
