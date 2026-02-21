// Integration tests using local synthetic data — no network required.
// These exercise the full pipeline end-to-end: fetch → readability →
// image optimization → heading normalization → epub generation,
// and provide benchmarks for performance tracking.
package main

import (
	"archive/zip"
	"encoding/base64"
	"fmt"
	"image/color"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ---------- helpers ----------

// syntheticArticle builds a realistic HTML page with configurable content.
type syntheticArticle struct {
	title       string
	byline      string
	siteName    string
	paragraphs  int           // number of body paragraphs
	images      []syntheticImg // images to embed
	headings    []string      // sub-headings (h2)
	hasLazyImgs bool          // use data-src instead of src for external imgs
}

type syntheticImg struct {
	width, height int
	mime          string // "png" or "jpeg"
	external      bool   // if true, use a URL placeholder (filled in by server)
}

func (a syntheticArticle) render(srvURL string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf(`<!DOCTYPE html>
<html><head>
<title>%s - Test Site</title>
<meta name="author" content="%s">
<meta property="og:site_name" content="%s">
</head><body>
<header><nav><a href="/">Home</a> | <a href="/blog">Blog</a></nav></header>
<main><article>
<h1>%s</h1>
`, a.title, a.byline, a.siteName, a.title))

	imgIdx := 0
	for i := 0; i < a.paragraphs; i++ {
		b.WriteString(fmt.Sprintf(`<p>Paragraph %d of the article about %s. This paragraph contains
enough text to satisfy the readability algorithm's content density heuristics.
The readability extractor needs substantial content in each section to correctly
identify this as the main article region of the page. Additional filler text
ensures the word count is high enough for reliable extraction.</p>
`, i+1, a.title))

		// Insert a heading every few paragraphs
		if len(a.headings) > 0 && i > 0 && i%3 == 0 {
			hIdx := (i / 3) - 1
			if hIdx < len(a.headings) {
				b.WriteString(fmt.Sprintf("<h2>%s</h2>\n", a.headings[hIdx]))
			}
		}

		// Insert images evenly distributed through the article
		if imgIdx < len(a.images) && i > 0 && (i-1)%(max(a.paragraphs/max(len(a.images), 1), 1)) == 0 {
			img := a.images[imgIdx]
			if img.external {
				url := fmt.Sprintf("%s/img/%d.%s", srvURL, imgIdx, img.mime)
				if a.hasLazyImgs {
					b.WriteString(fmt.Sprintf(`<img data-src="%s" src="data:image/svg+xml;base64,PHN2Zz4=" alt="image %d">`, url, imgIdx))
				} else {
					b.WriteString(fmt.Sprintf(`<img src="%s" alt="image %d">`, url, imgIdx))
				}
			} else {
				var data []byte
				if img.mime == "jpeg" {
					data = makeJPEG(img.width, img.height, color.NRGBA{100, 150, 200, 255})
				} else {
					data = makePNG(img.width, img.height, color.NRGBA{100, 150, 200, 255})
				}
				uri := "data:" + "image/" + img.mime + ";base64," + base64.StdEncoding.EncodeToString(data)
				b.WriteString(fmt.Sprintf(`<img src="%s" alt="image %d">`, uri, imgIdx))
			}
			b.WriteByte('\n')
			imgIdx++
		}
	}

	b.WriteString(`</article></main>
<aside><h3>Sidebar</h3><p>Unrelated sidebar content that should be stripped.</p></aside>
<footer><p>Copyright 2024 Test Site | Privacy Policy</p></footer>
</body></html>`)
	return b.String()
}

// serveArticles returns an httptest server that serves different HTML pages
// based on path and synthetic image data on /img/ paths.
func serveArticles(articles map[string]string, images map[string][]byte) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/img/") && images != nil {
			key := strings.TrimPrefix(r.URL.Path, "/img/")
			if data, ok := images[key]; ok {
				if strings.HasSuffix(key, ".jpeg") {
					w.Header().Set("Content-Type", "image/jpeg")
				} else {
					w.Header().Set("Content-Type", "image/png")
				}
				w.Write(data)
				return
			}
			w.WriteHeader(404)
			return
		}

		path := r.URL.Path
		if path == "/" {
			path = "/index"
		}
		if html, ok := articles[path]; ok {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(html))
			return
		}
		// Default: serve first article
		for _, html := range articles {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(html))
			return
		}
		w.WriteHeader(404)
	}))
}

