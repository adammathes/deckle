package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFetchHTML_Success(t *testing.T) {
	expected := "<html><body>Hello</body></html>"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(expected))
	}))
	defer srv.Close()

	body, u, err := fetchHTML(srv.URL, 5*time.Second, defaultUA)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != expected {
		t.Errorf("got %q, want %q", string(body), expected)
	}
	if u.Host == "" {
		t.Error("expected parsed URL with host")
	}
}

func TestFetchHTML_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	_, _, err := fetchHTML(srv.URL, 5*time.Second, defaultUA)
	if err == nil {
		t.Fatal("expected error for 404")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("expected 404 in error, got: %v", err)
	}
}

func TestFetchHTML_UserAgent(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	_, _, err := fetchHTML(srv.URL, 5*time.Second, "my-custom-agent/2.0")
	if err != nil {
		t.Fatal(err)
	}
	if gotUA != "my-custom-agent/2.0" {
		t.Errorf("User-Agent = %q, want %q", gotUA, "my-custom-agent/2.0")
	}
}

func TestFetchHTML_BrowserHeaders(t *testing.T) {
	var headers http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers = r.Header
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	_, _, err := fetchHTML(srv.URL, 5*time.Second, defaultUA)
	if err != nil {
		t.Fatal(err)
	}

	required := map[string]string{
		"Sec-Fetch-Dest": "document",
		"Sec-Fetch-Mode": "navigate",
		"Sec-Fetch-Site": "none",
		"Accept":         "text/html",
	}
	for header, wantSubstr := range required {
		got := headers.Get(header)
		if got == "" {
			t.Errorf("missing header %s", header)
		} else if !strings.Contains(got, wantSubstr) {
			t.Errorf("%s = %q, want substring %q", header, got, wantSubstr)
		}
	}
}

func TestHasPort(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{"example.com:443", true},
		{"example.com:80", true},
		{"[::1]:8080", true},
		{"example.com", false},
		{"localhost", false},
	}
	for _, tt := range tests {
		got := hasPort(tt.host)
		if got != tt.want {
			t.Errorf("hasPort(%q) = %v, want %v", tt.host, got, tt.want)
		}
	}
}

func TestIgnoreCertClient(t *testing.T) {
	client := ignoreCertClient(10 * time.Second)
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.Timeout != 10*time.Second {
		t.Errorf("timeout = %v, want 10s", client.Timeout)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatal("expected *http.Transport")
	}
	if transport.TLSClientConfig == nil || !transport.TLSClientConfig.InsecureSkipVerify {
		t.Error("expected InsecureSkipVerify to be true")
	}
}

func TestFetchHTML_InvalidURL(t *testing.T) {
	_, _, err := fetchHTML("://bad-url", 5*time.Second, defaultUA)
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

// --- readLimited tests ---

func TestReadLimited_UnderLimit(t *testing.T) {
	data := bytes.Repeat([]byte("a"), 100)
	got, err := readLimited(bytes.NewReader(data), 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 100 {
		t.Errorf("got %d bytes, want 100", len(got))
	}
}

func TestReadLimited_ExactlyAtLimit(t *testing.T) {
	data := bytes.Repeat([]byte("b"), 200)
	got, err := readLimited(bytes.NewReader(data), 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 200 {
		t.Errorf("got %d bytes, want 200", len(got))
	}
}

func TestReadLimited_ExceedsLimit(t *testing.T) {
	data := bytes.Repeat([]byte("c"), 201)
	_, err := readLimited(bytes.NewReader(data), 200)
	if err == nil {
		t.Fatal("expected error when exceeding limit")
	}
	if !strings.Contains(err.Error(), "exceeds maximum allowed size") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestReadLimited_ZeroMeansUnlimited(t *testing.T) {
	data := bytes.Repeat([]byte("d"), 10000)
	got, err := readLimited(bytes.NewReader(data), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 10000 {
		t.Errorf("got %d bytes, want 10000", len(got))
	}
}

func TestReadLimited_NegativeMeansUnlimited(t *testing.T) {
	data := bytes.Repeat([]byte("e"), 5000)
	got, err := readLimited(bytes.NewReader(data), -1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5000 {
		t.Errorf("got %d bytes, want 5000", len(got))
	}
}

func TestReadLimited_EmptyReader(t *testing.T) {
	got, err := readLimited(bytes.NewReader(nil), 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("got %d bytes, want 0", len(got))
	}
}

// --- fetchHTML size limit integration tests ---

func TestFetchHTML_ExceedsSizeLimit(t *testing.T) {
	// Save and restore maxResponseBytes
	saved := maxResponseBytes
	defer func() { maxResponseBytes = saved }()
	maxResponseBytes = 100

	// Server sends 200 bytes (exceeds 100 byte limit)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(bytes.Repeat([]byte("x"), 200))
	}))
	defer srv.Close()

	_, _, err := fetchHTML(srv.URL, 5*time.Second, defaultUA)
	if err == nil {
		t.Fatal("expected error when response exceeds size limit")
	}
	if !strings.Contains(err.Error(), "exceeds maximum allowed size") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestFetchHTML_WithinSizeLimit(t *testing.T) {
	saved := maxResponseBytes
	defer func() { maxResponseBytes = saved }()
	maxResponseBytes = 1000

	expected := "<html><body>Small page</body></html>"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(expected))
	}))
	defer srv.Close()

	body, _, err := fetchHTML(srv.URL, 5*time.Second, defaultUA)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != expected {
		t.Errorf("got %q, want %q", string(body), expected)
	}
}

func TestFetchHTML_UnlimitedSizeZero(t *testing.T) {
	saved := maxResponseBytes
	defer func() { maxResponseBytes = saved }()
	maxResponseBytes = 0

	// Large-ish response should succeed with no limit
	largeBody := strings.Repeat("z", 50000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(largeBody))
	}))
	defer srv.Close()

	body, _, err := fetchHTML(srv.URL, 5*time.Second, defaultUA)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != 50000 {
		t.Errorf("got %d bytes, want 50000", len(body))
	}
}
