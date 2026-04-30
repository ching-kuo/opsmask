package pseudo

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"

	"github.com/ching-kuo/opsmask/internal/store"
)

type Allocator struct {
	Secret []byte
	Store  store.Store
	mu     sync.Mutex
	cache  map[string]string
}

type Plan struct {
	Type, Index string
	Real        []byte
	HMACFull    []byte
}

func New(secret []byte, st store.Store) *Allocator {
	return &Allocator{Secret: secret, Store: st, cache: map[string]string{}}
}

func (a *Allocator) Plan(typ string, real []byte) Plan {
	full := FullHMAC(a.Secret, typ, real)
	return Plan{Type: typ, Index: hex.EncodeToString(full[:8]), HMACFull: full, Real: append([]byte(nil), real...)}
}

func (a *Allocator) CommitPlans(ctx context.Context, plans []Plan) error {
	rows := make([]store.Mapping, 0, len(plans))
	seen := map[string]bool{}
	now := time.Now()
	a.mu.Lock()
	for _, p := range plans {
		if p.Type == "" {
			continue
		}
		key := p.Type + "\x00" + string(p.Real)
		if a.cache[key] != "" || seen[key] {
			continue
		}
		seen[key] = true
		rows = append(rows, store.Mapping{Type: p.Type, HMACFull: p.HMACFull, Index: p.Index, RealValue: p.Real, FirstSeenAt: now})
	}
	a.mu.Unlock()
	if err := a.Store.InsertBatch(ctx, rows); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, p := range plans {
		if p.Type == "" {
			continue
		}
		a.cache[p.Type+"\x00"+string(p.Real)] = p.Index
	}
	return nil
}

func FullHMAC(secret []byte, typ string, real []byte) []byte {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(typ))
	m.Write([]byte{0})
	m.Write(real)
	return m.Sum(nil)
}
