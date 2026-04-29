package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteInsertLookupListPrune(t *testing.T) {
	dir := t.TempDir()
	st, err := OpenSQLite(filepath.Join(dir, "mapping.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	m := Mapping{Type: "ip4", Index: "0123456789abcdef", HMACFull: []byte("01234567890123456789012345678901"), RealValue: []byte("10.0.0.1"), FirstSeenAt: time.Now().Add(-time.Hour)}
	if err := st.Insert(ctx, m); err != nil {
		t.Fatal(err)
	}
	got, ok, err := st.Lookup(ctx, "ip4", m.Index)
	if err != nil || !ok || string(got) != "10.0.0.1" {
		t.Fatalf("lookup got=%q ok=%v err=%v", got, ok, err)
	}
	rows, err := st.List(ctx, "ip4", 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("list len=%d err=%v", len(rows), err)
	}
	n, err := st.Prune(ctx, "ip4", time.Minute)
	if err != nil || n != 1 {
		t.Fatalf("prune n=%d err=%v", n, err)
	}
}
