// url2html: Fetch a URL and produce clean, optimized HTML for epub conversion.
// Replaces monolith + html-img-optimize + go-readability + html-normalize-headings.
//
// Usage: url2html [options] <URL>
package main

import (
	"flag"
	"fmt"
	"os"
	"time"
)

func main() {
	maxWidth := flag.Int("max-width", 800, "Max pixel width (height scales proportionally)")
	quality := flag.Int("quality", 60, "JPEG quality 1-95")
	grayscale := flag.Bool("grayscale", false, "Convert to grayscale")
	output := flag.String("o", "", "Output file (default: stdout)")
	titleOverride := flag.String("title", "", "Override article title")
	timeout := flag.Duration("timeout", 30*time.Second, "HTTP fetch timeout")
	userAgent := flag.String("user-agent", defaultUA, "HTTP User-Agent header")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: url2html [options] <URL>\n\n")
		fmt.Fprintf(os.Stderr, "Fetch a URL and produce clean, optimized HTML for epub conversion.\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(1)
	}
	rawURL := flag.Arg(0)

	// 1. Fetch the page
	htmlBytes, pageURL, err := fetchHTML(rawURL, *timeout, *userAgent)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// 2. Promote lazy-loaded images (data-src → src) before readability
	htmlBytes = promoteLazySrc(htmlBytes)

	// 3. Extract article content (strips nav, footer, ads)
	content, title, err := extractArticle(htmlBytes, pageURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "Title: %s\n", title)

	// 4. Process article images (fetch external, collapse <picture>, optimize)
	opts := optimizeOpts{
		maxWidth:  *maxWidth,
		quality:   *quality,
		grayscale: *grayscale,
	}
	result := processArticleImages([]byte(content), opts)

	// 5. Normalize headings (shift down, inject H1 with title)
	finalTitle := title
	if *titleOverride != "" {
		finalTitle = *titleOverride
	}
	final := normalizeHeadings(string(result), finalTitle)

	// 6. Write output
	if *output != "" {
		if err := os.WriteFile(*output, []byte(final), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing output: %v\n", err)
			os.Exit(1)
		}
	} else {
		os.Stdout.WriteString(final)
	}
}
