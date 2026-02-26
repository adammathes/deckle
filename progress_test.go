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

// withProgressCapture enables progress output to a buffer, runs fn, and
// returns the captured output. run() creates its own tracker via startProgress.
func withProgressCapture(fn func()) string {
	var buf bytes.Buffer
	savedProgress := progress
	savedOut := progressOut
	progressOut = &buf
	defer func() {
		progress = savedProgress
		progressOut = savedOut
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

	if !strings.Contains(output, "DONE") {
		t.Errorf("expected DONE in progress output, got:\n%s", output)
	}
	if !strings.Contains(output, "wrote") {
		t.Errorf("expected 'wrote' in progress output, got:\n%s", output)
	}
}

func TestProgress_SingleURL_NoOutput_NoProgress(t *testing.T) {
	// When no -o flag is set, progressOut should remain io.Discard.
	saved := progressOut
	progressOut = io.Discard
	defer func() { progressOut = saved }()

	savedP := progress
	progress = nil
	defer func() { progress = savedP }()

	// Verify helper functions are safe to call with nil progress.
	progressArticleDone()
	progressArticleFailed()
	progressAddImages(5)
	progressImageDone()
	finishProgress("DONE")
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

	// Should show downloaded article progress
	if !strings.Contains(output, "downloaded") {
		t.Errorf("expected 'downloaded' in progress, got:\n%s", output)
	}
	// Should reference article count
	if !strings.Contains(output, "3") {
		t.Errorf("expected article count '3' in progress, got:\n%s", output)
	}
	// Should show DONE with output path
	if !strings.Contains(output, "DONE") {
		t.Errorf("expected DONE in progress, got:\n%s", output)
	}
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

	// Should show DONE with failure count
	if !strings.Contains(output, "DONE") {
		t.Errorf("expected DONE in progress, got:\n%s", output)
	}
	if !strings.Contains(output, "failed") {
		t.Errorf("expected 'failed' count in progress, got:\n%s", output)
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

	// Should show image optimization progress
	if !strings.Contains(output, "images") {
		t.Errorf("expected 'images' in progress output, got:\n%s", output)
	}
	if !strings.Contains(output, "optimizing") {
		t.Errorf("expected 'optimizing' in image progress, got:\n%s", output)
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

	// External images get embedded then optimized — should show image counter
	if !strings.Contains(output, "images") {
		t.Errorf("expected 'images' in progress output, got:\n%s", output)
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

	if !strings.Contains(output, "fetching") {
		t.Errorf("expected 'fetching' in markdown single-URL progress, got:\n%s", output)
	}
	if !strings.Contains(output, "DONE") {
		t.Errorf("expected 'DONE' in markdown single-URL progress, got:\n%s", output)
	}
	if !strings.Contains(output, "wrote") {
		t.Errorf("expected 'wrote' in markdown single-URL progress, got:\n%s", output)
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

	if !strings.Contains(output, "downloaded") {
		t.Errorf("expected 'downloaded' in progress, got:\n%s", output)
	}
	if !strings.Contains(output, "DONE") {
		t.Errorf("expected 'DONE' in markdown multi-URL progress, got:\n%s", output)
	}
}

func TestProgress_ConcurrentSafety(t *testing.T) {
	// Verify concurrent articleDone/imageDone calls don't panic.
	var buf bytes.Buffer
	savedProgress := progress
	savedOut := progressOut
	progressOut = &buf
	progress = newStatusTracker(&buf, 10)
	defer func() {
		progress = savedProgress
		progressOut = savedOut
	}()

	progressAddImages(20)
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			progressArticleDone()
			progressImageDone()
			progressImageDone()
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}

	finishProgress("DONE")
	output := buf.String()
	if !strings.Contains(output, "DONE") {
		t.Errorf("expected DONE in concurrent progress output, got:\n%s", output)
	}
}

func TestStatusTracker_Redraw(t *testing.T) {
	var buf bytes.Buffer
	s := newStatusTracker(&buf, 5)

	// Initial state: no articles done, no images
	s.mu.Lock()
	s.redraw()
	s.mu.Unlock()
	out := buf.String()
	if !strings.Contains(out, "downloaded 0/5") {
		t.Errorf("expected 'downloaded 0/5' in initial redraw, got: %q", out)
	}

	// After some progress
	buf.Reset()
	s.articleDone()
	s.addImages(8)
	s.imageDone()
	out = buf.String()
	if !strings.Contains(out, "downloaded 1/5") {
		t.Errorf("expected 'downloaded 1/5', got: %q", out)
	}
	if !strings.Contains(out, "optimizing 1/8 images") {
		t.Errorf("expected 'optimizing 1/8 images', got: %q", out)
	}
}

func TestStatusTracker_SingleArticle(t *testing.T) {
	var buf bytes.Buffer
	s := newStatusTracker(&buf, 1)

	// Single article, no images yet: should show [fetching]
	s.mu.Lock()
	s.redraw()
	s.mu.Unlock()
	out := buf.String()
	if !strings.Contains(out, "[fetching]") {
		t.Errorf("expected '[fetching]' for single article, got: %q", out)
	}

	// After images discovered
	buf.Reset()
	s.addImages(3)
	out = buf.String()
	if !strings.Contains(out, "optimizing 0/3 images") {
		t.Errorf("expected 'optimizing 0/3 images', got: %q", out)
	}
}

func TestStatusTracker_Finish(t *testing.T) {
	var buf bytes.Buffer
	s := newStatusTracker(&buf, 1)
	s.finish("DONE -- wrote output.html")
	out := buf.String()
	if !strings.Contains(out, "DONE -- wrote output.html") {
		t.Errorf("expected finish message, got: %q", out)
	}
	if !strings.HasSuffix(out, "\n") {
		t.Error("finish should end with newline")
	}
}
