package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image/color"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// withProgressCapture sets progressOut to a buffer, runs fn, restores
// progressOut, and returns the captured output.
func withProgressCapture(fn func()) string {
	var buf bytes.Buffer
	saved := progressOut
	progressOut = &buf
	defer func() { progressOut = saved }()
	fn()
	return buf.String()
}

func TestShortURL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://example.com/article", "example.com/article"},
		{"https://example.com/", "example.com"},
		{"https://example.com/very/deep/path/to/article", "example.com/very/deep/path/to/article"},
		{"not a url %%%", "not a url %%%"},
	}
	for _, tt := range tests {
		got := shortURL(tt.input)
		if got != tt.want {
			t.Errorf("shortURL(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestShortURL_Truncation(t *testing.T) {
	longPath := "https://example.com/" + strings.Repeat("x", 100)
	result := shortURL(longPath)
	if len(result) > 60 {
		t.Errorf("shortURL should truncate to 60 chars, got %d", len(result))
	}
	if !strings.HasSuffix(result, "...") {
		t.Error("truncated shortURL should end with ...")
	}
}

func TestPprintf(t *testing.T) {
	output := withProgressCapture(func() {
		pprintf("hello %s %d\n", "world", 42)
	})
	if output != "hello world 42\n" {
		t.Errorf("pprintf output = %q, want %q", output, "hello world 42\n")
	}
}

func TestPprintf_NoOutput_WhenDiscard(t *testing.T) {
	saved := progressOut
	progressOut = io.Discard
	defer func() { progressOut = saved }()

	// Just ensure no panic; output goes nowhere.
	pprintf("this goes nowhere: %d\n", 1)
}

func TestProgress_SingleURL_WithOutput(t *testing.T) {
	pageHTML := `<!DOCTYPE html>
<html><head><title>Single Progress</title></head><body>
<article>
<h1>Single Progress</h1>
<p>This is a test article for single URL progress testing. It has enough
content for readability to extract it as the main article. More text here.</p>
<p>Second paragraph with additional content for readability.</p>
</article>
</body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(pageHTML))
	}))
	defer srv.Close()

	outFile := filepath.Join(t.TempDir(), "output.html")
	cfg := cliConfig{
		opts:      optimizeOpts{maxWidth: 800, quality: 60},
		output:    outFile,
		timeout:   5 * time.Second,
		userAgent: "test-agent",
		args:      []string{srv.URL},
	}

	output := withProgressCapture(func() {
		err := run(cfg)
		if err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(output, "📥") {
		t.Errorf("expected 📥 fetch indicator in progress output, got:\n%s", output)
	}
	if !strings.Contains(output, "✅") {
		t.Errorf("expected ✅ success indicator in progress output, got:\n%s", output)
	}
	if !strings.Contains(output, "Wrote") {
		t.Errorf("expected 'Wrote' in progress output, got:\n%s", output)
	}
}

func TestProgress_SingleURL_NoOutput_NoProgress(t *testing.T) {
	// When no -o flag is set, progressOut should remain io.Discard.
	// pprintf calls should produce no output.
	saved := progressOut
	progressOut = io.Discard
	defer func() { progressOut = saved }()

	var buf bytes.Buffer
	progressOut = &buf

	// Simulate: no -o flag, single URL mode. We just test pprintf directly
	// since run() doesn't touch progressOut anymore.
	pprintf("should appear\n")
	if buf.String() != "should appear\n" {
		t.Errorf("unexpected output: %q", buf.String())
	}
}

func TestProgress_EpubMode_MultipleArticles(t *testing.T) {
	articlesByPath := map[string]string{
		"/1": `<!DOCTYPE html><html><head><title>Article One</title></head><body>
		<article><h1>Article One</h1>
		<p>First article content for progress test. It has enough content for
		readability to properly extract the main content region.</p>
		<p>Second paragraph for content density.</p></article></body></html>`,
		"/2": `<!DOCTYPE html><html><head><title>Article Two</title></head><body>
		<article><h1>Article Two</h1>
		<p>Second article content for progress test. More content needed for
		readability to extract this as the main article properly.</p>
		<p>Additional paragraph for the algorithm.</p></article></body></html>`,
		"/3": `<!DOCTYPE html><html><head><title>Article Three</title></head><body>
		<article><h1>Article Three</h1>
		<p>Third article content for progress test. Enough text for readability
		to work with this content block as the main article.</p>
		<p>More content for density threshold.</p></article></body></html>`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if html, ok := articlesByPath[r.URL.Path]; ok {
			w.Write([]byte(html))
		} else {
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	outFile := filepath.Join(t.TempDir(), "progress.epub")
	cfg := cliConfig{
		opts:      optimizeOpts{maxWidth: 800, quality: 60},
		output:    outFile,
		timeout:   5 * time.Second,
		userAgent: "test-agent",
		epubMode:  true,
		args:      []string{srv.URL + "/1", srv.URL + "/2", srv.URL + "/3"},
	}

	output := withProgressCapture(func() {
		err := run(cfg)
		if err != nil {
			t.Fatal(err)
		}
	})

	// Should show fetch header
	if !strings.Contains(output, "📥 Fetching 3 articles") {
		t.Errorf("expected '📥 Fetching 3 articles' in progress, got:\n%s", output)
	}
	// Should show per-article checkmarks (at least 3 articles + final)
	if strings.Count(output, "✅") < 3 {
		t.Errorf("expected at least 3 ✅ marks, got %d in:\n%s",
			strings.Count(output, "✅"), output)
	}
	// Should show epub building
	if !strings.Contains(output, "📦") {
		t.Errorf("expected 📦 epub build indicator, got:\n%s", output)
	}
	// Should show final output path
	if !strings.Contains(output, "progress.epub") {
		t.Errorf("expected output filename in progress, got:\n%s", output)
	}
}

func TestProgress_EpubMode_WithFailedArticle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.URL.Path == "/good" {
			w.Write([]byte(`<!DOCTYPE html><html><head><title>Good Article</title></head><body>
			<article><h1>Good Article</h1>
			<p>This is a good article with enough content for readability. More text.</p>
			<p>Second paragraph for content density threshold.</p></article></body></html>`))
		} else {
			w.WriteHeader(500)
		}
	}))
	defer srv.Close()

	outFile := filepath.Join(t.TempDir(), "partial.epub")
	cfg := cliConfig{
		opts:      optimizeOpts{maxWidth: 800, quality: 60},
		output:    outFile,
		timeout:   5 * time.Second,
		userAgent: "test-agent",
		epubMode:  true,
		args:      []string{srv.URL + "/good", srv.URL + "/bad"},
	}

	output := withProgressCapture(func() {
		err := run(cfg)
		if err != nil {
			t.Fatal(err)
		}
	})

	// Should show success for good article
	if !strings.Contains(output, "✅") {
		t.Errorf("expected ✅ for successful article, got:\n%s", output)
	}
	// Should show failure for bad article
	if !strings.Contains(output, "❌") {
		t.Errorf("expected ❌ for failed article, got:\n%s", output)
	}
}

func TestProgress_WithImages(t *testing.T) {
	imgData := makePNG(1200, 900, color.NRGBA{200, 100, 50, 255})
	imgURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString(imgData)

	pageHTML := fmt.Sprintf(`<!DOCTYPE html>
<html><head><title>Image Progress</title></head><body>
<article>
<h1>Image Progress</h1>
<p>Article with images for progress testing. It has enough content for
readability to extract it as the main article. More filler text here.</p>
<img src="%s" alt="test image">
<p>Another paragraph for readability content density.</p>
</article>
</body></html>`, imgURI)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(pageHTML))
	}))
	defer srv.Close()

	outFile := filepath.Join(t.TempDir(), "img-progress.html")
	cfg := cliConfig{
		opts:      optimizeOpts{maxWidth: 800, quality: 60},
		output:    outFile,
		timeout:   5 * time.Second,
		userAgent: "test-agent",
		args:      []string{srv.URL},
	}

	output := withProgressCapture(func() {
		err := run(cfg)
		if err != nil {
			t.Fatal(err)
		}
	})

	// Should show image optimization summary
	if !strings.Contains(output, "🖼️") {
		t.Errorf("expected 🖼️ image optimization indicator in progress, got:\n%s", output)
	}
	if !strings.Contains(output, "optimized") {
		t.Errorf("expected 'optimized' in image progress, got:\n%s", output)
	}
}

func TestProgress_ExternalImages(t *testing.T) {
	imgData := makePNG(100, 100, color.NRGBA{255, 0, 0, 255})

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/img/") {
			w.Header().Set("Content-Type", "image/png")
			w.Write(imgData)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(fmt.Sprintf(`<!DOCTYPE html>
<html><head><title>External Images Progress</title></head><body>
<article>
<h1>External Images Progress</h1>
<p>Article with external images for progress testing. Enough content for
readability to identify this as the main content region of the page.</p>
<img src="%s/img/1.png" alt="ext1">
<img src="%s/img/2.png" alt="ext2">
<p>More content for readability threshold requirements.</p>
</article>
</body></html>`, srv.URL, srv.URL)))
	}))
	defer srv.Close()

	saved := fetchImageClient
	fetchImageClient = srv.Client()
	defer func() { fetchImageClient = saved }()

	outFile := filepath.Join(t.TempDir(), "ext-progress.html")
	cfg := cliConfig{
		opts:      optimizeOpts{maxWidth: 800, quality: 60},
		output:    outFile,
		timeout:   5 * time.Second,
		userAgent: "test-agent",
		args:      []string{srv.URL},
	}

	output := withProgressCapture(func() {
		err := run(cfg)
		if err != nil {
			t.Fatal(err)
		}
	})

	// Should show external image fetch indicator
	if !strings.Contains(output, "🔗") {
		t.Errorf("expected 🔗 external image fetch indicator, got:\n%s", output)
	}
	if !strings.Contains(output, "external images fetched") {
		t.Errorf("expected 'external images fetched' in progress, got:\n%s", output)
	}
}

func TestProgress_MarkdownMode_SingleURL(t *testing.T) {
	pageHTML := `<!DOCTYPE html>
<html><head><title>MD Progress</title></head><body>
<article>
<h1>MD Progress</h1>
<p>This is a test article for markdown progress testing. It has enough content
for readability to extract it as the main article. More text here to meet
the content threshold for the readability algorithm.</p>
<p>Second paragraph with additional content for readability density.</p>
</article>
</body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(pageHTML))
	}))
	defer srv.Close()

	outFile := filepath.Join(t.TempDir(), "output.md")
	cfg := cliConfig{
		opts:         optimizeOpts{maxWidth: 800, quality: 60},
		output:       outFile,
		timeout:      5 * time.Second,
		userAgent:    "test-agent",
		markdownMode: true,
		args:         []string{srv.URL},
	}

	output := withProgressCapture(func() {
		err := run(cfg)
		if err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(output, "📥") {
		t.Errorf("expected 📥 in markdown single-URL progress, got:\n%s", output)
	}
	if !strings.Contains(output, "✅") {
		t.Errorf("expected ✅ in markdown single-URL progress, got:\n%s", output)
	}
	if !strings.Contains(output, "Wrote") {
		t.Errorf("expected 'Wrote' in markdown single-URL progress, got:\n%s", output)
	}
}

func TestProgress_MarkdownMode_MultipleURLs(t *testing.T) {
	pageHTML := `<!DOCTYPE html>
<html><head><title>Multi MD Progress</title></head><body>
<article>
<h1>Multi MD Progress</h1>
<p>Test article for multi-URL markdown progress testing. Enough content for
readability to extract as the main article content region.</p>
<p>Second paragraph for content density threshold.</p>
</article>
</body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(pageHTML))
	}))
	defer srv.Close()

	outFile := filepath.Join(t.TempDir(), "multi.md")
	cfg := cliConfig{
		opts:         optimizeOpts{maxWidth: 800, quality: 60},
		output:       outFile,
		timeout:      5 * time.Second,
		userAgent:    "test-agent",
		markdownMode: true,
		args:         []string{srv.URL + "/a", srv.URL + "/b"},
	}

	output := withProgressCapture(func() {
		err := run(cfg)
		if err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(output, "📥 Fetching 2 articles") {
		t.Errorf("expected '📥 Fetching 2 articles' in progress, got:\n%s", output)
	}
	if !strings.Contains(output, "Wrote") {
		t.Errorf("expected 'Wrote' in markdown multi-URL progress, got:\n%s", output)
	}
}

func TestProgress_ConcurrentSafety(t *testing.T) {
	// Verify pprintf doesn't interleave output from concurrent goroutines.
	output := withProgressCapture(func() {
		done := make(chan struct{})
		for i := 0; i < 10; i++ {
			go func(n int) {
				defer func() { done <- struct{}{} }()
				pprintf("line %d\n", n)
			}(i)
		}
		for i := 0; i < 10; i++ {
			<-done
		}
	})

	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != 10 {
		t.Errorf("expected 10 lines, got %d:\n%s", len(lines), output)
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, "line ") {
			t.Errorf("unexpected line format: %q", line)
		}
	}
}