// readZipFile reads a file from a zip archive by name.
func readZipFile(zr *zip.ReadCloser, name string) (string, bool) {
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				return "", false
			}
			defer rc.Close()
			data, err := io.ReadAll(rc)
			if err != nil {
				return "", false
			}
			return string(data), true
		}
	}
	return "", false
}

// ---------- integration tests ----------

// TestIntegration_RichArticle exercises the full pipeline with a realistic
// article containing metadata, multiple images, sub-headings, and boilerplate.
func TestIntegration_RichArticle(t *testing.T) {
	art := syntheticArticle{
		title:      "Understanding Modern E-Readers",
		byline:     "Jane Author",
		siteName:   "Tech Review",
		paragraphs: 12,
		images: []syntheticImg{
			{1200, 900, "png", false},  // embedded, oversized → should resize
			{400, 300, "jpeg", false},   // embedded, small → no resize
			{1600, 1200, "png", false},  // embedded, large → should resize
		},
		headings: []string{"Display Technology", "Battery Life", "Software Ecosystem"},
	}

	srv := serveArticles(map[string]string{"/": art.render("")}, nil)
	defer srv.Close()

	// Run full pipeline
	opts := optimizeOpts{maxWidth: 800, quality: 60, grayscale: true}
	html, title, src, err := processURL(srv.URL, opts, 5*time.Second, "test-agent", "")
	if err != nil {
		t.Fatal(err)
	}

	// Title extraction
	if !strings.Contains(title, "Understanding Modern E-Readers") {
		t.Errorf("title = %q, want to contain article title", title)
	}

	// Source info
	if src.URL != srv.URL {
		t.Errorf("src.URL = %q, want %q", src.URL, srv.URL)
	}

	// H1 inserted, original headings shifted
	if !strings.Contains(html, "<h1>") {
		t.Error("expected H1 title in output")
	}
	if strings.Count(html, "<h1>") != 1 {
		t.Errorf("expected exactly 1 H1, got %d", strings.Count(html, "<h1>"))
	}

	// Images optimized to JPEG
	jpegCount := strings.Count(html, "data:image/jpeg;base64,")
	if jpegCount < 3 {
		t.Errorf("expected at least 3 optimized JPEG images, got %d", jpegCount)
	}

	// No raw PNG data should remain (all converted to JPEG)
	if strings.Contains(html, "data:image/png;base64,") {
		t.Error("PNG images should be converted to JPEG")
	}

	// Boilerplate stripped
	if strings.Contains(html, "Privacy Policy") {
		t.Error("footer boilerplate should be stripped")
	}
	if strings.Contains(html, "Sidebar") || strings.Contains(html, "Unrelated sidebar") {
		t.Error("sidebar should be stripped")
	}

	// Sub-headings present (shifted to h3 since readability may restructure)
	for _, h := range art.headings {
		if !strings.Contains(html, h) {
			t.Errorf("expected heading %q in output", h)
		}
	}
}

