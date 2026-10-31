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
// compose.yaml. No bed address is configured anywhere: the bed is found by
// scanning (DB_PATH holds which one the user picked, by id), so a bed whose
// address changes is simply found again.
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
	"rest-bed-app/internal/connection"
	"rest-bed-app/internal/discovery"
	"rest-bed-app/internal/store"
	"rest-bed-app/internal/web"
)

// discoveryInterval and discoveryTimeout control how long the dashboard rests
// between sweeps of the network for beds (sweeps never overlap), and how long
// each address gets to answer - see internal/discovery. The bed answers in
// about half a second and has been seen to miss a probe at 800 ms.
const (
	discoveryInterval = 15 * time.Second
	discoveryTimeout  = 1500 * time.Millisecond
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("exiting", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	dbPath := env("DB_PATH", "/data/restbed.db")
	addr := env("ADDR", ":8000")

	s, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer s.Close()

	bedClient := bed.New("") // no target until a scan finds the picked bed
	coll := collector.New(bedClient, s, log)
	scanner := discovery.New(discoveryInterval, discoveryTimeout, scanSubnets(), log)
	conn := connection.New(connection.Options{
		Client: bedClient, Finder: scanner, Settings: s, Target: coll, Log: log,
	})
	scanner.OnSweep(func(found []discovery.Device) { conn.Observe(found) })
	srv := web.New(web.Options{
		Addr: addr, Live: coll, History: s, Controller: conn,
		Discoverer: scanner, Switcher: conn, Log: log,
	})

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := conn.LoadSaved(ctx); err != nil {
		return err
	}

	log.Info("starting", "db_path", dbPath, "addr", addr, "picked_bed", conn.Picked())
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error { return coll.Run(gctx) })
	g.Go(func() error { return conn.Run(gctx) })
	g.Go(func() error { return scanner.Run(gctx) })
	g.Go(func() error { return srv.Run(gctx) })
	return g.Wait()
}

// scanSubnets is what discovery sweeps beyond this host's own interfaces: the
// comma-separated /24 prefixes (e.g. "192.168.1") in SCAN_SUBNETS. Needed where
// the process has no interface on the LAN the bed is on, such as a container
// on the pod or Docker Desktop network.
func scanSubnets() []string {
	var out []string
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
