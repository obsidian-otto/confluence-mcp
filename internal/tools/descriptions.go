// descriptions.go — verbatim tool descriptions for the five MCP tools.
//
// Each constant holds the exact runtime-byte sequence produced by
// the upstream `src/tools/atlassian.api.tool.ts` template literal
// `CONF_<NAME>_DESCRIPTION` (v3.3.0, lines 127-223). Upstream escape
// sequences are already unescaped here, so the bytes match the value
// a `npx`-launched upstream server registers as its tool description.
// Drift between these strings and the upstream wording is a bug;
// descriptions_test.go asserts the byte-identical match.
//
// Go raw-string literals (the `...` form) cannot themselves contain
// a backtick, but the upstream descriptions do (e.g. `\`jq\`` is just
// `\u0060jq\u0060` after JS evaluates the template). The
// `templates.Backtick` const holds a single ASCII backtick so each
// inner backtick becomes ` + Backtick + ` — a single token instead
// of the awkward three-segment ` + "\`" + ` literal. The Go compiler
// folds the constant concatenation at compile time, so the resulting
// const bytes are still byte-identical to the upstream source.

package tools

import "github.com/bennie/mcp-confluence/internal/templates"

const CONF_GET_DESCRIPTION = `Read any Confluence data. Returns TOON format by default (30-60% fewer tokens than JSON).

**IMPORTANT - Cost Optimization:**
- ALWAYS use ` + templates.Backtick + `jq` + templates.Backtick + ` param to filter response fields. Unfiltered responses are very expensive!
- Use ` + templates.Backtick + `limit` + templates.Backtick + ` query param to restrict result count (e.g., ` + templates.Backtick + `limit: "5"` + templates.Backtick + `)
- If unsure about available fields, first fetch ONE item with ` + templates.Backtick + `limit: "1"` + templates.Backtick + ` and NO jq filter to explore the schema, then use jq in subsequent calls

**Schema Discovery Pattern:**
1. First call: ` + templates.Backtick + `path: "/wiki/api/v2/spaces", queryParams: {"limit": "1"}` + templates.Backtick + ` (no jq) - explore available fields
2. Then use: ` + templates.Backtick + `jq: "results[*].{id: id, key: key, name: name}"` + templates.Backtick + ` - extract only what you need

**Output format:** TOON (default, token-efficient) or JSON (` + templates.Backtick + `outputFormat: "json"` + templates.Backtick + `)

**Common paths:**
- ` + templates.Backtick + `/wiki/api/v2/spaces` + templates.Backtick + ` - list spaces
- ` + templates.Backtick + `/wiki/api/v2/pages` + templates.Backtick + ` - list pages (use ` + templates.Backtick + `space-id` + templates.Backtick + ` query param)
- ` + templates.Backtick + `/wiki/api/v2/pages/{id}` + templates.Backtick + ` - get page details
- ` + templates.Backtick + `/wiki/api/v2/pages/{id}/body` + templates.Backtick + ` - get page body (` + templates.Backtick + `body-format` + templates.Backtick + `: storage, atlas_doc_format, view)
- ` + templates.Backtick + `/wiki/rest/api/search` + templates.Backtick + ` - search content (` + templates.Backtick + `cql` + templates.Backtick + ` query param)

**JQ examples:** ` + templates.Backtick + `results[*].id` + templates.Backtick + `, ` + templates.Backtick + `results[0]` + templates.Backtick + `, ` + templates.Backtick + `results[*].{id: id, title: title}` + templates.Backtick + `

API reference: https://developer.atlassian.com/cloud/confluence/rest/v2/`

