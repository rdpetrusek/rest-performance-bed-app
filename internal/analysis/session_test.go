package analysis

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"rest-bed-app/internal/store"
)

func row(t *testing.T, tsOffset time.Duration, present, moving bool, hr, rr float64) store.Row {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"present":   present,
		"moving":    moving,
		"heartrate": hr,
		"breathing": map[string]any{"rate": rr},
	})
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 10, 19, 22, 0, 0, 0, time.UTC)
	return store.Row{TS: base.Add(tsOffset), Data: data}
}

func TestLastSession(t *testing.T) {
	now := time.Date(2026, 10, 19, 22, 10, 0, 0, time.UTC)
	gap := 10 * time.Minute

	t.Run("no rows", func(t *testing.T) {
		got := LastSession(nil, now, gap)
		want := Session{Found: false, Reason: "no body events recorded in lookback window"}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("never present", func(t *testing.T) {
		rows := []store.Row{row(t, 0, false, false, 0, 0), row(t, time.Minute, false, false, 0, 0)}
		got := LastSession(rows, now, gap)
		if got.Found {
			t.Errorf("Found = true, want false")
		}
	})

	t.Run("one contiguous session, stats and movement", func(t *testing.T) {
		rows := []store.Row{
			row(t, 0*time.Minute, true, false, 60, 14),
			row(t, 1*time.Minute, true, true, 64, 15),
			row(t, 2*time.Minute, true, false, 58, 13),
		}
		got := LastSession(rows, now, gap)
		if !got.Found {
			t.Fatalf("Found = false, want true")
		}
		if got.SampleCount != 3 {
			t.Errorf("SampleCount = %d, want 3", got.SampleCount)
		}
		if got.MovementEvents != 1 {
			t.Errorf("MovementEvents = %d, want 1", got.MovementEvents)
		}
		if got.HeartRate == nil || got.HeartRate.Min != 58 || got.HeartRate.Max != 64 {
			t.Errorf("HeartRate = %+v, want min 58 max 64", got.HeartRate)
		}
		if got.DurationMinutes != 2 {
			t.Errorf("DurationMinutes = %v, want 2", got.DurationMinutes)
		}
	})

	t.Run("missed sample within gap bridges without an explicit false row", func(t *testing.T) {
		// No row reports present:false in between - this is a dropped/missed
		// sample, not a sensor flicker. An explicit false row always closes a
		// session (matching the bed's own semantics); only a time gap between
		// consecutive present readings is bridged.
		rows := []store.Row{
			row(t, 0*time.Minute, true, false, 60, 14),
			row(t, 4*time.Minute, true, false, 62, 14),
		}
		got := LastSession(rows, now, gap)
		if !got.Found || got.SampleCount != 2 {
			t.Fatalf("got %+v, want one bridged session with 2 present samples", got)
		}
	})

	t.Run("explicit false row always closes the session, even within the gap window", func(t *testing.T) {
		rows := []store.Row{
			row(t, 0*time.Minute, true, false, 60, 14),
			row(t, 2*time.Minute, false, false, 0, 0),
			row(t, 4*time.Minute, true, false, 62, 14),
		}
		got := LastSession(rows, now, gap)
		if !got.Found || got.SampleCount != 1 {
			t.Fatalf("got %+v, want only the latest session (1 sample after the false row)", got)
		}
	})

	t.Run("gap longer than threshold splits into separate sessions, keeps the latest", func(t *testing.T) {
		rows := []store.Row{
			row(t, 0*time.Minute, true, false, 50, 12),
			row(t, 5*time.Minute, false, false, 0, 0),
			row(t, 30*time.Minute, true, false, 70, 16),
			row(t, 31*time.Minute, true, false, 71, 16),
		}
		got := LastSession(rows, now, gap)
		if !got.Found || got.SampleCount != 2 {
			t.Fatalf("got %+v, want the latest session only (2 samples)", got)
		}
		if got.HeartRate == nil || got.HeartRate.Min != 70 {
			t.Errorf("HeartRate = %+v, want the second session's values", got.HeartRate)
		}
	})

	t.Run("ongoing when still present near now", func(t *testing.T) {
		rows := []store.Row{row(t, 9*time.Minute, true, false, 60, 14)}
		got := LastSession(rows, now, gap)
		if !got.Ongoing {
			t.Errorf("Ongoing = false, want true (last sample within gap of now)")
		}
	})
}
