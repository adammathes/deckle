// Epub generation from HTML articles using go-epub.
// Replaces pandoc for combining multiple articles into an epub3 with TOC.
package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	gohtml "html"
	"regexp"
	"strings"
	"time"

	epub "github.com/go-shiori/go-epub"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

var (
	// Matches <img src="data:MIME;base64,DATA"> for extracting embedded images
	imgDataURIRe = regexp.MustCompile(`(<img\b[^>]*?\bsrc\s*=\s*")data:([^;]+);base64,([^"]*)(")`)
	// Strips HTML tags for plain text extraction
	stripTagsRe = regexp.MustCompile(`<[^>]*>`)
)

// epubArticle holds a processed article and its metadata for epub inclusion.
type epubArticle struct {
	HTML          string     // Full HTML (with <body> tags)
	Title         string     // Cleaned article title
	URL           string     // Original source URL
	Byline        string     // Author name from metadata
	SiteName      string     // Publication name from metadata
	PublishedTime *time.Time // Publication date, if available
}

// extractBodyContent extracts the content between <body> and </body> tags.
// If no body tags are found, returns the full HTML.
func extractBodyContent(html string) string {
	lower := strings.ToLower(html)
	start := strings.Index(lower, "<body")
	if start < 0 {
		return html
	}
	// Skip past the <body...> tag
	end := strings.Index(html[start:], ">")
	if end < 0 {
		return html
	}
	start = start + end + 1

	bodyEnd := strings.Index(lower[start:], "</body>")
	if bodyEnd < 0 {
		return html[start:]
	}
	return html[start : start+bodyEnd]
}

// extractH1Title extracts the text content of the first H1 element.
func extractH1Title(html string) string {
	m := firstH1Re.FindStringSubmatch(html)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(stripTagsRe.ReplaceAllString(m[1], ""))
}

// extractImages finds all base64 data URI images in the HTML body,
// registers them with the epub, and rewrites src attributes to internal paths.
func extractImages(e *epub.Epub, body string, chapterIdx int) (string, error) {
	imgIdx := 0
	var lastErr error

	result := imgDataURIRe.ReplaceAllStringFunc(body, func(match string) string {
		parts := imgDataURIRe.FindStringSubmatch(match)
		if parts == nil {
			return match
		}
		prefix := parts[1]  // <img ... src="
		mime := parts[2]    // image/jpeg
		b64data := parts[3] // base64 data
		suffix := parts[4]  // "

		// Determine file extension from MIME type
		ext := ".jpg"
		switch {
		case strings.Contains(mime, "png"):
			ext = ".png"
		case strings.Contains(mime, "gif"):
			ext = ".gif"
		case strings.Contains(mime, "svg"):
			ext = ".svg"
		case strings.Contains(mime, "webp"):
			ext = ".webp"
		}

		filename := fmt.Sprintf("ch%03d_img%03d%s", chapterIdx, imgIdx, ext)
		imgIdx++

		// Decode base64 to verify it's valid
		_, err := base64.StdEncoding.DecodeString(b64data)
		if err != nil {
			// Try raw encoding (no padding)
			_, err = base64.RawStdEncoding.DecodeString(b64data)
			if err != nil {
				fmt.Fprintf(logOut, "Warning: invalid base64 for %s: %v\n", filename, err)
				return match
			}
		}

		// go-epub accepts data URIs directly via AddImage
		dataURI := "data:" + mime + ";base64," + b64data
		internalPath, err := e.AddImage(dataURI, filename)
		if err != nil {
			fmt.Fprintf(logOut, "Warning: failed to add image %s: %v\n", filename, err)
			lastErr = err
			return match
		}

		return prefix + internalPath + suffix
	})

	return result, lastErr
}

// allowedAttrs defines which attributes are safe for XHTML epub content.
// Returns true if the attribute should be kept.
func isAllowedAttr(a html.Attribute) bool {
	key := a.Key
	// Standard safe attributes
	switch key {
	case "id", "class", "style", "title", "lang", "dir",
		"href", "src", "alt", "width", "height",
		"colspan", "rowspan", "scope", "headers",
		"cite", "datetime", "name", "value", "type",
		"rel", "media", "start", "reversed":
		return true
	}
	// aria-* attributes are allowed in epub
	if strings.HasPrefix(key, "aria-") {
		return true
	}
	// epub:type is allowed
	if key == "epub:type" {
		return true
	}
	return false
}

