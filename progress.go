// Verbose output for deckle.
// Default: no output except errors. With -v, simple summary lines on stderr.
package main

import (
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"
)

// verboseOut is the writer for verbose summary lines. Set to os.Stderr
// when -v is specified, otherwise io.Discard (silent by default).
var verboseOut io.Writer = io.Discard

// verboseMu serialises writes to verboseOut so concurrent goroutines
// don't interleave output lines.
var verboseMu sync.Mutex

// vprintf writes a formatted line to verboseOut when -v is active.
func vprintf(format string, args ...any) {
	verboseMu.Lock()
	defer verboseMu.Unlock()
	fmt.Fprintf(verboseOut, format, args...)
}

// shortURL returns a compact display form of a URL: host + trimmed path,
// no scheme. Truncated to 60 characters with "..." if needed.
func shortURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	display := u.Host + u.Path
	display = strings.TrimSuffix(display, "/")
	if len(display) > 60 {
		display = display[:57] + "..."
	}
	return display
}
