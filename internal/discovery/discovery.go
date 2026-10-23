// Package discovery finds ReST Bed pump boxes on the local network: it scans
// periodically and keeps the latest list in memory, so the dashboard can show
// a picker instead of a fixed BED_HOST. See docs/protocol.md's Discovery
// section for where the scan approach (subnet sweep, GET /api/device, accept
// class "ReST Bed"/"SmartBed") comes from - the official app does the same
// thing as a fallback to mDNS.
package discovery

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Device is one bed found on the network.
type Device struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Address       string `json:"address"`
	Class         string `json:"class"`
	Model         string `json:"model"`
	Configuration string `json:"configuration"`
}

// Scanner periodically sweeps the local network for beds.
type Scanner struct {
	interval time.Duration
	timeout  time.Duration
	extra    []string
	log      *slog.Logger

	mu      sync.RWMutex
	devices []Device
}

// New returns a scanner that sweeps every interval, probing each candidate
// address with timeout. extra lists /24 prefixes ("192.168.1") to sweep on
// top of this host's own interfaces.
func New(interval, timeout time.Duration, extra []string, log *slog.Logger) *Scanner {
	return &Scanner{interval: interval, timeout: timeout, extra: extra, log: log}
}

// Snapshot returns the most recently found devices, oldest-seen-address first.
func (s *Scanner) Snapshot() []Device {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Device, len(s.devices))
	copy(out, s.devices)
	return out
}

// Run sweeps immediately, then every interval, until ctx is done.
func (s *Scanner) Run(ctx context.Context) error {
	s.sweep(ctx)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			s.sweep(ctx)
		}
	}
}

func (s *Scanner) sweep(ctx context.Context) {
	found, err := scanOnce(ctx, s.timeout, s.extra)
	if err != nil {
		s.log.Warn("device scan failed", "err", err)
		return
	}
	s.mu.Lock()
	s.devices = found
	s.mu.Unlock()
}
