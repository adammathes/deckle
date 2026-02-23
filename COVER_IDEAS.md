# EPUB Cover Ideas for Deckle

Currently deckle produces EPUBs with no cover at all. The `go-epub` library
supports `SetCover(imagePath, cssPath)` which expects a pre-added image.
Since every EPUB is unique (different titles, articles, dates), the cover
needs to be generated programmatically in Go at build time.

Constraints worth keeping in mind:
- Primary target is **e-ink displays** (grayscale, ~167 ppi, 6-7" screens)
- No external dependencies or network calls for cover generation
- Must work with the metadata we already have: book title, article
  count, individual article titles/authors/sites, generation date
- Standard EPUB cover dimensions are roughly **1600x2400 px** (2:3 ratio)

---

## Idea 1: Typographic Title Card

A clean, text-only cover rendered from an SVG template. Large title text
centered vertically, with a subtle rule or divider, article count at the
bottom, and the generation date.

```
┌──────────────────────────┐
│                          │
│                          │
│                          │
│      WEEKLY READS        │
│      ─────────           │
│                          │
│      12 articles         │
│      Feb 2026            │
│                          │
│                          │
│                  deckle  │
└──────────────────────────┘
```

**Pros:** Dead simple, looks good on e-ink, zero dependencies, small file
size. Timeless "book" feel.

**Cons:** Could feel too plain. SVG text rendering varies across e-readers
(though rasterizing to PNG before embedding solves this).

**Implementation:** Build an SVG string in Go with `fmt.Sprintf`, rasterize
to PNG using Go's `image` and `image/draw` packages with embedded font
data, then pass to `e.AddImage()` + `e.SetCover()`.

---

## Idea 2: Table-of-Contents Collage

Use the cover as a preview of what's inside. List the first N article
titles (truncated to fit) in a stacked layout, like a newspaper front page
or magazine cover.

```
┌──────────────────────────┐
│  WEEKLY READS            │
│  ════════════════════    │
│                          │
│  Why Rust Is Eating      │
│  the World               │
│  ─ Jane Doe, Wired       │
│                          │
│  The Future of WebAssem- │
│  bly in Production       │
│  ─ John Smith, InfoQ     │
│                          │
│  Understanding the New   │
│  CSS Color Spaces        │
│  ─ Sara Chen, Smashing   │
│                          │
│  + 9 more articles       │
│                  deckle  │
└──────────────────────────┘
```

**Pros:** Immediately tells you what's in the collection. Makes each EPUB
feel distinct. Works well on e-ink at a glance.

**Cons:** More complex text layout. Need to handle long titles, missing
authors, variable article counts. Risk of looking cluttered.

**Implementation:** Same SVG-to-PNG pipeline as Idea 1 but with more
layout logic. Iterate over articles, truncate/ellipsize titles, stack
vertically with consistent spacing.

---

## Idea 3: Geometric Pattern + Title

Generate a unique geometric pattern (grid of squares, circles, or lines)
seeded from a hash of the book title or article URLs. Overlays the title
on top. Each EPUB gets a visually distinct but recognizable cover.

```
┌──────────────────────────┐
│ ▪ ▫ ▪ ▪ ▫ ▪ ▫ ▫ ▪ ▪ ▫  │
│ ▫ ▪ ▫ ▪ ▪ ▫ ▪ ▪ ▫ ▪ ▪  │
│ ▪ ▪ ▫ ▫ ▪ ▫ ▫ ▪ ▪ ▫ ▪  │
│ ▫ ▪ ▪ ▫ ▫ ▪ ▪ ▫ ▪ ▪ ▫  │
│ ▪ ▫ ▪ ▪ ▫ ▫ ▪ ▫ ▪ ▫ ▪  │
│                          │
│      WEEKLY READS        │
│                          │
│ ▪ ▫ ▫ ▪ ▪ ▫ ▪ ▫ ▪ ▪ ▫  │
│ ▫ ▪ ▪ ▫ ▪ ▪ ▫ ▪ ▫ ▪ ▪  │
│ ▪ ▪ ▫ ▪ ▫ ▪ ▪ ▫ ▪ ▫ ▪  │
│                  deckle  │
└──────────────────────────┘
```

**Pros:** Each cover is unique and recognizable in a library view. Looks
modern. Works great in grayscale. No text-layout complexity beyond the
title.

**Cons:** More code to write the pattern generator. The patterns have no
semantic meaning. May feel "techy" rather than "bookish."

**Implementation:** Hash the title string, use bits to drive a grid of
filled/unfilled cells drawn on an `image.Gray`. Composite the title text
on top. Pure Go, no SVG needed.

---

## Idea 4: Stripe / Color-Block Spine

Horizontal or vertical stripes where each stripe represents one article.
Stripe width (or shade of gray) could vary by article length. Title text
overlaid in a contrasting block. Inspired by the aesthetic of O'Reilly
book covers.

```
┌──────────────────────────┐
│██████████████████████████│
│▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓│
│░░░░░░░░░░░░░░░░░░░░░░░░│
│██████████████████████████│
│▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓│
│                          │
│   ┌──────────────────┐   │
│   │  WEEKLY READS    │   │
│   │  12 articles     │   │
│   └──────────────────┘   │
│                          │
│░░░░░░░░░░░░░░░░░░░░░░░░│
│██████████████████████████│
│▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓│
│░░░░░░░░░░░░░░░░░░░░░░░░│
│██████████████████████████│
│                  deckle  │
└──────────────────────────┘
```

**Pros:** Data-driven and visually interesting. Each collection looks
different. Very e-ink friendly (pure grayscale gradients). Clean and
modern.

**Cons:** The stripe-per-article metaphor might not be obvious to users.
Could look like a test pattern if the shades are too uniform.

**Implementation:** Draw horizontal bands on `image.Gray`, shade computed
from `len(article.HTML)` mapped to a gray range. Overlay a white rectangle
with title text.

---

## Idea 5: Big Initial Letter + Minimalist Frame

A single oversized letter (the first letter of the title) dominates the
cover, with the full title in smaller text below. Thin border frame.
Classic editorial/literary magazine aesthetic.

```
┌──────────────────────────┐
│ ┌──────────────────────┐ │
│ │                      │ │
│ │                      │ │
│ │         W            │ │
│ │                      │ │
│ │                      │ │
│ │   Weekly Reads       │ │
│ │                      │ │
│ │   12 articles        │ │
│ │   February 2026      │ │
│ │                      │ │
│ │                      │ │
│ │              deckle  │ │
│ └──────────────────────┘ │
└──────────────────────────┘
```

**Pros:** Striking and immediately recognizable. Very simple to implement.
E-ink friendly. Has a sophisticated, editorial feel.

**Cons:** Doesn't convey content. Could feel monotonous across many EPUBs
if titles start with the same letter.

**Implementation:** Draw border rectangle, render oversized first letter
centered, smaller title text below. Pure `image/draw` with embedded font.

---

## Implementation Notes (applies to all)

Whichever direction we pick, the mechanics are the same:

1. Add a `generateCover(title string, articles []epubArticle) ([]byte, error)`
   function in `epub.go` that returns PNG bytes.
2. In `buildEpub`, call it before adding sections:
   ```go
   coverPNG, err := generateCover(title, articles)
   coverURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString(coverPNG)
   imgPath, _ := e.AddImage(coverURI, "cover.png")
   e.SetCover(imgPath, "")
   ```
3. For text rendering, embed a small open-source font (e.g., Go's
   bundled fonts from `golang.org/x/image/font/gofont`, or embed a TTF
   via `embed`). Use `golang.org/x/image/font` + `golang.org/x/image/math/fixed`
   for drawing. The project already depends on `golang.org/x/image`.
4. Target output: **1200x1800 PNG** (2:3 ratio, good for most e-readers).

---

## Questions to Decide

- Which visual direction (or combination) feels right?
- Should the cover vary per-run, or be deterministic for the same inputs?
- Any preference on embedded font (Go mono? A serif? Sans-serif)?
- Should there be a `--no-cover` flag to skip cover generation?
