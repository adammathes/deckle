# Deckle Roadmap

## STATUS

Deckle generates valid EPUB 3 output. A stress test with 87 Hacker News
article pages (February 2025) found 552 epubcheck errors + 1 fatal, all of
which have been fixed. The EPUB now validates with 0 errors, 0 warnings.

129 tests pass, including 26 regression tests for every validation issue found.

### Architecture overview

The HTML processing pipeline has two cleaning stages:

1. **Regex-based** (`imgoptimize.go`): `processArticleImages` handles lazy-load
   promotion, external image fetching/embedding, `<picture>` collapse via regex,
   image optimization, then `cleanForEpub` strips AVIF images, external srcset
   attrs, data-* attrs, and inline SVGs.

2. **DOM-based** (`epub.go`): `sanitizeForXHTML` parses to a DOM tree and
   handles element/attribute whitelisting, invalid XML chars, external image
   removal, `<source>`/`<picture>` removal, ID dedup, dimension sanitization,
   nesting fixes, `<dl>` content model, and `<figcaption>` placement.

Both stages independently address some of the same concerns (external images,
picture elements, disallowed attributes). This is defensive—the regex pass runs
during article processing while the DOM pass runs at EPUB generation time—but
creates conceptual overlap.

`sanitizeForXHTML` is now ~290 lines as a single function with a recursive
closure. It handles XML cleaning, element transforms, attribute filtering,
nesting repair, and content model fixes all in one pass. The function works
correctly but is the densest part of the codebase.

### Current file layout

| File | Lines | Role |
|------|-------|------|
| `epub.go` | 765 | EPUB building + XHTML sanitization |
| `epub_test.go` | 868 | EPUB + sanitization tests |
| `imgoptimize.go` | 542 | Image optimization + regex HTML cleanup |
| `imgoptimize_test.go` | 848 | Image optimization tests |
| `cover.go` | 370 | Cover image generation |
| `main.go` | 287 | CLI + pipeline orchestration |
| `fetch.go` | 233 | HTTP fetching with TLS fingerprinting |
| `headings.go` | 197 | Title extraction, heading normalization |
| `ssrf.go` | 77 | SSRF protection |
| `readability.go` | 39 | Readability extraction wrapper |

---
## COMPLETED

- **Stress test infrastructure**: Scripts and documentation for running epubcheck
  validation against arbitrary web sources are checked in under `scripts/` and
  `docs/stress-testing.md`.

- **Skip image downloads in markdown mode**: Added `skipImageFetch bool` to
  `optimizeOpts`. When true, `fetchAndEmbed` and the `<picture>` srcset fetch
  path are skipped. `run()` sets this flag automatically in markdown mode.
  External image URLs are preserved as-is (markdown renders them without needing
  embedded data URIs). Covered by `TestProcessArticleImages_SkipImageFetch*`
  and `TestRun_MarkdownMode_SkipsExternalImages`.

- **Add CI epubcheck validation**: `sudo apt-get install -y epubcheck` added to
  `.github/workflows/ci.yml` before the test step. The existing
  `TestBuildEpub_EpubCheck` test now runs (rather than skipping) on every CI
  push/PR, catching EPUB 3 regressions automatically.

- **Proxy-aware fetching**: Added `--proxy` flag and `fetchProxyURL` global.
  When set, all outgoing HTTP requests (articles and images) use a standard-TLS
  `http.Transport` with `http.ProxyURL`, bypassing uTLS fingerprinting which
  cannot negotiate CONNECT tunnels. uTLS fingerprinting is preserved when no
  proxy is configured. Covered by `TestNewProxyClient_*` and
  `TestFetchHTML_WithProxy`.

## APPROVED
*(Large work items approved by humans.)*

### Extract sanitizeForXHTML into its own file

`epub.go` currently mixes two concerns: EPUB assembly (`buildEpub`,
`buildTOCBody`, `extractImages`) and HTML→XHTML sanitization
(`sanitizeForXHTML` + 7 helper functions). Extracting the sanitization
code into `sanitize.go` would:

