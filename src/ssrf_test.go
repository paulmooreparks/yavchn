package main

import (
	"net"
	"testing"
)

func TestIsInternalIP(t *testing.T) {
	cases := []struct {
		ip       string
		internal bool
		why      string
	}{
		{"127.0.0.1", true, "loopback v4"},
		{"::1", true, "loopback v6"},
		{"10.0.0.1", true, "RFC1918 10/8"},
		{"172.16.0.1", true, "RFC1918 172.16/12"},
		{"192.168.1.1", true, "RFC1918 192.168/16"},
		{"169.254.1.1", true, "link-local v4"},
		{"fe80::1", true, "link-local v6"},
		{"224.0.0.1", true, "multicast v4"},
		{"ff02::1", true, "multicast v6"},
		{"0.0.0.0", true, "unspecified v4"},
		{"::", true, "unspecified v6"},
		{"100.64.0.1", true, "CGNAT low"},
		{"100.127.255.254", true, "CGNAT high"},
		{"fc00::1", true, "IPv6 ULA"},
		{"fd00::1", true, "IPv6 ULA"},

		{"8.8.8.8", false, "public v4 (Google DNS)"},
		{"1.1.1.1", false, "public v4 (Cloudflare DNS)"},
		{"2606:4700:4700::1111", false, "public v6 (Cloudflare DNS)"},
		{"100.63.255.254", false, "just below CGNAT range"},
		{"100.128.0.1", false, "just above CGNAT range"},
		{"203.0.113.1", false, "TEST-NET-3 (public-routable, RFC5737 documentation)"},
	}
	for _, tc := range cases {
		ip := net.ParseIP(tc.ip)
		if ip == nil {
			t.Fatalf("test bug: invalid ip %q (%s)", tc.ip, tc.why)
		}
		got := isInternalIP(ip)
		if got != tc.internal {
			t.Errorf("isInternalIP(%s) = %v, want %v (%s)", tc.ip, got, tc.internal, tc.why)
		}
	}
}

func TestIsInternalIP_NilIsInternal(t *testing.T) {
	if !isInternalIP(nil) {
		t.Fatal("nil IP must be treated as internal (default-deny)")
	}
}
