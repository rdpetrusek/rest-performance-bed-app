package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"rest-bed-app/internal/bed"
	"rest-bed-app/internal/discovery"
	"rest-bed-app/internal/store"
)

type fakeLive struct{ snapshot map[string]json.RawMessage }

func (f *fakeLive) Snapshot() map[string]json.RawMessage { return f.snapshot }

type fakeHistory struct {
	rows []store.Row
	err  error
}

func (f *fakeHistory) Query(_ context.Context, _ string, _, _ time.Time, _ int) ([]store.Row, error) {
	return f.rows, f.err
}

type fakeController struct {
	calls []string
	err   error
}

func (f *fakeController) SetMode(_ context.Context, mode string) error {
	f.calls = append(f.calls, "mode:"+mode)
	return f.err
}
func (f *fakeController) SetFirmness(_ context.Context, value int) error {
	f.calls = append(f.calls, "firmness")
	return f.err
}
func (f *fakeController) SetManualProfile(_ context.Context, zones [4]int) error {
	f.calls = append(f.calls, "manual-profile")
	return f.err
}
func (f *fakeController) StartFilling(_ context.Context) error {
	f.calls = append(f.calls, "fill")
	return f.err
}
func (f *fakeController) Stop(_ context.Context) error {
	f.calls = append(f.calls, "stop")
	return f.err
}

func (f *fakeController) SetConfiguration(_ context.Context, config string) error {
	f.calls = append(f.calls, "configuration:"+config)
	return f.err
}

type fakeDiscoverer struct{ devices []discovery.Device }

func (f *fakeDiscoverer) Snapshot() []discovery.Device { return f.devices }

type fakeSwitcher struct {
	pickedID string
	err      error
}

func (f *fakeSwitcher) Pick(_ context.Context, id string) error {
	f.pickedID = id
	return f.err
}

// newTestServer fills in a discard logger and empty fakes for whatever opts
// leaves out, so each test only sets the collaborators it actually exercises.
func newTestServer(t *testing.T, opts Options) *httptest.Server {
	t.Helper()
	if opts.Live == nil {
		opts.Live = &fakeLive{}
	}
	if opts.History == nil {
		opts.History = &fakeHistory{}
	}
	if opts.Controller == nil {
		opts.Controller = &fakeController{}
	}
	if opts.Discoverer == nil {
		opts.Discoverer = &fakeDiscoverer{}
	}
	if opts.Switcher == nil {
		opts.Switcher = &fakeSwitcher{}
	}
	opts.Log = slog.New(slog.DiscardHandler)
	srv := httptest.NewServer(New(opts).Handler())
	t.Cleanup(srv.Close)
	return srv
}

func TestHandleLive(t *testing.T) {
	live := &fakeLive{snapshot: map[string]json.RawMessage{"body": json.RawMessage(`{"present":true}`)}}
	srv := newTestServer(t, Options{Live: live})

	resp, err := http.Get(srv.URL + "/api/live")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got map[string]json.RawMessage
	json.NewDecoder(resp.Body).Decode(&got)
	if string(got["body"]) != `{"present":true}` {
		t.Errorf("got %v", got)
	}
}

