// Image optimization for HTML with base64-embedded images.
// Resizes, converts to grayscale, JPEG-encodes for e-readers.
package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"html"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	_ "image/png"
	"math"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

func humanSize(n int64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	f := float64(n)
	for _, u := range units {
		if math.Abs(f) < 1024 {
			return fmt.Sprintf("%.1f%s", f, u)
		}
		f /= 1024
	}
	return fmt.Sprintf("%.1f%s", f, units[len(units)-1])
}

// resize downscales an image using BiLinear resampling.
func resize(src image.Image, dstW, dstH int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, dstW, dstH))
	xdraw.BiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)
	return dst
}

func toGrayscale(src image.Image) *image.Gray {
	b := src.Bounds()
	gray := image.NewGray(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			gray.Set(x, y, color.GrayModel.Convert(src.At(x, y)))
		}
	}
	return gray
}

// flattenAlpha composites src onto a white background.
func flattenAlpha(src image.Image) *image.NRGBA {
	b := src.Bounds()
	dst := image.NewNRGBA(b)
	white := image.NewUniform(color.White)
	draw.Draw(dst, b, white, image.Point{}, draw.Src)
	draw.Draw(dst, b, src, b.Min, draw.Over)
	return dst
}

func isAnimatedGIF(data []byte) bool {
	g, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		return false
	}
	return len(g.Image) > 1
}

type optimizeOpts struct {
	maxWidth       int
	quality        int
	grayscale      bool
	skipImageFetch bool // skip downloading external images (e.g. markdown mode)
}

// optimizeImage returns the new data URI string and raw JPEG byte count,
// or empty string to signal "skip / pass through".
func optimizeImage(data []byte, mime string, opts optimizeOpts) (string, int) {
	// Pass through SVG
	if strings.Contains(mime, "svg") {
		return "", 0
	}
	// Pass through AVIF (no Go decoder; already well-compressed)
	if strings.Contains(mime, "avif") {
		return "", 0
	}
	// Pass through animated GIF
	if strings.Contains(mime, "gif") && isAnimatedGIF(data) {
		return "", 0
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		fmt.Fprintf(logOut, "Warning: could not decode image (%s): %v\n", mime, err)
		return "", 0
	}

	// Flatten alpha onto white for JPEG
	img = flattenAlpha(img)

	// Downscale by width only (never upscale)
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w > opts.maxWidth {
		ratio := float64(opts.maxWidth) / float64(w)
		newW := opts.maxWidth
		newH := int(math.Round(float64(h) * ratio))
		if newH < 1 {
			newH = 1
		}
		img = resize(img, newW, newH)
	}

	var encImg image.Image = img
	if opts.grayscale {
		encImg = toGrayscale(img)
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, encImg, &jpeg.Options{Quality: opts.quality}); err != nil {
		fmt.Fprintf(logOut, "Warning: JPEG encode failed: %v\n", err)
		return "", 0
	}

	encoded := base64.StdEncoding.EncodeToString(buf.Bytes())
	uri := "data:image/jpeg;base64," + encoded
	return uri, buf.Len()
}

var (
	// Matches <img ... src="data:mime;base64,DATA">
	dataURIRe = regexp.MustCompile(`(<img\b[^>]*?\bsrc\s*=\s*")data:([^;]+);base64,([^"]*)(")`)
	// Matches <picture>...</picture> (non-greedy across newlines)
	pictureRe = regexp.MustCompile(`(?s)<picture\b[^>]*>.*?</picture>`)
	// Extracts data URIs from srcset or src attributes inside <source>/<img> tags
	dataURIExtractRe = regexp.MustCompile(`data:([^;]+);base64,([^\s",]+)`)
	// Extracts external URLs from srcset attributes (e.g. "https://...jpg 640w, https://...jpg 1400w")
	extSrcsetURLRe = regexp.MustCompile(`(https?://[^\s",]+)(?:\s+\d+w)?`)
	// Extracts alt attribute
	altRe = regexp.MustCompile(`\balt\s*=\s*"([^"]*)"`)
	// Matches data-src or data-srcset on img tags (lazy loading)
	lazySrcRe    = regexp.MustCompile(`(<img\b[^>]*?)\bdata-src=`)
	lazySrcsetRe = regexp.MustCompile(`(<img\b[^>]*?)\bdata-srcset=`)
	// Matches an entire <img> tag that has data-src (lazy loading)
	lazyImgRe = regexp.MustCompile(`<img\b[^>]*\bdata-src\s*=[^>]*>`)
	// Matches src="data:image/svg+xml;base64,..." (placeholder) within an img tag
	svgSrcAttrRe = regexp.MustCompile(`\bsrc\s*=\s*"data:image/svg\+xml;base64,[^"]*"`)
)

