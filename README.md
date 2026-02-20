# deckle

Turn a reading list into a clean epub. Paste some URLs, get a book for your e-reader — no ads, no popups, no cookie banners, just the articles and their images.

Fetches each article, strips away everything that isn't content, optimizes images for e-ink displays, and packages the result as an epub with a table of contents.

## Quick start

```bash
# Build (requires Go 1.24+)
go install ./...

# Create a reading list
cat > reading-list.txt <<EOF
https://example.com/interesting-article
https://medium.com/@someone/another-great-post
EOF

# Convert to epub
deckle -epub -grayscale -o reading-list.epub reading-list.txt
```

## How it works

1. **Fetches** each page with browser-like TLS fingerprinting (handles Cloudflare, Medium, etc.)
2. **Extracts** the article using Mozilla's Readability algorithm — strips nav, footers, ads, sidebars
3. **Optimizes images** — fetches external images, collapses `<picture>` elements, resizes for e-ink (800px wide, grayscale JPEG)
4. **Adds attribution** — author byline and source URL under each chapter title
5. **Packages** into a valid epub3 with table of contents

## Epub mode

```bash
deckle -epub -grayscale -o output.epub urls.txt
deckle -epub -o output.epub https://example.com/article1 https://example.com/article2
```

You can mix URL files and individual URLs:

```bash
deckle -epub -o output.epub urls.txt https://example.com/bonus-article
```

## Single article mode

Process a single URL to HTML:

```bash
deckle https://example.com/article > article.html
deckle -grayscale -o article.html https://example.com/article
```

## Options

```
  -epub              Generate epub (requires -o)
  -max-width INT     Max image pixel width (default: 800)
  -quality INT       JPEG quality 1-95 (default: 60)
  -grayscale         Convert images to grayscale
  -title STRING      Override article/book title
  -o FILE            Output file (default: stdout)
  -timeout DURATION  HTTP fetch timeout (default: 30s)
```
