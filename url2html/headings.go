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

// normalizeHeadings shifts all headings down one level and inserts an H1
// with the article title. If titleOverride is non-empty, it is used instead
// of extracting the title from the HTML.
func normalizeHeadings(text string, titleOverride string) string {
	title := titleOverride
	if title != "" {
		title = cleanTitle(title)
	} else {
		title = extractTitle(text)
	}

	// Shift all existing headings down one level
	text = shiftHeadings(text)

	// Insert H1 title right after <body> (or at start if no body tag)
	titleHTML := fmt.Sprintf("<h1>%s</h1>\n", html.EscapeString(title))
	if loc := bodyTagRe.FindStringIndex(text); loc != nil {
		pos := loc[1]
		text = text[:pos] + "\n" + titleHTML + text[pos:]
	} else {
		text = titleHTML + text
	}

	return text
}