// Matches <img ... src="https://..."> (external URL images)
var extImgRe = regexp.MustCompile(`(<img\b[^>]*?\bsrc\s*=\s*")(https?://[^"]+)(")`)

// getImageClient returns the HTTP client for fetching external images.
// When a proxy is configured, uses a standard-TLS proxy-aware client.
// Otherwise uses fetchImageClient (browser TLS fingerprint) when available,
// falling back to a plain client (for tests).
func getImageClient() *http.Client {
	if fetchProxyURL != "" {
		return newProxyClient(fetchProxyURL, 30*time.Second)
	}
	if fetchImageClient != nil {
		return fetchImageClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// promoteLazySrc rewrites data-src="..." to src="..." on img tags
// that use lazy loading, so downstream tools see the real image URLs.
// Also removes SVG placeholder src attrs to avoid duplicates.
func promoteLazySrc(html []byte) []byte {
	// Remove SVG placeholder src attrs on img tags that also have data-src.
	// WordPress et al. use src="data:image/svg+xml;base64,..." as a 1x1 pixel
	// placeholder alongside data-src="real-url". Promoting data-src would create
	// duplicate src attributes.
	html = lazyImgRe.ReplaceAllFunc(html, func(match []byte) []byte {
		return svgSrcAttrRe.ReplaceAll(match, nil)
	})
	html = lazySrcRe.ReplaceAll(html, []byte("${1}src="))
	html = lazySrcsetRe.ReplaceAll(html, []byte("${1}srcset="))
	return html
}

// fetchOneImage downloads a single external image URL and returns its data URI
// components, or empty strings on failure.
func fetchOneImage(imgURL string) (mime, encoded string) {
	// Unescape HTML entities in URL (e.g. &amp; -> &)
	imgURL = html.UnescapeString(imgURL)

	resp, err := getImageClient().Get(imgURL)
	if err != nil {
		fmt.Fprintf(logOut, "Warning: could not fetch %s: %v\n", imgURL, err)
		return "", ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		fmt.Fprintf(logOut, "Warning: HTTP %d for %s\n", resp.StatusCode, imgURL)
		return "", ""
	}

	data, err := readLimited(resp.Body, maxResponseBytes)
	if err != nil {
		fmt.Fprintf(logOut, "Warning: could not read %s: %v\n", imgURL, err)
		return "", ""
	}

	// Detect MIME from Content-Type header or sniff
	m := resp.Header.Get("Content-Type")
	if i := strings.Index(m, ";"); i >= 0 {
		m = m[:i]
	}
	m = strings.TrimSpace(m)
	if m == "" || m == "application/octet-stream" {
		m = http.DetectContentType(data)
		if i := strings.Index(m, ";"); i >= 0 {
			m = m[:i]
		}
	}

	return m, base64.StdEncoding.EncodeToString(data)
}

// fetchAndEmbed downloads external image URLs and embeds them as data URIs.
// concurrency controls how many images are fetched in parallel (min 1).
func fetchAndEmbed(html []byte, concurrency int) []byte {
	if concurrency < 1 {
		concurrency = 1
	}

	// Find all external image matches
	matches := extImgRe.FindAllSubmatchIndex(html, -1)
	if len(matches) == 0 {
		return html
	}

	// Fetch all images concurrently
	type fetchResult struct {
		mime    string
		encoded string
	}
	results := make([]fetchResult, len(matches))
	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)

	for i, m := range matches {
		imgURL := string(html[m[4]:m[5]]) // group 2: the URL
		wg.Add(1)
		go func(i int, imgURL string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			mime, encoded := fetchOneImage(imgURL)
			results[i] = fetchResult{mime: mime, encoded: encoded}
		}(i, imgURL)
	}
	wg.Wait()

	// Rebuild HTML with fetched results
	var out bytes.Buffer
	prev := 0
	fetched := 0
	for i, m := range matches {
		out.Write(html[prev:m[0]])
		if results[i].encoded != "" {
			// Write prefix (group 1)
			out.Write(html[m[2]:m[3]])
			out.WriteString("data:")
			out.WriteString(results[i].mime)
			out.WriteString(";base64,")
			out.WriteString(results[i].encoded)
			// Write suffix (group 3: closing quote)
			out.Write(html[m[6]:m[7]])
			fetched++
		} else {
			out.Write(html[m[0]:m[1]]) // keep original
		}
		prev = m[1]
	}
	out.Write(html[prev:])

	if fetched > 0 {
		fmt.Fprintf(logOut, "Fetched and embedded %d external images\n", fetched)
	}
	return out.Bytes()
}

type stats struct {
	count          int
	originalTotal  int64
	optimizedTotal int64
}

// decodeBase64 tries standard then raw (no-padding) base64.
func decodeBase64(s string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(s)
	}
	return raw, err
}

