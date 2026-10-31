// Package bed is a thin client for the ReST Bed's local HTTP API.
//
// It only wraps endpoints actually confirmed either from the official app's JS
// or by testing against a live bed - see docs/protocol.md in the repo root for
// the full writeup. Endpoints that exist per the firmware's string table but
// whose request bodies were never exercised against real hardware are
// deliberately left out here rather than guessed at.
package bed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// DefaultTimeout bounds every request to the bed.
const DefaultTimeout = 8 * time.Second

// ErrNoTarget is what every call returns while the client has no bed to talk
// to (nothing picked yet, or the picked bed hasn't been found on the network).
var ErrNoTarget = errors.New("no bed connected")

// StatusError is a bed answering with an HTTP error status: the request was
// delivered and the bed refused it, so resending it elsewhere or again won't help.
type StatusError struct {
	Method, Path, Status string
}

func (e *StatusError) Error() string { return e.Method + " " + e.Path + ": " + e.Status }

// Client talks to one bed over its local HTTP API (no auth, per the firmware).
// The target host can change at runtime (SetHost), when the user picks a
// different bed from the discovered list - every method reads it fresh.
type Client struct {
	hc *http.Client

	mu   sync.RWMutex
	host string
}

// New returns a client for the bed at host (a bare address, optionally with a
// port). An empty host means no target yet: every call fails with ErrNoTarget.
func New(host string) *Client {
	return &Client{host: host, hc: &http.Client{Timeout: DefaultTimeout}}
}

// Host returns the bed host this client currently targets.
func (c *Client) Host() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.host
}

// SetHost retargets the client at a different bed ("" clears the target).
func (c *Client) SetHost(host string) {
	c.mu.Lock()
	c.host = host
	c.mu.Unlock()
}

func (c *Client) base() (string, error) {
	host := c.Host()
	if host == "" {
		return "", ErrNoTarget
	}
	return "http://" + host + "/api", nil
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	base, err := c.base()
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return &StatusError{http.MethodGet, path, resp.Status}
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) put(ctx context.Context, path, body string) error {
	base, err := c.base()
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, base+path, strings.NewReader(body))
	if err != nil {
		return err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return &StatusError{http.MethodPut, path, resp.Status}
	}
	return nil
}

// --- reads -----------------------------------------------------------------

// Device returns the bed's /api/device payload, raw (its shape isn't ours to
// own - the dashboard passes it straight through to the browser).
func (c *Client) Device(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.get(ctx, "/device", &out)
	return out, err
}

// ID returns the id the bed at the current target reports (its MAC, stable
// across address changes). The caller compares it with the bed it meant to talk
// to before sending anything: addresses get reassigned.
func (c *Client) ID(ctx context.Context) (string, error) {
	var d struct {
		ID string `json:"id"`
	}
	if err := c.get(ctx, "/device", &d); err != nil {
		return "", err
	}
	if d.ID == "" {
		return "", errors.New("device reported no id")
	}
	return d.ID, nil
}

// Values returns the bed's /api/values/<key> payload, raw.
func (c *Client) Values(ctx context.Context, key string) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.get(ctx, "/values/"+key, &out)
	return out, err
}

// Air returns the bed's /api/air payload (filling, targets, pressures, flows,
// minimum, maximum), raw.
func (c *Client) Air(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.get(ctx, "/air", &out)
	return out, err
}

// Surface returns the bed's /api/surface payload, raw. Its rows/columns are
// 0 while the sensor pad is disconnected - and the firmware won't run the
// pump or valves at all in that state (docs/protocol.md, "Root cause").
func (c *Client) Surface(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.get(ctx, "/surface", &out)
	return out, err
}

// FirmwareVersion returns the bed's /api/firmware/version payload, raw.
func (c *Client) FirmwareVersion(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.get(ctx, "/firmware/version", &out)
	return out, err
}

// --- writes (confirmed shapes only; this is physical hardware) -------------

// Mode values accepted by SetMode.
const (
	ModeManual   = "manual"
	ModePressure = "pressure"
	ModePosition = "position"
)

// SetMode sets the bed's operating mode.
func (c *Client) SetMode(ctx context.Context, mode string) error {
	switch mode {
	case ModeManual, ModePressure, ModePosition:
	default:
		return fmt.Errorf("bed: invalid mode %q", mode)
	}
	return c.put(ctx, "/preferences/mode", `"`+mode+`"`)
}

