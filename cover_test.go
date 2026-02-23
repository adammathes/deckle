package main

import (
	"archive/zip"
	"bytes"
	"image/png"
	"strings"
	"testing"

	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
)

func TestGenerateCover_Basic(t *testing.T) {
	data, err := generateCover("Weekly Reads", 12)
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

func TestGenerateCover_Deterministic(t *testing.T) {
	a, err := generateCover("Same Title", 5)
	if err != nil {
		t.Fatal(err)
	}
	b, err := generateCover("Same Title", 5)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Error("same inputs should produce identical covers")
	}
}

func TestGenerateCover_DifferentTitles(t *testing.T) {
	a, err := generateCover("Title A", 3)
	if err != nil {
		t.Fatal(err)
	}
	b, err := generateCover("Title B", 3)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a, b) {
		t.Error("different titles should produce different covers")
	}
}

func TestGenerateCover_SingleArticle(t *testing.T) {
	data, err := generateCover("Solo Article", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("cover PNG is empty")
	}
}

func TestGenerateCover_LongTitle(t *testing.T) {
	title := "This Is a Very Long Title That Should Wrap Across Multiple Lines on the Cover Image"
	data, err := generateCover(title, 7)
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

	outPath := t.TempDir() + "/cover_test.epub"
	if err := buildEpub(articles, "Cover Test", outPath); err != nil {
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
