// Package web is the dashboard's JSON API: live state, history,
// sleep-session analysis, and bed controls. The Angular app is served
// separately (ui/, behind nginx), which proxies /api here. It holds no state of its own -
// everything it answers comes from the Live/History/Controller it was built
// with.
package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"rest-bed-app/internal/analysis"
	"rest-bed-app/internal/bed"
	"rest-bed-app/internal/discovery"
	"rest-bed-app/internal/store"
)

// Live is the in-memory cache of the latest payload per SSE event name.
// Declared here (the consumer) rather than in the collector package that
// implements it.
type Live interface {
	Snapshot() map[string]json.RawMessage
}

// History answers range queries over stored events.
type History interface {
	Query(ctx context.Context, event string, start, end time.Time, limit int) ([]store.Row, error)
}

// Controller issues control commands to the bed. Declared here against the
// concrete *bed.Client passed in by main, per the one real seam this server
// needs (tests can fake it without a live bed).
type Controller interface {
	SetMode(ctx context.Context, mode string) error
	SetFirmness(ctx context.Context, value int) error
	SetManualProfile(ctx context.Context, zones [4]int) error
	StartFilling(ctx context.Context) error
	Stop(ctx context.Context) error
	SetConfiguration(ctx context.Context, config string) error
}

// Discoverer answers the latest sweep of beds found on the network.
type Discoverer interface {
	Snapshot() []discovery.Device
}

// Switcher retargets live state at a different bed.
type Switcher interface {
	SwitchTarget(host string)
}

// validEvents are the SSE event names /api/history may be asked for.
var validEvents = map[string]bool{
	"device": true, "preferences": true, "body": true,
	"surface": true, "temperature": true,
}

// Server is the dashboard's web server.
type Server struct {
	addr       string
	live       Live
	history    History
	controller Controller
	discoverer Discoverer
	switcher   Switcher
	log        *slog.Logger
}

// Options configure the server.
type Options struct {
	Addr       string // where to listen, e.g. ":8000"
	Live       Live
	History    History
	Controller Controller
	Discoverer Discoverer
	Switcher   Switcher
	Log        *slog.Logger
}

// New returns a server.
func New(o Options) *Server {
	return &Server{
		addr: o.Addr, live: o.Live, history: o.History, controller: o.Controller,
		discoverer: o.Discoverer, switcher: o.Switcher, log: o.Log,
	}
}

// Handler is the whole API.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/live", s.handleLive)
	mux.HandleFunc("GET /api/devices", s.handleDevices)
	mux.HandleFunc("POST /api/devices/select", s.handleSelectDevice)
	mux.HandleFunc("GET /api/history", s.handleHistory)
	mux.HandleFunc("GET /api/analysis/last-session", s.handleLastSession)
	mux.HandleFunc("POST /api/control/mode", s.handleControlMode)
	mux.HandleFunc("POST /api/control/firmness", s.handleControlFirmness)
	mux.HandleFunc("POST /api/control/manual-profile", s.handleControlManualProfile)
	mux.HandleFunc("POST /api/control/fill", s.handleControlFill)
	mux.HandleFunc("POST /api/control/stop", s.handleControlStop)
	mux.HandleFunc("POST /api/control/configuration", s.handleControlConfiguration)
	return mux
}

// Run serves until ctx is done.
func (s *Server) Run(ctx context.Context) error {
	hs := &http.Server{Addr: s.addr, Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		hs.Shutdown(sctx)
	}()
	s.log.Info("web serving", "addr", s.addr)
	if err := hs.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (s *Server) handleLive(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.live.Snapshot())
}

func (s *Server) handleDevices(w http.ResponseWriter, r *http.Request) {
	devices := s.discoverer.Snapshot()
	if devices == nil {
		devices = []discovery.Device{}
	}
	writeJSON(w, http.StatusOK, devices)
}

type selectDeviceBody struct {
	Address string `json:"address"`
}

