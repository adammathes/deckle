// Package builder provides a library API for deckle: fetch URLs and produce
// clean HTML, Markdown, or EPUB suitable for e-readers.
//
// The primary entry point is [Build], which accepts a list of URLs and an
// [Options] struct controlling output format, image optimization, and other
// settings.
//
//	err := builder.Build(urls, builder.Options{
//	    Format: "epub",
//	    Output: "book.epub",
//	})
package builder

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Options configures a Build invocation.
type Options struct {
	// Title overrides the article or book title.
	Title string

	// Output is the file path to write to. Required for epub format.
	// For html and markdown, if empty the result is written to Writer
	// (which defaults to os.Stdout).
	Output string

	// Format is the output format: "html", "markdown", or "epub".
	// Defaults to "markdown" if empty.
	Format string

	// MaxWidth is the maximum image pixel width (height scales proportionally).
	// Defaults to 800.
	MaxWidth int

	// Quality is the JPEG quality (1-95) for optimized images.
	// Defaults to 60.
	Quality int

	// Grayscale converts images to grayscale when true.
	Grayscale bool

	// Timeout is the HTTP fetch timeout. Defaults to 30s.
	Timeout time.Duration

	// UserAgent is the HTTP User-Agent header. Defaults to a Firefox UA string.
	UserAgent string

	// CoverStyle controls epub cover generation: "typographic" (default),
	// "collage", "pattern", or "none".
	CoverStyle string

	// Concurrency is the maximum number of concurrent downloads for articles
	// and images. Defaults to 5.
	Concurrency int

	// MaxResponseSize is the maximum allowed HTTP response size in bytes.
	// 0 means unlimited. Defaults to 128 MB.
	MaxResponseSize int64

	// ProxyURL is an HTTP proxy URL for all outgoing requests.
	// When set, deckle falls back to standard TLS (no browser fingerprinting).
	ProxyURL string

	// Verbose enables progress output written to LogWriter.
	Verbose bool

	// LogWriter receives verbose/progress output when Verbose is true.
	// Defaults to os.Stderr.
	LogWriter io.Writer

	// Writer receives html/markdown output when Output is empty.
	// Defaults to os.Stdout. Ignored for epub format.
	Writer io.Writer
}

func (o *Options) applyDefaults() {
	if o.Format == "" {
		o.Format = "markdown"
	}
	if o.MaxWidth == 0 {
		o.MaxWidth = 800
	}
	if o.Quality == 0 {
		o.Quality = 60
	}
	if o.Timeout == 0 {
		o.Timeout = 30 * time.Second
	}
	if o.UserAgent == "" {
		o.UserAgent = defaultUA
	}
	if o.CoverStyle == "" {
		o.CoverStyle = "typographic"
	}
	if o.Concurrency < 1 {
		o.Concurrency = 5
	}
	if o.MaxResponseSize == 0 {
		o.MaxResponseSize = 128 * 1024 * 1024
	}
	if o.LogWriter == nil {
		o.LogWriter = os.Stderr
	}
	if o.Writer == nil {
		o.Writer = os.Stdout
	}
}

// Build processes the given URLs and produces output in the specified format.
// For epub format, Options.Output must be set.
//
// This is the main entry point for using deckle as a library.
func Build(urls []string, opts Options) error {
	opts.applyDefaults()

	switch opts.Format {
	case "html", "markdown", "epub":
	default:
		return fmt.Errorf("unknown format %q (must be html, markdown, or epub)", opts.Format)
	}

	if opts.Format == "epub" && opts.Output == "" {
		return fmt.Errorf("epub format requires an output path")
	}

	if len(urls) == 0 {
		return fmt.Errorf("no URLs provided")
	}

	// Configure package-level settings from options.
	maxResponseBytes = opts.MaxResponseSize
	fetchProxyURL = opts.ProxyURL

	if opts.Verbose {
		verboseOut = opts.LogWriter
	} else {
		verboseOut = io.Discard
	}
	logOut = io.Discard

	imgOpts := optimizeOpts{
		maxWidth:  opts.MaxWidth,
		quality:   opts.Quality,
		grayscale: opts.Grayscale,
	}

	cfg := buildConfig{
		opts:          imgOpts,
		output:        opts.Output,
		titleOverride: opts.Title,
		timeout:       opts.Timeout,
		userAgent:     opts.UserAgent,
		format:        opts.Format,
		coverStyle:    opts.CoverStyle,
		concurrency:   opts.Concurrency,
		writer:        opts.Writer,
	}

	switch opts.Format {
	case "epub":
		return runEpub(cfg, urls, "")
	case "markdown":
		return runMarkdown(cfg, urls)
	case "html":
		return runHTML(cfg, urls)
	}
	return nil
}

