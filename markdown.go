// Markdown export: converts processed articles to CommonMark Markdown.
package main

import (
	"fmt"
	"strings"
	"sync"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/dom"
	"golang.org/x/net/html"
)

var (
	mdConverter     *converter.Converter
	mdConverterOnce sync.Once
)

// getMarkdownConverter returns a shared converter that replaces base64 data URI
// images with alt-text placeholders instead of embedding the raw data URI.
func getMarkdownConverter() *converter.Converter {
	mdConverterOnce.Do(func() {
		mdConverter = converter.NewConverter(
			converter.WithPlugins(
				base.NewBasePlugin(),
				commonmark.NewCommonmarkPlugin(),
			),
		)
		// Override img rendering: strip data URIs, keep plain URLs as-is.
		// PriorityEarly (100) runs before the commonmark plugin (PriorityStandard 500).
		mdConverter.Register.RendererFor("img", converter.TagTypeInline,
			func(ctx converter.Context, w converter.Writer, n *html.Node) converter.RenderStatus {
				src := dom.GetAttributeOr(n, "src", "")
				if !strings.HasPrefix(src, "data:") {
					// Regular URL – let the default commonmark handler take over.
					return converter.RenderTryNext
				}
				// Data URI: emit alt text as a placeholder, or nothing.
				alt := dom.GetAttributeOr(n, "alt", "")
				alt = strings.TrimSpace(alt)
				if alt != "" {
					w.WriteString("[Image: " + alt + "]")
				}
				return converter.RenderSuccess
			},
			converter.PriorityEarly,
		)
	})
	return mdConverter
}

// convertArticleToMarkdown converts a processed article HTML string (as
// returned by processURL or renderFullHTML) to CommonMark Markdown.
// Base64 data URI images are replaced by alt-text placeholders.
func convertArticleToMarkdown(htmlStr string) (string, error) {
	body := extractBodyContent(htmlStr)
	md, err := getMarkdownConverter().ConvertString(body)
	if err != nil {
		return "", fmt.Errorf("markdown conversion: %w", err)
	}
	return strings.TrimSpace(md), nil
}

// articlesToMarkdown converts a slice of processed articles to a single
// Markdown document. Articles are separated by a horizontal rule.
func articlesToMarkdown(articles []epubArticle) (string, error) {
	var parts []string
	for _, a := range articles {
		md, err := convertArticleToMarkdown(a.HTML)
		if err != nil {
			fmt.Fprintf(logOut, "Warning: markdown conversion failed for %q: %v\n", a.Title, err)
			continue
		}
		parts = append(parts, md)
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("no articles converted to markdown")
	}
	return strings.Join(parts, "\n\n---\n\n"), nil
}

