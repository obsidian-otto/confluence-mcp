# 10-markdown-roundtrip — Post-processing pipeline

## Overview

Goldmark produces **CommonMark HTML** — `<pre><code>` for
code blocks, plain `<img>` for images, plain `<a>` for links.
Confluence expects **storage-format XHTML** — `<ac:structured-macro>`
wrapping for code blocks, `<ac:image><ri:url/></ac:image>` for
images, `<ac:link><ri:url/></ac:link>` for links, and
`xmlns:ac="..."` on the root. This spec defines the
post-processing pass that bridges the two.

## Sources

- `confluence.atlassian.com/doc/confluence-storage-format-790796544.html`
  — canonical definition of storage format
- `confluence.atlassian.com/conf59/confluence-storage-format-for-macros-792499117.html`
  — `<ac:structured-macro>` reference (the `code` macro, the
  `status` macro, the `note` / `info` / `warning` / `tip`
  panel macros)
- `developer.atlassian.com/cloud/confluence/rest/v2/intro/` —
  confirms the v2 API accepts `{representation: "storage",
  value: <XHTML>}` for page bodies
- goldmark AST node reference: `goldmark/ast` package docs

## Spec

### Pipeline shape

The upload path is a 3-stage pipeline:

```
markdown bytes
  │  (1) goldmark.Render — CommonMark → HTML
  ▼
<html>...HTML...</html>     (goldmark output)
  │  (2) post-processor — HTML → storage XHTML
  ▼
<ac:...>...</ac:...>        (storage format, wire-ready)
  │  (3) envelope builder — wrap in
  │      {"representation":"storage","value":<above>}
  ▼
{"representation":"storage","value":"<ac:...>...</ac:...>"}
```

Stage (2) is the only one with non-trivial logic. Stages (1)
and (3) are 1-2 LOC each.

### Stage (2) — the post-processor

The post-processor walks the HTML with goquery and applies
five transformation rules in order:

| # | Source (goldmark HTML) | Target (storage XHTML) |
|---|---|---|
| 1 | `<pre><code class="language-X">CODE</code></pre>` | `<ac:structured-macro ac:name="code" ac:schema-version="1"><ac:parameter ac:name="language">X</ac:parameter><ac:plain-text-body><![CDATA[CODE]]></ac:plain-text-body></ac:structured-macro>` |
| 2 | `<img src="URL" alt="ALT">` (external image) | `<ac:image><ri:url ri:value="URL"/></ac:image>` (alt text is dropped — see known-lossy register) |
| 3 | `<a href="URL">TEXT</a>` (external link) | `<ac:link><ri:url ri:value="URL"/></ac:link><ac:plain-text-link-body><![CDATA[TEXT]]></ac:plain-text-link-body>` (or simpler `<a href="URL">TEXT</a>` — Confluence accepts both) |
| 4 | (root-level) | Inject `xmlns:ac="http://atlassian.com/content"` and `xmlns:ri="http://atlassian.com/resource/identifier"` onto the `<html>` element if absent |
| 5 | `<br>`, `<hr>`, `<img>` (in code blocks) | Ensure self-closing form `<br/>`, `<hr/>`, `<img/>` — `encoding/xml` rewrites these automatically when round-tripping through `xml.Marshal` |

### Rule 1 — code blocks in detail

The single most-confused rule, because there are two valid
storage-format shapes depending on whether a language is
specified:

```xml
<!-- WITH language -->
<ac:structured-macro ac:name="code" ac:schema-version="1">
  <ac:parameter ac:name="language">go</ac:parameter>
  <ac:plain-text-body><![CDATA[package main
func main() {}
]]></ac:plain-text-body>
</ac:structured-macro>

<!-- WITHOUT language (no class attribute on the <code>) -->
<ac:structured-macro ac:name="code" ac:schema-version="1">
  <ac:plain-text-body><![CDATA[...code...]]></ac:plain-text-body>
</ac:structured-macro>
```

The CDATA wrapper is **required** — without it, the
`<` and `&` characters inside the code become XML
parse errors. The `ac:plain-text-body` element is a
property of the `code` macro specifically; the generic
`ac:rich-text-body` is for macros that contain rich
content (e.g. expand / panel).

### Rule 3 — links — keep it simple

The minimal-effort path for external links is to leave them
as plain `<a href="URL">TEXT</a>` — Confluence accepts
this on the write path and the renderer turns them into
proper linkified content. The `<ac:link><ri:url/>` form
is only needed if the link target is **inside Confluence**
(a page reference, an attachment, a mention). Since
markdown links in the wild are overwhelmingly external,
the post-processor leaves `<a href>` as-is and only wraps
in `<ac:link>` when the URL is detected as an internal
`/wiki/...` path or a `spacekey:pagetitle` style
reference (which goldmark does not produce anyway, so
this is a no-op for the typical case).

### Rule 4 — namespace injection

Goldmark's HTML output is `<html><head>...</head><body>...</body></html>`.
The Confluence API is lenient about whether the root is
`<html>` or just a fragment, but it REQUIRES the `xmlns:ac`
and `xmlns:ri` declarations somewhere accessible to the
macro elements. We inject them on the root element if
absent, which keeps the XHTML self-contained. goquery
exposes this as `doc.Find("html").Each(func(_ int, s
*goquery.Selection) { s.SetAttr("xmlns:ac", "...") })`.

### Stage (2) — implementation boundary

The post-processor lives in
`internal/markdown/postprocess.go` and is **exported** as
`Apply(html string) (string, error)`. The signature is
deliberately `(html, xhtml)` (not `(ast, ast)`) so the
function is easy to test with a hand-written input string
and a hand-written expected output, and so it can be
called on hand-built XHTML in tests (e.g. when a user
passes raw XHTML as a fallback path).

### Why three stages, not two

The split between (1) and (2) exists because (1) is a
mature, third-party CommonMark renderer with a
four-thousand-test corpus and (2) is a Confluence-specific
post-processor that we own. The split between (2) and
(3) exists because the envelope shape
(`{representation, value}`) is a wire-format concern that
already lives in the existing CRUD tools; if it changes
in a future Confluence API version, the change is
isolated to the envelope helper, not the post-processor.

## Verification

- `go test ./internal/markdown/...` runs the 28-roundtrip
  test suite drawn from `acon/testdata/roundtrip-test.sh`
- `go test ./internal/markdown/... -update` regenerates
  golden files when intentional changes to the
  post-processor are committed
- Manual: `cat fixtures/hello.md | go run ./cmd/md2conf
  -confluence-out` produces XHTML that, pasted into a
  Confluence page, renders identically to the source
  markdown in the Confluence WYSIWYG view
