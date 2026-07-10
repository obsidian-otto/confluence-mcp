// markdown_args.go — Phase 14: argument types for the v2 markdown
// round-trip tools (conf_post_markdown, conf_put_markdown,
// conf_get_page_markdown).
//
// These are thin wrappers over the existing CRUD tool arg shapes
// (PostArgs, PutArgs, GetPageBodyArgs) that add a `markdown` and/or
// `markdownFile` field. The handler in markdown_handlers.go reads
// the markdown source (or the file from disk), converts it to
// Confluence storage XHTML via internal/markdown.MarkdownToStorageXHTML
// (or its reverse), and delegates to the underlying CRUD handler
// (HandlePost / HandlePut / HandleGetPageBody) so the 9-step
// TOON/JMESPath/truncation pipeline is shared.
//
// Each field carries a `jsonschema:"description=..."` tag with an
// inline example, mirroring the existing 10 args structs in
// internal/tools/args.go. The metoro-io/mcp-golang framework reads
// these tags via invopop/jsonschema to build the tool input schema
// the MCP client sees over the wire.
//
// Wire format note: the v2 markdown tools accept the markdown body
// in one of two forms:
//
//  1. The `markdown` field — the source text is provided inline.
//     This is the common path for small pages.
//  2. The `markdownFile` field — a path on the local filesystem.
//     The handler reads the file (size-capped at 1 MB) and uses
//     its contents as the markdown source. This is the path for
//     large pages, multi-file workflows, or content kept in version
//     control.
//
// If both are set, `markdown` wins. If neither is set, the handler
// returns an error (the markdown source is required).
package tools

// PostMarkdownArgs is the argument set for the `conf_post_markdown`
// tool. It is a conf_post wrapper: the handler builds the storage
// XHTML from the markdown source, then delegates to HandlePost with
// the same path the upstream /wiki/api/v2/pages endpoint expects.
type PostMarkdownArgs struct {
	// SpaceID is the numeric Confluence space id the new page
	// belongs to. Required.
	SpaceID string `json:"spaceId" jsonschema:"description=Numeric space id where the new page will live (required, e.g. '780763211'). The page is created at the top level unless a parentId is supplied."`
	// Title is the new page's title. Required.
	Title string `json:"title" jsonschema:"description=Title for the new page (required). Shown in the Confluence page tree and in the page header."`
	// Markdown is the inline markdown source. Mutually exclusive
	// with MarkdownFile (the handler picks Markdown when both are
	// set). At least one of Markdown or MarkdownFile must be
	// non-empty.
	Markdown string `json:"markdown,omitempty" jsonschema:"description=Markdown source for the new page body (alternative to markdownFile). CommonMark + GFM (tables, task lists, fenced code blocks, strikethrough) is supported. Example: '# Hello\\n\\nA **bold** paragraph with a [link](https://example.com).'"`
	// MarkdownFile is a filesystem path. The handler reads it
	// (size-capped at 1 MB) and uses its contents as the markdown
	// source. Mutually exclusive with Markdown.
	MarkdownFile string `json:"markdownFile,omitempty" jsonschema:"description=Absolute path to a markdown file on disk (alternative to the markdown field; the file is read at call time, capped at 1 MB). Example: '/home/bennie/pages/oncall.md'."`
	// ParentID is the optional parent page id. Omit to create a
	// top-level page in the space.
	ParentID string `json:"parentId,omitempty" jsonschema:"description=Optional parent page id; omit for a top-level page in the space. Example: '163935'."`
	// Status is "current" (default) or "archived". Mirrors the
	// upstream v2 API field shape.
	Status string `json:"status,omitempty" jsonschema:"description=Page status: 'current' (default) for an active page, 'archived' for an archived page. Omit to default to 'current'."`
	// OutputFormat is the standard pipeline selector: empty for
	// TOON (the default), "json" for raw JSON.
	OutputFormat string `json:"outputFormat,omitempty" jsonschema:"description=Output format. Empty or omitted (default) returns TOON; 'json' returns the raw JSON response from Confluence."`
	// JQ is the optional JMESPath filter applied to the response
	// (the created-page object, not the markdown body).
	JQ string `json:"jq,omitempty" jsonschema:"description=Optional JMESPath expression evaluated against the created-page response. Example: '{id: id, title: title, version: version.number}'."`
}

// PutMarkdownArgs is the argument set for the `conf_put_markdown`
// tool. Same shape as PostMarkdownArgs plus a required PageID; the
// handler delegates to HandlePut (which already handles the
// version.number increment).
type PutMarkdownArgs struct {
	// PageID is the numeric id of the page to update. Required.
	PageID string `json:"pageId" jsonschema:"description=Numeric page id of the page to update (required, e.g. '163935'). The version.number is auto-incremented by the underlying HandlePut handler."`
	// Title is the new page title. If empty, the existing title
	// is preserved (the handler only overwrites fields that the
	// caller supplied).
	Title string `json:"title,omitempty" jsonschema:"description=New page title. Omit to keep the existing title. The full replacement is sent on the wire (PUT semantics), so any field you want to preserve must be echoed back."`
	// Markdown is the inline markdown source. Mutually exclusive
	// with MarkdownFile.
	Markdown string `json:"markdown,omitempty" jsonschema:"description=Markdown source for the new page body (alternative to markdownFile). CommonMark + GFM is supported. Example: '## Section\\n\\nUpdated content.'"`
	// MarkdownFile is a filesystem path. Mutually exclusive with
	// Markdown.
	MarkdownFile string `json:"markdownFile,omitempty" jsonschema:"description=Absolute path to a markdown file on disk (alternative to the markdown field; the file is read at call time, capped at 1 MB). Example: '/home/bennie/pages/oncall.md'."`
	// OutputFormat is the standard pipeline selector.
	OutputFormat string `json:"outputFormat,omitempty" jsonschema:"description=Output format. Empty or omitted (default) returns TOON; 'json' returns the raw JSON response from Confluence."`
	// JQ is the optional JMESPath filter applied to the response.
	JQ string `json:"jq,omitempty" jsonschema:"description=Optional JMESPath expression evaluated against the updated-page response. Example: '{id: id, version: version.number}'."`
}

// GetPageMarkdownArgs is the argument set for the
// `conf_get_page_markdown` tool. The handler delegates to
// HandleGetPageBody (which fetches the storage XHTML), then converts
// the body to markdown via internal/markdown.StorageXHTMLToMarkdown.
type GetPageMarkdownArgs struct {
	// PageID is the numeric id of the page to fetch. Required.
	PageID string `json:"page-id" jsonschema:"description=Numeric page id (required, e.g. '163935'). The page is fetched and its body is converted to markdown before being returned."`
	// OutputFormat is the standard pipeline selector.
	OutputFormat string `json:"outputFormat,omitempty" jsonschema:"description=Output format. Empty or omitted (default) returns a TOON-encoded JSON envelope {pageId, title, markdown}; 'json' returns the same envelope as raw JSON. The raw markdown text is never the top-level output — it is always inside the envelope."`
	// JQ is the optional JMESPath filter applied to the response
	// envelope (NOT to the markdown text). Useful for stripping
	// the pageId/title from the response when the caller only
	// wants the markdown text.
	JQ string `json:"jq,omitempty" jsonschema:"description=Optional JMESPath expression evaluated against the response envelope. Example: 'markdown' to return just the markdown text. Note: a JMESPath filter can extract the markdown field but cannot transform the markdown text itself."`
}
