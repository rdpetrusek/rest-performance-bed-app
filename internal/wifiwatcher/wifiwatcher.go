// Package wifiwatcher finds beds still broadcasting their own direct-mode AP
// (SSID "ReST-<id>") and provisions each one onto the home network, so it
// rejoins the LAN internal/discovery already scans. This automates the
// join-then-configure sequence verified live, once, as a supervised manual
// test on 2026-10-19 (see internal/bed's SetWifi/SetWifiMode).
//
// A bed only ever broadcasts its own AP while it has NOT successfully joined
// a home network - the two are mutually exclusive on this hardware (see
// docs/hardware.md). So every "ReST-<id>" SSID a scan turns up is, by
// definition, a bed that needs fixing; there's no separate "is it already
// fine" check to make.
package wifiwatcher

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"
)

// SSIDPrefix identifies a bed's own direct-mode AP SSID.
const SSIDPrefix = "ReST-"

// bedGateway is every bed's own fixed address while broadcasting its AP.
const bedGateway = "10.0.0.1"

// indirectMode is the wire value for bed.WifiModeIndirect - kept as a local
// literal instead of importing internal/bed, so this package depends only on
// the Provisioner interface below, not the concrete client.
const indirectMode = "indirect"

// joinSettle and credentialSettle are package vars (not consts) so tests can
// shrink them instead of a multi-second real sleep per sweep.
var (
	joinSettle       = 2 * time.Second
	credentialSettle = 500 * time.Millisecond
)

// Radio is this host's own Wi-Fi adapter: list what's in range, see what
// it's joined to, and join/leave networks. Declared here (the consumer);
// internal/wifi has the OS-specific implementations.
type Radio interface {
	Scan(ctx context.Context) ([]string, error)
	CurrentSSID(ctx context.Context) (string, error)
	Connect(ctx context.Context, ssid string) error
	Disconnect(ctx context.Context) error
}

// Provisioner issues the configure-then-switch sequence against whatever bed
// is reachable at its own AP gateway. Declared here against *bed.Client's
// existing shape (the one real seam this package needs, for fakes in tests).
type Provisioner interface {
	SetHost(host string)
	Device(ctx context.Context) (json.RawMessage, error)
	SetWifi(ctx context.Context, ssid, password string) error
	SetWifiMode(ctx context.Context, mode string) error
}

// Options configure a Watcher.
type Options struct {
	HomeSSID     string
	HomePassword string
}

// Watcher periodically scans for beds broadcasting their own AP and
// provisions each one onto the home network.
type Watcher struct {
	radio Radio
	bed   Provisioner
	opts  Options
	log   *slog.Logger
}

// New returns a watcher. bed is retargeted (SetHost) on every provisioning
// attempt - callers should not share it with anything reading live state.
func New(radio Radio, bedClient Provisioner, opts Options, log *slog.Logger) *Watcher {
	return &Watcher{radio: radio, bed: bedClient, opts: opts, log: log}
}

// Run sweeps immediately, then every interval, until ctx is done.
func (w *Watcher) Run(ctx context.Context, interval time.Duration) error {
	w.sweep(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			w.sweep(ctx)
		}
	}
}

func (w *Watcher) sweep(ctx context.Context) {
	original, err := w.radio.CurrentSSID(ctx)
	if err != nil {
		w.log.Warn("could not read current wifi state; skipping sweep", "err", err)
		return
	}
	ssids, err := w.radio.Scan(ctx)
	if err != nil {
		w.log.Warn("wifi scan failed", "err", err)
		return
	}
	bedSSIDs := dedupBedSSIDs(ssids)
	if len(bedSSIDs) == 0 {
		return
	}
	for _, ssid := range bedSSIDs {
		if ctx.Err() != nil {
			return
		}
		w.provision(ctx, ssid)
	}
	w.restore(ctx, original)
}

// dedupBedSSIDs returns the distinct ReST-* SSIDs from a scan.
func dedupBedSSIDs(ssids []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range ssids {
		if !strings.HasPrefix(s, SSIDPrefix) || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func (w *Watcher) provision(ctx context.Context, ssid string) {
	log := w.log.With("ssid", ssid)
	if err := w.radio.Connect(ctx, ssid); err != nil {
		log.Warn("could not join the bed's AP", "err", err)
		return
	}
	defer w.radio.Disconnect(ctx)

	if !sleep(ctx, joinSettle) {
		return
	}

	w.bed.SetHost(bedGateway)
	device, err := w.bed.Device(ctx)
	if err != nil {
		log.Warn("bed unreachable after joining its AP", "err", err)
		return
	}
	log = log.With("device", string(device))

	if err := w.bed.SetWifi(ctx, w.opts.HomeSSID, w.opts.HomePassword); err != nil {
		log.Error("setting home wifi credentials failed", "err", err)
		return
	}
	if !sleep(ctx, credentialSettle) {
		return
	}
	if err := w.bed.SetWifiMode(ctx, indirectMode); err != nil {
		log.Error("switching to indirect mode failed", "err", err)
		return
	}
	log.Info("provisioned into indirect mode")
}

// restore rejoins whatever network the host was on before this sweep (or
// just disconnects, if it was on nothing) - never leaves the host sitting on
// a bed's AP after a sweep finishes.
func (w *Watcher) restore(ctx context.Context, original string) {
	if original == "" {
		w.radio.Disconnect(ctx)
		return
	}
	if err := w.radio.Connect(ctx, original); err != nil {
		w.log.Warn("could not rejoin the original network", "ssid", original, "err", err)
	}
}

// sleep waits for d or ctx being done, whichever comes first, and reports
// whether it was the wait that finished (false means the caller should bail).
func sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}
