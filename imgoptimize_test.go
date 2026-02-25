package main

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
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
	result := processArticleImages([]byte(html), opts, 5)

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
	result := string(processArticleImages([]byte(html), opts, 5))

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
	result := string(processArticleImages([]byte(html), opts, 5))

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
		{1099511627776, "1.0TB"},
		{1125899906842624, "1.0TB"},  // exactly 1 PB - overflows to final return
	}
	for _, tt := range tests {
		got := humanSize(tt.input)
		if got != tt.want {
			t.Errorf("humanSize(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsAnimatedGIF_Static(t *testing.T) {
	// Create a single-frame GIF
	img := image.NewPaletted(image.Rect(0, 0, 2, 2), color.Palette{color.White, color.Black})
	var buf bytes.Buffer
	gif.Encode(&buf, img, nil)
	if isAnimatedGIF(buf.Bytes()) {
		t.Error("single-frame GIF should not be animated")
	}
}

func TestIsAnimatedGIF_Animated(t *testing.T) {
	// Create a multi-frame GIF
	palette := color.Palette{color.White, color.Black}
	g := &gif.GIF{
		Image: []*image.Paletted{
			image.NewPaletted(image.Rect(0, 0, 2, 2), palette),
			image.NewPaletted(image.Rect(0, 0, 2, 2), palette),
		},
		Delay: []int{10, 10},
	}
	var buf bytes.Buffer
	gif.EncodeAll(&buf, g)
	if !isAnimatedGIF(buf.Bytes()) {
		t.Error("multi-frame GIF should be animated")
	}
}

func TestIsAnimatedGIF_InvalidData(t *testing.T) {
	if isAnimatedGIF([]byte("not a gif")) {
		t.Error("invalid data should return false")
	}
}

func TestOptimizeImage_AnimatedGIF(t *testing.T) {
	palette := color.Palette{color.White, color.Black}
	g := &gif.GIF{
		Image: []*image.Paletted{
			image.NewPaletted(image.Rect(0, 0, 2, 2), palette),
			image.NewPaletted(image.Rect(0, 0, 2, 2), palette),
		},
		Delay: []int{10, 10},
	}
	var buf bytes.Buffer
	gif.EncodeAll(&buf, g)
	uri, _ := optimizeImage(buf.Bytes(), "image/gif", optimizeOpts{maxWidth: 800, quality: 60})
	if uri != "" {
		t.Error("animated GIF should be passed through (empty URI)")
	}
}

func TestOptimizeImage_StaticGIF(t *testing.T) {
	// A static GIF should be optimized (converted to JPEG)
	img := image.NewPaletted(image.Rect(0, 0, 100, 100), color.Palette{color.White, color.Black})
	var buf bytes.Buffer
	gif.Encode(&buf, img, nil)
	uri, _ := optimizeImage(buf.Bytes(), "image/gif", optimizeOpts{maxWidth: 800, quality: 60})
	if uri == "" {
		t.Error("static GIF should be optimized")
	}
	if !strings.HasPrefix(uri, "data:image/jpeg;base64,") {
		t.Error("optimized GIF should produce JPEG data URI")
	}
}

func TestOptimizeImage_InvalidData(t *testing.T) {
	uri, n := optimizeImage([]byte("not an image"), "image/png", optimizeOpts{maxWidth: 800, quality: 60})
	if uri != "" {
		t.Error("invalid image data should return empty URI")
	}
	if n != 0 {
		t.Error("invalid image data should return 0 byte count")
	}
}

func TestDecodeBase64_Standard(t *testing.T) {
	original := []byte("hello world")
	encoded := base64.StdEncoding.EncodeToString(original)
	decoded, err := decodeBase64(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded) != "hello world" {
		t.Errorf("got %q, want %q", string(decoded), "hello world")
	}
}

func TestDecodeBase64_RawNoPadding(t *testing.T) {
	original := []byte("hello world")
	encoded := base64.RawStdEncoding.EncodeToString(original)
	decoded, err := decodeBase64(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded) != "hello world" {
		t.Errorf("got %q, want %q", string(decoded), "hello world")
	}
}

func TestDecodeBase64_Invalid(t *testing.T) {
	_, err := decodeBase64("!!!not-base64!!!")
	if err == nil {
		t.Error("expected error for invalid base64")
	}
}

func TestTryOptimizeDataURI(t *testing.T) {
	imgData := makePNG(100, 100, color.NRGBA{255, 0, 0, 255})
	b64 := base64.StdEncoding.EncodeToString(imgData)
	opts := optimizeOpts{maxWidth: 800, quality: 60}
	var st stats
	uri := tryOptimizeDataURI("image/png", b64, opts, &st)
	if uri == "" {
		t.Fatal("expected optimized URI")
	}
	if !strings.HasPrefix(uri, "data:image/jpeg;base64,") {
		t.Error("expected JPEG data URI")
	}
	if st.count != 1 {
		t.Errorf("expected count=1, got %d", st.count)
	}
	if st.originalTotal == 0 {
		t.Error("expected non-zero originalTotal")
	}
	if st.optimizedTotal == 0 {
		t.Error("expected non-zero optimizedTotal")
	}
}

func TestTryOptimizeDataURI_SVGPassthrough(t *testing.T) {
	b64 := base64.StdEncoding.EncodeToString([]byte("<svg></svg>"))
	opts := optimizeOpts{maxWidth: 800, quality: 60}
	var st stats
	uri := tryOptimizeDataURI("image/svg+xml", b64, opts, &st)
	if uri != "" {
		t.Error("SVG should be passed through (empty URI)")
	}
	if st.count != 0 {
		t.Error("SVG passthrough should not increment count")
	}
}

func TestTryOptimizeDataURI_InvalidBase64(t *testing.T) {
	opts := optimizeOpts{maxWidth: 800, quality: 60}
	var st stats
	uri := tryOptimizeDataURI("image/png", "!!!invalid!!!", opts, &st)
	if uri != "" {
		t.Error("invalid base64 should return empty URI")
	}
}

func TestGetImageClient_Fallback(t *testing.T) {
	// Save and clear the global client
	saved := fetchImageClient
	fetchImageClient = nil
	defer func() { fetchImageClient = saved }()

	client := getImageClient()
	if client == nil {
		t.Fatal("expected non-nil fallback client")
	}
	if client.Timeout == 0 {
		t.Error("expected fallback client to have a timeout")
	}
}

func TestGetImageClient_UsesGlobal(t *testing.T) {
	// fetchImageClient is set by init()
	client := getImageClient()
	if client != fetchImageClient {
		t.Error("expected getImageClient to return fetchImageClient when set")
	}
}

func TestFetchAndEmbed_Success(t *testing.T) {
	imgData := makePNG(10, 10, color.NRGBA{255, 0, 0, 255})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(imgData)
	}))
	defer srv.Close()

	// Replace global client for test
	saved := fetchImageClient
	fetchImageClient = srv.Client()
	defer func() { fetchImageClient = saved }()

	html := []byte(`<img src="` + srv.URL + `/img.png" alt="test">`)
	result := fetchAndEmbed(html, 5)

	if !strings.Contains(string(result), "data:image/png;base64,") {
		t.Error("expected data URI in output")
	}
	if strings.Contains(string(result), "http://") {
		t.Error("external URL should be replaced with data URI")
	}
}

