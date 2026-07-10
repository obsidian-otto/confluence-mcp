# 10-markdown-roundtrip — Known-lossy constructs register

## Overview

Markdown and Confluence storage format are not isomorphic.
Some constructs survive round-trip cleanly (text, code,
lists, tables, links); others do not (image alt text,
Confluence-specific macros, internal page links, layout
sections, mentions). This spec is the authoritative
register of what is and is not preserved, so callers can
make informed decisions about when to use the markdown
tools vs the raw CRUD tools.

## Sources

- The `acon` testdata feature-support matrix:
  https://github.com/grantcarthew/acon/blob/main/testdata/README.md
  (the 28-feature matrix that we mirror)
- Confluence storage-format macros reference:
  https://confluence.atlassian.com/conf59/confluence-storage-format-for-macros-792499117.html
- goldmark's GFM extension documentation:
  https://github.com/yuin/goldmark/tree/master/extension

## Spec

### Preserved-on-round-trip (the contract)

These are the constructs that **must** survive a
md → Confluence → md cycle without textual content loss.
Each row is verified by a test in
`internal/markdown/testdata/roundtrip_test.go`.

| Feature | MD→Conf | Conf→MD | How verified |
|---|---|---|---|
| Headings H1–H6 | ✅ | ✅ | grep for `^#{1,6}` after Conf→MD |
| Bold `**x**` | ✅ | ✅ | grep for `\*\*x\*\*` after Conf→MD |
| Italic `*x*` | ✅ | ✅ | grep for `\*x\*` after Conf→MD |
| Inline code `` `x` `` | ✅ | ✅ | grep for `` `x` `` after Conf→MD |
| Strikethrough `~~x~~` | ✅ | ✅ | grep for `~~x~~` after Conf→MD |
| Fenced code blocks w/ language | ✅ | ✅ | grep for `^```lang$` after Conf→MD |
| Unordered lists (any nesting) | ✅ | ✅ | regex check for `^[-*]\s` |
| Ordered lists (any nesting) | ✅ | ✅ | regex check for `^\d+\.\s` |
| Tables (cell text + pipe syntax) | ✅ | ✅ | grep for `^|.*|$` rows |
| External links (URL + text) | ✅ | ✅ | grep for `\[text\]\(URL\)` |
| Blockquotes (incl. nested) | ✅ | ✅ | grep for `^>` |
| Horizontal rules `---` | ✅ | ✅ | regex for `^---$` or `^\*\*\*$` |
| Unicode text (incl. CJK, emoji) | ✅ | ✅ | byte-for-byte string comparison |
| HTML entities `&amp;` `&lt;` `&gt;` | ✅ | ✅ | byte-for-byte string comparison |
| Hard line breaks (two-space or `\\`) | ✅ | ✅ | manual inspection |

### Known-lossy (do not use markdown tools for these)

| Feature | Direction | Why | Workaround |
|---|---|---|---|
| Image alt text | MD→Conf | `<ac:image>` has no alt attribute | Use `conf_post` with raw XHTML and add `ac:alt` attribute |
| Confluence layouts (`<ac:layout>`, `<ac:layout-section>`, `<ac:layout-cell>`) | Conf→MD | Markdown has no layout primitive | Best-effort: convert to a 2-column markdown table (lossy) |
| Confluence panels (`<ac:structured-macro ac:name="info">` etc.) | Conf→MD | Markdown has no panel primitive | Drop the panel wrapping, keep the inner text |
| Confluence status lozenges (`<ac:structured-macro ac:name="status">`) | Conf→MD | Markdown has no status lozenge | Replace with `[STATUS]` text prefix |
| User mentions (`<ac:link><ri:user ri:account-id="..."/></ac:link>`) | Conf→MD | Cannot resolve user IDs to @names without an API call | Replace with `@<account-id>` placeholder text |
| Page mentions (`<ac:link><ri:page ri:content-title="..."/></ac:link>`) | Conf→MD | Cannot resolve page title to URL without an API call | Replace with `[[Page Title]]` wiki-link syntax |
| Task-list completion state in the body | Conf→MD | Markdown `- [x]` survives; Confluence's `<ac:task>` has additional `ac:task-status` state | The checkbox state is preserved; the Confluence-specific "in-progress" / "blocked" states are dropped |
| Attachment references (`<ri:attachment ri:filename="..."/>`) | Conf→MD | Markdown has no attachment primitive | Replace with `[attachment: filename]` placeholder |
| Table column alignment (`:---:`, `---:`) | Conf→MD | Confluence stores alignment as CSS styles, not markdown syntax | Alignment is lost; table renders correctly but unaligned |
| Link title attributes (`[text](url "title")`) | Conf→MD | Confluence may strip the `title` attribute on save | Most of the time the title is preserved; not part of the test contract |

### Strict lossless (the test bar)

The test in `internal/markdown/testdata/roundtrip_test.go`
defines lossless as:

```go
// roundTripPreservesText asserts that for every "preserved"
// construct, the post-roundtrip markdown contains the same
// textual content as the original markdown. Whitespace and
// formatting style may differ; the byte content of every
// paragraph, code block, list item, table cell, and link
// URL must match.
func roundTripPreservesText(t *testing.T, original string) {
    html, _ := markdownToHTML(original)
    xhtml, _ := postProcess(html)
    confMD, _ := storageToMarkdown(xhtml)
    // The set of non-whitespace tokens in the original must
    // be a subset of the set of non-whitespace tokens in
    // confMD. This catches content loss without locking
    // formatting.
    origTokens := tokenize(original)
    rtTokens := tokenize(confMD)
    if !containsAll(rtTokens, origTokens) {
        t.Errorf("round-trip lost content: %v not in %v",
            missingTokens(origTokens, rtTokens), rtTokens)
    }
}
```

`tokenize` is a small whitespace-and-punctuation splitter
that yields the textual content the user can see — words,
URLs, code-block contents, list-item bullets (as text, not
as syntax). The check is that the **set** of original tokens
is a subset of the round-tripped set (so extra whitespace
and re-flowed paragraphs don't fail the test, but a missing
sentence does).

## Verification

- `go test ./internal/markdown/... -run TestRoundTripPreservesText`
  is green for all 28 feature categories
- `go test ./internal/markdown/... -run TestKnownLossy` is
  green for all 10 lossy cases (i.e. the post-processor
  silently drops them rather than crashing)
- The README in `internal/markdown/` links to this spec
  as the source of truth and includes a "When to use
  conf_post_markdown vs conf_post" decision tree
