package engine_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ching-kuo/opsmask/internal/detect"
	"github.com/ching-kuo/opsmask/internal/engine"
	"github.com/ching-kuo/opsmask/internal/pseudo"
	"github.com/ching-kuo/opsmask/internal/store"
)

func newTestAllocator(t *testing.T) *pseudo.Allocator {
	t.Helper()
	dir := t.TempDir()
	st, err := store.OpenSQLite(filepath.Join(dir, "mapping.sqlite"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return pseudo.New([]byte("test-secret"), st)
}

func TestProcessRespectsCancelledContext(t *testing.T) {
	rules, err := detect.BuiltinRules()
	if err != nil {
		t.Fatalf("BuiltinRules: %v", err)
	}
	alloc := newTestAllocator(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Synthetic input large enough that an uncancelled call would consume
	// many chunks; we expect the first ctx-check site to short-circuit.
	in := strings.NewReader(strings.Repeat("noise ", 1<<10))
	var out bytes.Buffer
	_, err = engine.Process(ctx, in, &out, rules, alloc, engine.Options{})
	if err == nil {
		t.Fatal("expected ctx error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestProcessCancelMidStream(t *testing.T) {
	rules, err := detect.BuiltinRules()
	if err != nil {
		t.Fatalf("BuiltinRules: %v", err)
	}
	alloc := newTestAllocator(t)

	// 2 MiB input drives the engine through multiple chunker iterations.
	// Cancel after a short delay; assert we return within a generous bound.
	in := strings.NewReader(strings.Repeat("noise ", 1<<18))
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	deadline := time.Now().Add(2 * time.Second)
	done := make(chan error, 1)
	go func() {
		_, err := engine.Process(ctx, in, io.Discard, rules, alloc, engine.Options{})
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected ctx error")
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(time.Until(deadline)):
		t.Fatal("Process did not return after cancel")
	}
}
