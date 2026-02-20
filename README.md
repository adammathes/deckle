# urls2epub

Convert a list of URLs into an optimized epub for e-readers.

**Pipeline:** url2html (fetch + readability + image optimize + headings) → pandoc (epub)

## Prerequisites

```bash
# Go tools
go build -o url2html ./url2html/

# System packages
# Ubuntu/Debian:
sudo apt install pandoc fish
```

Make sure `url2html` and `pandoc` are on your `$PATH`.

## Usage

Create a text file with one URL per line (blank lines and `#` comments are ignored):

```
# my-reading-list.txt
https://example.com/article-one
https://example.com/article-two
```

Then run:

```fish
source urls2epub.fish
urls2epub my-reading-list.txt my-book.epub
```

Or use `url2html` directly on a single URL:

```bash
url2html --grayscale https://example.com/article > article.html
```

## What each stage does

| Stage | Tool | Purpose |
|-------|------|---------|
| Fetch | `url2html` | Downloads the page HTML via HTTP |
| Extract | `url2html` | Strips navigation, footers, ads — keeps only article content (go-readability) |
| Images | `url2html` | Fetches external images, collapses `<picture>` elements, resizes to 800px wide, converts to grayscale JPEG at quality 60 |
| Headings | `url2html` | Extracts article title, injects as H1, shifts content headings to H2+ |
| Build | `pandoc` | Combines all articles into epub3 with table of contents |

## url2html options

```
url2html [options] <URL>
  --max-width INT     Max image pixel width (default: 800)
  --quality INT       JPEG quality 1-95 (default: 60)
  --grayscale         Convert images to grayscale
  --title STRING      Override article title
  -o FILE             Output file (default: stdout)
  --timeout DURATION  HTTP fetch timeout (default: 30s)
  --user-agent STRING HTTP User-Agent header
```

## Files

- `urls2epub.fish` — main pipeline script (fish shell function)
- `url2html/` — Go tool that fetches, extracts, and optimizes articles
- `darksoftware/urls.txt` — example URL list
