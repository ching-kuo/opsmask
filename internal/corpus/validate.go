package corpus

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// scenarioNameRe enforces lowercase kebab-case with a minimum length of 3
// (one leading char, at least one inner char, one trailing char). The '+'
// quantifier is load-bearing - '*' would allow length-2 names like "aa"
// that violate the documented contract.
var scenarioNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]+[a-z0-9]$`)

// ValidateScenarioName accepts kebab-case names of length 3+ and rejects
// anything that could escape the corpus root (path separators, traversal,
// uppercase, whitespace, leading/trailing hyphen). The smoke scenario
// directory `_smoke-hello/` is created at planning time and is enumerated
// by Discover without re-validation; this validator gates only user input.
func ValidateScenarioName(name string) error {
	if name == "" {
		return fmt.Errorf("scenario name: empty")
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("scenario name: must not contain path separators: %q", name)
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("scenario name: must not contain '..': %q", name)
	}
	if !scenarioNameRe.MatchString(name) {
		return fmt.Errorf("scenario name: must match %s (length >= 3): %q", scenarioNameRe, name)
	}
	return nil
}

// ScenarioPath joins root and name after validating the name and confirms
// that the cleaned join stays inside root via filepath.Rel. String-prefix
// containment checks are intentionally avoided - they break on symlinks
// and trailing-separator edge cases.
func ScenarioPath(root, name string) (string, error) {
	if err := ValidateScenarioName(name); err != nil {
		return "", err
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("scenario path: abs root: %w", err)
	}
	joined := filepath.Clean(filepath.Join(absRoot, name))
	rel, err := filepath.Rel(absRoot, joined)
	if err != nil {
		return "", fmt.Errorf("scenario path: rel: %w", err)
	}
	if rel == "." || rel == "" || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", fmt.Errorf("scenario path: escape detected for name %q", name)
	}
	return joined, nil
}