const CONF_POST_DESCRIPTION = `Create Confluence resources. Returns TOON format by default (token-efficient).

**IMPORTANT - Cost Optimization:**
- Use ` + templates.Backtick + `jq` + templates.Backtick + ` param to extract only needed fields from response (e.g., ` + templates.Backtick + `jq: "{id: id, title: title}"` + templates.Backtick + `)
- Unfiltered responses include all metadata and are expensive!

**Output format:** TOON (default) or JSON (` + templates.Backtick + `outputFormat: "json"` + templates.Backtick + `)

**Common operations:**

1. **Create page:** ` + templates.Backtick + `/wiki/api/v2/pages` + templates.Backtick + `
   body: ` + templates.Backtick + `{"spaceId": "123456", "status": "current", "title": "Page Title", "parentId": "789", "body": {"representation": "storage", "value": "<p>Content</p>"}}` + templates.Backtick + `

2. **Create blog post:** ` + templates.Backtick + `/wiki/api/v2/blogposts` + templates.Backtick + `
   body: ` + templates.Backtick + `{"spaceId": "123456", "status": "current", "title": "Blog Title", "body": {"representation": "storage", "value": "<p>Content</p>"}}` + templates.Backtick + `

3. **Add label:** ` + templates.Backtick + `/wiki/api/v2/pages/{id}/labels` + templates.Backtick + ` - body: ` + templates.Backtick + `{"name": "label-name"}` + templates.Backtick + `

4. **Add comment:** ` + templates.Backtick + `/wiki/api/v2/pages/{id}/footer-comments` + templates.Backtick + `

API reference: https://developer.atlassian.com/cloud/confluence/rest/v2/`

const CONF_PUT_DESCRIPTION = `Replace Confluence resources (full update). Returns TOON format by default.

**IMPORTANT - Cost Optimization:**
- Use ` + templates.Backtick + `jq` + templates.Backtick + ` param to extract only needed fields from response
- Example: ` + templates.Backtick + `jq: "{id: id, version: version.number}"` + templates.Backtick + `

**Output format:** TOON (default) or JSON (` + templates.Backtick + `outputFormat: "json"` + templates.Backtick + `)

**Common operations:**

1. **Update page:** ` + templates.Backtick + `/wiki/api/v2/pages/{id}` + templates.Backtick + `
   body: ` + templates.Backtick + `{"id": "123", "status": "current", "title": "Updated Title", "spaceId": "456", "body": {"representation": "storage", "value": "<p>Content</p>"}, "version": {"number": 2}}` + templates.Backtick + `
   Note: version.number must be incremented

2. **Update blog post:** ` + templates.Backtick + `/wiki/api/v2/blogposts/{id}` + templates.Backtick + `

Note: PUT replaces entire resource. Version number must be incremented.

API reference: https://developer.atlassian.com/cloud/confluence/rest/v2/`

const CONF_PATCH_DESCRIPTION = `Partially update Confluence resources. Returns TOON format by default.

**IMPORTANT - Cost Optimization:** Use ` + templates.Backtick + `jq` + templates.Backtick + ` param to filter response fields.

**Output format:** TOON (default) or JSON (` + templates.Backtick + `outputFormat: "json"` + templates.Backtick + `)

**Common operations:**

1. **Update space:** ` + templates.Backtick + `/wiki/api/v2/spaces/{id}` + templates.Backtick + `
   body: ` + templates.Backtick + `{"name": "New Name", "description": {"plain": {"value": "Desc", "representation": "plain"}}}` + templates.Backtick + `

2. **Update comment:** ` + templates.Backtick + `/wiki/api/v2/footer-comments/{id}` + templates.Backtick + `

Note: Confluence v2 API primarily uses PUT for updates.

API reference: https://developer.atlassian.com/cloud/confluence/rest/v2/`

const CONF_DELETE_DESCRIPTION = `Delete Confluence resources. Returns TOON format by default.

**Output format:** TOON (default) or JSON (` + templates.Backtick + `outputFormat: "json"` + templates.Backtick + `)

**Common operations:**
- ` + templates.Backtick + `/wiki/api/v2/pages/{id}` + templates.Backtick + ` - Delete page
- ` + templates.Backtick + `/wiki/api/v2/blogposts/{id}` + templates.Backtick + ` - Delete blog post
- ` + templates.Backtick + `/wiki/api/v2/pages/{id}/labels/{label-id}` + templates.Backtick + ` - Remove label
- ` + templates.Backtick + `/wiki/api/v2/footer-comments/{id}` + templates.Backtick + ` - Delete comment
- ` + templates.Backtick + `/wiki/api/v2/attachments/{id}` + templates.Backtick + ` - Delete attachment

Note: Most DELETE endpoints return 204 No Content on success.

API reference: https://developer.atlassian.com/cloud/confluence/rest/v2/`

