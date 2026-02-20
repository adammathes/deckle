package main

import (
	"archive/zip"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

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

	articles := []string{
		`<html><body><h1>First Article</h1><p>Some content here.</p></body></html>`,
		`<html><body><h1>Second Article</h1><p>More content.</p><img src="` + imgURI + `" alt="test"></body></html>`,
	}

	outPath := filepath.Join(t.TempDir(), "test.epub")
	err := buildEpub(articles, "Test Book", outPath)
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
}

func TestBuildEpub_EpubCheck(t *testing.T) {
	// Only run if epubcheck is available
	if _, err := os.Stat("/usr/bin/epubcheck"); err != nil {
		t.Skip("epubcheck not installed")
	}

	imgData := makeJPEG(200, 150, color.NRGBA{0, 100, 200, 255})
	imgURI := dataURI("image/jpeg", imgData)

	articles := []string{
		`<html><body><h1>Chapter One</h1>
		<p>This is a test chapter with some content for validation.</p>
		<img src="` + imgURI + `" alt="test image"/>
		<p>Another paragraph after the image.</p></body></html>`,
		`<html><body><h1>Chapter Two</h1>
		<p>Second chapter with more content to test epub generation.</p></body></html>`,
	}

	outPath := filepath.Join(t.TempDir(), "check.epub")
	err := buildEpub(articles, "EpubCheck Test", outPath)
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
