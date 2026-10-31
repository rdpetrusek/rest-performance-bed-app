package wifiwatcher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

type fakeRadio struct {
	current     string
	scan        []string
	connectErrs map[string]error

	connects    []string
	disconnects int
}

func (f *fakeRadio) CurrentSSID(context.Context) (string, error) { return f.current, nil }
func (f *fakeRadio) Scan(context.Context) ([]string, error)      { return f.scan, nil }

func (f *fakeRadio) Connect(_ context.Context, ssid string) error {
	f.connects = append(f.connects, ssid)
	if err := f.connectErrs[ssid]; err != nil {
		return err
	}
	f.current = ssid
	return nil
}

func (f *fakeRadio) Disconnect(context.Context) error {
	f.disconnects++
	f.current = ""
	return nil
}

type fakeProvisioner struct {
	deviceErr  error
	setWifiErr error
	setModeErr error

	calls []string
}

func (f *fakeProvisioner) SetHost(host string) { f.calls = append(f.calls, "host:"+host) }

func (f *fakeProvisioner) Device(context.Context) (json.RawMessage, error) {
	f.calls = append(f.calls, "device")
	if f.deviceErr != nil {
		return nil, f.deviceErr
	}
	return json.RawMessage(`{"id":"abc"}`), nil
}

func (f *fakeProvisioner) SetWifi(_ context.Context, ssid, password string) error {
	f.calls = append(f.calls, fmt.Sprintf("setwifi:%s:%s", ssid, password))
	return f.setWifiErr
}

func (f *fakeProvisioner) SetWifiMode(_ context.Context, mode string) error {
	f.calls = append(f.calls, "mode:"+mode)
	return f.setModeErr
}

func noDelay(t *testing.T) {
	t.Helper()
	origJoin, origCred := joinSettle, credentialSettle
	joinSettle, credentialSettle = 0, 0
	t.Cleanup(func() { joinSettle, credentialSettle = origJoin, origCred })
}

func TestSweepProvisionsEachBedSSIDAndRestoresOriginal(t *testing.T) {
	noDelay(t)
	radio := &fakeRadio{current: "HomeWifi", scan: []string{"ReST-aaa", "SomeOtherNetwork", "ReST-bbb", "ReST-aaa"}}
	prov := &fakeProvisioner{}
	w := New(radio, prov, Options{HomeSSID: "HomeWifi", HomePassword: "secret"}, slog.New(slog.DiscardHandler))

	w.sweep(context.Background())

	wantConnects := []string{"ReST-aaa", "ReST-bbb", "HomeWifi"}
	if diff := cmp.Diff(wantConnects, radio.connects); diff != "" {
		t.Errorf("connects mismatch (-want +got):\n%s", diff)
	}
	if radio.disconnects != 2 {
		t.Errorf("disconnects = %d, want 2 (one per provisioned bed)", radio.disconnects)
	}

	wantCalls := []string{
		"host:" + bedGateway, "device", "setwifi:HomeWifi:secret", "mode:indirect",
		"host:" + bedGateway, "device", "setwifi:HomeWifi:secret", "mode:indirect",
	}
	if diff := cmp.Diff(wantCalls, prov.calls); diff != "" {
		t.Errorf("provisioner calls mismatch (-want +got):\n%s", diff)
	}
}

func TestSweepSkipsWhenNoBedSSIDsFound(t *testing.T) {
	noDelay(t)
	radio := &fakeRadio{current: "HomeWifi", scan: []string{"SomeOtherNetwork"}}
	prov := &fakeProvisioner{}
	w := New(radio, prov, Options{}, slog.New(slog.DiscardHandler))

	w.sweep(context.Background())

	if len(radio.connects) != 0 || radio.disconnects != 0 || len(prov.calls) != 0 {
		t.Errorf("expected no activity, got connects=%v disconnects=%d calls=%v", radio.connects, radio.disconnects, prov.calls)
	}
}

func TestSweepDisconnectsWhenOriginalWasEmpty(t *testing.T) {
	noDelay(t)
	radio := &fakeRadio{current: "", scan: []string{"ReST-aaa"}}
	prov := &fakeProvisioner{}
	w := New(radio, prov, Options{}, slog.New(slog.DiscardHandler))

	w.sweep(context.Background())

	// One Disconnect from provision()'s own defer, one more from restore().
	if radio.disconnects != 2 {
		t.Errorf("disconnects = %d, want 2", radio.disconnects)
	}
	if len(radio.connects) != 1 || radio.connects[0] != "ReST-aaa" {
		t.Errorf("connects = %v, want only the bed's own SSID", radio.connects)
	}
}

func TestProvisionStopsWhenBedUnreachableAfterJoining(t *testing.T) {
	noDelay(t)
	radio := &fakeRadio{scan: []string{"ReST-aaa"}}
	prov := &fakeProvisioner{deviceErr: errors.New("connection refused")}
	w := New(radio, prov, Options{}, slog.New(slog.DiscardHandler))

	w.sweep(context.Background())

	wantCalls := []string{"host:" + bedGateway, "device"}
	if diff := cmp.Diff(wantCalls, prov.calls); diff != "" {
		t.Errorf("provisioner calls mismatch (-want +got):\n%s", diff)
	}
	if radio.disconnects == 0 {
		t.Error("expected the bed's AP to be left even after a failed provision")
	}
}

func TestProvisionStopsWhenSetWifiFails(t *testing.T) {
	noDelay(t)
	radio := &fakeRadio{scan: []string{"ReST-aaa"}}
	prov := &fakeProvisioner{setWifiErr: errors.New("500")}
	w := New(radio, prov, Options{}, slog.New(slog.DiscardHandler))

	w.sweep(context.Background())

	for _, call := range prov.calls {
		if call == "mode:indirect" {
			t.Fatal("SetWifiMode should never be called after SetWifi fails")
		}
	}
}

func TestRunStopsOnContextCancel(t *testing.T) {
	noDelay(t)
	radio := &fakeRadio{scan: nil}
	prov := &fakeProvisioner{}
	w := New(radio, prov, Options{}, slog.New(slog.DiscardHandler))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx, time.Hour) }()
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run() = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx was cancelled")
	}
}

func TestDedupBedSSIDs(t *testing.T) {
	got := dedupBedSSIDs([]string{"ReST-a", "Home", "ReST-b", "ReST-a", "restricted-network"})
	want := []string{"ReST-a", "ReST-b"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("mismatch (-want +got):\n%s", diff)
	}
}
