# Deckle – Agent Guidelines

## Development Workflow

### Red/Green TDD

All new functionality and bug fixes must be developed with test-driven development:

1. **Write a failing test first** (red). The test must compile but fail because the
   feature does not exist yet.
2. **Write the minimum production code** to make the test pass (green).
3. **Refactor** if needed, keeping all tests green throughout.

Do not write production code before you have a failing test that demands it.

### Run the Full Test Suite Before Every Commit

Before committing, always run:

```bash
go test -race -coverprofile=coverage.out -covermode=atomic ./...
go tool cover -func=coverage.out | grep '^total:'
```

Confirm all tests pass and the total coverage stays **at or above 80%**.

Also run `go vet ./...` to catch common issues.

### Commit Frequently

Make small, focused commits as you work. Commit after each green/refactor cycle –
do not batch up dozens of changes into one giant commit. A good commit message
explains *why*, not just *what*.

---

## Work Items

Current work items live in **[ROADMAP.md](./ROADMAP.md)**, organized into sections:

| Section | Meaning |
|---------|---------|
| **APPROVED** | Prioritized items approved for implementation – do these first. |
| **PROPOSED** | Ideas that have not yet been approved – do not start without approval. |
| **COMPLETED** | Finished work kept for historical reference. |

### Updating the ROADMAP

- When you **start** an approved item, note it as in-progress (e.g. add an
  "In progress" tag or a brief note).
- When you **finish** an item, move it from **APPROVED** to **COMPLETED** with a
  short summary of what was done.
- If you discover new work worth doing, add it to **PROPOSED** with enough detail
  for a human to make an approval decision. Never self-approve proposed items.

---

## Testing Standards

### Unit coverage ≥ 80 %

The CI pipeline enforces an 80 % total coverage threshold (see
`.github/workflows/ci.yml`). Do not merge changes that drop coverage below this
floor. If a piece of code is genuinely hard to test (e.g. `main()`), document why
in a comment rather than ignoring coverage silently.

### Race detection

Always run the test suite with `-race`. The CI uses it; local development should
too:

```bash
go test -race ./...
```

### Regression tests for bugs

Every bug fix must be accompanied by a regression test that would have caught the
bug. The test should be named descriptively so it is obvious which scenario it
covers (e.g. `TestFetchHTML_ExceedsSizeLimit`).

### Integration and benchmark tests

- `integration_test.go` contains end-to-end pipeline tests. Keep them green.
- Benchmark tests (`_bench_test.go` / `func BenchmarkXxx`) must continue to
  compile and run (`go test -bench=. -benchmem -run='^$' ./...`).

### epubcheck

When the `epubcheck` tool is installed (it is present in CI – see
`.github/workflows/ci.yml`), the `TestBuildEpub_EpubCheck` test validates EPUB
output against the W3C spec. Do not introduce EPUB 3 validation errors.

---

## Code Style Reminders

- This is a **Go** project. Follow standard Go style (`gofmt`, `go vet`).
- Prefer editing existing files over creating new ones.
- Add comments only where the logic is non-obvious.
- Avoid over-engineering. The minimum code to make the test pass is the right
  amount.
