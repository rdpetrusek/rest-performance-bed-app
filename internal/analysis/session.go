// Package analysis is offline, deterministic sleep-session analysis over the
// dashboard's own stored events. Pure arithmetic, no I/O and no network calls -
// nothing about sleep ever leaves this machine.
package analysis

import (
	"encoding/json"
	"time"

	"rest-bed-app/internal/store"
)

// Stats summarizes a series of samples.
type Stats struct {
	Min float64 `json:"min"`
	Max float64 `json:"max"`
	Avg float64 `json:"avg"`
}

func stats(values []float64) *Stats {
	if len(values) == 0 {
		return nil
	}
	s := Stats{Min: values[0], Max: values[0]}
	sum := 0.0
	for _, v := range values {
		if v < s.Min {
			s.Min = v
		}
		if v > s.Max {
			s.Max = v
		}
		sum += v
	}
	s.Avg = sum / float64(len(values))
	return &s
}

// Session summarizes one contiguous "present" sleep session.
type Session struct {
	Found           bool       `json:"found"`
	Reason          string     `json:"reason,omitempty"`
	Ongoing         bool       `json:"ongoing"`
	Start           *time.Time `json:"start,omitempty"`
	End             *time.Time `json:"end,omitempty"`
	DurationMinutes float64    `json:"duration_minutes"`
	SampleCount     int        `json:"sample_count"`
	MovementEvents  int        `json:"movement_events"`
	MovementRatio   float64    `json:"movement_ratio"`
	HeartRate       *Stats     `json:"heart_rate"`
	RespirationRate *Stats     `json:"respiration_rate"`
}

type bodyEvent struct {
	Present   bool    `json:"present"`
	Moving    bool    `json:"moving"`
	HeartRate float64 `json:"heartrate"`
	Breathing struct {
		Rate float64 `json:"rate"`
	} `json:"breathing"`
}

// LastSession finds the most recent contiguous "present" session from rows
// (raw body events, oldest first) and summarizes it. A brief presence
// flicker shorter than gap does not end a session.
func LastSession(rows []store.Row, now time.Time, gap time.Duration) Session {
	if len(rows) == 0 {
		return Session{Found: false, Reason: "no body events recorded in lookback window"}
	}

	type sample struct {
		ts   time.Time
		body bodyEvent
	}
	var sessions [][]sample
	var current []sample
	var prevTS time.Time
	for _, row := range rows {
		var b bodyEvent
		json.Unmarshal(row.Data, &b) // malformed rows decode as zero value and are skipped by Present
		if b.Present {
			if len(current) > 0 && row.TS.Sub(prevTS) > gap {
				sessions = append(sessions, current)
				current = nil
			}
			current = append(current, sample{ts: row.TS, body: b})
			prevTS = row.TS
		} else if len(current) > 0 {
			sessions = append(sessions, current)
			current = nil
		}
	}
	if len(current) > 0 {
		sessions = append(sessions, current)
	}
	if len(sessions) == 0 {
		return Session{Found: false, Reason: "no 'present' session in lookback window"}
	}

	session := sessions[len(sessions)-1]
	started := session[0].ts
	ended := session[len(session)-1].ts
	ongoing := session[len(session)-1].body.Present && now.Sub(ended) < gap

	var heartRates, respRates []float64
	movementCount := 0
	for _, s := range session {
		if s.body.HeartRate > 0 {
			heartRates = append(heartRates, s.body.HeartRate)
		}
		if s.body.Breathing.Rate > 0 {
			respRates = append(respRates, s.body.Breathing.Rate)
		}
		if s.body.Moving {
			movementCount++
		}
	}

	return Session{
		Found:           true,
		Ongoing:         ongoing,
		Start:           &started,
		End:             &ended,
		DurationMinutes: round1(ended.Sub(started).Minutes()),
		SampleCount:     len(session),
		MovementEvents:  movementCount,
		MovementRatio:   round3(float64(movementCount) / float64(len(session))),
		HeartRate:       stats(heartRates),
		RespirationRate: stats(respRates),
	}
}

func round1(v float64) float64 { return roundTo(v, 10) }
func round3(v float64) float64 { return roundTo(v, 1000) }
func roundTo(v, factor float64) float64 {
	if v < 0 {
		return -roundTo(-v, factor)
	}
	return float64(int64(v*factor+0.5)) / factor
}
