// Command server is the ReST Bed dashboard's API: a self-hosted replacement
// for the dead vendor cloud. It connects directly to the bed's local HTTP API
// (see docs/protocol.md), stores every SSE event into a local SQLite
// database, and serves the JSON API the Angular app (ui/) calls for live
// state, history charts, basic controls, and an offline/deterministic "last
// sleep session" summary. No cloud, no
// account, no external AI calls.
//
// Usage:
//
//	server
//
// Configured entirely by environment variables (see below), to keep the
// container image a single static binary with no flags to template in
// compose.yaml.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	"rest-bed-app/internal/bed"
	"rest-bed-app/internal/collector"
	"rest-bed-app/internal/discovery"
	"rest-bed-app/internal/store"
	"rest-bed-app/internal/web"
)

// discoveryInterval and discoveryTimeout control how often the dashboard
// sweeps the network for beds, and how long each address gets to answer -
// see internal/discovery.
const (
	discoveryInterval = 15 * time.Second
	discoveryTimeout  = 800 * time.Millisecond
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("exiting", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	bedHost := env("BED_HOST", "10.0.0.1")
	dbPath := env("DB_PATH", "/data/restbed.db")
	addr := env("ADDR", ":8000")

	s, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer s.Close()

	bedClient := bed.New(bedHost)
	coll := collector.New(bedClient, s, log)
	scanner := discovery.New(discoveryInterval, discoveryTimeout, scanSubnets(bedHost), log)
	srv := web.New(web.Options{
		Addr: addr, Live: coll, History: s, Controller: bedClient,
		Discoverer: scanner, Switcher: coll, Log: log,
	})

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	log.Info("starting", "bed_host", bedHost, "db_path", dbPath, "addr", addr)
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error { return coll.Run(gctx) })
	g.Go(func() error { return scanner.Run(gctx) })
	g.Go(func() error { return srv.Run(gctx) })
	return g.Wait()
}

// scanSubnets is what discovery sweeps beyond this host's own interfaces:
// BED_HOST's /24 (a bed's neighbours are usually on the same LAN) plus any
// comma-separated /24 prefixes in SCAN_SUBNETS. Needed in Docker on Windows,
// where the container has no interface on the home LAN at all.
func scanSubnets(bedHost string) []string {
	var out []string
	if s := discovery.SubnetOf(bedHost); s != "" {
		out = append(out, s)
	}
	for _, s := range strings.Split(os.Getenv("SCAN_SUBNETS"), ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func env(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}
