// Package tools provides the MCP tool argument types for the
// `confluence` server, the per-method handlers, and the
// safeHandler panic-recovery wrapper.
//
// The argument shapes for the five CRUD tools (conf_get, conf_post,
// conf_put, conf_patch, conf_delete) mirror the upstream
// `@aashari/mcp-server-atlassian-confluence` v3.3.0 zod schemas
// (`GetApiToolArgs`, `RequestWithBodyArgs`, `DeleteApiToolArgs`).
//
// Each struct field carries a `jsonschema:"description=...,required"`
// tag. The metoro-io/mcp-golang framework reads these tags via
// invopop/jsonschema to build the tool input schema that clients
// see over the wire; explicit tags make the schema accurate (no
// reflection surprises) and let humans reading the schema know
// what each field expects without consulting the source.
package tools

// GetArgs is the argument set for the `conf_get` tool (HTTP GET).
// Mirrors upstream `GetApiToolArgs` (no body).
type GetArgs struct {
	Path         string            `json:"path" jsonschema:"description=The Confluence REST API path. Must start with /wiki/. Examples: '/wiki/api/v2/spaces' or '/wiki/api/v2/pages?limit=5'. Query-string portion (after '?') is split out and merged with the query map, required"`
	Query        map[string]string `json:"query,omitempty" jsonschema:"description=Optional URL query parameters as a flat string→string map. Examples: {\\\"limit\\\":\\\"5\\\"} or {\\\"space-id\\\":\\\"123\\\"}. When the path itself contains '?' parameters, those are merged with this map; explicit entries here win."`
	JQ           string            `json:"jq,omitempty" jsonschema:"description=Optional JMESPath expression evaluated against the decoded response. Short-circuits to no parse cost when omitted. Examples: 'results[*].id', 'results[*].{id: id, name: name}'"`
	OutputFormat string            `json:"outputFormat,omitempty" jsonschema:"description=Output format selector. Empty or omitted → TOON (30-60% fewer tokens than JSON). Set to 'json' for plain JSON."`
}

// PostArgs is the argument set for the `conf_post` tool (HTTP POST).
// Mirrors upstream `RequestWithBodyArgs` / `PostApiToolArgs`. The
// request body is a JSON object — required for most write endpoints.
type PostArgs struct {
	Path         string            `json:"path" jsonschema:"description=The Confluence REST API path (POST target). Example: '/wiki/api/v2/pages' to create a page, required"`
	Query        map[string]string `json:"query,omitempty" jsonschema:"description=Optional URL query parameters."`
	Body         map[string]any    `json:"body,omitempty" jsonschema:"description=JSON object to send as the request body. For creating a Confluence page: {\\\"spaceId\\\":\\\"<numeric>\\\",\\\"status\\\":\\\"current\\\",\\\"title\\\":\\\"My title\\\",\\\"body\\\":{\\\"representation\\\":\\\"storage\\\",\\\"value\\\":\\\"<p>HTML content</p>\\\"}}. Object-only; use a single object, never an array, for POST endpoints."`
	JQ           string            `json:"jq,omitempty" jsonschema:"description=Optional JMESPath filter applied to the response."`
	OutputFormat string            `json:"outputFormat,omitempty" jsonschema:"description=Output format. Default TOON; 'json' for plain JSON."`
}

// PutArgs is the argument set for the `conf_put` tool (HTTP PUT —
// full replacement). Mirrors upstream `PutApiToolArgs`.
type PutArgs struct {
	Path         string            `json:"path" jsonschema:"description=The Confluence REST API path (PUT target). Example: '/wiki/api/v2/pages/{id}'. Versioning: include version.number incremented by 1 in the body, required"`
	Query        map[string]string `json:"query,omitempty" jsonschema:"description=Optional URL query parameters."`
	Body         map[string]any    `json:"body,omitempty" jsonschema:"description=JSON object representing the full replacement resource. For updating a page: {\\\"id\\\":\\\"<numeric>\\\",\\\"status\\\":\\\"current\\\",\\\"title\\\":\\\"New title\\\",\\\"spaceId\\\":\\\"<numeric>\\\",\\\"body\\\":{\\\"representation\\\":\\\"storage\\\",\\\"value\\\":\\\"<p>Updated</p>\\\"},\\\"version\\\":{\\\"number\\\":<N+1>\\\"}}. Object-only."`
	JQ           string            `json:"jq,omitempty" jsonschema:"description=Optional JMESPath filter applied to the response."`
	OutputFormat string            `json:"outputFormat,omitempty" jsonschema:"description=Output format. Default TOON; 'json' for plain JSON."`
}

