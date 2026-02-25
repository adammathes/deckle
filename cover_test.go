package main

import (
	"archive/zip"
	"bytes"
	"image/png"
	"strings"
	"testing"
	"path/filepath"

	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
)

func TestGenerateCover_Collage(t *testing.T) {
	articles := []epubArticle{
		{Title: "Article 1", Byline: "Author 1", SiteName: "Site 1"},
		{Title: "Article 2", Byline: "Author 2", SiteName: "Site 2"},
	}
	data, err := generateCover("Weekly Reads", articles, "collage")
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("cover PNG is empty")
	}

	// Decode to verify it's a valid PNG with expected dimensions
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("invalid PNG: %v", err)
	}
	bounds := img.Bounds()
	if bounds.Dx() != coverWidth || bounds.Dy() != coverHeight {
		t.Errorf("cover size = %dx%d, want %dx%d", bounds.Dx(), bounds.Dy(), coverWidth, coverHeight)
	}
}

func TestGenerateCover_Pattern(t *testing.T) {
	articles := []epubArticle{
		{Title: "Article 1"},
	}
	data, err := generateCover("Weekly Reads", articles, "pattern")
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("cover PNG is empty")
	}
}

func TestGenerateCover_Deterministic(t *testing.T) {
	articles := []epubArticle{{Title: "A"}}
	a, err := generateCover("Same Title", articles, "collage")
	if err != nil {
		t.Fatal(err)
	}
	b, err := generateCover("Same Title", articles, "collage")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Error("same inputs should produce identical covers")
	}
}

func TestGenerateCover_DifferentStyles(t *testing.T) {
	articles := []epubArticle{{Title: "A"}}
	a, err := generateCover("Same Title", articles, "collage")
	if err != nil {
		t.Fatal(err)
	}
	b, err := generateCover("Same Title", articles, "pattern")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a, b) {
		t.Error("different styles should produce different covers")
	}
}

func TestGenerateCover_LongTitle(t *testing.T) {
	title := "This Is a Very Long Title That Should Wrap Across Multiple Lines on the Cover Image"
	articles := []epubArticle{{Title: "A"}}
	data, err := generateCover(title, articles, "collage")
	if err != nil {
		t.Fatal(err)
	}

	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("invalid PNG: %v", err)
	}
	bounds := img.Bounds()
	if bounds.Dx() != coverWidth || bounds.Dy() != coverHeight {
		t.Errorf("cover size = %dx%d, want %dx%d", bounds.Dx(), bounds.Dy(), coverWidth, coverHeight)
	}
}

func TestWrapText(t *testing.T) {
	face, err := loadFace(goregular.TTF, 32)
	if err != nil {
		t.Fatal(err)
	}

	lines := wrapText("Hello World", face, 10000)
	if len(lines) != 1 {
		t.Errorf("short text should be 1 line, got %d", len(lines))
	}

	lines = wrapText("This is a much longer piece of text that definitely needs wrapping", face, 200)
	if len(lines) < 2 {
		t.Errorf("long text with narrow width should wrap, got %d lines", len(lines))
	}
}

func TestWrapText_EmptyString(t *testing.T) {
	face, err := loadFace(goregular.TTF, 32)
	if err != nil {
		t.Fatal(err)
	}
	lines := wrapText("", face, 500)
	if len(lines) != 1 {
		t.Errorf("empty string should produce 1 line, got %d", len(lines))
	}
}

func TestSplitWords(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"hello world", 2},
		{"one", 1},
		{"  spaces  between  ", 2},
		{"tabs\there", 2},
		{"", 0},
	}
	for _, tt := range tests {
		words := splitWords(tt.input)
		if len(words) != tt.want {
			t.Errorf("splitWords(%q) = %v (len %d), want len %d", tt.input, words, len(words), tt.want)
		}
	}
}

func TestSplitWords_Content(t *testing.T) {
	words := splitWords("  hello\tworld\nnewline  ")
	if len(words) != 3 {
		t.Fatalf("expected 3 words, got %d: %v", len(words), words)
	}
	if words[0] != "hello" || words[1] != "world" || words[2] != "newline" {
		t.Errorf("got %v, want [hello world newline]", words)
	}
}

func TestSplitWords_Unicode(t *testing.T) {
	words := splitWords("café résumé naïve")
	if len(words) != 3 {
		t.Fatalf("expected 3 words, got %d: %v", len(words), words)
	}
	if words[0] != "café" || words[1] != "résumé" || words[2] != "naïve" {
		t.Errorf("got %v, want [café résumé naïve]", words)
	}
}

func BenchmarkSplitWords(b *testing.B) {
	title := "This Is a Very Long Title That Should Wrap Across Multiple Lines on the Cover Image for Testing Performance"
	for b.Loop() {
		splitWords(title)
	}
}

func TestLoadFace(t *testing.T) {
	face, err := loadFace(gobold.TTF, 48)
	if err != nil {
		t.Fatal(err)
	}
	m := face.Metrics()
	if m.Height <= 0 {
		t.Error("font face should have positive height")
	}
}

func TestBuildEpub_HasCover(t *testing.T) {
	articles := []epubArticle{
		{
			HTML:  `<html><body><h1>Test Article</h1><p>Content.</p></body></html>`,
			Title: "Test Article",
			URL:   "https://example.com/test",
		},
	}

	outPath := filepath.Join(t.TempDir(), "cover_test.epub")
	if err := buildEpub(articles, "Cover Test", outPath, "collage"); err != nil {
		t.Fatal(err)
	}

	// Open the epub (zip) and check for cover.png
	zr, err := zip.OpenReader(outPath)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()

	hasCover := false
	for _, f := range zr.File {
		if strings.Contains(f.Name, "cover.png") {
			hasCover = true
			break
		}
	}
	if !hasCover {
		t.Error("epub should contain cover.png")
		for _, f := range zr.File {
			t.Logf("  %s", f.Name)
		}
	}
}

func TestBuildEpub_NoCover(t *testing.T) {
	articles := []epubArticle{
		{
			HTML:  `<html><body><h1>Test Article</h1><p>Content.</p></body></html>`,
			Title: "Test Article",
			URL:   "https://example.com/test",
		},
	}

	outPath := filepath.Join(t.TempDir(), "nocover_test.epub")
	if err := buildEpub(articles, "Cover Test", outPath, "none"); err != nil {
		t.Fatal(err)
	}

	// Open the epub (zip) and check that cover.png is MISSING
	zr, err := zip.OpenReader(outPath)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()

	hasCover := false
	for _, f := range zr.File {
		if strings.Contains(f.Name, "cover.png") {
			hasCover = true
			break
		}
	}
	if hasCover {
		t.Error("epub should NOT contain cover.png when style is 'none'")
	}
}
