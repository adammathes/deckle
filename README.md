# deckle

Turn a reading list into a tidy, efficient, optimized epub. Paste some URLs, get a book suitable for an e-reader with just the articles and their images and all the fat trimmed off.

Fetches each article, strips away everything that isn't content, optimizes images for e-ink displays, and packages the result as an epub with a table of contents.

Also outputs clean HTML or Markdown for single articles or collections.

## ⚠️ WARNING: Vibecoded Experiment

The author (Adam Mathes) is using this regularly but it is new and AI-generated. I think the tests and things are good but use at your own risk.

## Install

```bash
go install github.com/adammathes/deckle@latest
```

## Quick start

```bash
# Single article to markdown (default format)
deckle https://example.com/interesting-article

# Reading list to epub
deckle -format epub -grayscale -o reading-list.epub -i reading-list.txt

# Pipe URLs from stdin
cat urls.txt | deckle -format epub -o book.epub
```

## How it works

1. **Fetches** each page with browser-like TLS fingerprinting (handles Cloudflare, Medium, etc.)
2. **Extracts** the article using Mozilla's [Readability](https://codeberg.org/readeck/go-readability) algorithm — strips nav, footers, ads, sidebars
3. **Optimizes images** — fetches external images, collapses `<picture>` elements, resizes for e-ink (default 800px wide, grayscale JPEG)
4. **Adds attribution** — author byline and source URL under each chapter title
5. **Packages** into the chosen output format (markdown, HTML, or epub3 with table of contents)

## Usage

```
deckle [options] <URL> [<URL>...]
deckle [options] -i urls.txt
cat urls.txt | deckle [options]
deckle [options] -format epub -o out.epub <URL> [<URL>...]
```

URLs can be provided three ways, and all sources are combined:

- **Positional arguments**: direct URLs or `.txt` files (one URL per line)
- **`-i` flag**: an input file containing URLs (one per line, `#` comments and blank lines ignored)
- **Stdin**: pipe URLs in, one per line (`#` comments and blank lines ignored)

## Output formats

The `-format` flag controls the output (default: `markdown`):

### Markdown (default)

```bash
# Single article
deckle https://example.com/article > article.md

# Multiple articles, separated by ---
deckle https://example.com/article1 https://example.com/article2 -o combined.md

# From a URL file
deckle -i urls.txt -o articles.md
```

External image URLs are preserved as-is. Embedded data URI images are replaced with `[Image: alt text]` placeholders.

### HTML

```bash
# Single article
deckle -format html https://example.com/article -o article.html

# Multiple articles, separated by <hr>
deckle -format html -i urls.txt -o combined.html
```

Images are fetched, optimized, and embedded as data URIs. Output is a complete HTML document with inline styles.

### Epub

```bash
# From a URL file
deckle -format epub -grayscale -o reading-list.epub -i reading-list.txt

# From URLs directly
deckle -format epub -o output.epub https://example.com/article1 https://example.com/article2

# Pipe from stdin
echo "https://example.com/article" | deckle -format epub -o article.epub

# Mix sources
deckle -format epub -o book.epub -i urls.txt https://example.com/bonus-article
```

Epub requires `-o` for the output file. The book title is derived from: `-title` flag > input filename > first article title > output filename.

## Options

```
  -format STRING        Output format: html, markdown, or epub (default: markdown)
  -i FILE               Input file containing URLs (one per line, # comments ignored)
  -o FILE               Output file (default: stdout)
  -title STRING         Override article/book title
  -max-width INT        Max image pixel width (default: 800)
  -quality INT          JPEG quality 1-95 (default: 60)
  -grayscale            Convert images to grayscale
  -concurrency INT      Max concurrent downloads (default: 5)
  -cover STRING         Epub cover style: collage, pattern, or none (default: collage)
  -timeout DURATION     HTTP fetch timeout (default: 30s)
  -user-agent STRING    HTTP User-Agent header
  -proxy URL            HTTP proxy URL (e.g. http://proxy.example.com:8080)
  -max-response-size N  Max HTTP response size in bytes (default: 128MB, 0 for unlimited)
  -v                    Verbose output (show progress on stderr)
```

The old `-epub` and `-markdown` flags still work as aliases for `-format epub` and `-format markdown`.

## Origin

I asked AI to give me some reading lists on topics for my ereader and a pipeline for generating epub out of it using existing nice tools. I ended up with epubs with literally hundreds of megabytes of images and garbage in them that often didn't even get through the conversion process to epub! Or were invalid epubs. So I rolled up my sleeves, then rolled them back up and told AI to make this.

## Etymology

Books: [A deckle edge is a feathered edge on a piece of paper, in contrast to a cut edge ](https://en.wikipedia.org/wiki/Deckle_edge)

Beef: in the conext of corned beef: the point cut of the brisket, a fattier, more intensely flavored, and marbled portion of the beef brisket