// tryOptimizeDataURI attempts to decode and optimize a single data URI.
// Returns the optimized JPEG data URI, or "" if it should be passed through.
func tryOptimizeDataURI(mime, b64data string, opts optimizeOpts, st *stats) string {
	raw, err := decodeBase64(b64data)
	if err != nil {
		fmt.Fprintf(logOut, "Warning: broken base64, skipping: %v\n", err)
		return ""
	}

	uri, jpegLen := optimizeImage(raw, mime, opts)
	if uri == "" {
		return ""
	}

	st.originalTotal += int64(len(raw))
	st.optimizedTotal += int64(jpegLen)
	st.count++
	return uri
}

// fetchImage downloads an image URL and returns its bytes and MIME type.
func fetchImage(imgURL string) ([]byte, string, error) {
	// Unescape HTML entities in URL (e.g. &amp; -> &)
	imgURL = html.UnescapeString(imgURL)

	resp, err := getImageClient().Get(imgURL)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	data, err := readLimited(resp.Body, maxResponseBytes)
	if err != nil {
		return nil, "", err
	}

	mime := resp.Header.Get("Content-Type")
	if i := strings.Index(mime, ";"); i >= 0 {
		mime = mime[:i]
	}
	mime = strings.TrimSpace(mime)
	if mime == "" || mime == "application/octet-stream" {
		mime = http.DetectContentType(data)
		if i := strings.Index(mime, ";"); i >= 0 {
			mime = mime[:i]
		}
	}
	return data, mime, nil
}

// pickBestSrcsetURL extracts URLs from a srcset attribute value and picks
// the largest one (by the "Nw" width descriptor). Prefers non-webp URLs
// when available at similar sizes.
func pickBestSrcsetURL(pictureHTML []byte) string {
	matches := extSrcsetURLRe.FindAllSubmatch(pictureHTML, -1)
	if len(matches) == 0 {
		return ""
	}

	// Collect unique URLs, preferring non-webp formats
	var bestURL string
	var bestWidth int
	for _, m := range matches {
		u := string(m[1])
		// Skip webp format URLs if we can (Medium provides both)
		if strings.Contains(u, "/format:webp/") {
			continue
		}
		// Parse width from "Nw" descriptor if present
		w := 0
		full := string(m[0])
		if idx := strings.LastIndex(full, " "); idx > 0 {
			fmt.Sscanf(full[idx+1:], "%dw", &w)
		}
		if w > bestWidth || bestURL == "" {
			bestURL = u
			bestWidth = w
		}
	}

	// If all URLs were webp, take the largest webp
	if bestURL == "" {
		for _, m := range matches {
			u := string(m[1])
			w := 0
			full := string(m[0])
			if idx := strings.LastIndex(full, " "); idx > 0 {
				fmt.Sscanf(full[idx+1:], "%dw", &w)
			}
			if w > bestWidth || bestURL == "" {
				bestURL = u
				bestWidth = w
			}
		}
	}

	return bestURL
}

