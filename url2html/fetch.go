package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

const defaultUA = "Mozilla/5.0 (compatible; url2html/1.0; +https://github.com/anthropics/url2html)"

// fetchHTML downloads a URL and returns the HTML body, parsed URL, and any error.
func fetchHTML(rawURL string, timeout time.Duration, userAgent string) ([]byte, *url.URL, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid URL %q: %w", rawURL, err)
	}

	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, fmt.Errorf("HTTP %d for %s", resp.StatusCode, rawURL)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("reading response: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Fetched %s (%s)\n", rawURL, humanSize(int64(len(body))))
	return body, parsed, nil
}
