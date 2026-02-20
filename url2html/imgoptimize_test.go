package main

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

// makePNG creates a solid-color PNG image at the given dimensions.
func makePNG(w, h int, c color.Color) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

// makeJPEG creates a solid-color JPEG image at the given dimensions.
func makeJPEG(w, h int, c color.Color) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90})
	return buf.Bytes()
}

func dataURI(mime string, data []byte) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}

func decodeJPEGDimensions(data []byte) (w, h int) {
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return 0, 0
	}
	b := img.Bounds()
	return b.Dx(), b.Dy()
}

func TestOptimizeImage_MaxWidthOnly(t *testing.T) {
	opts := optimizeOpts{maxWidth: 800, quality: 60, grayscale: false}

	// Wide image: 1200x900 should be scaled to 800x600
	wide := makePNG(1200, 900, color.NRGBA{255, 0, 0, 255})
	uri, _ := optimizeImage(wide, "image/png", opts)
	if uri == "" {
		t.Fatal("expected optimized URI for wide image")
	}
	b64 := strings.TrimPrefix(uri, "data:image/jpeg;base64,")
	raw, _ := base64.StdEncoding.DecodeString(b64)
	w, h := decodeJPEGDimensions(raw)
	if w != 800 || h != 600 {
		t.Errorf("wide image: got %dx%d, want 800x600", w, h)
	}

	// Tall narrow image: 400x1200 should NOT be resized (width < max)
	tall := makePNG(400, 1200, color.NRGBA{0, 255, 0, 255})
	uri, _ = optimizeImage(tall, "image/png", opts)
	if uri == "" {
		t.Fatal("expected optimized URI for tall image")
	}
	b64 = strings.TrimPrefix(uri, "data:image/jpeg;base64,")
	raw, _ = base64.StdEncoding.DecodeString(b64)
	w, h = decodeJPEGDimensions(raw)
	if w != 400 || h != 1200 {
		t.Errorf("tall image: got %dx%d, want 400x1200", w, h)
	}

	// Small image: 200x150 should NOT be resized
	small := makePNG(200, 150, color.NRGBA{0, 0, 255, 255})
	uri, _ = optimizeImage(small, "image/png", opts)
	if uri == "" {
		t.Fatal("expected optimized URI for small image")
	}
	b64 = strings.TrimPrefix(uri, "data:image/jpeg;base64,")
	raw, _ = base64.StdEncoding.DecodeString(b64)
	w, h = decodeJPEGDimensions(raw)
	if w != 200 || h != 150 {
		t.Errorf("small image: got %dx%d, want 200x150", w, h)
	}
}

func TestOptimizeImage_Grayscale(t *testing.T) {
	opts := optimizeOpts{maxWidth: 800, quality: 60, grayscale: true}
	data := makePNG(100, 100, color.NRGBA{255, 0, 0, 255})
	uri, _ := optimizeImage(data, "image/png", opts)
	if uri == "" {
		t.Fatal("expected optimized URI")
	}
	if !strings.HasPrefix(uri, "data:image/jpeg;base64,") {
		t.Error("expected JPEG data URI")
	}
}

func TestOptimizeImage_PassthroughSVG(t *testing.T) {
	uri, _ := optimizeImage([]byte("<svg></svg>"), "image/svg+xml", optimizeOpts{maxWidth: 800, quality: 60})
	if uri != "" {
		t.Error("SVG should be passed through (empty URI)")
	}
}

func TestOptimizeImage_PassthroughAVIF(t *testing.T) {
	uri, _ := optimizeImage([]byte{0x00}, "image/avif", optimizeOpts{maxWidth: 800, quality: 60})
	if uri != "" {
		t.Error("AVIF should be passed through (empty URI)")
	}
}

func TestProcessArticleImages_StandaloneImg(t *testing.T) {
	imgData := makePNG(1200, 900, color.NRGBA{255, 0, 0, 255})
	html := `<html><body><img src="` + dataURI("image/png", imgData) + `" alt="test"></body></html>`

	opts := optimizeOpts{maxWidth: 800, quality: 60, grayscale: false}
	result := processArticleImages([]byte(html), opts)

	if !strings.Contains(string(result), "data:image/jpeg;base64,") {
		t.Error("expected JPEG data URI in output")
	}
	if strings.Contains(string(result), "data:image/png;base64,") {
		t.Error("PNG should have been replaced with JPEG")
	}
}

