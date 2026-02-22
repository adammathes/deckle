# deckle

Turn a reading list into a tidy, efficient, optimized epub. Paste some URLs, get a book suitable for an e-reader with just the articles and their images and all the fat trimmed off.

Fetches each article, strips away everything that isn't content, optimizes images for e-ink displays, and packages the result as an epub with a table of contents.

## ⚠️ WARNING: Vibecoded Experiment

The author (Adam Mathes) is using this regularly but it is new and AI-generated. I think the tests and things are good but use at your own risk.

## Install

```bash
go install github.com/adammathes/deckle@latest
```

## Quick start

```bash
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
2. **Extracts** the article using Mozilla's [Readability](https://codeberg.org/readeck/go-readability) algorithm — strips nav, footers, ads, sidebars
3. **Optimizes images** — fetches external images, collapses `<picture>` elements, resizes for e-ink (default 800px wide, grayscale JPEG)
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
  -epub                 Generate epub (requires -o)
  -max-width INT        Max image pixel width (default: 800)
  -quality INT          JPEG quality 1-95 (default: 60)
  -grayscale            Convert images to grayscale
  -title STRING         Override article/book title
  -o FILE               Output file (default: stdout)
  -timeout DURATION     HTTP fetch timeout (default: 30s)
  -user-agent STRING    HTTP User-Agent header
  -concurrency INT      Max concurrent downloads for articles and images (default: 5)
  -silent               Suppress all output except errors (for pipeline use)
```

## Origin 

I asked AI to give me some reading lists on topics for my ereader and a pipeline for generating epub out of it using existing nice tools. I ended up with epubs with literally hundreds of megabytes of images and garbage in them that often didn't even get through the conversion process to epub! Or were invalid epubs. So I rolled up my sleeves, then rolled them back up and told AI to make this.

## Etymology

Books: [A deckle edge is a feathered edge on a piece of paper, in contrast to a cut edge ](https://en.wikipedia.org/wiki/Deckle_edge)

Beef: in the conext of corned beef: the point cut of the brisket, a fattier, more intensely flavored, and marbled portion of the beef brisket
