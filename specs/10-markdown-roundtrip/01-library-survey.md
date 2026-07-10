# 10-markdown-roundtrip — Library survey

## Overview

The v2 feature (markdown upload + download) requires two
high-quality, MIT-licensed, Go-native conversion libraries
that can be combined into a lossless-on-text-content pipeline.
This spec surveys the candidate libraries, compares them on
the dimensions that matter for this project, and locks the
selection so the implementation phases don't have to
re-evaluate.

## Sources

- `github.com/yuin/goldmark` — https://github.com/yuin/goldmark
  (4.9k stars, 35k+ dependents, MIT, v1.7.13 current, last
  commit 2026-03-25, actively maintained)
- `github.com/JohannesKaufmann/html-to-markdown` —
  https://github.com/JohannesKaufmann/html-to-markdown
  (3.7k stars, 213 forks, MIT, v2.5.2, last release 2026-06-07,
  actively maintained; CLI + online demo + REST API in
  addition to the Go library)
- `github.com/PuerkitoBio/goquery` —
  https://github.com/PuerkitoBio/goquery
  (14.4k stars, MIT, jQuery-style HTML walker; needed for the
  Confluence-specific namespace-stripping + macro rewrap pass
  that lives between goldmark's output and the API wire format)
- `github.com/grantcarthew/acon` —
  https://github.com/grantcarthew/acon
  (the reference implementation; CLI for Confluence from the
  terminal with the same feature surface; uses the same two
  primary libraries and ships a 28-check round-trip test
  script)

## Spec

### Selection criteria (in priority order)

1. **Round-trip textual fidelity.** The fundamental
   requirement: markdown text, code-block contents, list
   items, table cell text, and link URLs must survive both
   directions without alteration. Cosmetic differences
   (whitespace, reference-link → inline-link, attribute
   order) are acceptable; textual content loss is not.
2. **License compatibility.** MIT only. AGPL/SSPL/Commons
   Clause ruled out. Both goldmark and html-to-markdown are
   MIT — clean closure with this project's MIT license.
3. **Active maintenance.** Last commit within 12 months.
   Both libraries cleared this (goldmark 2026-03, h2m
   2026-06). goldmark has 60 contributors; h2m has fewer but
   has a stable API and a release cadence.
4. **CommonMark / GFM coverage.** Goldmark is CommonMark +
   GFM compliant with all the extensions this project needs:
   tables, strikethrough, task lists, fenced code blocks,
   autolinks, definition lists.
5. **Plugin / customisation surface.** Both libraries expose
   AST walkers and renderer hooks. h2m has first-class
   `plugin.Rule` API for custom element handling (needed
   for `ac:link`, `ac:image`, `ac:task` → markdown); goldmark
   has `renderer.NodeRendererFunc` for custom node rendering
   (needed for `ac:structured-macro` → fenced code block).
6. **Test corpus quality.** h2m ships a "golden file" test
   corpus (`.in.html` → `.out.md` pairs across 100+ edge
   cases). This is the test pattern we mirror for our own
   `internal/markdown/testdata/`.
7. **Dependency footprint.** Goldmark depends only on the
   Go stdlib. h2m v2 depends on
   `github.com/JohannesKaufmann/dom` and `golang.org/x/net`.
   All three are MIT, no AGPL transitive deps.

### Locked library decisions (Q23-Q26, locked 2026-07-10)

- **Q23 — Markdown → HTML**: `github.com/yuin/goldmark v1.7.13`
  with the GFM extension enabled (`goldmark.WithExtensions(
  extension.GFM)`). Used for the `conf_post_markdown` /
  `conf_put_markdown` upload path.
- **Q24 — HTML → Markdown**: `github.com/JohannesKaufmann/
  html-to-markdown/v2 v2.5.2` with the default
  `NewConverter()` (uses the `CommonPlugins` set which
  covers tables, strikethrough, task lists, and link
  normalisation). Used for the `conf_get_page_markdown`
  download path.
- **Q25 — Post-processing for the upload direction**:
  `github.com/PuerkitoBio/goquery v1.10.x` for the AST
  walk that converts goldmark's HTML output to Confluence
  storage format. This pass does five things: (1) wrap
  `<pre><code class="language-X">` blocks in
  `<ac:structured-macro ac:name="code">`, (2) convert
  `<img src>` tags to `<ac:image><ri:url ri:value="..."/></ac:image>`
  for external images, (3) convert `<a href>` tags to
  `<ac:link><ri:url ri:value="..."/></ac:link>` for
  external links, (4) add the `xmlns:ac="..."` namespace
  declaration to the root element, (5) ensure self-closing
  tags have a trailing slash (`<br>` → `<br/>`).
- **Q26 — Test methodology**: Mirror h2m's golden-file
  pattern. Each test case is a pair `(testdata/<name>.in.md,
  testdata/<name>.want.xhtml)` for the upload direction and
  `(testdata/<name>.in.xhtml, testdata/<name>.want.md)` for
  the download direction. Update with
  `go test ./internal/markdown/... -update` (a `tflag` we
  add that matches h2m's `go test -update`).

### Rejected alternatives

- `github.com/cseeger-epages/markdown2confluence` — last
  commit 2018, marked unmaintained, depends on the
  unmaintained `a8m/mark` library. Rejected.
- Custom in-tree converter — would be ~2000 LOC for the
  same coverage; both libraries have a decade of production
  hardening. Rejected (we do the Confluence-specific
  post-processing, not the base conversion).
- `pandoc` subprocess — would break the static-binary
  contract (Paketo distroless has no shell-out for
  arbitrary binaries) and add a 200+MB dependency.
  Rejected.

## Verification

- `go list -m github.com/yuin/goldmark github.com/JohannesKaufmann/
  html-to-markdown/v2 github.com/PuerkitoBio/goquery` shows
  exact pinned versions matching the locked decisions above.
- `grep -r "AGPL\|SSPL\|Commons Clause" go.sum` returns 0
  matches.
- All 28 feature checks from `acon/testdata/
  roundtrip-test.sh` are mirrored as Go tests in
  `internal/markdown/testdata/roundtrip_test.go`.