func TestFetchAndEmbed_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	saved := fetchImageClient
	fetchImageClient = srv.Client()
	defer func() { fetchImageClient = saved }()

	html := []byte(`<img src="` + srv.URL + `/missing.png" alt="test">`)
	result := fetchAndEmbed(html, 5)

	// Should keep original URL on failure
	if !strings.Contains(string(result), srv.URL) {
		t.Error("expected original URL preserved on 404")
	}
}

func TestFetchAndEmbed_NoExternalImages(t *testing.T) {
	html := []byte(`<img src="data:image/png;base64,abc" alt="test">`)
	result := fetchAndEmbed(html, 5)
	if string(result) != string(html) {
		t.Error("data URI images should be left unchanged")
	}
}

func TestFetchAndEmbed_MIMESniffing(t *testing.T) {
	imgData := makeJPEG(10, 10, color.NRGBA{0, 0, 255, 255})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Send with generic Content-Type to trigger sniffing
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(imgData)
	}))
	defer srv.Close()

	saved := fetchImageClient
	fetchImageClient = srv.Client()
	defer func() { fetchImageClient = saved }()

	html := []byte(`<img src="` + srv.URL + `/img.bin" alt="test">`)
	result := fetchAndEmbed(html, 5)

	if !strings.Contains(string(result), "data:image/jpeg;base64,") {
		t.Error("expected MIME to be sniffed as JPEG")
	}
}

