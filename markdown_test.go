package main

import (
	"encoding/base64"
	"image/color"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------- unit tests for convertArticleToMarkdown ----------

func TestConvertArticleToMarkdown_Basic(t *testing.T) {
	html := `<html><body><h1>Hello World</h1><p>A simple paragraph.</p></body></html>`
	md, err := convertArticleToMarkdown(html)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "# Hello World") {
		t.Errorf("expected H1 heading, got:\n%s", md)
	}
	if !strings.Contains(md, "A simple paragraph.") {
		t.Errorf("expected paragraph text, got:\n%s", md)
	}
}

func TestConvertArticleToMarkdown_Headings(t *testing.T) {
	html := `<html><body><h1>Title</h1><h2>Section</h2><h3>Sub</h3><p>text</p></body></html>`
	md, err := convertArticleToMarkdown(html)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "# Title") {
		t.Errorf("expected # Title in:\n%s", md)
	}
	if !strings.Contains(md, "## Section") {
		t.Errorf("expected ## Section in:\n%s", md)
	}
	if !strings.Contains(md, "### Sub") {
		t.Errorf("expected ### Sub in:\n%s", md)
	}
}

func TestConvertArticleToMarkdown_Links(t *testing.T) {
	html := `<html><body><p>See <a href="https://example.com">example</a> for details.</p></body></html>`
	md, err := convertArticleToMarkdown(html)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "[example](https://example.com)") {
		t.Errorf("expected markdown link, got:\n%s", md)
	}
}

func TestConvertArticleToMarkdown_RegularImageURLs(t *testing.T) {
	html := `<html><body><img src="https://example.com/photo.jpg" alt="A photo"></body></html>`
	md, err := convertArticleToMarkdown(html)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "![A photo](https://example.com/photo.jpg)") {
		t.Errorf("expected markdown image, got:\n%s", md)
	}
}

func TestConvertArticleToMarkdown_DataURIImages(t *testing.T) {
	imgData := makePNG(50, 50, color.NRGBA{100, 150, 200, 255})
	uri := "data:image/png;base64," + base64.StdEncoding.EncodeToString(imgData)
	html := `<html><body><img src="` + uri + `" alt="a diagram"><p>text</p></body></html>`

	md, err := convertArticleToMarkdown(html)
	if err != nil {
		t.Fatal(err)
	}
	// Data URI should not appear in output
	if strings.Contains(md, "data:") {
		t.Errorf("data URI should be stripped, got:\n%s", md[:min(len(md), 200)])
	}
	// Alt text should appear as placeholder
	if !strings.Contains(md, "[Image: a diagram]") {
		t.Errorf("expected alt-text placeholder [Image: a diagram], got:\n%s", md)
	}
}

func TestConvertArticleToMarkdown_DataURINoAlt(t *testing.T) {
	imgData := makeJPEG(30, 30, color.NRGBA{200, 100, 50, 255})
	uri := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(imgData)
	html := `<html><body><p>before</p><img src="` + uri + `"><p>after</p></body></html>`

	md, err := convertArticleToMarkdown(html)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(md, "data:") {
		t.Errorf("data URI should be stripped, got:\n%s", md[:min(len(md), 200)])
	}
	if strings.Contains(md, "[Image:") {
		t.Errorf("no placeholder expected when alt is empty, got:\n%s", md)
	}
	if !strings.Contains(md, "before") || !strings.Contains(md, "after") {
		t.Errorf("surrounding text should be preserved, got:\n%s", md)
	}
}

func TestConvertArticleToMarkdown_CodeBlock(t *testing.T) {
	html := `<html><body><pre><code>func hello() {
    fmt.Println("hi")
}</code></pre></body></html>`
	md, err := convertArticleToMarkdown(html)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "```") {
		t.Errorf("expected fenced code block, got:\n%s", md)
	}
	if !strings.Contains(md, "fmt.Println") {
		t.Errorf("expected code content preserved, got:\n%s", md)
	}
}

func TestConvertArticleToMarkdown_Blockquote(t *testing.T) {
	html := `<html><body><blockquote><p>A famous quote.</p></blockquote></body></html>`
	md, err := convertArticleToMarkdown(html)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, ">") {
		t.Errorf("expected blockquote syntax, got:\n%s", md)
	}
}

func TestConvertArticleToMarkdown_StripsStyleTags(t *testing.T) {
	// renderFullHTML wraps content in a full HTML doc with inline <style>.
	// convertArticleToMarkdown should use extractBodyContent to avoid
	// rendering CSS rules as text.
	html := `<html><head><style>body { color: red; }</style></head><body><h1>Title</h1><p>Content.</p></body></html>`
	md, err := convertArticleToMarkdown(html)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(md, "color: red") {
		t.Errorf("CSS should not appear in markdown output, got:\n%s", md)
	}
	if !strings.Contains(md, "# Title") {
		t.Errorf("expected heading in markdown, got:\n%s", md)
	}
}

// ---------- unit tests for articlesToMarkdown ----------

