package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rest-bed-app/internal/bed"
	"rest-bed-app/internal/store"
)

// sseServer serves one SSE event (named "device", carrying {"from": name})
// and then blocks until the client disconnects, simulating a bed that's
// still streaming.
func sseServer(name string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher := w.(http.Flusher)
		fmt.Fprintf(w, "event:device\ndata:{\"from\":\"%s\"}\n\n", name)
		flusher.Flush()
		<-r.Context().Done()
	}))
}

func hostOf(srv *httptest.Server) string {
	return strings.TrimPrefix(srv.URL, "http://")
}

func TestSwitchTargetReconnectsAndResetsCache(t *testing.T) {
	a := sseServer("A")
	defer a.Close()
	b := sseServer("B")
	defer b.Close()

	s, err := store.Open(filepath.Join(t.TempDir(), "restbed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	c := New(bed.New(hostOf(a)), s, slog.New(slog.DiscardHandler))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	waitFor(t, func() bool { return string(c.Snapshot()["device"]) == `{"from":"A"}` })

	c.SwitchTarget(hostOf(b))
	waitFor(t, func() bool { return string(c.Snapshot()["device"]) == `{"from":"B"}` })

	cancel()
	if err := <-done; err != nil {
		t.Errorf("Run() = %v, want nil after ctx cancel", err)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition never became true")
}

func TestHandleIgnoresTimeEvent(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "restbed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	c := New(bed.New("unused"), s, slog.New(slog.DiscardHandler))
	c.handle(bed.Event{Name: "time", Data: json.RawMessage(`{"t":1}`)})
	if _, ok := c.Snapshot()["time"]; ok {
		t.Error("time event should not be cached")
	}
}

func TestRunWithNoTargetDialsNothingUntilSwitched(t *testing.T) {
	a := sseServer("A")
	defer a.Close()

	s, err := store.Open(filepath.Join(t.TempDir(), "restbed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	c := New(bed.New(""), s, slog.New(slog.DiscardHandler))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	time.Sleep(100 * time.Millisecond)
	if got := len(c.Snapshot()); got != 0 {
		t.Fatalf("snapshot has %d events with no bed connected, want 0", got)
	}

	c.SwitchTarget(hostOf(a))
	waitFor(t, func() bool { return string(c.Snapshot()["device"]) == `{"from":"A"}` })

	c.SwitchTarget("") // the bed was lost: disconnect and clear
	waitFor(t, func() bool { return len(c.Snapshot()) == 0 })

	cancel()
	if err := <-done; err != nil {
		t.Errorf("Run() = %v, want nil after ctx cancel", err)
	}
}
