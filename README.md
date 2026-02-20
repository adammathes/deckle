# urls2epub

Turn a reading list into a clean epub. Paste some URLs, get a book for your e-reader — no ads, no popups, no cookie banners, just the articles and their images.

The tool fetches each article, strips away everything that isn't content, optimizes images for e-ink displays, and packages the result as an epub with a table of contents.

## Quick start

```bash
# Build (requires Go 1.24+)
cd url2html && go install ./... && cd ..

# Create a reading list
cat > reading-list.txt <<EOF
https://example.com/interesting-article
https://medium.com/@someone/another-great-post
EOF

# Convert to epub
source urls2epub.fish
urls2epub reading-list.txt reading-list.epub
```

You'll also need [pandoc](https://pandoc.org/installing.html) and [fish](https://fishshell.com/) installed.

## How it works

Each URL goes through a single Go binary (`url2html`) that:

1. **Fetches** the page with browser-like TLS fingerprinting (handles Cloudflare, Medium, etc.)
2. **Extracts** the article using Mozilla's Readability algorithm — strips nav, footers, ads, sidebars
3. **Optimizes images** — fetches external images, collapses `<picture>` elements, resizes for e-ink (800px wide, grayscale JPEG)
4. **Normalizes headings** — extracts the title, gives each article a clean H1 for epub chapter breaks

Then `pandoc` combines all articles into an epub3 with a table of contents.

## Single article mode

Process a single URL without building an epub:

```bash
url2html https://example.com/article > article.html
url2html --grayscale -o article.html https://example.com/article
```

### Options

```
url2html [options] <URL>
  --max-width INT     Max image pixel width (default: 800)
  --quality INT       JPEG quality 1-95 (default: 60)
  --grayscale         Convert images to grayscale
  --title STRING      Override article title
  -o FILE             Output file (default: stdout)
  --timeout DURATION  HTTP fetch timeout (default: 30s)
```

## Files

- `urls2epub.fish` — batch pipeline: loops URLs, calls url2html, then pandoc
- `url2html/` — Go binary that does the heavy lifting
- `darksoftware/urls.txt` — example URL list
