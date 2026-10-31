package bed

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// Event is one decoded Server-Sent-Event from the bed's /api/sse stream.
type Event struct {
	Name string
	Data json.RawMessage
}

// StreamSSE connects to the bed's /api/sse stream, calls onConnect once the
// connection is established, then calls onEvent for every event the bed
// raises, in order, until ctx is done or the connection drops. It returns the
// error that ended the stream (nil only if ctx was done).
func (c *Client) StreamSSE(ctx context.Context, onConnect func(), onEvent func(Event)) error {
	base, err := c.base()
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/sse", nil)
	if err != nil {
		return err
	}
	// This connection is held open for as long as the bed keeps streaming, so
	// it must not share DefaultTimeout with ordinary request/response calls.
	hc := &http.Client{}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return &statusError{resp.Status}
	}
	onConnect()
	return scanSSE(resp.Body, onEvent)
}

type statusError struct{ status string }

func (e *statusError) Error() string { return "GET /sse: " + e.status }

// scanSSE parses the wire format directly: "event:<name>" then "data:<json>"
// lines, blank line between events. The bed's own encoder never wraps a
// single event across more than one data: line.
func scanSSE(r io.Reader, onEvent func(Event)) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var name string
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event:"):
			name = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:") && name != "":
			raw := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			onEvent(Event{Name: name, Data: json.RawMessage(raw)})
			name = ""
		}
	}
	return scanner.Err()
}
