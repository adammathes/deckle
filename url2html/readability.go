package main

import (
	"bytes"
	"fmt"
	"net/url"

	readability "codeberg.org/readeck/go-readability"
)

// articleMeta holds metadata extracted alongside the article content.
type articleMeta struct {
	Title    string
	Byline   string // Author attribution (e.g. "Steve Yegge")
	SiteName string // Publication name (e.g. "Medium")
}

// extractArticle runs go-readability on the HTML and returns the article
// HTML content and metadata.
func extractArticle(htmlBytes []byte, pageURL *url.URL) (content string, meta articleMeta, err error) {
	article, err := readability.FromReader(bytes.NewReader(htmlBytes), pageURL)
	if err != nil {
		return "", articleMeta{}, fmt.Errorf("readability extraction failed: %w", err)
	}

	if article.Content == "" {
		return "", articleMeta{}, fmt.Errorf("readability extracted no content from %s", pageURL)
	}

	meta = articleMeta{
		Title:    article.Title,
		Byline:   article.Byline,
		SiteName: article.SiteName,
	}
	return article.Content, meta, nil
}
