// Package connection keeps the dashboard pointed at the bed the user picked.
//
// A pick is a bed's id (its MAC), never an address: addresses are handed out
// by DHCP and change, and a second bed can inherit the first one's. So the
// Manager treats the address as a cache to be verified. It never assumes one:
// the picked id is restored from settings, found by a scan, and re-checked
// regularly; a command that fails because the address went stale triggers a
// rescan, a retarget and one automatic retry, and no command is ever sent to a
// device whose id has not just been checked against the picked one.
package connection

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"rest-bed-app/internal/bed"
	"rest-bed-app/internal/discovery"
)

// Finder is the network scanner. Scan is shared between concurrent callers
// (see discovery.Scanner.Scan); Snapshot is the list the picker shows.
type Finder interface {
	Scan(ctx context.Context, notBefore time.Time) ([]discovery.Device, error)
	Snapshot() []discovery.Device
}

// Settings persists the picked bed's id across restarts.
type Settings interface {
	PickedBed(ctx context.Context) (string, error)
	SetPickedBed(ctx context.Context, id string) error
}

// Target is what follows the bed: it retargets live state at an address.
// An empty address disconnects.
type Target interface {
	SwitchTarget(host string)
}

// Defaults for Options.
const (
	DefaultVerifyInterval   = 10 * time.Second
	DefaultVerifyTimeout    = 3 * time.Second
	DefaultOperationTimeout = 45 * time.Second
)

// Options configure a Manager. Zero durations take the defaults above.
type Options struct {
	Client   *bed.Client // the client commands are sent with; Target must retarget this same client
	Finder   Finder
	Settings Settings
	Target   Target
	Log      *slog.Logger

	VerifyInterval   time.Duration // how often the current address is re-checked
	VerifyTimeout    time.Duration // how long one identity check may take
	OperationTimeout time.Duration // bounds a whole command, rescan and retry included
}

// Manager owns which bed is picked and where it currently lives.
type Manager struct {
	client   *bed.Client
	finder   Finder
	settings Settings
	target   Target
	log      *slog.Logger

	verifyInterval, verifyTimeout, opTimeout time.Duration

	// mu guards picked and makes "check the id, then send" atomic with respect
	// to retargeting: commands hold it for reading across both steps, and
	// switching the target takes it for writing.
	mu     sync.RWMutex
	picked string

	kick chan struct{}
}

// New returns a manager with nothing picked and no target.
func New(o Options) *Manager {
	m := &Manager{
		client: o.Client, finder: o.Finder, settings: o.Settings, target: o.Target, log: o.Log,
		verifyInterval: orDefault(o.VerifyInterval, DefaultVerifyInterval),
		verifyTimeout:  orDefault(o.VerifyTimeout, DefaultVerifyTimeout),
		opTimeout:      orDefault(o.OperationTimeout, DefaultOperationTimeout),
		kick:           make(chan struct{}, 1),
	}
	return m
}

func orDefault(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return def
}

// LoadSaved restores the picked id from settings. Call it before the scanner
// starts: the first sweep then connects to the saved bed as soon as it is found.
func (m *Manager) LoadSaved(ctx context.Context) error {
	id, err := m.settings.PickedBed(ctx)
	if err != nil {
		return fmt.Errorf("load picked bed: %w", err)
	}
	m.mu.Lock()
	m.picked = id
	m.mu.Unlock()
	if id != "" {
		m.log.Info("restored picked bed; waiting for a scan to find it", "id", id)
	}
	return nil
}

// Picked returns the id of the picked bed, or "" if there is none.
func (m *Manager) Picked() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.picked
}

// Pick makes the bed with this id the one to talk to, remembers the choice,
// and points the target at wherever the scanner last saw it.
func (m *Manager) Pick(ctx context.Context, id string) error {
	var dev *discovery.Device
	for _, d := range m.finder.Snapshot() {
		if d.ID == id {
			dev = &d
			break
		}
	}
	if dev == nil {
		return fmt.Errorf("%w: %q", discovery.ErrUnknownBed, id)
	}
	if err := m.settings.SetPickedBed(ctx, id); err != nil {
		return fmt.Errorf("save picked bed: %w", err)
	}
	m.mu.Lock()
	m.picked = id
	if dev.Address != m.client.Host() {
		m.target.SwitchTarget(dev.Address)
	}
	m.mu.Unlock()
	m.log.Info("bed picked", "id", id, "name", dev.Name)
	// The listed address may be a few sweeps old: check it now rather than at the next tick.
	select {
	case m.kick <- struct{}{}:
	default:
	}
	return nil
}

// Observe takes the beds a sweep found and follows the picked one to its
// current address. With nothing picked, a lone bed is connected to (not saved:
// the user has not chosen it, and a second bed may simply have missed this
// sweep); several beds mean waiting for a pick. It reports whether the picked
// bed was among them. Register it as the scanner's OnSweep callback.
func (m *Manager) Observe(devs []discovery.Device) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.picked == "" {
		if len(devs) == 1 && devs[0].ID != "" {
			m.picked = devs[0].ID
			m.target.SwitchTarget(devs[0].Address)
			m.log.Info("only one bed on the network; connected", "id", devs[0].ID, "address", devs[0].Address)
		}
		return false
	}
	var at *discovery.Device
	for _, d := range devs {
		if d.ID != m.picked {
			continue
		}
		if d.Address == m.client.Host() {
			return true // still where we are connected
		}
		at = &d
	}
	if at == nil {
		return false
	}
	m.log.Info("picked bed is at a new address; following it", "id", at.ID, "name", at.Name)
	m.target.SwitchTarget(at.Address)
	return true
}

