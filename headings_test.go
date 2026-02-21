package main

import (
	"strings"
	"testing"
	"time"
)

func TestExtractTitle_FromTitleTag(t *testing.T) {
	html := `<html><head><title>My Great Article</title></head><body></body></html>`
	got := extractTitle(html)
	if got != "My Great Article" {
		t.Errorf("got %q, want %q", got, "My Great Article")
	}
}

func TestExtractTitle_FromTitleTagWithSuffix(t *testing.T) {
	html := `<html><head><title>My Article - Site Name</title></head><body></body></html>`
	got := extractTitle(html)
	if got != "My Article" {
		t.Errorf("got %q, want %q", got, "My Article")
	}
}

func TestExtractTitle_FromH1(t *testing.T) {
	html := `<html><body><h1>Heading Title</h1></body></html>`
	got := extractTitle(html)
	if got != "Heading Title" {
		t.Errorf("got %q, want %q", got, "Heading Title")
	}
}

func TestExtractTitle_FromH1WithTags(t *testing.T) {
	html := `<html><body><h1><a href="#">Link <em>Title</em></a></h1></body></html>`
	got := extractTitle(html)
	if got != "Link Title" {
		t.Errorf("got %q, want %q", got, "Link Title")
	}
}

func TestExtractTitle_Fallback(t *testing.T) {
	html := `<html><body><p>No headings here</p></body></html>`
	got := extractTitle(html)
	if got != "Untitled" {
		t.Errorf("got %q, want %q", got, "Untitled")
	}
}

func TestCleanTitle_Dash(t *testing.T) {
	got := cleanTitle("Article Title - Site Name")
	if got != "Article Title" {
		t.Errorf("got %q, want %q", got, "Article Title")
	}
}

func TestCleanTitle_Pipe(t *testing.T) {
	got := cleanTitle("Article | Site")
	if got != "Article" {
		t.Errorf("got %q, want %q", got, "Article")
	}
}

func TestCleanTitle_EmDash(t *testing.T) {
	got := cleanTitle("Article \u2014 Site")
	if got != "Article" {
		t.Errorf("got %q, want %q", got, "Article")
	}
}

func TestCleanTitle_EnDash(t *testing.T) {
	got := cleanTitle("Article \u2013 Site")
	if got != "Article" {
		t.Errorf("got %q, want %q", got, "Article")
	}
}

func TestCleanTitle_NoSuffix(t *testing.T) {
	got := cleanTitle("Simple Title")
	if got != "Simple Title" {
		t.Errorf("got %q, want %q", got, "Simple Title")
	}
}

func TestCleanTitle_Empty(t *testing.T) {
	got := cleanTitle("")
	if got != "Untitled" {
		t.Errorf("got %q, want %q", got, "Untitled")
	}
}

