package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTrustPathAndContent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(t.TempDir(), ".opsmask")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfg, []byte("literals: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ok, err := IsTrusted(cfg)
	if err != nil || ok {
		t.Fatalf("pre trust ok=%v err=%v", ok, err)
	}
	if err := Trust(cfg); err != nil {
		t.Fatal(err)
	}
	ok, err = IsTrusted(cfg)
	if err != nil || !ok {
		t.Fatalf("post trust ok=%v err=%v", ok, err)
	}
	if err := os.WriteFile(cfg, []byte("literals:\n- name: x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ok, err = IsTrusted(cfg)
	if err != nil || ok {
		t.Fatalf("edit invalidation ok=%v err=%v", ok, err)
	}
}

func TestExecConfigDefaultsAndValidation(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfg, []byte("exec:\n  enabled: true\n  default_timeout: 30s\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.ProjectExec.Enabled || loaded.ProjectExec.Scope != ScopeReadOnly {
		t.Fatalf("exec config = %+v, want enabled read-only default", loaded.ProjectExec)
	}
	if loaded.ProjectExec.DefaultTimeout.String() != "30s" {
		t.Fatalf("timeout = %s", loaded.ProjectExec.DefaultTimeout)
	}
}

func TestExecConfigRejectsLegacyAllowShellAndBadOptOuts(t *testing.T) {
	for name, body := range map[string]string{
		"allow_shell":   "exec:\n  enabled: true\n  allow_shell: true\n",
		"optout_gate":   "exec:\n  enabled: true\n  scope: freeform\n  deny_opt_out:\n    - name: tar\n      reason: needed\n",
		"optout_scope":  "exec:\n  enabled: true\n  scope: read-only\n  allow_deny_opt_out: true\n  deny_opt_out:\n    - name: tar\n      reason: needed\n",
		"optout_reason": "exec:\n  enabled: true\n  scope: freeform\n  allow_deny_opt_out: true\n  deny_opt_out:\n    - name: tar\n      reason: ' '\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Chmod(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			cfg := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadFile(cfg); err == nil {
				t.Fatal("expected config load error")
			}
		})
	}
}

func TestUserWideExecEnableIsIgnored(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg, err := userConfigPath("config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	userDir := filepath.Dir(cfg)
	if err := os.MkdirAll(userDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for p := filepath.Dir(userDir); p != home && p != filepath.Dir(p); p = filepath.Dir(p) {
		_ = os.Chmod(p, 0o700)
	}
	if err := os.WriteFile(cfg, []byte("exec:\n  enabled: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var warnings []string
	loaded, err := Load(t.TempDir(), func(s string) { warnings = append(warnings, s) }, true)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.UserExec.Enabled || loaded.ProjectExec.Enabled {
		t.Fatalf("loaded exec = user:%+v project:%+v", loaded.UserExec, loaded.ProjectExec)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "ignored") {
		t.Fatalf("warnings = %#v", warnings)
	}
}

func TestTrustedProjectDetectorsLoad(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	projectRoot := t.TempDir()
	opsmaskDir := filepath.Join(projectRoot, ".opsmask")
	if err := os.Mkdir(opsmaskDir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(opsmaskDir, "config.yaml")
	body := "detectors:\n  hostname:\n    internal_tlds: [acme, mycorp]\n"
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Trust(cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(projectRoot, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	got := loaded.ProjectDetectors.Hostname.InternalTLDs
	if strings.Join(got, ",") != "acme,mycorp" {
		t.Fatalf("internal_tlds = %#v", got)
	}
}

func TestUserWideDetectorsAreIgnored(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg, err := userConfigPath("config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	userDir := filepath.Dir(cfg)
	if err := os.MkdirAll(userDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for p := filepath.Dir(userDir); p != home && p != filepath.Dir(p); p = filepath.Dir(p) {
		_ = os.Chmod(p, 0o700)
	}
	body := "detectors:\n  hostname:\n    internal_tlds: [acme]\n"
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	var warnings []string
	loaded, err := Load(t.TempDir(), func(s string) { warnings = append(warnings, s) }, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.ProjectDetectors.Hostname.InternalTLDs) != 0 {
		t.Fatalf("user-wide detectors leaked into project config: %+v", loaded.ProjectDetectors)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "user-wide detectors block") {
		t.Fatalf("warnings = %#v", warnings)
	}
}

func TestLoadFileKeepsDetectorConfigForRuntimeGate(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "config.yaml")
	body := "detectors:\n  hostname:\n    internal_tlds: [acme]\n"
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(loaded.ProjectDetectors.Hostname.InternalTLDs, ",") != "acme" {
		t.Fatalf("internal_tlds = %#v", loaded.ProjectDetectors.Hostname.InternalTLDs)
	}
}

func TestUntrustedProjectDetectorsAreIgnored(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	projectRoot := t.TempDir()
	opsmaskDir := filepath.Join(projectRoot, ".opsmask")
	if err := os.Mkdir(opsmaskDir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(opsmaskDir, "config.yaml")
	body := "detectors:\n  hostname:\n    internal_tlds: [acme]\n"
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(projectRoot, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Untrusted {
		t.Fatal("expected untrusted config")
	}
	if len(loaded.ProjectDetectors.Hostname.InternalTLDs) != 0 {
		t.Fatalf("untrusted detectors loaded: %+v", loaded.ProjectDetectors)
	}
}

func TestDetectorInternalTLDValidation(t *testing.T) {
	for name, body := range map[string]string{
		"uppercase": "detectors:\n  hostname:\n    internal_tlds: [Foo]\n",
		"dot":       "detectors:\n  hostname:\n    internal_tlds: [a.b]\n",
		"empty":     "detectors:\n  hostname:\n    internal_tlds: ['']\n",
		"duplicate": "detectors:\n  hostname:\n    internal_tlds: [acme, acme]\n",
		"default":   "detectors:\n  hostname:\n    internal_tlds: [local]\n",
		"collision": "detectors:\n  hostname:\n    internal_tlds: [py]\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Chmod(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			cfg := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadFile(cfg); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestEmptyDetectorsBlockParses(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfg, []byte("detectors: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(cfg); err != nil {
		t.Fatal(err)
	}
}

func TestCustomDetectorCookbookConfigShape(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "config.yaml")
	body := `literals: []
regex_rules:
  - name: app-user-id
    type: app_user
    pattern: '\buser_\d+\b'
    policy: pseudonymize
  - name: app-order-id
    type: app_order
    pattern: '\border_[A-Za-z0-9]+\b'
    policy: pseudonymize
  - name: app-tenant-id
    type: app_tenant
    pattern: '\btenant_[0-9a-f-]{8,}\b'
    policy: pseudonymize
deny_list: []
exec:
  enabled: false
  scope: read-only
`
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Rules) != 3 {
		t.Fatalf("rules = %d, want 3", len(loaded.Rules))
	}
}
