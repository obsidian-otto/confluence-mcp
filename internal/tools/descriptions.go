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
