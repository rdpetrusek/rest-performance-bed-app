package connection

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"rest-bed-app/internal/bed"
	"rest-bed-app/internal/discovery"
)

const (
	bedA = "aaaaaaaaaaaa" // the bed under test
	bedB = "bbbbbbbbbbbb" // a second bed that may inherit A's old address
)

// fakeBed is an httptest stand-in for a bed answering as id.
type fakeBed struct {
	srv       *httptest.Server
	id        string
	puts      atomic.Int32 // PUT /api/preferences/firmness requests received
	putStatus int
	hangPut   bool   // accept a PUT and never answer it
	gate      func() // if set, GET /api/device waits on it
}

func newFakeBed(t *testing.T, id string) *fakeBed {
	t.Helper()
	b := &fakeBed{id: id, putStatus: http.StatusNoContent}
	b.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/device":
			if b.gate != nil {
				b.gate()
			}
			fmt.Fprintf(w, `{"class":"ReST Bed","id":%q,"name":"ReST-%s"}`, b.id, b.id[:6])
		case r.Method == http.MethodPut && r.URL.Path == "/api/preferences/firmness":
			b.puts.Add(1)
			io.Copy(io.Discard, r.Body) // lets the server notice the client giving up
			if b.hangPut {
				<-r.Context().Done()
				return
			}
			w.WriteHeader(b.putStatus)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(b.srv.Close)
	return b
}

func (b *fakeBed) addr() string { return strings.TrimPrefix(b.srv.URL, "http://") }

func (b *fakeBed) dev() discovery.Device {
	return discovery.Device{ID: b.id, Name: "ReST-" + b.id[:6], Address: b.addr(), Class: "ReST Bed"}
}

// deadAddr returns an address nothing listens on (connection refused).
func deadAddr() string {
	srv := httptest.NewServer(http.NotFoundHandler())
	a := strings.TrimPrefix(srv.URL, "http://")
	srv.Close()
	return a
}

type memSettings struct {
	mu  sync.Mutex
	id  string
	err error
}

func (s *memSettings) PickedBed(context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.id, s.err
}

func (s *memSettings) SetPickedBed(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.id = id
	return nil
}

func (s *memSettings) saved() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.id
}

// clientTarget retargets the client the way the collector does.
type clientTarget struct {
	c        *bed.Client
	switches atomic.Int32
}

func (t *clientTarget) SwitchTarget(host string) {
	t.switches.Add(1)
	t.c.SetHost(host)
}

// rig wires a Manager to a real Scanner whose sweep result the test controls.
type rig struct {
	m        *Manager
	client   *bed.Client
	scanner  *discovery.Scanner
	target   *clientTarget
	settings *memSettings

	mu     sync.Mutex
	onLAN  []discovery.Device // what the next sweep finds
	sweeps atomic.Int32
}

func newRig(t *testing.T, saved string) *rig {
	t.Helper()
	r := &rig{client: bed.New(""), settings: &memSettings{id: saved}}
	r.target = &clientTarget{c: r.client}
	log := slog.New(slog.DiscardHandler)
	r.scanner = discovery.NewWithSweep(time.Hour, func(context.Context) ([]discovery.Device, error) {
		r.sweeps.Add(1)
		r.mu.Lock()
		defer r.mu.Unlock()
		return append([]discovery.Device(nil), r.onLAN...), nil
	}, log)
	r.m = New(Options{
		Client: r.client, Finder: r.scanner, Settings: r.settings, Target: r.target, Log: log,
		VerifyTimeout: time.Second, OperationTimeout: 10 * time.Second,
	})
	r.scanner.OnSweep(func(found []discovery.Device) { r.m.Observe(found) })
	if err := r.m.LoadSaved(context.Background()); err != nil {
		t.Fatal(err)
	}
	return r
}

func (r *rig) setLAN(ds ...discovery.Device) {
	r.mu.Lock()
	r.onLAN = ds
	r.mu.Unlock()
}

