package main

import (
	"fmt"
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