// SetFirmness sets the overall firmness preference.
func (c *Client) SetFirmness(ctx context.Context, value int) error {
	return c.put(ctx, "/preferences/firmness", fmt.Sprintf("%d", value))
}

// SetManualProfile sets the four manual-mode zone targets.
func (c *Client) SetManualProfile(ctx context.Context, zones [4]int) error {
	body := fmt.Sprintf("[%d,%d,%d,%d]", zones[0], zones[1], zones[2], zones[3])
	return c.put(ctx, "/preferences/manualprofile", body)
}

// PadBypass maps every configuration the P3 firmware recognizes to its
// counterpart with the sensor-pad requirement flipped: "z65"/"z49" configs
// read the pad every control cycle and skip all air control when it fails,
// while their "z00" twins never read it (firmware FUN_1f008284 maps these
// eight strings to bed types 0-7; types 4-7 bypass the pad - see
// docs/protocol.md). Configurations outside this table are refused, since
// the firmware treats any other string as unknown.
var PadBypass = map[string]string{
	"publ5z65ha": "publ5z00ha", "publ5z00ha": "publ5z65ha",
	"pubr5z65ha": "pubr5z00ha", "pubr5z00ha": "pubr5z65ha",
	"pibl3z49ha": "pibl3z00ha", "pibl3z00ha": "pibl3z49ha",
	"pibr3z49ha": "pibr3z00ha", "pibr3z00ha": "pibr3z49ha",
}

// SetConfiguration sets the bed's configuration string, which the firmware
// persists to flash. Only the strings in PadBypass are accepted.
func (c *Client) SetConfiguration(ctx context.Context, config string) error {
	if _, ok := PadBypass[config]; !ok {
		return fmt.Errorf("bed: unknown configuration %q", config)
	}
	return c.put(ctx, "/device/configuration", `"`+config+`"`)
}

// StartFilling starts the fill cycle (the app's "Fill" control).
func (c *Client) StartFilling(ctx context.Context) error {
	return c.put(ctx, "/air/filling", "true")
}

// Stop halts all zones (the app's "Stop" control).
//
// This also clears the filling status flag - confirmed by testing that
// PUT /air/flows alone halts the pump (flows go to "off", pressures stop
// moving) but leaves filling: true stuck in /api/air until /air/filling is
// explicitly set back to false.
func (c *Client) Stop(ctx context.Context) error {
	if err := c.put(ctx, "/air/flows", `["off","off","off","off"]`); err != nil {
		return err
	}
	return c.put(ctx, "/air/filling", "false")
}

// --- Wi-Fi provisioning (confirmed live once, as a supervised manual test on
// 2026-10-19 - see internal/wifiwatcher for the automated version built on
// top of these) ----------------------------------------------------------

// Wi-Fi modes accepted by SetWifiMode.
const (
	WifiModeDirect   = "direct"   // the bed broadcasts its own AP (ReST-<id>)
	WifiModeIndirect = "indirect" // the bed joins the network set by SetWifi
)

// SetWifi tells the bed which network to join when switched to indirect mode.
// Only takes effect once SetWifiMode(ctx, WifiModeIndirect) follows - the bed
// needs about 500ms between the two.
func (c *Client) SetWifi(ctx context.Context, ssid, password string) error {
	body, err := json.Marshal(struct {
		SSID     string `json:"ssid"`
		Password string `json:"password"`
	}{ssid, password})
	if err != nil {
		return err
	}
	return c.put(ctx, "/wifi", string(body))
}

// SetWifiMode switches the bed between broadcasting its own AP ("direct") and
// joining the network set by SetWifi ("indirect"). Switching to indirect mode
// makes the bed drop its own AP, so the caller's own connection to it (if
// that's how it's currently reachable) ends right after this call succeeds.
func (c *Client) SetWifiMode(ctx context.Context, mode string) error {
	switch mode {
	case WifiModeDirect, WifiModeIndirect:
	default:
		return fmt.Errorf("bed: invalid wifi mode %q", mode)
	}
	return c.put(ctx, "/wifi/mode", `"`+mode+`"`)
}
