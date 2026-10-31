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
			body: `{"class":"ReST Bed","name":"ReST-ee0d07","id":"d8c0a6ee0d07","address":"10.0.0.1","model":"mac","configuration":"publ5z65ha"}`,
			ip:   "10.0.0.1",
			want: Device{ID: "d8c0a6ee0d07", Name: "ReST-ee0d07", Address: "10.0.0.1", Class: "ReST Bed", Model: "mac", Configuration: "publ5z65ha"},
			ok:   true,
		},
		{
			name: "SmartBed class also accepted",
			body: `{"class":"SmartBed","name":"SM-1234","id":"abc","address":"192.168.1.50"}`,
			ip:   "192.168.1.50",
			want: Device{ID: "abc", Name: "SM-1234", Address: "192.168.1.50", Class: "SmartBed"},
			ok:   true,
		},
		{
			name: "address missing from body falls back to the probed ip",
			body: `{"class":"ReST Bed","name":"ReST-x","id":"x"}`,
			ip:   "10.0.0.7",
			want: Device{ID: "x", Name: "ReST-x", Address: "10.0.0.7", Class: "ReST Bed"},
			ok:   true,
		},
		{
			name: "unrelated device class rejected",
			body: `{"class":"SomeOtherThing","name":"not a bed"}`,
			ip:   "10.0.0.9",
			ok:   false,
		},
		{
			name: "malformed JSON rejected",
			body: `not json`,
			ip:   "10.0.0.9",
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
			name:  "no interfaces still seeds the bed's own AP subnet",
			addrs: nil,
			want:  []string{"10.0.0"},
		},
		{
			name:  "loopback is excluded",
			addrs: []net.Addr{ipNet("127.0.0.1", 8)},
			want:  []string{"10.0.0"},
		},
		{
			name:  "joined directly to the bed's AP: no duplicate of the seed",
			addrs: []net.Addr{ipNet("10.0.0.2", 24)},
			want:  []string{"10.0.0"},
		},
		{
			name:  "home LAN interface adds its own subnet alongside the seed",
			addrs: []net.Addr{ipNet("192.168.1.69", 24)},
			want:  []string{"10.0.0", "192.168.1"},
		},
		{
			name:  "IPv6 addresses are ignored",
			addrs: []net.Addr{ipNet("fe80::1", 64), ipNet("192.168.1.69", 24)},
			want:  []string{"10.0.0", "192.168.1"},
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

func TestSubnetOf(t *testing.T) {
	tests := []struct {
		host string
		want string
	}{
		{"192.168.1.240", "192.168.1"},
		{"10.0.0.1", "10.0.0"},
		{"bed.local", ""},
		{"fe80::1", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := SubnetOf(tt.host); got != tt.want {
			t.Errorf("SubnetOf(%q) = %q, want %q", tt.host, got, tt.want)
		}
	}
}