// Run re-checks, every VerifyInterval, that the current address still answers
// as the picked bed, and rescans when it does not. It returns when ctx is done.
func (m *Manager) Run(ctx context.Context) error {
	t := time.NewTicker(m.verifyInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		case <-m.kick:
		}
		m.check(ctx)
	}
}

// check verifies the current address and recovers if it is wrong.
func (m *Manager) check(ctx context.Context) {
	m.mu.RLock()
	picked := m.picked
	m.mu.RUnlock()
	if picked == "" {
		return
	}
	started := time.Now()
	if m.client.Host() != "" {
		vctx, cancel := context.WithTimeout(ctx, m.verifyTimeout)
		id, err := m.client.ID(vctx)
		cancel()
		if err == nil && id == picked {
			return
		}
		m.log.Warn("picked bed does not answer at its address; rescanning", "id", picked, "answered", id, "err", err)
	}
	// A sweep from the last interval is fresh enough here: nothing was sent.
	if err := m.recover(ctx, started.Add(-m.verifyInterval)); err != nil && ctx.Err() == nil {
		m.log.Warn("could not find the picked bed", "id", picked, "err", err)
	}
}

// recover rescans (sharing any sweep begun at or after since) and retargets at
// the picked bed's new address. The error wraps bed.ErrNoTarget if it was not found.
func (m *Manager) recover(ctx context.Context, since time.Time) error {
	devs, err := m.finder.Scan(ctx, since)
	if err != nil {
		return fmt.Errorf("rescan: %w", err)
	}
	if !m.Observe(devs) {
		return fmt.Errorf("%w: bed %s not found on the network", bed.ErrNoTarget, m.Picked())
	}
	return nil
}

// verifyError means the identity check before a command failed. Nothing was
// sent to the device, so retrying is always safe.
type verifyError struct{ err error }

func (e *verifyError) Error() string { return "verify bed: " + e.err.Error() }
func (e *verifyError) Unwrap() error { return e.err }

// attempt checks that the device at the current address is the picked bed and,
// only if so, runs op against it. Retargeting is excluded for the duration.
func (m *Manager) attempt(ctx context.Context, op func(context.Context, *bed.Client) error) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.picked == "" {
		return fmt.Errorf("%w: no bed picked", bed.ErrNoTarget)
	}
	vctx, cancel := context.WithTimeout(ctx, m.verifyTimeout)
	id, err := m.client.ID(vctx)
	cancel()
	if err != nil {
		return &verifyError{err}
	}
	if id != m.picked {
		return &verifyError{fmt.Errorf("address now answers as bed %s, not the picked %s", id, m.picked)}
	}
	return op(ctx, m.client)
}

// Do runs a command against the picked bed.
//
// Before each attempt the device at the current address is asked for its id,
// and the command is sent only if it is the picked bed. If that check fails
// the address is stale: Do rescans, retargets and tries once more. If the
// command itself fails, Do retries once after the same recovery only when
// resending is safe:
//   - the failure proves nothing was delivered (the connection could not be
//     opened), or
//   - the command is idempotent, i.e. an absolute set-point that is harmless to
//     apply twice, so even a timeout that may have been applied can be resent.
//
// A bed that answers with an HTTP error is the right bed refusing, so that is
// returned as is. The whole operation, rescan included, is bounded by the
// manager's OperationTimeout.
func (m *Manager) Do(ctx context.Context, idempotent bool, op func(context.Context, *bed.Client) error) error {
	ctx, cancel := context.WithTimeout(ctx, m.opTimeout)
	defer cancel()

	started := time.Now()
	err := m.attempt(ctx, op)
	if err == nil || ctx.Err() != nil || !retryable(err, idempotent) {
		return err
	}
	m.log.Warn("command failed; looking for the bed and retrying once", "err", err)
	if rerr := m.recover(ctx, started); rerr != nil {
		return fmt.Errorf("%w (first attempt: %v)", rerr, err)
	}
	return m.attempt(ctx, op)
}

// retryable reports whether a failed attempt may be repeated after a rescan.
func retryable(err error, idempotent bool) bool {
	var ve *verifyError
	var se *bed.StatusError
	switch {
	case errors.As(err, &ve):
		return true // nothing was sent
	case errors.Is(err, bed.ErrNoTarget), errors.As(err, &se):
		return false
	case provesNonDelivery(err):
		return true
	default:
		// A timeout, reset or EOF after the connection opened: the bed may
		// have acted on it.
		return idempotent
	}
}

// provesNonDelivery reports whether err is a failure to open the connection,
// before any byte of the request could have been sent.
func provesNonDelivery(err error) bool {
	var op *net.OpError
	return errors.As(err, &op) && op.Op == "dial"
}