// TestIntegration_ExternalImages exercises the pipeline with external images
// served from a local httptest server, including lazy-loaded images.
func TestIntegration_ExternalImages(t *testing.T) {
	imgData := map[string][]byte{
		"0.png":  makePNG(1000, 750, color.NRGBA{255, 0, 0, 255}),
		"1.jpeg": makeJPEG(800, 600, color.NRGBA{0, 255, 0, 255}),
		"2.png":  makePNG(500, 400, color.NRGBA{0, 0, 255, 255}),
	}

	art := syntheticArticle{
		title:       "External Image Test",
		byline:      "Photo Editor",
		siteName:    "Gallery Blog",
		paragraphs:  9,
		images: []syntheticImg{
			{1000, 750, "png", true},
			{800, 600, "jpeg", true},
			{500, 400, "png", true},
		},
		hasLazyImgs: true,
		headings:    []string{"First Gallery", "Second Gallery"},
	}

	// Need to render after server is up (URLs need server address)
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/img/") {
			key := strings.TrimPrefix(r.URL.Path, "/img/")
			if data, ok := imgData[key]; ok {
				if strings.HasSuffix(key, ".jpeg") {
					w.Header().Set("Content-Type", "image/jpeg")
				} else {
					w.Header().Set("Content-Type", "image/png")
				}
				w.Write(data)
				return
			}
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(art.render(srv.URL)))
	}))
	defer srv.Close()

	// Replace global image client for test
	saved := fetchImageClient
	fetchImageClient = srv.Client()
	defer func() { fetchImageClient = saved }()

	opts := optimizeOpts{maxWidth: 800, quality: 60, grayscale: false}
	html, _, _, err := processURL(srv.URL, opts, 5*time.Second, "test-agent", "")
	if err != nil {
		t.Fatal(err)
	}

	// No data-src should remain (lazy loading promoted)
	if strings.Contains(html, "data-src=") {
		t.Error("data-src attributes should be promoted to src")
	}
	// At least some images should be fetched, embedded, and optimized
	embeddedCount := strings.Count(html, "data:image/jpeg;base64,")
	if embeddedCount < 1 {
		t.Errorf("expected at least 1 embedded JPEG image, got %d", embeddedCount)
	}
	t.Logf("Embedded %d external images as JPEG", embeddedCount)
}

// TestIntegration_TextOnlyArticle verifies the pipeline handles articles
// with no images at all.
func TestIntegration_TextOnlyArticle(t *testing.T) {
	art := syntheticArticle{
		title:      "A Pure Text Essay",
		byline:     "Essayist",
		siteName:   "Literary Review",
		paragraphs: 8,
		images:     nil, // no images
		headings:   []string{"Introduction", "Argument"},
	}

	srv := serveArticles(map[string]string{"/": art.render("")}, nil)
	defer srv.Close()

	opts := optimizeOpts{maxWidth: 800, quality: 60}
	html, title, _, err := processURL(srv.URL, opts, 5*time.Second, "test-agent", "")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(title, "Pure Text Essay") {
		t.Errorf("title = %q, want to contain article title", title)
	}
	if strings.Contains(html, "<img") {
		t.Error("text-only article should have no img tags")
	}
	if !strings.Contains(html, "<h1>") {
		t.Error("expected H1 title")
	}
	if !strings.Contains(html, "Introduction") {
		t.Error("sub-headings should be preserved")
	}
}

// TestIntegration_ManyImages exercises the pipeline with a large number of
// embedded images to verify performance and correctness at scale.
func TestIntegration_ManyImages(t *testing.T) {
	var images []syntheticImg
	for i := 0; i < 15; i++ {
		images = append(images, syntheticImg{
			width: 600 + (i * 50), height: 400 + (i * 30),
			mime: "png", external: false,
		})
	}

	art := syntheticArticle{
		title:      "Image-Heavy Photo Essay",
		byline:     "Photographer",
		siteName:   "Photo Journal",
		paragraphs: 30,
		images:     images,
		headings:   []string{"Morning", "Afternoon", "Evening", "Night", "Dawn", "Twilight", "Midnight"},
	}

	srv := serveArticles(map[string]string{"/": art.render("")}, nil)
	defer srv.Close()

	start := time.Now()
	opts := optimizeOpts{maxWidth: 800, quality: 60, grayscale: true}
	html, _, _, err := processURL(srv.URL, opts, 10*time.Second, "test-agent", "")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatal(err)
	}

	jpegCount := strings.Count(html, "data:image/jpeg;base64,")
	if jpegCount < 10 {
		t.Errorf("expected at least 10 optimized images, got %d", jpegCount)
	}

	t.Logf("Processed %d images in %v", jpegCount, elapsed)

	// Sanity check: shouldn't take more than 30s for 15 images
	if elapsed > 30*time.Second {
		t.Errorf("image processing took too long: %v", elapsed)
	}
}

