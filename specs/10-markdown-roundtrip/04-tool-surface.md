# 10-markdown-roundtrip — New tool surface

## Overview

The v2 feature requires **two new tools** for round-trip
operation. They are thin wrappers over the existing CRUD
tools that add the conversion step in front of (or behind)
the wire call. This spec defines their input shapes,
behavior, and where they sit in the existing 10-tool
surface.

## Sources

- `specs/05-tool-surface-design/01-output-formats.md` (the
  existing TOON/JMESPath/truncation pipeline that the new
  tools reuse)
- `specs/05-tool-surface-design/02-args-shapes.md` (the
  existing args-struct convention with `jsonschema` tags
  and example values)
- The `acon` reference for what these tools feel like from
  the user side

## Spec

### Two new tools, 12 total

The existing 10 tools are unchanged. The two new tools
are:

| Tool | HTTP method | Path | Purpose |
|---|---|---|---|
| `conf_post_markdown` | POST | `/wiki/api/v2/pages` | Create a page from a markdown body |
| `conf_put_markdown` | PUT | `/wiki/api/v2/pages/{id}` | Update a page from a markdown body |
| `conf_get_page_markdown` | GET | `/wiki/api/v2/pages/{id}?body-format=storage` then convert | Fetch a page and return its body as markdown |

(That's actually three tools. Adjust: the two upload
tools and one download tool. The user's spec said "uploads"
and "downloads", which gives us three total.)

### `conf_post_markdown`

Input args (`PostMarkdownArgs`):

```go
type PostMarkdownArgs struct {
    SpaceID  string `json:"spaceId"  jsonschema:"description=Numeric space id (required, e.g. '780763211')"`
    Title    string `json:"title"    jsonschema:"description=Page title (required)"`
    Markdown string `json:"markdown" jsonschema:"description=Markdown source (required; alternative to markdownFile)"`
    // OR
    MarkdownFile string `json:"markdownFile" jsonschema:"description=Path to a markdown file on disk (alternative to markdown; the file is read at call time)"`
    ParentID string `json:"parentId" jsonschema:"description=Optional parent page id; omit for a top-level page"`
    Status   string `json:"status"   jsonschema:"description=current (default) or archived"`
    // Standard pipeline args:
    OutputFormat string `json:"outputFormat" jsonschema:"description=Empty = TOON; 'json' for plain JSON"`
    JQ           string `json:"jq"           jsonschema:"description=Optional JMESPath filter on the created-page response"`
}
```

Behavior:

1. Resolve `markdown`: if `MarkdownFile` is set, read it
   from disk (size cap: 1 MB; error otherwise). Otherwise
   use the `Markdown` field.
2. Convert: `markdownToStorageXHTML(markdown)` (a
   3-stage pipeline from the
   `02-post-processing.md` spec).
3. Build the envelope:
   `{"representation":"storage","value": <XHTML>}`.
4. Delegate to the existing `HandlePost` (the
   Phase 7 CRUD handler) with the envelope in `Body`.
   Reuse the existing 9-step pipeline (truncate,
   JMESPath, TOON, etc.).

**Why not a brand-new handler**: the wire shape after
conversion is byte-identical to a `conf_post` body
— same envelope, same `/wiki/api/v2/pages` path. The
only new thing is the conversion step in front. Reusing
`HandlePost` keeps the 9-step pipeline shared and means
the new tool gets the same TOON/JMESPath/truncation
treatment for free.

### `conf_put_markdown`

Same shape as `conf_post_markdown` plus a required
`PageID` field. Delegates to `HandlePut` after the
conversion step. The version-number increment is
inherited from `HandlePut` (the existing handler already
does `version.number = current + 1`).

### `conf_get_page_markdown`

Input args:

```go
type GetPageMarkdownArgs struct {
    PageID    string `json:"page-id"    jsonschema:"description=Numeric page id (required)"`
    BodyFormat string `json:"body-format" jsonschema:"description=Ignored for markdown output; present for symmetry with conf_get_page_body"`
    OutputFormat string `json:"outputFormat" jsonschema:"description=Empty = TOON; 'json' for plain JSON"`
    JQ           string `json:"jq"           jsonschema:"description=Optional JMESPath filter"`
}
```

Behavior:

1. Fetch the page via the existing
   `HandleGetPageBody` path
   (`/wiki/api/v2/pages/{id}?body-format=storage`).
2. Extract the `value` field from the storage envelope.
3. Convert: `storageXHTMLToMarkdown(xhtml)` (the
   h2m-backed reverse pipeline).
4. Return either a JSON envelope
   `{"pageId": "...", "title": "...", "markdown": "..."}`
   (default, TOON-encoded) or, if `outputFormat=markdown`
   is added later, the raw markdown text.

### Tool-name compatibility with the existing 5 CRUD

The existing 5 tools (`conf_get`, `conf_post`, `conf_put`,
`conf_patch`, `conf_delete`) are unchanged. The 5
post-v1 quality-of-life tools (`conf_list_spaces`,
`conf_list_pages`, `conf_get_page_body`, `conf_search`,
`conf_help`) are unchanged. The 3 new tools bring the
total to **13**.

`hermes mcp test confluence` should list 13 tools. The
`TestNew_RegistersAll*Tools` test variable is renamed
to `TestNew_RegistersAllThirteenTools`.

## Verification

- `go test ./internal/tools/... -run TestNew` shows
  13 registered tools with the expected names
- `hermes mcp test confluence conf_post_markdown
  --spaceId=... --title=... --markdown='# Hello'`
  creates a page and returns its TOON-encoded response
- `hermes mcp test confluence conf_get_page_markdown
  --page-id=...` returns the same page's body as
  markdown
- `make check` exits 0 with all 13 tools described and
  tested
- `make build && make image` produces a working OCI
  image with the 3 new tools registered
