package main

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// htmlAttr creates an html.Attribute for testing.
func htmlAttr(key, val string) html.Attribute {
	return html.Attribute{Key: key, Val: val}
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

// --- Regression tests for EPUB validation issues found during stress testing ---

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
		{"1.5", "2"},       // decimal → rounded integer
		{"99.4", "99"},     // rounds down
		{"100px", "100"},   // strips px
		{"16em", "16"},     // strips em
		{"50%", "50"},      // strips %
		{"-5", ""},         // negative
		{"abc", ""},        // non-numeric
		{"", ""},           // empty
		{"  200  ", "200"}, // whitespace trimmed
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
