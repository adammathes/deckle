package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSSRFProtection(t *testing.T) {
	// Ensure protection is active for this test
	t.Setenv("DECKLE_TEST_ALLOW_LOCAL", "")

	// Start a local server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("secret internal data"))
	}))
	defer srv.Close()

	// Attempt to fetch from the local server
	// This should now FAIL with a blocked error.
	_, _, err := fetchHTML(srv.URL, 5*time.Second, defaultUA)
	if err == nil {
		t.Fatal("Expected error fetching local URL, but got success")
	}

	// Check if the error message contains the expected reason
	if !strings.Contains(err.Error(), "blocked connection") {
		t.Errorf("Expected 'blocked connection' error, got: %v", err)
	}

	fmt.Println("SSRF protection verified: blocked local fetch")
}

func TestIsPrivateIP_Loopback(t *testing.T) {
	t.Setenv("DECKLE_TEST_ALLOW_LOCAL", "")
	if !isPrivateIP(net.ParseIP("127.0.0.1")) {
		t.Error("127.0.0.1 should be private")
	}
	if !isPrivateIP(net.ParseIP("::1")) {
		t.Error("::1 should be private")
	}
}

func TestIsPrivateIP_RFC1918(t *testing.T) {
	t.Setenv("DECKLE_TEST_ALLOW_LOCAL", "")
	privateIPs := []string{
		"10.0.0.1",
		"10.255.255.255",
		"172.16.0.1",
		"172.31.255.255",
		"192.168.0.1",
		"192.168.255.255",
	}
	for _, ip := range privateIPs {
		if !isPrivateIP(net.ParseIP(ip)) {
			t.Errorf("%s should be private", ip)
		}
	}
}

func TestIsPrivateIP_PublicIPs(t *testing.T) {
	t.Setenv("DECKLE_TEST_ALLOW_LOCAL", "")
	publicIPs := []string{
		"8.8.8.8",
		"1.1.1.1",
		"203.0.113.1",
		"93.184.216.34",
	}
	for _, ip := range publicIPs {
		if isPrivateIP(net.ParseIP(ip)) {
			t.Errorf("%s should be public", ip)
		}
	}
}

func TestIsPrivateIP_LinkLocal(t *testing.T) {
	t.Setenv("DECKLE_TEST_ALLOW_LOCAL", "")
	if !isPrivateIP(net.ParseIP("169.254.1.1")) {
		t.Error("169.254.1.1 should be private (link-local)")
	}
	if !isPrivateIP(net.ParseIP("fe80::1")) {
		t.Error("fe80::1 should be private (IPv6 link-local)")
	}
}

func TestIsPrivateIP_IPv6UniqueLocal(t *testing.T) {
	t.Setenv("DECKLE_TEST_ALLOW_LOCAL", "")
	if !isPrivateIP(net.ParseIP("fd00::1")) {
		t.Error("fd00::1 should be private (IPv6 unique local)")
	}
}

func TestIsPrivateIP_TestBypass(t *testing.T) {
	t.Setenv("DECKLE_TEST_ALLOW_LOCAL", "1")
	// With bypass enabled, even loopback should return false
	if isPrivateIP(net.ParseIP("127.0.0.1")) {
		t.Error("127.0.0.1 should not be private when DECKLE_TEST_ALLOW_LOCAL=1")
	}
}

func TestSafeDialContext_BlocksPrivate(t *testing.T) {
	t.Setenv("DECKLE_TEST_ALLOW_LOCAL", "")
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	dial := safeDialContext(dialer)
	_, err := dial(context.Background(), "tcp", "127.0.0.1:80")
	if err == nil {
		t.Fatal("expected error dialing private IP")
	}
	if !strings.Contains(err.Error(), "blocked connection") {
		t.Errorf("expected 'blocked connection' error, got: %v", err)
	}
}

func TestSafeDialContext_InvalidAddr(t *testing.T) {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	dial := safeDialContext(dialer)
	_, err := dial(context.Background(), "tcp", "not-a-valid-address")
	if err == nil {
		t.Fatal("expected error for invalid address")
	}
}
