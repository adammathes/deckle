// deckle: Fetch URLs and produce clean HTML or epub for e-readers.
//
// Single article mode:
//
//	deckle [options] <URL>
//
// Epub mode (multiple articles):
//
//	deckle [options] -epub -o output.epub <URL|file> [<URL|file>...]
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// logOut is the writer for informational/progress output.
// In silent mode it is set to io.Discard so only errors reach the user.
var logOut io.Writer = os.Stderr

// processURL fetches a URL and runs the full article pipeline.
// Returns the final HTML string, article title, source info, and any error.
func processURL(rawURL string, opts optimizeOpts, timeout time.Duration, userAgent string, titleOverride string) (string, string, sourceInfo, error) {
	htmlBytes, pageURL, err := fetchHTML(rawURL, timeout, userAgent)
	if err != nil {
		return "", "", sourceInfo{}, err
	}

	htmlBytes = promoteLazySrc(htmlBytes)

	content, meta, err := extractArticle(htmlBytes, pageURL)
	if err != nil {
		return "", "", sourceInfo{}, err
	}
	fmt.Fprintf(logOut, "Title: %s\n", meta.Title)

	result := processArticleImages([]byte(content), opts)

	finalTitle := meta.Title
	if titleOverride != "" {
		finalTitle = titleOverride
	}

	src := sourceInfo{
		URL:           rawURL,
		Byline:        meta.Byline,
		SiteName:      meta.SiteName,
		PublishedTime: meta.PublishedTime,
	}
	final := normalizeHeadings(string(result), finalTitle, src)

	return final, finalTitle, src, nil
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

// cliConfig holds parsed command-line options.
type cliConfig struct {
	opts          optimizeOpts
	output        string
	titleOverride string
	timeout       time.Duration
	userAgent     string
	epubMode      bool
	args          []string
}

// run executes the main application logic, returning any error.
func run(cfg cliConfig) error {
	if cfg.epubMode {
		if cfg.output == "" {
			return fmt.Errorf("-epub requires -o output.epub")
		}
		if len(cfg.args) < 1 {
			return fmt.Errorf("epub mode requires at least one URL or file argument")
		}

		// Collect URLs from args (URLs or .txt files)
		var urls []string
		var txtFilename string // basename of first .txt file (for title derivation)
		for _, arg := range cfg.args {
			if strings.HasSuffix(arg, ".txt") {
				fileURLs, err := readURLFile(arg)
				if err != nil {
					return fmt.Errorf("reading %s: %w", arg, err)
				}
				urls = append(urls, fileURLs...)
				if txtFilename == "" {
					name := arg
					if idx := strings.LastIndex(name, "/"); idx >= 0 {
						name = name[idx+1:]
					}
					txtFilename = strings.TrimSuffix(name, ".txt")
				}
			} else {
				urls = append(urls, arg)
			}
		}

		if len(urls) == 0 {
			return fmt.Errorf("no URLs provided")
		}

		// Process each URL
		// Parallelize with a bounded semaphore to avoid overwhelming resources
		type result struct {
			html  string
			title string
			src   sourceInfo
			ok    bool
		}
		results := make([]result, len(urls))
		var wg sync.WaitGroup
		sem := make(chan struct{}, 5) // Limit to 5 concurrent jobs

		for i, rawURL := range urls {
			wg.Add(1)
			go func(i int, rawURL string) {
				defer wg.Done()
				sem <- struct{}{}        // Acquire
				defer func() { <-sem }() // Release

				fmt.Fprintf(logOut, "[%d/%d] %s\n", i+1, len(urls), rawURL)
				h, t, src, err := processURL(rawURL, cfg.opts, cfg.timeout, cfg.userAgent, "")
				if err != nil {
					fmt.Fprintf(logOut, "  Error: %v (skipping)\n", err)
					return
				}
				results[i] = result{html: h, title: t, src: src, ok: true}
			}(i, rawURL)
		}
		wg.Wait()

		// Filter successful articles preserving order
		var articles []epubArticle
		for _, r := range results {
			if r.ok {
				articles = append(articles, epubArticle{
					HTML:          r.html,
					Title:         r.title,
					URL:           r.src.URL,
					Byline:        r.src.Byline,
					SiteName:      r.src.SiteName,
					PublishedTime: r.src.PublishedTime,
				})
			}
		}

		if len(articles) == 0 {
			return fmt.Errorf("no articles converted")
		}

		// Derive book title: -title flag > .txt filename > first article title > output filename
		bookTitle := cfg.titleOverride
		if bookTitle == "" && txtFilename != "" {
			bookTitle = txtFilename
		}
		if bookTitle == "" {
			if len(articles) > 1 {
				bookTitle = articles[0].Title + " & more"
			} else {
				bookTitle = articles[0].Title
			}
		}
		if bookTitle == "" {
			// Final fallback: output filename
			bookTitle = strings.TrimSuffix(cfg.output, ".epub")
			if idx := strings.LastIndex(bookTitle, "/"); idx >= 0 {
				bookTitle = bookTitle[idx+1:]
			}
		}

		fmt.Fprintf(logOut, "Building epub from %d articles...\n", len(articles))
		if err := buildEpub(articles, bookTitle, cfg.output); err != nil {
			return fmt.Errorf("building epub: %w", err)
		}
		fmt.Fprintf(logOut, "✓ %s (%d articles)\n", cfg.output, len(articles))
		return nil
	}

	// Single URL mode
	if len(cfg.args) != 1 {
		return fmt.Errorf("single URL mode requires exactly one URL argument")
	}

	final, _, _, err := processURL(cfg.args[0], cfg.opts, cfg.timeout, cfg.userAgent, cfg.titleOverride)
	if err != nil {
		return err
	}

	if cfg.output != "" {
		if err := os.WriteFile(cfg.output, []byte(final), 0644); err != nil {
			return fmt.Errorf("writing output: %w", err)
		}
	} else {
		os.Stdout.WriteString(final)
	}
	return nil
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
	silent := flag.Bool("silent", false, "Suppress all output except errors (for pipeline use)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: deckle [options] <URL>\n")
		fmt.Fprintf(os.Stderr, "       deckle [options] -epub -o out.epub <URL|file.txt> [...]\n\n")
		fmt.Fprintf(os.Stderr, "Fetch URLs and produce clean HTML or epub for e-readers.\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *silent {
		logOut = io.Discard
	}

	cfg := cliConfig{
		opts: optimizeOpts{
			maxWidth:  *maxWidth,
			quality:   *quality,
			grayscale: *grayscale,
		},
		output:        *output,
		titleOverride: *titleOverride,
		timeout:       *timeout,
		userAgent:     *userAgent,
		epubMode:      *epubMode,
		args:          flag.Args(),
	}

	if err := run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