func TestFetchImage_Success(t *testing.T) {
	imgData := makePNG(10, 10, color.NRGBA{255, 0, 0, 255})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(imgData)
	}))
	defer srv.Close()

	saved := fetchImageClient
	fetchImageClient = srv.Client()
	defer func() { fetchImageClient = saved }()

	data, mime, err := fetchImage(srv.URL + "/img.png")
	if err != nil {
		t.Fatal(err)
	}
	if mime != "image/png" {
		t.Errorf("mime = %q, want image/png", mime)
	}
	if len(data) == 0 {
		t.Error("expected non-empty data")
	}
}

func TestFetchImage_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	saved := fetchImageClient
	fetchImageClient = srv.Client()
	defer func() { fetchImageClient = saved }()

	_, _, err := fetchImage(srv.URL + "/missing.png")
	if err == nil {
		t.Error("expected error for 404")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("expected 404 in error, got: %v", err)
	}
}

func TestFetchImage_MIMESniff(t *testing.T) {
	imgData := makeJPEG(10, 10, color.NRGBA{0, 0, 255, 255})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "")
		w.Write(imgData)
	}))
	defer srv.Close()

	saved := fetchImageClient
	fetchImageClient = srv.Client()
	defer func() { fetchImageClient = saved }()

	_, mime, err := fetchImage(srv.URL + "/img")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mime, "jpeg") {
		t.Errorf("expected sniffed JPEG mime, got %q", mime)
	}
}

func TestFetchImage_ContentTypeWithCharset(t *testing.T) {
	imgData := makePNG(10, 10, color.NRGBA{0, 255, 0, 255})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png; charset=utf-8")
		w.Write(imgData)
	}))
	defer srv.Close()

	saved := fetchImageClient
	fetchImageClient = srv.Client()
	defer func() { fetchImageClient = saved }()

	_, mime, err := fetchImage(srv.URL + "/img.png")
	if err != nil {
		t.Fatal(err)
	}
	if mime != "image/png" {
		t.Errorf("mime = %q, want image/png (charset should be stripped)", mime)
	}
}

func TestPromoteLazySrc_BasicDataSrc(t *testing.T) {
	html := []byte(`<img class="lazy" data-src="https://example.com/img.jpg" alt="test">`)
	result := promoteLazySrc(html)
	if strings.Contains(string(result), "data-src=") {
		t.Error("data-src should be promoted to src")
	}
	if !strings.Contains(string(result), `src="https://example.com/img.jpg"`) {
		t.Error("expected src with data-src URL")
	}
}

func TestPromoteLazySrc_SVGPlaceholder(t *testing.T) {
	// WordPress-style: SVG placeholder in src + real URL in data-src
	html := []byte(`<img src="data:image/svg+xml;base64,PHN2Zz4=" data-src="https://example.com/real.jpg" alt="test">`)
	result := promoteLazySrc(html)
	if strings.Contains(string(result), "svg+xml") {
		t.Error("SVG placeholder should be removed")
	}
	if !strings.Contains(string(result), `src="https://example.com/real.jpg"`) {
		t.Error("expected promoted data-src URL")
	}
}

func TestPromoteLazySrc_DataSrcset(t *testing.T) {
	html := []byte(`<img data-srcset="https://example.com/img.jpg 640w" alt="test">`)
	result := promoteLazySrc(html)
	if strings.Contains(string(result), "data-srcset=") {
		t.Error("data-srcset should be promoted to srcset")
	}
	if !strings.Contains(string(result), `srcset="https://example.com/img.jpg 640w"`) {
		t.Error("expected srcset with data-srcset value")
	}
}

