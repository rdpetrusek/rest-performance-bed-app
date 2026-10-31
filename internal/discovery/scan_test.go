package discovery

import (
	"net"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParseDeviceResponse(t *testing.T) {
	tests := []struct {
		name string
		body string
		ip   string
		want Device
		ok   bool
	}{
		{
			name: "ReST Bed accepted, address filled in from the body",
			body: `{"class":"ReST Bed","name":"ReST-ee0d07","id":"d8c0a6ee0d07","address":"192.0.2.1","model":"mac","configuration":"publ5z65ha"}`,
			ip:   "192.0.2.1",
			want: Device{ID: "d8c0a6ee0d07", Name: "ReST-ee0d07", Address: "192.0.2.1", Class: "ReST Bed", Model: "mac", Configuration: "publ5z65ha"},
			ok:   true,
		},
		{
			name: "SmartBed class also accepted",
			body: `{"class":"SmartBed","name":"SM-1234","id":"abc","address":"192.0.2.50"}`,
			ip:   "192.0.2.50",
			want: Device{ID: "abc", Name: "SM-1234", Address: "192.0.2.50", Class: "SmartBed"},
			ok:   true,
		},
		{
			name: "address missing from body falls back to the probed ip",
			body: `{"class":"ReST Bed","name":"ReST-x","id":"x"}`,
			ip:   "192.0.2.7",
			want: Device{ID: "x", Name: "ReST-x", Address: "192.0.2.7", Class: "ReST Bed"},
			ok:   true,
		},
		{
			name: "unrelated device class rejected",
			body: `{"class":"SomeOtherThing","name":"not a bed"}`,
			ip:   "192.0.2.9",
			ok:   false,
		},
		{
			name: "malformed JSON rejected",
			body: `not json`,
			ip:   "192.0.2.9",
			ok:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseDeviceResponse([]byte(tt.body), tt.ip)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if !ok {
				return
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func ipNet(ip string, bits int) net.Addr {
	return &net.IPNet{IP: net.ParseIP(ip), Mask: net.CIDRMask(bits, 32)}
}

func TestSubnetsFromAddrs(t *testing.T) {
	tests := []struct {
		name  string
		addrs []net.Addr
		want  []string
	}{
		{
			name:  "no interfaces scans nothing: no subnet is assumed",
			addrs: nil,
			want:  nil,
		},
		{
			name:  "loopback and link-local are excluded",
			addrs: []net.Addr{ipNet("127.0.0.1", 8), ipNet("169.254.7.7", 16)},
			want:  nil,
		},
		{
			name:  "joined directly to a bed's AP: its subnet is derived from the interface",
			addrs: []net.Addr{ipNet("198.51.100.2", 24)},
			want:  []string{"198.51.100"},
		},
		{
			name:  "two interfaces on one subnet are listed once",
			addrs: []net.Addr{ipNet("192.0.2.69", 24), ipNet("192.0.2.70", 24)},
			want:  []string{"192.0.2"},
		},
		{
			name:  "IPv6 addresses are ignored",
			addrs: []net.Addr{ipNet("fe80::1", 64), ipNet("192.0.2.69", 24)},
			want:  []string{"192.0.2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := subnetsFromAddrs(tt.addrs)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