// buildConfig is the internal configuration used by the processing pipeline.
type buildConfig struct {
	opts          optimizeOpts
	output        string
	titleOverride string
	timeout       time.Duration
	userAgent     string
	format        string
	coverStyle    string
	concurrency   int
	writer        io.Writer // fallback writer when output is empty
}

// writeOutput writes content to a file, or to the config's writer if path is empty.
func writeOutput(cfg buildConfig, content string) error {
	if cfg.output != "" {
		if err := os.WriteFile(cfg.output, []byte(content), 0644); err != nil {
			return fmt.Errorf("writing output: %w", err)
		}
		return nil
	}
	if _, err := io.WriteString(cfg.writer, content); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}
	return nil
}

func runEpub(cfg buildConfig, urls []string, txtFilename string) error {
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

func runMarkdown(cfg buildConfig, urls []string) error {
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
		return writeOutput(cfg, md+"\n")
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
	return writeOutput(cfg, md+"\n")
}

func runHTML(cfg buildConfig, urls []string) error {
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
		return writeOutput(cfg, final)
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
	return writeOutput(cfg, html)
}

// collectURLsFromFile reads a file containing one URL per line, skipping blanks and comments.
func collectURLsFromFile(path string) ([]string, error) {
	return readURLFile(path)
}

// collectAllURLsForCLI gathers URLs from all CLI sources: input file, positional args,
// and stdin (when piped). This is used by the CLI wrapper.
func collectAllURLsForCLI(inputFile string, args []string, stdinReader io.Reader) (urls []string, txtFilename string, err error) {
	// From -i flag
	if inputFile != "" {
		fileURLs, ferr := readURLFile(inputFile)
		if ferr != nil {
			return nil, "", fmt.Errorf("reading %s: %w", inputFile, ferr)
		}
		urls = append(urls, fileURLs...)
		name := filepath.Base(inputFile)
		ext := filepath.Ext(name)
		txtFilename = strings.TrimSuffix(name, ext)
	}

	// From positional args (URLs and .txt files)
	argURLs, argTxt, aerr := collectURLs(args)
	if aerr != nil {
		return nil, "", aerr
	}
	urls = append(urls, argURLs...)
	if txtFilename == "" && argTxt != "" {
		txtFilename = argTxt
	}

	// From stdin (when piped)
	if stdinReader != nil {
		stdinURLs, serr := readURLLines(stdinReader)
		if serr != nil {
			return nil, "", fmt.Errorf("reading stdin: %w", serr)
		}
		urls = append(urls, stdinURLs...)
	}

	return urls, txtFilename, nil
}

// RunCLI executes the CLI-oriented workflow: collects URLs from all sources
// (input file, args, stdin) and calls Build. This is used by the CLI wrapper
// and supports txt file title derivation for epub.
func RunCLI(inputFile string, args []string, stdinReader io.Reader, opts Options) error {
	opts.applyDefaults()

	switch opts.Format {
	case "html", "markdown", "epub":
	default:
		return fmt.Errorf("unknown format %q (must be html, markdown, or epub)", opts.Format)
	}

	if opts.Format == "epub" && opts.Output == "" {
		return fmt.Errorf("epub format requires -o output.epub")
	}

	urls, txtFilename, err := collectAllURLsForCLI(inputFile, args, stdinReader)
	if err != nil {
		return err
	}
	if len(urls) == 0 {
		return fmt.Errorf("no URLs provided")
	}

	// Configure package-level settings from options.
	maxResponseBytes = opts.MaxResponseSize
	fetchProxyURL = opts.ProxyURL

	if opts.Verbose {
		verboseOut = opts.LogWriter
	} else {
		verboseOut = io.Discard
	}
	logOut = io.Discard

	imgOpts := optimizeOpts{
		maxWidth:  opts.MaxWidth,
		quality:   opts.Quality,
		grayscale: opts.Grayscale,
	}

	cfg := buildConfig{
		opts:          imgOpts,
		output:        opts.Output,
		titleOverride: opts.Title,
		timeout:       opts.Timeout,
		userAgent:     opts.UserAgent,
		format:        opts.Format,
		coverStyle:    opts.CoverStyle,
		concurrency:   opts.Concurrency,
		writer:        opts.Writer,
	}

	switch opts.Format {
	case "epub":
		return runEpub(cfg, urls, txtFilename)
	case "markdown":
		return runMarkdown(cfg, urls)
	case "html":
		return runHTML(cfg, urls)
	}
	return nil
}
