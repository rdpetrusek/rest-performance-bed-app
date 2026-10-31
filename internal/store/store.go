// Package store is the dashboard's event log: every SSE event the collector
// reads from the bed, persisted to SQLite so history and session analysis can
// read it back. It is the only package that opens the database file.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

const schema = `
CREATE TABLE IF NOT EXISTS events (
	id     INTEGER PRIMARY KEY AUTOINCREMENT,
	ts_ms  INTEGER NOT NULL,
	event  TEXT NOT NULL,
	data   TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS events_by_event_ts ON events(event, ts_ms);
`

// Store is the event log.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed, including parent directories) the database
// at path.
func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("open %s: %w", path, err)
		}
	}
	q := url.Values{}
	for _, p := range []string{"journal_mode(WAL)", "busy_timeout(10000)", "synchronous(NORMAL)"} {
		q.Add("_pragma", p)
	}
	dsn := "file:" + strings.ReplaceAll(path, `\`, "/") + "?" + q.Encode()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if err := migrateLegacy(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate legacy events: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: db}, nil
}

// migrateLegacy converts a database written by the old Python dashboard (an
// events table keyed by an ISO-8601 text "ts" column instead of "ts_ms") into
// the current schema in place, keeping every row. It does nothing to a
// database that's new or already current.
func migrateLegacy(db *sql.DB) error {
	var legacy int
	err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('events') WHERE name = 'ts'`).Scan(&legacy)
	if err != nil || legacy == 0 {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, stmt := range []string{
		`ALTER TABLE events RENAME TO events_legacy`,
		`DROP INDEX IF EXISTS idx_events_event_ts`,
		schema,
		// julianday() parses the Python isoformat() strings, offset included.
		`INSERT INTO events (ts_ms, event, data)
			SELECT CAST(ROUND((julianday(ts) - 2440587.5) * 86400000) AS INTEGER), event, data
			FROM events_legacy WHERE julianday(ts) IS NOT NULL ORDER BY id`,
		`DROP TABLE events_legacy`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Row is one recorded event.
type Row struct {
	TS   time.Time
	Data json.RawMessage
}

// Record persists one event raised at ts.
func (s *Store) Record(ctx context.Context, event string, ts time.Time, data json.RawMessage) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO events (ts_ms, event, data) VALUES (?, ?, ?)`,
		ts.UnixMilli(), event, string(data))
	return err
}

// Query returns every row for event in [start, end], oldest first, capped at limit.
func (s *Store) Query(ctx context.Context, event string, start, end time.Time, limit int) ([]Row, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT ts_ms, data FROM events WHERE event = ? AND ts_ms BETWEEN ? AND ? ORDER BY ts_ms ASC LIMIT ?`,
		event, start.UnixMilli(), end.UnixMilli(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Row
	for rows.Next() {
		var ms int64
		var data string
		if err := rows.Scan(&ms, &data); err != nil {
			return nil, err
		}
		out = append(out, Row{TS: time.UnixMilli(ms).UTC(), Data: json.RawMessage(data)})
	}
	return out, rows.Err()
}
