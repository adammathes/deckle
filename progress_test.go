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

// withVerboseCapture enables verbose output to a buffer, runs fn, restores
// state, and returns the captured output.
func withVerboseCapture(fn func()) string {
	var buf bytes.Buffer
	savedVerbose := verboseOut
	savedLog := logOut
	verboseOut = &buf
	logOut = &buf
	defer func() {
		verboseOut = savedVerbose
		logOut = savedLog
	}()
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

func TestVprintf(t *testing.T) {
	var buf bytes.Buffer
	saved := verboseOut
	verboseOut = &buf
	defer func() { verboseOut = saved }()

	vprintf("hello %s %d\n", "world", 42)
	if buf.String() != "hello world 42\n" {
		t.Errorf("vprintf output = %q, want %q", buf.String(), "hello world 42\n")
	}
}

func TestVprintf_NoOutput_WhenDiscard(t *testing.T) {
	saved := verboseOut
	verboseOut = io.Discard
	defer func() { verboseOut = saved }()

	// Just ensure no panic; output goes nowhere.
	vprintf("this goes nowhere: %d\n", 1)
}

func TestDefaultSilence(t *testing.T) {
	// By default, logOut and verboseOut should be io.Discard.
	// We can't test the actual defaults since other tests may have run,
	// but we verify the helpers are safe to call with Discard.
	saved := verboseOut
	savedLog := logOut
	verboseOut = io.Discard
	logOut = io.Discard
	defer func() {
		verboseOut = saved
		logOut = savedLog
	}()

	vprintf("should not appear\n")
	fmt.Fprintf(logOut, "should not appear\n")
}

func TestVerbose_SingleURL_WithOutput(t *testing.T) {
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

	output := withVerboseCapture(func() {
		err := run(cfg)
		if err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(output, "Downloading 1 article") {
		t.Errorf("expected 'Downloading 1 article' in verbose output, got:\n%s", output)
	}
}

func TestVerbose_EpubMode_MultipleArticles(t *testing.T) {
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

	output := withVerboseCapture(func() {
		err := run(cfg)
		if err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(output, "Downloading 3 articles") {
		t.Errorf("expected 'Downloading 3 articles' in verbose, got:\n%s", output)
	}
	if !strings.Contains(output, "Building epub at") {
		t.Errorf("expected 'Building epub at' in verbose, got:\n%s", output)
	}
}

func TestVerbose_EpubMode_WithFailedArticle(t *testing.T) {
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

	output := withVerboseCapture(func() {
		err := run(cfg)
		if err != nil {
			t.Fatal(err)
		}
	})

	// Should show download line and error for bad article
	if !strings.Contains(output, "Downloading 2 articles") {
		t.Errorf("expected 'Downloading 2 articles' in verbose, got:\n%s", output)
	}
	if !strings.Contains(output, "Error") {
		t.Errorf("expected 'Error' for failed article in verbose, got:\n%s", output)
	}
}

func TestVerbose_WithImages(t *testing.T) {
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

	output := withVerboseCapture(func() {
		err := run(cfg)
		if err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(output, "Optimizing") {
		t.Errorf("expected 'Optimizing' in verbose output, got:\n%s", output)
	}
	if !strings.Contains(output, "images") {
		t.Errorf("expected 'images' in verbose output, got:\n%s", output)
	}
}

func TestVerbose_ExternalImages(t *testing.T) {
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

	output := withVerboseCapture(func() {
		err := run(cfg)
		if err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(output, "Fetching") {
		t.Errorf("expected 'Fetching' in verbose output, got:\n%s", output)
	}
	if !strings.Contains(output, "images") {
		t.Errorf("expected 'images' in verbose output, got:\n%s", output)
	}
}

func TestVerbose_MarkdownMode_SingleURL(t *testing.T) {
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

	output := withVerboseCapture(func() {
		err := run(cfg)
		if err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(output, "Downloading 1 article") {
		t.Errorf("expected 'Downloading 1 article' in verbose, got:\n%s", output)
	}
}

func TestVerbose_MarkdownMode_MultipleURLs(t *testing.T) {
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

	output := withVerboseCapture(func() {
		err := run(cfg)
		if err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(output, "Downloading 2 articles") {
		t.Errorf("expected 'Downloading 2 articles' in verbose, got:\n%s", output)
	}
}

func TestVerbose_ConcurrentSafety(t *testing.T) {
	// Verify concurrent vprintf calls don't interleave.
	var buf bytes.Buffer
	saved := verboseOut
	verboseOut = &buf
	defer func() { verboseOut = saved }()

	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func(n int) {
			defer func() { done <- struct{}{} }()
			vprintf("line %d\n", n)
		}(i)
	}
	for i := 0; i < 10; i++ {
		<-done
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 10 {
		t.Errorf("expected 10 lines, got %d:\n%s", len(lines), buf.String())
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, "line ") {
			t.Errorf("unexpected line format: %q", line)
		}
	}
}
