# EPUB Stress Testing

This guide explains how to stress test deckle's EPUB output by generating EPUBs from large, diverse sets of real-world web pages and validating them with epubcheck.

## Prerequisites

- **epubcheck** (W3C EPUB validator, Java-based):
  ```bash
  # Ubuntu/Debian
  sudo apt-get install epubcheck

  # macOS (homebrew)
  brew install epubcheck

  # Or download from https://www.w3.org/publishing/epubcheck/
  ```

- **Python 3** (for the page fetching script)
- **Go** (to build deckle)

## Quick Start

```bash
# 1. Build deckle
go build -o deckle .

# 2. Fetch pages from a URL source (e.g., Hacker News top stories)
python3 scripts/fetch_pages.py --source hn --limit 100 --output /tmp/stress_urls.txt

# 3. Generate the EPUB
./deckle -epub -o /tmp/stress_test.epub $(cat /tmp/stress_urls.txt | tr '\n' ' ')

# 4. Validate with epubcheck
epubcheck /tmp/stress_test.epub
```

## Page Fetching Script

The `scripts/fetch_pages.py` script fetches web pages, saves them locally, and serves them via a local HTTP server so deckle can process them. This avoids issues with TLS fingerprinting and proxy servers.

### Built-in Sources

**Hacker News** (top stories from the past 24 hours):
```bash
python3 scripts/fetch_pages.py --source hn --limit 100 --output /tmp/urls.txt
```

**Techmeme** (current headlines):
```bash
python3 scripts/fetch_pages.py --source techmeme --output /tmp/urls.txt
```

**Custom URL list** (from a file, one URL per line):
```bash
python3 scripts/fetch_pages.py --source file --input my_urls.txt --output /tmp/urls.txt
```

### How It Works

1. Fetches URLs from the chosen source
2. Downloads each page's HTML content
3. Saves pages to a local directory (`/tmp/stress_pages/` by default)
4. Starts a local HTTP server (default port 8765)
5. Writes localhost URLs to the output file

The script handles:
- Rate limiting to avoid hammering servers
- Retries on transient failures
- Deduplication of URLs
- Graceful handling of pages that fail to fetch

### Options

```
--source       Source type: hn, techmeme, file (required)
--input        Input file path (required for --source file)
--output       Output URL file path (default: /tmp/stress_urls.txt)
--limit        Max number of pages to fetch (default: 100)
--pages-dir    Directory to save fetched pages (default: /tmp/stress_pages)
--port         Local HTTP server port (default: 8765)
--no-serve     Don't start HTTP server (just fetch and save)
```

## Validation

After generating an EPUB, validate it:

```bash
# Basic validation
epubcheck /tmp/stress_test.epub

# Save output to a file for analysis
epubcheck /tmp/stress_test.epub 2>&1 | tee /tmp/epubcheck_results.txt

# Count errors by type
epubcheck /tmp/stress_test.epub 2>&1 | grep -oP '(RSC-\d+|OPF-\d+|CSS-\d+)' | sort | uniq -c | sort -rn
```

### Common Error Categories

| Code | Description | Typical Cause |
|------|-------------|---------------|
| RSC-006 | Remote resource reference | External `http://` or `https://` URLs in `src` attributes |
| RSC-005 | XHTML schema validation | Invalid nesting, missing attributes, bad attribute values |
| OPF-014 | Missing `remote-resources` property | Remote resource referenced without declaring it |
| FATAL | Invalid XML character | Control characters (U+0000-U+001F except tab/newline/cr) |

### Interpreting Results

- **0 errors**: EPUB is fully valid
- **RSC-006 errors**: Images or other resources with external URLs — `sanitizeForXHTML` should strip these
- **RSC-005 errors**: XHTML schema violations — check element nesting, attribute values, content models
- **FATAL errors**: Usually invalid XML characters in the source HTML

## Writing a Custom Source

To add a new source to `fetch_pages.py`, add a function that returns a list of URLs:

```python
def fetch_my_source_urls(limit):
    """Fetch URLs from my custom source."""
    # ... fetch and parse your source ...
    return ["https://example.com/article1", "https://example.com/article2"]
```

Then add it to the `SOURCES` dict in the script.

Alternatively, just create a plain text file with one URL per line and use `--source file --input urls.txt`.

## Tips

- **Diverse sources produce better coverage.** Different sites use different HTML patterns (tables, definition lists, nested picture elements, etc.)
- **100+ pages is a good target.** This catches edge cases that smaller test sets miss.
- **Save epubcheck output.** When fixing issues, compare before/after validation results.
- **Run `go test` after fixes.** Regression tests in `epub_test.go` cover all previously-found validation issues.

## Previously Found Issues

The following validation issues were discovered during stress testing with 87 Hacker News pages (February 2025) and fixed:

1. **Invalid XML characters** (FATAL) — Control characters like U+0012 in article text
2. **`<source>` elements** (RSC-005) — Missing required `srcset` attribute
3. **`<picture>` elements** (RSC-005/RSC-006) — Not valid EPUB elements; must collapse to `<img>`
4. **External image URLs** (RSC-006) — `http://` and `https://` `src` on `<img>` tags
5. **Duplicate IDs** (RSC-005) — Same `id` attribute on multiple elements
6. **Invalid dimension attributes** (RSC-005) — Decimal values like `width="1.5"` (must be integers)
7. **Dimensions on wrong elements** (RSC-005) — `width`/`height` on `<source>`, `<div>`, etc.
8. **`<table>` inside `<p>`** (RSC-005) — Block elements nested inside phrasing content
9. **`<dl>` content model** (RSC-005) — Bare text, missing `<dt>`/`<dd>` pairs
10. **`<figcaption>` outside `<figure>`** (RSC-005) — Invalid placement
11. **IDs with whitespace** (RSC-005) — Spaces in `id` attribute values
