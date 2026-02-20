// url2html: Fetch URLs and produce clean HTML or epub for e-readers.
//
// Single article mode:
//   url2html [options] <URL>
//
// Epub mode (multiple articles):
//   url2html [options] -epub -o output.epub <URL|file> [<URL|file>...]
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

// processURL fetches a URL and runs the full article pipeline.
// Returns the final HTML string and the article title.
func processURL(rawURL string, opts optimizeOpts, timeout time.Duration, userAgent string, titleOverride string) (string, string, error) {
	htmlBytes, pageURL, err := fetchHTML(rawURL, timeout, userAgent)
	if err != nil {
		return "", "", err
	}

	htmlBytes = promoteLazySrc(htmlBytes)

	content, meta, err := extractArticle(htmlBytes, pageURL)
	if err != nil {
		return "", "", err
	}
	fmt.Fprintf(os.Stderr, "Title: %s\n", meta.Title)

	result := processArticleImages([]byte(content), opts)

	finalTitle := meta.Title
	if titleOverride != "" {
		finalTitle = titleOverride
	}

	src := sourceInfo{
		URL:      rawURL,
		Byline:   meta.Byline,
		SiteName: meta.SiteName,
	}
	final := normalizeHeadings(string(result), finalTitle, src)

	return final, finalTitle, nil
}

// readURLFile reads a file containing one URL per line, skipping blanks and comments.
func readURLFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var urls []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		urls = append(urls, line)
	}
	return urls, scanner.Err()
}

func main() {
	maxWidth := flag.Int("max-width", 800, "Max pixel width (height scales proportionally)")
	quality := flag.Int("quality", 60, "JPEG quality 1-95")
	grayscale := flag.Bool("grayscale", false, "Convert to grayscale")
	output := flag.String("o", "", "Output file (default: stdout)")
	titleOverride := flag.String("title", "", "Override article/book title")
	timeout := flag.Duration("timeout", 30*time.Second, "HTTP fetch timeout")
	userAgent := flag.String("user-agent", defaultUA, "HTTP User-Agent header")
	epubMode := flag.Bool("epub", false, "Generate epub (requires -o, accepts multiple URLs or a .txt file)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: url2html [options] <URL>\n")
		fmt.Fprintf(os.Stderr, "       url2html [options] -epub -o out.epub <URL|file.txt> [...]\n\n")
		fmt.Fprintf(os.Stderr, "Fetch URLs and produce clean HTML or epub for e-readers.\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	opts := optimizeOpts{
		maxWidth:  *maxWidth,
		quality:   *quality,
		grayscale: *grayscale,
	}

	if *epubMode {
		if *output == "" {
			fmt.Fprintln(os.Stderr, "Error: -epub requires -o output.epub")
			os.Exit(1)
		}
		if flag.NArg() < 1 {
			flag.Usage()
			os.Exit(1)
		}

		// Collect URLs from args (URLs or .txt files)
		var urls []string
		for _, arg := range flag.Args() {
			if strings.HasSuffix(arg, ".txt") {
				fileURLs, err := readURLFile(arg)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error reading %s: %v\n", arg, err)
					os.Exit(1)
				}
				urls = append(urls, fileURLs...)
			} else {
				urls = append(urls, arg)
			}
		}

		if len(urls) == 0 {
			fmt.Fprintln(os.Stderr, "Error: no URLs provided")
			os.Exit(1)
		}

		// Process each URL
		var articles []string
		for i, rawURL := range urls {
			fmt.Fprintf(os.Stderr, "[%d/%d] %s\n", i+1, len(urls), rawURL)
			html, _, err := processURL(rawURL, opts, *timeout, *userAgent, "")
			if err != nil {
				fmt.Fprintf(os.Stderr, "  Error: %v (skipping)\n", err)
				continue
			}
			articles = append(articles, html)
		}

		if len(articles) == 0 {
			fmt.Fprintln(os.Stderr, "Error: no articles converted")
			os.Exit(1)
		}

		// Build epub
		bookTitle := *titleOverride
		if bookTitle == "" {
			// Derive from output filename
			bookTitle = strings.TrimSuffix(*output, ".epub")
			if idx := strings.LastIndex(bookTitle, "/"); idx >= 0 {
				bookTitle = bookTitle[idx+1:]
			}
		}

		fmt.Fprintf(os.Stderr, "Building epub from %d articles...\n", len(articles))
		if err := buildEpub(articles, bookTitle, *output); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "✓ %s (%d articles)\n", *output, len(articles))

	} else {
		// Single URL mode
		if flag.NArg() != 1 {
			flag.Usage()
			os.Exit(1)
		}

		final, _, err := processURL(flag.Arg(0), opts, *timeout, *userAgent, *titleOverride)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if *output != "" {
			if err := os.WriteFile(*output, []byte(final), 0644); err != nil {
				fmt.Fprintf(os.Stderr, "Error writing output: %v\n", err)
				os.Exit(1)
			}
		} else {
			os.Stdout.WriteString(final)
		}
	}
}