// voidElements are HTML elements that must be self-closing in XHTML.
var voidElements = map[atom.Atom]bool{
	atom.Area: true, atom.Base: true, atom.Br: true, atom.Col: true,
	atom.Embed: true, atom.Hr: true, atom.Img: true, atom.Input: true,
	atom.Link: true, atom.Meta: true, atom.Source: true, atom.Wbr: true,
}

// sanitizeForXHTML converts HTML to valid XHTML for epub.
// Strips non-standard attributes, ensures self-closing void elements,
// and removes broken fragment links.
func sanitizeForXHTML(htmlStr string) string {
	doc, err := html.Parse(strings.NewReader(htmlStr))
	if err != nil {
		return htmlStr // fallback: return as-is
	}

	// Collect all IDs in the document
	ids := map[string]bool{}
	var collectIDs func(*html.Node)
	collectIDs = func(n *html.Node) {
		if n.Type == html.ElementNode {
			for _, a := range n.Attr {
				if a.Key == "id" {
					ids[a.Val] = true
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			collectIDs(c)
		}
	}
	collectIDs(doc)

	// Walk and clean the tree
	var clean func(*html.Node)
	clean = func(n *html.Node) {
		if n.Type == html.ElementNode {
			// Filter attributes
			var filtered []html.Attribute
			for _, a := range n.Attr {
				if !isAllowedAttr(a) {
					continue
				}
				// Fix broken fragment links
				if a.Key == "href" && strings.HasPrefix(a.Val, "#") {
					frag := a.Val[1:]
					if frag != "" && !ids[frag] {
						continue // drop href to non-existent ID
					}
				}
				filtered = append(filtered, a)
			}
			n.Attr = filtered
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			clean(c)
		}
	}
	clean(doc)

	// Render as XHTML
	var buf bytes.Buffer
	renderXHTML(&buf, doc)

	result := buf.String()

	// html.Parse wraps in <html><head><body>, extract just the body content
	if idx := strings.Index(result, "<body>"); idx >= 0 {
		result = result[idx+len("<body>"):]
		if end := strings.LastIndex(result, "</body>"); end >= 0 {
			result = result[:end]
		}
	}

	return result
}

// renderXHTML renders an html.Node tree as XHTML (self-closing void elements).
func renderXHTML(buf *bytes.Buffer, n *html.Node) {
	switch n.Type {
	case html.TextNode:
		buf.WriteString(html.EscapeString(n.Data))
	case html.ElementNode:
		buf.WriteByte('<')
		buf.WriteString(n.Data)
		for _, a := range n.Attr {
			buf.WriteByte(' ')
			buf.WriteString(a.Key)
			buf.WriteString(`="`)
			buf.WriteString(html.EscapeString(a.Val))
			buf.WriteByte('"')
		}
		if voidElements[n.DataAtom] && n.FirstChild == nil {
			buf.WriteString("/>")
			return
		}
		buf.WriteByte('>')
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			renderXHTML(buf, c)
		}
		buf.WriteString("</")
		buf.WriteString(n.Data)
		buf.WriteByte('>')
	case html.DocumentNode:
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			renderXHTML(buf, c)
		}
	case html.CommentNode:
		// skip comments
	case html.RawNode:
		buf.WriteString(n.Data)
	}
}