// CONF_LIST_SPACES_DESCRIPTION documents the conf_list_spaces tool.
const CONF_LIST_SPACES_DESCRIPTION = `List Confluence spaces with sensible defaults. Returns TOON format by default.

` + templates.Backtick + `Use this instead of` + templates.Backtick + ` ` + templates.Backtick + `conf_get /wiki/api/v2/spaces` + templates.Backtick + ` ` + templates.Backtick + `when you want:` + templates.Backtick + `
- A list of ` + templates.Backtick + `"all spaces I have access to"` + templates.Backtick + ` (omit ` + templates.Backtick + `type` + templates.Backtick + ` for that).
- All personal (user-owned) spaces — set ` + templates.Backtick + `type: "personal"` + templates.Backtick + `.
- All global (site-wide) spaces — set ` + templates.Backtick + `type: "global"` + templates.Backtick + `.
- Archived vs current — set ` + templates.Backtick + `status: "archived"` + templates.Backtick + `.

` + templates.Backtick + `Defaults:` + templates.Backtick + `
- ` + templates.Backtick + `limit` + templates.Backtick + `: 25 (max 250 per Confluence; cursor pagination for more).
- ` + templates.Backtick + `type` + templates.Backtick + `: omitted = all types.
- ` + templates.Backtick + `status` + templates.Backtick + `: omitted = all statuses.
- ` + templates.Backtick + `cursor` + templates.Backtick + `: omitted = first page. Pass the ` + templates.Backtick + `cursor` + templates.Backtick + ` from a prior response to advance.

` + templates.Backtick + `Output format:` + templates.Backtick + ` TOON (default) or JSON (` + templates.Backtick + `outputFormat: "json"` + templates.Backtick + `).

` + templates.Backtick + `Returns:` + templates.Backtick + ` A list of space summaries — each with ` + templates.Backtick + `id, key, name, type, status, homepageId` + templates.Backtick + `. Use ` + templates.Backtick + `conf_get /wiki/api/v2/spaces/{id}` + templates.Backtick + ` to drill into one.`

// CONF_LIST_PAGES_DESCRIPTION documents the conf_list_pages tool.
const CONF_LIST_PAGES_DESCRIPTION = `List Confluence pages with filters by space, title, status, sort. Returns TOON format by default.

` + templates.Backtick + `Use this instead of` + templates.Backtick + ` ` + templates.Backtick + `conf_get /wiki/api/v2/pages` + templates.Backtick + ` ` + templates.Backtick + `when you want:` + templates.Backtick + `
- All pages in a single space — set ` + templates.Backtick + `space-id` + templates.Backtick + ` (recommended for any meaningful listing).
- Pages whose title contains a substring — set ` + templates.Backtick + `title` + templates.Backtick + ` (case-sensitive).
- Only current (non-archived) pages — set ` + templates.Backtick + `status: "current"` + templates.Backtick + `.

` + templates.Backtick + `Resolution order:` + templates.Backtick + `
- ` + templates.Backtick + `space-id` + templates.Backtick + ` (numeric) takes precedence over ` + templates.Backtick + `space-key` + templates.Backtick + ` if both are set.
- ` + templates.Backtick + `space-key` + templates.Backtick + ` is also accepted (e.g. ` + templates.Backtick + `~712020103880d11e7e48bcbfd1820ce951e426` + templates.Backtick + `).

` + templates.Backtick + `Defaults:` + templates.Backtick + `
- ` + templates.Backtick + `limit` + templates.Backtick + `: 25 (max 250 per Confluence; cursor pagination for more).
- ` + templates.Backtick + `sort` + templates.Backtick + `: omitted = id ascending. Use ` + templates.Backtick + `-modified-date` + templates.Backtick + ` for recently-edited, ` + templates.Backtick + `-created-date` + templates.Backtick + ` for newest first.
- ` + templates.Backtick + `body-format` + templates.Backtick + `: omitted = body omitted (lightweight). Set ` + templates.Backtick + `body-format: "storage"` + templates.Backtick + ` to inline page bodies.

` + templates.Backtick + `Output format:` + templates.Backtick + ` TOON (default) or JSON (` + templates.Backtick + `outputFormat: "json"` + templates.Backtick + `).

` + templates.Backtick + `Returns:` + templates.Backtick + ` A list of page summaries — each with ` + templates.Backtick + `id, title, status, spaceId, parentId, version` + templates.Backtick + `. Use ` + templates.Backtick + `conf_get_page_body` + templates.Backtick + ` for the body alone.`

