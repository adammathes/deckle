// Epub generation from HTML articles using go-epub.
// Replaces pandoc for combining multiple articles into an epub3 with TOC.
package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"regexp"
	"strings"

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

// article holds a single processed article ready for epub inclusion.
type article struct {
	title string
	body  string // HTML body content (between <body> tags)
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
				fmt.Fprintf(os.Stderr, "Warning: invalid base64 for %s: %v\n", filename, err)
				return match
			}
		}

		// go-epub accepts data URIs directly via AddImage
		dataURI := "data:" + mime + ";base64," + b64data
		internalPath, err := e.AddImage(dataURI, filename)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to add image %s: %v\n", filename, err)
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

// buildEpub creates an epub3 file from a list of HTML article contents.
// Each article should be complete HTML with <body> containing the article
// and an <h1> title element.
func buildEpub(articles []string, title string, outputPath string) error {
	e, err := epub.NewEpub(title)
	if err != nil {
		return fmt.Errorf("creating epub: %w", err)
	}
	e.SetLang("en")

	// Add minimal CSS for readability on e-readers
	css := `body { margin: 1em; line-height: 1.5; }
img { max-width: 100%; height: auto; }
pre, code { font-size: 0.85em; }
blockquote { margin-left: 1em; padding-left: 0.5em; border-left: 2px solid #999; }`
	cssDataURI := "data:text/css;base64," + base64.StdEncoding.EncodeToString([]byte(css))
	cssPath, err := e.AddCSS(cssDataURI, "styles.css")
	if err != nil {
		// CSS is optional, continue without it
		fmt.Fprintf(os.Stderr, "Warning: could not add CSS: %v\n", err)
		cssPath = ""
	}

	for i, articleHTML := range articles {
		body := extractBodyContent(articleHTML)
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
			fmt.Fprintf(os.Stderr, "Warning: could not add section %q: %v\n", chTitle, err)
			continue
		}
	}

	if err := e.Write(outputPath); err != nil {
		return fmt.Errorf("writing epub: %w", err)
	}

	return nil
}
