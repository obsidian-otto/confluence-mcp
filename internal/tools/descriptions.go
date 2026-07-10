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
// `\u0060jq\u0060` after JS evaluates the template). We split each
// upstream string on every backtick and rejoin with a literal "\`"
// constant; the result is a single untyped const whose bytes match
// the upstream byte-for-byte.

package tools

const CONF_GET_DESCRIPTION = `Read any Confluence data. Returns TOON format by default (30-60% fewer tokens than JSON).

**IMPORTANT - Cost Optimization:**
- ALWAYS use ` + "`" + `jq` + "`" + ` param to filter response fields. Unfiltered responses are very expensive!
- Use ` + "`" + `limit` + "`" + ` query param to restrict result count (e.g., ` + "`" + `limit: "5"` + "`" + `)
- If unsure about available fields, first fetch ONE item with ` + "`" + `limit: "1"` + "`" + ` and NO jq filter to explore the schema, then use jq in subsequent calls

**Schema Discovery Pattern:**
1. First call: ` + "`" + `path: "/wiki/api/v2/spaces", queryParams: {"limit": "1"}` + "`" + ` (no jq) - explore available fields
2. Then use: ` + "`" + `jq: "results[*].{id: id, key: key, name: name}"` + "`" + ` - extract only what you need

**Output format:** TOON (default, token-efficient) or JSON (` + "`" + `outputFormat: "json"` + "`" + `)

**Common paths:**
- ` + "`" + `/wiki/api/v2/spaces` + "`" + ` - list spaces
- ` + "`" + `/wiki/api/v2/pages` + "`" + ` - list pages (use ` + "`" + `space-id` + "`" + ` query param)
- ` + "`" + `/wiki/api/v2/pages/{id}` + "`" + ` - get page details
- ` + "`" + `/wiki/api/v2/pages/{id}/body` + "`" + ` - get page body (` + "`" + `body-format` + "`" + `: storage, atlas_doc_format, view)
- ` + "`" + `/wiki/rest/api/search` + "`" + ` - search content (` + "`" + `cql` + "`" + ` query param)

**JQ examples:** ` + "`" + `results[*].id` + "`" + `, ` + "`" + `results[0]` + "`" + `, ` + "`" + `results[*].{id: id, title: title}` + "`" + `

API reference: https://developer.atlassian.com/cloud/confluence/rest/v2/`

const CONF_POST_DESCRIPTION = `Create Confluence resources. Returns TOON format by default (token-efficient).

**IMPORTANT - Cost Optimization:**
- Use ` + "`" + `jq` + "`" + ` param to extract only needed fields from response (e.g., ` + "`" + `jq: "{id: id, title: title}"` + "`" + `)
- Unfiltered responses include all metadata and are expensive!

**Output format:** TOON (default) or JSON (` + "`" + `outputFormat: "json"` + "`" + `)

**Common operations:**

1. **Create page:** ` + "`" + `/wiki/api/v2/pages` + "`" + `
   body: ` + "`" + `{"spaceId": "123456", "status": "current", "title": "Page Title", "parentId": "789", "body": {"representation": "storage", "value": "<p>Content</p>"}}` + "`" + `

2. **Create blog post:** ` + "`" + `/wiki/api/v2/blogposts` + "`" + `
   body: ` + "`" + `{"spaceId": "123456", "status": "current", "title": "Blog Title", "body": {"representation": "storage", "value": "<p>Content</p>"}}` + "`" + `

3. **Add label:** ` + "`" + `/wiki/api/v2/pages/{id}/labels` + "`" + ` - body: ` + "`" + `{"name": "label-name"}` + "`" + `

4. **Add comment:** ` + "`" + `/wiki/api/v2/pages/{id}/footer-comments` + "`" + `

API reference: https://developer.atlassian.com/cloud/confluence/rest/v2/`

const CONF_PUT_DESCRIPTION = `Replace Confluence resources (full update). Returns TOON format by default.

**IMPORTANT - Cost Optimization:**
- Use ` + "`" + `jq` + "`" + ` param to extract only needed fields from response
- Example: ` + "`" + `jq: "{id: id, version: version.number}"` + "`" + `

**Output format:** TOON (default) or JSON (` + "`" + `outputFormat: "json"` + "`" + `)

**Common operations:**

1. **Update page:** ` + "`" + `/wiki/api/v2/pages/{id}` + "`" + `
   body: ` + "`" + `{"id": "123", "status": "current", "title": "Updated Title", "spaceId": "456", "body": {"representation": "storage", "value": "<p>Content</p>"}, "version": {"number": 2}}` + "`" + `
   Note: version.number must be incremented

2. **Update blog post:** ` + "`" + `/wiki/api/v2/blogposts/{id}` + "`" + `

Note: PUT replaces entire resource. Version number must be incremented.

API reference: https://developer.atlassian.com/cloud/confluence/rest/v2/`

