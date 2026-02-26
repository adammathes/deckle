// deckle: Fetch URLs and produce clean HTML, Markdown, or epub for e-readers.
//
//	deckle [options] <URL> [<URL>...]
//	deckle [options] -i urls.txt
//	cat urls.txt | deckle [options]
//	deckle [options] -format epub -o output.epub <URL> [<URL>...]
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// logOut is the writer for detailed informational output (warnings, per-URL
// status). Defaults to io.Discard (silent). Enabled by -v.
var logOut io.Writer = io.Discard

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

	return readURLLines(f)
}

// readURLLines reads URLs from a reader, one per line, skipping blanks and
// lines starting with #.
func readURLLines(r io.Reader) ([]string, error) {
	var urls []string
	scanner := bufio.NewScanner(r)
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

// collectAllURLs gathers URLs from all sources: -i file, positional args,
// and stdin (when piped).
func collectAllURLs(cfg cliConfig) (urls []string, txtFilename string, err error) {
	// From -i flag
	if cfg.inputFile != "" {
		fileURLs, ferr := readURLFile(cfg.inputFile)
		if ferr != nil {
			return nil, "", fmt.Errorf("reading %s: %w", cfg.inputFile, ferr)
		}
		urls = append(urls, fileURLs...)
		name := filepath.Base(cfg.inputFile)
		ext := filepath.Ext(name)
		txtFilename = strings.TrimSuffix(name, ext)
	}

	// From positional args (URLs and .txt files)
	argURLs, argTxt, aerr := collectURLs(cfg.args)
	if aerr != nil {
		return nil, "", aerr
	}
	urls = append(urls, argURLs...)
	if txtFilename == "" && argTxt != "" {
		txtFilename = argTxt
	}

	// From stdin (when piped)
	if cfg.stdinReader != nil {
		stdinURLs, serr := readURLLines(cfg.stdinReader)
		if serr != nil {
			return nil, "", fmt.Errorf("reading stdin: %w", serr)
		}
		urls = append(urls, stdinURLs...)
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
				return
			}
			results[i] = result{html: h, title: t, src: src, ok: true}
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

// articlesToHTML concatenates a slice of processed articles into a single
// HTML document. Articles are separated by a horizontal rule.
func articlesToHTML(articles []epubArticle) (string, error) {
	if len(articles) == 0 {
		return "", fmt.Errorf("no articles to render")
	}

	var parts []string
	for _, a := range articles {
		body := extractBodyContent(a.HTML)
		parts = append(parts, body)
	}

	combined := strings.Join(parts, "\n<hr>\n")

	title := articles[0].Title
	if len(articles) > 1 {
		title += " & more"
	}
	return renderFullHTML(combined, title, sourceInfo{}), nil
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
	format        string    // "html", "markdown", or "epub"
	coverStyle    string
	concurrency   int
	inputFile     string    // -i flag: read URLs from this file
	stdinReader   io.Reader // if non-nil, read URLs from this reader (stdin pipe)
	args          []string  // positional arguments (URLs or .txt files)
}

// run executes the main application logic, returning any error.
func run(cfg cliConfig) error {
	if cfg.format == "" {
		cfg.format = "markdown"
	}
	if cfg.concurrency < 1 {
		cfg.concurrency = 5
	}

	switch cfg.format {
	case "html", "markdown", "epub":
	default:
		return fmt.Errorf("unknown format %q (must be html, markdown, or epub)", cfg.format)
	}

	if cfg.format == "epub" && cfg.output == "" {
		return fmt.Errorf("epub format requires -o output.epub")
	}

	urls, txtFilename, err := collectAllURLs(cfg)
	if err != nil {
		return err
	}
	if len(urls) == 0 {
		return fmt.Errorf("no URLs provided")
	}

	switch cfg.format {
	case "epub":
		return runEpub(cfg, urls, txtFilename)
	case "markdown":
		return runMarkdown(cfg, urls)
	case "html":
		return runHTML(cfg, urls)
	}
	return nil
}

func runEpub(cfg cliConfig, urls []string, txtFilename string) error {
	totalImages.Store(0)
	vprintf("Fetching %d URLs\n", len(urls))

	articles := fetchMultipleArticles(urls, cfg)
	if len(articles) == 0 {
		return fmt.Errorf("no articles converted")
	}
	if n := totalImages.Load(); n > 0 {
		vprintf("Fetching, optimizing and embedding %d images\n", n)
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

	vprintf("Building epub at %s\n", cfg.output)
	if err := buildEpub(articles, bookTitle, cfg.output, cfg.coverStyle); err != nil {
		return fmt.Errorf("building epub: %w", err)
	}
	return nil
}

func runMarkdown(cfg cliConfig, urls []string) error {
	// Markdown output uses original image URLs, not embedded data URIs,
	// so there is no point downloading images.
	mdOpts := cfg.opts
	mdOpts.skipImageFetch = true

	if len(urls) == 1 {
		vprintf("Fetching 1 URL\n")
		final, _, _, err := processURL(urls[0], mdOpts, cfg.timeout, cfg.userAgent, cfg.titleOverride, cfg.concurrency)
		if err != nil {
			return err
		}
		md, err := convertArticleToMarkdown(final)
		if err != nil {
			return err
		}
		return writeOutput(cfg.output, md+"\n")
	}

	// Multiple URLs: fetch in parallel, concatenate with separators.
	mdCfg := cfg
	mdCfg.opts = mdOpts
	vprintf("Fetching %d URLs\n", len(urls))
	articles := fetchMultipleArticles(urls, mdCfg)
	if len(articles) == 0 {
		return fmt.Errorf("no articles converted")
	}
	md, err := articlesToMarkdown(articles)
	if err != nil {
		return err
	}
	return writeOutput(cfg.output, md+"\n")
}

func runHTML(cfg cliConfig, urls []string) error {
	totalImages.Store(0)

	if len(urls) == 1 {
		vprintf("Fetching 1 URL\n")
		final, _, _, err := processURL(urls[0], cfg.opts, cfg.timeout, cfg.userAgent, cfg.titleOverride, cfg.concurrency)
		if err != nil {
			return err
		}
		if n := totalImages.Load(); n > 0 {
			vprintf("Fetching, optimizing and embedding %d images\n", n)
		}
		return writeOutput(cfg.output, final)
	}

	// Multiple URLs: fetch in parallel, concatenate with separators.
	vprintf("Fetching %d URLs\n", len(urls))
	articles := fetchMultipleArticles(urls, cfg)
	if len(articles) == 0 {
		return fmt.Errorf("no articles converted")
	}
	if n := totalImages.Load(); n > 0 {
		vprintf("Fetching, optimizing and embedding %d images\n", n)
	}
	html, err := articlesToHTML(articles)
	if err != nil {
		return err
	}
	return writeOutput(cfg.output, html)
}

func main() {
	maxWidth := flag.Int("max-width", 800, "Max pixel width (height scales proportionally)")
	quality := flag.Int("quality", 60, "JPEG quality 1-95")
	grayscale := flag.Bool("grayscale", false, "Convert to grayscale")
	output := flag.String("o", "", "Output file (default: stdout)")
	titleOverride := flag.String("title", "", "Override article/book title")
	timeout := flag.Duration("timeout", 30*time.Second, "HTTP fetch timeout")
	userAgent := flag.String("user-agent", defaultUA, "HTTP User-Agent header")
	outputFmt := flag.String("format", "markdown", "Output format: html, markdown, or epub")
	inputFile := flag.String("i", "", "Input file containing URLs (one per line, # comments ignored)")
	coverStyle := flag.String("cover", "collage", "Cover style: 'collage', 'pattern', or 'none'")
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

	if *verbose {
		verboseOut = os.Stderr
	}

	maxResponseBytes = *maxRespSize
	fetchProxyURL = *proxy

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
		format:        fmtVal,
		coverStyle:    *coverStyle,
		concurrency:   conc,
		inputFile:     *inputFile,
		stdinReader:   stdinReader,
		args:          flag.Args(),
	}

	if err := run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
