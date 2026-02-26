package main

import (
	"encoding/base64"
	"image/color"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFullPipeline(t *testing.T) {
	// Create a test image for embedding
	imgData := makePNG(1200, 900, color.NRGBA{200, 100, 50, 255})
	imgURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString(imgData)

	pageHTML := `<!DOCTYPE html>
<html>
<head><title>Test Article - Example Site</title></head>
<body>
	<nav><a href="/">Home</a> | <a href="/blog">Blog</a></nav>
	<article>
		<h1>Test Article</h1>
		<p>This is a test article with enough content to be considered the main readable
		content by the readability algorithm. It contains multiple paragraphs of text
		that discuss important topics at length.</p>
		<img src="` + imgURI + `" alt="test image">
		<h2>Section One</h2>
		<p>This section discusses the first major point of the article. It has substantial
		text to ensure readability keeps it as part of the main content extraction. The
		algorithm needs to see meaningful content in each section.</p>
		<p>Another paragraph in section one with more detailed discussion of the topic
		at hand. This adds more weight to the content extraction decision.</p>
		<h2>Section Two</h2>
		<p>The second section covers a different aspect. Again with enough text to be
		significant. The readability algorithm will keep this content as part of the
		main article because it appears within the article element.</p>
	</article>
	<aside><h3>Related Posts</h3><p>Some sidebar content</p></aside>
	<footer><p>Copyright 2024 Example Site</p></footer>
</body>
</html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(pageHTML))
	}))
	defer srv.Close()

	// Step 1: Fetch
	htmlBytes, pageURL, err := fetchHTML(srv.URL, 5*time.Second, "test-agent")
	if err != nil {
		t.Fatal(err)
	}

	// Step 2: Promote lazy src
	htmlBytes = promoteLazySrc(htmlBytes)

	// Step 3: Extract article
	content, meta, err := extractArticle(htmlBytes, pageURL)
	if err != nil {
		t.Fatal(err)
	}

	// Readability may return the full title including site suffix;
	// normalizeHeadings handles cleaning via cleanTitle.
	if !strings.Contains(meta.Title, "Test Article") {
		t.Errorf("title = %q, expected to contain %q", meta.Title, "Test Article")
	}

	// Step 4: Process images
	opts := optimizeOpts{maxWidth: 800, quality: 60, grayscale: true}
	result := processArticleImages([]byte(content), opts, 5)

	// Step 5: Normalize headings
	final := normalizeHeadings(string(result), meta.Title, sourceInfo{})

	// Verify: H1 title is present
	if !strings.Contains(final, "<h1>Test Article</h1>") {
		t.Error("expected H1 title in output")
	}

	// Verify: original H1 should be shifted to H2
	// (readability may restructure headings, so check for section headings shifted)
	// At minimum, the content should have shifted headings
	if strings.Contains(final, "<h1>Section") {
		t.Error("section headings should not be H1")
	}

	// Verify: image was optimized to JPEG
	if !strings.Contains(final, "data:image/jpeg;base64,") {
		t.Error("expected JPEG data URI in output (image should be optimized)")
	}

	// Verify: PNG source was replaced
	if strings.Contains(final, "data:image/png;base64,") {
		t.Error("PNG should have been replaced with JPEG")
	}

	// Verify: JPEG image is resized (800px wide, not 1200px)
	b64Start := strings.Index(final, "data:image/jpeg;base64,") + len("data:image/jpeg;base64,")
	b64End := strings.Index(final[b64Start:], `"`)
	if b64End > 0 {
		raw, err := base64.StdEncoding.DecodeString(final[b64Start : b64Start+b64End])
		if err == nil {
			w, _ := decodeJPEGDimensions(raw)
			if w != 800 {
				t.Errorf("image width = %d, want 800", w)
			}
		}
	}

	// Verify: nav and footer stripped
	if strings.Contains(final, "Copyright 2024") {
		t.Error("footer should be stripped by readability")
	}
	if strings.Contains(final, "Related Posts") {
		t.Error("sidebar should be stripped by readability")
	}
}

// TestPipeline_LazyLoadPromote verifies that data-src attributes are
// promoted to src before readability strips the page.
func TestPipeline_LazyLoadPromote(t *testing.T) {
	pageHTML := `<!DOCTYPE html><html><head><title>Lazy Test</title></head><body>
		<article>
			<h1>Lazy Test</h1>
			<p>This article has lazy-loaded images that use data-src instead of src.
			The pipeline should promote data-src to src so the images are visible
			to readability and the image optimizer. Here is enough text to satisfy
			the readability algorithm's content threshold requirements.</p>
			<img class="lazyload" data-src="https://example.com/img.jpg" alt="lazy image">
			<p>More content to ensure readability keeps this section as the main article.</p>
		</article>
	</body></html>`

	promoted := promoteLazySrc([]byte(pageHTML))
	html := string(promoted)

	// data-src should be promoted to src
	if strings.Contains(html, "data-src=") {
		t.Error("data-src should have been promoted to src")
	}
	if !strings.Contains(html, `src="https://example.com/img.jpg"`) {
		t.Error("expected src attribute with the original data-src URL")
	}
}

// TestPipeline_ReadabilityStripsBoilerplate verifies that readability
// removes navigation, footer, and sidebar content.
func TestPipeline_ReadabilityStripsBoilerplate(t *testing.T) {
	pageHTML := `<!DOCTYPE html><html><head><title>Boilerplate Test</title></head><body>
		<header><nav><a href="/">Home</a> | <a href="/blog">Blog</a> | <a href="/about">About</a></nav></header>
		<main>
			<article>
				<h1>Boilerplate Test</h1>
				<p>This is the main article content that should be preserved by readability.
				It needs to be long enough that readability identifies it as the primary content.
				More text here to increase the content density and word count.</p>
				<p>A second paragraph with additional discussion and analysis to further
				establish this as the main content area of the page. Readability uses
				text density heuristics so we need substantial text.</p>
				<p>Third paragraph continuing the article with important information
				that should definitely be kept in the final output.</p>
			</article>
		</main>
		<aside><h3>Trending</h3><ul><li>Post 1</li><li>Post 2</li></ul></aside>
		<footer><p>Copyright 2024 | Privacy Policy | Terms of Service</p></footer>
	</body></html>`

	u, _ := url.Parse("https://example.com/article")
	content, _, err := extractArticle([]byte(pageHTML), u)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(content, "main article content") {
		t.Error("expected article content to be preserved")
	}
	if strings.Contains(content, "Privacy Policy") {
		t.Error("footer should be stripped")
	}
	if strings.Contains(content, "Trending") {
		t.Error("sidebar should be stripped")
	}
}

func TestReadURLFile(t *testing.T) {
	content := "https://example.com/article1\n\n# This is a comment\nhttps://example.com/article2\n  \nhttps://example.com/article3\n"
	tmpFile := filepath.Join(t.TempDir(), "urls.txt")
	os.WriteFile(tmpFile, []byte(content), 0644)

	urls, err := readURLFile(tmpFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(urls) != 3 {
		t.Errorf("expected 3 URLs, got %d", len(urls))
	}
	expected := []string{
		"https://example.com/article1",
		"https://example.com/article2",
		"https://example.com/article3",
	}
	for i, u := range urls {
		if u != expected[i] {
			t.Errorf("urls[%d] = %q, want %q", i, u, expected[i])
		}
	}
}

func TestReadURLFile_Empty(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "empty.txt")
	os.WriteFile(tmpFile, []byte("# only comments\n\n"), 0644)

	urls, err := readURLFile(tmpFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(urls) != 0 {
		t.Errorf("expected 0 URLs, got %d", len(urls))
	}
}

func TestReadURLFile_NotFound(t *testing.T) {
	_, err := readURLFile("/nonexistent/file.txt")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestProcessURL(t *testing.T) {
	imgData := makePNG(100, 100, color.NRGBA{200, 100, 50, 255})
	imgURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString(imgData)

	pageHTML := `<!DOCTYPE html>
<html><head><title>Process Test</title></head><body>
<article>
<h1>Process Test</h1>
<p>This is a test article for the processURL function. It has enough content
for readability to extract it as the main article. More text here to ensure
the content threshold is met by the readability algorithm.</p>
<img src="` + imgURI + `" alt="test">
<p>Another paragraph with more content to satisfy readability requirements.
The article needs substantial text to be recognized properly.</p>
</article>
</body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(pageHTML))
	}))
	defer srv.Close()

	opts := optimizeOpts{maxWidth: 800, quality: 60}
	html, title, src, err := processURL(srv.URL, opts, 5*time.Second, "test-agent", "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(title, "Process Test") {
		t.Errorf("title = %q, expected to contain 'Process Test'", title)
	}
	if src.URL != srv.URL {
		t.Errorf("src.URL = %q, want %q", src.URL, srv.URL)
	}
	if !strings.Contains(html, "<h1>") {
		t.Error("expected H1 in output")
	}
}

func TestProcessURL_TitleOverride(t *testing.T) {
	pageHTML := `<!DOCTYPE html>
<html><head><title>Original Title</title></head><body>
<article>
<h1>Original Title</h1>
<p>This is a test article for title override testing. It has enough content
for readability. More text here to ensure the content threshold is properly met.</p>
<p>Second paragraph with more meaningful content that helps readability decide
this is the main article content region of the page.</p>
</article>
</body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(pageHTML))
	}))
	defer srv.Close()

	opts := optimizeOpts{maxWidth: 800, quality: 60}
	_, title, _, err := processURL(srv.URL, opts, 5*time.Second, "test-agent", "Custom Title", 5)
	if err != nil {
		t.Fatal(err)
	}
	if title != "Custom Title" {
		t.Errorf("title = %q, want 'Custom Title'", title)
	}
}

