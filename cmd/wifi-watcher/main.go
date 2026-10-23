// Command wifi-watcher periodically scans for ReST Bed pump boxes still
// broadcasting their own direct-mode AP and provisions each one onto the
// home network, so internal/discovery (run by cmd/server) finds it on the
// LAN like any other device - automating the join-provision-leave sequence
// verified live, once, as a supervised manual test on 2026-10-19.
//
// Windows only for now (internal/wifi's netsh-based Radio) - see AGENTS.md
// for the plan for a Linux (nmcli) implementation for the real Pi
// deployment.
//
// Usage:
//
//	wifi-watcher
//
// Configured by environment variables: HOME_WIFI_SSID and HOME_WIFI_PASSWORD
// (required - the network to provision beds onto) and WATCH_INTERVAL
// (optional, Go duration syntax, default 5m).
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"rest-bed-app/internal/bed"
	"rest-bed-app/internal/wifi"
	"rest-bed-app/internal/wifiwatcher"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("exiting", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	ssid := os.Getenv("HOME_WIFI_SSID")
	password := os.Getenv("HOME_WIFI_PASSWORD")
	if ssid == "" || password == "" {
		return errors.New("HOME_WIFI_SSID and HOME_WIFI_PASSWORD must both be set")
	}
	interval := 5 * time.Minute
	if v := os.Getenv("WATCH_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return err
		}
		interval = d
	}

	w := wifiwatcher.New(wifi.NewNetshRadio(), bed.New(""), wifiwatcher.Options{
		HomeSSID: ssid, HomePassword: password,
	}, log)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	log.Info("starting", "home_ssid", ssid, "interval", interval)
	return w.Run(ctx, interval)
}
