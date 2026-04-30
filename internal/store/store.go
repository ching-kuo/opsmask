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

type Store interface {
	Lookup(ctx context.Context, typ, index string) ([]byte, bool, error)
	Insert(ctx context.Context, m Mapping) error
	InsertBatch(ctx context.Context, rows []Mapping) error
	List(ctx context.Context, typ string, limit int) ([]Mapping, error)
	Prune(ctx context.Context, typ string, olderThan time.Duration) (int64, error)
	Stats(ctx context.Context) (Stats, error)
	Close() error
}
