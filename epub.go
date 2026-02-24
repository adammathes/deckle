// Epub generation from HTML articles using go-epub.
// Replaces pandoc for combining multiple articles into an epub3 with TOC.
package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	gohtml "html"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

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

// stripInvalidXMLChars removes characters not allowed in XML 1.0 content.
// Valid XML chars: #x9 | #xA | #xD | [#x20-#xD7FF] | [#xE000-#xFFFD] | [#x10000-#x10FFFF]
func stripInvalidXMLChars(s string) string {
	return strings.Map(func(r rune) rune {
		if r == 0x9 || r == 0xA || r == 0xD ||
			(r >= 0x20 && r <= 0xD7FF) ||
			(r >= 0xE000 && r <= 0xFFFD) ||
			(r >= 0x10000 && r <= 0x10FFFF) {
			return r
		}
		return -1 // strip
	}, s)
}

// sanitizeDimensionAttr cleans width/height attribute values to be valid
// EPUB integers (no decimals, no units).
func sanitizeDimensionAttr(val string) string {
	// Strip common CSS units
	val = strings.TrimSpace(val)
	for _, suffix := range []string{"px", "em", "rem", "%", "pt"} {
		val = strings.TrimSuffix(val, suffix)
	}
	// Try parsing as float and round to integer
	f, err := strconv.ParseFloat(val, 64)
	if err != nil || f < 0 {
		return ""
	}
	return strconv.Itoa(int(math.Round(f)))
}

