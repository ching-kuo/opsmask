package corpus

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Scenario is a single corpus entry: a directory under the corpus root
// containing input.txt and expected.txt (and optionally README.md).
type Scenario struct {
	Name         string
	Dir          string
	InputPath    string
	ExpectedPath string
}

// Discover enumerates direct child directories of root as scenarios. Each
// scenario must contain both input.txt and expected.txt; missing files are
// reported as errors naming the offending scenario. Hidden directories
// (starting with '.') are skipped. Names starting with '_' are kept so the
// smoke scenario `_smoke-hello/` shows up.
func Discover(root string) ([]Scenario, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("corpus discover: %w", err)
	}
	scenarios := make([]Scenario, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		dir := filepath.Join(root, name)
		input := filepath.Join(dir, "input.txt")
		expected := filepath.Join(dir, "expected.txt")
		if _, err := os.Stat(input); err != nil {
			return nil, fmt.Errorf("corpus discover: scenario %q missing input.txt: %w", name, err)
		}
		if _, err := os.Stat(expected); err != nil {
			return nil, fmt.Errorf("corpus discover: scenario %q missing expected.txt: %w", name, err)
		}
		scenarios = append(scenarios, Scenario{
			Name:         name,
			Dir:          dir,
			InputPath:    input,
			ExpectedPath: expected,
		})
	}
	sort.Slice(scenarios, func(i, j int) bool { return scenarios[i].Name < scenarios[j].Name })
	return scenarios, nil
}
