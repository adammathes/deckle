// deckle: Fetch URLs and produce clean HTML, Markdown, or epub for e-readers.
//
//	deckle [options] <URL> [<URL>...]
//	deckle [options] -i urls.txt
//	cat urls.txt | deckle [options]
//	deckle [options] -format epub -o output.epub <URL> [<URL>...]
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/adammathes/deckle/pkg/builder"
)

func main() {
	maxWidth := flag.Int("max-width", 800, "Max pixel width (height scales proportionally)")
	quality := flag.Int("quality", 60, "JPEG quality 1-95")
	grayscale := flag.Bool("grayscale", false, "Convert to grayscale")
	output := flag.String("o", "", "Output file (default: stdout)")
	titleOverride := flag.String("title", "", "Override article/book title")
	timeout := flag.Duration("timeout", 30*time.Second, "HTTP fetch timeout")
	userAgent := flag.String("user-agent", "", "HTTP User-Agent header")
	outputFmt := flag.String("format", "markdown", "Output format: html, markdown, or epub")
	inputFile := flag.String("i", "", "Input file containing URLs (one per line, # comments ignored)")
	coverStyle := flag.String("cover", "typographic", "Cover style: 'typographic', 'collage', 'pattern', or 'none'")
	concurrency := flag.Int("concurrency", 5, "Max concurrent downloads for articles and images")
	maxRespSize := flag.Int64("max-response-size", 128*1024*1024, "Maximum allowed HTTP response size in bytes (0 for unlimited)")
	proxy := flag.String("proxy", "", "HTTP proxy URL (falls back to standard TLS, e.g. http://proxy.example.com:8080)")
	verbose := flag.Bool("v", false, "Verbose output (show progress on stderr)")

	// Deprecated flags for backward compatibility
	epubMode := flag.Bool("epub", false, "Deprecated: use -format epub")
	markdownMode := flag.Bool("markdown", false, "Deprecated: use -format markdown")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: deckle [options] <URL> [<URL>...]\n")
		fmt.Fprintf(os.Stderr, "       deckle [options] -i urls.txt\n")
		fmt.Fprintf(os.Stderr, "       cat urls.txt | deckle [options]\n")
		fmt.Fprintf(os.Stderr, "       deckle [options] -format epub -o out.epub <URL> [...]\n\n")
		fmt.Fprintf(os.Stderr, "Fetch URLs and produce clean HTML, Markdown, or epub for e-readers.\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	// Backward compat: -epub and -markdown flags override -format
	fmtVal := *outputFmt
	if *epubMode {
		fmtVal = "epub"
	} else if *markdownMode {
		fmtVal = "markdown"
	}

	conc := *concurrency
	if conc < 1 {
		conc = 1
	}

	// Check if stdin is a pipe
	var stdinReader io.Reader
	if info, err := os.Stdin.Stat(); err == nil && info.Mode()&os.ModeCharDevice == 0 {
		stdinReader = os.Stdin
	}

	opts := builder.Options{
		Title:           *titleOverride,
		Output:          *output,
		Format:          fmtVal,
		MaxWidth:        *maxWidth,
		Quality:         *quality,
		Grayscale:       *grayscale,
		Timeout:         *timeout,
		UserAgent:       *userAgent,
		CoverStyle:      *coverStyle,
		Concurrency:     conc,
		MaxResponseSize: *maxRespSize,
		ProxyURL:        *proxy,
		Verbose:         *verbose,
	}

	if err := builder.RunCLI(*inputFile, flag.Args(), stdinReader, opts); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