func TestProcessArticleImages_PictureElement(t *testing.T) {
	imgData := makeJPEG(1200, 900, color.NRGBA{255, 0, 0, 255})
	uri := dataURI("image/jpeg", imgData)

	html := `<html><body><picture>` +
		`<source srcset="` + uri + ` 640w, ` + uri + ` 1200w" sizes="100vw" type="image/jpeg">` +
		`<img alt="hero image">` +
		`</picture></body></html>`

	opts := optimizeOpts{maxWidth: 800, quality: 60, grayscale: false}
	result := string(processArticleImages([]byte(html), opts))

	if strings.Contains(result, "<picture") {
		t.Error("expected <picture> to be collapsed")
	}
	if strings.Contains(result, "<source") {
		t.Error("expected <source> to be removed")
	}
	if !strings.Contains(result, `<img src="data:image/jpeg;base64,`) {
		t.Error("expected optimized <img> in output")
	}
	if !strings.Contains(result, `alt="hero image"`) {
		t.Error("expected alt text to be preserved")
	}
}

func TestProcessArticleImages_SVGPassthrough(t *testing.T) {
	svgData := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><circle r="10"/></svg>`)
	uri := dataURI("image/svg+xml", svgData)
	html := `<img src="` + uri + `">`

	opts := optimizeOpts{maxWidth: 800, quality: 60}
	result := string(processArticleImages([]byte(html), opts))

	if !strings.Contains(result, "image/svg+xml") {
		t.Error("SVG data URI should be preserved")
	}
}

func TestPickBestSrcsetURL(t *testing.T) {
	// Medium-style picture element with webp and jpeg sources
	medium := []byte(`<picture>
		<source srcSet="https://miro.medium.com/v2/resize:fit:640/format:webp/1*abc.jpeg 640w, https://miro.medium.com/v2/resize:fit:1400/format:webp/1*abc.jpeg 1400w" type="image/webp"/>
		<source srcSet="https://miro.medium.com/v2/resize:fit:640/1*abc.jpeg 640w, https://miro.medium.com/v2/resize:fit:1400/1*abc.jpeg 1400w"/>
		<img alt="" width="700" height="382"/>
	</picture>`)

	url := pickBestSrcsetURL(medium)
	if url == "" {
		t.Fatal("expected URL from Medium picture element")
	}
	// Should prefer non-webp URL
	if strings.Contains(url, "format:webp") {
		t.Errorf("should prefer non-webp, got: %s", url)
	}
	// Should pick the largest (1400w)
	if !strings.Contains(url, "fit:1400") {
		t.Errorf("should pick largest variant, got: %s", url)
	}
}

func TestPickBestSrcsetURL_WebpOnly(t *testing.T) {
	webpOnly := []byte(`<picture>
		<source srcSet="https://example.com/img.webp 640w, https://example.com/img-lg.webp 1200w" type="image/webp"/>
		<img alt=""/>
	</picture>`)

	url := pickBestSrcsetURL(webpOnly)
	if url == "" {
		t.Fatal("expected URL even when only webp available")
	}
	if !strings.Contains(url, "img-lg.webp") {
		t.Errorf("should pick largest webp, got: %s", url)
	}
}

func TestPickBestSrcsetURL_NoURLs(t *testing.T) {
	empty := []byte(`<picture><img alt=""/></picture>`)
	url := pickBestSrcsetURL(empty)
	if url != "" {
		t.Errorf("expected empty for picture with no srcset URLs, got: %s", url)
	}
}

func TestHumanSize(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0.0B"},
		{1023, "1023.0B"},
		{1024, "1.0KB"},
		{1048576, "1.0MB"},
		{1073741824, "1.0GB"},
	}
	for _, tt := range tests {
		got := humanSize(tt.input)
		if got != tt.want {
			t.Errorf("humanSize(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
