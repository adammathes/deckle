package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"
)

const defaultUA = "Mozilla/5.0 (X11; Linux x86_64; rv:133.0) Gecko/20100101 Firefox/133.0"

// maxResponseBytes is the maximum number of bytes to read from any single
// HTTP response body. Responses exceeding this limit are rejected with an
// error. Set from the -max-response-size CLI flag; 0 means unlimited.
var maxResponseBytes int64 = 128 * 1024 * 1024 // 128 MB default

// fetchProxyURL is the HTTP proxy URL for all outgoing requests.
// When non-empty, deckle falls back to standard TLS (no uTLS fingerprinting)
// so the request can tunnel through the proxy. Set by the --proxy CLI flag.
var fetchProxyURL string

// newProxyClient creates an HTTP client that routes through the given proxy
// address using standard TLS. If proxyAddr is empty, it creates a direct
// (no-proxy) client with standard TLS.
func newProxyClient(proxyAddr string, timeout time.Duration) *http.Client {
	transport := &http.Transport{
		DialContext: safeDialContext(&net.Dialer{Timeout: timeout}),
	}
	if proxyAddr != "" {
		if proxyURL, err := url.Parse(proxyAddr); err == nil {
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}
}

// readLimited reads up to maxResponseBytes from r. If the response exceeds
// the limit, it returns an error. If maxResponseBytes is 0, it reads without
// limit (equivalent to io.ReadAll).
func readLimited(r io.Reader, limit int64) ([]byte, error) {
	if limit <= 0 {
		return io.ReadAll(r)
	}
	// Read limit+1 bytes so we can detect overflow without a custom reader.
	lr := io.LimitReader(r, limit+1)
	data, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response body exceeds maximum allowed size (%s)", humanSize(limit))
	}
	return data, nil
}

// utlsConn wraps a utls.UConn and satisfies net.Conn + the
// ConnectionState interface that net/http2 needs.
type utlsConn struct {
	*utls.UConn
}

func (c *utlsConn) ConnectionState() tls.ConnectionState {
	cs := c.UConn.ConnectionState()
	return tls.ConnectionState{
		Version:                    cs.Version,
		HandshakeComplete:          cs.HandshakeComplete,
		CipherSuite:                cs.CipherSuite,
		NegotiatedProtocol:         cs.NegotiatedProtocol,
		NegotiatedProtocolIsMutual: cs.NegotiatedProtocolIsMutual,
		ServerName:                 cs.ServerName,
		PeerCertificates:           cs.PeerCertificates,
		VerifiedChains:             cs.VerifiedChains,
		OCSPResponse:               cs.OCSPResponse,
		TLSUnique:                  cs.TLSUnique,
	}
}

// newBrowserClient creates an HTTP client that mimics a real browser's
// TLS fingerprint using utls. Supports both HTTP/1.1 and HTTP/2.
func newBrowserClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: timeout}

	// HTTP/2 transport for h2 connections
	h2Transport := &http2.Transport{}

	// HTTP/1.1 transport with utls dialer
	h1Transport := &http.Transport{
		DialContext: safeDialContext(dialer),
	}

	// Custom round tripper that dials with utls and routes to h1 or h2
	// based on ALPN negotiation.
	rt := &browserTransport{
		dialer:  dialer,
		h1:      h1Transport,
		h2:      h2Transport,
		timeout: timeout,
	}

	return &http.Client{
		Timeout:   timeout,
		Transport: rt,
	}
}

type browserTransport struct {
	dialer  *net.Dialer
	h1      *http.Transport
	h2      *http2.Transport
	timeout time.Duration
}

func (bt *browserTransport) dialUTLS(ctx context.Context, network, addr string) (net.Conn, string, error) {
	conn, err := safeDialContext(bt.dialer)(ctx, network, addr)
	if err != nil {
		return nil, "", err
	}

	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}

	tlsConn := utls.UClient(conn, &utls.Config{
		ServerName: host,
	}, utls.HelloFirefox_120)

	if err := tlsConn.HandshakeContext(ctx); err != nil {
		conn.Close()
		return nil, "", err
	}

	alpn := tlsConn.ConnectionState().NegotiatedProtocol
	return &utlsConn{tlsConn}, alpn, nil
}

func (bt *browserTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != "https" {
		return bt.h1.RoundTrip(req)
	}

	addr := req.URL.Host
	if !hasPort(addr) {
		addr = addr + ":443"
	}

	conn, alpn, err := bt.dialUTLS(req.Context(), "tcp", addr)
	if err != nil {
		return nil, err
	}

	if alpn == "h2" {
		// For HTTP/2, use http2.ClientConn directly
		h2conn, err := bt.h2.NewClientConn(conn)
		if err != nil {
			conn.Close()
			return nil, err
		}
		return h2conn.RoundTrip(req)
	}

	// For HTTP/1.1, inject the TLS conn into a one-shot transport
	transport := &http.Transport{
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return conn, nil
		},
	}
	return transport.RoundTrip(req)
}

func hasPort(host string) bool {
	_, _, err := net.SplitHostPort(host)
	return err == nil
}

// fetchHTML downloads a URL and returns the HTML body, parsed URL, and any error.
// Uses browser-like TLS fingerprint and headers to avoid bot detection.
func fetchHTML(rawURL string, timeout time.Duration, userAgent string) ([]byte, *url.URL, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid URL %q: %w", rawURL, err)
	}

	var client *http.Client
	if fetchProxyURL != "" {
		// When a proxy is configured, fall back to standard TLS so the request
		// can tunnel through the proxy (uTLS cannot negotiate CONNECT tunnels).
		client = newProxyClient(fetchProxyURL, timeout)
	} else if parsed.Scheme == "https" {
		client = newBrowserClient(timeout)
	} else {
		client = &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				DialContext: safeDialContext(&net.Dialer{Timeout: timeout}),
			},
		}
	}

	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "none")

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, fmt.Errorf("HTTP %d for %s", resp.StatusCode, rawURL)
	}

	body, err := readLimited(resp.Body, maxResponseBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("reading response: %w", err)
	}

	fmt.Fprintf(logOut, "Fetched %s (%s)\n", rawURL, humanSize(int64(len(body))))
	return body, parsed, nil
}

// fetchImageClient is used by imgoptimize.go for downloading external images.
var fetchImageClient *http.Client

func init() {
	fetchImageClient = newBrowserClient(30 * time.Second)
}

// ignoreCertClient returns an HTTP client that skips TLS verification.
// Used only for tests with httptest TLS servers.
func ignoreCertClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
}
