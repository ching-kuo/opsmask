package store_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ching-kuo/opsmask/internal/store"
)

func newSQLite(t *testing.T) *store.SQLite {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	st, err := store.OpenSQLite(filepath.Join(dir, "mapping.sqlite"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestStatsEmpty(t *testing.T) {
	st := newSQLite(t)
	s, err := st.Stats(context.Background())
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if s.Total != 0 {
		t.Fatalf("Total = %d, want 0", s.Total)
	}
	if len(s.ByType) != 0 {
		t.Fatalf("ByType = %v, want empty", s.ByType)
	}
}

func TestStatsAggregatesRows(t *testing.T) {
	st := newSQLite(t)
	now := time.Now()
	rows := []store.Mapping{
		{Type: "ip4", HMACFull: []byte("h1"), Index: "0000000000000001", RealValue: []byte("1.1.1.1"), FirstSeenAt: now},
		{Type: "ip4", HMACFull: []byte("h2"), Index: "0000000000000002", RealValue: []byte("1.1.1.2"), FirstSeenAt: now},
		{Type: "ip4", HMACFull: []byte("h3"), Index: "0000000000000003", RealValue: []byte("1.1.1.3"), FirstSeenAt: now},
		{Type: "email", HMACFull: []byte("e1"), Index: "0000000000000010", RealValue: []byte("a@b"), FirstSeenAt: now},
		{Type: "email", HMACFull: []byte("e2"), Index: "0000000000000011", RealValue: []byte("c@d"), FirstSeenAt: now},
	}
	if err := st.InsertBatch(context.Background(), rows); err != nil {
		t.Fatalf("InsertBatch: %v", err)
	}
	s, err := st.Stats(context.Background())
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if s.Total != 5 {
		t.Fatalf("Total = %d, want 5", s.Total)
	}
	if s.ByType["ip4"] != 3 || s.ByType["email"] != 2 {
		t.Fatalf("ByType = %v, want ip4=3 email=2", s.ByType)
	}
}

func TestStatsCancelledCtx(t *testing.T) {
	st := newSQLite(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := st.Stats(ctx); err == nil {
		t.Fatal("expected ctx error")
	}
}