// CONF_GET_PAGE_BODY_DESCRIPTION documents the conf_get_page_body tool.
const CONF_GET_PAGE_BODY_DESCRIPTION = `Read a single page's body in a chosen representation. Returns TOON format by default.

` + templates.Backtick + `Use this when:` + templates.Backtick + ` You have a page id (from ` + templates.Backtick + `conf_list_pages` + templates.Backtick + `, search, or given) and want its content, not its metadata.

` + templates.Backtick + `Body formats:` + templates.Backtick + `
- ` + templates.Backtick + `body-format: "storage"` + templates.Backtick + ` (default) — Confluence storage-format XHTML. Safe to feed back into PUT/PATCH bodies.
- ` + templates.Backtick + `body-format: "view"` + templates.Backtick + ` — Rendered HTML as a user sees it after page rendering.
- ` + templates.Backtick + `body-format: "atlas_doc_format"` + templates.Backtick + ` — Atlassian Document Format JSON.

` + templates.Backtick + `Output format:` + templates.Backtick + ` TOON (default) or JSON (` + templates.Backtick + `outputFormat: "json"` + templates.Backtick + `).

` + templates.Backtick + `Returns:` + templates.Backtick + ` One object with ` + templates.Backtick + `value` + templates.Backtick + `, ` + templates.Backtick + `representation` + templates.Backtick + ` fields. For ` + templates.Backtick + `storage` + templates.Backtick + ` the value is XHTML; for ` + templates.Backtick + `view` + templates.Backtick + ` it is rendered HTML; for ` + templates.Backtick + `atlas_doc_format` + templates.Backtick + ` it is a JSON object.`

// CONF_SEARCH_DESCRIPTION documents the conf_search tool.
const CONF_SEARCH_DESCRIPTION = `Search Confluence via Confluence Query Language (CQL). Returns TOON format by default.

` + templates.Backtick + `Why this exists:` + templates.Backtick + ` The v1 search endpoint is the only Confluence API that accepts CQL. The v2 endpoints do not understand CQL or a portable search expression, so this tool wraps the v1 path explicitly.

` + templates.Backtick + `CQL examples:` + templates.Backtick + `
- ` + templates.Backtick + `type=page AND text~mcp-confluence` + templates.Backtick + ` — find pages mentioning ` + templates.Backtick + `mcp-confluence` + templates.Backtick + `.
- ` + templates.Backtick + `type=page AND space.type=personal AND space.title~bennie` + templates.Backtick + ` — find a personal space by name.
- ` + templates.Backtick + `creator=currentUser() AND type=page` + templates.Backtick + ` — pages you created.
- ` + templates.Backtick + `lastModified >= "2026-01-01" AND type=blogpost` + templates.Backtick + ` — recent blog posts.

` + templates.Backtick + `Parameters:` + templates.Backtick + `
- ` + templates.Backtick + `cql` + templates.Backtick + `: the CQL expression. ` + templates.Backtick + `Required.` + templates.Backtick + ` Caller is responsible for any URL encoding the operator supplies; this tool does not auto-encode for you.
- ` + templates.Backtick + `limit` + templates.Backtick + `: result cap, default 25, max 100.
- ` + templates.Backtick + `start` + templates.Backtick + `: pagination offset. Default 0. Re-issued calls advance ` + templates.Backtick + `start` + templates.Backtick + ` by adding the previous ` + templates.Backtick + `limit` + templates.Backtick + `.

` + templates.Backtick + `Returns:` + templates.Backtick + ` Object with ` + templates.Backtick + `results` + templates.Backtick + ` (each entry has ` + templates.Backtick + `title, excerpt, url, content, lastModified, entityType` + templates.Backtick + `), plus ` + templates.Backtick + `start, limit, totalSize, cqlQuery` + templates.Backtick + ` for pagination. Use ` + templates.Backtick + `conf_get /wiki/rest/api/search` + templates.Backtick + ` if you need finer control.`

