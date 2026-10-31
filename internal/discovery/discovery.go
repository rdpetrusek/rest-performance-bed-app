// Package discovery finds ReST Bed pump boxes on the local network: it scans
// periodically and keeps the latest list in memory, so the dashboard can show
// a picker and follow a bed whose address changes. Nothing here knows an
// address in advance: the subnets swept come from this host's own interfaces
// plus the operator's SCAN_SUBNETS. See docs/protocol.md's Discovery
// section for where the scan approach (subnet sweep, GET /api/device, accept
// class "ReST Bed"/"SmartBed") comes from - the official app does the same
// thing as a fallback to mDNS.
package discovery

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sort"
	"sync"
	"time"
)

// ErrUnknownBed is the error for a bed id no scan has seen.
var ErrUnknownBed = errors.New("unknown bed")

// keepSweeps is how many consecutive sweeps a bed may miss before it is
// dropped from the list. One probe per host can be lost (the bed has been seen
// to miss a sweep it answered the one before), and a flickering picker is worse
// than a briefly stale one.
const keepSweeps = 3

// Device is one bed found on the network.
type Device struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Address       string `json:"address"`
	Class         string `json:"class"`
	Model         string `json:"model"`
	Configuration string `json:"configuration"`
}

// key is what identifies a bed across sweeps: its id, which survives an
// address change.
func (d Device) key() string {
	if d.ID != "" {
		return d.ID
	}
	return d.Address
}

type seen struct {
	dev    Device
	missed int
}

// Scanner periodically sweeps the local network for beds. Sweeps never
// overlap: Scan serializes them, and Run waits a full interval after one ends.
type Scanner struct {
	interval time.Duration
	log      *slog.Logger
	scan     func(context.Context) ([]Device, error) // one raw sweep; replaced in tests
	slot     chan struct{}                           // holds a token while a sweep runs

	mu        sync.RWMutex
	known     map[string]*seen
	lastStart time.Time // when the newest sweep began
	lastFound []Device  // what that sweep found, fresh only (no remembered beds)
	onSweep   func([]Device)
}

// New returns a scanner that sweeps every interval, probing each candidate
// address with timeout. extra lists /24 prefixes ("192.168.1") to sweep on
// top of this host's own interfaces.
func New(interval, timeout time.Duration, extra []string, log *slog.Logger) *Scanner {
	return NewWithSweep(interval, func(ctx context.Context) ([]Device, error) {
		return scanOnce(ctx, timeout, extra)
	}, log)
}

// NewWithSweep is New with the sweep itself supplied by the caller, so other
// packages' tests can drive a scanner without touching a network.
func NewWithSweep(interval time.Duration, sweep func(context.Context) ([]Device, error), log *slog.Logger) *Scanner {
	return &Scanner{
		interval: interval, log: log, scan: sweep,
		slot:  make(chan struct{}, 1),
		known: map[string]*seen{},
	}
}

// OnSweep registers fn to be called with the beds a sweep actually found
// (not the remembered ones), after every completed sweep. Call it before Run.
func (s *Scanner) OnSweep(fn func([]Device)) { s.onSweep = fn }

// Snapshot returns the beds currently listed, sorted by name then id. A bed
// that missed fewer than keepSweeps sweeps in a row is still listed, at its
// last known address.
func (s *Scanner) Snapshot() []Device {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Device, 0, len(s.known))
	for _, k := range s.known {
		out = append(out, k.dev)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Run sweeps immediately, then waits interval after each sweep ends, until
// ctx is done.
func (s *Scanner) Run(ctx context.Context) error {
	for {
		if _, err := s.Scan(ctx, time.Now()); err != nil && ctx.Err() == nil {
			s.log.Warn("device scan failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(s.interval):
		}
	}
}

// Scan sweeps the network and returns the beds found. Callers asking at the
// same time share one sweep: if a sweep that began at or after notBefore has
// already run (or is running, as the caller waits its turn), its result is
// returned instead of starting another. Pass the moment the caller noticed a
// problem so that stale results are never reused for it.
func (s *Scanner) Scan(ctx context.Context, notBefore time.Time) ([]Device, error) {
	select {
	case s.slot <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-s.slot }()

	s.mu.RLock()
	last, lastFound := s.lastStart, slices.Clone(s.lastFound)
	s.mu.RUnlock()
	if !last.IsZero() && !last.Before(notBefore) {
		return lastFound, nil
	}

	start := time.Now()
	found, err := s.scan(ctx)
	if err != nil {
		return nil, err
	}
	s.record(start, found)
	if s.onSweep != nil {
		s.onSweep(found)
	}
	return found, nil
}

// record folds one sweep into the remembered list.
func (s *Scanner) record(start time.Time, found []Device) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastStart = start
	s.lastFound = slices.Clone(found)
	hit := map[string]bool{}
	for _, d := range found {
		hit[d.key()] = true
		s.known[d.key()] = &seen{dev: d}
	}
	for k, v := range s.known {
		if hit[k] {
			continue
		}
		if v.missed++; v.missed >= keepSweeps {
			delete(s.known, k)
		}
	}
}
