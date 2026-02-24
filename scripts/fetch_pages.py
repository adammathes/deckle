#!/usr/bin/env python3
"""Fetch web pages for EPUB stress testing.

Downloads pages from various sources, saves them locally, and optionally
starts a local HTTP server so deckle can process them without network issues.

Usage:
    # Hacker News top stories
    python3 fetch_pages.py --source hn --limit 100 --output /tmp/urls.txt

    # Techmeme headlines
    python3 fetch_pages.py --source techmeme --output /tmp/urls.txt

    # Custom URL list
    python3 fetch_pages.py --source file --input urls.txt --output /tmp/urls.txt
"""

import argparse
import hashlib
import http.server
import json
import os
import re
import signal
import sys
import threading
import time
import urllib.request
import urllib.error


DEFAULT_PAGES_DIR = "/tmp/stress_pages"
DEFAULT_PORT = 8765
DEFAULT_OUTPUT = "/tmp/stress_urls.txt"


def fetch_url(url, timeout=15, retries=2):
    """Fetch a URL with retries. Returns response body as string or None."""
    for attempt in range(retries + 1):
        try:
            req = urllib.request.Request(url, headers={
                "User-Agent": "Mozilla/5.0 (compatible; deckle-stresstest/1.0)"
            })
            with urllib.request.urlopen(req, timeout=timeout) as resp:
                return resp.read().decode("utf-8", errors="replace")
        except Exception as e:
            if attempt < retries:
                time.sleep(1 * (attempt + 1))
            else:
                print(f"  Failed to fetch {url}: {e}", file=sys.stderr)
                return None


def fetch_hn_urls(limit=100):
    """Fetch top story URLs from Hacker News Firebase API."""
    print(f"Fetching top {limit} Hacker News stories...")
    body = fetch_url("https://hacker-news.firebaseio.com/v0/topstories.json")
    if not body:
        print("Failed to fetch HN top stories", file=sys.stderr)
        return []

    story_ids = json.loads(body)[:limit * 2]  # fetch extra in case some lack URLs
    urls = []

    for i, sid in enumerate(story_ids):
        if len(urls) >= limit:
            break
        item_body = fetch_url(f"https://hacker-news.firebaseio.com/v0/item/{sid}.json")
        if not item_body:
            continue
        item = json.loads(item_body)
        url = item.get("url", "")
        if url and not url.startswith("https://news.ycombinator.com"):
            urls.append(url)
            if (i + 1) % 20 == 0:
                print(f"  Found {len(urls)} URLs so far...")
        time.sleep(0.05)  # rate limit

    print(f"Found {len(urls)} story URLs")
    return urls


def fetch_techmeme_urls(limit=100):
    """Fetch headline URLs from Techmeme."""
    print("Fetching Techmeme headlines...")
    body = fetch_url("https://techmeme.com/")
    if not body:
        print("Failed to fetch Techmeme", file=sys.stderr)
        return []

    # Extract article URLs from Techmeme's headline links
    # Techmeme links to external articles via specific patterns
    urls = []
    seen = set()
    for match in re.finditer(r'href="(https?://(?!techmeme\.com)[^"]+)"', body):
        url = match.group(1)
        # Skip common non-article URLs
        if any(skip in url for skip in [
            "twitter.com", "x.com", "facebook.com", "linkedin.com",
            "youtube.com", "reddit.com", ".js", ".css", ".png", ".jpg",
            "accounts.google", "play.google", "apps.apple"
        ]):
            continue
        if url not in seen:
            seen.add(url)
            urls.append(url)
            if len(urls) >= limit:
                break

    print(f"Found {len(urls)} article URLs")
    return urls


def fetch_file_urls(input_path, limit=100):
    """Read URLs from a file, one per line."""
    if not os.path.exists(input_path):
        print(f"Input file not found: {input_path}", file=sys.stderr)
        return []

    urls = []
    with open(input_path) as f:
        for line in f:
            url = line.strip()
            if url and url.startswith("http"):
                urls.append(url)
                if len(urls) >= limit:
                    break

    print(f"Read {len(urls)} URLs from {input_path}")
    return urls