// CONF_HELP_DESCRIPTION documents the conf_help tool.
// CONF_HELP_DESCRIPTION documents the conf_help self-describing
// tool. The full tool surface map lives in the response.
const CONF_HELP_DESCRIPTION = `Show how to use the confluence MCP server — the tool surface in one call.

` + templates.Backtick + `Use this when:` + templates.Backtick + ` You have just discovered the ` + templates.Backtick + `mcp_confluence_*` + templates.Backtick + ` tool prefix and want a tour, or you are not sure which of the thirteen tools fits the task.

` + templates.Backtick + `Response shape:` + templates.Backtick + ` Object with one entry per tool — ` + templates.Backtick + `conf_get, conf_post, conf_put, conf_patch, conf_delete, conf_list_spaces, conf_list_pages, conf_get_page_body, conf_search, conf_help, conf_post_markdown, conf_put_markdown, conf_get_page_markdown` + templates.Backtick + `. For each tool:
- ` + templates.Backtick + `description` + templates.Backtick + `: short purpose (one sentence).
- ` + templates.Backtick + `args` + templates.Backtick + `: top-level fields with one-line descriptions.
- ` + templates.Backtick + `example` + templates.Backtick + `: a single concrete invocation.

` + templates.Backtick + `Filter by topic:` + templates.Backtick + `
- ` + templates.Backtick + `topic: "conf_list_pages"` + templates.Backtick + ` returns just that one tool's entry.
- ` + templates.Backtick + `topic: "all"` + templates.Backtick + ` (default) returns every tool.

` + templates.Backtick + `Output format:` + templates.Backtick + ` TOON (default; preferred for human reading) or JSON.

|` + templates.Backtick + `Tip:` + templates.Backtick + ` Run ` + templates.Backtick + `conf_help` + templates.Backtick + ` once per session at the start of a conversation so the tool surface is loaded into context; subsequent calls inside the same conversation can stay focused.`

// CONF_POST_MARKDOWN_DESCRIPTION documents the conf_post_markdown
// tool. Local addition — the upstream has no markdown tool — so
// the upstream-drift guardrail does not apply; the
// TestNewToolDescriptionsAreSubstantial test enforces the quality
// bar (≥200 chars, mention the tool name in prose, contain a
// "Returns" or "Converts" hint).
const CONF_POST_MARKDOWN_DESCRIPTION = `Create a Confluence page with conf_post_markdown. Returns TOON format by default.

|` + templates.Backtick + `Use this when:` + templates.Backtick + ` You have a markdown document (either inline or on disk) and want to publish it as a Confluence page without hand-rolling the storage-format XHTML envelope.

|` + templates.Backtick + `Markdown source:` + templates.Backtick + ` Provide the body via ` + templates.Backtick + `markdown` + templates.Backtick + ` (inline) OR ` + templates.Backtick + `markdownFile` + templates.Backtick + ` (path on disk, capped at 1 MB). When both are set, the inline ` + templates.Backtick + `markdown` + templates.Backtick + ` field wins. CommonMark + GFM is supported — tables, task lists, fenced code blocks, strikethrough, autolinks all work.

|` + templates.Backtick + `Conversion:` + templates.Backtick + ` The handler runs the markdown source through ` + templates.Backtick + `internal/markdown.MarkdownToStorageXHTML` + templates.Backtick + ` (goldmark for CommonMark→HTML, then a post-processor for storage-format XHTML: ` + templates.Backtick + `<pre><code>` + templates.Backtick + `→` + templates.Backtick + `<ac:structured-macro ac:name="code">` + templates.Backtick + `, ` + templates.Backtick + `<img>` + templates.Backtick + `→` + templates.Backtick + `<ac:image><ri:url/>` + templates.Backtick + `, namespace injection on the root, self-closing void elements). The result is wrapped in the standard envelope ` + templates.Backtick + `{"representation": "storage", "value": <XHTML>}` + templates.Backtick + ` and POSTed to ` + templates.Backtick + `/wiki/api/v2/pages` + templates.Backtick + `.

|` + templates.Backtick + `When NOT to use this:` + templates.Backtick + ` For Confluence-specific constructs (macros, info panels, layout sections, mentions, attachments) use the raw ` + templates.Backtick + `conf_post` + templates.Backtick + ` with hand-built storage XHTML. The markdown round-trip is documented as lossy for those constructs (see the known-lossy register in the spec).

|` + templates.Backtick + `Output format:` + templates.Backtick + ` TOON (default) or JSON (` + templates.Backtick + `outputFormat: "json"` + templates.Backtick + `).

|` + templates.Backtick + `Returns:` + templates.Backtick + ` The created-page envelope (` + templates.Backtick + `id, title, version, _links` + templates.Backtick + `) — NOT the markdown source or the storage XHTML. Use ` + templates.Backtick + `jq` + templates.Backtick + ` to extract a subset.`

