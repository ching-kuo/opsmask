package store

import (
	"context"
	"errors"
	"time"
)

var ErrTruncationCollision = errors.New("truncated pseudonym collision")

type Mapping struct {
	Type, Index string
	HMACFull    []byte
	RealValue   []byte
	FirstSeenAt time.Time
}

// Stats summarizes the rows currently stored. Total is the sum of ByType
// values, included as a convenience so callers do not have to re-aggregate.
type Stats struct {
	Total  int            `json:"total"`
	ByType map[string]int `json:"by_type"`
}

// TokenRow is the safe projection of a Mapping that the MCP mapping
// resource needs: token form (rendered from Type+Index by the caller),
// the byte length of the underlying real value, and the first-seen time.
// Plaintext (RealValue) and HMACFull never leave the store layer.
type TokenRow struct {
	Type        string
	Index       string
	RealLength  int
	FirstSeenAt time.Time
}

type Store interface {
	Lookup(ctx context.Context, typ, index string) ([]byte, bool, error)
	Insert(ctx context.Context, m Mapping) error
	InsertBatch(ctx context.Context, rows []Mapping) error
	List(ctx context.Context, typ string, limit int) ([]Mapping, error)
	// ListTokens returns only safe projection columns. Use this from any
	// surface that exposes mappings to an LLM agent — keeps plaintext off
	// the heap of any process that doesn't need it.
	ListTokens(ctx context.Context, typ string, limit int) ([]TokenRow, error)
	Prune(ctx context.Context, typ string, olderThan time.Duration) (int64, error)
	Stats(ctx context.Context) (Stats, error)
	Close() error
}
