package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestRecordAndQuery(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "restbed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	base := time.Date(2026, 10, 19, 22, 0, 0, 0, time.UTC)
	if err := s.Record(ctx, "body", base, json.RawMessage(`{"present":true}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.Record(ctx, "body", base.Add(time.Minute), json.RawMessage(`{"present":false}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.Record(ctx, "preferences", base, json.RawMessage(`{"firmness":28}`)); err != nil {
		t.Fatal(err)
	}

	got, err := s.Query(ctx, "body", base.Add(-time.Hour), base.Add(time.Hour), 100)
	if err != nil {
		t.Fatal(err)
	}
	want := []Row{
		{TS: base, Data: json.RawMessage(`{"present":true}`)},
		{TS: base.Add(time.Minute), Data: json.RawMessage(`{"present":false}`)},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("mismatch (-want +got):\n%s", diff)
	}
}

func TestQueryFiltersByEventAndRange(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "restbed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	base := time.Date(2026, 10, 19, 22, 0, 0, 0, time.UTC)
	s.Record(ctx, "body", base.Add(-2*time.Hour), json.RawMessage(`{"n":1}`))
	s.Record(ctx, "body", base, json.RawMessage(`{"n":2}`))
	s.Record(ctx, "surface", base, json.RawMessage(`{"n":3}`))

	got, err := s.Query(ctx, "body", base.Add(-time.Hour), base.Add(time.Hour), 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || string(got[0].Data) != `{"n":2}` {
		t.Errorf("got %+v, want only the in-range body row", got)
	}
}

func TestOpenMigratesLegacyPythonDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "restbed.db")

	// The old Python dashboard's schema and timestamp format, as found in the
	// deployed data volume.
	legacy, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE TABLE events (id INTEGER PRIMARY KEY AUTOINCREMENT, ts TEXT NOT NULL, event TEXT NOT NULL, data TEXT NOT NULL)`,
		`CREATE INDEX idx_events_event_ts ON events(event, ts)`,
		`INSERT INTO events (ts, event, data) VALUES ('2026-10-20T00:18:04.356977+00:00', 'temperature', '{"cpu":52.4}')`,
		`INSERT INTO events (ts, event, data) VALUES ('2026-10-20T00:18:10.101111+00:00', 'temperature', '{"cpu":52.3}')`,
	} {
		if _, err := legacy.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	legacy.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open() on a legacy database: %v", err)
	}
	defer s.Close()

	base := time.Date(2026, 10, 20, 0, 18, 0, 0, time.UTC)
	got, err := s.Query(ctx, "temperature", base, base.Add(time.Minute), 100)
	if err != nil {
		t.Fatal(err)
	}
	want := []Row{
		{TS: base.Add(4357 * time.Millisecond), Data: json.RawMessage(`{"cpu":52.4}`)},
		{TS: base.Add(10101 * time.Millisecond), Data: json.RawMessage(`{"cpu":52.3}`)},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("migrated rows mismatch (-want +got):\n%s", diff)
	}

	// New writes must work against the migrated table.
	if err := s.Record(ctx, "temperature", base.Add(30*time.Second), json.RawMessage(`{}`)); err != nil {
		t.Errorf("Record() after migration: %v", err)
	}
}

func TestQueryRespectsLimit(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "restbed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	base := time.Date(2026, 10, 19, 22, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		s.Record(ctx, "body", base.Add(time.Duration(i)*time.Second), json.RawMessage(`{}`))
	}
	got, err := s.Query(ctx, "body", base.Add(-time.Hour), base.Add(time.Hour), 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("len(got) = %d, want 2", len(got))
	}
}

func TestPickedBedSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "restbed.db")

	steps := []struct {
		name string
		set  string // "" means only read
		want string
	}{
		{"nothing picked yet", "", ""},
		{"first pick", "d8c0a6ee0d07", "d8c0a6ee0d07"},
		{"pick replaced", "aabbccddeeff", "aabbccddeeff"},
	}
	for _, st := range steps {
		s, err := Open(path) // reopened each step: the pick must be durable
		if err != nil {
			t.Fatal(err)
		}
		got, err := s.PickedBed(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if st.set != "" {
			if err := s.SetPickedBed(ctx, st.set); err != nil {
				t.Fatal(err)
			}
			if got, err = s.PickedBed(ctx); err != nil {
				t.Fatal(err)
			}
		}
		if got != st.want {
			t.Errorf("%s: PickedBed = %q, want %q", st.name, got, st.want)
		}
		s.Close()
	}
}