- Separate the XHTML compliance concern from EPUB packaging
- Make `epub.go` focused on epub assembly (~300 lines)
- Make `sanitize.go` focused on HTML→XHTML conversion (~450 lines)
- Make it easier to test and extend each concern independently

The helpers that would move: `stripInvalidXMLChars`, `sanitizeDimensionAttr`,
`sanitizeID`, `isPhrasingElement`, `isStructuralBlock`, `isBlockElement`,
`elemAllowsDimensions`, `sanitizeForXHTML`, `renderXHTML`.

**Risk**: Low. Pure file reorganization, no behavior change.

### Consolidate HTML cleaning between imgoptimize.go and epub.go

Both `cleanForEpub` (regex-based) and `sanitizeForXHTML` (DOM-based)
independently handle overlapping concerns:

| Concern | `cleanForEpub` | `sanitizeForXHTML` |
|---------|----------------|-------------------|
| External images | — | strips `<img>` with http/https src |
| `<picture>` elements | regex collapse | DOM collapse |
| `<source>` elements | — | removes |
| External srcset | regex strip | — |
| data-* attributes | regex strip | attribute whitelist |
| Inline SVG | regex strip | element whitelist |
| AVIF images | regex strip | — |

The regex pass runs during `processArticleImages` (before EPUB generation).
The DOM pass runs in `buildEpub`. Having both is defensive but means bugs
could hide in one pass while the other compensates.

Options:
- **Keep both** (current): safest, minor perf cost, some conceptual duplication
- **Move regex cleanup into DOM pass**: single source of truth, but would
  require the DOM pass to run earlier in the pipeline (or imgoptimize to
  produce a DOM rather than bytes)
- **Make regex pass more minimal**: have `cleanForEpub` only handle things
  the DOM pass can't (AVIF detection, animated GIF passthrough) and let
  `sanitizeForXHTML` be the authoritative cleaner

**Risk**: Medium. Changing the pipeline order could surface edge cases.
Would need a stress test run to validate.

### Break up the sanitizeForXHTML closure

The inner `clean()` closure is ~260 lines handling many distinct concerns
via a long chain of if/else blocks. Refactoring into a struct with methods
would:

- Make each concern (media conversion, element filtering, attribute
  filtering, nesting repair, content model fixes) a named method
- Replace closure-captured state (`ids`, `usedIDs`) with struct fields
- Make it easier to add new content model fixes without growing one function

Sketch:
```go
type xhtmlSanitizer struct {
    ids     map[string]bool
    usedIDs map[string]bool
}

func (s *xhtmlSanitizer) clean(n *html.Node) *html.Node { ... }
func (s *xhtmlSanitizer) transformElement(n *html.Node) *html.Node { ... }
func (s *xhtmlSanitizer) filterAttributes(n *html.Node) { ... }
func (s *xhtmlSanitizer) fixNesting(n *html.Node) { ... }
func (s *xhtmlSanitizer) fixDLContent(n *html.Node) { ... }
```

**Risk**: Low-medium. Refactoring only, but the tree manipulation is subtle
(removing/inserting nodes during traversal). Would need full test suite +
stress test to validate.

### Add fuzz testing for sanitizeForXHTML

`sanitizeForXHTML` processes arbitrary HTML from the wild internet. Go's
built-in fuzzing (`go test -fuzz`) could find panics or invalid output
that unit tests miss. A fuzz target would:

- Feed random/mutated HTML strings to `sanitizeForXHTML`
- Verify the output is valid XML (parse with `encoding/xml`)
- Verify no disallowed elements survive
- Verify no invalid XML characters remain

This would complement the stress testing approach (real-world pages) with
randomized input testing.

**Risk**: None. Additive testing only.


---

## PROPOSED

*(Potential work identified during AI-driven stress testing. Not yet approved.)*


