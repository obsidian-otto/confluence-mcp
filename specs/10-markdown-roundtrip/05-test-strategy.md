# 10-markdown-roundtrip — Test strategy

## Overview

The markdown round-trip feature is high-risk in two ways:
(a) the third-party libraries are 35k-LoC and can have
edge-case behaviour that surprises us, and (b) the
post-processor is new code that touches a non-trivial
input space (every legal CommonMark document). The test
strategy is layered: unit tests per stage, golden-file
round-trip tests for the integration, and an end-to-end
smoke test against a real Confluence instance for
faithfulness.

## Sources

- The h2m golden-file test pattern:
  https://github.com/JohannesKaufmann/html-to-markdown#testing
- goldmark's own test suite structure (testdata/ folder
  with .md input + expected AST or HTML output)
- The 28-check `acon/testdata/roundtrip-test.sh` script

## Spec

### Test layers

The package has 4 test layers, run in order, each gating
the next:

1. **Per-stage unit tests** (`internal/markdown/*_test.go`)
   - `TestMarkdownToHTML_CommonMark` — goldmark returns
     expected HTML for canonical CommonMark inputs
     (headings, lists, code blocks, tables, links,
     blockquotes, horizontal rules, inline code, emphasis)
   - `TestPostProcess_CodeBlocks`, `TestPostProcess_Images`,
     `TestPostProcess_Links`, `TestPostProcess_Namespaces`
     — each of the 5 post-processor rules in isolation,
     with hand-written input/output pairs
   - `TestStorageToMarkdown_Reverse` — h2m's output is
     the expected markdown for known storage inputs
2. **Golden-file round-trip tests**
   (`internal/markdown/testdata/roundtrip_test.go`)
   - 28 fixture pairs (`.in.md` + `.want.xhtml` for
     upload direction, `.in.xhtml` + `.want.md` for
     download direction) drawn from the 28 checks in
     `acon/testdata/roundtrip-test.sh`
   - Update with `go test -update` (added as a custom
     test flag mirroring h2m's pattern)
3. **No-textual-content-loss test**
   (`TestRoundTripPreservesText` from
   `03-known-lossy-constructs.md`)
   - For each of 14 "preserved" feature categories,
     the set of non-whitespace tokens in the original
     markdown is a subset of the set of tokens after
     md → Conf storage → md round-trip
4. **End-to-end smoke test**
   (`internal/tools/markdown_handlers_test.go`, tagged
   `//go:build integration`)
   - Spawns an `httptest.NewServer` that mimics
     Confluence's v2 REST surface
   - Calls `HandlePostMarkdown` → asserts the wire
     request has the expected storage envelope
   - Calls `HandleGetPageMarkdown` with a fixture
     storage response → asserts the returned markdown
     matches the expected text
   - Run with `make test-integration` (new Makefile
     target)

### Golden-file layout

```
internal/markdown/testdata/
  ├── README.md                            # how to update goldens
  ├── golden/
  │   ├── headings/
  │   │   ├── in.md                        # "# H1\n## H2\n..."
  │   │   ├── want.xhtml                   # "<h1>H1</h1><h2>H2</h2>..."
  │   │   └── want.xhtml.md                # what conf_get_page_markdown
  │   │                                   # would yield for the same content
  │   ├── code-blocks/
  │   │   ├── in.md
  │   │   ├── want.xhtml
  │   │   └── want.xhtml.md
  │   ├── tables/
  │   ├── links/
  │   ├── images/
  │   ├── task-lists/
  │   ├── blockquotes/
  │   ├── horizontal-rules/
  │   ├── strikethrough/
  │   ├── inline-code/
  │   ├── unicode/
  │   ├── emoji/
  │   ├── html-entities/
  │   ├── snake-case-identifiers/         # the regression test
  │   │   ├── in.md                        # "_a_b_c_"
  │   │   └── want.md                      # must NOT be "\\_a\\_b\\_c\\_"
  │   └── ...                              # 28 categories total
  └── roundtrip_test.go
```

The `*.want.xhtml.md` file is the markdown that
`conf_get_page_markdown` would yield for the same
storage content. The test reads all three files
together and asserts:
- `markdownToStorageXHTML(read("in.md")) == read("want.xhtml")`
- `storageXHTMLToMarkdown(read("want.xhtml")) == read("want.xhtml.md")`

### Update flag

```go
// In internal/markdown/markdown_test.go:
//go:build update

func TestUpdate(t *testing.T) { ... }
```

When `go test -tags update` is passed, every test
regenerates its golden file instead of asserting
equality. This is the pattern h2m uses; we copy it
verbatim. The Makefile gets a `make test-update` target
that runs this tag.

### CI gate

`make check` runs layers 1–3 and fails on any drift.
`make test-integration` runs layer 4 separately (the
build tag prevents it from running in the default
loop, since it requires the httptest server
infrastructure to be present). `make all` runs both.

### What this does NOT cover

- Real Confluence API quirks (version-number sequencing
  on concurrent updates) — covered by Phase 10 smoke
  test against the live instance
- Macro-rewriting for non-code macros (info / panel /
  expand) — the known-lossy register calls these out
  and they are explicitly NOT in the v2 contract
- Performance under load — not a v2 requirement; the
  reference implementation `acon` doesn't measure this
  either

## Verification

- `go test ./internal/markdown/...` runs layers 1–3
  and is green
- `make test-integration` runs layer 4 and is green
- `go test -tags update ./internal/markdown/...`
  regenerates goldens without error
- A new `markdown-testdata-locked` CI check (if
  applicable) fails if a golden file is changed
  without an accompanying source change in the
  post-processor