// processArticleImages handles all image processing for article HTML:
// promotes lazy-loaded images, fetches external images, collapses <picture>
// elements, and optimizes all images for e-readers.
// concurrency controls how many external images are fetched in parallel.
func processArticleImages(html []byte, opts optimizeOpts, concurrency int) []byte {
	var st stats

	// Promote lazy-loaded images (data-src → src)
	html = promoteLazySrc(html)

	// Fetch external image URLs and embed as data URIs.
	// Skipped in markdown mode: images stay as external URLs there.
	if !opts.skipImageFetch {
		html = fetchAndEmbed(html, concurrency)
	}

	// Collapse <picture> elements into single <img> tags
	html = pictureRe.ReplaceAllFunc(html, func(match []byte) []byte {
		alt := ""
		if m := altRe.FindSubmatch(match); m != nil {
			alt = string(m[1])
		}

		// First try: data URIs already embedded
		uris := dataURIExtractRe.FindAllSubmatch(match, -1)
		if len(uris) > 0 {
			for _, u := range uris {
				mime := string(u[1])
				b64 := string(u[2])
				uri := tryOptimizeDataURI(mime, b64, opts, &st)
				if uri != "" {
					return []byte(fmt.Sprintf(`<img src="%s" alt="%s">`, uri, alt))
				}
			}

			// None were optimizable — keep first source as img
			for _, u := range uris {
				mime := string(u[1])
				b64 := string(u[2])
				raw, err := decodeBase64(b64)
				if err != nil {
					continue
				}
				encoded := base64.StdEncoding.EncodeToString(raw)
				return []byte(fmt.Sprintf(`<img src="data:%s;base64,%s" alt="%s">`, mime, encoded, alt))
			}
		}

		// Second try: external URLs in srcset (e.g. Medium).
		// Skip when image fetching is disabled (markdown mode).
		if !opts.skipImageFetch {
			imgURL := pickBestSrcsetURL(match)
			if imgURL != "" {
				data, mime, err := fetchImage(imgURL)
				if err != nil {
					fmt.Fprintf(logOut, "Warning: could not fetch picture image %s: %v\n", imgURL, err)
					return match
				}

				uri, jpegLen := optimizeImage(data, mime, opts)
				if uri != "" {
					st.originalTotal += int64(len(data))
					st.optimizedTotal += int64(jpegLen)
					st.count++
					return []byte(fmt.Sprintf(`<img src="%s" alt="%s">`, uri, alt))
				}

				// Can't optimize (SVG/AVIF) — embed as-is
				encoded := base64.StdEncoding.EncodeToString(data)
				return []byte(fmt.Sprintf(`<img src="data:%s;base64,%s" alt="%s">`, mime, encoded, alt))
			}
		}

		return match
	})

	// Optimize standalone <img src="data:..."> (not inside <picture>)
	html = dataURIRe.ReplaceAllFunc(html, func(match []byte) []byte {
		parts := dataURIRe.FindSubmatch(match)
		if parts == nil {
			return match
		}
		prefix := parts[1]
		mime := string(parts[2])
		b64data := string(parts[3])
		suffix := parts[4]

		uri := tryOptimizeDataURI(mime, b64data, opts, &st)
		if uri == "" {
			return match
		}

		var out bytes.Buffer
		out.Write(prefix)
		out.WriteString(uri)
		out.Write(suffix)
		return out.Bytes()
	})

	if st.count > 0 {
		fmt.Fprintf(logOut, "Optimized %d images: %s → %s\n",
			st.count, humanSize(st.originalTotal), humanSize(st.optimizedTotal))
	} else {
		fmt.Fprintln(logOut, "No optimizable images found.")
	}

	// Cleanup for epub validity
	html = cleanForEpub(html)

	return html
}

// cleanForEpub is a no-op retained for pipeline compatibility.
// All HTML cleanup concerns (AVIF images, external srcset, data-* attributes,
// inline SVG) are now handled authoritatively by sanitizeForXHTML during
// EPUB generation. See sanitize.go.
func cleanForEpub(html []byte) []byte {
	return html
}
