package main

import (
	"encoding/base64"
	"image/color"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	result := processArticleImages([]byte(content), opts)

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

// TestPipeline_Medium runs the full pipeline against a live Medium article.
// Skip in short mode since it requires network access.
func TestPipeline_Medium(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live network test in short mode")
	}

	rawURL := "https://steve-yegge.medium.com/welcome-to-gas-town-4f25ee16dd04"

	// Fetch
	htmlBytes, pageURL, err := fetchHTML(rawURL, 30*time.Second, defaultUA)
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}

	// Promote lazy src
	htmlBytes = promoteLazySrc(htmlBytes)

	// Extract article
	content, meta, err := extractArticle(htmlBytes, pageURL)
	if err != nil {
		t.Fatalf("readability failed: %v", err)
	}

	if !strings.Contains(meta.Title, "Gas Town") {
		t.Errorf("title = %q, expected to contain 'Gas Town'", meta.Title)
	}
	if len(content) < 1000 {
		t.Errorf("article content suspiciously small (%d chars)", len(content))
	}

	// Process images — Medium uses <picture> with external srcset URLs
	opts := optimizeOpts{maxWidth: 800, quality: 60, grayscale: true}
	result := processArticleImages([]byte(content), opts)

	// Normalize headings
	final := normalizeHeadings(string(result), meta.Title, sourceInfo{URL: rawURL, Byline: meta.Byline, SiteName: meta.SiteName})

	// Verify structure
	if !strings.Contains(final, "<h1>") {
		t.Error("expected H1 title in output")
	}
	if len(final) < 1000 {
		t.Errorf("final output suspiciously small (%d chars)", len(final))
	}

	// Verify images were fetched from Medium's picture/srcset elements
	imgCount := strings.Count(final, "<img ")
	if imgCount < 5 {
		t.Errorf("expected at least 5 images in Medium article, got %d", imgCount)
	}
	if !strings.Contains(final, "data:image/jpeg;base64,") {
		t.Error("expected embedded JPEG images from Medium's picture elements")
	}
}

// TestPipeline_DanShapiro runs the full pipeline against a WordPress site
// with lazy-loaded images. Skip in short mode.
func TestPipeline_DanShapiro(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live network test in short mode")
	}

	rawURL := "https://www.danshapiro.com/blog/2026/01/the-five-levels-from-spicy-autocomplete-to-the-software-factory/"

	htmlBytes, pageURL, err := fetchHTML(rawURL, 30*time.Second, defaultUA)
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}

	htmlBytes = promoteLazySrc(htmlBytes)

	content, meta, err := extractArticle(htmlBytes, pageURL)
	if err != nil {
		t.Fatalf("readability failed: %v", err)
	}

	if !strings.Contains(meta.Title, "Five Levels") {
		t.Errorf("title = %q, expected to contain 'Five Levels'", meta.Title)
	}

	opts := optimizeOpts{maxWidth: 800, quality: 60, grayscale: true}
	result := processArticleImages([]byte(content), opts)
	final := normalizeHeadings(string(result), meta.Title, sourceInfo{URL: rawURL, Byline: meta.Byline, SiteName: meta.SiteName})

	// This article should have images (they were lazy-loaded + external)
	if !strings.Contains(final, "data:image/jpeg;base64,") {
		t.Error("expected embedded JPEG images (lazy-loaded images should be fetched)")
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
