package store

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"time"

	"modernc.org/sqlite"
)

// SQLite primary result codes. The driver may return extended codes
// (e.g. SQLITE_BUSY_RECOVERY=261) whose low byte is the primary code.
const (
	sqliteBusy   = 5
	sqliteLocked = 6
)

//go:embed migrations/0001_initial.sql
var migration001 string

type SQLite struct {
	writer *sql.DB
	reader *sql.DB
}

func OpenSQLite(path string) (*SQLite, error) {
	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=temp_store(MEMORY)&_txlock=immediate"
	w, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	r, err := sql.Open("sqlite", dsn)
	if err != nil {
		_ = w.Close()
		return nil, err
	}
	w.SetMaxOpenConns(1)
	s := &SQLite{writer: w, reader: r}
	if err := s.migrate(context.Background()); err != nil {
		_ = s.Close()
		return nil, err
	}
	_ = os.Chmod(path, 0o600)
	return s, nil
}

func (s *SQLite) migrate(ctx context.Context) error {
	return retryExec(ctx, s.writer, migration001)
}

// execer is the subset of *sql.DB and *sql.Conn that retryExec needs.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// retryExec runs a single SQL statement under retryBusy, discarding the
// sql.Result. Used for statements where RowsAffected is not inspected.
func retryExec(ctx context.Context, x execer, query string) error {
	return retryBusy(ctx, func() error {
		_, err := x.ExecContext(ctx, query)
		return err
	})
}

func (s *SQLite) Close() error {
	if s.reader != nil {
		_ = s.reader.Close()
	}
	if s.writer != nil {
		return s.writer.Close()
	}
	return nil
}

func (s *SQLite) Lookup(ctx context.Context, typ, index string) ([]byte, bool, error) {
	row := s.reader.QueryRowContext(ctx, `SELECT real_value FROM mapping WHERE type=? AND index_truncated=?`, typ, index)
	var b []byte
	err := row.Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return b, true, nil
}

func (s *SQLite) Insert(ctx context.Context, m Mapping) error {
	return s.InsertBatch(ctx, []Mapping{m})
}

func (s *SQLite) InsertBatch(ctx context.Context, rows []Mapping) error {
	if len(rows) == 0 {
		return nil
	}
	conn, err := s.writer.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := retryExec(ctx, conn, `BEGIN IMMEDIATE`); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		}
	}()
	for _, m := range rows {
		var res sql.Result
		err := retryBusy(ctx, func() error {
			var execErr error
			res, execErr = conn.ExecContext(ctx, `INSERT OR IGNORE INTO mapping(type,hmac_full,index_truncated,real_value,first_seen_at) VALUES(?,?,?,?,?)`,
				m.Type, m.HMACFull, m.Index, m.RealValue, m.FirstSeenAt.Unix())
			return execErr
		})
		if err != nil {
			return err
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 1 {
			continue
		}
		var existing []byte
		err = retryBusy(ctx, func() error {
			return conn.QueryRowContext(ctx, `SELECT real_value FROM mapping WHERE type=? AND hmac_full=?`, m.Type, m.HMACFull).Scan(&existing)
		})
		if err == nil {
			continue
		}
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: type=%s index=%s", ErrTruncationCollision, m.Type, m.Index)
		}
		return err
	}
	if err := retryExec(ctx, conn, `COMMIT`); err != nil {
		return err
	}
	committed = true
	return nil
}

func retryBusy(ctx context.Context, fn func() error) error {
	var err error
	for i := 0; i < 100; i++ {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		err = fn()
		if err == nil || !isBusyOrLocked(err) {
			return err
		}
		timer := time.NewTimer(time.Duration(10+i*5) * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return err
}

func isBusyOrLocked(err error) bool {
	var se *sqlite.Error
	if !errors.As(err, &se) {
		return false
	}
	switch se.Code() & 0xFF {
	case sqliteBusy, sqliteLocked:
		return true
	}
	return false
}

func (s *SQLite) List(ctx context.Context, typ string, limit int) ([]Mapping, error) {
	if limit <= 0 {
		limit = 100
	}
	q := `SELECT type,hmac_full,index_truncated,real_value,first_seen_at FROM mapping`
	args := []any{}
	if typ != "" {
		q += ` WHERE type=?`
		args = append(args, typ)
	}
	q += ` ORDER BY first_seen_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.reader.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Mapping
	for rows.Next() {
		var m Mapping
		var ts int64
		if err := rows.Scan(&m.Type, &m.HMACFull, &m.Index, &m.RealValue, &ts); err != nil {
			return nil, err
		}
		m.FirstSeenAt = time.Unix(ts, 0)
		out = append(out, m)
	}
	return out, rows.Err()
}

// Stats returns row counts per type in a single read query. Used by the
// MCP `mapping_stats` tool and any future observability surface.
func (s *SQLite) Stats(ctx context.Context) (Stats, error) {
	rows, err := s.reader.QueryContext(ctx, `SELECT type, COUNT(*) FROM mapping GROUP BY type`)
	if err != nil {
		return Stats{}, err
	}
	defer rows.Close()
	out := Stats{ByType: map[string]int{}}
	for rows.Next() {
		var typ string
		var n int
		if err := rows.Scan(&typ, &n); err != nil {
			return Stats{}, err
		}
		out.ByType[typ] = n
		out.Total += n
	}
	return out, rows.Err()
}

// Prune deletes mapping rows older than the given duration. Callers must pass
// a positive duration; a non-positive value is rejected to avoid wiping the
// whole store by accident.
func (s *SQLite) Prune(ctx context.Context, typ string, olderThan time.Duration) (int64, error) {
	if olderThan <= 0 {
		return 0, fmt.Errorf("Prune: olderThan must be positive")
	}
	cut := time.Now().Add(-olderThan).Unix()
	q := `DELETE FROM mapping WHERE first_seen_at < ?`
	args := []any{cut}
	if typ != "" {
		q += ` AND type=?`
		args = append(args, typ)
	}
	res, err := s.writer.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