// PatchArgs is the argument set for the `conf_patch` tool
// (HTTP PATCH — partial update). The upstream API accepts the
// patch operations as a JSON array (RFC 6902-style), so Body is
// a slice of objects rather than a single object.
type PatchArgs struct {
	Path         string            `json:"path" jsonschema:"description=The Confluence REST API path (PATCH target). Example: '/wiki/api/v2/pages/{id}'. required"`
	Query        map[string]string `json:"query,omitempty" jsonschema:"description=Optional URL query parameters."`
	Body         []map[string]any  `json:"body,omitempty" jsonschema:"description=Array of patch operations (RFC 6902 JSON Patch style). Example: [{op:'replace', path:'/title', value:'New title'},{op:'replace', path:'/version/number', value:'<N+1>'}]. Each element is an object with at minimum 'op' and 'path'."`
	JQ           string            `json:"jq,omitempty" jsonschema:"description=Optional JMESPath filter applied to the response."`
	OutputFormat string            `json:"outputFormat,omitempty" jsonschema:"description=Output format. Default TOON; 'json' for plain JSON."`
}

// DeleteArgs is the argument set for the `conf_delete` tool
// (HTTP DELETE — no body). Mirrors upstream `DeleteApiToolArgs`
// which is identical in shape to `GetApiToolArgs`.
type DeleteArgs struct {
	Path         string            `json:"path" jsonschema:"description=The Confluence REST API path (DELETE target). Example: '/wiki/api/v2/pages/{id}', required"`
	Query        map[string]string `json:"query,omitempty" jsonschema:"description=Optional URL query parameters."`
	JQ           string            `json:"jq,omitempty" jsonschema:"description=Optional JMESPath filter applied to the (likely empty) response."`
	OutputFormat string            `json:"outputFormat,omitempty" jsonschema:"description=Output format. Default TOON; 'json' for plain JSON. Note: most DELETE endpoints return 204 No Content."`
}

// ListSpacesArgs is the argument set for the `conf_list_spaces`
// convenience tool. It is a wrapper over conf_get with
// sensible-by-default field selection so the caller does not need
// to know about JMESPath or the API path layout.
type ListSpacesArgs struct {
	// Limit caps the number of spaces returned. Defaults to 25; max 250.
	Limit int `json:"limit,omitempty" jsonschema:"description=Maximum number of spaces to return. Defaults to 25 if omitted. The Confluence API caps this at 250; values above 250 are honoured via cursor pagination but each paginated call counts toward the response size budget."`
	// Cursor is the opaque pagination cursor returned by a prior call.
	Cursor string `json:"cursor,omitempty" jsonschema:"description=Opaque pagination cursor from a previous list_spaces response. Omit for the first page."`
	// Type filters by space type ("personal", "global", or empty for all).
	Type string `json:"type,omitempty" jsonschema:"description=Filter by space type. Empty string returns all types. Common values: 'personal' (user-owned), 'global' (site-wide)."`
	// Status filters by space status ("current", "archived"). Empty for all.
	Status string `json:"status,omitempty" jsonschema:"description=Filter by space status. Empty returns all statuses. Common values: 'current', 'archived'."`
	// Output format selector (same semantics as the other tools).
	OutputFormat string `json:"outputFormat,omitempty" jsonschema:"description=Output format. Default TOON; 'json' for plain JSON."`
}

