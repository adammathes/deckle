package main

import (
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
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
	URL      string // Original article URL
	Byline   string // Author name from metadata
	SiteName string // Site/publication name from metadata
}

// formatByline builds a byline HTML paragraph from the source info.
// Returns empty string if there's nothing to show.
func formatByline(src sourceInfo) string {
	var parts []string

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

	return text
}