// TestIntegration_MultiArticleEpub exercises the full epub pipeline with
// multiple synthetic articles, verifying TOC, metadata, images, and structure.
func TestIntegration_MultiArticleEpub(t *testing.T) {
	articles := []syntheticArticle{
		{
			title: "The Art of Reading", byline: "Alice Writer", siteName: "Book Review",
			paragraphs: 8,
			images:     []syntheticImg{{800, 600, "png", false}, {600, 400, "jpeg", false}},
			headings:   []string{"Chapter Overview"},
		},
		{
			title: "Digital Minimalism", byline: "Bob Author", siteName: "Tech Blog",
			paragraphs: 6,
			images:     nil, // text-only article
			headings:   []string{"The Problem"},
		},
		{
			title: "Weekend Cooking", byline: "Chef Carlos", siteName: "Food Magazine",
			paragraphs: 10,
			images:     []syntheticImg{{1200, 900, "jpeg", false}, {1000, 800, "png", false}, {900, 700, "png", false}},
			headings:   []string{"Appetizers", "Main Course", "Dessert"},
		},
	}

	// Serve each article on a different path
	pages := make(map[string]string)
	for i, art := range articles {
		path := fmt.Sprintf("/article/%d", i+1)
		pages[path] = art.render("")
	}
	srv := serveArticles(pages, nil)
	defer srv.Close()

	tmpDir := t.TempDir()

	// Write URL file
	urlFile := filepath.Join(tmpDir, "reading-list.txt")
	var urlContent strings.Builder
	for i := range articles {
		fmt.Fprintf(&urlContent, "%s/article/%d\n", srv.URL, i+1)
	}
	os.WriteFile(urlFile, []byte(urlContent.String()), 0644)

	outFile := filepath.Join(tmpDir, "reading-list.epub")
	cfg := cliConfig{
		opts:      optimizeOpts{maxWidth: 800, quality: 60, grayscale: true},
		output:    outFile,
		timeout:   10 * time.Second,
		userAgent: "test-agent",
		epubMode:  true,
		args:      []string{urlFile},
	}

	err := run(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Open and inspect epub
	zr, err := zip.OpenReader(outFile)
	if err != nil {
		t.Fatalf("not a valid zip: %v", err)
	}
	defer zr.Close()

	fileNames := map[string]bool{}
	for _, f := range zr.File {
		fileNames[f.Name] = true
	}

	// Verify expected structure
	expectedFiles := []string{
		"EPUB/xhtml/contents.xhtml",
		"EPUB/xhtml/article001.xhtml",
		"EPUB/xhtml/article002.xhtml",
		"EPUB/xhtml/article003.xhtml",
	}
	for _, name := range expectedFiles {
		if !fileNames[name] {
			t.Errorf("missing expected file: %s", name)
		}
	}

	// Verify TOC content
	toc, ok := readZipFile(zr, "EPUB/xhtml/contents.xhtml")
	if !ok {
		t.Fatal("could not read contents.xhtml")
	}
	for _, art := range articles {
		if !strings.Contains(toc, art.title) {
			t.Errorf("TOC should contain title %q", art.title)
		}
		if art.byline != "" && !strings.Contains(toc, art.byline) {
			t.Errorf("TOC should contain byline %q", art.byline)
		}
		if art.siteName != "" && !strings.Contains(toc, art.siteName) {
			t.Errorf("TOC should contain site name %q", art.siteName)
		}
	}
	if !strings.Contains(toc, "article001.xhtml") {
		t.Error("TOC should link to article001.xhtml")
	}

	// Verify article 1 has images
	art1, ok := readZipFile(zr, "EPUB/xhtml/article001.xhtml")
	if !ok {
		t.Fatal("could not read article001.xhtml")
	}
	if !strings.Contains(art1, "<img") {
		t.Error("article 1 should contain images")
	}
	if !strings.Contains(art1, "Art of Reading") {
		t.Error("article 1 should contain its title")
	}

	// Verify article 2 is text-only
	art2, ok := readZipFile(zr, "EPUB/xhtml/article002.xhtml")
	if !ok {
		t.Fatal("could not read article002.xhtml")
	}
	if !strings.Contains(art2, "Digital Minimalism") {
		t.Error("article 2 should contain its title")
	}

	// Verify article 3 has images (should have embedded image files)
	hasArt3Img := false
	for name := range fileNames {
		if strings.Contains(name, "ch003_img") {
			hasArt3Img = true
			break
		}
	}
	if !hasArt3Img {
		t.Error("article 3 should have embedded image files")
	}

	// Book title should be derived from txt filename
	// (reading-list.txt → "reading-list")
	t.Logf("epub size: %d bytes", mustFileSize(outFile))
	t.Logf("epub files: %d", len(fileNames))
}

func mustFileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

// TestIntegration_BrokenImages verifies graceful degradation when some
// images fail to load (404) while others succeed.
func TestIntegration_BrokenImages(t *testing.T) {
	goodImg := makePNG(100, 100, color.NRGBA{0, 255, 0, 255})
	var requestCount atomic.Int32

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/img/") {
			requestCount.Add(1)
			if r.URL.Path == "/img/good.png" {
				w.Header().Set("Content-Type", "image/png")
				w.Write(goodImg)
			} else {
				w.WriteHeader(404) // broken image
			}
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(fmt.Sprintf(`<!DOCTYPE html>
<html><head><title>Broken Image Test</title></head><body>
<article>
<h1>Broken Image Test</h1>
<p>This article has images where some are broken (404) and some work.
The pipeline should gracefully handle failures and still produce output.
More text here for readability content density threshold requirements.</p>
<img src="%s/img/good.png" alt="good image">
<p>Another paragraph of content for readability. This text ensures the
article is long enough to be properly extracted by the algorithm.</p>
<img src="%s/img/broken.png" alt="broken image">
<p>Final paragraph with more content for the readability extractor.</p>
</article>
</body></html>`, srv.URL, srv.URL)))
	}))
	defer srv.Close()

	saved := fetchImageClient
	fetchImageClient = srv.Client()
	defer func() { fetchImageClient = saved }()

	opts := optimizeOpts{maxWidth: 800, quality: 60}
	html, _, _, err := processURL(srv.URL, opts, 5*time.Second, "test-agent", "")
	if err != nil {
		t.Fatal(err)
	}

	// Should still produce output despite broken image
	if !strings.Contains(html, "<h1>") {
		t.Error("expected H1 title despite broken images")
	}

	// Good image should be embedded
	if !strings.Contains(html, "data:image/jpeg;base64,") {
		t.Error("good image should be fetched and embedded")
	}

	// Pipeline should not fail
	if html == "" {
		t.Error("output should not be empty")
	}
}

