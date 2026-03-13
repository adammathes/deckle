package builder

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIsLocalPath(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"/tmp/article.html", true},
		{"./article.html", true},
		{"../article.html", true},
		{"~/docs/article.html", true},
		{"file:///tmp/article.html", true},
		{"https://example.com", false},
		{"http://example.com", false},
		{"example.com", false},
		{"article.html", false}, // relative without ./ prefix is not detected
	}
	for _, tt := range tests {
		if got := isLocalPath(tt.input); got != tt.want {
			t.Errorf("isLocalPath(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestResolveLocalPath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/tmp/article.html", "/tmp/article.html"},
		{"./article.html", "./article.html"},
		{"file:///tmp/article.html", "/tmp/article.html"},
	}
	for _, tt := range tests {
		if got := resolveLocalPath(tt.input); got != tt.want {
			t.Errorf("resolveLocalPath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestProcessLocalFile_HTML(t *testing.T) {
	dir := t.TempDir()
	htmlFile := filepath.Join(dir, "test-article.html")
	content := `<h1>My Local Article</h1>
<p>This is a local HTML article with some content for testing purposes.</p>
<p>It has multiple paragraphs to ensure the processing works correctly.</p>`

	if err := os.WriteFile(htmlFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	opts := optimizeOpts{maxWidth: 800, quality: 60, skipImageFetch: true}
	html, title, _, err := processLocalFile(htmlFile, opts, "", 5)
	if err != nil {
		t.Fatal(err)
	}

	if title != "My Local Article" {
		t.Errorf("title = %q, want %q", title, "My Local Article")
	}
	if !strings.Contains(html, "local HTML article") {
		t.Error("output should contain the article content")
	}
}

func TestProcessLocalFile_Markdown(t *testing.T) {
	dir := t.TempDir()
	mdFile := filepath.Join(dir, "test-article.md")
	content := `# Markdown Article

This is a **markdown** article with some content.

## Section One

More content in the first section.
`
	if err := os.WriteFile(mdFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	opts := optimizeOpts{maxWidth: 800, quality: 60, skipImageFetch: true}
	html, title, _, err := processLocalFile(mdFile, opts, "", 5)
	if err != nil {
		t.Fatal(err)
	}

	if title != "Markdown Article" {
		t.Errorf("title = %q, want %q", title, "Markdown Article")
	}
	// Markdown bold should be converted to <strong>
	if !strings.Contains(html, "<strong>markdown</strong>") {
		t.Error("expected markdown bold to be converted to HTML")
	}
	// Section heading should be present (shifted from h2 to h3)
	if !strings.Contains(html, "Section One") {
		t.Error("expected section heading in output")
	}
}

func TestProcessLocalFile_TitleFallbackToFilename(t *testing.T) {
	dir := t.TempDir()
	htmlFile := filepath.Join(dir, "my-summary.html")
	// No <h1> or <title> — should fall back to filename
	content := `<p>Just some content without a heading.</p>`
	if err := os.WriteFile(htmlFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	opts := optimizeOpts{maxWidth: 800, quality: 60, skipImageFetch: true}
	_, title, _, err := processLocalFile(htmlFile, opts, "", 5)
	if err != nil {
		t.Fatal(err)
	}

	if title != "my-summary" {
		t.Errorf("title = %q, want %q (filename without extension)", title, "my-summary")
	}
}

func TestProcessLocalFile_TitleOverride(t *testing.T) {
	dir := t.TempDir()
	htmlFile := filepath.Join(dir, "article.html")
	content := `<h1>Original Title</h1><p>Some content.</p>`
	if err := os.WriteFile(htmlFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	opts := optimizeOpts{maxWidth: 800, quality: 60, skipImageFetch: true}
	_, title, _, err := processLocalFile(htmlFile, opts, "Custom Title", 5)
	if err != nil {
		t.Fatal(err)
	}

	if title != "Custom Title" {
		t.Errorf("title = %q, want %q", title, "Custom Title")
	}
}

func TestProcessURL_LocalPath(t *testing.T) {
	dir := t.TempDir()
	htmlFile := filepath.Join(dir, "local.html")
	content := `<h1>Local via processURL</h1>
<p>Testing that processURL routes local paths correctly.</p>`
	if err := os.WriteFile(htmlFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	opts := optimizeOpts{maxWidth: 800, quality: 60, skipImageFetch: true}
	html, title, _, err := processURL(htmlFile, opts, 5*time.Second, "test-agent", "", 5)
	if err != nil {
		t.Fatal(err)
	}

	if title != "Local via processURL" {
		t.Errorf("title = %q, want %q", title, "Local via processURL")
	}
	if !strings.Contains(html, "routes local paths correctly") {
		t.Error("expected local file content in output")
	}
}

func TestBuild_LocalHTMLFile(t *testing.T) {
	dir := t.TempDir()
	htmlFile := filepath.Join(dir, "article.html")
	content := `<h1>Build Local Test</h1>
<p>Testing Build() with a local HTML file input.</p>`
	if err := os.WriteFile(htmlFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	opts := Options{
		Format:    "html",
		MaxWidth:  800,
		Quality:   60,
		Timeout:   5 * time.Second,
		UserAgent: "test-agent",
		Writer:    &buf,
	}

	err := Build([]string{htmlFile}, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Build Local Test") {
		t.Error("expected article title in output")
	}
}

func TestBuild_LocalMarkdownFile(t *testing.T) {
	dir := t.TempDir()
	mdFile := filepath.Join(dir, "article.md")
	content := `# Markdown Build Test

This is a **markdown** file processed through Build().
`
	if err := os.WriteFile(mdFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	opts := Options{
		Format:    "html",
		MaxWidth:  800,
		Quality:   60,
		Timeout:   5 * time.Second,
		UserAgent: "test-agent",
		Writer:    &buf,
	}

	err := Build([]string{mdFile}, opts)
	if err != nil {
		t.Fatal(err)
	}
	output := buf.String()
	if !strings.Contains(output, "Markdown Build Test") {
		t.Error("expected article title in output")
	}
	if !strings.Contains(output, "<strong>markdown</strong>") {
		t.Error("expected markdown bold converted to HTML")
	}
}

func TestBuild_MixedLocalAndURL(t *testing.T) {
	// Create a local HTML file
	dir := t.TempDir()
	htmlFile := filepath.Join(dir, "summary.html")
	localContent := `<h1>AI Summary</h1>
<p>This is a locally generated AI summary of recent articles.</p>`
	if err := os.WriteFile(htmlFile, []byte(localContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a local markdown file
	mdFile := filepath.Join(dir, "notes.md")
	mdContent := `# Editor Notes

Some **editorial notes** about the articles.
`
	if err := os.WriteFile(mdFile, []byte(mdContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a test HTTP server for a URL
	pageHTML := `<!DOCTYPE html>
<html><head><title>Remote Article</title></head><body>
<article>
<h1>Remote Article</h1>
<p>This is a remote article fetched from a URL. It has enough content
for readability to extract it as the main article content. More text here.</p>
<p>Second paragraph with additional content for the readability algorithm.</p>
</article>
</body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(pageHTML))
	}))
	defer srv.Close()

	// Build epub with mixed inputs: local HTML + local MD + remote URL
	outFile := filepath.Join(dir, "output.epub")
	opts := Options{
		Format:    "epub",
		Output:    outFile,
		Title:     "Mixed Input Test",
		MaxWidth:  800,
		Quality:   60,
		Timeout:   5 * time.Second,
		UserAgent: "test-agent",
		Writer:    io.Discard,
	}

	err := Build([]string{htmlFile, mdFile, srv.URL}, opts)
	if err != nil {
		t.Fatal(err)
	}

	// Verify the epub file was created
	info, err := os.Stat(outFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Error("epub file should not be empty")
	}
}

func TestBuild_LocalFileNotFound(t *testing.T) {
	var buf bytes.Buffer
	opts := Options{
		Format:    "html",
		MaxWidth:  800,
		Quality:   60,
		Timeout:   5 * time.Second,
		UserAgent: "test-agent",
		Writer:    &buf,
	}

	err := Build([]string{"/tmp/nonexistent-file-12345.html"}, opts)
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestBuild_NoInputs(t *testing.T) {
	opts := Options{
		Format: "html",
		Writer: io.Discard,
	}
	err := Build(nil, opts)
	if err == nil {
		t.Error("expected error when no inputs provided")
	}
	if !strings.Contains(err.Error(), "no inputs provided") {
		t.Errorf("expected 'no inputs provided' error, got: %v", err)
	}
}

func TestProcessLocalFile_FileURIScheme(t *testing.T) {
	dir := t.TempDir()
	htmlFile := filepath.Join(dir, "article.html")
	content := `<h1>File URI Test</h1><p>Content via file:// URI.</p>`
	if err := os.WriteFile(htmlFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	opts := optimizeOpts{maxWidth: 800, quality: 60, skipImageFetch: true}
	html, title, _, err := processURL("file://"+htmlFile, opts, 5*time.Second, "test-agent", "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if title != "File URI Test" {
		t.Errorf("title = %q, want %q", title, "File URI Test")
	}
	if !strings.Contains(html, "file:// URI") {
		t.Error("expected content from file:// URI")
	}
}
