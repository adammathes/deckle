# urls2epub

Convert a list of URLs into an optimized epub for e-readers.

**Pipeline:** monolith (fetch+embed) → html-img-optimize (shrink images) → go-readability (extract article) → pandoc (epub)

## Prerequisites

```bash
# Rust + monolith
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh
cargo install monolith

# Go tools
go build -o html-img-optimize ./html-img-optimize/
go install codeberg.org/readeck/go-readability/cmd/go-readability@latest

# System packages
# Ubuntu/Debian:
sudo apt install pandoc python3 fish
```

Make sure `monolith`, `html-img-optimize`, `go-readability`, `pandoc`, and `python3` are all on your `$PATH`.

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

## What each stage does

| Stage | Tool | Purpose |
|-------|------|---------|
| Fetch | `monolith` | Downloads each URL as self-contained HTML with all images embedded as base64 data URIs |
| Images | `html-img-optimize` | Resizes images to 800px wide, converts to grayscale JPEG at quality 60, collapses `<picture>` elements. Handles PNG, JPEG, GIF, WebP; passes through SVG and AVIF |
| Extract | `go-readability` | Strips navigation, footers, ads, comments — keeps only article content and images |
| Headings | `html-normalize-headings.py` | Extracts article title, injects as H1, shifts content headings to H2+ for consistent epub chapters |
| Build | `pandoc` | Combines all articles into epub3 with table of contents |

## Files

- `urls2epub.fish` — main pipeline script (fish shell function)
- `html-img-optimize/` — Go tool for optimizing base64-embedded images in HTML
- `html-normalize-headings.py` — Python script for normalizing heading hierarchy
- `darksoftware/urls.txt` — example URL list