func TestArticlesToMarkdown_Separator(t *testing.T) {
	articles := []epubArticle{
		{HTML: `<html><body><h1>First</h1><p>Article one.</p></body></html>`, Title: "First"},
		{HTML: `<html><body><h1>Second</h1><p>Article two.</p></body></html>`, Title: "Second"},
	}
	md, err := articlesToMarkdown(articles)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "# First") {
		t.Errorf("expected first article heading, got:\n%s", md)
	}
	if !strings.Contains(md, "# Second") {
		t.Errorf("expected second article heading, got:\n%s", md)
	}
	if !strings.Contains(md, "\n\n---\n\n") {
		t.Errorf("expected horizontal rule separator, got:\n%s", md)
	}
}

func TestArticlesToMarkdown_Empty(t *testing.T) {
	_, err := articlesToMarkdown(nil)
	if err == nil {
		t.Error("expected error for empty articles slice")
	}
}

// ---------- integration tests via run() ----------

func TestRun_MarkdownAndEpubMutuallyExclusive(t *testing.T) {
	cfg := cliConfig{
		markdownMode: true,
		epubMode:     true,
		concurrency:  1,
		args:         []string{"https://example.com"},
	}
	err := run(cfg)
	if err == nil {
		t.Fatal("expected error when both -markdown and -epub are set")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("expected 'mutually exclusive' error, got: %v", err)
	}
}

func TestRun_MarkdownMode_SingleURL(t *testing.T) {
	pageHTML := makeArticleHTML("Markdown Test Article", "A paragraph of content for testing markdown output.")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(pageHTML))
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "out.md")

	cfg := cliConfig{
		opts:         optimizeOpts{maxWidth: 800, quality: 60},
		output:       outFile,
		markdownMode: true,
		timeout:      5 * time.Second,
		userAgent:    "test-agent",
		concurrency:  2,
		args:         []string{srv.URL},
	}
	logOut = os.Stderr // ensure logOut is set

	if err := run(cfg); err != nil {
		t.Fatalf("run() error: %v", err)
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatal(err)
	}
	md := string(data)

	// Should be a markdown file, not HTML
	if strings.Contains(md, "<html") || strings.Contains(md, "<body") {
		t.Errorf("output should not contain HTML tags, got:\n%s", md[:min(len(md), 500)])
	}
	// Should have the article title as a heading
	if !strings.Contains(md, "Markdown Test Article") {
		t.Errorf("expected article title in markdown, got:\n%s", md)
	}
	// Should contain the paragraph text
	if !strings.Contains(md, "paragraph of content") {
		t.Errorf("expected article content in markdown, got:\n%s", md)
	}
}

func TestRun_MarkdownMode_MultiURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		var title string
		switch r.URL.Path {
		case "/article1":
			title = "First Multi Article"
		case "/article2":
			title = "Second Multi Article"
		default:
			w.WriteHeader(404)
			return
		}
		w.Write([]byte(makeArticleHTML(title, "Content of "+title+".")))
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "combined.md")

	cfg := cliConfig{
		opts:         optimizeOpts{maxWidth: 800, quality: 60},
		output:       outFile,
		markdownMode: true,
		timeout:      5 * time.Second,
		userAgent:    "test-agent",
		concurrency:  2,
		args:         []string{srv.URL + "/article1", srv.URL + "/article2"},
	}

	if err := run(cfg); err != nil {
		t.Fatalf("run() error: %v", err)
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatal(err)
	}
	md := string(data)

	if !strings.Contains(md, "First Multi Article") {
		t.Errorf("expected first article in output, got:\n%s", md)
	}
	if !strings.Contains(md, "Second Multi Article") {
		t.Errorf("expected second article in output, got:\n%s", md)
	}
	if !strings.Contains(md, "---") {
		t.Errorf("expected horizontal rule separator between articles, got:\n%s", md)
	}
}

func TestRun_MarkdownMode_DataURIImagesNotInOutput(t *testing.T) {
	imgData := makePNG(100, 100, color.NRGBA{100, 150, 200, 255})
	imgURI := dataURI("image/png", imgData)

	pageHTML := `<!DOCTYPE html>
<html><head><title>Image Test Article</title></head>
<body><article>
<h1>Image Test Article</h1>
<p>Article with an embedded image for markdown export testing. This paragraph
is long enough to satisfy the readability content extraction algorithm.</p>
<img src="` + imgURI + `" alt="test diagram">
<p>Text after the image to ensure extraction continues.</p>
</article></body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(pageHTML))
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "img_test.md")

	cfg := cliConfig{
		opts:         optimizeOpts{maxWidth: 800, quality: 60},
		output:       outFile,
		markdownMode: true,
		timeout:      5 * time.Second,
		userAgent:    "test-agent",
		concurrency:  2,
		args:         []string{srv.URL},
	}

	if err := run(cfg); err != nil {
		t.Fatalf("run() error: %v", err)
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatal(err)
	}
	md := string(data)

	if strings.Contains(md, "data:") {
		t.Errorf("data URI image should not appear in markdown output")
	}
}

// makeArticleHTML builds a minimal HTML article page with sufficient content
// for readability extraction.
func makeArticleHTML(title, extraContent string) string {
	return `<!DOCTYPE html>
<html><head><title>` + title + `</title></head>
<body><article>
<h1>` + title + `</h1>
<p>` + extraContent + `</p>
<p>This paragraph provides additional content to ensure the readability
algorithm correctly identifies this as the main article region. The algorithm
needs sufficient text density to extract the content reliably.</p>
<p>A third paragraph with more text about the topic at hand to further
increase the content weight of the article section.</p>
</article></body></html>`
}