// sweep runs one scan, as the periodic scanner would.
func (r *rig) sweep(t *testing.T) {
	t.Helper()
	time.Sleep(2 * time.Millisecond) // a sweep is only shared with callers that asked no later than it began
	if _, err := r.scanner.Scan(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestStartupAlwaysScans(t *testing.T) {
	a, b := newFakeBed(t, bedA), newFakeBed(t, bedB)
	tests := []struct {
		name       string
		saved      string
		onLAN      []discovery.Device
		wantPicked string
		wantHost   string
	}{
		{"saved id is found by the scan", bedB, []discovery.Device{a.dev(), b.dev()}, bedB, b.addr()},
		{"saved id is not on the network: nothing else is substituted", bedB, []discovery.Device{a.dev()}, bedB, ""},
		{"nothing saved, exactly one bed: connect to it", "", []discovery.Device{a.dev()}, bedA, a.addr()},
		{"nothing saved, several beds: connect to none and wait for a pick", "", []discovery.Device{a.dev(), b.dev()}, "", ""},
		{"nothing saved, no bed: keep scanning", "", nil, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, tt.saved)
			if got := r.client.Host(); got != "" {
				t.Fatalf("host before the first scan = %q, want none", got)
			}
			r.setLAN(tt.onLAN...)
			r.sweep(t)
			if got := r.m.Picked(); got != tt.wantPicked {
				t.Errorf("picked = %q, want %q", got, tt.wantPicked)
			}
			if got := r.client.Host(); got != tt.wantHost {
				t.Errorf("host = %q, want %q", got, tt.wantHost)
			}
		})
	}
}

func TestStartupKeepsScanningUntilABedAppears(t *testing.T) {
	a := newFakeBed(t, bedA)
	r := newRig(t, "")
	r.sweep(t)
	if r.client.Host() != "" {
		t.Fatal("connected with nothing on the network")
	}
	r.setLAN(a.dev())
	r.sweep(t)
	if got := r.client.Host(); got != a.addr() {
		t.Errorf("host = %q, want %q", got, a.addr())
	}
}

func TestAutoPickedBedIsNotSaved(t *testing.T) {
	a := newFakeBed(t, bedA)
	r := newRig(t, "")
	r.setLAN(a.dev())
	r.sweep(t)
	if got := r.settings.saved(); got != "" {
		t.Errorf("saved id = %q: only an explicit pick may be remembered", got)
	}
}

func TestEverySweepFollowsThePickedBed(t *testing.T) {
	a, moved := newFakeBed(t, bedA), newFakeBed(t, bedA)
	r := newRig(t, bedA)
	r.setLAN(a.dev())
	r.sweep(t)
	if r.client.Host() != a.addr() {
		t.Fatalf("host = %q, want %q", r.client.Host(), a.addr())
	}
	r.setLAN(moved.dev()) // DHCP handed the same bed a new address
	r.sweep(t)
	if got := r.client.Host(); got != moved.addr() {
		t.Errorf("host after the move = %q, want %q", got, moved.addr())
	}
	switches := r.target.switches.Load()
	r.sweep(t) // unchanged: no needless reconnect
	if got := r.target.switches.Load(); got != switches {
		t.Errorf("switches went from %d to %d on an unchanged sweep", switches, got)
	}
}

func TestSweepMissingThePickedBedLeavesTheTargetAlone(t *testing.T) {
	a := newFakeBed(t, bedA)
	r := newRig(t, bedA)
	r.setLAN(a.dev())
	r.sweep(t)
	r.setLAN() // one lost sweep
	r.sweep(t)
	if got := r.client.Host(); got != a.addr() {
		t.Errorf("host = %q, want it kept at %q", got, a.addr())
	}
}

// staleAddresses are the two ways the address a bed used to have goes bad.
var staleAddresses = []struct {
	name  string
	stale func(other *fakeBed) string
}{
	{"old address refuses connections", func(*fakeBed) string { return deadAddr() }},
	{"old address now belongs to a different bed", func(other *fakeBed) string { return other.addr() }},
}

func TestCheckRescansWhenAddressIsStale(t *testing.T) {
	for _, tt := range staleAddresses {
		t.Run(tt.name, func(t *testing.T) {
			other, home := newFakeBed(t, bedB), newFakeBed(t, bedA)
			r := newRig(t, bedA)
			r.client.SetHost(tt.stale(other))
			r.setLAN(other.dev(), home.dev())

			r.m.check(context.Background())

			if got := r.client.Host(); got != home.addr() {
				t.Errorf("host = %q, want the picked bed at %q", got, home.addr())
			}
			if got := r.sweeps.Load(); got != 1 {
				t.Errorf("sweeps = %d, want 1", got)
			}
		})
	}
}

func TestCheckDoesNotScanWhileAddressIsRight(t *testing.T) {
	a := newFakeBed(t, bedA)
	r := newRig(t, bedA)
	r.client.SetHost(a.addr())
	r.m.check(context.Background())
	if got := r.sweeps.Load(); got != 0 {
		t.Errorf("sweeps = %d, want 0", got)
	}
}

