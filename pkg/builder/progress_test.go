package builder

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

func TestVerbose_SingleURL_WithOutput(t *testing.T) {
	pageHTML := `<!DOCTYPE html>
<html><head><title>Single Test</title></head><body>
<article>
<h1>Single Test</h1>
<p>This is a test article for single URL verbose testing. It has enough
content for readability to extract it as the main article. More text here.</p>
<p>Second paragraph with additional content for readability.</p>
</article>
</body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(pageHTML))
	}))
	defer srv.Close()

	var buf bytes.Buffer
	outFile := filepath.Join(t.TempDir(), "output.html")
	opts := Options{
		MaxWidth:  800,
		Quality:   60,
		Output:    outFile,
		Format:    "html",
		Timeout:   5 * time.Second,
		UserAgent: "test-agent",
		Verbose:   true,
		LogWriter: &buf,
		Writer:    io.Discard,
	}

	if err := Build([]string{srv.URL}, opts); err != nil {
		t.Fatal(err)
	}

	output := buf.String()
	if !strings.Contains(output, "Fetching 1 URL") {
		t.Errorf("expected 'Fetching 1 URL' in verbose output, got:\n%s", output)
	}
}

func TestVerbose_EpubMode_MultipleArticles(t *testing.T) {
	articlesByPath := map[string]string{
		"/1": `<!DOCTYPE html><html><head><title>Article One</title></head><body>
		<article><h1>Article One</h1>
		<p>First article content for test. It has enough content for
		readability to properly extract the main content region.</p>
		<p>Second paragraph for content density.</p></article></body></html>`,
		"/2": `<!DOCTYPE html><html><head><title>Article Two</title></head><body>
		<article><h1>Article Two</h1>
		<p>Second article content for test. More content needed for
		readability to extract this as the main article properly.</p>
		<p>Additional paragraph for the algorithm.</p></article></body></html>`,
		"/3": `<!DOCTYPE html><html><head><title>Article Three</title></head><body>
		<article><h1>Article Three</h1>
		<p>Third article content for test. Enough text for readability
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

	var buf bytes.Buffer
	outFile := filepath.Join(t.TempDir(), "test.epub")
	opts := Options{
		MaxWidth:  800,
		Quality:   60,
		Output:    outFile,
		Format:    "epub",
		Timeout:   5 * time.Second,
		UserAgent: "test-agent",
		Verbose:   true,
		LogWriter: &buf,
		Writer:    io.Discard,
	}

	if err := Build([]string{srv.URL + "/1", srv.URL + "/2", srv.URL + "/3"}, opts); err != nil {
		t.Fatal(err)
	}

	output := buf.String()
	if !strings.Contains(output, "Fetching 3 URLs") {
		t.Errorf("expected 'Fetching 3 URLs' in verbose, got:\n%s", output)
	}
	if !strings.Contains(output, "Building epub at") {
		t.Errorf("expected 'Building epub at' in verbose, got:\n%s", output)
	}
}

