# 02.2 — The Five Tools (Input/Output Shape)

## Overview

The five tools (`conf_get`, `conf_post`, `conf_put`,
`conf_patch`, `conf_delete`) are the entire surface of the
upstream. Each has a **distinct input shape** (zod schema in
the upstream; Go struct + `jsonschema` tags in the Go port)
and a **uniform output shape** (text content with TOON or
JSON encoding, optionally truncated with a `/tmp/mcp/` raw-
response pointer).

This file documents each tool's exact input shape and output
contract so the Go port can mirror them byte-for-byte. The
shapes are taken verbatim from the upstream's
`src/tools/atlassian.api.types.ts` (zod schemas, 117 lines) and
the `CONF_*_DESCRIPTION` strings in
`src/tools/atlassian.api.tool.ts`.

## Sources

- Upstream source: `src/tools/atlassian.api.types.ts` (zod
  schemas for `GetApiToolArgs`, `RequestWithBodyArgs`,
  `DeleteApiToolArgs`).
- Upstream source: `src/tools/atlassian.api.tool.ts`
  (the `CONF_*_DESCRIPTION` constants, lines 127-223).
- Upstream README (lines 134-226 of
  `/tmp/mcp-research/aashari-README.md`): the **Common API
  Paths** table and the **Tool Parameters** section.
- `mcp-golang` framework (tools API):
  https://mcpgolang.com/tools (or see
  `04-mcp-golang-framework/01-server-api.md`).

## Spec

### Common shape

Every tool returns:

```go
type ToolResponse struct {
    Content []*Content  // always exactly one TextContent in this server
}

type Content struct {
    Type string  // "text"
    Text string  // the TOON- or JSON-encoded body, or the error message
}
```

Error responses use the same `ToolResponse` shape with the
**content text** being the error message and an `IsError`
boolean field set to `true` on the response wrapper. See
`09-anti-patterns/03-error-shapes.md` for the exact error
response shape.

### `conf_get` (GET requests)

**Purpose:** read any Confluence resource.

**Input (Go struct with jsonschema tags):**

```go
type ConfGetArgs struct {
    Path         string            `json:"path" jsonschema:"required,description=The API endpoint path (e.g. /wiki/api/v2/spaces). Must start with /"`
    QueryParams  map[string]string `json:"queryParams,omitempty" jsonschema:"description=Query parameters as key-value pairs (e.g. {\"limit\":\"25\",\"space-id\":\"123\"})"`
    JQ           string            `json:"jq,omitempty" jsonschema:"description=JMESPath expression to filter/transform the response. ALWAYS use to reduce token cost."`
    OutputFormat string            `json:"outputFormat,omitempty" jsonschema:"description=Output format: 'toon' (default, 30-60% fewer tokens) or 'json',enum=toon,enum=json"`
}
```

**Behavior:**

1. Validate `path` starts with `/`.
2. Build URL `https://${site}.atlassian.net/wiki${path}${queryString}`.
3. `client.Auth.SetBasicAuth(email, token)`.
4. `client.HTTP.Get(url)` → JSON.
5. If `jq` set → apply JMESPath.
6. If `outputFormat == "json"` (or unset → "toon" default) →
   encode.
7. Truncate if >40k chars; embed `/tmp/mcp/...` pointer.
8. Return text content.

**Tool description (verbatim from upstream):**

> Read any Confluence data. Returns TOON format by default
> (30-60% fewer tokens than JSON).
>
> **IMPORTANT - Cost Optimization:**
> - ALWAYS use `jq` param to filter response fields.
>   Unfiltered responses are very expensive!
> - Use `limit` query param to restrict result count
>   (e.g. `limit: "5"`)
> - If unsure about available fields, first fetch ONE item
>   with `limit: "1"` and NO jq filter to explore the schema,
>   then use jq in subsequent calls
>
> **Output format:** TOON (default, token-efficient) or JSON
> (`outputFormat: "json"`)
>
> **Common paths:**
> - `/wiki/api/v2/spaces` - list spaces
> - `/wiki/api/v2/pages` - list pages (use `space-id` query
>   param)
> - `/wiki/api/v2/pages/{id}` - get page details
> - `/wiki/api/v2/pages/{id}/body` - get page body
>   (`body-format`: storage, atlas_doc_format, view)
> - `/wiki/rest/api/search` - search content (`cql` query
>   param)
>
> **JQ examples:** `results[*].id`, `results[0]`,
> `results[*].{id: id, title: title}`
>
> API reference:
> https://developer.atlassian.com/cloud/confluence/rest/v2/

### `conf_post` (POST requests)

**Input:**

```go
type ConfPostArgs struct {
    Path         string            `json:"path" jsonschema:"required,description=API endpoint path"`
    QueryParams  map[string]string `json:"queryParams,omitempty" jsonschema:"description=Query parameters"`
    Body         map[string]any    `json:"body" jsonschema:"required,description=Request body as JSON object"`
    JQ           string            `json:"jq,omitempty" jsonschema:"description=JMESPath filter"`
    OutputFormat string            `json:"outputFormat,omitempty" jsonschema:"description=Output format: 'toon' or 'json',enum=toon,enum=json"`
}
```

