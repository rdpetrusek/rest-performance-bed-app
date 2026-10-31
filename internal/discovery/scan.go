package discovery

import (
	"context"
	"encoding/json"
	"errors"
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

// maxConcurrentProbes bounds how many /api/device requests are in flight at
// once, so a sweep doesn't open 254+ sockets in a single burst. A /24 of
// silent hosts then costs about two probe timeouts per attempt.
const maxConcurrentProbes = 128

// probeAttempts is how many times a silent address is tried before it is
// written off. A bed that answers in half a second has still been seen to miss
// a single probe, so a timeout earns one more try. Refusals and other fast
// failures are not retried.
const probeAttempts = 2

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
// "SmartBed" class some firmware builds use), retrying once if it just timed out.
func probe(ctx context.Context, ip string, timeout time.Duration) (Device, bool) {
	for range probeAttempts {
		d, ok, timedOut := probeOnce(ctx, ip, timeout)
		if ok || !timedOut || ctx.Err() != nil {
			return d, ok
		}
	}
	return Device{}, false
}

func probeOnce(ctx context.Context, ip string, timeout time.Duration) (d Device, ok, timedOut bool) {
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	url := fmt.Sprintf("http://%s:%d/api/device", ip, probePort)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return Device{}, false, false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Device{}, false, errors.Is(err, context.DeadlineExceeded)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return Device{}, false, false
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Device{}, false, errors.Is(err, context.DeadlineExceeded)
	}
	d, ok = parseDeviceResponse(body, ip)
	return d, ok, false
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
// IPv4 address this host currently has (covers a home LAN with one or more beds
// in "indirect" mode, and joining a bed's own AP, which gives this host an
// address on the bed's subnet), plus extra - the subnets this host can reach
// but has no interface on, e.g. the home LAN as seen from inside a container.
// No address is assumed: a host with no usable interface and no extra scans
// nothing.
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

// subnetsFromAddrs is candidateSubnets' pure core: given a host's interface
// addresses, which /24 prefixes are worth probing. Split out so it's
// testable with crafted net.Addr values instead of this machine's real ones.
func subnetsFromAddrs(addrs []net.Addr) []string {
	var subnets []string
	seen := map[string]bool{}
	for _, a := range addrs {
		ipNet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip4 := ipNet.IP.To4()
		if ip4 == nil || ip4.IsLoopback() || ip4.IsLinkLocalUnicast() {
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