func TestVerbose_WithImages(t *testing.T) {
	imgData := makePNG(1200, 900, color.NRGBA{200, 100, 50, 255})
	imgURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString(imgData)

	pageHTML := fmt.Sprintf(`<!DOCTYPE html>
<html><head><title>Image Test</title></head><body>
<article>
<h1>Image Test</h1>
<p>Article with images for testing. It has enough content for
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

	var buf bytes.Buffer
	outFile := filepath.Join(t.TempDir(), "img-test.html")
	opts := Options{
		MaxWidth:  800,
		Quality:   60,
		Output:    outFile,
		Format:    "html",
		Timeout:   5 * time.Second,
		UserAgent: "test-agent",
		Verbose:   true,
		LogWriter: &buf,
		Writer:    io.Discard,
	}

	if err := Build([]string{srv.URL}, opts); err != nil {
		t.Fatal(err)
	}

	output := buf.String()
	// Should show aggregate image line
	if !strings.Contains(output, "embedding") {
		t.Errorf("expected 'embedding' in verbose output, got:\n%s", output)
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
<html><head><title>External Images Test</title></head><body>
<article>
<h1>External Images Test</h1>
<p>Article with external images for testing. Enough content for
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

	var buf bytes.Buffer
	outFile := filepath.Join(t.TempDir(), "ext-test.html")
	opts := Options{
		MaxWidth:  800,
		Quality:   60,
		Output:    outFile,
		Format:    "html",
		Timeout:   5 * time.Second,
		UserAgent: "test-agent",
		Verbose:   true,
		LogWriter: &buf,
		Writer:    io.Discard,
	}

	if err := Build([]string{srv.URL}, opts); err != nil {
		t.Fatal(err)
	}

	output := buf.String()
	// Should show aggregate image count
	if !strings.Contains(output, "embedding") {
		t.Errorf("expected 'embedding' in verbose output, got:\n%s", output)
	}
}

func TestVerbose_MarkdownMode_SingleURL(t *testing.T) {
	pageHTML := `<!DOCTYPE html>
<html><head><title>MD Test</title></head><body>
<article>
<h1>MD Test</h1>
<p>This is a test article for markdown verbose testing. It has enough content
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

	var buf bytes.Buffer
	outFile := filepath.Join(t.TempDir(), "output.md")
	opts := Options{
		MaxWidth:  800,
		Quality:   60,
		Output:    outFile,
		Format:    "markdown",
		Timeout:   5 * time.Second,
		UserAgent: "test-agent",
		Verbose:   true,
		LogWriter: &buf,
		Writer:    io.Discard,
	}

	if err := Build([]string{srv.URL}, opts); err != nil {
		t.Fatal(err)
	}

	output := buf.String()
	if !strings.Contains(output, "Fetching 1 URL") {
		t.Errorf("expected 'Fetching 1 URL' in verbose, got:\n%s", output)
	}
	// Markdown mode skips images, so no image line
	if strings.Contains(output, "embedding") {
		t.Errorf("markdown mode should not mention image embedding, got:\n%s", output)
	}
}

func TestVerbose_MarkdownMode_MultipleURLs(t *testing.T) {
	pageHTML := `<!DOCTYPE html>
<html><head><title>Multi MD Test</title></head><body>
<article>
<h1>Multi MD Test</h1>
<p>Test article for multi-URL markdown verbose testing. Enough content for
readability to extract as the main article content region.</p>
<p>Second paragraph for content density threshold.</p>
</article>
</body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(pageHTML))
	}))
	defer srv.Close()

	var buf bytes.Buffer
	outFile := filepath.Join(t.TempDir(), "multi.md")
	opts := Options{
		MaxWidth:  800,
		Quality:   60,
		Output:    outFile,
		Format:    "markdown",
		Timeout:   5 * time.Second,
		UserAgent: "test-agent",
		Verbose:   true,
		LogWriter: &buf,
		Writer:    io.Discard,
	}

	if err := Build([]string{srv.URL + "/a", srv.URL + "/b"}, opts); err != nil {
		t.Fatal(err)
	}

	output := buf.String()
	if !strings.Contains(output, "Fetching 2 URLs") {
		t.Errorf("expected 'Fetching 2 URLs' in verbose, got:\n%s", output)
	}
}

func TestDefaultSilence_NoOutput(t *testing.T) {
	pageHTML := `<!DOCTYPE html>
<html><head><title>Silent Test</title></head><body>
<article>
<h1>Silent Test</h1>
<p>This is a test article. It has enough content for readability to extract
it as the main article. More text here for the algorithm.</p>
<p>Second paragraph with additional content.</p>
</article>
</body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(pageHTML))
	}))
	defer srv.Close()

	var buf bytes.Buffer
	outFile := filepath.Join(t.TempDir(), "silent.html")
	opts := Options{
		MaxWidth:  800,
		Quality:   60,
		Output:    outFile,
		Format:    "html",
		Timeout:   5 * time.Second,
		UserAgent: "test-agent",
		LogWriter: &buf,
		Writer:    io.Discard,
	}

	if err := Build([]string{srv.URL}, opts); err != nil {
		t.Fatal(err)
	}

	if buf.Len() != 0 {
		t.Errorf("expected no output in default (silent) mode, got: %q", buf.String())
	}
}