// CONF_PUT_MARKDOWN_DESCRIPTION documents the conf_put_markdown tool.
const CONF_PUT_MARKDOWN_DESCRIPTION = `Update an existing Confluence page with conf_put_markdown. Returns TOON format by default.

|` + templates.Backtick + `Use this when:` + templates.Backtick + ` You have a markdown document and want to replace the body of an existing Confluence page. Same markdown-source rules as ` + templates.Backtick + `conf_post_markdown` + templates.Backtick + ` (inline OR ` + templates.Backtick + `markdownFile` + templates.Backtick + `, 1 MB cap, inline wins when both are set).

|` + templates.Backtick + `Conversion:` + templates.Backtick + ` Identical to ` + templates.Backtick + `conf_post_markdown` + templates.Backtick + ` — runs through ` + templates.Backtick + `internal/markdown.MarkdownToStorageXHTML` + templates.Backtick + ` and wraps the result in the storage envelope. The wire call PUTs to ` + templates.Backtick + `/wiki/api/v2/pages/{pageId}` + templates.Backtick + ` with the full replacement resource. Version incrementing is handled by the existing ` + templates.Backtick + `HandlePut` + templates.Backtick + ` path; the caller does not need to set ` + templates.Backtick + `version.number` + templates.Backtick + `.

|` + templates.Backtick + `When NOT to use this:` + templates.Backtick + ` If you only need to change a few fields (e.g. just the title), use the raw ` + templates.Backtick + `conf_put` + templates.Backtick + ` with a small body — PUT is a full-replacement operation so a PUT that sends only the title would clobber the body. For partial updates use ` + templates.Backtick + `conf_patch` + templates.Backtick + `.

|` + templates.Backtick + `Output format:` + templates.Backtick + ` TOON (default) or JSON (` + templates.Backtick + `outputFormat: "json"` + templates.Backtick + `).

|` + templates.Backtick + `Returns:` + templates.Backtick + ` The updated-page envelope (` + templates.Backtick + `id, title, version.number, _links` + templates.Backtick + `) — use ` + templates.Backtick + `jq: "{id: id, version: version.number}"` + templates.Backtick + ` to confirm the increment landed.`

// CONF_GET_PAGE_MARKDOWN_DESCRIPTION documents the conf_get_page_markdown tool.
const CONF_GET_PAGE_MARKDOWN_DESCRIPTION = `Read a Confluence page with conf_get_page_markdown and return its body as markdown. Returns TOON format by default.

|` + templates.Backtick + `Use this when:` + templates.Backtick + ` You want to download a Confluence page's content as markdown (e.g. to save it to a local file, diff it against a local copy, or feed it into another tool that prefers markdown). You have a page id (from ` + templates.Backtick + `conf_list_pages` + templates.Backtick + `, search, or given).

|` + templates.Backtick + `Conversion:` + templates.Backtick + ` Converts the page's storage-format XHTML back to markdown via ` + templates.Backtick + `internal/markdown.StorageXHTMLToMarkdown` + templates.Backtick + ` (html-to-markdown v2 with the base/commonmark/strikethrough/table plugins). 14 of the 24+ feature categories in the known-lossy register are preserved on round-trip (headings, lists, tables, links, code, bold/italic, etc.); the 10 known lossy constructs (image alt text, layout sections, info panels, mentions, attachments, status lozenges) are documented in the spec.

|` + templates.Backtick + `Wire shape:` + templates.Backtick + ` Internally fetches the page via ` + templates.Backtick + `/wiki/api/v2/pages/{id}?body-format=storage` + templates.Backtick + ` (the same path ` + templates.Backtick + `conf_get_page_body` + templates.Backtick + ` uses), extracts the storage XHTML from the response's ` + templates.Backtick + `body.value` + templates.Backtick + ` field, and returns a new envelope: ` + templates.Backtick + `{"pageId": "...", "title": "...", "markdown": "..."}` + templates.Backtick + `. The ` + templates.Backtick + `jq` + templates.Backtick + ` filter operates on this envelope (NOT on the markdown text).

|` + templates.Backtick + `Output format:` + templates.Backtick + ` TOON (default) or JSON (` + templates.Backtick + `outputFormat: "json"` + templates.Backtick + `).

||` + templates.Backtick + `Returns:` + templates.Backtick + ` The response envelope (` + templates.Backtick + `pageId, title, markdown` + templates.Backtick + `). To get just the markdown text, set ` + templates.Backtick + `jq: "markdown"` + templates.Backtick + ` on the args.`