func TestProcessURL_FetchError(t *testing.T) {
	opts := optimizeOpts{maxWidth: 800, quality: 60}
	_, _, _, err := processURL("http://localhost:1/nonexistent", opts, 1*time.Second, "test-agent", "", 5)
	if err == nil {
		t.Error("expected error for unreachable URL")
	}
}

func TestRun_SingleURLMode(t *testing.T) {
	pageHTML := `<!DOCTYPE html>
<html><head><title>Run Test</title></head><body>
<article>
<h1>Run Test</h1>
<p>This is a test article for the run function. It has enough content
for readability to extract it as the main article. More text here to
ensure the content threshold is met by the readability algorithm.</p>
<p>Another paragraph with additional content for readability.</p>
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
	if !strings.Contains(string(data), "Run Test") {
		t.Error("output should contain article title")
	}
}

func TestRun_SingleURLMode_NoOutput(t *testing.T) {
	pageHTML := `<!DOCTYPE html>
<html><head><title>Stdout Test</title></head><body>
<article>
<h1>Stdout Test</h1>
<p>This is a test article that will be written to stdout. It has enough
content for readability to extract it as the main article properly.</p>
<p>Another paragraph for the readability algorithm threshold.</p>
</article>
</body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(pageHTML))
	}))
	defer srv.Close()

	// No output file - goes to stdout
	cfg := cliConfig{
		opts:      optimizeOpts{maxWidth: 800, quality: 60},
		format:    "html",
		timeout:   5 * time.Second,
		userAgent: "test-agent",
		args:      []string{srv.URL},
	}

	// Redirect stdout for this test
	err := run(cfg)
	if err != nil {
		t.Fatal(err)
	}
}