func TestPickBestSrcsetURL_SingleURL(t *testing.T) {
	html := []byte(`<source srcset="https://example.com/only.jpg">`)
	u := pickBestSrcsetURL(html)
	if u == "" {
		t.Fatal("expected URL from single srcset entry")
	}
	if !strings.Contains(u, "only.jpg") {
		t.Errorf("expected only.jpg, got: %s", u)
	}
}

func TestProcessArticleImages_PictureWithNonOptimizableDataURI(t *testing.T) {
	// Picture with SVG data URI that can't be optimized
	svgData := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect width="10" height="10"/></svg>`)
	b64 := base64.StdEncoding.EncodeToString(svgData)

	html := `<picture>` +
		`<source srcset="data:image/svg+xml;base64,` + b64 + `">` +
		`<img alt="svg pic">` +
		`</picture>`
	opts := optimizeOpts{maxWidth: 800, quality: 60}
	result := string(processArticleImages([]byte(html), opts, 5))

	if strings.Contains(result, "<picture") {
		t.Error("picture should be collapsed")
	}
	// SVG can't be optimized, so it should be kept as-is data URI
	if !strings.Contains(result, "image/svg+xml") {
		t.Error("SVG data URI should be preserved in fallback path")
	}
}

func TestProcessArticleImages_LazyLoadAndExternal(t *testing.T) {
	imgData := makePNG(50, 50, color.NRGBA{0, 255, 0, 255})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(imgData)
	}))
	defer srv.Close()

	saved := fetchImageClient
	fetchImageClient = srv.Client()
	defer func() { fetchImageClient = saved }()

	html := `<img data-src="` + srv.URL + `/lazy.png" alt="lazy">`
	opts := optimizeOpts{maxWidth: 800, quality: 60}
	result := string(processArticleImages([]byte(html), opts, 5))

	if strings.Contains(result, "data-src=") {
		t.Error("data-src should be promoted")
	}
	if !strings.Contains(result, "data:image/jpeg;base64,") {
		t.Error("lazy-loaded external image should be fetched and embedded")
	}
}

func TestProcessArticleImages_ExternalImages(t *testing.T) {
	imgData := makePNG(100, 100, color.NRGBA{255, 0, 0, 255})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(imgData)
	}))
	defer srv.Close()

	saved := fetchImageClient
	fetchImageClient = srv.Client()
	defer func() { fetchImageClient = saved }()

	html := `<html><body><img src="` + srv.URL + `/img.png" alt="test"></body></html>`
	opts := optimizeOpts{maxWidth: 800, quality: 60}
	result := processArticleImages([]byte(html), opts, 5)

	if strings.Contains(string(result), "http://") {
		t.Error("external URLs should be embedded as data URIs")
	}
	if !strings.Contains(string(result), "data:image/jpeg;base64,") {
		t.Error("expected embedded JPEG data URI")
	}
}

func TestProcessArticleImages_PictureWithExternalSrcset(t *testing.T) {
	imgData := makeJPEG(200, 150, color.NRGBA{0, 100, 200, 255})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(imgData)
	}))
	defer srv.Close()

	saved := fetchImageClient
	fetchImageClient = srv.Client()
	defer func() { fetchImageClient = saved }()

	html := `<picture>` +
		`<source srcset="` + srv.URL + `/img-sm.jpg 640w, ` + srv.URL + `/img-lg.jpg 1400w">` +
		`<img alt="hero">` +
		`</picture>`
	opts := optimizeOpts{maxWidth: 800, quality: 60}
	result := string(processArticleImages([]byte(html), opts, 5))

	if strings.Contains(result, "<picture") {
		t.Error("picture element should be collapsed")
	}
	if !strings.Contains(result, `<img src="data:image/jpeg;base64,`) {
		t.Error("expected embedded JPEG in collapsed img")
	}
	if !strings.Contains(result, `alt="hero"`) {
		t.Error("alt text should be preserved")
	}
}

func TestProcessArticleImages_NoImages(t *testing.T) {
	html := `<p>No images here.</p>`
	opts := optimizeOpts{maxWidth: 800, quality: 60}
	result := processArticleImages([]byte(html), opts, 5)
	if !strings.Contains(string(result), "No images here.") {
		t.Error("text content should be preserved")
	}
}

