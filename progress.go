// Progress indicators for stdout when output goes to a file.
// Displays a single updating status line with article/image counters and a spinner.
package main

import (
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"
	"time"
)

// progressOut is the writer for progress indicators. Set to os.Stdout when
// -o is specified (stdout is not used for content output). In all other
// cases (stdout mode or --silent) it is io.Discard.
var progressOut io.Writer = io.Discard

// progress is the active status tracker, or nil when progress is disabled.
var progress *statusTracker

// statusTracker manages a single updating status line showing article and
// image processing progress with an ASCII spinner.
type statusTracker struct {
	mu            sync.Mutex
	out           io.Writer
	totalArticles int
	doneArticles  int
	totalImages   int
	doneImages    int
	spinIdx       int
	stopCh        chan struct{}
}

var spinChars = [...]byte{'|', '/', '-', '\\'}

func newStatusTracker(out io.Writer, totalArticles int) *statusTracker {
	return &statusTracker{
		out:           out,
		totalArticles: totalArticles,
		stopCh:        make(chan struct{}),
	}
}

func (s *statusTracker) startSpinner() {
	go func() {
		ticker := time.NewTicker(150 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-s.stopCh:
				return
			case <-ticker.C:
				s.mu.Lock()
				s.spinIdx++
				s.redraw()
				s.mu.Unlock()
			}
		}
	}()
}

// redraw writes the current status line. Must be called with mu held.
func (s *statusTracker) redraw() {
	spin := spinChars[s.spinIdx%len(spinChars)]

	var parts []string

	if s.totalArticles > 1 {
		parts = append(parts, fmt.Sprintf("[downloaded %d/%d articles]", s.doneArticles, s.totalArticles))
	} else if s.totalArticles == 1 && s.doneArticles == 0 && s.totalImages == 0 {
		parts = append(parts, "[fetching]")
	}

	if s.totalImages > 0 {
		parts = append(parts, fmt.Sprintf("[optimizing %d/%d images]", s.doneImages, s.totalImages))
	}

	if len(parts) == 0 {
		parts = append(parts, "[processing]")
	}

	line := strings.Join(parts, " ") + " " + string(spin)
	if n := 72 - len(line); n > 0 {
		line += strings.Repeat(" ", n)
	}
	fmt.Fprintf(s.out, "\r%s", line)
}

func (s *statusTracker) articleDone() {
	s.mu.Lock()
	s.doneArticles++
	s.redraw()
	s.mu.Unlock()
}

func (s *statusTracker) articleFailed() {
	s.mu.Lock()
	s.doneArticles++
	s.redraw()
	s.mu.Unlock()
}

func (s *statusTracker) addImages(n int) {
	if n <= 0 {
		return
	}
	s.mu.Lock()
	s.totalImages += n
	s.redraw()
	s.mu.Unlock()
}

func (s *statusTracker) imageDone() {
	s.mu.Lock()
	s.doneImages++
	s.redraw()
	s.mu.Unlock()
}

func (s *statusTracker) finish(msg string) {
	s.mu.Lock()
	select {
	case <-s.stopCh:
	default:
		close(s.stopCh)
	}
	line := msg
	if n := 72 - len(line); n > 0 {
		line += strings.Repeat(" ", n)
	}
	fmt.Fprintf(s.out, "\r%s\n", line)
	s.mu.Unlock()
}

// Package-level helpers that are safe to call when progress is nil.

func progressArticleDone() {
	if progress != nil {
		progress.articleDone()
	}
}

func progressArticleFailed() {
	if progress != nil {
		progress.articleFailed()
	}
}

func progressAddImages(n int) {
	if progress != nil {
		progress.addImages(n)
	}
}

func progressImageDone() {
	if progress != nil {
		progress.imageDone()
	}
}

func startProgress(totalArticles int) {
	if progressOut == io.Discard {
		return
	}
	progress = newStatusTracker(progressOut, totalArticles)
	// Draw initial state immediately so fast operations still show progress.
	progress.mu.Lock()
	progress.redraw()
	progress.mu.Unlock()
	progress.startSpinner()
}

func finishProgress(msg string) {
	if progress != nil {
		progress.finish(msg)
		progress = nil
	}
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
