// Pipeline functions: URL processing, parallel fetching, and output assembly.
package builder

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

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

// fetchMultipleArticles fetches a list of URLs in parallel and returns the
// successfully processed articles in input order, skipping failures.
func fetchMultipleArticles(urls []string, cfg buildConfig) []epubArticle {
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