func TestHandleHistoryRejectsUnknownEvent(t *testing.T) {
	srv := newTestServer(t, Options{})

	resp, err := http.Get(srv.URL + "/api/history?event=nonsense")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestHandleControlStop(t *testing.T) {
	ctrl := &fakeController{}
	srv := newTestServer(t, Options{Controller: ctrl})

	resp, err := http.Post(srv.URL+"/api/control/stop", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if len(ctrl.calls) != 1 || ctrl.calls[0] != "stop" {
		t.Errorf("calls = %v, want [stop]", ctrl.calls)
	}
}

func TestHandleControlManualProfileRejectsWrongZoneCount(t *testing.T) {
	srv := newTestServer(t, Options{})

	resp, err := http.Post(srv.URL+"/api/control/manual-profile", "application/json",
		strings.NewReader(`{"zones":[1,2,3]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestControlHonorsAirLimits(t *testing.T) {
	tests := []struct {
		name     string
		air      string // live "air" payload; "" means none cached yet
		path     string
		body     string
		wantCode int
	}{
		{"firmness in range", `{"minimum":5,"maximum":50}`, "/api/control/firmness", `{"value":28}`, http.StatusOK},
		{"firmness at max", `{"minimum":5,"maximum":50}`, "/api/control/firmness", `{"value":50}`, http.StatusOK},
		{"firmness above max", `{"minimum":5,"maximum":50}`, "/api/control/firmness", `{"value":92}`, http.StatusBadRequest},
		{"firmness below min", `{"minimum":5,"maximum":50}`, "/api/control/firmness", `{"value":4}`, http.StatusBadRequest},
		{"firmness uses bed limits", `{"minimum":10,"maximum":40}`, "/api/control/firmness", `{"value":45}`, http.StatusBadRequest},
		{"firmness default limits", "", "/api/control/firmness", `{"value":51}`, http.StatusBadRequest},
		{"zones in range", `{"minimum":5,"maximum":50}`, "/api/control/manual-profile", `{"zones":[10,10,10,10]}`, http.StatusOK},
		{"one zone below min", `{"minimum":5,"maximum":50}`, "/api/control/manual-profile", `{"zones":[1,10,10,10]}`, http.StatusBadRequest},
		{"one zone above max", `{"minimum":5,"maximum":50}`, "/api/control/manual-profile", `{"zones":[10,10,10,60]}`, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			live := &fakeLive{snapshot: map[string]json.RawMessage{}}
			if tt.air != "" {
				live.snapshot["air"] = json.RawMessage(tt.air)
			}
			ctrl := &fakeController{}
			srv := newTestServer(t, Options{Live: live, Controller: ctrl})

			resp, err := http.Post(srv.URL+tt.path, "application/json", strings.NewReader(tt.body))
			if err != nil {
				t.Fatal(err)
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode != tt.wantCode {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantCode)
			}
			if sent := len(ctrl.calls) > 0; sent != (tt.wantCode == http.StatusOK) {
				t.Errorf("controller calls = %v; a rejected value must never reach the bed", ctrl.calls)
			}
		})
	}
}

func TestHandleControlConfiguration(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantCode  int
		wantCalls []string
	}{
		{"pad bypass", `{"configuration":"publ5z00ha"}`, http.StatusOK, []string{"configuration:publ5z00ha"}},
		{"pad required", `{"configuration":"pubr5z65ha"}`, http.StatusOK, []string{"configuration:pubr5z65ha"}},
		{"unknown string", `{"configuration":"publ5z99ha"}`, http.StatusBadRequest, nil},
		{"empty", `{"configuration":""}`, http.StatusBadRequest, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := &fakeController{}
			srv := newTestServer(t, Options{Controller: ctrl})

			resp, err := http.Post(srv.URL+"/api/control/configuration", "application/json", strings.NewReader(tt.body))
			if err != nil {
				t.Fatal(err)
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode != tt.wantCode {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantCode)
			}
			if diff := cmp.Diff(tt.wantCalls, ctrl.calls); diff != "" {
				t.Errorf("controller calls (-want +got):\n%s", diff)
			}
		})
	}
}

func TestHandleControlModeAppliesBody(t *testing.T) {
	ctrl := &fakeController{}
	srv := newTestServer(t, Options{Controller: ctrl})

	resp, err := http.Post(srv.URL+"/api/control/mode", "application/json", strings.NewReader(`{"mode":"pressure"}`))
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if len(ctrl.calls) != 1 || ctrl.calls[0] != "mode:pressure" {
		t.Errorf("calls = %v, want [mode:pressure]", ctrl.calls)
	}
}

func TestHandleDevices(t *testing.T) {
	disc := &fakeDiscoverer{devices: []discovery.Device{
		{ID: "abc123", Name: "ReST-abc123", Address: "192.0.2.1", Class: "ReST Bed", Model: "mac"},
	}}
	srv := newTestServer(t, Options{Discoverer: disc})

	resp, err := http.Get(srv.URL + "/api/devices")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got []discovery.Device
	json.NewDecoder(resp.Body).Decode(&got)
	if len(got) != 1 || got[0].Address != "192.0.2.1" {
		t.Errorf("got %+v", got)
	}
}

func TestHandleSelectDevice(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		pickErr  error
		wantCode int
		wantID   string
	}{
		{"picks by id", `{"id":"d8c0a6ee0d07"}`, nil, http.StatusOK, "d8c0a6ee0d07"},
		{"empty id rejected", `{"id":""}`, nil, http.StatusBadRequest, ""},
		{"an address is not an id", `{"address":"192.0.2.5"}`, nil, http.StatusBadRequest, ""},
		{"id no scan has seen", `{"id":"ffff"}`, fmt.Errorf("%w: %q", discovery.ErrUnknownBed, "ffff"), http.StatusNotFound, "ffff"},
		{"saving the pick failed", `{"id":"abc"}`, errors.New("disk full"), http.StatusInternalServerError, "abc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sw := &fakeSwitcher{err: tt.pickErr}
			srv := newTestServer(t, Options{Switcher: sw})

			resp, err := http.Post(srv.URL+"/api/devices/select", "application/json", strings.NewReader(tt.body))
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tt.wantCode {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantCode)
			}
			if sw.pickedID != tt.wantID {
				t.Errorf("pickedID = %q, want %q", sw.pickedID, tt.wantID)
			}
		})
	}
}

func TestControlErrorStatus(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode int
	}{
		{"no bed connected is 503", fmt.Errorf("%w: no bed picked", bed.ErrNoTarget), http.StatusServiceUnavailable},
		{"any other failure is 502", errors.New("connection reset"), http.StatusBadGateway},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newTestServer(t, Options{Controller: &fakeController{err: tt.err}})
			resp, err := http.Post(srv.URL+"/api/control/stop", "application/json", nil)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tt.wantCode {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantCode)
			}
		})
	}
}