SOURCES = {
    "hn": lambda limit, **_: fetch_hn_urls(limit),
    "techmeme": lambda limit, **_: fetch_techmeme_urls(limit),
    "file": lambda limit, input=None, **_: fetch_file_urls(input, limit),
}


def download_pages(urls, pages_dir):
    """Download each URL and save to pages_dir. Returns list of saved filenames."""
    os.makedirs(pages_dir, exist_ok=True)
    saved = []

    for i, url in enumerate(urls):
        filename = hashlib.md5(url.encode()).hexdigest() + ".html"
        filepath = os.path.join(pages_dir, filename)

        # Skip if already downloaded
        if os.path.exists(filepath) and os.path.getsize(filepath) > 0:
            saved.append(filename)
            continue

        body = fetch_url(url, timeout=20)
        if body and len(body) > 100:
            with open(filepath, "w", encoding="utf-8") as f:
                f.write(body)
            saved.append(filename)
        else:
            print(f"  Skipping ({i+1}/{len(urls)}): {url}", file=sys.stderr)

        if (i + 1) % 10 == 0:
            print(f"  Downloaded {i+1}/{len(urls)} pages ({len(saved)} saved)")
        time.sleep(0.2)  # rate limit

    print(f"Downloaded {len(saved)}/{len(urls)} pages to {pages_dir}")
    return saved


def start_server(pages_dir, port):
    """Start a local HTTP server serving pages_dir."""
    handler = lambda *args: http.server.SimpleHTTPRequestHandler(
        *args, directory=pages_dir
    )
    server = http.server.HTTPServer(("localhost", port), handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    print(f"Serving pages at http://localhost:{port}/")
    return server


def main():
    parser = argparse.ArgumentParser(
        description="Fetch web pages for deckle EPUB stress testing"
    )
    parser.add_argument(
        "--source", required=True, choices=list(SOURCES.keys()),
        help="URL source: hn (Hacker News), techmeme, file"
    )
    parser.add_argument(
        "--input", default=None,
        help="Input file path (required for --source file)"
    )
    parser.add_argument(
        "--output", default=DEFAULT_OUTPUT,
        help=f"Output URL file path (default: {DEFAULT_OUTPUT})"
    )
    parser.add_argument(
        "--limit", type=int, default=100,
        help="Max pages to fetch (default: 100)"
    )
    parser.add_argument(
        "--pages-dir", default=DEFAULT_PAGES_DIR,
        help=f"Directory to save pages (default: {DEFAULT_PAGES_DIR})"
    )
    parser.add_argument(
        "--port", type=int, default=DEFAULT_PORT,
        help=f"Local HTTP server port (default: {DEFAULT_PORT})"
    )
    parser.add_argument(
        "--no-serve", action="store_true",
        help="Don't start HTTP server (just fetch and save)"
    )

    args = parser.parse_args()

    if args.source == "file" and not args.input:
        parser.error("--input is required when --source is file")

    # Fetch URLs from source
    source_fn = SOURCES[args.source]
    urls = source_fn(limit=args.limit, input=args.input)
    if not urls:
        print("No URLs found", file=sys.stderr)
        sys.exit(1)

    # Download pages
    saved = download_pages(urls, args.pages_dir)
    if not saved:
        print("No pages downloaded", file=sys.stderr)
        sys.exit(1)

    # Start server and write localhost URLs
    if not args.no_serve:
        server = start_server(args.pages_dir, args.port)

    with open(args.output, "w") as f:
        for filename in saved:
            if args.no_serve:
                f.write(os.path.join(args.pages_dir, filename) + "\n")
            else:
                f.write(f"http://localhost:{args.port}/{filename}\n")

    print(f"Wrote {len(saved)} URLs to {args.output}")

    if not args.no_serve:
        print(f"\nServer running. Generate EPUB with:")
        print(f"  ./deckle -epub -o stress_test.epub $(cat {args.output} | tr '\\n' ' ')")
        print(f"\nPress Ctrl+C to stop the server.")
        try:
            signal.pause()
        except KeyboardInterrupt:
            print("\nShutting down server...")
            server.shutdown()


if __name__ == "__main__":
    main()