// ListPagesArgs is the argument set for the `conf_list_pages`
// convenience tool. It is a wrapper over conf_get scoped to
// /wiki/api/v2/pages so the caller can list, filter, and paginate
// pages without remembering the v2 endpoint shape.
type ListPagesArgs struct {
	// SpaceID filters the pages to a single space (recommended).
	SpaceID string `json:"space-id,omitempty" jsonschema:"description=Numeric space id (e.g. '780763211') to restrict listing to that space. Strongly recommended for any meaningful exploration; without it the result set is the entire site."`
	// SpaceKey filters the pages to a single space by key instead of id.
	SpaceKey string `json:"space-key,omitempty" jsonschema:"description=Space key (e.g. '~712020103880d11e7e48bcbfd1820ce951e426') as an alternative to space-id. Mutually exclusive: if both are set, space-id wins. The Confluence v2 API resolves both internally."`
	// Title substring filter.
	Title string `json:"title,omitempty" jsonschema:"description=Substring filter on page titles. Case-sensitive. Omit for no filter."`
	// Status filters by page status.
	Status string `json:"status,omitempty" jsonschema:"description=Page status filter. 'current' for non-archived pages, 'archived' for archived. Empty returns all."`
	// Limit caps results.
	Limit int `json:"limit,omitempty" jsonschema:"description=Maximum pages to return. Defaults to 25; max 250."`
	// Cursor for pagination.
	Cursor string `json:"cursor,omitempty" jsonschema:"description=Opaque pagination cursor from a previous list_pages response."`
	// SortField selects a server-side sort field.
	SortField string `json:"sort,omitempty" jsonschema:"description=Server-side sort field. One of: 'id' (default), 'title', 'created-date', 'modified-date', or '-id'/'-title'/etc for descending."`
	// BodyFormat hints whether to inline page bodies (heavy).
	BodyFormat string `json:"body-format,omitempty" jsonschema:"description=If set to 'storage' or 'view' or 'atlas_doc_format', the response includes the page body inline. Omit to omit the body and keep responses light."`
	// Output format selector.
	OutputFormat string `json:"outputFormat,omitempty" jsonschema:"description=Output format. Default TOON; 'json' for plain JSON."`
}

// GetPageBodyArgs is the argument set for the `conf_get_page_body`
// convenience tool. It fetches only the body of a page, with the
// requested representation, leaving the rest of the page metadata
// to a separate call if needed.
type GetPageBodyArgs struct {
	// PageID identifies the page to fetch.
	PageID string `json:"page-id" jsonschema:"description=Numeric page id (or use the page's id string). Example: '163935', required"`
	// BodyFormat selects the representation: storage, atlas_doc_format, or view.
	BodyFormat string `json:"body-format,omitempty" jsonschema:"description=Body representation. 'storage' (default) for raw Confluence storage XHTML; 'view' for rendered HTML; 'atlas_doc_format' for Atlassian Document Format."`
	// Output format selector.
	OutputFormat string `json:"outputFormat,omitempty" jsonschema:"description=Output format. Default TOON; 'json' for plain JSON."`
}

// SearchArgs is the argument set for the `conf_search` convenience
// tool. It wraps /wiki/rest/api/search (v1 search) with a CQL
// argument since CQL is the only knob Confluence's search exposes
// and JMESPath can't construct it on the wire.
type SearchArgs struct {
	// CQL is the Confluence Query Language expression.
	CQL string `json:"cql" jsonschema:"description=Confluence Query Language expression. Examples: 'type=page AND space.type=personal AND space.title~bennie', 'text~mcp-confluence AND type=page', 'creator=currentUser()'. Always URL-encode special characters; this tool does NOT auto-encode for you, required"`
	// Limit caps results.
	Limit int `json:"limit,omitempty" jsonschema:"description=Maximum search results to return. Defaults to 25; max 100."`
	// Cursor for pagination (v1 search uses start/limit, not cursor).
	Start int `json:"start,omitempty" jsonschema:"description=Pagination start offset for v1 search. Defaults to 0. The returned results contain 'start', 'limit', and 'totalSize' so callers can advance by adding limit to start."`
	// ExcludedContent picks what to skip.
	ExcludedContent string `json:"excludedContent,omitempty" jsonschema:"description=Optional v1 search 'excerpt' inclusion. 'excerpt' (default) shows excerpts; 'content' or 'highlight' add inline matches. Note: v1 search returns both excerpt and content fields; this arg is informational only and not all values are honoured by every endpoint version."`
	// Output format selector.
	OutputFormat string `json:"outputFormat,omitempty" jsonschema:"description=Output format. Default TOON; 'json' for plain JSON."`
}

// HelpArgs is the argument set for the `conf_help` convenience
// tool. Its only purpose is to return the human-readable "how to
// use this server" guide, derived from the registered tool
// descriptions. A catch-all for callers who arrive at a 5-tool
// surface without knowing which one to pick.
type HelpArgs struct {
	// Topic optionally narrows help to a single tool name.
	Topic string `json:"topic,omitempty" jsonschema:"description=Optional tool name to filter the help response (e.g. 'conf_get'). Empty or 'all' returns the full surface map."`
	// Output format selector.
	OutputFormat string `json:"outputFormat,omitempty" jsonschema:"description=Output format. Default TOON (preferred for human reading); 'json' for plain JSON."`
}
