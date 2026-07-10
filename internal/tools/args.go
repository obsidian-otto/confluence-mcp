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

// UploadAttachmentArgs is the argument set for the
// `conf_upload_attachment` tool. Uploads a single binary file from
// disk as an attachment to a Confluence page.
//
// Note: the on-the-wire endpoint is the v1 REST API
// (POST /wiki/rest/api/content/{pageId}/child/attachment with
// multipart/form-data + X-Atlassian-Token: no-check). Confluence
// Cloud has no v2 upload endpoint as of 2026-07-10 — full
// rationale in specs/11-attachments/01-research-and-surface.md.
type UploadAttachmentArgs struct {
	// PageId is the numeric page id the attachment is uploaded to.
	PageId string `json:"pageId" jsonschema:"description=Numeric page id where the attachment will live (required). Example: '163935'."`
	// FilePath is the absolute path to the file on disk. The handler
	// opens it with os.Open and streams via io.Copy — files do NOT
	// load into memory beyond the multipart body buffer. 100 MB
	// is the Atlassian Cloud hard cap; calls over the cap return
	// 413 Payload Too Large from the server.
	FilePath string `json:"filePath" jsonschema:"description=Absolute path to the file on disk (required). PNG, PDF, drawio XML, JPEG, SVG, DOCX, XLSX, MP4, ZIP all work — the file is uploaded as-is, no base64 inflation. Example: '/home/user/diagram.drawio'."`
	// Comment is the changelog message for the new attachment
	// version. Empty string means no comment.
	Comment string `json:"comment,omitempty" jsonschema:"description=Optional changelog / version comment shown next to the attachment in the page's attachment history. Default: empty."`
	// MinorEdit marks the new attachment version as a minor edit.
	// Go's zero value is false, which sends minorEdit=false on the
	// wire. Pass true explicitly to mark the upload as a minor
	// version bump.
	MinorEdit bool `json:"minorEdit,omitempty" jsonschema:"description=Whether the new attachment version is a minor edit. Go's zero value is false (omitted from args = not a minor edit). Set true to mark as a minor version bump."`
	// OutputFormat selector.
	OutputFormat string `json:"outputFormat,omitempty" jsonschema:"description=Output format. Default TOON; 'json' for plain JSON."`
	// JQ filter applied to the response envelope.
	JQ string `json:"jq,omitempty" jsonschema:"description=Optional JMESPath filter evaluated against the v1 ContentPageScheme envelope (e.g. 'results[0].{id: id, title: title, mediaType: mediaType, fileSize: extensions.fileSize}' to extract a single attachment summary)."`
}

// ListAttachmentsArgs is the argument set for the
// `conf_list_attachments` tool. Lists the attachments on a page via
// the v2 GET /wiki/api/v2/pages/{id}/attachments endpoint.
type ListAttachmentsArgs struct {
	// PageId is the numeric page id to list attachments for.
	PageId string `json:"pageId" jsonschema:"description=Numeric page id whose attachments should be listed (required). Example: '163935'."`
	// Cursor for v2 cursor-based pagination.
	Cursor string `json:"cursor,omitempty" jsonschema:"description=Opaque pagination cursor from a previous list_attachments response. Omit for the first page."`
	// Limit caps results; v2 caps at 100.
	Limit int `json:"limit,omitempty" jsonschema:"description=Maximum attachments to return. Default 25; max 100 (the v2 endpoint caps at 100)."`
	// MediaType filters by media type substring (e.g. 'image').
	MediaType string `json:"mediaType,omitempty" jsonschema:"description=Substring filter on the attachment's mediaType (e.g. 'image' to match image/png and image/jpeg)."`
	// Filename filters by exact filename.
	Filename string `json:"filename,omitempty" jsonschema:"description=Exact filename filter (case-sensitive)."`
	// OutputFormat selector.
	OutputFormat string `json:"outputFormat,omitempty" jsonschema:"description=Output format. Default TOON; 'json' for plain JSON."`
	// JQ filter.
	JQ string `json:"jq,omitempty" jsonschema:"description=Optional JMESPath filter evaluated against the v2 MultiEntityResult<Attachment> envelope."`
}

// DeleteAttachmentArgs is the argument set for the
// `conf_delete_attachment` tool. Deletes an attachment by id via
// the v2 DELETE /wiki/api/v2/attachments/{id} endpoint.
type DeleteAttachmentArgs struct {
	// AttachmentId is the attachment's numeric id.
	AttachmentId string `json:"attachmentId" jsonschema:"description=Numeric attachment id (required). Get the id from list_attachments or the page's attachment metadata."`
	// Purge permanently deletes (skips trash) when true.
	Purge bool `json:"purge,omitempty" jsonschema:"description=Set true to permanently delete (purge) the attachment instead of moving it to trash. Default false. Purging is irreversible."`
	// OutputFormat selector.
	OutputFormat string `json:"outputFormat,omitempty" jsonschema:"description=Output format. Default TOON; 'json' for plain JSON."`
	// JQ filter.
	JQ string `json:"jq,omitempty" jsonschema:"description=Optional JMESPath filter — most DELETE responses are 204 No Content, so jq has nothing to evaluate."`
}
