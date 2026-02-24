#!/bin/bash
# Run a full EPUB stress test: fetch pages, build epub, validate.
#
# Usage:
#   ./scripts/run_stress_test.sh                    # HN top 100 (default)
#   ./scripts/run_stress_test.sh hn 50              # HN top 50
#   ./scripts/run_stress_test.sh techmeme           # Techmeme headlines
#   ./scripts/run_stress_test.sh file urls.txt 200  # Custom URL list

set -e

SOURCE="${1:-hn}"
LIMIT="${3:-100}"
INPUT_FILE="${2:-}"
PORT=8765
PAGES_DIR="/tmp/stress_pages_$$"
URL_FILE="/tmp/stress_urls_$$.txt"
EPUB_FILE="/tmp/stress_test_$$.epub"

cleanup() {
    # Kill background server if running
    if [ -n "$SERVER_PID" ]; then
        kill "$SERVER_PID" 2>/dev/null || true
    fi
    rm -rf "$PAGES_DIR" "$URL_FILE"
}
trap cleanup EXIT

echo "=== Deckle EPUB Stress Test ==="
echo "Source: $SOURCE"
echo "Limit: $LIMIT"
echo ""

# Build deckle
echo "Building deckle..."
go build -o /tmp/deckle_stress_test .

# Fetch pages
FETCH_ARGS="--source $SOURCE --limit $LIMIT --pages-dir $PAGES_DIR --port $PORT --output $URL_FILE --no-serve"
if [ "$SOURCE" = "file" ] && [ -n "$INPUT_FILE" ]; then
    FETCH_ARGS="$FETCH_ARGS --input $INPUT_FILE"
fi

echo "Fetching pages..."
python3 scripts/fetch_pages.py $FETCH_ARGS

# Start a simple HTTP server in the background
echo "Starting local HTTP server on port $PORT..."
cd "$PAGES_DIR"
python3 -m http.server "$PORT" --bind localhost &>/dev/null &
SERVER_PID=$!
cd - > /dev/null
sleep 1

# Rewrite URLs to use localhost
sed -i "s|$PAGES_DIR/|http://localhost:$PORT/|g" "$URL_FILE"

# Count URLs
NUM_URLS=$(wc -l < "$URL_FILE")
echo "Fetched $NUM_URLS pages"

# Generate EPUB
echo ""
echo "Generating EPUB..."
/tmp/deckle_stress_test -epub -o "$EPUB_FILE" $(cat "$URL_FILE" | tr '\n' ' ')
echo "EPUB: $EPUB_FILE ($(du -h "$EPUB_FILE" | cut -f1))"

# Validate
echo ""
echo "=== Validating with epubcheck ==="
if ! command -v epubcheck &>/dev/null; then
    echo "ERROR: epubcheck is not installed. Install it and rerun."
    echo "  Ubuntu/Debian: sudo apt-get install epubcheck"
    echo "  macOS: brew install epubcheck"
    exit 1
fi

RESULTS_FILE="/tmp/epubcheck_results_$$.txt"
epubcheck "$EPUB_FILE" 2>&1 | tee "$RESULTS_FILE"

# Summary
echo ""
echo "=== Summary ==="
ERRORS=$(grep -c "^ERROR" "$RESULTS_FILE" 2>/dev/null || echo 0)
WARNINGS=$(grep -c "^WARNING" "$RESULTS_FILE" 2>/dev/null || echo 0)
FATALS=$(grep -c "^FATAL" "$RESULTS_FILE" 2>/dev/null || echo 0)
echo "Fatal: $FATALS"
echo "Errors: $ERRORS"
echo "Warnings: $WARNINGS"
echo "EPUB: $EPUB_FILE"
echo "Results: $RESULTS_FILE"

if [ "$FATALS" -gt 0 ] || [ "$ERRORS" -gt 0 ]; then
    echo ""
    echo "Error breakdown:"
    grep -oP '(RSC-\d+|OPF-\d+|CSS-\d+|HTM-\d+)' "$RESULTS_FILE" 2>/dev/null | sort | uniq -c | sort -rn || true
    exit 1
fi

echo ""
echo "EPUB is valid!"
