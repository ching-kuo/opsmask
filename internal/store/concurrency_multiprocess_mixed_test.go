package store

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// TestSQLiteMixedLongLivedAndSubprocessWriters models a long-lived
// `opsmask mcp serve` process performing reads/writes against a mapping
// store while a separate subprocess hammers the same file. The plan
// guarantees this scenario must work without truncation collisions or
// stale reads. The existing subprocess-writer-only test does not exercise
// the mixed pattern.
func TestSQLiteMixedLongLivedAndSubprocessWriters(t *testing.T) {
	if os.Getenv("OPSMASK_STORE_MIXED_CHILD") == "1" {
		mixedChildInsert(os.Getenv("OPSMASK_STORE_PATH"))
		return
	}
	if runtime.GOOS == "windows" {
		t.Skip("multi-process file locking semantics differ on Windows")
	}
	path := filepath.Join(t.TempDir(), "mapping.sqlite")

	// Long-lived: continuously insert/lookup/list for the duration of the
	// test, simulating an MCP server that gets sustained tool calls.
	st, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer st.Close()

	stop := make(chan struct{})
	var mixedErr atomic.Value
	go func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
			}
			idx := fmt.Sprintf("%016x", 1_000_000+i)
			m := Mapping{
				Type:        "mcp",
				Index:       idx,
				HMACFull:    []byte(fmt.Sprintf("mcp%029d", i)),
				RealValue:   []byte(fmt.Sprintf("mcp-value-%d", i)),
				FirstSeenAt: time.Now(),
			}
			if err := st.Insert(ctx, m); err != nil {
				mixedErr.Store(fmt.Errorf("mixed insert: %w", err))
				return
			}
			if _, _, err := st.Lookup(ctx, "mcp", idx); err != nil {
				mixedErr.Store(fmt.Errorf("mixed lookup: %w", err))
				return
			}
			if _, err := st.List(ctx, "mcp", 5); err != nil {
				mixedErr.Store(fmt.Errorf("mixed list: %w", err))
				return
			}
			i++
		}
	}()

	// Subprocess writer mirrors the existing concurrency test shape.
	cmd := exec.Command(os.Args[0], "-test.run=TestSQLiteMixedLongLivedAndSubprocessWriters")
	cmd.Env = append(os.Environ(), "OPSMASK_STORE_MIXED_CHILD=1", "OPSMASK_STORE_PATH="+path)
	out, err := cmd.CombinedOutput()
	close(stop)
	if err != nil {
		t.Fatalf("subprocess: %v: %s", err, out)
	}
	if v := mixedErr.Load(); v != nil {
		t.Fatalf("mixed worker: %v", v)
	}
	// Reopen and verify both writer streams left consistent state.
	checker, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer checker.Close()
	stats, err := checker.Stats(context.Background())
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.ByType["ip4"] != 500 {
		t.Fatalf("subprocess inserts left ip4 count = %d, want 500", stats.ByType["ip4"])
	}
	if stats.ByType["mcp"] == 0 {
		t.Fatal("long-lived writer never inserted mcp rows")
	}
}

func mixedChildInsert(path string) {
	st, err := OpenSQLite(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open: %v\n", err)
		os.Exit(1)
	}
	defer st.Close()
	for i := 0; i < 500; i++ {
		idx := fmt.Sprintf("%016x", i)
		m := Mapping{
			Type:        "ip4",
			Index:       idx,
			HMACFull:    []byte(fmt.Sprintf("%032d", i)),
			RealValue:   []byte(fmt.Sprintf("10.1.0.%d", i)),
			FirstSeenAt: time.Now(),
		}
		if err := st.Insert(context.Background(), m); err != nil {
			fmt.Fprintf(os.Stderr, "insert: %v\n", err)
			os.Exit(1)
		}
	}
}