// sanitizeID cleans an id attribute value to be valid in XHTML
// (must not contain whitespace, must not be empty).
func sanitizeID(val string) string {
	val = strings.TrimSpace(val)
	if val == "" {
		return ""
	}
	// Replace whitespace with hyphens
	var b strings.Builder
	for _, r := range val {
		if unicode.IsSpace(r) {
			b.WriteByte('-')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// isPhrasingElement returns true if the tag is a phrasing content element
// that cannot contain block-level elements in EPUB XHTML.
func isPhrasingElement(tag string) bool {
	switch tag {
	case "h1", "h2", "h3", "h4", "h5", "h6", "p",
		"span", "b", "strong", "i", "em", "a",
		"code", "samp", "kbd", "var", "sub", "sup",
		"small", "s", "u", "mark", "abbr", "dfn",
		"cite", "del", "ins", "bdi", "bdo", "time", "data":
		return true
	}
	return false
}

// isStructuralBlock returns true if the block element has significant
// internal structure that must be preserved (e.g., table, pre).
// These should be moved out of inline parents intact rather than unwrapped.
func isStructuralBlock(tag string) bool {
	switch tag {
	case "table", "pre", "ul", "ol", "dl", "blockquote", "figure":
		return true
	}
	return false
}

// isBlockElement returns true if the tag is a block-level element
// that cannot be nested inside phrasing content.
func isBlockElement(tag string) bool {
	switch tag {
	case "p", "div", "h1", "h2", "h3", "h4", "h5", "h6",
		"ul", "ol", "li", "dl", "dt", "dd",
		"blockquote", "section", "article", "aside",
		"header", "footer", "main", "figure", "figcaption", "nav",
		"table", "pre", "hr", "address":
		return true
	}
	return false
}

// elemAllowsDimensions returns true if the element may have width/height attributes.
func elemAllowsDimensions(tag string) bool {
	switch tag {
	case "img", "td", "th", "col", "colgroup", "table":
		return true
	}
	return false
}

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

// isAllowedAttr defines which attributes are safe for XHTML epub content.
// Returns true if the attribute should be kept.
func isAllowedAttr(a html.Attribute) bool {
	key := a.Key
	// Standard safe attributes for EPUB 3
	switch key {
	case "id", "class", "style", "title", "lang", "dir",
		"href", "src", "alt", "width", "height",
		"colspan", "rowspan", "scope", "headers",
		"cite", "datetime", "value", "type",
		"rel", "media", "start", "reversed":
		return true
	}
	// epub:type is allowed and encouraged for semantic inflection
	if key == "epub:type" {
		return true
	}
	return false
}

// isAllowedElement returns true if the tag is allowed in EPUB 3 XHTML.
func isAllowedElement(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return true
	}
	switch n.Data {
	case "div", "p", "h1", "h2", "h3", "h4", "h5", "h6", "ul", "ol", "li", "dl", "dt", "dd",
		"address", "hr", "pre", "blockquote", "cite", "em", "strong", "small", "s", "dfn",
		"abbr", "data", "time", "code", "var", "samp", "kbd", "sub", "sup", "i", "b", "u",
		"mark", "ruby", "rt", "rp", "bdi", "bdo", "span", "br", "wbr", "ins", "del", "img",
		"table", "caption", "colgroup", "col", "tbody", "thead", "tfoot", "tr", "td", "th",
		"section", "article", "aside", "header", "footer", "main", "figure", "figcaption", "nav",
		"a":
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
// removes broken fragment links, and eliminates disallowed tags/nesting.
func sanitizeForXHTML(htmlStr string) string {
	// Strip invalid XML characters (control chars like U+0012)
	htmlStr = stripInvalidXMLChars(htmlStr)

	doc, err := html.Parse(strings.NewReader(htmlStr))
	if err != nil {
		return htmlStr // fallback: return as-is
	}

	// Collect all IDs in the document (after sanitizing them)
	ids := map[string]bool{}
	var collectIDs func(*html.Node)
	collectIDs = func(n *html.Node) {
		if n.Type == html.ElementNode {
			for _, a := range n.Attr {
				if a.Key == "id" {
					cleaned := sanitizeID(a.Val)
					if cleaned != "" {
						ids[cleaned] = true
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			collectIDs(c)
		}
	}
	collectIDs(doc)

	// Track used IDs for deduplication
	usedIDs := map[string]bool{}

	// Walk and clean the tree
	var clean func(*html.Node) *html.Node
	clean = func(n *html.Node) *html.Node {
		if n.Type == html.ElementNode {
			// Special handling for media tags: convert to links
			if n.Data == "video" || n.Data == "audio" {
				src := ""
				for _, a := range n.Attr {
					if a.Key == "src" {
						src = a.Val
						break
					}
				}
				if src == "" {
					for c := n.FirstChild; c != nil; c = c.NextSibling {
						if c.Type == html.ElementNode && c.Data == "source" {
							for _, a := range c.Attr {
								if a.Key == "src" {
									src = a.Val
									break
								}
							}
						}
						if src != "" {
							break
						}
					}
				}
				if src != "" {
					link := &html.Node{
						Type: html.ElementNode,
						Data: "a",
						Attr: []html.Attribute{{Key: "href", Val: src}},
					}
					text := &html.Node{
						Type: html.TextNode,
						Data: "[Media: " + src + "]",
					}
					link.AppendChild(text)
					return link
				}
				return nil
			}

			// Remove <source> elements — they require srcset in EPUB XHTML
			// and should have been collapsed by processArticleImages already.
			if n.Data == "source" {
				return nil
			}

			// Remove <picture> elements — collapse to first <img> child if any
			if n.Data == "picture" {
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == html.ElementNode && c.Data == "img" {
						n.RemoveChild(c)
						return clean(c) // clean the extracted img (filter attrs, check external src)
					}
				}
				return nil
			}

			// Apply element whitelist
			if !isAllowedElement(n) {
				if n.Data != "html" && n.Data != "head" && n.Data != "body" {
					return nil
				}
			}

			// Special check for images: must have src, and src must not be
			// an external URL (remote resources are not allowed in EPUB).
			if n.Data == "img" {
				hasSrc := false
				for _, a := range n.Attr {
					if a.Key == "src" && strings.TrimSpace(a.Val) != "" {
						src := strings.TrimSpace(a.Val)
						// Remove images with external URLs (RSC-006)
						if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
							return nil
						}
						hasSrc = true
						break
					}
				}
				if !hasSrc {
					return nil
				}
			}

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
				// Sanitize and deduplicate IDs
				if a.Key == "id" {
					cleaned := sanitizeID(a.Val)
					if cleaned == "" {
						continue // drop empty IDs
					}
					if usedIDs[cleaned] {
						// Deduplicate: append suffix
						for i := 2; ; i++ {
							candidate := fmt.Sprintf("%s-%d", cleaned, i)
							if !usedIDs[candidate] {
								cleaned = candidate
								break
							}
						}
					}
					usedIDs[cleaned] = true
					a.Val = cleaned
				}
				// Sanitize width/height: must be integers, only on elements that allow them
				if a.Key == "width" || a.Key == "height" {
					if !elemAllowsDimensions(n.Data) {
						continue // strip dimension attrs on non-dimension elements
					}
					cleaned := sanitizeDimensionAttr(a.Val)
					if cleaned == "" || cleaned == "0" {
						continue // drop invalid dimensions
					}
					a.Val = cleaned
				}
				filtered = append(filtered, a)
			}
			n.Attr = filtered

			// Fix nesting: phrasing elements that cannot contain block elements.
			if isPhrasingElement(n.Data) {
				for c := n.FirstChild; c != nil; {
					next := c.NextSibling
					if c.Type == html.ElementNode && isBlockElement(c.Data) {
						if isStructuralBlock(c.Data) && n.Parent != nil {
							// Structural blocks (table, pre, etc.) must keep their
							// internal structure. Move them above all phrasing ancestors.
							n.RemoveChild(c)
							target := n
							for target.Parent != nil && target.Parent.Type == html.ElementNode && isPhrasingElement(target.Parent.Data) {
								target = target.Parent
							}
							if target.Parent != nil {
								target.Parent.InsertBefore(c, target)
							}
						} else {
							// Simple wrappers (p, div, etc.): unwrap children inline
							for cc := c.FirstChild; cc != nil; {
								cnext := cc.NextSibling
								c.RemoveChild(cc)
								n.InsertBefore(cc, c)
								cc = cnext
							}
							n.RemoveChild(c)
						}
					}
					c = next
				}
			}

			// Fix <dl> content model: must contain dt/dd pairs.
			// - <dd> before any <dt> needs a <dt> inserted before it
			// - <dt> at end without following <dd> needs a <dd> appended
			// - Bare text and disallowed children are wrapped appropriately
			if n.Data == "dl" {
				// First pass: wrap bare text and disallowed elements
				for c := n.FirstChild; c != nil; {
					next := c.NextSibling
					if c.Type == html.TextNode {
						if strings.TrimSpace(c.Data) != "" {
							dt := &html.Node{Type: html.ElementNode, Data: "dt", DataAtom: atom.Dt}
							n.InsertBefore(dt, c)
							n.RemoveChild(c)
							dt.AppendChild(c)
						}
					} else if c.Type == html.ElementNode {
						if c.Data != "dt" && c.Data != "dd" && c.Data != "div" {
							dd := &html.Node{Type: html.ElementNode, Data: "dd", DataAtom: atom.Dd}
							n.InsertBefore(dd, c)
							n.RemoveChild(c)
							dd.AppendChild(c)
						}
					}
					c = next
				}
				// Second pass: ensure dd comes after dt (not before)
				seenDt := false
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == html.ElementNode {
						if c.Data == "dt" {
							seenDt = true
						} else if c.Data == "dd" && !seenDt {
							// dd before any dt: insert empty dt before it
							dt := &html.Node{Type: html.ElementNode, Data: "dt", DataAtom: atom.Dt}
							n.InsertBefore(dt, c)
							seenDt = true
						}
					}
				}
				// Third pass: ensure last dt has a following dd
				var lastDt *html.Node
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == html.ElementNode {
						if c.Data == "dt" {
							lastDt = c
						} else if c.Data == "dd" || c.Data == "div" {
							lastDt = nil
						}
					}
				}
				if lastDt != nil {
					dd := &html.Node{Type: html.ElementNode, Data: "dd", DataAtom: atom.Dd}
					if lastDt.NextSibling != nil {
						n.InsertBefore(dd, lastDt.NextSibling)
					} else {
						n.AppendChild(dd)
					}
				}
				// Ensure <dl> has at least one dt/dd pair
				hasDt := false
				hasDd := false
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == html.ElementNode {
						if c.Data == "dt" {
							hasDt = true
						}
						if c.Data == "dd" || c.Data == "div" {
							hasDd = true
						}
					}
				}
				if !hasDt {
					dt := &html.Node{Type: html.ElementNode, Data: "dt", DataAtom: atom.Dt}
					if n.FirstChild != nil {
						n.InsertBefore(dt, n.FirstChild)
					} else {
						n.AppendChild(dt)
					}
				}
				if !hasDd {
					dd := &html.Node{Type: html.ElementNode, Data: "dd", DataAtom: atom.Dd}
					n.AppendChild(dd)
				}
			}

			// Fix <figcaption> outside <figure>: convert to <p>
			if n.Data == "figcaption" {
				if n.Parent == nil || n.Parent.Data != "figure" {
					n.Data = "p"
					n.DataAtom = atom.P
				}
			}
		}

		for c := n.FirstChild; c != nil; {
			next := c.NextSibling
			if result := clean(c); result == nil {
				n.RemoveChild(c)
			} else if result != c {
				n.InsertBefore(result, c)
				n.RemoveChild(c)
			}
			c = next
		}
		return n
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
func buildEpub(articles []epubArticle, title string, outputPath string, coverStyle string) error {
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

	// Generate and set cover image
	if coverStyle != "none" {
		coverPNG, err := generateCover(title, articles, coverStyle)
		if err != nil {
			fmt.Fprintf(logOut, "Warning: could not generate cover: %v\n", err)
		} else {
			coverURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString(coverPNG)
			imgPath, err := e.AddImage(coverURI, "cover.png")
			if err != nil {
				fmt.Fprintf(logOut, "Warning: could not add cover image: %v\n", err)
			} else if err := e.SetCover(imgPath, ""); err != nil {
				fmt.Fprintf(logOut, "Warning: could not set cover: %v\n", err)
			}
		}
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
