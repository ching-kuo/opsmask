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

// pacedReader delivers data in small windows and watches ctx so cancellation
// is event-driven rather than wall-clock racing the engine. Each Read either
// returns ctx.Err() (when canceled) or yields up to 1 KiB after a brief pace
// delay; this keeps the engine demonstrably mid-stream when cancel fires.
type pacedReader struct {
	ctx context.Context
	r   io.Reader
}

func (p *pacedReader) Read(buf []byte) (int, error) {
	select {
	case <-p.ctx.Done():
		return 0, p.ctx.Err()
	case <-time.After(5 * time.Millisecond):
	}
	if len(buf) > 1024 {
		buf = buf[:1024]
	}
	return p.r.Read(buf)
}

func TestProcessCancelMidStream(t *testing.T) {
	rules, err := detect.BuiltinRules()
	if err != nil {
		t.Fatalf("BuiltinRules: %v", err)
	}
	alloc := newTestAllocator(t)

	ctx, cancel := context.WithCancel(context.Background())
	in := &pacedReader{ctx: ctx, r: strings.NewReader(strings.Repeat("noise ", 1<<18))}
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
