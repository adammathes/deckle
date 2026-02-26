package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHTMLOutput_CompleteDocument(t *testing.T) {
	pageHTML := `<!DOCTYPE html>
<html>
<head>
	<title>Test Title - Site Name</title>
	<meta name="author" content="John Doe">
	<meta property="article:published_time" content="2024-05-20T12:00:00Z">
</head>
<body>
<article>
	<h1>Test Title</h1>
	<p>This is a test article content that is long enough for readability. 
	It needs to have several sentences to be recognized as the main content.
	We are testing that the output is a complete HTML document.</p>
	<p>Second paragraph to ensure enough text density for the algorithm.
	The goal is to have a full HTML structure in the final output.</p>
</article>
</body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(pageHTML))
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "output.html")

	cfg := cliConfig{
		opts:      optimizeOpts{maxWidth: 800, quality: 60},
		output:    outFile,
		format:    "html",
		timeout:   5 * time.Second,
		userAgent: "test-agent",
		args:      []string{srv.URL},
	}

	err := run(cfg)
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatal(err)
	}

	content := string(data)

	// Check for complete HTML structure
	if !strings.Contains(content, "<!DOCTYPE html>") {
		t.Error("output missing DOCTYPE")
	}
	if !strings.Contains(content, "<html>") || !strings.Contains(content, "</html>") {
		t.Error("output missing html tags")
	}
	if !strings.Contains(content, "<head>") || !strings.Contains(content, "</head>") {
		t.Error("output missing head tags")
	}
	if !strings.Contains(content, "<body>") || !strings.Contains(content, "</body>") {
		t.Error("output missing body tags")
	}

	// Check for metadata in head
	if !strings.Contains(content, "<title>Test Title</title>") {
		t.Errorf("output missing or incorrect title in head. Got: %s", content)
	}

	// Check for article content
	if !strings.Contains(content, "<h1>Test Title</h1>") {
		t.Error("output missing article H1")
	}
}
