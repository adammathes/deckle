package main

import (
	"encoding/xml"
	"strings"
	"testing"
)

// FuzzSanitizeForXHTML feeds random and mutated HTML strings to sanitizeForXHTML
// and verifies that the output is valid XML with no disallowed elements or
// invalid XML characters.
func FuzzSanitizeForXHTML(f *testing.F) {
	// Seed corpus with representative HTML patterns
	seeds := []string{
		`<p>Hello World</p>`,
		`<div><script>alert(1)</script><p>text</p></div>`,
		`<img src="data:image/png;base64,abc" alt="test"/>`,
		`<img src="https://example.com/img.jpg" alt="ext"/>`,
		`<img src="data:image/avif;base64,abc" alt="avif"/>`,
		`<picture><source media="(max-width: 480px)"/><img src="x.jpg" alt="pic"/></picture>`,
		`<video src="movie.mp4"></video>`,
		`<audio><source src="audio.mp3"/></audio>`,
		`<p id="test" onclick="alert(1)" data-track="click">text</p>`,
		`<a href="#exists">link</a><div id="exists">target</div>`,
		`<a href="#missing">broken</a>`,
		`<div width="100" height="200"><img src="x.jpg" alt="t" width="1.5" height="916.7"/></div>`,
		`<h1><p>Title</p></h1>`,
		`<span>start <div>middle</div> end</span>`,
		`<p>Before<table><tr><td>cell</td></tr></table>After</p>`,
		`<dl>bare text<dt>term</dt><dd>definition</dd></dl>`,
		`<dl><dt>orphan term</dt></dl>`,
		`<dl><dd>orphan def</dd></dl>`,
		`<div><figcaption>Caption text</figcaption></div>`,
		`<figure><img src="x.jpg" alt="t"/><figcaption>Valid caption</figcaption></figure>`,
		`<p>Hello` + "\x12" + `World</p>`,
		`<p>` + "\x00\x01\x08\x0B\x0C\x0E\x1F" + ` text</p>`,
		`<div id="intro">First</div><div id="intro">Second</div>`,
		`<section aria-label="chapter" class="main" epub:type="chapter">content</section>`,
		`<svg xmlns="http://www.w3.org/2000/svg"><circle r="10"/></svg>`,
		``,
		`<p></p>`,
		`<></>`,
		`<div><div><div><div><div>deep</div></div></div></div></div>`,
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		result := sanitizeForXHTML(input)

		// The output must be valid XML when wrapped in a root element.
		wrapped := "<root>" + result + "</root>"
		decoder := xml.NewDecoder(strings.NewReader(wrapped))
		decoder.Strict = false
		for {
			_, err := decoder.Token()
			if err != nil {
				if err.Error() == "EOF" {
					break
				}
				t.Fatalf("output is not valid XML:\ninput:  %q\noutput: %q\nerror:  %v", input, result, err)
			}
		}

		// No disallowed elements should survive.
		disallowed := []string{
			"<script", "<style", "<object", "<embed", "<form",
			"<input", "<select", "<textarea", "<button",
			"<video", "<audio", "<source", "<picture", "<svg",
			"<iframe", "<canvas", "<noscript",
		}
		lower := strings.ToLower(result)
		for _, tag := range disallowed {
			if strings.Contains(lower, tag) {
				t.Errorf("disallowed element %q found in output:\ninput:  %q\noutput: %q", tag, input, result)
			}
		}

		// No invalid XML characters should remain.
		for _, r := range result {
			if r == 0x9 || r == 0xA || r == 0xD {
				continue // valid
			}
			if r >= 0x20 && r <= 0xD7FF {
				continue
			}
			if r >= 0xE000 && r <= 0xFFFD {
				continue
			}
			if r >= 0x10000 && r <= 0x10FFFF {
				continue
			}
			t.Errorf("invalid XML character U+%04X in output:\ninput:  %q\noutput: %q", r, input, result)
		}
	})
}