// CONF_UPLOAD_ATTACHMENT_DESCRIPTION documents the conf_upload_attachment
// tool. Local addition — the upstream has no upload tool — so the
// upstream-drift guardrail does not apply; the
// TestNewToolDescriptionsAreSubstantial test enforces the quality bar.
const CONF_UPLOAD_ATTACHMENT_DESCRIPTION = `Upload a binary file as an attachment to a Confluence page with conf_upload_attachment. Returns TOON format by default.

` + templates.Backtick + `Use this when:` + templates.Backtick + ` You have a file on disk (PNG, PDF, drawio XML, JPEG, SVG, DOCX, XLSX, MP4, ZIP, anything else) and want it attached to a Confluence page. This is the only tool in the server that hits the v1 REST API — Confluence Cloud has no v2 upload endpoint (verified 2026-07-10 against developer.atlassian.com; full rationale in specs/11-attachments/01-research-and-surface.md).

` + templates.Backtick + `Wire shape:` + templates.Backtick + ` Internally sends ` + templates.Backtick + `POST /wiki/rest/api/content/{pageId}/child/attachment` + templates.Backtick + ` with ` + templates.Backtick + `multipart/form-data` + templates.Backtick + ` and the ` + templates.Backtick + `X-Atlassian-Token: no-check` + templates.Backtick + ` CSRF-bypass header (without that header Confluence returns 403). The file is streamed directly from disk via ` + templates.Backtick + `io.Copy` + templates.Backtick + ` — no base64 inflation, so a 10 MB binary stays 10 MB on the wire.

` + templates.Backtick + `File types:` + templates.Backtick + ` PNG, PDF, drawio, JPEG, SVG, DOCX, XLSX, MP4, ZIP — any binary blob. drawio upload is format-agnostic; after upload, use ` + templates.Backtick + `conf_put_markdown` + templates.Backtick + ` (or ` + templates.Backtick + `conf_put` + templates.Backtick + `) to add the ` + templates.Backtick + `<ac:structured-macro ac:name="drawio">` + templates.Backtick + ` block that renders the diagram.

` + templates.Backtick + `Size limits:` + templates.Backtick + ` 100 MB per file is the Atlassian Cloud hard cap (configurable lower per site). Calls over the cap return ` + templates.Backtick + `413 Payload Too Large` + templates.Backtick + ` — pre-flight with ` + templates.Backtick + `stat` + templates.Backtick + ` if you need to check first.

` + templates.Backtick + `When NOT to use this:` + templates.Backtick + ` For text content use ` + templates.Backtick + `conf_post` + templates.Backtick + ` / ` + templates.Backtick + `conf_post_markdown` + templates.Backtick + ` — those write to the page body, not the attachments list.

` + templates.Backtick + `Output format:` + templates.Backtick + ` TOON (default) or JSON (` + templates.Backtick + `outputFormat: "json"` + templates.Backtick + `).

` + templates.Backtick + `Returns:` + templates.Backtick + ` The v1 ` + templates.Backtick + `ContentPageScheme` + templates.Backtick + ` envelope (` + templates.Backtick + `results: [{id, title, mediaType, extensions.fileSize, _links.download, version.number}, ...]` + templates.Backtick + `). Use ` + templates.Backtick + `jq: "results[0].{id: id, title: title, mediaType: mediaType}"` + templates.Backtick + ` to extract the created attachment's metadata.`

