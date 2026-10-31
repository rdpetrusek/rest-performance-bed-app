// Package collector keeps the dashboard in sync with one bed: it holds the
// SSE connection, keeps the latest payload per event name in memory for live
// reads, and persists every event (other than the periodic "time" tick) to
// the store. It owns the only in-memory cache the dashboard reads live state
// from.
package collector

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"rest-bed-app/internal/bed"
	"rest-bed-app/internal/store"
)

// minBackoff and maxBackoff bound the reconnect delay after a dropped stream.
const (
	minBackoff = 2 * time.Second
	maxBackoff = 60 * time.Second
)

// pollInterval is how often the collector re-reads the events the bed doesn't
// reliably push over SSE (see polledEvents).
const pollInterval = 5 * time.Second

// polledEvents are re-read directly on a timer, on top of SSE: the bed was
// seen not pushing a fresh "air" event after state changes (docs/protocol.md,
// "SSE staleness caveat"), "surface" is how the dashboard tells whether the
// sensor pad is connected, and "device" carries the configuration the
// dashboard can change (SSE only sends it once, on connect).
// Polled reads only refresh the live cache; they aren't persisted.
var polledEvents = []struct {
	name string
	read func(*bed.Client, context.Context) (json.RawMessage, error)
}{
	{"air", (*bed.Client).Air},
	{"surface", (*bed.Client).Surface},
	{"device", (*bed.Client).Device},
}

// Collector streams one bed's SSE events, caches the latest of each, and
// persists them. SwitchTarget retargets it at a different bed at runtime -
// see internal/web for how a user's dropdown pick reaches here.
type Collector struct {
	bed   *bed.Client
	store *store.Store
	log   *slog.Logger

	mu        sync.RWMutex
	latest    map[string]json.RawMessage
	cancelCur context.CancelFunc // cancels the in-flight StreamSSE call, nil if none
}

// New returns a collector for bedClient, persisting events to s.
func New(bedClient *bed.Client, s *store.Store, log *slog.Logger) *Collector {
	return &Collector{bed: bedClient, store: s, log: log, latest: map[string]json.RawMessage{}}
}

// Snapshot returns the latest payload seen for every event name.
func (c *Collector) Snapshot() map[string]json.RawMessage {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[string]json.RawMessage, len(c.latest))
	for k, v := range c.latest {
		out[k] = v
	}
	return out
}

// Run streams until ctx is done, reconnecting with exponential backoff after
// any dropped connection, and polls polledEvents alongside. It only returns
// when ctx is done.
func (c *Collector) Run(ctx context.Context) error {
	go c.poll(ctx)
	backoff := minBackoff
	for {
		connCtx, cancel := context.WithCancel(ctx)
		c.mu.Lock()
		c.cancelCur = cancel
		c.mu.Unlock()

		err := c.bed.StreamSSE(connCtx, func() {
			c.log.Info("sse connected", "host", c.bed.Host())
			backoff = minBackoff
		}, c.handle)
		cancel()
		if ctx.Err() != nil {
			return nil
		}
		if connCtx.Err() != nil {
			// Cancelled by SwitchTarget, not a dropped connection - reconnect
			// immediately against the new host, no backoff wait.
			continue
		}
		c.log.Warn("sse connection lost; reconnecting", "err", err, "in", backoff)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, maxBackoff)
	}
}

// SwitchTarget retargets the collector at a different bed: it points the
// client at host, drops any live state from the old bed, and interrupts the
// current connection so Run reconnects against host right away.
func (c *Collector) SwitchTarget(host string) {
	c.bed.SetHost(host)
	c.mu.Lock()
	c.latest = map[string]json.RawMessage{}
	cancel := c.cancelCur
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// poll refreshes polledEvents in the live cache every pollInterval until ctx
// is done. A failed read leaves the cached value alone; SSE reconnect logging
// already covers an unreachable bed.
func (c *Collector) poll(ctx context.Context) {
	t := time.NewTicker(pollInterval)
	defer t.Stop()
	for {
		for _, p := range polledEvents {
			rctx, cancel := context.WithTimeout(ctx, pollInterval)
			data, err := p.read(c.bed, rctx)
			cancel()
			if err != nil {
				continue
			}
			c.mu.Lock()
			c.latest[p.name] = data
			c.mu.Unlock()
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// timeEvent is the periodic clock tick the bed sends; it carries no state
// worth caching or persisting.
const timeEvent = "time"

func (c *Collector) handle(e bed.Event) {
	if e.Name == timeEvent {
		return
	}
	c.mu.Lock()
	c.latest[e.Name] = e.Data
	c.mu.Unlock()

	if err := c.store.Record(context.Background(), e.Name, time.Now().UTC(), e.Data); err != nil {
		c.log.Warn("record event failed", "event", e.Name, "err", err)
	}
}
