package main

import (
	"archive/zip"
	"encoding/base64"
	"image/color"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	epub "github.com/go-shiori/go-epub"
	"golang.org/x/net/html"
)

// htmlAttr creates an html.Attribute for testing.
func htmlAttr(key, val string) html.Attribute {
	return html.Attribute{Key: key, Val: val}
}

func runCommand(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestExtractBodyContent(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"with body tags", `<html><body><p>hello</p></body></html>`, `<p>hello</p>`},
		{"no body tags", `<p>hello</p>`, `<p>hello</p>`},
		{"body with attrs", `<html><body class="x"><p>hi</p></body></html>`, `<p>hi</p>`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractBodyContent(tt.input)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractH1Title(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"simple h1", `<h1>My Title</h1><p>text</p>`, "My Title"},
		{"h1 with tags", `<h1><em>Bold</em> Title</h1>`, "Bold Title"},
		{"no h1", `<h2>Sub</h2><p>text</p>`, ""},
		{"h1 with attrs", `<h1 id="top">Title</h1>`, "Title"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractH1Title(tt.input)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildEpub_Basic(t *testing.T) {
	// Create test HTML with an embedded image
	imgData := makePNG(100, 100, color.NRGBA{255, 0, 0, 255})
	imgURI := dataURI("image/png", imgData)

	articles := []epubArticle{
		{
			HTML:  `<html><body><h1>First Article</h1><p>Some content here.</p></body></html>`,
			Title: "First Article",
			URL:   "https://example.com/first",
		},
		{
			HTML:     `<html><body><h1>Second Article</h1><p>More content.</p><img src="` + imgURI + `" alt="test"></body></html>`,
			Title:    "Second Article",
			URL:      "https://example.com/second",
			Byline:   "Jane Doe",
			SiteName: "Example",
		},
	}

	outPath := filepath.Join(t.TempDir(), "test.epub")
	err := buildEpub(articles, "Test Book", outPath, "collage")
	if err != nil {
		t.Fatal(err)
	}

	// Verify epub was created
	info, err := os.Stat(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() < 100 {
		t.Errorf("epub too small: %d bytes", info.Size())
	}

	// Verify it's a valid zip (epub is a zip file)
	zr, err := zip.OpenReader(outPath)
	if err != nil {
		t.Fatalf("not a valid zip: %v", err)
	}
	defer zr.Close()

	// Check expected files exist
	fileNames := map[string]bool{}
	for _, f := range zr.File {
		fileNames[f.Name] = true
	}

	// Must have table of contents
	if !fileNames["EPUB/xhtml/contents.xhtml"] {
		t.Error("missing contents.xhtml (front matter TOC)")
	}

	// Must have article files
	if !fileNames["EPUB/xhtml/article001.xhtml"] {
		t.Error("missing article001.xhtml")
	}
	if !fileNames["EPUB/xhtml/article002.xhtml"] {
		t.Error("missing article002.xhtml")
	}

	// Must have the embedded image
	hasImage := false
	for name := range fileNames {
		if strings.Contains(name, "ch002_img000") {
			hasImage = true
			break
		}
	}
	if !hasImage {
		t.Error("missing embedded image file")
		for name := range fileNames {
			t.Logf("  %s", name)
		}
	}

	// Must have navigation (TOC)
	hasNav := false
	for name := range fileNames {
		if strings.Contains(name, "nav") {
			hasNav = true
			break
		}
	}
	if !hasNav {
		t.Error("missing nav file (TOC)")
		for name := range fileNames {
			t.Logf("  %s", name)
		}
	}

	// Verify TOC content has article links and metadata
	tocFile := findZipFile(zr, "EPUB/xhtml/contents.xhtml")
	if tocFile != "" {
		if !strings.Contains(tocFile, "First Article") {
			t.Error("TOC should contain 'First Article'")
		}
		if !strings.Contains(tocFile, "Second Article") {
			t.Error("TOC should contain 'Second Article'")
		}
		if !strings.Contains(tocFile, "Jane Doe") {
			t.Error("TOC should contain author 'Jane Doe'")
		}
		if !strings.Contains(tocFile, "example.com/second") {
			t.Error("TOC should contain source URL")
		}
		if !strings.Contains(tocFile, "article001.xhtml") {
			t.Error("TOC should link to article001.xhtml")
		}
	}
}

// findZipFile reads the contents of a file from a zip reader by name.
func findZipFile(zr *zip.ReadCloser, name string) string {
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				return ""
			}
			defer rc.Close()
			data, err := io.ReadAll(rc)
			if err != nil {
				return ""
			}
			return string(data)
		}
	}
	return ""
}

func TestIsAllowedAttr(t *testing.T) {
	tests := []struct {
		name string
		key  string
		val  string
		want bool
	}{
		{"id", "id", "main", true},
		{"class", "class", "container", true},
		{"href", "href", "https://example.com", true},
		{"src", "src", "image.jpg", true},
		{"alt", "alt", "description", true},
		{"style", "style", "color: red", true},
		{"colspan", "colspan", "2", true},
		{"rel", "rel", "noopener", true},
		{"aria-label", "aria-label", "Close", false},
		{"aria-hidden", "aria-hidden", "true", false},
		{"epub:type", "epub:type", "chapter", true},
		{"data-custom", "data-custom", "value", false},
		{"onclick", "onclick", "alert(1)", false},
		{"tabindex", "tabindex", "0", false},
		{"role", "role", "button", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attr := htmlAttr(tt.key, tt.val)
			got := isAllowedAttr(attr)
			if got != tt.want {
				t.Errorf("isAllowedAttr(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

func TestSanitizeForXHTML_FiltersAttrs(t *testing.T) {
	input := `<p id="intro" onclick="alert(1)" data-track="click">Hello</p>`
	result := sanitizeForXHTML(input)
	if strings.Contains(result, "onclick") {
		t.Error("onclick should be stripped")
	}
	if strings.Contains(result, "data-track") {
		t.Error("data-track should be stripped")
	}
	if !strings.Contains(result, `id="intro"`) {
		t.Error("id should be kept")
	}
	if !strings.Contains(result, "Hello") {
		t.Error("text content should be preserved")
	}
}

func TestSanitizeForXHTML_FixesBrokenFragmentLinks(t *testing.T) {
	input := `<a href="#exists">ok</a><a href="#missing">broken</a><div id="exists">target</div>`
	result := sanitizeForXHTML(input)
	if !strings.Contains(result, `href="#exists"`) {
		t.Error("link to existing ID should be kept")
	}
	// The broken link should have href removed
	if strings.Contains(result, `href="#missing"`) {
		t.Error("link to non-existent ID should be dropped")
	}
}

func TestSanitizeForXHTML_VoidElements(t *testing.T) {
	input := `<p>text<br>more<hr><img src="x.jpg" alt="test"></p>`
	result := sanitizeForXHTML(input)
	if !strings.Contains(result, "<br/>") {
		t.Error("br should be self-closing in XHTML")
	}
	if !strings.Contains(result, "<hr/>") {
		t.Error("hr should be self-closing in XHTML")
	}
	if !strings.Contains(result, "/>") {
		t.Error("img should be self-closing in XHTML")
	}
}

func TestSanitizeForXHTML_AriaAndEpubAttrs(t *testing.T) {
	input := `<section aria-label="chapter" class="main" epub:type="chapter">content</section>`
	result := sanitizeForXHTML(input)
	if strings.Contains(result, `aria-label="chapter"`) {
		t.Error("aria-label should be stripped")
	}
	if !strings.Contains(result, `class="main"`) {
		t.Error("class should be preserved")
	}
	if !strings.Contains(result, `epub:type="chapter"`) {
		t.Error("epub:type should be preserved")
	}
}

func TestSanitizeForXHTML_StrictWhitelist(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string // partial match check
		not   string // should not contain
	}{
		{
			name:  "removes script",
			input: `<div><script>alert(1)</script><p>text</p></div>`,
			want:  `<p>text</p>`,
			not:   "script",
		},
		{
			name:  "removes object",
			input: `<div><object data="foo"></object><p>text</p></div>`,
			want:  `<p>text</p>`,
			not:   "object",
		},
		{
			name:  "converts video to link",
			input: `<div><video src="movie.mp4"></video></div>`,
			want:  `<a href="movie.mp4">[Media: movie.mp4]</a>`,
			not:   "video",
		},
		{
			name:  "unwraps nested p in h1",
			input: `<h1><p>Title</p></h1>`,
			want:  `<h1>Title</h1>`,
			not:   "<p>",
		},
		{
			name:  "unwraps div in span",
			input: `<span>start <div>middle</div> end</span>`,
			want:  `<span>start middle end</span>`, // div stripped, content merged
			not:   "<div>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeForXHTML(tt.input)
			if tt.want != "" && !strings.Contains(got, tt.want) {
				t.Errorf("got %q, want substring %q", got, tt.want)
			}
			if tt.not != "" && strings.Contains(got, tt.not) {
				t.Errorf("got %q, should not contain %q", got, tt.not)
			}
		})
	}
}

func TestExtractImages_MultipleMIMETypes(t *testing.T) {
	pngData := makePNG(10, 10, color.NRGBA{255, 0, 0, 255})

	body := `<p>Text</p>` +
		`<img src="data:image/png;base64,` + base64.StdEncoding.EncodeToString(pngData) + `" alt="png">` +
		`<img src="data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7" alt="gif">`

	e, _ := epub.NewEpub("test")
	result, err := extractImages(e, body, 1)
	if err != nil {
		t.Logf("extractImages returned error (may be expected): %v", err)
	}
	// Images should be replaced with internal paths
	if strings.Contains(result, "data:image/png;base64,") {
		t.Error("PNG data URI should be replaced with internal path")
	}
}

func TestExtractImages_InvalidBase64(t *testing.T) {
	body := `<img src="data:image/jpeg;base64,!!!invalid!!!" alt="broken">`

	e, _ := epub.NewEpub("test")
	result, _ := extractImages(e, body, 1)
	// Invalid base64 should keep the original
	if !strings.Contains(result, "!!!invalid!!!") {
		t.Error("invalid base64 image should be kept as-is")
	}
}

func TestBuildTOCBody_EmptyTitle(t *testing.T) {
	articles := []epubArticle{
		{HTML: "<body><p>content</p></body>", Title: "", URL: "https://example.com"},
	}
	result := buildTOCBody(articles)
	if !strings.Contains(result, "Article 1") {
		t.Error("empty title should fall back to 'Article N'")
	}
}

func TestBuildTOCBody_FullMetadata(t *testing.T) {
	pubDate := time.Date(2024, time.March, 15, 0, 0, 0, 0, time.UTC)
	articles := []epubArticle{
		{
			HTML:          "<body><p>content</p></body>",
			Title:         "My Article",
			URL:           "https://example.com/post",
			Byline:        "Jane Doe",
			SiteName:      "Example Blog",
			PublishedTime: &pubDate,
		},
	}
	result := buildTOCBody(articles)
	if !strings.Contains(result, "My Article") {
		t.Error("expected article title in TOC")
	}
	if !strings.Contains(result, "March 15, 2024") {
		t.Error("expected published date in TOC")
	}
	if !strings.Contains(result, "Jane Doe") {
		t.Error("expected author in TOC")
	}
	if !strings.Contains(result, "Example Blog") {
		t.Error("expected site name in TOC")
	}
	if !strings.Contains(result, "example.com/post") {
		t.Error("expected URL in TOC")
	}
}

func TestBuildTOCBody_DateOnly(t *testing.T) {
	pubDate := time.Date(2023, time.December, 1, 0, 0, 0, 0, time.UTC)
	articles := []epubArticle{
		{
			HTML:          "<body><p>content</p></body>",
			Title:         "Dated Article",
			PublishedTime: &pubDate,
		},
	}
	result := buildTOCBody(articles)
	if !strings.Contains(result, "December 1, 2023") {
		t.Error("expected published date in TOC")
	}
	if !strings.Contains(result, "toc-meta") {
		t.Error("expected metadata paragraph when date is present")
	}
}

func TestBuildTOCBody_URLOnly(t *testing.T) {
	articles := []epubArticle{
		{HTML: "<body><p>c</p></body>", Title: "T", URL: "https://example.com/"},
	}
	result := buildTOCBody(articles)
	// URL should have scheme and trailing slash stripped
	if !strings.Contains(result, "example.com") {
		t.Error("expected clean URL in TOC")
	}
}

func TestExtractBodyContent_NoEndBody(t *testing.T) {
	input := `<html><body><p>hello</p>`
	got := extractBodyContent(input)
	if got != "<p>hello</p>" {
		t.Errorf("got %q, want %q", got, "<p>hello</p>")
	}
}

func TestBuildEpub_NoTitleFallback(t *testing.T) {
	articles := []epubArticle{
		{HTML: `<html><body><p>No heading here.</p></body></html>`, Title: ""},
	}
	outPath := filepath.Join(t.TempDir(), "notitle.epub")
	err := buildEpub(articles, "Fallback Title", outPath, "collage")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() < 100 {
		t.Error("epub should have been created")
	}
}

// --- Regression tests for EPUB validation issues found during stress testing ---
// These tests cover issues discovered by generating EPUBs from the top 100
// Hacker News stories and validating with epubcheck.

func TestSanitizeForXHTML_StripInvalidXMLChars(t *testing.T) {
	// U+0012 (Device Control 2) is not valid in XML 1.0 and causes FATAL errors.
	input := "<p>Hello\x12World</p>"
	result := sanitizeForXHTML(input)
	if strings.Contains(result, "\x12") {
		t.Error("U+0012 control character should be stripped")
	}
	if !strings.Contains(result, "HelloWorld") {
		t.Errorf("text content should be preserved (got %q)", result)
	}
}

func TestSanitizeForXHTML_StripMultipleInvalidChars(t *testing.T) {
	// Test various invalid XML control characters
	input := "<p>\x00\x01\x08\x0B\x0C\x0E\x1F text</p>"
	result := sanitizeForXHTML(input)
	if !strings.Contains(result, "text") {
		t.Error("valid text should be preserved")
	}
	for _, c := range []byte{0x00, 0x01, 0x08, 0x0B, 0x0C, 0x0E, 0x1F} {
		if strings.ContainsRune(result, rune(c)) {
			t.Errorf("invalid XML char U+%04X should be stripped", c)
		}
	}
}

func TestSanitizeForXHTML_PreservesValidXMLChars(t *testing.T) {
	// Tab, newline, carriage return are valid XML characters
	input := "<p>line1\nline2\ttabbed\rreturned</p>"
	result := sanitizeForXHTML(input)
	if !strings.Contains(result, "\n") {
		t.Error("newline should be preserved")
	}
	if !strings.Contains(result, "\t") {
		t.Error("tab should be preserved")
	}
}

func TestSanitizeForXHTML_RemoveSourceElements(t *testing.T) {
	// <source> elements without srcset cause RSC-005 validation errors.
	input := `<div><source media="(max-width: 480px)"/><img src="img.jpg" alt="test"/></div>`
	result := sanitizeForXHTML(input)
	if strings.Contains(result, "<source") {
		t.Error("<source> elements should be removed")
	}
	if strings.Contains(result, "max-width") {
		t.Error("source media attributes should not remain")
	}
}

func TestSanitizeForXHTML_CollapsePictureToImg(t *testing.T) {
	// <picture> elements must be collapsed to their first <img> child.
	input := `<div><picture><source media="(max-width: 480px)"/><source media="(max-width: 767px)"/><img src="data:image/png;base64,abc" alt="photo"/></picture></div>`
	result := sanitizeForXHTML(input)
	if strings.Contains(result, "<picture") {
		t.Error("<picture> should be collapsed")
	}
	if strings.Contains(result, "<source") {
		t.Error("<source> should be removed")
	}
	if !strings.Contains(result, `alt="photo"`) {
		t.Errorf("img from picture should be preserved (got %q)", result)
	}
}

func TestSanitizeForXHTML_PictureImgCleaned(t *testing.T) {
	// When <picture> is collapsed to <img>, the img must also be cleaned
	// (e.g., external src removed, attrs filtered). This was a subtle bug
	// where the extracted img bypassed the clean() function.
	input := `<picture><img src="https://external.com/photo.jpg" loading="lazy" alt="ext"/></picture>`
	result := sanitizeForXHTML(input)
	if strings.Contains(result, "external.com") {
		t.Error("external img extracted from <picture> should be stripped")
	}
	if strings.Contains(result, "loading") {
		t.Error("loading attribute should be stripped from picture's img")
	}
}

func TestSanitizeForXHTML_StripExternalImages(t *testing.T) {
	// Images with http:// or https:// src cause RSC-006 (remote resource reference).
	tests := []struct {
		name  string
		input string
	}{
		{"https", `<p>Before</p><img src="https://cdn.example.com/img.jpg" alt="test"/><p>After</p>`},
		{"http", `<p>Before</p><img src="http://cdn.example.com/img.jpg" alt="test"/><p>After</p>`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeForXHTML(tt.input)
			if strings.Contains(result, "cdn.example.com") {
				t.Error("external image should be removed")
			}
			if !strings.Contains(result, "Before") || !strings.Contains(result, "After") {
				t.Error("surrounding content should be preserved")
			}
		})
	}
}

func TestSanitizeForXHTML_KeepInternalImages(t *testing.T) {
	// Relative and data URI images should be preserved.
	input := `<img src="../images/photo.jpg" alt="local"/>`
	result := sanitizeForXHTML(input)
	if !strings.Contains(result, "photo.jpg") {
		t.Error("relative image src should be preserved")
	}
}

func TestSanitizeForXHTML_DeduplicateIDs(t *testing.T) {
	// Duplicate IDs cause validation errors in EPUB XHTML.
	input := `<div id="intro">First</div><div id="intro">Second</div><div id="intro">Third</div>`
	result := sanitizeForXHTML(input)
	if !strings.Contains(result, `id="intro"`) {
		t.Error("first occurrence of ID should be kept as-is")
	}
	if !strings.Contains(result, `id="intro-2"`) {
		t.Errorf("second occurrence should be deduplicated (got %q)", result)
	}
	if !strings.Contains(result, `id="intro-3"`) {
		t.Errorf("third occurrence should be deduplicated (got %q)", result)
	}
}

func TestSanitizeForXHTML_SanitizeIDWhitespace(t *testing.T) {
	// IDs with whitespace are invalid in XHTML.
	input := `<h3 id="galaxy upcycle initial pitch">Title</h3>`
	result := sanitizeForXHTML(input)
	if strings.Contains(result, `id="galaxy upcycle`) {
		t.Error("ID with whitespace should be sanitized")
	}
	if !strings.Contains(result, `id="galaxy-upcycle-initial-pitch"`) {
		t.Errorf("whitespace should be replaced with hyphens (got %q)", result)
	}
}

func TestSanitizeDimensionAttr(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"100", "100"},
		{"1650", "1650"},
		{"1.5", "2"},         // decimal → rounded integer
		{"99.4", "99"},       // rounds down
		{"100px", "100"},     // strips px
		{"16em", "16"},       // strips em
		{"50%", "50"},        // strips %
		{"-5", ""},           // negative
		{"abc", ""},          // non-numeric
		{"", ""},             // empty
		{"  200  ", "200"},   // whitespace trimmed
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeDimensionAttr(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeDimensionAttr(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSanitizeForXHTML_DimensionsOnlyOnAllowedElements(t *testing.T) {
	// Width/height on elements like <source> or <div> should be stripped.
	input := `<div width="100" height="200"><img src="x.jpg" alt="t" width="100" height="200"/></div>`
	result := sanitizeForXHTML(input)
	// img should keep dimensions
	if !strings.Contains(result, `width="100"`) {
		t.Error("img should keep width")
	}
	// Check that div doesn't have width/height (div is not in elemAllowsDimensions)
	divIdx := strings.Index(result, "<div")
	imgIdx := strings.Index(result, "<img")
	if divIdx >= 0 && imgIdx > divIdx {
		divTag := result[divIdx:imgIdx]
		if strings.Contains(divTag, "width") || strings.Contains(divTag, "height") {
			t.Error("div should not have width/height attributes")
		}
	}
}

func TestSanitizeForXHTML_DecimalDimensions(t *testing.T) {
	// Width "1.5" is invalid — must be rounded to integer.
	input := `<img src="x.jpg" alt="t" width="1.5" height="916.7"/>`
	result := sanitizeForXHTML(input)
	if strings.Contains(result, `"1.5"`) {
		t.Error("decimal width should be rounded to integer")
	}
	if strings.Contains(result, `"916.7"`) {
		t.Error("decimal height should be rounded to integer")
	}
	if !strings.Contains(result, `width="2"`) {
		t.Errorf("1.5 should round to 2 (got %q)", result)
	}
	if !strings.Contains(result, `height="917"`) {
		t.Errorf("916.7 should round to 917 (got %q)", result)
	}
}

func TestSanitizeForXHTML_TableInP(t *testing.T) {
	// <table> inside <p> is invalid — table must be moved out.
	input := `<p>Before<table><tr><td>cell</td></tr></table>After</p>`
	result := sanitizeForXHTML(input)
	if strings.Contains(result, "<p") && strings.Contains(result, "<table") {
		// Make sure table is not nested inside p
		pIdx := strings.Index(result, "<p")
		pEnd := strings.Index(result[pIdx:], "</p>")
		if pEnd >= 0 {
			pContent := result[pIdx : pIdx+pEnd+4]
			if strings.Contains(pContent, "<table") {
				t.Errorf("table should not be inside p (got %q)", result)
			}
		}
	}
	// Table content should still exist
	if !strings.Contains(result, "cell") {
		t.Errorf("table content should be preserved (got %q)", result)
	}
}

func TestSanitizeForXHTML_TableInCodeInP(t *testing.T) {
	// <table> nested inside <code> inside <p> must traverse up through
	// all phrasing ancestors before being inserted.
	input := `<p><code>text<table><tr><td>data</td></tr></table></code></p>`
	result := sanitizeForXHTML(input)
	// Table should not be inside any phrasing element
	tableIdx := strings.Index(result, "<table")
	if tableIdx >= 0 {
		before := result[:tableIdx]
		// Count open/close p tags to see if table is inside a p
		openP := strings.Count(before, "<p")
		closeP := strings.Count(before, "</p>")
		if openP > closeP {
			t.Errorf("table should not be nested inside <p> (got %q)", result)
		}
	}
	if !strings.Contains(result, "data") {
		t.Error("table content should be preserved")
	}
}

func TestSanitizeForXHTML_DLContentModel(t *testing.T) {
	// <dl> must contain dt/dd pairs. Bare text is invalid.
	input := `<dl>bare text<dt>term</dt><dd>definition</dd></dl>`
	result := sanitizeForXHTML(input)
	// The bare text should be wrapped in dt
	if !strings.Contains(result, "<dt") {
		t.Errorf("dl should contain dt elements (got %q)", result)
	}
	if !strings.Contains(result, "term") {
		t.Error("dt content should be preserved")
	}
	if !strings.Contains(result, "definition") {
		t.Error("dd content should be preserved")
	}
}

func TestSanitizeForXHTML_DLDtWithoutDd(t *testing.T) {
	// A <dt> at the end of a <dl> without a following <dd> is invalid.
	input := `<dl><dt>orphan term</dt></dl>`
	result := sanitizeForXHTML(input)
	if !strings.Contains(result, "<dd") {
		t.Errorf("dt without dd should get an empty dd added (got %q)", result)
	}
}

func TestSanitizeForXHTML_DLDdBeforeDt(t *testing.T) {
	// A <dd> before any <dt> needs a <dt> inserted before it.
	input := `<dl><dd>orphan def</dd></dl>`
	result := sanitizeForXHTML(input)
	dtIdx := strings.Index(result, "<dt")
	ddIdx := strings.Index(result, "<dd")
	if dtIdx < 0 || ddIdx < 0 {
		t.Fatalf("dl should have both dt and dd (got %q)", result)
	}
	if dtIdx > ddIdx {
		t.Errorf("dt should come before dd (got %q)", result)
	}
}

func TestSanitizeForXHTML_FigcaptionOutsideFigure(t *testing.T) {
	// <figcaption> outside <figure> is invalid — convert to <p>.
	input := `<div><figcaption>Caption text</figcaption></div>`
	result := sanitizeForXHTML(input)
	if strings.Contains(result, "<figcaption") {
		t.Error("figcaption outside figure should be converted to p")
	}
	if !strings.Contains(result, "<p") {
		t.Errorf("figcaption should become p (got %q)", result)
	}
	if !strings.Contains(result, "Caption text") {
		t.Error("caption text should be preserved")
	}
}

func TestSanitizeForXHTML_FigcaptionInsideFigure(t *testing.T) {
	// <figcaption> inside <figure> is valid and should be kept.
	input := `<figure><img src="x.jpg" alt="t"/><figcaption>Valid caption</figcaption></figure>`
	result := sanitizeForXHTML(input)
	if !strings.Contains(result, "<figcaption") {
		t.Error("figcaption inside figure should be preserved")
	}
}

func TestSanitizeID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"simple", "simple"},
		{"with spaces", "with-spaces"},
		{"  leading-trailing  ", "leading-trailing"},
		{"tab\there", "tab-here"},
		{"", ""},
		{"multi  spaces", "multi--spaces"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeID(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeID(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSanitizeForXHTML_PictureWithoutImg(t *testing.T) {
	// <picture> without any <img> child should be removed entirely.
	input := `<div><picture><source media="(max-width: 480px)"/></picture><p>text</p></div>`
	result := sanitizeForXHTML(input)
	if strings.Contains(result, "<picture") {
		t.Error("picture without img should be removed")
	}
	if !strings.Contains(result, "text") {
		t.Error("surrounding content should be preserved")
	}
}

func TestSanitizeForXHTML_PreInP(t *testing.T) {
	// <pre> inside <p> should be moved out (structural block).
	input := `<p>intro<pre>code block</pre>outro</p>`
	result := sanitizeForXHTML(input)
	if strings.Contains(result, "<p") {
		pIdx := strings.Index(result, "<p")
		pEnd := strings.Index(result[pIdx:], "</p>")
		if pEnd >= 0 {
			pContent := result[pIdx : pIdx+pEnd+4]
			if strings.Contains(pContent, "<pre") {
				t.Errorf("pre should not be inside p (got %q)", result)
			}
		}
	}
	if !strings.Contains(result, "code block") {
		t.Error("pre content should be preserved")
	}
}

func TestBuildEpub_EpubCheck(t *testing.T) {
	// Only run if epubcheck is available
	if _, err := os.Stat("/usr/bin/epubcheck"); err != nil {
		t.Skip("epubcheck not installed")
	}

	imgData := makeJPEG(200, 150, color.NRGBA{0, 100, 200, 255})
	imgURI := dataURI("image/jpeg", imgData)

	articles := []epubArticle{
		{
			HTML:     `<html><body><h1>Chapter One</h1><p>This is a test chapter with some content for validation.</p><img src="` + imgURI + `" alt="test image"/><p>Another paragraph after the image.</p></body></html>`,
			Title:    "Chapter One",
			URL:      "https://example.com/chapter-one",
			Byline:   "Test Author",
			SiteName: "Example Blog",
		},
		{
			HTML:  `<html><body><h1>Chapter Two</h1><p>Second chapter with more content to test epub generation.</p></body></html>`,
			Title: "Chapter Two",
			URL:   "https://example.com/chapter-two",
		},
	}

	outPath := filepath.Join(t.TempDir(), "check.epub")
	err := buildEpub(articles, "EpubCheck Test", outPath, "collage")
	if err != nil {
		t.Fatal(err)
	}

	// Run epubcheck
	// This is tested separately since it requires the external tool
	out, err := runCommand("epubcheck", outPath)
	if err != nil {
		t.Errorf("epubcheck failed:\n%s", out)
	}
}