func TestCommandFollowsABedThatMoved(t *testing.T) {
	for _, tt := range staleAddresses {
		t.Run(tt.name, func(t *testing.T) {
			other, home := newFakeBed(t, bedB), newFakeBed(t, bedA)
			r := newRig(t, bedA)
			r.client.SetHost(tt.stale(other))
			r.setLAN(other.dev(), home.dev())

			if err := r.m.SetFirmness(context.Background(), 28); err != nil {
				t.Fatalf("SetFirmness = %v, want it to find the bed and succeed", err)
			}
			if got := other.puts.Load(); got != 0 {
				t.Errorf("the wrong bed received %d commands, want 0", got)
			}
			if got := home.puts.Load(); got != 1 {
				t.Errorf("the picked bed received %d commands, want exactly 1", got)
			}
			if got := r.sweeps.Load(); got != 1 {
				t.Errorf("sweeps = %d, want 1", got)
			}
		})
	}
}

func TestWrongBedAtTheOldAddressGetsNoCommandWhenThePickedBedIsNotFound(t *testing.T) {
	other := newFakeBed(t, bedB)
	r := newRig(t, bedA)
	r.client.SetHost(other.addr())
	r.setLAN(other.dev()) // the picked bed is nowhere

	err := r.m.SetFirmness(context.Background(), 28)
	if !errors.Is(err, bed.ErrNoTarget) {
		t.Errorf("err = %v, want one wrapping ErrNoTarget", err)
	}
	if got := other.puts.Load(); got != 0 {
		t.Errorf("the wrong bed received %d commands, want 0", got)
	}
}

func TestCorrectBedAnsweringHTTPErrorIsNotRetried(t *testing.T) {
	a := newFakeBed(t, bedA)
	a.putStatus = http.StatusInternalServerError
	r := newRig(t, bedA)
	r.client.SetHost(a.addr())
	r.setLAN(a.dev())

	err := r.m.SetFirmness(context.Background(), 28)
	var se *bed.StatusError
	if !errors.As(err, &se) {
		t.Fatalf("err = %v, want a *bed.StatusError", err)
	}
	if got := a.puts.Load(); got != 1 {
		t.Errorf("PUTs = %d, want 1 (no retry)", got)
	}
	if got := r.sweeps.Load(); got != 0 {
		t.Errorf("sweeps = %d, want 0", got)
	}
}

func TestConcurrentFailuresShareOneScan(t *testing.T) {
	const n = 8
	other, home := newFakeBed(t, bedB), newFakeBed(t, bedA)
	// Hold every identity check at the old address until all n commands are
	// in, so none can start after the shared sweep has already begun.
	var all sync.WaitGroup
	all.Add(n)
	var arrived atomic.Int32
	other.gate = func() {
		if arrived.Add(1) <= n {
			all.Done()
		}
		all.Wait()
	}
	r := newRig(t, bedA)
	r.client.SetHost(other.addr())
	r.setLAN(other.dev(), home.dev())

	var wg sync.WaitGroup
	errs := make(chan error, n)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- r.m.SetFirmness(context.Background(), 28)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("command failed: %v", err)
		}
	}
	if got := r.sweeps.Load(); got != 1 {
		t.Errorf("sweeps = %d, want 1 shared by all %d commands", got, n)
	}
	if got := home.puts.Load(); got != n {
		t.Errorf("picked bed received %d commands, want %d", got, n)
	}
	if got := other.puts.Load(); got != 0 {
		t.Errorf("the wrong bed received %d commands, want 0", got)
	}
}

func TestAmbiguousTimeoutOnlyResendsIdempotentCommands(t *testing.T) {
	tests := []struct {
		name       string
		idempotent bool
		wantPUTs   int32
		wantSweeps int32
	}{
		{"idempotent: resent after a rescan", true, 2, 1},
		{"not idempotent: the bed may have acted, so never resent", false, 1, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newFakeBed(t, bedA)
			a.hangPut = true // accepts the command, never answers
			r := newRig(t, bedA)
			r.client.SetHost(a.addr())
			r.setLAN(a.dev())

			err := r.m.Do(context.Background(), tt.idempotent, func(ctx context.Context, c *bed.Client) error {
				ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
				defer cancel()
				return c.SetFirmness(ctx, 28)
			})
			if err == nil {
				t.Fatal("err = nil, want the timeout")
			}
			if got := a.puts.Load(); got != tt.wantPUTs {
				t.Errorf("PUTs = %d, want %d", got, tt.wantPUTs)
			}
			if got := r.sweeps.Load(); got != tt.wantSweeps {
				t.Errorf("sweeps = %d, want %d", got, tt.wantSweeps)
			}
		})
	}
}

