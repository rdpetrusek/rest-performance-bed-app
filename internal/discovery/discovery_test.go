package discovery

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

// scripted returns a scanner whose sweeps return each batch in turn.
func scripted(batches ...[]Device) *Scanner {
	s := New(time.Hour, time.Second, nil, slog.New(slog.DiscardHandler))
	var n int
	s.scan = func(context.Context) ([]Device, error) {
		b := batches[min(n, len(batches)-1)]
		n++
		return b, nil
	}
	return s
}

func ids(ds []Device) []string {
	out := []string{}
	for _, d := range ds {
		out = append(out, d.ID+"@"+d.Address)
	}
	return out
}

func TestSnapshotKeepsBedThatMissesSweeps(t *testing.T) {
	a := Device{ID: "a", Name: "A", Address: "192.0.2.10"}
	b := Device{ID: "b", Name: "B", Address: "192.0.2.11"}
	moved := Device{ID: "a", Name: "A", Address: "192.0.2.99"}

	tests := []struct {
		name    string
		batches [][]Device
		want    []string
	}{
		{"one miss keeps the bed listed", [][]Device{{a, b}, {b}}, []string{"a@192.0.2.10", "b@192.0.2.11"}},
		{"two misses still keep it", [][]Device{{a, b}, {b}, {b}}, []string{"a@192.0.2.10", "b@192.0.2.11"}},
		{"keepSweeps misses in a row drop it", [][]Device{{a, b}, {b}, {b}, {b}}, []string{"b@192.0.2.11"}},
		{"a hit resets the count", [][]Device{{a, b}, {b}, {b}, {a, b}, {b}, {b}}, []string{"a@192.0.2.10", "b@192.0.2.11"}},
		{"a moved bed is listed once, at its new address", [][]Device{{a, b}, {moved, b}}, []string{"a@192.0.2.99", "b@192.0.2.11"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := scripted(tt.batches...)
			for i := range tt.batches {
				// Each call asks for a sweep begun after the previous one.
				if _, err := s.Scan(context.Background(), time.Now().Add(time.Duration(i)*time.Nanosecond)); err != nil {
					t.Fatal(err)
				}
				time.Sleep(time.Millisecond)
			}
			if diff := cmp.Diff(tt.want, ids(s.Snapshot())); diff != "" {
				t.Errorf("Snapshot (-want +got):\n%s", diff)
			}
		})
	}
}

func TestScanSharesOneSweepBetweenConcurrentCallers(t *testing.T) {
	s := New(time.Hour, time.Second, nil, slog.New(slog.DiscardHandler))
	var sweeps atomic.Int32
	release := make(chan struct{})
	s.scan = func(context.Context) ([]Device, error) {
		sweeps.Add(1)
		<-release
		return []Device{{ID: "a", Address: "192.0.2.10"}}, nil
	}

	noticed := time.Now() // every caller saw its failure before any sweep began
	var wg sync.WaitGroup
	results := make([][]Device, 8)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], _ = s.Scan(context.Background(), noticed)
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := sweeps.Load(); got != 1 {
		t.Errorf("sweeps = %d, want 1", got)
	}
	for i, r := range results {
		if len(r) != 1 || r[0].ID != "a" {
			t.Errorf("caller %d got %v, want the one sweep's result", i, r)
		}
	}
}

func TestScanNeverReusesASweepOlderThanTheCallersProblem(t *testing.T) {
	s := scripted([]Device{{ID: "a", Address: "192.0.2.10"}}, []Device{{ID: "a", Address: "192.0.2.99"}})
	if _, err := s.Scan(context.Background(), time.Time{}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	got, err := s.Scan(context.Background(), time.Now()) // problem noticed after the first sweep
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]string{"a@192.0.2.99"}, ids(got)); diff != "" {
		t.Errorf("fresh sweep expected (-want +got):\n%s", diff)
	}
}

func TestScanErrorIsReported(t *testing.T) {
	s := New(time.Hour, time.Second, nil, slog.New(slog.DiscardHandler))
	s.scan = func(context.Context) ([]Device, error) { return nil, errors.New("boom") }
	if _, err := s.Scan(context.Background(), time.Now()); err == nil {
		t.Error("Scan = nil error, want the sweep's error")
	}
}

func TestOnSweepGetsOnlyFreshFinds(t *testing.T) {
	a := Device{ID: "a", Address: "192.0.2.10"}
	s := scripted([]Device{a}, nil)
	var got [][]string
	s.OnSweep(func(ds []Device) { got = append(got, ids(ds)) })
	s.Scan(context.Background(), time.Now())
	time.Sleep(time.Millisecond)
	s.Scan(context.Background(), time.Now())
	if diff := cmp.Diff([][]string{{"a@192.0.2.10"}, {}}, got); diff != "" {
		t.Errorf("OnSweep (-want +got):\n%s", diff)
	}
}

// TestProbeRetriesAfterTimeout drives probe() against a loopback fake bed that
// ignores its first request for longer than the timeout.
func TestProbeRetriesAfterTimeout(t *testing.T) {
	tests := []struct {
		name      string
		stallFor  int // leading requests to stall
		wantOK    bool
		wantCalls int32
	}{
		{"answers first time", 0, true, 1},
		{"first probe lost, retry succeeds", 1, true, 2},
		{"silent through every attempt", 5, false, probeAttempts},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if int(calls.Add(1)) <= tt.stallFor {
					<-r.Context().Done()
					return
				}
				w.Write([]byte(`{"class":"ReST Bed","id":"a","name":"ReST-a"}`))
			}))
			defer srv.Close()
			host, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
			p, _ := strconv.Atoi(port)
			old := probePort
			probePort = p
			defer func() { probePort = old }()

			_, ok := probe(context.Background(), host, 100*time.Millisecond)
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if got := calls.Load(); got != tt.wantCalls {
				t.Errorf("requests = %d, want %d", got, tt.wantCalls)
			}
		})
	}
}
