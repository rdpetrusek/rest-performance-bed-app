package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"sort"
	"sync"
	"time"
)

// seedSubnet is the bed's own default Wi-Fi AP subnet (direct mode) - always
// probed even when this host's own interfaces don't happen to be on it yet.
const seedSubnet = "10.0.0"

// maxConcurrentProbes bounds how many /api/device requests are in flight at
// once, so a sweep doesn't open 254+ sockets in a single burst.
const maxConcurrentProbes = 64

// scanOnce probes every address in every candidate /24 and returns the beds
// that answered, sorted by address.
func scanOnce(ctx context.Context, timeout time.Duration, extra []string) ([]Device, error) {
	subnets, err := candidateSubnets(extra)
	if err != nil {
		return nil, err
	}

	var (
		mu    sync.Mutex
		found []Device
		wg    sync.WaitGroup
		sem   = make(chan struct{}, maxConcurrentProbes)
	)
	for _, subnet := range subnets {
		for n := 1; n <= 254; n++ {
			ip := fmt.Sprintf("%s.%d", subnet, n)
			wg.Add(1)
			sem <- struct{}{}
			go func(ip string) {
				defer wg.Done()
				defer func() { <-sem }()
				if d, ok := probe(ctx, ip, timeout); ok {
					mu.Lock()
					found = append(found, d)
					mu.Unlock()
				}
			}(ip)
		}
	}
	wg.Wait()

	sort.Slice(found, func(i, j int) bool {
		a, aErr := netip.ParseAddr(found[i].Address)
		b, bErr := netip.ParseAddr(found[j].Address)
		if aErr != nil || bErr != nil {
			return found[i].Address < found[j].Address
		}
		return a.Compare(b) < 0
	})
	return found, nil
}

// probePort is the bed's own fixed API port. A var, not a const, so a test
// can point probe() at an httptest server instead of real port 80.
var probePort = 80

// probe GETs ip's /api/device and reports whether it's a ReST Bed (or the
// "SmartBed" class some firmware builds use).
func probe(ctx context.Context, ip string, timeout time.Duration) (Device, bool) {
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	url := fmt.Sprintf("http://%s:%d/api/device", ip, probePort)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return Device{}, false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Device{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return Device{}, false
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Device{}, false
	}
	return parseDeviceResponse(body, ip)
}

// parseDeviceResponse decodes an /api/device response and reports whether it
// identifies a bed - pulled out of probe() so this classification logic is
// testable without a real network call.
func parseDeviceResponse(body []byte, ip string) (Device, bool) {
	var d Device
	if err := json.Unmarshal(body, &d); err != nil {
		return Device{}, false
	}
	if d.Class != "ReST Bed" && d.Class != "SmartBed" {
		return Device{}, false
	}
	if d.Address == "" {
		d.Address = ip
	}
	return d, true
}

// candidateSubnets returns the /24 prefixes ("a.b.c") worth probing: every
// IPv4 address this host currently has (covers both joining the bed's own AP
// directly and a home LAN with one or more beds in "indirect" mode), plus the
// bed's own default AP subnet as a seed even if not currently joined to it,
// plus extra - the subnets this host can reach but has no interface on, e.g.
// the home LAN as seen from inside a Docker container on Windows.
func candidateSubnets(extra []string) ([]string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	subnets := subnetsFromAddrs(addrs)
	for _, e := range extra {
		if !slices.Contains(subnets, e) {
			subnets = append(subnets, e)
		}
	}
	return subnets, nil
}

// SubnetOf returns the /24 prefix ("a.b.c") of an IPv4 address, or "" if
// host isn't one.
func SubnetOf(host string) string {
	ip, err := netip.ParseAddr(host)
	if err != nil || !ip.Is4() {
		return ""
	}
	b := ip.As4()
	return fmt.Sprintf("%d.%d.%d", b[0], b[1], b[2])
}

// subnetsFromAddrs is candidateSubnets' pure core: given a host's interface
// addresses, which /24 prefixes are worth probing. Split out so it's
// testable with crafted net.Addr values instead of this machine's real ones.
func subnetsFromAddrs(addrs []net.Addr) []string {
	seen := map[string]bool{seedSubnet: true}
	subnets := []string{seedSubnet}
	for _, a := range addrs {
		ipNet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip4 := ipNet.IP.To4()
		if ip4 == nil || ip4.IsLoopback() {
			continue
		}
		prefix := fmt.Sprintf("%d.%d.%d", ip4[0], ip4[1], ip4[2])
		if !seen[prefix] {
			seen[prefix] = true
			subnets = append(subnets, prefix)
		}
	}
	return subnets
}
