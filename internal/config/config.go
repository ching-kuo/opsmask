package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ching-kuo/opsmask/internal/detect"
	"github.com/ching-kuo/opsmask/internal/exec/denybase"
	"github.com/ching-kuo/opsmask/internal/policy"
	"gopkg.in/yaml.v3"
)

const MaxRules = 100

type Config struct {
	Literals   []Literal       `yaml:"literals"`
	RegexRules []RegexRule     `yaml:"regex_rules"`
	DenyList   []DenyEntry     `yaml:"deny_list"`
	Exec       ExecConfig      `yaml:"exec"`
	Detectors  DetectorsConfig `yaml:"detectors"`
}

type Literal struct {
	Name   string        `yaml:"name"`
	Type   string        `yaml:"type"`
	Value  string        `yaml:"value"`
	Policy policy.Policy `yaml:"policy"`
}

type RegexRule struct {
	Name    string        `yaml:"name"`
	Type    string        `yaml:"type"`
	Pattern string        `yaml:"pattern"`
	Policy  policy.Policy `yaml:"policy"`
}

type DenyEntry struct {
	Name    string `yaml:"name"`
	Literal string `yaml:"literal"`
	Pattern string `yaml:"pattern"`
}

type DetectorsConfig struct {
	Hostname HostnameDetectorConfig `yaml:"hostname"`
}

type HostnameDetectorConfig struct {
	InternalTLDs []string `yaml:"internal_tlds"`
}

type ExecScope string

const (
	ScopeReadOnly    ExecScope = "read-only"
	ScopeInvestigate ExecScope = "investigate"
	ScopeFreeform    ExecScope = "freeform"
)

type AllowEntry struct {
	Name      string              `yaml:"name"`
	Elements  []*regexp.Regexp    `yaml:"-"`
	AnyTail   bool                `yaml:"-"`
	MatchFunc func([]string) bool `yaml:"-"`
}

type DenyOptOutEntry struct {
	Name   string `yaml:"name" json:"name"`
	Reason string `yaml:"reason" json:"reason"`
}

type ExecConfig struct {
	Enabled         bool              `yaml:"enabled"`
	Scope           ExecScope         `yaml:"scope"`
	Allow           []AllowEntry      `yaml:"allow"`
	AllowDenyOptOut bool              `yaml:"allow_deny_opt_out"`
	DenyOptOut      []DenyOptOutEntry `yaml:"deny_opt_out"`
	EnvAllow        []string          `yaml:"env_allow"`
	EnvDeny         []string          `yaml:"env_deny"`
	DefaultTimeout  time.Duration     `yaml:"-"`
}

func (e *ExecConfig) UnmarshalYAML(value *yaml.Node) error {
	type rawExecConfig struct {
		Enabled         bool              `yaml:"enabled"`
		Scope           ExecScope         `yaml:"scope"`
		Allow           []AllowEntry      `yaml:"allow"`
		AllowDenyOptOut bool              `yaml:"allow_deny_opt_out"`
		DenyOptOut      []DenyOptOutEntry `yaml:"deny_opt_out"`
		EnvAllow        []string          `yaml:"env_allow"`
		EnvDeny         []string          `yaml:"env_deny"`
		DefaultTimeout  string            `yaml:"default_timeout"`
		AllowShell      *bool             `yaml:"allow_shell"`
	}
	var raw rawExecConfig
	if err := value.Decode(&raw); err != nil {
		return err
	}
	if raw.AllowShell != nil {
		return fmt.Errorf("exec.allow_shell is no longer supported; use exec.scope: freeform instead")
	}
	*e = ExecConfig{
		Enabled: raw.Enabled, Scope: raw.Scope, Allow: raw.Allow,
		AllowDenyOptOut: raw.AllowDenyOptOut, DenyOptOut: raw.DenyOptOut,
		EnvAllow: raw.EnvAllow, EnvDeny: raw.EnvDeny,
	}
	if raw.DefaultTimeout != "" {
		d, err := time.ParseDuration(raw.DefaultTimeout)
		if err != nil {
			return fmt.Errorf("exec.default_timeout %q: %w", raw.DefaultTimeout, err)
		}
		e.DefaultTimeout = d
	}
	return nil
}

