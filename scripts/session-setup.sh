#!/bin/bash
# session-setup.sh — Ensure Go modules are available for build/test.
#
# In environments without direct DNS (e.g. Claude Code web sessions),
# Go's module proxy downloads fail because storage.googleapis.com is
# unreachable via the default DNS resolver. This script detects the
# global agent proxy and uses it for `go mod download`.
#
# The script is idempotent: if modules are already cached it exits fast.

set -euo pipefail

cd "$(dirname "$0")/.."

# Quick check: if `go build` works, modules are already cached.
if go build ./... 2>/dev/null; then
    exit 0
fi

echo "Go modules not fully cached — downloading via proxy..."

# Use the global agent proxy if available (Claude Code web environment).
# The default no_proxy includes *.googleapis.com which breaks Go module
# downloads, so we override it to only exclude localhost.
PROXY_URL="${GLOBAL_AGENT_HTTPS_PROXY:-${GLOBAL_AGENT_HTTP_PROXY:-}}"
if [ -n "$PROXY_URL" ]; then
    export HTTPS_PROXY="$PROXY_URL"
    export HTTP_PROXY="$PROXY_URL"
    export NO_PROXY="localhost,127.0.0.1"
fi

go mod download all
echo "Go modules downloaded successfully."