// --- size limit tests for image fetching ---

func TestFetchOneImage_ExceedsSizeLimit(t *testing.T) {
	saved := maxResponseBytes
	defer func() { maxResponseBytes = saved }()
	maxResponseBytes = 50 // very small limit

	// Server sends a large image
	imgData := makePNG(200, 200, color.NRGBA{255, 0, 0, 255})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(imgData)
	}))
	defer srv.Close()

	savedClient := fetchImageClient
	fetchImageClient = srv.Client()
	defer func() { fetchImageClient = savedClient }()

	mime, encoded := fetchOneImage(srv.URL + "/big.png")
	if mime != "" || encoded != "" {
		t.Error("expected empty result when image exceeds size limit")
	}
}

func TestFetchImage_ExceedsSizeLimit(t *testing.T) {
	saved := maxResponseBytes
	defer func() { maxResponseBytes = saved }()
	maxResponseBytes = 50

	imgData := makePNG(200, 200, color.NRGBA{0, 255, 0, 255})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(imgData)
	}))
	defer srv.Close()

	savedClient := fetchImageClient
	fetchImageClient = srv.Client()
	defer func() { fetchImageClient = savedClient }()

	_, _, err := fetchImage(srv.URL + "/big.png")
	if err == nil {
		t.Fatal("expected error when image exceeds size limit")
	}
	if !strings.Contains(err.Error(), "exceeds maximum allowed size") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestFetchAndEmbed_ExceedsSizeLimit(t *testing.T) {
	saved := maxResponseBytes
	defer func() { maxResponseBytes = saved }()
	maxResponseBytes = 50

	imgData := makePNG(200, 200, color.NRGBA{0, 0, 255, 255})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(imgData)
	}))
	defer srv.Close()

	savedClient := fetchImageClient
	fetchImageClient = srv.Client()
	defer func() { fetchImageClient = savedClient }()

	html := []byte(`<img src="` + srv.URL + `/big.png" alt="test">`)
	result := fetchAndEmbed(html, 5)

	// Image should NOT be embedded (too large), original URL kept
	if strings.Contains(string(result), "data:image/png;base64,") {
		t.Error("oversized image should not be embedded")
	}
	if !strings.Contains(string(result), srv.URL) {
		t.Error("original URL should be preserved when image exceeds limit")
	}
}

// TestProcessArticleImages_SkipImageFetch verifies that when skipImageFetch
// is true, external image URLs are not downloaded and picture elements with
// srcset URLs are not fetched either.
func TestProcessArticleImages_SkipImageFetch(t *testing.T) {
	fetched := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetched = true
		w.Header().Set("Content-Type", "image/png")
		w.Write(makePNG(10, 10, color.NRGBA{100, 100, 100, 255}))
	}))
	defer srv.Close()

	html := []byte(`<img src="` + srv.URL + `/image.png" alt="ext">`)
	opts := optimizeOpts{maxWidth: 800, quality: 60, skipImageFetch: true}
	result := processArticleImages(html, opts, 1)

	if fetched {
		t.Error("skipImageFetch: external image should not have been downloaded")
	}
	// Original external URL must survive unchanged
	if !strings.Contains(string(result), srv.URL+"/image.png") {
		t.Errorf("skipImageFetch: original URL should be preserved, got: %s", result)
	}
}

// TestProcessArticleImages_SkipImageFetch_Picture verifies that <picture>
// elements with srcset URLs are not fetched when skipImageFetch is true.
func TestProcessArticleImages_SkipImageFetch_Picture(t *testing.T) {
	fetched := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetched = true
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(makeJPEG(10, 10, color.NRGBA{100, 100, 100, 255}))
	}))
	defer srv.Close()

	html := []byte(`<picture><source srcset="` + srv.URL + `/img.jpg 800w"><img src="` + srv.URL + `/img.jpg" alt="photo"></picture>`)
	opts := optimizeOpts{maxWidth: 800, quality: 60, skipImageFetch: true}
	result := processArticleImages(html, opts, 1)

	if fetched {
		t.Error("skipImageFetch: picture srcset image should not have been downloaded")
	}
	_ = result
}
