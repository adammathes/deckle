package main

import (
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

	body, u, err := fetchHTML(srv.URL, 5*time.Second, "test-agent")
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

	_, _, err := fetchHTML(srv.URL, 5*time.Second, "test-agent")
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