const CONF_PATCH_DESCRIPTION = `Partially update Confluence resources. Returns TOON format by default.

**IMPORTANT - Cost Optimization:** Use ` + "`" + `jq` + "`" + ` param to filter response fields.

**Output format:** TOON (default) or JSON (` + "`" + `outputFormat: "json"` + "`" + `)

**Common operations:**

1. **Update space:** ` + "`" + `/wiki/api/v2/spaces/{id}` + "`" + `
   body: ` + "`" + `{"name": "New Name", "description": {"plain": {"value": "Desc", "representation": "plain"}}}` + "`" + `

2. **Update comment:** ` + "`" + `/wiki/api/v2/footer-comments/{id}` + "`" + `

Note: Confluence v2 API primarily uses PUT for updates.

API reference: https://developer.atlassian.com/cloud/confluence/rest/v2/`

const CONF_DELETE_DESCRIPTION = `Delete Confluence resources. Returns TOON format by default.

**Output format:** TOON (default) or JSON (` + "`" + `outputFormat: "json"` + "`" + `)

**Common operations:**
- ` + "`" + `/wiki/api/v2/pages/{id}` + "`" + ` - Delete page
- ` + "`" + `/wiki/api/v2/blogposts/{id}` + "`" + ` - Delete blog post
- ` + "`" + `/wiki/api/v2/pages/{id}/labels/{label-id}` + "`" + ` - Remove label
- ` + "`" + `/wiki/api/v2/footer-comments/{id}` + "`" + ` - Delete comment
- ` + "`" + `/wiki/api/v2/attachments/{id}` + "`" + ` - Delete attachment

Note: Most DELETE endpoints return 204 No Content on success.

API reference: https://developer.atlassian.com/cloud/confluence/rest/v2/`

// CONF_LIST_SPACES_DESCRIPTION documents the conf_list_spaces tool.
const CONF_LIST_SPACES_DESCRIPTION = `List Confluence spaces with sensible defaults. Returns TOON format by default.

` + "`" + `Use this instead of` + "`" + ` ` + "`" + `conf_get /wiki/api/v2/spaces` + "`" + ` ` + "`" + `when you want:` + "`" + `
- A list of ` + "`" + `"all spaces I have access to"` + "`" + ` (omit ` + "`" + `type` + "`" + ` for that).
- All personal (user-owned) spaces — set ` + "`" + `type: "personal"` + "`" + `.
- All global (site-wide) spaces — set ` + "`" + `type: "global"` + "`" + `.
- Archived vs current — set ` + "`" + `status: "archived"` + "`" + `.

` + "`" + `Defaults:` + "`" + `
- ` + "`" + `limit` + "`" + `: 25 (max 250 per Confluence; cursor pagination for more).
- ` + "`" + `type` + "`" + `: omitted = all types.
- ` + "`" + `status` + "`" + `: omitted = all statuses.
- ` + "`" + `cursor` + "`" + `: omitted = first page. Pass the ` + "`" + `cursor` + "`" + ` from a prior response to advance.

` + "`" + `Output format:` + "`" + ` TOON (default) or JSON (` + "`" + `outputFormat: "json"` + "`" + `).

` + "`" + `Returns:` + "`" + ` A list of space summaries — each with ` + "`" + `id, key, name, type, status, homepageId` + "`" + `. Use ` + "`" + `conf_get /wiki/api/v2/spaces/{id}` + "`" + ` to drill into one.`

// CONF_LIST_PAGES_DESCRIPTION documents the conf_list_pages tool.
const CONF_LIST_PAGES_DESCRIPTION = `List Confluence pages with filters by space, title, status, sort. Returns TOON format by default.

` + "`" + `Use this instead of` + "`" + ` ` + "`" + `conf_get /wiki/api/v2/pages` + "`" + ` ` + "`" + `when you want:` + "`" + `
- All pages in a single space — set ` + "`" + `space-id` + "`" + ` (recommended for any meaningful listing).
- Pages whose title contains a substring — set ` + "`" + `title` + "`" + ` (case-sensitive).
- Only current (non-archived) pages — set ` + "`" + `status: "current"` + "`" + `.

` + "`" + `Resolution order:` + "`" + `
- ` + "`" + `space-id` + "`" + ` (numeric) takes precedence over ` + "`" + `space-key` + "`" + ` if both are set.
- ` + "`" + `space-key` + "`" + ` is also accepted (e.g. ` + "`" + `~712020103880d11e7e48bcbfd1820ce951e426` + "`" + `).

` + "`" + `Defaults:` + "`" + `
- ` + "`" + `limit` + "`" + `: 25 (max 250 per Confluence; cursor pagination for more).
- ` + "`" + `sort` + "`" + `: omitted = id ascending. Use ` + "`" + `-modified-date` + "`" + ` for recently-edited, ` + "`" + `-created-date` + "`" + ` for newest first.
- ` + "`" + `body-format` + "`" + `: omitted = body omitted (lightweight). Set ` + "`" + `body-format: "storage"` + "`" + ` to inline page bodies.

` + "`" + `Output format:` + "`" + ` TOON (default) or JSON (` + "`" + `outputFormat: "json"` + "`" + `).

` + "`" + `Returns:` + "`" + ` A list of page summaries — each with ` + "`" + `id, title, status, spaceId, parentId, version` + "`" + `. Use ` + "`" + `conf_get_page_body` + "`" + ` for the body alone.`

