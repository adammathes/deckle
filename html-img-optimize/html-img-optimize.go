// html-img-optimize: Optimize base64-embedded images in HTML for e-readers.
// Build: go build -o html-img-optimize
package main

import (
	"bytes"
	"encoding/base64"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

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

// lanczosResize downscales an image using Lanczos-like resampling via
// a simple area-average approach. Good enough for downscaling.
func areaResize(src image.Image, dstW, dstH int) *image.NRGBA {
	srcB := src.Bounds()
	srcW := srcB.Dx()
	srcH := srcB.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, dstW, dstH))

	xRatio := float64(srcW) / float64(dstW)
	yRatio := float64(srcH) / float64(dstH)

	for dy := 0; dy < dstH; dy++ {
		sy0 := float64(dy) * yRatio
		sy1 := float64(dy+1) * yRatio
		for dx := 0; dx < dstW; dx++ {
			sx0 := float64(dx) * xRatio
			sx1 := float64(dx+1) * xRatio

			var rSum, gSum, bSum, aSum, area float64
			for sy := int(sy0); sy < int(math.Ceil(sy1)); sy++ {
				if sy >= srcH {
					break
				}
				// vertical coverage
				fy0 := math.Max(float64(sy), sy0)
				fy1 := math.Min(float64(sy+1), sy1)
				yCov := fy1 - fy0

				for sx := int(sx0); sx < int(math.Ceil(sx1)); sx++ {
					if sx >= srcW {
						break
					}
					fx0 := math.Max(float64(sx), sx0)
					fx1 := math.Min(float64(sx+1), sx1)
					xCov := fx1 - fx0
					w := xCov * yCov

					r, g, b, a := src.At(srcB.Min.X+sx, srcB.Min.Y+sy).RGBA()
					rSum += float64(r) * w
					gSum += float64(g) * w
					bSum += float64(b) * w
					aSum += float64(a) * w
					area += w
				}
			}
			if area > 0 {
				dst.SetNRGBA(dx, dy, color.NRGBA{
					R: uint8(rSum / area / 257),
					G: uint8(gSum / area / 257),
					B: uint8(bSum / area / 257),
					A: uint8(aSum / area / 257),
				})
			}
		}
	}
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
	maxWidth  int
	quality   int
	grayscale bool
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
		fmt.Fprintf(os.Stderr, "Warning: could not decode image (%s): %v\n", mime, err)
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
		img = areaResize(img, newW, newH)
	}

	var encImg image.Image = img
	if opts.grayscale {
		encImg = toGrayscale(img)
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, encImg, &jpeg.Options{Quality: opts.quality}); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: JPEG encode failed: %v\n", err)
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
	// Extracts alt attribute
	altRe = regexp.MustCompile(`\balt\s*=\s*"([^"]*)"`)
	// Matches data-src or data-srcset on img tags (lazy loading)
	lazySrcRe    = regexp.MustCompile(`(<img\b[^>]*?)\bdata-src=`)
	lazySrcsetRe = regexp.MustCompile(`(<img\b[^>]*?)\bdata-srcset=`)
)

// Matches <img ... src="https://..."> (external URL images)
var extImgRe = regexp.MustCompile(`(<img\b[^>]*?\bsrc\s*=\s*")(https?://[^"]+)(")`)

var httpClient = &http.Client{Timeout: 30 * time.Second}

// promoteLazySrc rewrites data-src="..." to src="..." on img tags
// that use lazy loading, so downstream tools see the real image URLs.
func promoteLazySrc(html []byte) []byte {
	html = lazySrcRe.ReplaceAll(html, []byte("${1}src="))
	html = lazySrcsetRe.ReplaceAll(html, []byte("${1}srcset="))
	return html
}

