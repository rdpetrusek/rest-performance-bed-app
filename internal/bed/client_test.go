package bed

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// requestLog records every request a test server saw, so tests can assert on
// method/path/body without hand-rolling a handler each time.
type requestLog struct {
	method string
	path   string
	body   string
}

func newLoggingServer(t *testing.T, status int) (*httptest.Server, *[]requestLog) {
	t.Helper()
	var log []requestLog
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		log = append(log, requestLog{method: r.Method, path: r.URL.Path, body: string(body)})
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv, &log
}

func hostOf(srv *httptest.Server) string {
	return strings.TrimPrefix(srv.URL, "http://")
}

func TestSetWifiSendsSSIDAndPassword(t *testing.T) {
	srv, log := newLoggingServer(t, http.StatusNoContent)
	c := New(hostOf(srv))

	if err := c.SetWifi(context.Background(), "TheInternet", "eliisacutiepie"); err != nil {
		t.Fatal(err)
	}
	if len(*log) != 1 {
		t.Fatalf("got %d requests, want 1", len(*log))
	}
	got := (*log)[0]
	if got.method != http.MethodPut || got.path != "/api/wifi" {
		t.Errorf("method/path = %s %s, want PUT /api/wifi", got.method, got.path)
	}
	if !strings.Contains(got.body, `"ssid":"TheInternet"`) || !strings.Contains(got.body, `"password":"eliisacutiepie"`) {
		t.Errorf("body = %s, want ssid and password fields", got.body)
	}
}

func TestSetWifiModeAcceptsOnlyDocumentedValues(t *testing.T) {
	srv, log := newLoggingServer(t, http.StatusNoContent)
	c := New(hostOf(srv))

	if err := c.SetWifiMode(context.Background(), WifiModeIndirect); err != nil {
		t.Fatal(err)
	}
	if got := (*log)[0].body; got != `"indirect"` {
		t.Errorf("body = %s, want \"indirect\"", got)
	}

	if err := c.SetWifiMode(context.Background(), "sideways"); err == nil {
		t.Error("SetWifiMode(\"sideways\") = nil error, want a rejection")
	}
}

func TestSetWifiPropagatesServerError(t *testing.T) {
	srv, _ := newLoggingServer(t, http.StatusInternalServerError)
	c := New(hostOf(srv))

	if err := c.SetWifi(context.Background(), "ssid", "pw"); err == nil {
		t.Error("SetWifi against a 500 response = nil error, want one")
	}
}

func TestSetHostRetargetsSubsequentCalls(t *testing.T) {
	a, logA := newLoggingServer(t, http.StatusNoContent)
	b, logB := newLoggingServer(t, http.StatusNoContent)
	c := New(hostOf(a))

	if err := c.SetFirmness(context.Background(), 30); err != nil {
		t.Fatal(err)
	}
	c.SetHost(hostOf(b))
	if err := c.SetFirmness(context.Background(), 40); err != nil {
		t.Fatal(err)
	}

	if len(*logA) != 1 || len(*logB) != 1 {
		t.Fatalf("logA=%d logB=%d, want 1 and 1", len(*logA), len(*logB))
	}
}
