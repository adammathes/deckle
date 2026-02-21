package main

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"net/url"
	"strings"
	"testing"
)

func TestExtractArticle_BasicHTML(t *testing.T) {
	html := `<html><head><title>Test Article</title></head><body>
		<nav><a href="/">Home</a><a href="/about">About</a></nav>
		<article>
			<h1>Test Article</h1>
			<p>This is a test article with enough content to be considered the main article.
			It needs to be reasonably long so that readability considers it significant content.
			Here is another paragraph to add more text. And another sentence for good measure.
			The readability algorithm needs substantial text to work properly.</p>
			<p>Second paragraph with more content. This helps readability determine that this
			is indeed the main article content of the page. More text here for thoroughness.
			And even more text to ensure this passes the readability threshold easily.</p>
		</article>
		<footer>Copyright 2024</footer>
	</body></html>`

	u, _ := url.Parse("https://example.com/article")
	content, meta, err := extractArticle([]byte(html), u)
	if err != nil {
		t.Fatal(err)
	}

	if meta.Title != "Test Article" {
		t.Errorf("title = %q, want %q", meta.Title, "Test Article")
	}

	if !strings.Contains(content, "test article with enough content") {
		t.Error("expected article content in output")
	}
}

func TestExtractArticle_PreservesDataURIs(t *testing.T) {
	// Create a small PNG image
	img := image.NewNRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			img.Set(x, y, color.NRGBA{255, 0, 0, 255})
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	dataURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())

	html := `<html><head><title>Image Test</title></head><body>
		<article>
			<h1>Image Test</h1>
			<p>This article contains an image with a data URI that should be preserved.
			It needs enough text so readability considers this the main content area.
			Here is additional text padding for the readability algorithm.</p>
			<img src="` + dataURI + `" alt="test image">
			<p>More article content here. This paragraph adds more text to the article
			so that readability is confident this is the main content region. The more
			text we have, the more confident readability will be in extracting it.</p>
		</article>
	</body></html>`

	u, _ := url.Parse("https://example.com/article")
	content, _, err := extractArticle([]byte(html), u)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(content, "data:image/png;base64,") {
		t.Error("expected data URI to be preserved in article content")
	}
}

func TestExtractArticle_EmptyContent(t *testing.T) {
	// Minimal HTML that readability can't extract meaningful content from
	html := `<html><head><title>Empty</title></head><body></body></html>`
	u, _ := url.Parse("https://example.com/empty")
	_, _, err := extractArticle([]byte(html), u)
	if err == nil {
		t.Error("expected error for empty content")
	}
	if err != nil && !strings.Contains(err.Error(), "no content") {
		t.Errorf("expected 'no content' error, got: %v", err)
	}
}

func TestExtractArticle_WithMetadata(t *testing.T) {
	html := `<html><head>
		<title>Metadata Test</title>
		<meta name="author" content="John Doe">
		<meta property="og:site_name" content="Test Site">
	</head><body>
		<article>
			<h1>Metadata Test</h1>
			<p>This article tests metadata extraction. It has enough content for
			readability to identify it as the main article. More text is needed
			to ensure the algorithm works correctly and finds this as main content.</p>
			<p>Second paragraph with additional content to boost the text density.
			The readability algorithm uses content length as one of its signals.
			Having substantial text helps it identify the main article region.</p>
		</article>
	</body></html>`

	u, _ := url.Parse("https://example.com/article")
	_, meta, err := extractArticle([]byte(html), u)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(meta.Title, "Metadata Test") {
		t.Errorf("title = %q, expected to contain 'Metadata Test'", meta.Title)
	}
}