func TestRun_EpubMode(t *testing.T) {
	pageHTML := `<!DOCTYPE html>
<html><head><title>Epub Test</title></head><body>
<article>
<h1>Epub Test</h1>
<p>This is a test article for epub mode. It has enough content for
readability to extract it as the main article content. More text here.</p>
<p>Second paragraph with additional content for the readability algorithm.</p>
</article>
</body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(pageHTML))
	}))
	defer srv.Close()

	outFile := filepath.Join(t.TempDir(), "test.epub")
	cfg := cliConfig{
		opts:      optimizeOpts{maxWidth: 800, quality: 60},
		output:    outFile,
		format:    "epub",
		timeout:   5 * time.Second,
		userAgent: "test-agent",
		args:      []string{srv.URL},
	}

	err := run(cfg)
	if err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(outFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() < 100 {
		t.Error("epub file too small")
	}
}

func TestRun_EpubMode_WithTxtFile(t *testing.T) {
	pageHTML := `<!DOCTYPE html>
<html><head><title>TXT Test</title></head><body>
<article>
<h1>TXT Test</h1>
<p>This is a test article loaded via a txt file. It has enough content
for readability to work with. More padding text for the algorithm.</p>
<p>Additional paragraph for content density threshold.</p>
</article>
</body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(pageHTML))
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	urlFile := filepath.Join(tmpDir, "reading-list.txt")
	os.WriteFile(urlFile, []byte(srv.URL+"\n"), 0644)

	outFile := filepath.Join(tmpDir, "test.epub")
	cfg := cliConfig{
		opts:      optimizeOpts{maxWidth: 800, quality: 60},
		output:    outFile,
		format:    "epub",
		timeout:   5 * time.Second,
		userAgent: "test-agent",
		args:      []string{urlFile},
	}

	err := run(cfg)
	if err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(outFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() < 100 {
		t.Error("epub file too small")
	}
}

