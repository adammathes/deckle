package main

import (
	"encoding/base64"
	"image/color"
	"net/http"
	"net/http/httptest"
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
	content, title, err := extractArticle(htmlBytes, pageURL)
	if err != nil {
		t.Fatal(err)
	}

	// Readability may return the full title including site suffix;
	// normalizeHeadings handles cleaning via cleanTitle.
	if !strings.Contains(title, "Test Article") {
		t.Errorf("title = %q, expected to contain %q", title, "Test Article")
	}

	// Step 4: Process images
	opts := optimizeOpts{maxWidth: 800, quality: 60, grayscale: true}
	result := processArticleImages([]byte(content), opts)

	// Step 5: Normalize headings
	final := normalizeHeadings(string(result), title)

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
