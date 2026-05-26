package main

import (
	"context"
	"fmt"
	"net"
	"time"
)

// newSafeDialContext returns a DialContext that resolves the hostname, rejects
// any address in a private / internal range, and dials the resolved IP
// directly (pinning the address so a later DNS lookup can't swap a public
// answer for a private one between resolution and connection -- the
// "DNS rebinding" attack).
//
// Applies to every connection the http.Transport opens, including ones
// triggered by HTTP redirects, so redirect-to-private is also blocked.
func newSafeDialContext() func(ctx context.Context, network, addr string) (net.Conn, error) {
	base := &net.Dialer{
		Timeout:   5 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("no addresses for %s", host)
		}
		// Reject if ANY resolved IP is internal -- multi-A-record poisoning
		// could otherwise slip a private address past us.
		var safe net.IP
		for _, ipa := range ips {
			ip := ipa.IP
			if isInternalIP(ip) {
				return nil, fmt.Errorf("blocked: %s resolves to internal address %s", host, ip)
			}
			if safe == nil {
				safe = ip
			}
		}
		// Dial the pinned IP directly so DNS rebinding can't substitute.
		return base.DialContext(ctx, network, net.JoinHostPort(safe.String(), port))
	}
}

// isInternalIP reports whether an IP address is in a range we never want the
// article extractor to reach: loopback, RFC 1918 private, link-local,
// multicast, unspecified, CGNAT (100.64/10), or IPv6 ULA.
func isInternalIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() ||
		ip.IsPrivate() || // RFC 1918 v4 + RFC 4193 v6 ULA (fc00::/7)
		ip.IsLinkLocalUnicast() || // 169.254/16 + fe80::/10
		ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified() {
		return true
	}
	// CGNAT (100.64.0.0/10) -- Tailscale uses this, residential ISPs too;
	// not covered by net.IP.IsPrivate().
	if v4 := ip.To4(); v4 != nil {
		if v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
			return true
		}
	}
	return false
}