// TestIntegration_LargeArticle tests the pipeline with a very long article
// to verify memory and performance are acceptable.
func TestIntegration_LargeArticle(t *testing.T) {
	art := syntheticArticle{
		title:      "An Extremely Long-Form Investigation",
		byline:     "Investigative Reporter",
		siteName:   "Long Reads",
		paragraphs: 100, // ~100 paragraphs of substantial text
		images:     []syntheticImg{{800, 600, "jpeg", false}},
		headings: []string{
			"Background", "Discovery", "Investigation", "Evidence",
			"Analysis", "Impact", "Response", "Conclusion",
		},
	}

	srv := serveArticles(map[string]string{"/": art.render("")}, nil)
	defer srv.Close()

	start := time.Now()
	opts := optimizeOpts{maxWidth: 800, quality: 60}
	html, _, _, err := processURL(srv.URL, opts, 10*time.Second, "test-agent", "")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatal(err)
	}

	if len(html) < 10000 {
		t.Errorf("expected large output, got %d bytes", len(html))
	}

	t.Logf("Large article: %d bytes output in %v", len(html), elapsed)

	// Should complete in reasonable time
	if elapsed > 30*time.Second {
		t.Errorf("large article processing took too long: %v", elapsed)
	}
}

// TestIntegration_EpubWithExternalImages exercises the full epub pipeline
// with articles containing external images served locally.
func TestIntegration_EpubWithExternalImages(t *testing.T) {
	imgData := map[string][]byte{
		"hero.png": makePNG(1200, 800, color.NRGBA{200, 50, 50, 255}),
	}

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/img/") {
			key := strings.TrimPrefix(r.URL.Path, "/img/")
			if data, ok := imgData[key]; ok {
				w.Header().Set("Content-Type", "image/png")
				w.Write(data)
				return
			}
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(fmt.Sprintf(`<!DOCTYPE html>
<html><head><title>External Epub Test</title></head><body>
<article>
<h1>External Epub Test</h1>
<p>Article with an external image that needs to be fetched, embedded,
and included in the epub. The pipeline should handle external URLs
when building epub files. More text for readability threshold.</p>
<img src="%s/img/hero.png" alt="hero image">
<p>Additional content after the image. This paragraph adds more text
density for the readability algorithm to work properly.</p>
</article>
</body></html>`, srv.URL)))
	}))
	defer srv.Close()

	saved := fetchImageClient
	fetchImageClient = srv.Client()
	defer func() { fetchImageClient = saved }()

	outFile := filepath.Join(t.TempDir(), "external.epub")
	cfg := cliConfig{
		opts:      optimizeOpts{maxWidth: 800, quality: 60},
		output:    outFile,
		timeout:   10 * time.Second,
		userAgent: "test-agent",
		epubMode:  true,
		args:      []string{srv.URL},
	}

	err := run(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Verify epub has the image
	zr, err := zip.OpenReader(outFile)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()

	hasImage := false
	for _, f := range zr.File {
		if strings.Contains(f.Name, "ch001_img") {
			hasImage = true
			break
		}
	}
	if !hasImage {
		t.Error("epub should contain the fetched external image")
		for _, f := range zr.File {
			t.Logf("  %s", f.Name)
		}
	}
}