**Behavior:** same as `conf_get`, but `POST` method, body sent
as JSON-encoded payload.

**Tool description (verbatim):**

> Create Confluence resources. Returns TOON format by default
> (token-efficient).
>
> **IMPORTANT - Cost Optimization:**
> - Use `jq` param to extract only needed fields from
>   response (e.g. `jq: "{id: id, title: title}"`)
> - Unfiltered responses include all metadata and are
>   expensive!
>
> **Common operations:**
>
> 1. **Create page:** `/wiki/api/v2/pages`
>    body: `{"spaceId": "123456", "status": "current",
>    "title": "Page Title", "parentId": "789", "body":
>    {"representation": "storage", "value":
>    "<p>Content</p>"}}`
>
> 2. **Create blog post:** `/wiki/api/v2/blogposts`
>    body: `{"spaceId": "123456", "status": "current",
>    "title": "Blog Title", "body": {"representation":
>    "storage", "value": "<p>Content</p>"}}`
>
> 3. **Add label:** `/wiki/api/v2/pages/{id}/labels`
>    body: `{"name": "label-name"}`
>
> 4. **Add comment:** `/wiki/api/v2/pages/{id}/footer-comments`
>
> API reference:
> https://developer.atlassian.com/cloud/confluence/rest/v2/

### `conf_put` (PUT requests — full replacement)

**Input:** same as `ConfPostArgs` (body is required).

**Behavior:** PUT, body required, used for full resource
replacement (e.g. page update with `version.number`).

**Tool description (verbatim):**

> Replace Confluence resources (full update). Returns TOON
> format by default.
>
> **IMPORTANT - Cost Optimization:** Use `jq` param to
> filter response fields.
>
> **Common operations:**
>
> 1. **Update page:** `/wiki/api/v2/pages/{id}`
>    body: `{"id": "123", "status": "current", "title":
>    "Updated Title", "spaceId": "456", "body":
>    {"representation": "storage", "value":
>    "<p>Updated content</p>"}, "version": {"number": 2}}`
>    Note: version.number must be incremented
>
> 2. **Update blog post:** `/wiki/api/v2/blogposts/{id}`
>
> Note: PUT replaces entire resource. Version number must
> be incremented.
>
> API reference:
> https://developer.atlassian.com/cloud/confluence/rest/v2/

### `conf_patch` (PATCH requests — partial update)

**Input:** same as `ConfPostArgs`.

**Tool description (verbatim):**

> Partially update Confluence resources. Returns TOON
> format by default.
>
> **IMPORTANT - Cost Optimization:** Use `jq` param to
> filter response fields.
>
> **Common operations:**
>
> 1. **Update space:** `/wiki/api/v2/spaces/{id}`
>    body: `{"name": "New Name", "description": {"plain":
>    {"value": "Desc", "representation": "plain"}}}`
>
> 2. **Update comment:** `/wiki/api/v2/footer-comments/{id}`
>
> Note: Confluence v2 API primarily uses PUT for updates.
>
> API reference:
> https://developer.atlassian.com/cloud/confluence/rest/v2/

### `conf_delete` (DELETE requests)

**Input:** same as `ConfGetArgs` (no body).

**Tool description (verbatim):**

> Delete Confluence resources. Returns TOON format by
> default.
>
> **Common operations:**
> - `/wiki/api/v2/pages/{id}` - Delete page
> - `/wiki/api/v2/blogposts/{id}` - Delete blog post
> - `/wiki/api/v2/pages/{id}/labels/{label-id}` - Remove
>   label
> - `/wiki/api/v2/footer-comments/{id}` - Delete comment
> - `/wiki/api/v2/attachments/{id}` - Delete attachment
>
> Note: Most DELETE endpoints return 204 No Content on
> success.
>
> API reference:
> https://developer.atlassian.com/cloud/confluence/rest/v2/

### Parameter normalization rules (carried over from upstream)

| Rule | Where enforced |
| ---- | -------------- |
| `path` must start with `/` | service layer (`normalizePath` adds `/` if missing; Go port does the same) |
| `queryParams` is `Record<string,string>` | zod schema in upstream; Go struct field is `map[string]string` |
| `jq` is a JMESPath expression string | jmespath library applies it |
| `outputFormat` defaults to `"toon"` | handler checks for `"json"`; anything else → TOON |
| Truncation threshold | 40,000 chars (≈10,000 tokens) — see `09-anti-patterns/03-error-shapes.md` |

## Verification

A reader of this spec should be able to:

1. Confirm each tool's input shape by grepping the upstream
   source.
2. Confirm each tool's description matches what `hermes mcp
   test confluence` displays when listing tools.
3. Run `npx -y @aashari/mcp-server-atlassian-confluence get
   --path /wiki/api/v2/spaces --limit 5 --jq "results[*].{id:
   id, key: key, name: name}"` and confirm the upstream's
   output shape matches what the Go port produces for the
   same call.