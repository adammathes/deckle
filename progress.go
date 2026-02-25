// Progress indicators for stdout when output goes to a file.
// When -o is specified, stdout is free for user-facing progress display.
package main

import (
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"
)

// progressOut is the writer for progress indicators. Set to os.Stdout when
// -o is specified (stdout is not used for content output). In all other
// cases (stdout mode or --silent) it is io.Discard.
var progressOut io.Writer = io.Discard

// progressMu serialises writes to progressOut so concurrent goroutines
// (e.g. in fetchMultipleArticles) don't interleave output lines.
var progressMu sync.Mutex

// pprintf writes a formatted progress line to progressOut, holding the
// mutex to prevent interleaving from concurrent goroutines.
func pprintf(format string, args ...any) {
	progressMu.Lock()
	defer progressMu.Unlock()
	fmt.Fprintf(progressOut, format, args...)
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
