# Deckle Roadmap

## STATUS

Deckle generates valid EPUB 3 output. A stress test with 87 Hacker News
article pages (February 2025) found 552 epubcheck errors + 1 fatal, all of
which have been fixed. The EPUB now validates with 0 errors, 0 warnings.

133 tests pass (including fuzz seed corpus), with 85.3% statement coverage.

### Architecture overview

The HTML processing pipeline has two stages:

1. **Image processing** (`imgoptimize.go`): `processArticleImages` handles
   lazy-load promotion, external image fetching/embedding, `<picture>`
   collapse via regex, and image optimization for e-readers.

2. **DOM-based sanitization** (`sanitize.go`): `sanitizeForXHTML` is the
   authoritative HTML→XHTML cleaner. It parses to a DOM tree and handles
   element/attribute whitelisting, invalid XML chars, external image removal,
   AVIF stripping, `<source>`/`<picture>` removal, ID dedup, dimension
   sanitization, nesting fixes, `<dl>` content model, and `<figcaption>`
   placement. Implemented as an `xhtmlSanitizer` struct with separate methods
   for each concern (transformElement, filterAttributes, fixNesting,
   fixDLContent, fixFigcaption).

EPUB assembly (`epub.go`) is separate from sanitization.

### Current file layout

| File | Lines | Role |
|------|-------|------|
| `sanitize.go` | ~350 | HTML→XHTML sanitization (xhtmlSanitizer struct) |
| `sanitize_test.go` | ~540 | Sanitization unit tests |
| `sanitize_fuzz_test.go` | ~90 | Fuzz testing for sanitizeForXHTML |
| `epub.go` | ~250 | EPUB building (buildEpub, TOC, image extraction) |
| `epub_test.go` | ~280 | EPUB assembly tests |
| `imgoptimize.go` | ~530 | Image optimization + lazy-load promotion |
| `imgoptimize_test.go` | ~830 | Image optimization tests |
| `cover.go` | 370 | Cover image generation |
| `main.go` | 287 | CLI + pipeline orchestration |
| `fetch.go` | 233 | HTTP fetching with TLS fingerprinting |
| `headings.go` | 197 | Title extraction, heading normalization |
| `ssrf.go` | 77 | SSRF protection |
| `readability.go` | 39 | Readability extraction wrapper |

---
## COMPLETED

- **Extract sanitizeForXHTML into its own file**: Moved all XHTML
  sanitization code from `epub.go` into `sanitize.go`, with corresponding
  tests in `sanitize_test.go`. `epub.go` now focuses on EPUB packaging
  (~250 lines), `sanitize.go` on HTML→XHTML conversion (~350 lines).
  Pure file reorganization, no behavior change.

- **Consolidate HTML cleaning between imgoptimize.go and epub.go**: Made
  `sanitizeForXHTML` the single source of truth for all HTML cleanup.
  Added AVIF image stripping to the DOM-based sanitizer. `cleanForEpub`
  is now a documented no-op passthrough — its four regex patterns
  (avifImgRe, extSrcsetAttrRe, dataAttrRe, inlineSVGRe) were removed
  since the DOM sanitizer's attribute/element whitelists already handle
  all those concerns.

- **Break up the sanitizeForXHTML closure**: Replaced the ~260-line
  `clean()` closure with an `xhtmlSanitizer` struct with named methods:
  `transformElement`, `filterAttributes`, `fixNesting`, `fixDLContent`,
  `fixFigcaption`, and `clean` (recursive walker). Closure-captured state
  (`ids`, `usedIDs`) became struct fields. `collectIDs` extracted to a
  standalone function.

- **Add fuzz testing for sanitizeForXHTML**: Added `FuzzSanitizeForXHTML`
  using Go's built-in fuzzing with a 29-entry seed corpus. The fuzz target
  verifies valid XML output, no disallowed elements, and no invalid XML
  characters. The fuzzer immediately found a bug: structural blocks moved
  out of phrasing elements during nesting repair bypassed attribute
  filtering. Fixed by calling `s.clean(c)` on moved nodes before insertion.
  Regression test: `TestSanitizeForXHTML_MovedBlockGetsAttributesFiltered`.

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


---

## PROPOSED

*(Potential work identified during code review and stress testing. Not yet approved.)*