// CONF_GET_PAGE_BODY_DESCRIPTION documents the conf_get_page_body tool.
const CONF_GET_PAGE_BODY_DESCRIPTION = `Read a single page's body in a chosen representation. Returns TOON format by default.

` + "`" + `Use this when:` + "`" + ` You have a page id (from ` + "`" + `conf_list_pages` + "`" + `, search, or given) and want its content, not its metadata.

` + "`" + `Body formats:` + "`" + `
- ` + "`" + `body-format: "storage"` + "`" + ` (default) — Confluence storage-format XHTML. Safe to feed back into PUT/PATCH bodies.
- ` + "`" + `body-format: "view"` + "`" + ` — Rendered HTML as a user sees it after page rendering.
- ` + "`" + `body-format: "atlas_doc_format"` + "`" + ` — Atlassian Document Format JSON.

` + "`" + `Output format:` + "`" + ` TOON (default) or JSON (` + "`" + `outputFormat: "json"` + "`" + `).

` + "`" + `Returns:` + "`" + ` One object with ` + "`" + `value` + "`" + `, ` + "`" + `representation` + "`" + ` fields. For ` + "`" + `storage` + "`" + ` the value is XHTML; for ` + "`" + `view` + "`" + ` it is rendered HTML; for ` + "`" + `atlas_doc_format` + "`" + ` it is a JSON object.`

// CONF_SEARCH_DESCRIPTION documents the conf_search tool.
const CONF_SEARCH_DESCRIPTION = `Search Confluence via Confluence Query Language (CQL). Returns TOON format by default.

` + "`" + `Why this exists:` + "`" + ` The v1 search endpoint is the only Confluence API that accepts CQL. The v2 endpoints do not understand CQL or a portable search expression, so this tool wraps the v1 path explicitly.

` + "`" + `CQL examples:` + "`" + `
- ` + "`" + `type=page AND text~mcp-confluence` + "`" + ` — find pages mentioning ` + "`" + `mcp-confluence` + "`" + `.
- ` + "`" + `type=page AND space.type=personal AND space.title~bennie` + "`" + ` — find a personal space by name.
- ` + "`" + `creator=currentUser() AND type=page` + "`" + ` — pages you created.
- ` + "`" + `lastModified >= "2026-01-01" AND type=blogpost` + "`" + ` — recent blog posts.

` + "`" + `Parameters:` + "`" + `
- ` + "`" + `cql` + "`" + `: the CQL expression. ` + "`" + `Required.` + "`" + ` Caller is responsible for any URL encoding the operator supplies; this tool does not auto-encode for you.
- ` + "`" + `limit` + "`" + `: result cap, default 25, max 100.
- ` + "`" + `start` + "`" + `: pagination offset. Default 0. Re-issued calls advance ` + "`" + `start` + "`" + ` by adding the previous ` + "`" + `limit` + "`" + `.

` + "`" + `Returns:` + "`" + ` Object with ` + "`" + `results` + "`" + ` (each entry has ` + "`" + `title, excerpt, url, content, lastModified, entityType` + "`" + `), plus ` + "`" + `start, limit, totalSize, cqlQuery` + "`" + ` for pagination. Use ` + "`" + `conf_get /wiki/rest/api/search` + "`" + ` if you need finer control.`

// CONF_HELP_DESCRIPTION documents the conf_help tool.
// CONF_HELP_DESCRIPTION documents the conf_help self-describing
// tool. The full tool surface map lives in the response.
const CONF_HELP_DESCRIPTION = `Show how to use the confluence MCP server — the tool surface in one call.

` + "`" + `Use this when:` + "`" + ` You have just discovered the ` + "`" + `mcp_confluence_*` + "`" + ` tool prefix and want a tour, or you are not sure which of the ten tools fits the task.

` + "`" + `Response shape:` + "`" + ` Object with one entry per tool — ` + "`" + `conf_get, conf_post, conf_put, conf_patch, conf_delete, conf_list_spaces, conf_list_pages, conf_get_page_body, conf_search, conf_help` + "`" + `. For each tool:
- ` + "`" + `description` + "`" + `: short purpose (one sentence).
- ` + "`" + `args` + "`" + `: top-level fields with one-line descriptions.
- ` + "`" + `example` + "`" + `: a single concrete invocation.

` + "`" + `Filter by topic:` + "`" + `
- ` + "`" + `topic: "conf_list_pages"` + "`" + ` returns just that one tool's entry.
- ` + "`" + `topic: "all"` + "`" + ` (default) returns every tool.

` + "`" + `Output format:` + "`" + ` TOON (default; preferred for human reading) or JSON.

` + "`" + `Tip:` + "`" + ` Run ` + "`" + `conf_help` + "`" + ` once per session at the start of a conversation so the tool surface is loaded into context; subsequent calls inside the same conversation can stay focused.`