func (a *AllowEntry) UnmarshalYAML(value *yaml.Node) error {
	type rawAllowEntry struct {
		Name     string   `yaml:"name"`
		Elements []string `yaml:"elements"`
	}
	var raw rawAllowEntry
	if err := value.Decode(&raw); err != nil {
		return err
	}
	a.Name = raw.Name
	if len(raw.Elements) == 0 {
		return fmt.Errorf("exec.allow entry %q has no elements", raw.Name)
	}
	elems := raw.Elements
	if elems[len(elems)-1] == "…" || elems[len(elems)-1] == "..." {
		a.AnyTail = true
		elems = elems[:len(elems)-1]
	}
	for i, pat := range elems {
		re, err := regexp.Compile(pat)
		if err != nil {
			return fmt.Errorf("exec.allow entry %q element %d regex %q: %w", raw.Name, i, pat, err)
		}
		a.Elements = append(a.Elements, re)
	}
	return nil
}

type Loaded struct {
	Rules            []detect.Rule
	UserRules        []detect.Rule
	ProjectRules     []detect.Rule
	DenyList         []DenyEntry
	UserExec         ExecConfig
	ProjectExec      ExecConfig
	ProjectDetectors DetectorsConfig
	Warnings         []string
	Untrusted        bool
}

func Load(cwd string, stderr func(string), requireProjectTrust bool) (Loaded, error) {
	var out Loaded
	homeCfg, _ := userConfigPath("config.yaml")
	if fileExists(homeCfg) {
		cfg, err := parseConfig(homeCfg)
		if err != nil {
			return out, err
		}
		rules, err := rulesFromConfig(cfg)
		if err != nil {
			return out, err
		}
		out.UserRules = append(out.UserRules, rules...)
		out.Rules = append(out.Rules, rules...)
		out.DenyList = append(out.DenyList, cfg.DenyList...)
		out.UserExec = cfg.Exec
		if cfg.Exec.Enabled {
			msg := fmt.Sprintf("user-wide exec.enabled in %s is ignored; enable exec only in a trusted project .opsmask/config.yaml", homeCfg)
			out.Warnings = append(out.Warnings, msg)
			if stderr != nil {
				stderr(msg)
			}
		}
		if detectorsConfigured(cfg.Detectors) {
			msg := fmt.Sprintf("user-wide detectors block in %s is ignored; configure detectors via trusted project .opsmask/config.yaml", homeCfg)
			out.Warnings = append(out.Warnings, msg)
			if stderr != nil {
				stderr(msg)
			}
		}
	}
	project := findProjectConfig(cwd)
	if project == "" {
		return out, nil
	}
	if requireProjectTrust {
		ok, err := IsTrusted(project)
		if err != nil {
			return out, err
		}
		if !ok {
			empty, err := projectConfigEmpty(project)
			if err != nil {
				return out, err
			}
			if empty {
				return out, nil
			}
			out.Untrusted = true
			summary := summarizeUntrusted(project)
			msg := "project .opsmask/config.yaml is untrusted; run `opsmask config trust` to enable its rules" + summary
			out.Warnings = append(out.Warnings, msg)
			if stderr != nil {
				stderr(msg)
			}
			return out, nil
		}
	}
	cfg, err := parseConfig(project)
	if err != nil {
		return out, err
	}
	rules, err := rulesFromConfig(cfg)
	if err != nil {
		return out, err
	}
	out.Rules = append(out.Rules, rules...)
	out.ProjectRules = append(out.ProjectRules, rules...)
	out.DenyList = append(out.DenyList, cfg.DenyList...)
	out.ProjectExec = cfg.Exec
	out.ProjectDetectors = cfg.Detectors
	return out, nil
}

func summarizeUntrusted(path string) string {
	cfg, err := parseConfig(path)
	if err != nil {
		return ""
	}
	execBlocks := 0
	if cfg.Exec.Enabled {
		execBlocks = 1
	}
	detectorBlocks := 0
	if detectorsConfigured(cfg.Detectors) {
		detectorBlocks = 1
	}
	return fmt.Sprintf(" (pending: literals=%d regex_rules=%d deny_list=%d exec=%d detectors=%d)", len(cfg.Literals), len(cfg.RegexRules), len(cfg.DenyList), execBlocks, detectorBlocks)
}

