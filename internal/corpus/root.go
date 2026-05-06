package corpus

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// maxRootWalkDepth bounds the go.mod search so an invocation outside any
// Go module fails fast instead of climbing to the filesystem root.
const maxRootWalkDepth = 16

// ErrNoModule is returned by CorpusRoot when no go.mod is found by walking
// up from the current working directory.
var ErrNoModule = errors.New("corpus: no go.mod found above cwd")

// CorpusRoot returns the absolute path to <repo>/testdata/corpus by walking
// up from the current working directory until it finds a go.mod file. Used
// by both the test harness and the CLI so they resolve the same root
// regardless of where they were invoked from.
func CorpusRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("corpus: getwd: %w", err)
	}
	dir, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("corpus: abs cwd: %w", err)
	}
	for depth := 0; depth < maxRootWalkDepth; depth++ {
		if fi, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !fi.IsDir() {
			return filepath.Join(dir, "testdata", "corpus"), nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", ErrNoModule
}