### Connection leak and no connection reuse in browserTransport

`browserTransport.RoundTrip` (`fetch.go:151`) creates a new TLS connection
via `dialUTLS` for every single request. For HTTP/1.1 responses, it wraps
the connection in a throwaway `http.Transport` (line 177) that is never
reused. The `conns map[string]net.Conn` field on the struct (line 124)
was intended for connection caching but is never written to — it is dead
code.

Impact: every article fetch and every external image fetch from the same
host performs a full TCP + TLS handshake. For a 20-article epub from the
same domain, this means 20+ redundant TLS handshakes. For HTTP/2
connections the `h2conn` is similarly never cached.

**Fix**: Implement connection pooling keyed by host, or delegate to the
standard `http.Transport` pool after the initial uTLS handshake. Also
remove the unused `conns` field.

**Risk**: Medium. Requires careful handling of connection lifecycle and
concurrent access. The current code is correct but wasteful.

---

### Deduplicate fetchOneImage / fetchImage

`fetchOneImage` (`imgoptimize.go:191`) and `fetchImage` (`imgoptimize.go:328`)
perform the same HTTP fetch + MIME detection logic with different return
types. The MIME parsing code (Content-Type header fallback to
`http.DetectContentType`) is duplicated across both functions.

**Fix**: Extract a shared `fetchImageData(url) ([]byte, string, error)`
helper and have both callers wrap it.

**Risk**: None. Pure refactor.

---

### Remove cleanForEpub no-op

`cleanForEpub` is currently a no-op passthrough retained for pipeline
compatibility. It could be removed entirely and its call site in
`processArticleImages` deleted. This is trivial cleanup.

**Risk**: None. Removing dead code.

---

### Remove dead `conns` field from browserTransport

`browserTransport` (`fetch.go:117`) declares a `conns map[string]net.Conn`
field that is never read or written. It should be removed to avoid
confusion. (Can be done as part of the connection reuse work above, or
independently.)

**Risk**: None. Removing dead code.

---

### Handle stdout write errors in writeOutput

`writeOutput` (`main.go:169`) calls `os.Stdout.WriteString(content)` without
checking the returned error. If stdout is a broken pipe (e.g., piped to
`head`), the error is silently ignored and `run()` returns nil. The caller
then exits 0 instead of signaling the failure.

**Fix**: Check the error from `WriteString` and return it.

**Risk**: None. Trivial fix.

---

### Reduce global mutable state

Several package-level variables are mutated at runtime:
- `maxResponseBytes` (`fetch.go:23`) — set from CLI flag
- `fetchProxyURL` (`fetch.go:28`) — set from CLI flag
- `fetchImageClient` (`fetch.go:245`) — set in `init()`, overridden in tests
- `logOut` (`main.go:29`) — set from `--silent` flag

Tests must save/restore these globals manually, which is fragile and
prevents `t.Parallel()`. Threading these through `cliConfig` or a context
object would improve testability and eliminate data race risk.

**Fix**: Pass configuration through function parameters or a config struct
instead of globals. The `cliConfig` struct already exists and could be
extended.

**Risk**: Medium. Touches many call sites. Can be done incrementally
(one global at a time).

---

### Migrate regex-based image processing to DOM

`imgoptimize.go` uses ~10 compiled regexes to manipulate HTML for image
optimization (lazy-load promotion, external fetch, `<picture>` collapse,
data URI replacement). While the DOM-based `sanitizeForXHTML` runs
afterward and catches most edge cases, the regex approach is inherently
fragile with real-world HTML (attributes containing `>`, unquoted
attributes, multi-line tags, etc.).

**Fix**: Move image processing to operate on a parsed DOM tree (like
`sanitize.go` already does), then serialize back to HTML. This would
eliminate an entire class of parsing edge cases.

**Risk**: High. Large refactor touching the core pipeline. Would need
thorough regression testing against the stress-test corpus. The current
approach works in practice due to the sanitizer safety net.

---

### splitWords string concatenation performance

`splitWords` (`cover.go:336`) builds words via `word += string(r)`, which
allocates a new string for every rune. For long titles this creates
unnecessary GC pressure.

**Fix**: Use `strings.Builder` for word accumulation.

**Risk**: None. Trivial optimization.