func (s *Server) handleSelectDevice(w http.ResponseWriter, r *http.Request) {
	var body selectDeviceBody
	if !decodeBody(w, r, &body) {
		return
	}
	if body.Address == "" {
		writeError(w, http.StatusBadRequest, "address is required")
		return
	}
	s.switcher.SwitchTarget(body.Address)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type historyRow struct {
	TS   time.Time       `json:"ts"`
	Data json.RawMessage `json:"data"`
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	event := r.URL.Query().Get("event")
	if !validEvents[event] {
		writeError(w, http.StatusBadRequest, "event must be one of device, preferences, body, surface, temperature")
		return
	}
	hours := queryFloat(r, "hours", 24, 0.1, 24*30)
	limit := queryInt(r, "limit", 5000, 1, 20000)

	end := time.Now().UTC()
	start := end.Add(-time.Duration(hours * float64(time.Hour)))
	rows, err := s.history.Query(r.Context(), event, start, end, limit)
	if err != nil {
		s.log.Warn("history query failed", "event", event, "err", err)
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	out := make([]historyRow, len(rows))
	for i, row := range rows {
		out[i] = historyRow{TS: row.TS, Data: row.Data}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleLastSession(w http.ResponseWriter, r *http.Request) {
	lookbackHours := queryFloat(r, "lookback_hours", 36, 1, 24*14)
	gapMinutes := queryFloat(r, "gap_minutes", 10, 1, 120)

	end := time.Now().UTC()
	start := end.Add(-time.Duration(lookbackHours * float64(time.Hour)))
	rows, err := s.history.Query(r.Context(), "body", start, end, 50000)
	if err != nil {
		s.log.Warn("last-session query failed", "err", err)
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	session := analysis.LastSession(rows, end, time.Duration(gapMinutes*float64(time.Minute)))
	writeJSON(w, http.StatusOK, session)
}

type modeBody struct {
	Mode string `json:"mode"`
}

func (s *Server) handleControlMode(w http.ResponseWriter, r *http.Request) {
	var body modeBody
	if !decodeBody(w, r, &body) {
		return
	}
	s.runControl(w, r, func(ctx context.Context) error { return s.controller.SetMode(ctx, body.Mode) })
}

type firmnessBody struct {
	Value int `json:"value"`
}

func (s *Server) handleControlFirmness(w http.ResponseWriter, r *http.Request) {
	var body firmnessBody
	if !decodeBody(w, r, &body) {
		return
	}
	if lo, hi := s.airLimits(); body.Value < lo || body.Value > hi {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("firmness must be between %d and %d", lo, hi))
		return
	}
	s.runControl(w, r, func(ctx context.Context) error { return s.controller.SetFirmness(ctx, body.Value) })
}

type profileBody struct {
	Zones []int `json:"zones"`
}

func (s *Server) handleControlManualProfile(w http.ResponseWriter, r *http.Request) {
	var body profileBody
	if !decodeBody(w, r, &body) {
		return
	}
	if len(body.Zones) != 4 {
		writeError(w, http.StatusBadRequest, "zones must have exactly 4 values")
		return
	}
	lo, hi := s.airLimits()
	for _, z := range body.Zones {
		if z < lo || z > hi {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("zones must each be between %d and %d", lo, hi))
			return
		}
	}
	zones := [4]int{body.Zones[0], body.Zones[1], body.Zones[2], body.Zones[3]}
	s.runControl(w, r, func(ctx context.Context) error { return s.controller.SetManualProfile(ctx, zones) })
}

func (s *Server) handleControlFill(w http.ResponseWriter, r *http.Request) {
	s.runControl(w, r, s.controller.StartFilling)
}

func (s *Server) handleControlStop(w http.ResponseWriter, r *http.Request) {
	s.runControl(w, r, s.controller.Stop)
}

type configurationBody struct {
	Configuration string `json:"configuration"`
}

func (s *Server) handleControlConfiguration(w http.ResponseWriter, r *http.Request) {
	var body configurationBody
	if !decodeBody(w, r, &body) {
		return
	}
	if _, ok := bed.PadBypass[body.Configuration]; !ok {
		writeError(w, http.StatusBadRequest, "unknown configuration "+body.Configuration)
		return
	}
	s.runControl(w, r, func(ctx context.Context) error { return s.controller.SetConfiguration(ctx, body.Configuration) })
}

// defaultAirMin and defaultAirMax are the pressure limits every bed seen so
// far reports in /api/air, used until the live cache has an air event.
const (
	defaultAirMin = 5
	defaultAirMax = 50
)

// airLimits returns the bed's own pressure limits (/api/air minimum and
// maximum), which firmness and manual-profile zones must stay within. The
// bed itself accepts out-of-range writes without complaint (firmness 92 and
// zones of 1 were both stored as-is), so the dashboard has to enforce them.
func (s *Server) airLimits() (lo, hi int) {
	var air struct {
		Minimum *int `json:"minimum"`
		Maximum *int `json:"maximum"`
	}
	lo, hi = defaultAirMin, defaultAirMax
	raw, ok := s.live.Snapshot()["air"]
	if !ok || json.Unmarshal(raw, &air) != nil {
		return lo, hi
	}
	if air.Minimum != nil && air.Maximum != nil && *air.Minimum <= *air.Maximum {
		lo, hi = *air.Minimum, *air.Maximum
	}
	return lo, hi
}

func (s *Server) runControl(w http.ResponseWriter, r *http.Request, fn func(ctx context.Context) error) {
	if err := fn(r.Context()); err != nil {
		s.log.Warn("control command failed", "err", err)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if r.Body == nil {
		writeError(w, http.StatusBadRequest, "missing request body")
		return false
	}
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return false
	}
	return true
}
