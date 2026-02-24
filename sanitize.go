// HTML to XHTML sanitization for EPUB 3 compliance.
// Converts arbitrary HTML from the web into valid XHTML suitable for
// embedding in EPUB 3 documents.
package main

import (
	"bytes"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
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

// xhtmlSanitizer holds state for a single HTML→XHTML sanitization pass.
type xhtmlSanitizer struct {
	ids     map[string]bool // all IDs present in the document
	usedIDs map[string]bool // IDs already emitted (for deduplication)
}

// transformElement handles element-level transformations that may replace or
// remove the node entirely: media→link conversion, source/picture removal,
// element whitelist, and image validation.
// Returns nil to remove, a different node to replace, or n to continue processing.
func (s *xhtmlSanitizer) transformElement(n *html.Node) *html.Node {
	// Convert media tags to links
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

	// Remove <source> elements
	if n.Data == "source" {
		return nil
	}

	// Collapse <picture> to first <img> child
	if n.Data == "picture" {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && c.Data == "img" {
				n.RemoveChild(c)
				return s.clean(c)
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

	// Validate images: must have src, no external URLs, no AVIF
	if n.Data == "img" {
		hasSrc := false
		for _, a := range n.Attr {
			if a.Key == "src" && strings.TrimSpace(a.Val) != "" {
				src := strings.TrimSpace(a.Val)
				if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
					return nil
				}
				if strings.HasPrefix(src, "data:image/avif") {
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

	return n // continue processing
}

// filterAttributes applies the attribute whitelist and sanitizes individual
// attribute values (fragment links, IDs, dimensions).
func (s *xhtmlSanitizer) filterAttributes(n *html.Node) {
	var filtered []html.Attribute
	for _, a := range n.Attr {
		if !isAllowedAttr(a) {
			continue
		}
		// Fix broken fragment links
		if a.Key == "href" && strings.HasPrefix(a.Val, "#") {
			frag := a.Val[1:]
			if frag != "" && !s.ids[frag] {
				continue
			}
		}
		// Sanitize and deduplicate IDs
		if a.Key == "id" {
			cleaned := sanitizeID(a.Val)
			if cleaned == "" {
				continue
			}
			if s.usedIDs[cleaned] {
				for i := 2; ; i++ {
					candidate := fmt.Sprintf("%s-%d", cleaned, i)
					if !s.usedIDs[candidate] {
						cleaned = candidate
						break
					}
				}
			}
			s.usedIDs[cleaned] = true
			a.Val = cleaned
		}
		// Sanitize width/height
		if a.Key == "width" || a.Key == "height" {
			if !elemAllowsDimensions(n.Data) {
				continue
			}
			cleaned := sanitizeDimensionAttr(a.Val)
			if cleaned == "" || cleaned == "0" {
				continue
			}
			a.Val = cleaned
		}
		filtered = append(filtered, a)
	}
	n.Attr = filtered
}

// fixNesting repairs invalid nesting where block elements appear inside
// phrasing (inline) elements. Structural blocks are moved above all phrasing
// ancestors; simple wrappers are unwrapped inline.
func (s *xhtmlSanitizer) fixNesting(n *html.Node) {
	if !isPhrasingElement(n.Data) {
		return
	}
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		if c.Type == html.ElementNode && isBlockElement(c.Data) {
			if isStructuralBlock(c.Data) && n.Parent != nil {
				n.RemoveChild(c)
				// Clean the moved node (filter attrs, recurse children)
				// before inserting — it bypasses the normal tree walk.
				s.clean(c)
				target := n
				for target.Parent != nil && target.Parent.Type == html.ElementNode && isPhrasingElement(target.Parent.Data) {
					target = target.Parent
				}
				if target.Parent != nil {
					target.Parent.InsertBefore(c, target)
				}
			} else {
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

// fixDLContent repairs the content model of <dl> elements to ensure they
// contain valid dt/dd pairs as required by EPUB XHTML.
func (s *xhtmlSanitizer) fixDLContent(n *html.Node) {
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

// fixFigcaption converts <figcaption> elements that appear outside a <figure>
// into <p> elements.
func (s *xhtmlSanitizer) fixFigcaption(n *html.Node) {
	if n.Data == "figcaption" {
		if n.Parent == nil || n.Parent.Data != "figure" {
			n.Data = "p"
			n.DataAtom = atom.P
		}
	}
}

// clean recursively processes a node and its children, applying all
// sanitization rules. Returns nil to remove the node, a different node
// to replace it, or n to keep it.
func (s *xhtmlSanitizer) clean(n *html.Node) *html.Node {
	if n.Type == html.ElementNode {
		result := s.transformElement(n)
		if result == nil {
			return nil
		}
		if result != n {
			return result
		}

		s.filterAttributes(n)
		s.fixNesting(n)
		if n.Data == "dl" {
			s.fixDLContent(n)
		}
		s.fixFigcaption(n)
	}

	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		if result := s.clean(c); result == nil {
			n.RemoveChild(c)
		} else if result != c {
			n.InsertBefore(result, c)
			n.RemoveChild(c)
		}
		c = next
	}
	return n
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

	s := &xhtmlSanitizer{
		ids:     collectIDs(doc),
		usedIDs: map[string]bool{},
	}
	s.clean(doc)

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

// collectIDs collects all sanitized ID values from the document tree.
func collectIDs(doc *html.Node) map[string]bool {
	ids := map[string]bool{}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
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
			walk(c)
		}
	}
	walk(doc)
	return ids
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
