package corpus

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ching-kuo/opsmask/internal/detect"
	"github.com/ching-kuo/opsmask/internal/engine"
	"github.com/ching-kuo/opsmask/internal/pseudo"
	"github.com/ching-kuo/opsmask/internal/store"
)

// fixedTestSecret is the allocator HMAC secret used by every corpus
// invocation. The value is irrelevant because Canonicalize wildcards token
// IDs; using a constant makes RunMask deterministic for any caller.
var fixedTestSecret = []byte("opsmask-corpus-fixed-test-secret-32b")

// RunMask masks input through the production engine.Process pipeline against
// builtin detectors, using an ephemeral SQLite mapping store that is created
// in os.TempDir and removed before return. No persistent state outside the
// scenario directory is touched.
func RunMask(ctx context.Context, input []byte) ([]byte, error) {
	tmpDir, err := os.MkdirTemp("", "opsmask-corpus-*")
	if err != nil {
		return nil, fmt.Errorf("corpus: create temp dir: %w", err)
	}
	// Cleanup must close the SQLite handle before removing the directory:
	// open file handles on macOS/Windows can prevent directory removal.
	st, err := store.OpenSQLite(filepath.Join(tmpDir, "mapping.sqlite"))
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("corpus: open ephemeral store: %w", err)
	}
	defer func() {
		_ = st.Close()
		_ = os.RemoveAll(tmpDir)
	}()

	rules, err := detect.BuiltinRules()
	if err != nil {
		return nil, fmt.Errorf("corpus: load builtin rules: %w", err)
	}
	alloc := pseudo.New(fixedTestSecret, st)

	var out bytes.Buffer
	if _, err := engine.Process(ctx, bytes.NewReader(input), &out, rules, alloc, engine.Options{}); err != nil {
		return nil, fmt.Errorf("corpus: engine.Process: %w", err)
	}
	return out.Bytes(), nil
}