// fetchAndEmbed downloads external image URLs and embeds them as data URIs.
func fetchAndEmbed(html []byte) []byte {
	var fetched int
	html = extImgRe.ReplaceAllFunc(html, func(match []byte) []byte {
		parts := extImgRe.FindSubmatch(match)
		if parts == nil {
			return match
		}
		prefix := parts[1]
		url := string(parts[2])
		suffix := parts[3]

		resp, err := httpClient.Get(url)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not fetch %s: %v\n", url, err)
			return match
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			fmt.Fprintf(os.Stderr, "Warning: HTTP %d for %s\n", resp.StatusCode, url)
			return match
		}

		data, err := io.ReadAll(resp.Body)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not read %s: %v\n", url, err)
			return match
		}

		// Detect MIME from Content-Type header or sniff
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

		encoded := base64.StdEncoding.EncodeToString(data)
		fetched++

		var out bytes.Buffer
		out.Write(prefix)
		out.WriteString("data:")
		out.WriteString(mime)
		out.WriteString(";base64,")
		out.WriteString(encoded)
		out.Write(suffix)
		return out.Bytes()
	})

	if fetched > 0 {
		fmt.Fprintf(os.Stderr, "Fetched and embedded %d external images\n", fetched)
	}
	return html
}

type stats struct {
	count         int
	originalTotal int64
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
		fmt.Fprintf(os.Stderr, "Warning: broken base64, skipping: %v\n", err)
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

func processHTML(html []byte, opts optimizeOpts) []byte {
	var st stats

	// Phase 0: Promote lazy-loaded images (data-src → src) and
	// fetch any external image URLs, embedding them as data URIs.
	html = promoteLazySrc(html)
	html = fetchAndEmbed(html)

	// Phase 1: Collapse <picture> elements.
	// Each <picture> may contain multiple <source srcset="data:..."> with
	// responsive variants. We pick the first decodable/optimizable image
	// and replace the entire <picture> with a single <img>.
	html = pictureRe.ReplaceAllFunc(html, func(match []byte) []byte {
		// Extract alt text from any <img> inside the picture
		alt := ""
		if m := altRe.FindSubmatch(match); m != nil {
			alt = string(m[1])
		}

		// Find all data URIs in the picture element
		uris := dataURIExtractRe.FindAllSubmatch(match, -1)
		if len(uris) == 0 {
			return match
		}

		// Try each data URI until one can be optimized
		for _, u := range uris {
			mime := string(u[1])
			b64 := string(u[2])
			uri := tryOptimizeDataURI(mime, b64, opts, &st)
			if uri != "" {
				return []byte(fmt.Sprintf(`<img src="%s" alt="%s">`, uri, alt))
			}
		}

		// None were optimizable (all SVG/AVIF/animated GIF) — keep first source as img
		// Try to find any decodable image to at least keep
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

		return match
	})

	// Phase 2: Handle standalone <img src="data:..."> (not inside <picture>)
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
		fmt.Fprintf(os.Stderr, "Optimized %d images: %s → %s\n",
			st.count, humanSize(st.originalTotal), humanSize(st.optimizedTotal))
	} else {
		fmt.Fprintln(os.Stderr, "No optimizable images found.")
	}

	return html
}

func main() {
	maxWidth := flag.Int("max-width", 800, "Max pixel width (height scales proportionally)")
	quality := flag.Int("quality", 60, "JPEG quality 1-95")
	grayscale := flag.Bool("grayscale", false, "Convert to grayscale")
	output := flag.String("o", "", "Output file (default: stdout)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: html-img-optimize [options] [input.html]\n\n")
		fmt.Fprintf(os.Stderr, "Optimize base64-embedded images in HTML for e-readers.\n")
		fmt.Fprintf(os.Stderr, "Reads from stdin if no file argument given.\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	var input []byte
	var err error

	if flag.NArg() > 0 {
		input, err = os.ReadFile(flag.Arg(0))
	} else {
		input, err = io.ReadAll(os.Stdin)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading input: %v\n", err)
		os.Exit(1)
	}

	opts := optimizeOpts{
		maxWidth:  *maxWidth,
		quality:   *quality,
		grayscale: *grayscale,
	}

	result := processHTML(input, opts)

	if *output != "" {
		if err := os.WriteFile(*output, result, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing output: %v\n", err)
			os.Exit(1)
		}
	} else {
		os.Stdout.Write(result)
	}
}
