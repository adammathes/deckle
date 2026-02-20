package main

import (
	"bytes"
	"fmt"
	"net/url"

	readability "codeberg.org/readeck/go-readability"
)

// extractArticle runs go-readability on the HTML and returns the article
// HTML content and extracted title.
func extractArticle(htmlBytes []byte, pageURL *url.URL) (content string, title string, err error) {
	article, err := readability.FromReader(bytes.NewReader(htmlBytes), pageURL)
	if err != nil {
		return "", "", fmt.Errorf("readability extraction failed: %w", err)
	}

	if article.Content == "" {
		return "", "", fmt.Errorf("readability extracted no content from %s", pageURL)
	}

	return article.Content, article.Title, nil
}