func TestShiftHeadings_AllLevels(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"<h1>one</h1>", "<h2>one</h2>"},
		{"<h2>two</h2>", "<h3>two</h3>"},
		{"<h3>three</h3>", "<h4>three</h4>"},
		{"<h4>four</h4>", "<h5>four</h5>"},
		{"<h5>five</h5>", "<h6>five</h6>"},
		{"<h6>six</h6>", "<h6>six</h6>"}, // clamped
	}
	for _, tt := range tests {
		got := shiftHeadings(tt.input)
		if got != tt.want {
			t.Errorf("shiftHeadings(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestShiftHeadings_WithAttributes(t *testing.T) {
	got := shiftHeadings(`<h2 class="foo" id="bar">text</h2>`)
	want := `<h3 class="foo" id="bar">text</h3>`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestShiftHeadings_CaseInsensitive(t *testing.T) {
	got := shiftHeadings(`<H1>Title</H1>`)
	want := `<h2>Title</h2>`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNormalizeHeadings_InsertsH1(t *testing.T) {
	html := `<html><head><title>My Article</title></head><body><h1>Old H1</h1><p>text</p></body></html>`
	result := normalizeHeadings(html, "", sourceInfo{})

	// Should have H1 with title after <body>
	if !strings.Contains(result, "<h1>My Article</h1>") {
		t.Error("expected H1 with title")
	}
	// Old H1 should be shifted to H2
	if !strings.Contains(result, "<h2>Old H1</h2>") {
		t.Error("expected old H1 shifted to H2")
	}
	// H1 should come before H2 in the output
	h1Pos := strings.Index(result, "<h1>My Article</h1>")
	h2Pos := strings.Index(result, "<h2>Old H1</h2>")
	if h1Pos >= h2Pos {
		t.Error("H1 should appear before the shifted H2")
	}
}

func TestNormalizeHeadings_TitleOverride(t *testing.T) {
	html := `<html><head><title>Original Title</title></head><body><p>text</p></body></html>`
	result := normalizeHeadings(html, "Custom Title", sourceInfo{})

	if !strings.Contains(result, "<h1>Custom Title</h1>") {
		t.Error("expected H1 with override title")
	}
}

func TestNormalizeHeadings_TitleOverrideCleansSuffix(t *testing.T) {
	html := `<html><body><p>text</p></body></html>`
	result := normalizeHeadings(html, "My Article - Site Name", sourceInfo{})

	if !strings.Contains(result, "<h1>My Article</h1>") {
		t.Errorf("expected cleaned title, got: %s", result)
	}
}

func TestNormalizeHeadings_NoBody(t *testing.T) {
	html := `<h2>Sub</h2><p>text</p>`
	result := normalizeHeadings(html, "Title", sourceInfo{})

	if !strings.HasPrefix(result, "<h1>Title</h1>") {
		t.Errorf("expected H1 at start when no body tag, got: %q", result[:50])
	}
}

func TestNormalizeHeadings_EscapesTitle(t *testing.T) {
	html := `<html><body><p>text</p></body></html>`
	result := normalizeHeadings(html, `Title with <script> & "quotes"`, sourceInfo{})

	if !strings.Contains(result, "Title with &lt;script&gt; &amp; &#34;quotes&#34;") {
		t.Errorf("expected HTML-escaped title, got: %s", result)
	}
}

func TestNormalizeHeadings_Byline(t *testing.T) {
	html := `<html><body><p>text</p></body></html>`
	src := sourceInfo{
		URL:      "https://example.com/article",
		Byline:   "Jane Doe",
		SiteName: "Example Blog",
	}
	result := normalizeHeadings(html, "Test", src)

	if !strings.Contains(result, `class="byline"`) {
		t.Error("expected byline paragraph")
	}
	if !strings.Contains(result, "Jane Doe") {
		t.Error("expected author name in byline")
	}
	if !strings.Contains(result, "Example Blog") {
		t.Error("expected site name in byline")
	}
	if !strings.Contains(result, "example.com/article") {
		t.Error("expected URL in byline")
	}
	// Byline should appear after H1
	h1Pos := strings.Index(result, "<h1>")
	bylinePos := strings.Index(result, `class="byline"`)
	if bylinePos < h1Pos {
		t.Error("byline should appear after H1")
	}
}

func TestNormalizeHeadings_BylineURLOnly(t *testing.T) {
	html := `<html><body><p>text</p></body></html>`
	src := sourceInfo{URL: "https://medium.com/@someone/my-post-abc123"}
	result := normalizeHeadings(html, "Test", src)

	if !strings.Contains(result, "medium.com/@someone/my-post-abc123") {
		t.Error("expected clean URL without scheme")
	}
	if strings.Contains(result, "https://medium.com") && !strings.Contains(result, `href="https://`) {
		t.Error("display URL should not have scheme prefix")
	}
}

func TestFormatByline_Empty(t *testing.T) {
	result := formatByline(sourceInfo{})
	if result != "" {
		t.Errorf("expected empty string for empty sourceInfo, got %q", result)
	}
}

func TestFormatByline_WithDate(t *testing.T) {
	pubDate := time.Date(2024, time.June, 10, 0, 0, 0, 0, time.UTC)
	src := sourceInfo{
		Byline:        "Jane Doe",
		SiteName:      "Tech Blog",
		PublishedTime: &pubDate,
	}
	result := formatByline(src)
	if !strings.Contains(result, "June 10, 2024") {
		t.Error("expected published date in byline")
	}
	if !strings.Contains(result, "Jane Doe") {
		t.Error("expected author in byline")
	}
	if !strings.Contains(result, "Tech Blog") {
		t.Error("expected site name in byline")
	}
}

func TestFormatByline_DateOnly(t *testing.T) {
	pubDate := time.Date(2023, time.January, 5, 0, 0, 0, 0, time.UTC)
	src := sourceInfo{PublishedTime: &pubDate}
	result := formatByline(src)
	if !strings.Contains(result, "January 5, 2023") {
		t.Error("expected published date in byline")
	}
	if !strings.Contains(result, `class="byline"`) {
		t.Error("expected byline paragraph when date is present")
	}
}