// ---------- benchmarks ----------

// BenchmarkProcessURL measures the full single-article pipeline.
func BenchmarkProcessURL(b *testing.B) {
	imgData := makePNG(1200, 900, color.NRGBA{200, 100, 50, 255})
	imgURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString(imgData)

	pageHTML := fmt.Sprintf(`<!DOCTYPE html>
<html><head><title>Bench Article - Site</title></head><body>
<article>
<h1>Bench Article</h1>
<p>This is a benchmark article with enough content for readability.
The paragraph has substantial text to pass content density checks.
More filler to ensure the readability algorithm works correctly.</p>
<img src="%s" alt="test">
<h2>Section</h2>
<p>Second section with more text for content density. Additional words
to satisfy readability requirements and heuristics for extraction.</p>
</article>
</body></html>`, imgURI)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(pageHTML))
	}))
	defer srv.Close()

	opts := optimizeOpts{maxWidth: 800, quality: 60, grayscale: true}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _, err := processURL(srv.URL, opts, 5*time.Second, "bench-agent", "")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkImageOptimize measures image optimization throughput.
func BenchmarkImageOptimize(b *testing.B) {
	data := makePNG(1200, 900, color.NRGBA{200, 100, 50, 255})
	opts := optimizeOpts{maxWidth: 800, quality: 60, grayscale: true}

	b.ResetTimer()
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		uri, _ := optimizeImage(data, "image/png", opts)
		if uri == "" {
			b.Fatal("expected optimized image")
		}
	}
}

// BenchmarkBuildEpub measures epub generation for a 5-article book.
func BenchmarkBuildEpub(b *testing.B) {
	imgData := makePNG(100, 100, color.NRGBA{255, 0, 0, 255})
	imgURI := dataURI("image/png", imgData)

	articles := make([]epubArticle, 5)
	for i := range articles {
		articles[i] = epubArticle{
			HTML:     fmt.Sprintf(`<html><body><h1>Article %d</h1><p>Content for article %d with enough text.</p><img src="%s" alt="img"></body></html>`, i+1, i+1, imgURI),
			Title:    fmt.Sprintf("Article %d", i+1),
			URL:      fmt.Sprintf("https://example.com/article-%d", i+1),
			Byline:   "Author",
			SiteName: "Site",
		}
	}

	dir := b.TempDir()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		outPath := filepath.Join(dir, fmt.Sprintf("bench_%d.epub", i))
		if err := buildEpub(articles, "Bench Book", outPath); err != nil {
			b.Fatal(err)
		}
	}
}
