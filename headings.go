package main

import (
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	titleTagRe   = regexp.MustCompile(`(?i)<title>([^<]+)</title>`)
	firstH1Re    = regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>`)
	htmlTagRe    = regexp.MustCompile(`<[^>]+>`)
	headingRe    = regexp.MustCompile(`(?i)<(/?)h([1-6])([^>]*)>`)
	titleSplitRe = regexp.MustCompile(`\s*[-|\x{2013}\x{2014}]\s+`)
	bodyTagRe    = regexp.MustCompile(`(?i)(<body[^>]*>)`)
)

// extractTitle extracts the article title from <title> tag or first <h1>.
func extractTitle(text string) string {
	// Try <title> first
	if m := titleTagRe.FindStringSubmatch(text); m != nil {
		title := cleanTitle(html.UnescapeString(strings.TrimSpace(m[1])))
		if title != "" && title != "Untitled" {
			return title
		}
	}

	// Fall back to first <h1>
	if m := firstH1Re.FindStringSubmatch(text); m != nil {
		return strings.TrimSpace(htmlTagRe.ReplaceAllString(m[1], ""))
	}

	return "Untitled"
}

// cleanTitle removes common site name suffixes like "Article - Site Name".
func cleanTitle(title string) string {
	parts := titleSplitRe.Split(title, -1)
	result := strings.TrimSpace(parts[0])
	if result == "" {
		return "Untitled"
	}
	return result
}

// shiftHeadings shifts all headings down one level (h1->h2, h2->h3, ..., clamped at h6).
func shiftHeadings(text string) string {
	return headingRe.ReplaceAllStringFunc(text, func(match string) string {
		parts := headingRe.FindStringSubmatch(match)
		if parts == nil {
			return match
		}
		isClose := parts[1] == "/"
		level, _ := strconv.Atoi(parts[2])
		newLevel := level + 1
		if newLevel > 6 {
			newLevel = 6
		}
		if isClose {
			return fmt.Sprintf("</h%d>", newLevel)
		}
		return fmt.Sprintf("<h%d%s>", newLevel, parts[3])
	})
}

// sourceInfo holds attribution info for an article.
type sourceInfo struct {
	URL           string     // Original article URL
	Byline        string     // Author name from metadata
	SiteName      string     // Site/publication name from metadata
	PublishedTime *time.Time // Publication date, if available
}

// formatByline builds a byline HTML paragraph from the source info.
// Returns empty string if there's nothing to show.
func formatByline(src sourceInfo) string {
	var parts []string

	if src.PublishedTime != nil {
		parts = append(parts, html.EscapeString(src.PublishedTime.Format("January 2, 2006")))
	}
	if src.Byline != "" {
		parts = append(parts, html.EscapeString(src.Byline))
	}
	if src.SiteName != "" {
		parts = append(parts, html.EscapeString(src.SiteName))
	}

	byline := strings.Join(parts, " · ")

	if src.URL != "" {
		// Show a clean version of the URL (strip scheme)
		displayURL := src.URL
		for _, prefix := range []string{"https://", "http://"} {
			displayURL = strings.TrimPrefix(displayURL, prefix)
		}
		displayURL = strings.TrimSuffix(displayURL, "/")
		link := fmt.Sprintf(`<a href="%s">%s</a>`,
			html.EscapeString(src.URL), html.EscapeString(displayURL))
		if byline != "" {
			byline += "<br/>" + link
		} else {
			byline = link
		}
	}

	if byline == "" {
		return ""
	}
	return fmt.Sprintf(`<p class="byline">%s</p>`, byline)
}

// normalizeHeadings shifts all headings down one level and inserts an H1
// with the article title and optional byline. If titleOverride is non-empty,
// it is used instead of extracting the title from the HTML.
func normalizeHeadings(text string, titleOverride string, src sourceInfo) string {
	title := titleOverride
	if title != "" {
		title = cleanTitle(title)
	} else {
		title = extractTitle(text)
	}

	// Shift all existing headings down one level
	text = shiftHeadings(text)

	// Build the header block: H1 title + optional byline
	header := fmt.Sprintf("<h1>%s</h1>\n", html.EscapeString(title))
	if byline := formatByline(src); byline != "" {
		header += byline + "\n"
	}

	// Insert right after <body> (or at start if no body tag)
	if loc := bodyTagRe.FindStringIndex(text); loc != nil {
		pos := loc[1]
		text = text[:pos] + "\n" + header + text[pos:]
	} else {
		text = header + text
	}

	return renderFullHTML(text, title, src)
}

// renderFullHTML wraps the article fragment in a complete HTML document.
func renderFullHTML(fragment string, title string, src sourceInfo) string {
	lower := strings.ToLower(fragment)
	// If it already looks like a full HTML document, don't wrap it again
	if strings.Contains(lower, "<html") || strings.Contains(lower, "<!doctype") {
		return fragment
	}

	// If it already contains a body tag, we should be careful not to double wrap
	// but we still want to add the head and metadata if they are missing.
	// For simplicity, if it has <body> we'll assume it's "full enough" or
	// handled elsewhere, but readability fragments usually don't have it.
	if strings.Contains(lower, "<body") {
		return fragment
	}

	var headExtra strings.Builder
	if src.Byline != "" {
		fmt.Fprintf(&headExtra, "\t<meta name=\"author\" content=\"%s\">\n", html.EscapeString(src.Byline))
	}
	if src.PublishedTime != nil {
		fmt.Fprintf(&headExtra, "\t<meta name=\"date\" content=\"%s\">\n", src.PublishedTime.Format(time.RFC3339))
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
	<meta charset="utf-8">
	<meta name="viewport" content="width=device-width, initial-scale=1">
	<title>%s</title>
%s	<style>
		body {
			font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
			line-height: 1.6;
			color: #333;
			max-width: 800px;
			margin: 0 auto;
			padding: 2rem 1rem;
		}
		img { max-width: 100%%; height: auto; }
		pre { white-space: pre-wrap; word-wrap: break-word; }
		.byline { color: #666; font-style: italic; margin-bottom: 2rem; }
		blockquote { border-left: 4px solid #eee; padding-left: 1rem; margin-left: 0; color: #666; }
	</style>
</head>
<body>
%s
</body>
</html>
`, html.EscapeString(title), headExtra.String(), fragment)
}
