# 10-markdown-roundtrip — Reading guide

The v2 feature for `confluence-mcp`: bidirectional markdown
↔ Confluence storage format conversion, exposed as three
new MCP tools (`conf_post_markdown`, `conf_put_markdown`,
`conf_get_page_markdown`).

## Reading order

1. `01-library-survey.md` — which Go libraries we picked
   and why. Read first; everything else assumes
   `goldmark` for the upload direction and
   `html-to-markdown/v2` for the download direction.
2. `02-post-processing.md` — the conversion pipeline
   shape. Read this before looking at the code; the
   3-stage pipeline is the architectural decision the
   rest of the package follows.
3. `03-known-lossy-constructs.md` — what survives
   round-trip and what doesn't. **Read this if you are
   a user** deciding whether to call
   `conf_post_markdown` or fall back to raw `conf_post`.
4. `04-tool-surface.md` — the three new MCP tools, their
   input shapes, and how they delegate to the existing
   CRUD handlers. Read this when wiring up
   `RegisterAll` in Phase 8.
5. `05-test-strategy.md` — the 4-layer test plan and
   the golden-file layout. Read this when writing the
   per-stage unit tests in Phase 13.

## Status (2026-07-10)

| Spec | Topic | Status |
|---|---|---|
| 01-library-survey.md | library selection | Spec complete |
| 02-post-processing.md | conversion pipeline | Spec complete |
| 03-known-lossy-constructs.md | lossless contract | Spec complete |
| 04-tool-surface.md | new MCP tools | Spec complete |
| 05-test-strategy.md | test layers + goldens | Spec complete |

## Implementation phases (added to IMPLEMENTATION_PLAN.md)

- **Phase 13** — `internal/markdown` package: 3-stage
  pipeline + 28 golden-file tests (parallel-safe with
  14)
- **Phase 14** — 3 new tool handlers
  (`conf_post_markdown`, `conf_put_markdown`,
  `conf_get_page_markdown`) + args types + descriptions
- **Phase 15** — Register the 3 new tools with the MCP
  server; expand `TestNew_RegistersAll*Tools` to
  `Thirteen`; rebuild image; smoke against live
  Confluence
