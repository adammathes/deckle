package main

import (
	"context"
	"fmt"
	"net"
	"os"
)

var privateIPBlocks []*net.IPNet

func init() {
	for _, cidr := range []string{
		"127.0.0.0/8",    // IPv4 loopback
		"10.0.0.0/8",     // RFC1918
		"172.16.0.0/12",  // RFC1918
		"192.168.0.0/16", // RFC1918
		"169.254.0.0/16", // RFC3927 link-local
		"::1/128",        // IPv6 loopback
		"fe80::/10",      // IPv6 link-local
		"fc00::/7",       // IPv6 unique local
	} {
		_, block, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(fmt.Errorf("parse error on %q: %v", cidr, err))
		}
		privateIPBlocks = append(privateIPBlocks, block)
	}
}

func isPrivateIP(ip net.IP) bool {
	if os.Getenv("DECKLE_TEST_ALLOW_LOCAL") == "1" {
		return false
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	for _, block := range privateIPBlocks {
		if block.Contains(ip) {
			return true
		}
	}
	return false
}

// safeDialContext wraps a dialer to block connections to private IPs.
// It resolves the hostname, checks the IP, and then dials the safe IP directly.
func safeDialContext(dialer *net.Dialer) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}

		ips, err := net.LookupIP(host)
		if err != nil {
			return nil, err
		}

		var safeIP net.IP
		for _, ip := range ips {
			if !isPrivateIP(ip) {
				safeIP = ip
				break
			}
		}

		if safeIP == nil {
			return nil, fmt.Errorf("blocked connection to private/local IP for %s", host)
		}

		// Dial the IP directly to avoid TOCTOU re-resolution.
		// Note: For TLS, the caller is responsible for SNI using the original hostname.
		safeAddr := net.JoinHostPort(safeIP.String(), port)
		return dialer.DialContext(ctx, network, safeAddr)
	}
}