// buildTOCBody generates the HTML body for the front matter table of contents.
// It creates a linked list of articles with their authors and source URLs.
func buildTOCBody(articles []epubArticle) string {
	var b strings.Builder
	b.WriteString("<h1>Contents</h1>\n<ol class=\"toc\">\n")
	for i, a := range articles {
		filename := fmt.Sprintf("article%03d.xhtml", i+1)
		title := a.Title
		if title == "" {
			title = fmt.Sprintf("Article %d", i+1)
		}
		b.WriteString("<li>\n")
		b.WriteString(fmt.Sprintf(`<a href="%s">%s</a>`, filename, gohtml.EscapeString(title)))
		b.WriteByte('\n')

		// Build metadata line: date · author · site · url
		var meta []string
		if a.PublishedTime != nil {
			meta = append(meta, gohtml.EscapeString(a.PublishedTime.Format("January 2, 2006")))
		}
		if a.Byline != "" {
			meta = append(meta, gohtml.EscapeString(a.Byline))
		}
		if a.SiteName != "" {
			meta = append(meta, gohtml.EscapeString(a.SiteName))
		}
		metaLine := strings.Join(meta, " · ")

		if a.URL != "" {
			displayURL := a.URL
			for _, prefix := range []string{"https://", "http://"} {
				displayURL = strings.TrimPrefix(displayURL, prefix)
			}
			displayURL = strings.TrimSuffix(displayURL, "/")
			link := fmt.Sprintf(`<a href="%s">%s</a>`,
				gohtml.EscapeString(a.URL), gohtml.EscapeString(displayURL))
			if metaLine != "" {
				metaLine += "<br/>" + link
			} else {
				metaLine = link
			}
		}

		if metaLine != "" {
			b.WriteString(fmt.Sprintf(`<p class="toc-meta">%s</p>`, metaLine))
			b.WriteByte('\n')
		}
		b.WriteString("</li>\n")
	}
	b.WriteString("</ol>\n")
	return b.String()
}

// buildEpub creates an epub3 file from a list of articles with metadata.
// It generates a front matter table of contents followed by the article sections.
func buildEpub(articles []epubArticle, title string, outputPath string) error {
	e, err := epub.NewEpub(title)
	if err != nil {
		return fmt.Errorf("creating epub: %w", err)
	}
	e.SetLang("en")
	e.SetAuthor("deckle")

	// Add minimal CSS for readability on e-readers
	css := `body { margin: 1em; line-height: 1.5; }
img { max-width: 100%; height: auto; }
pre, code { font-size: 0.85em; }
blockquote { margin-left: 1em; padding-left: 0.5em; border-left: 2px solid #999; }
.byline { font-size: 0.85em; color: #666; margin-top: -0.5em; margin-bottom: 1.5em; }
.byline a { color: #666; }
.toc { list-style-type: none; padding-left: 0; }
.toc li { margin-bottom: 1.2em; }
.toc a { text-decoration: none; }
.toc-meta { font-size: 0.85em; color: #666; margin-top: 0.1em; }
.toc-meta a { color: #666; }`
	cssDataURI := "data:text/css;base64," + base64.StdEncoding.EncodeToString([]byte(css))
	cssPath, err := e.AddCSS(cssDataURI, "styles.css")
	if err != nil {
		// CSS is optional, continue without it
		fmt.Fprintf(logOut, "Warning: could not add CSS: %v\n", err)
		cssPath = ""
	}

	// Add front matter table of contents
	tocBody := buildTOCBody(articles)
	_, err = e.AddSection(tocBody, "Contents", "contents.xhtml", cssPath)
	if err != nil {
		fmt.Fprintf(logOut, "Warning: could not add table of contents: %v\n", err)
	}

	for i, a := range articles {
		body := extractBodyContent(a.HTML)
		chTitle := extractH1Title(body)
		if chTitle == "" {
			chTitle = fmt.Sprintf("Article %d", i+1)
		}

		// Sanitize HTML to XHTML for epub compatibility
		body = sanitizeForXHTML(body)

		// Extract and embed base64 images
		body, _ = extractImages(e, body, i+1)

		filename := fmt.Sprintf("article%03d.xhtml", i+1)
		_, err := e.AddSection(body, chTitle, filename, cssPath)
		if err != nil {
			fmt.Fprintf(logOut, "Warning: could not add section %q: %v\n", chTitle, err)
			continue
		}
	}

	if err := e.Write(outputPath); err != nil {
		return fmt.Errorf("writing epub: %w", err)
	}

	return nil
}