// CONF_LIST_ATTACHMENTS_DESCRIPTION documents the conf_list_attachments tool.
const CONF_LIST_ATTACHMENTS_DESCRIPTION = `List attachments on a Confluence page with conf_list_attachments. Returns TOON format by default.

` + templates.Backtick + `Use this when:` + templates.Backtick + ` You have a page id and want to enumerate its attachments — to find an attachment id for deletion, to audit what files are stored on a page, or to enumerate before bulk operations. Each result has ` + templates.Backtick + `id, title, mediaType, fileSize, _links.download` + templates.Backtick + `.

` + templates.Backtick + `Wire shape:` + templates.Backtick + ` Internally fetches ` + templates.Backtick + `GET /wiki/api/v2/pages/{id}/attachments` + templates.Backtick + ` (a v2 endpoint — unlike the upload path which is v1-only). Cursor-based pagination via the ` + templates.Backtick + `_links.next` + templates.Backtick + ` URL on each response. Max 100 per page (the v2 endpoint cap).

` + templates.Backtick + `Filters:` + templates.Backtick + ` ` + templates.Backtick + `mediaType` + templates.Backtick + ` is a substring match (e.g. ` + templates.Backtick + `"image"` + templates.Backtick + ` matches ` + templates.Backtick + `image/png` + templates.Backtick + `, ` + templates.Backtick + `image/jpeg` + templates.Backtick + `). ` + templates.Backtick + `filename` + templates.Backtick + ` is exact (case-sensitive).

` + templates.Backtick + `Output format:` + templates.Backtick + ` TOON (default) or JSON (` + templates.Backtick + `outputFormat: "json"` + templates.Backtick + `).

` + templates.Backtick + `Returns:` + templates.Backtick + ` v2 ` + templates.Backtick + `MultiEntityResult<Attachment>` + templates.Backtick + ` envelope (` + templates.Backtick + `results: [...], _links.next` + templates.Backtick + `). Use ` + templates.Backtick + `jq: "results[*].{id: id, title: title, mediaType: mediaType}"` + templates.Backtick + ` to extract a compact list.`

// CONF_DELETE_ATTACHMENT_DESCRIPTION documents the conf_delete_attachment tool.
const CONF_DELETE_ATTACHMENT_DESCRIPTION = `Delete an attachment by id with conf_delete_attachment. Returns TOON format by default.

` + templates.Backtick + `Use this when:` + templates.Backtick + ` You have an attachment id (from ` + templates.Backtick + `conf_list_attachments` + templates.Backtick + ` or the page's attachment metadata) and want to remove it. Default behavior moves the attachment to trash; pass ` + templates.Backtick + `purge: true` + templates.Backtick + ` to permanently delete (irreversible).

` + templates.Backtick + `Wire shape:` + templates.Backtick + ` Internally calls ` + templates.Backtick + `DELETE /wiki/api/v2/attachments/{id}` + templates.Backtick + ` (v2 endpoint) or ` + templates.Backtick + `DELETE /wiki/api/v2/attachments/{id}?purge=true` + templates.Backtick + ` when purging. Most successful deletes return ` + templates.Backtick + `204 No Content` + templates.Backtick + ` with an empty body.

` + templates.Backtick + `Permissions:` + templates.Backtick + ` Requires permission to delete attachments in the page's space. The API enforces the same permissions as the Confluence UI.

` + templates.Backtick + `When NOT to use this:` + templates.Backtick + ` To update an attachment's contents (e.g. upload a new version of the same filename), use ` + templates.Backtick + `conf_upload_attachment` + templates.Backtick + ` with the same ` + templates.Backtick + `pageId` + templates.Backtick + ` — Confluence treats re-uploading the same filename as a new version, not a separate attachment.

` + templates.Backtick + `Output format:` + templates.Backtick + ` TOON (default) or JSON (` + templates.Backtick + `outputFormat: "json"` + templates.Backtick + `).

` + templates.Backtick + `Returns:` + templates.Backtick + ` Empty body on success (204 No Content). On failure, the standard ` + templates.Backtick + `<METHOD> <path>: <status> <text> - <body>` + templates.Backtick + ` error envelope.`