func projectConfigEmpty(path string) (bool, error) {
	cfg, err := parseConfig(path)
	if err != nil {
		return false, err
	}
	return len(cfg.Literals) == 0 && len(cfg.RegexRules) == 0 && len(cfg.DenyList) == 0 && !cfg.Exec.Enabled && !detectorsConfigured(cfg.Detectors), nil
}

func LoadFile(path string) (Loaded, error) {
	var out Loaded
	cfg, err := parseConfig(path)
	if err != nil {
		return out, err
	}
	rules, err := rulesFromConfig(cfg)
	if err != nil {
		return out, err
	}
	out.Rules = rules
	out.ProjectRules = rules
	out.DenyList = cfg.DenyList
	out.ProjectExec = cfg.Exec
	out.ProjectDetectors = cfg.Detectors
	return out, nil
}

func SummarizeFile(path string) (literals, regexRules, denyList int, err error) {
	cfg, err := parseConfig(path)
	if err != nil {
		return 0, 0, 0, err
	}
	return len(cfg.Literals), len(cfg.RegexRules), len(cfg.DenyList), nil
}

func parseConfig(path string) (Config, error) {
	if err := requirePrivate(path); err != nil {
		return Config{}, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	if len(b) > 128*1024 {
		return Config{}, fmt.Errorf("%s exceeds config size cap", path)
	}
	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader(b))
	if err := dec.Decode(&cfg); err != nil {
		if err != io.EOF {
			return Config{}, err
		}
	}
	if len(cfg.Literals)+len(cfg.RegexRules) > MaxRules {
		return Config{}, fmt.Errorf("config has more than %d rules", MaxRules)
	}
	if err := validateExecConfig(&cfg.Exec); err != nil {
		return Config{}, err
	}
	if err := validateDetectorsConfig(&cfg.Detectors); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// internalTLDRE matches the Hostname rule's final-label class
// (`[a-z]{2,24}` in builtin.go). Labels accepted here are guaranteed to
// satisfy the regex prefilter; hyphens and digits are rejected so config
// entries can't silently fail to ever match.
var internalTLDRE = regexp.MustCompile(`^[a-z]{2,24}$`)

func validateDetectorsConfig(d *DetectorsConfig) error {
	seen := map[string]bool{}
	for i, raw := range d.Hostname.InternalTLDs {
		label := strings.TrimSpace(raw)
		if label == "" {
			return fmt.Errorf("detectors.hostname.internal_tlds[%d] is empty", i)
		}
		if !internalTLDRE.MatchString(label) {
			return fmt.Errorf("detectors.hostname.internal_tlds[%d] %q is invalid; must be 2-24 lowercase letters (no digits, hyphens, or dots) to match the hostname final-label regex", i, raw)
		}
		if seen[label] {
			return fmt.Errorf("detectors.hostname.internal_tlds[%d] duplicates %q", i, label)
		}
		status, reason := detect.ReservedTLDStatus(label)
		switch status {
		case detect.TLDDefaultInternal:
			return fmt.Errorf("detectors.hostname.internal_tlds[%d] %q is already a default RFC-reserved internal TLD (%s)", i, label, reason)
		case detect.TLDCollision:
			return fmt.Errorf("detectors.hostname.internal_tlds[%d] %q collides with fixed code/log compatibility-exception ccTLD set (%s); use project-defined regex_rules to mask .%s-suffix paths", i, label, reason, label)
		}
		seen[label] = true
		d.Hostname.InternalTLDs[i] = label
	}
	return nil
}

func detectorsConfigured(d DetectorsConfig) bool {
	return len(d.Hostname.InternalTLDs) > 0
}

func validateExecConfig(e *ExecConfig) error {
	if !e.Enabled {
		if len(e.DenyOptOut) > 0 || e.AllowDenyOptOut {
			return fmt.Errorf("exec.deny_opt_out requires exec.enabled: true")
		}
		return nil
	}
	if e.Scope == "" {
		e.Scope = ScopeReadOnly
	}
	switch e.Scope {
	case ScopeReadOnly, ScopeInvestigate, ScopeFreeform:
	default:
		return fmt.Errorf("exec.scope %q is invalid; valid values are read-only, investigate, freeform", e.Scope)
	}
	for _, field := range []struct {
		name string
		vars []string
	}{
		{"exec.env_allow", e.EnvAllow},
		{"exec.env_deny", e.EnvDeny},
	} {
		for _, name := range field.vars {
			if strings.TrimSpace(name) != name || name == "" || strings.ContainsAny(name, "= \t\n\r") {
				return fmt.Errorf("%s contains invalid env var name %q", field.name, name)
			}
		}
	}
	if len(e.DenyOptOut) > 0 {
		if e.Scope != ScopeFreeform {
			return fmt.Errorf("exec.deny_opt_out is allowed only with exec.scope: freeform")
		}
		if !e.AllowDenyOptOut {
			return fmt.Errorf("exec.deny_opt_out requires exec.allow_deny_opt_out: true")
		}
		for _, entry := range e.DenyOptOut {
			if strings.TrimSpace(entry.Reason) == "" {
				return fmt.Errorf("exec.deny_opt_out %q requires a non-empty reason", entry.Name)
			}
			if !denybase.Contains(entry.Name) {
				return fmt.Errorf("exec.deny_opt_out name %q is not a known hard-deny entry", entry.Name)
			}
		}
	}
	return nil
}

func rulesFromConfig(cfg Config) ([]detect.Rule, error) {
	secrets := policy.BuiltinSecretTypes()
	var out []detect.Rule
	for _, lit := range cfg.Literals {
		if !lit.Policy.Valid() {
			return nil, fmt.Errorf("literal %s has invalid policy", lit.Name)
		}
		if secrets[lit.Type] && lit.Policy != policy.Destroy {
			return nil, fmt.Errorf("policy downgrade rejected for %s", lit.Type)
		}
		re := regexp.MustCompile(regexp.QuoteMeta(lit.Value))
		out = append(out, detect.Rule{Name: lit.Name, Type: lit.Type, Policy: lit.Policy, Regex: re, MaxMatchSpan: len(lit.Value) + 1})
	}
	for _, rr := range cfg.RegexRules {
		if !rr.Policy.Valid() {
			return nil, fmt.Errorf("regex %s has invalid policy", rr.Name)
		}
		if len(rr.Pattern) > MaxRegexPatternSize {
			return nil, fmt.Errorf("regex %s exceeds %d bytes", rr.Name, MaxRegexPatternSize)
		}
		re, err := regexp.Compile(rr.Pattern)
		if err != nil {
			return nil, err
		}
		if re.NumSubexp() > MaxRegexGroups {
			return nil, fmt.Errorf("regex %s has too many capture groups", rr.Name)
		}
		if secrets[rr.Type] && rr.Policy != policy.Destroy {
			return nil, fmt.Errorf("policy downgrade rejected for %s", rr.Type)
		}
		out = append(out, detect.Rule{Name: rr.Name, Type: rr.Type, Policy: rr.Policy, Regex: re, MaxMatchSpan: MaxMatchSpan})
	}
	return out, nil
}

func findProjectConfig(cwd string) string {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	for {
		p := filepath.Join(cwd, ".opsmask", "config.yaml")
		if fileExists(p) {
			return p
		}
		next := filepath.Dir(cwd)
		if next == cwd {
			return ""
		}
		cwd = next
	}
}

func HashFile(path string) (realPath, sum string, err error) {
	realPath, err = filepath.EvalSymlinks(path)
	if err != nil {
		return "", "", err
	}
	b, err := os.ReadFile(realPath)
	if err != nil {
		return "", "", err
	}
	h := sha256.Sum256(b)
	return realPath, hex.EncodeToString(h[:]), nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func requirePrivate(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s must not be group/world accessible", path)
	}
	parent := filepath.Dir(path)
	if pinfo, err := os.Stat(parent); err == nil && pinfo.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s must not be group/world accessible", parent)
	}
	return nil
}
