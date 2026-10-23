//go:build windows

// Package wifi implements wifiwatcher.Radio for the host OS - this file is
// the Windows implementation, built on `netsh wlan`. A Linux (nmcli)
// implementation for the real Pi deployment is not written yet - see
// AGENTS.md.
package wifi

import (
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// NetshRadio controls this Windows host's Wi-Fi adapter via `netsh wlan`.
type NetshRadio struct{}

// NewNetshRadio returns a Radio backed by the Windows `netsh wlan` CLI.
func NewNetshRadio() *NetshRadio { return &NetshRadio{} }

var ssidLineRE = regexp.MustCompile(`(?m)^SSID \d+ : (.*)$`)

// Scan lists SSIDs currently in range.
func (NetshRadio) Scan(ctx context.Context) ([]string, error) {
	out, err := run(ctx, "netsh", "wlan", "show", "networks")
	if err != nil {
		return nil, err
	}
	var ssids []string
	for _, m := range ssidLineRE.FindAllStringSubmatch(out, -1) {
		if name := strings.TrimSpace(m[1]); name != "" {
			ssids = append(ssids, name)
		}
	}
	return ssids, nil
}

var currentSSIDRE = regexp.MustCompile(`(?m)^\s*SSID\s*:\s*(.*)\r?$`)

// CurrentSSID returns the SSID this host is currently joined to, or "" if
// not connected to anything.
func (NetshRadio) CurrentSSID(ctx context.Context) (string, error) {
	out, err := run(ctx, "netsh", "wlan", "show", "interfaces")
	if err != nil {
		return "", err
	}
	m := currentSSIDRE.FindStringSubmatch(out)
	if m == nil {
		return "", nil
	}
	return strings.TrimSpace(m[1]), nil
}

// Connect joins ssid, which is almost always a bed's own open AP (no saved
// profile - netsh refuses to connect without one, so this adds a temporary
// open one) but is also used to rejoin whatever network the host was on
// before a sweep, which already has its own (possibly secured) saved
// profile. Only adds the temporary open profile when no profile - of any
// kind - already exists for ssid, so a real secured profile (e.g. the home
// Wi-Fi) is never touched.
func (NetshRadio) Connect(ctx context.Context, ssid string) error {
	if !hasProfile(ctx, ssid) {
		path, err := writeOpenProfile(ssid)
		if err != nil {
			return err
		}
		defer os.Remove(path)
		if _, err := run(ctx, "netsh", "wlan", "add", "profile", "filename="+path); err != nil {
			return err
		}
	}
	_, err := run(ctx, "netsh", "wlan", "connect", "name="+ssid, "ssid="+ssid)
	return err
}

// hasProfile reports whether a saved WLAN profile already exists for ssid.
func hasProfile(ctx context.Context, ssid string) bool {
	_, err := run(ctx, "netsh", "wlan", "show", "profiles", "name="+ssid)
	return err == nil
}

// Disconnect drops whatever network this host is currently joined to.
func (NetshRadio) Disconnect(ctx context.Context) error {
	_, err := run(ctx, "netsh", "wlan", "disconnect")
	return err
}

func run(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, out)
	}
	return string(out), nil
}

// wlanProfile is the minimal WLAN profile XML schema for an open network -
// see https://learn.microsoft.com/windows/win32/nativewifi/wlan-profileschema-elements.
type wlanProfile struct {
	XMLName        xml.Name `xml:"WLANProfile"`
	Xmlns          string   `xml:"xmlns,attr"`
	Name           string   `xml:"name"`
	SSIDConfig     ssidConfig
	ConnectionType string `xml:"connectionType"`
	ConnectionMode string `xml:"connectionMode"`
	MSM            msm
}

type ssidConfig struct {
	SSID struct {
		Name string `xml:"name"`
	} `xml:"SSID"`
}

type msm struct {
	Security security `xml:"security"`
}

type security struct {
	AuthEncryption authEncryption `xml:"authEncryption"`
}

type authEncryption struct {
	Authentication string `xml:"authentication"`
	Encryption     string `xml:"encryption"`
	UseOneX        bool   `xml:"useOneX"`
}

func writeOpenProfile(ssid string) (string, error) {
	p := wlanProfile{
		Xmlns: "http://www.microsoft.com/networking/WLAN/profile/v1",
		Name:  ssid, ConnectionType: "ESS", ConnectionMode: "manual",
	}
	p.SSIDConfig.SSID.Name = ssid
	p.MSM.Security.AuthEncryption = authEncryption{Authentication: "open", Encryption: "none", UseOneX: false}

	body, err := xml.MarshalIndent(p, "", "  ")
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp("", "restbed-wifi-*.xml")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.WriteString(xml.Header); err != nil {
		return "", err
	}
	if _, err := f.Write(body); err != nil {
		return "", err
	}
	return f.Name(), nil
}
