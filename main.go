// deckle: Fetch URLs and produce clean HTML, Markdown, or epub for e-readers.
//
// Single article mode:
//
//	deckle [options] <URL>
//
// Markdown mode (one or more articles):
//
//	deckle [options] -markdown <URL|file.txt> [<URL|file.txt>...]
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
// concurrency controls how many images are fetched in parallel.
func processURL(rawURL string, opts optimizeOpts, timeout time.Duration, userAgent string, titleOverride string, concurrency int) (string, string, sourceInfo, error) {
	if concurrency < 1 {
		concurrency = 1
	}

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

	result := processArticleImages([]byte(content), opts, concurrency)

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

// collectURLs expands args (direct URLs or .txt files) into a flat URL list.
// Also returns the basename of the first .txt file, for title derivation.
func collectURLs(args []string) (urls []string, txtFilename string, err error) {
	for _, arg := range args {
		if strings.HasSuffix(arg, ".txt") {
			fileURLs, ferr := readURLFile(arg)
			if ferr != nil {
				return nil, "", fmt.Errorf("reading %s: %w", arg, ferr)
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
	return urls, txtFilename, nil
}

// fetchMultipleArticles fetches a list of URLs in parallel and returns the
// successfully processed articles in input order, skipping failures.
func fetchMultipleArticles(urls []string, cfg cliConfig) []epubArticle {
	type result struct {
		html  string
		title string
		src   sourceInfo
		ok    bool
	}
	results := make([]result, len(urls))
	var wg sync.WaitGroup
	sem := make(chan struct{}, cfg.concurrency)

	pprintf("📥 Fetching %d articles...\n", len(urls))

	for i, rawURL := range urls {
		wg.Add(1)
		go func(i int, rawURL string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			fmt.Fprintf(logOut, "[%d/%d] %s\n", i+1, len(urls), rawURL)
			h, t, src, err := processURL(rawURL, cfg.opts, cfg.timeout, cfg.userAgent, "", cfg.concurrency)
			if err != nil {
				fmt.Fprintf(logOut, "  Error: %v (skipping)\n", err)
				pprintf("  ❌ [%d/%d] %s — %v\n", i+1, len(urls), shortURL(rawURL), err)
				return
			}
			results[i] = result{html: h, title: t, src: src, ok: true}
			pprintf("  ✅ [%d/%d] %s\n", i+1, len(urls), shortURL(rawURL))
		}(i, rawURL)
	}
	wg.Wait()

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
	return articles
}

// writeOutput writes content to a file, or stdout if path is empty.
func writeOutput(path, content string) error {
	if path != "" {
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return fmt.Errorf("writing output: %w", err)
		}
		return nil
	}
	if _, err := os.Stdout.WriteString(content); err != nil {
		return fmt.Errorf("writing to stdout: %w", err)
	}
	return nil
}

// cliConfig holds parsed command-line options.
type cliConfig struct {
	opts          optimizeOpts
	output        string
	titleOverride string
	timeout       time.Duration
	userAgent     string
	epubMode      bool
	markdownMode  bool
	coverStyle    string
	concurrency   int
	args          []string
}

// run executes the main application logic, returning any error.
// Progress output goes to the progressOut writer, which should be set
// by the caller before invoking run (main() sets it to os.Stdout when
// -o is specified and --silent is not active).
func run(cfg cliConfig) error {
	if cfg.markdownMode && cfg.epubMode {
		return fmt.Errorf("-markdown and -epub are mutually exclusive")
	}
	if cfg.concurrency < 1 {
		cfg.concurrency = 5
	}

	if cfg.epubMode {
		if cfg.output == "" {
			return fmt.Errorf("-epub requires -o output.epub")
		}
		if len(cfg.args) < 1 {
			return fmt.Errorf("epub mode requires at least one URL or file argument")
		}

		urls, txtFilename, err := collectURLs(cfg.args)
		if err != nil {
			return err
		}
		if len(urls) == 0 {
			return fmt.Errorf("no URLs provided")
		}

		articles := fetchMultipleArticles(urls, cfg)
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
			bookTitle = strings.TrimSuffix(cfg.output, ".epub")
			if idx := strings.LastIndex(bookTitle, "/"); idx >= 0 {
				bookTitle = bookTitle[idx+1:]
			}
		}

		pprintf("📦 Building epub from %d articles...\n", len(articles))
		fmt.Fprintf(logOut, "Building epub from %d articles...\n", len(articles))
		if err := buildEpub(articles, bookTitle, cfg.output, cfg.coverStyle); err != nil {
			return fmt.Errorf("building epub: %w", err)
		}
		pprintf("✅ %s (%d articles)\n", cfg.output, len(articles))
		fmt.Fprintf(logOut, "✓ %s (%d articles)\n", cfg.output, len(articles))
		return nil
	}

	if cfg.markdownMode {
		if len(cfg.args) < 1 {
			return fmt.Errorf("markdown mode requires at least one URL or file argument")
		}

		urls, _, err := collectURLs(cfg.args)
		if err != nil {
			return err
		}
		if len(urls) == 0 {
			return fmt.Errorf("no URLs provided")
		}

		// Markdown output uses original image URLs, not embedded data URIs,
		// so there is no point downloading images.
		mdOpts := cfg.opts
		mdOpts.skipImageFetch = true

		if len(urls) == 1 {
			pprintf("📥 Fetching article...\n")
			final, _, _, err := processURL(urls[0], mdOpts, cfg.timeout, cfg.userAgent, cfg.titleOverride, cfg.concurrency)
			if err != nil {
				pprintf("  ❌ %s — %v\n", shortURL(urls[0]), err)
				return err
			}
			pprintf("  ✅ %s\n", shortURL(urls[0]))
			md, err := convertArticleToMarkdown(final)
			if err != nil {
				return err
			}
			if err := writeOutput(cfg.output, md+"\n"); err != nil {
				return err
			}
			if cfg.output != "" {
				pprintf("✅ Wrote %s\n", cfg.output)
			}
			return nil
		}

		// Multiple URLs: fetch in parallel, concatenate with separators.
		mdCfg := cfg
		mdCfg.opts = mdOpts
		articles := fetchMultipleArticles(urls, mdCfg)
		if len(articles) == 0 {
			return fmt.Errorf("no articles converted")
		}
		md, err := articlesToMarkdown(articles)
		if err != nil {
			return err
		}
		if err := writeOutput(cfg.output, md+"\n"); err != nil {
			return err
		}
		if cfg.output != "" {
			pprintf("✅ Wrote %s (%d articles)\n", cfg.output, len(articles))
		}
		return nil
	}

	// Single URL HTML mode
	if len(cfg.args) != 1 {
		return fmt.Errorf("single URL mode requires exactly one URL argument")
	}

	pprintf("📥 Fetching article...\n")
	final, _, _, err := processURL(cfg.args[0], cfg.opts, cfg.timeout, cfg.userAgent, cfg.titleOverride, cfg.concurrency)
	if err != nil {
		pprintf("  ❌ %s — %v\n", shortURL(cfg.args[0]), err)
		return err
	}
	pprintf("  ✅ %s\n", shortURL(cfg.args[0]))
	if err := writeOutput(cfg.output, final); err != nil {
		return err
	}
	if cfg.output != "" {
		pprintf("✅ Wrote %s\n", cfg.output)
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
	markdownMode := flag.Bool("markdown", false, "Convert to Markdown (data URI images become alt-text placeholders)")
	coverStyle := flag.String("cover", "collage", "Cover style: 'collage', 'pattern', or 'none'")
	concurrency := flag.Int("concurrency", 5, "Max concurrent downloads for articles and images")
	maxRespSize := flag.Int64("max-response-size", 128*1024*1024, "Maximum allowed HTTP response size in bytes (0 for unlimited)")
	proxy := flag.String("proxy", "", "HTTP proxy URL (falls back to standard TLS, e.g. http://proxy.example.com:8080)")
	silent := flag.Bool("silent", false, "Suppress all output except errors (for pipeline use)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: deckle [options] <URL>\n")
		fmt.Fprintf(os.Stderr, "       deckle [options] -markdown <URL|file.txt> [...]\n")
		fmt.Fprintf(os.Stderr, "       deckle [options] -epub -o out.epub <URL|file.txt> [...]\n\n")
		fmt.Fprintf(os.Stderr, "Fetch URLs and produce clean HTML, Markdown, or epub for e-readers.\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *silent {
		logOut = io.Discard
	}

	// Enable progress on stdout when output goes to a file and we're not silent.
	if *output != "" && logOut != io.Discard {
		progressOut = os.Stdout
	}

	maxResponseBytes = *maxRespSize
	fetchProxyURL = *proxy

	conc := *concurrency
	if conc < 1 {
		conc = 1
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
		markdownMode:  *markdownMode,
		coverStyle:    *coverStyle,
		concurrency:   conc,
		args:          flag.Args(),
	}

	if err := run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