func TestNonIdempotentCommandIsResentWhenNonDeliveryIsProven(t *testing.T) {
	gone, home := newFakeBed(t, bedA), newFakeBed(t, bedA)
	r := newRig(t, bedA)
	r.client.SetHost(gone.addr())
	r.setLAN(home.dev())

	first := true
	err := r.m.Do(context.Background(), false, func(ctx context.Context, c *bed.Client) error {
		if first { // the bed drops off between the identity check and the send
			first = false
			gone.srv.Close()
		}
		return c.SetFirmness(ctx, 28)
	})
	if err != nil {
		t.Fatalf("Do = %v, want a connection-refused retry to succeed", err)
	}
	if got := home.puts.Load(); got != 1 {
		t.Errorf("picked bed received %d commands, want 1", got)
	}
}

func TestNoBedPickedOrFound(t *testing.T) {
	tests := []struct {
		name       string
		saved      string
		wantSweeps int32
	}{
		{"nothing picked: refuse without dialling or scanning", "", 0},
		{"picked but not on the network: scan once, then refuse", bedA, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, tt.saved)
			err := r.m.SetFirmness(context.Background(), 28)
			if !errors.Is(err, bed.ErrNoTarget) {
				t.Errorf("err = %v, want one wrapping ErrNoTarget", err)
			}
			if got := r.sweeps.Load(); got != tt.wantSweeps {
				t.Errorf("sweeps = %d, want %d", got, tt.wantSweeps)
			}
		})
	}
}

func TestPick(t *testing.T) {
	a, b := newFakeBed(t, bedA), newFakeBed(t, bedB)
	r := newRig(t, "")
	r.setLAN(a.dev(), b.dev())
	r.sweep(t)

	if err := r.m.Pick(context.Background(), "nope"); !errors.Is(err, discovery.ErrUnknownBed) {
		t.Errorf("Pick(unknown) = %v, want ErrUnknownBed", err)
	}
	if r.settings.saved() != "" || r.client.Host() != "" {
		t.Error("an unknown pick must change nothing")
	}

	if err := r.m.Pick(context.Background(), bedB); err != nil {
		t.Fatal(err)
	}
	if got := r.settings.saved(); got != bedB {
		t.Errorf("saved id = %q, want the bed's id, never its address", got)
	}
	if got := r.client.Host(); got != b.addr() {
		t.Errorf("host = %q, want %q", got, b.addr())
	}
	if err := r.m.SetFirmness(context.Background(), 30); err != nil {
		t.Fatal(err)
	}
	if a.puts.Load() != 0 || b.puts.Load() != 1 {
		t.Errorf("PUTs a=%d b=%d, want 0 and 1", a.puts.Load(), b.puts.Load())
	}
}

func TestRetryable(t *testing.T) {
	dial := &net.OpError{Op: "dial", Err: errors.New("connection refused")}
	read := &net.OpError{Op: "read", Err: errors.New("connection reset by peer")}
	tests := []struct {
		name       string
		err        error
		idempotent bool
		want       bool
	}{
		{"identity check failed", &verifyError{errors.New("x")}, false, true},
		{"correct bed refused with an HTTP status", &bed.StatusError{Method: "PUT", Path: "/x", Status: "500"}, true, false},
		{"no target", fmt.Errorf("%w: none", bed.ErrNoTarget), true, false},
		{"connection never opened, not idempotent", fmt.Errorf("put: %w", dial), false, true},
		{"connection never opened, idempotent", fmt.Errorf("put: %w", dial), true, true},
		{"reset mid-request, not idempotent", fmt.Errorf("put: %w", read), false, false},
		{"reset mid-request, idempotent", fmt.Errorf("put: %w", read), true, true},
		{"timeout, not idempotent", context.DeadlineExceeded, false, false},
		{"timeout, idempotent", context.DeadlineExceeded, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := retryable(tt.err, tt.idempotent); got != tt.want {
				t.Errorf("retryable = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoadSavedError(t *testing.T) {
	r := newRig(t, "")
	r.settings.err = errors.New("db locked")
	if err := r.m.LoadSaved(context.Background()); err == nil {
		t.Error("LoadSaved = nil, want the settings error")
	}
}
