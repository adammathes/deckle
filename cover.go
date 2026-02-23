// Cover image generation for epub output.
// Produces a deterministic geometric pattern seeded from the book title,
// with the title and article count overlaid as text.
package main

import (
	"crypto/sha256"
	"image"
	"image/color"
	"image/png"
	"bytes"
	"fmt"
	"math"

	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const (
	coverWidth  = 1200
	coverHeight = 1800
)

// generateCover creates a PNG cover image with a deterministic geometric
// pattern derived from the title and overlaid title text.
func generateCover(title string, articleCount int) ([]byte, error) {
	img := image.NewGray(image.Rect(0, 0, coverWidth, coverHeight))

	// Fill background white
	draw.Draw(img, img.Bounds(), image.NewUniform(color.Gray{0xFF}), image.Point{}, draw.Src)

	// Generate pattern from title hash
	hash := sha256.Sum256([]byte(title))
	drawPattern(img, hash)

	// Load fonts
	boldFace, err := loadFace(gobold.TTF, 64)
	if err != nil {
		return nil, fmt.Errorf("loading bold font: %w", err)
	}
	regularFace, err := loadFace(goregular.TTF, 32)
	if err != nil {
		return nil, fmt.Errorf("loading regular font: %w", err)
	}

	// Draw title block in the centre
	drawTitleBlock(img, title, articleCount, boldFace, regularFace)

	// Draw "deckle" in bottom-right
	drawLabel(img, "deckle", regularFace, coverWidth-40, coverHeight-40, anchorRight)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("encoding cover PNG: %w", err)
	}
	return buf.Bytes(), nil
}

// drawPattern fills the image with a grid of circles whose size and shade
// are determined by the hash bytes. The pattern is split into top and bottom
// bands with a clear central strip left for the title.
func drawPattern(img *image.Gray, hash [32]byte) {
	const (
		cols    = 12
		rows    = 18
		cellW   = coverWidth / cols
		cellH   = coverHeight / rows
		// Rows reserved for the title block (centre of image)
		titleRowStart = 7
		titleRowEnd   = 11
	)

	for row := 0; row < rows; row++ {
		// Skip the title area
		if row >= titleRowStart && row <= titleRowEnd {
			continue
		}
		for col := 0; col < cols; col++ {
			idx := (row*cols + col) % len(hash)
			b := hash[idx]

			// XOR with position-derived bits for more variation
			b ^= byte(row*17 + col*31)

			// Shade: map to a range that reads well on e-ink (0x30..0xD0)
			shade := uint8(0x30 + int(b)*(0xD0-0x30)/255)

			// Radius proportional to another bit
			idx2 := (idx + 7) % len(hash)
			b2 := hash[idx2] ^ byte(row*13+col*41)
			maxR := float64(cellW) / 2.2
			minR := maxR * 0.25
			radius := minR + (maxR-minR)*float64(b2)/255.0

			cx := col*cellW + cellW/2
			cy := row*cellH + cellH/2
			fillCircle(img, cx, cy, radius, color.Gray{shade})
		}
	}
}

// fillCircle draws a filled circle on a grayscale image.
func fillCircle(img *image.Gray, cx, cy int, radius float64, c color.Gray) {
	r := int(math.Ceil(radius))
	r2 := radius * radius
	for dy := -r; dy <= r; dy++ {
		for dx := -r; dx <= r; dx++ {
			if float64(dx*dx+dy*dy) <= r2 {
				x, y := cx+dx, cy+dy
				if x >= 0 && x < coverWidth && y >= 0 && y < coverHeight {
					img.SetGray(x, y, c)
				}
			}
		}
	}
}

// drawTitleBlock renders the title text (word-wrapped) and article count
// centred vertically in the middle of the cover, on a white band.
func drawTitleBlock(img *image.Gray, title string, articleCount int, titleFace, metaFace font.Face) {
	const (
		bandTop    = 650
		bandBottom = 1150
		padX       = 80
		maxWidth   = coverWidth - padX*2
	)

	// Clear the title band to white
	draw.Draw(img,
		image.Rect(0, bandTop, coverWidth, bandBottom),
		image.NewUniform(color.Gray{0xFF}),
		image.Point{},
		draw.Src,
	)

	// Draw thin horizontal rules
	for x := padX; x < coverWidth-padX; x++ {
		img.SetGray(x, bandTop+20, color.Gray{0x99})
		img.SetGray(x, bandBottom-20, color.Gray{0x99})
	}

	// Word-wrap and draw the title
	lines := wrapText(title, titleFace, maxWidth)
	lineHeight := titleFace.Metrics().Height.Ceil() + 8

	// Calculate vertical start so title + meta are centred in the band
	metaHeight := metaFace.Metrics().Height.Ceil() + 16
	totalHeight := len(lines)*lineHeight + metaHeight
	y := bandTop + (bandBottom-bandTop-totalHeight)/2 + titleFace.Metrics().Ascent.Ceil()

	for _, line := range lines {
		lineW := font.MeasureString(titleFace, line).Ceil()
		x := (coverWidth - lineW) / 2
		drawString(img, line, titleFace, x, y)
		y += lineHeight
	}

	// Article count below title
	y += 16
	meta := fmt.Sprintf("%d articles", articleCount)
	if articleCount == 1 {
		meta = "1 article"
	}
	metaW := font.MeasureString(metaFace, meta).Ceil()
	drawString(img, meta, metaFace, (coverWidth-metaW)/2, y)
}

type anchor int

const (
	anchorLeft anchor = iota
	anchorRight
)

// drawLabel draws a small text label at a given position.
func drawLabel(img *image.Gray, text string, face font.Face, x, y int, a anchor) {
	if a == anchorRight {
		w := font.MeasureString(face, text).Ceil()
		x -= w
	}
	drawString(img, text, face, x, y)
}

// drawString renders a string onto a grayscale image in black.
func drawString(img *image.Gray, s string, face font.Face, x, y int) {
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(color.Gray{0x00}),
		Face: face,
		Dot:  fixed.P(x, y),
	}
	d.DrawString(s)
}

// wrapText splits text into lines that fit within maxWidth pixels.
func wrapText(text string, face font.Face, maxWidth int) []string {
	words := splitWords(text)
	if len(words) == 0 {
		return []string{text}
	}

	var lines []string
	current := words[0]
	for _, word := range words[1:] {
		trial := current + " " + word
		if font.MeasureString(face, trial).Ceil() <= maxWidth {
			current = trial
		} else {
			lines = append(lines, current)
			current = word
		}
	}
	lines = append(lines, current)
	return lines
}

// splitWords splits a string on whitespace, returning non-empty tokens.
func splitWords(s string) []string {
	var words []string
	word := ""
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' {
			if word != "" {
				words = append(words, word)
				word = ""
			}
		} else {
			word += string(r)
		}
	}
	if word != "" {
		words = append(words, word)
	}
	return words
}

// loadFace parses an OpenType font and returns a Face at the given size in points.
func loadFace(ttf []byte, sizePt float64) (font.Face, error) {
	f, err := opentype.Parse(ttf)
	if err != nil {
		return nil, err
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{
		Size:    sizePt,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, err
	}
	return face, nil
}