func TestRun_EpubMode_MultipleArticles(t *testing.T) {
	articlesByPath := map[string]string{
		"/1": `<!DOCTYPE html><html><head><title>Article One</title></head><body>
		<article><h1>Article One</h1>
		<p>First article content for multi-article epub test. It has enough content
		for readability to properly extract the main content region.</p>
		<p>Second paragraph for content density.</p></article></body></html>`,
		"/2": `<!DOCTYPE html><html><head><title>Article Two</title></head><body>
		<article><h1>Article Two</h1>
		<p>Second article content for multi-article epub test. More content needed
		for readability to extract this as the main article properly.</p>
		<p>Additional paragraph for the algorithm.</p></article></body></html>`,
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

	outFile := filepath.Join(t.TempDir(), "multi.epub")
	cfg := cliConfig{
		opts:          optimizeOpts{maxWidth: 800, quality: 60},
		output:        outFile,
		titleOverride: "Multi Book",
		format:        "epub",
		timeout:       5 * time.Second,
		userAgent:     "test-agent",
		args:          []string{srv.URL + "/1", srv.URL + "/2"},
	}

	err := run(cfg)
	if err != nil {
		t.Fatal(err)
	}
}

func TestRun_EpubMode_NoOutput(t *testing.T) {
	cfg := cliConfig{
		opts:   optimizeOpts{maxWidth: 800, quality: 60},
		format: "epub",
		args:   []string{"https://example.com"},
	}
	err := run(cfg)
	if err == nil {
		t.Error("expected error when epub mode has no output")
	}
}

func TestRun_EpubMode_NoArgs(t *testing.T) {
	cfg := cliConfig{
		opts:   optimizeOpts{maxWidth: 800, quality: 60},
		output: "out.epub",
		format: "epub",
		args:   []string{},
	}
	err := run(cfg)
	if err == nil {
		t.Error("expected error when epub mode has no args")
	}
}

func TestRun_NoArgs(t *testing.T) {
	cfg := cliConfig{
		opts:   optimizeOpts{maxWidth: 800, quality: 60},
		format: "html",
		args:   []string{},
	}
	err := run(cfg)
	if err == nil {
		t.Error("expected error when no args provided")
	}
}

func TestRun_UnknownFormat(t *testing.T) {
	cfg := cliConfig{
		opts:   optimizeOpts{maxWidth: 800, quality: 60},
		format: "pdf",
		args:   []string{"https://example.com"},
	}
	err := run(cfg)
	if err == nil {
		t.Error("expected error for unknown format")
	}
	if !strings.Contains(err.Error(), "unknown format") {
		t.Errorf("expected 'unknown format' error, got: %v", err)
	}
}

func TestRun_DefaultFormatIsMarkdown(t *testing.T) {
	// When no format is set, default should be markdown
	cfg := cliConfig{
		opts: optimizeOpts{maxWidth: 800, quality: 60},
		args: []string{},
	}
	err := run(cfg)
	// Will fail because no URLs, but the error should be about URLs, not format
	if err == nil {
		t.Error("expected error")
	}
	if strings.Contains(err.Error(), "format") {
		t.Errorf("default format should be valid, got: %v", err)
	}
}

func TestWriteOutput_File(t *testing.T) {
	outFile := filepath.Join(t.TempDir(), "out.txt")
	err := writeOutput(outFile, "hello world")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello world" {
		t.Errorf("got %q, want %q", string(data), "hello world")
	}
}

func TestWriteOutput_FileError(t *testing.T) {
	// Writing to a nonexistent directory should fail
	err := writeOutput("/nonexistent/dir/file.txt", "hello")
	if err == nil {
		t.Error("expected error for nonexistent directory")
	}
}

func TestWriteOutput_Stdout(t *testing.T) {
	// writeOutput with empty path writes to stdout; just ensure no error
	// when stdout is valid (os.Pipe is a valid fd).
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	savedStdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = savedStdout }()

	err = writeOutput("", "test output")
	w.Close()
	if err != nil {
		t.Fatalf("writeOutput to stdout pipe: %v", err)
	}

	buf := make([]byte, 256)
	n, _ := r.Read(buf)
	if string(buf[:n]) != "test output" {
		t.Errorf("got %q, want %q", string(buf[:n]), "test output")
	}
}

func TestMain(m *testing.M) {
	// Enable local fetching for all tests by default, so existing tests using httptest pass.
	// Security tests (e.g. TestSSRFProtection) should explicitly unset this variable.
	os.Setenv("DECKLE_TEST_ALLOW_LOCAL", "1")
	os.Exit(m.Run())
}
