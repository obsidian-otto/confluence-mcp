// Package tools — register.go: the single point where the 18
// Confluence tool handlers are wired into a *mcp.Server.
//
// RegisterAll is called exactly once: from internal/server.New
// after the mcp.Server is constructed (transport + name/version
// options applied) but before Serve() is called. It is the public
// "give me the 5 tools" entry point; the server package owns the
// server, the tools package owns the tools.
//
// Tool naming contract:
//
//   - The 5 names registered here — conf_get, conf_post, conf_put,
//     conf_patch, conf_delete — are the EXACT names Hermes (and
//     any MCP client) will see in `tools/list` and the EXACT
//     names clients will use in `tools/call` requests. After the
//     `mcp_confluence_` server prefix, the on-the-wire tool
//     identifiers become mcp_confluence_conf_get, etc. Any drift
//     from this list is a wire-level breaking change; tests in
//     server_test.go assert the set membership.
//
//   - The 5 descriptions are the CONF_*_DESCRIPTION constants from
//     descriptions.go. They are the exact byte-for-byte strings
//     the upstream server registers, copied verbatim. Drift
//     from the upstream wording is a bug.
//
// Handler bridging:
//
//	mcp-golang's RegisterTool expects a handler with signature
//	`func(ctx, Args) (*mcp_golang.ToolResponse, error)` where Args
//	is a struct type the lib introspects for its JSON schema.
//
//	The Phase 7 Handler type is `func(ctx, args json.RawMessage)
//	(string, error)` — a different shape. Rather than rewrite
//	Phase 7's handler layer (which has a useful 9-step pipeline
//	and panic recovery), RegisterAll defines a thin per-tool
//	adapter closure:
//
//	  func(ctx context.Context, args T) (*mcp_golang.ToolResponse, error) {
//	      raw, _ := json.Marshal(args)              // re-marshal the typed struct
//	      result, err := safeHandler(name, HandleX)(ctx, client, raw)
//	      if err != nil {
//	          return nil, err                        // lib wraps as tool error
//	      }
//	      return mcp_golang.NewToolResponse(mcp_golang.NewTextContent(result)), nil
//	  }
//
//	The re-marshal is a small cost (a few hundred bytes per call)
//	that buys us zero duplication between the JSON-RPC arg shape
//	(declared in args.go) and the internal handler shape (which
//	needs json.RawMessage for its error-envelope path).
//
// Error propagation:
//
//	When the inner handler returns (result, err) with err != nil,
//	we return (nil, err) so mcp-golang wraps the error into a
//	`toolResponseSentError` that the wire protocol surfaces as a
//	`tools/call` result with `isError: true`. This matches the
//	upstream's `isError` content-block pattern documented in
//	specs/04-mcp-golang-framework/01-server-api.md §"Error
//	responses" and specs/09-anti-patterns/03-error-shapes.md.
//
//	The mcp-golang library DOES support the alternative pattern
//	of returning a *ToolResponse with error text and a nil error
//	(the upstream's `isError: true` content block). We chose the
//	(nil, err) shape because:
//
//	  1. It propagates the typed error from executeRequest
//	     (e.g. *atlassian.APIError) without losing the type
//	     information.
//	  2. It keeps the adapter closure a one-liner, so the
//	     per-tool boilerplate is minimized.
//	  3. mcp-golang's wire-level encoding of a tool error is
//	     functionally equivalent to the upstream `isError: true`
//	     pattern — both surface as a `tools/call` result with
//	     `isError: true` at the JSON-RPC layer.
package tools

import (
	"context"
	"encoding/json"
	"fmt"

	mcp "github.com/metoro-io/mcp-golang"

	"github.com/bennie/mcp-confluence/internal/atlassian"
)

// RegisterAll wires the 18 Confluence tool handlers into srv. It is
// idempotent only in the sense that mcp-golang's RegisterTool
// overwrites any prior registration of the same name; the project
// does not call RegisterAll twice. The function returns the first
// registration error (if any) so the caller can surface it as a
// fail-fast condition.
//
// The 18 names are: 5 CRUD tools (conf_get / conf_post / conf_put /
// conf_patch / conf_delete) byte-for-byte from the upstream
// `@aashari/mcp-server-atlassian-confluence` v3.3.0 tool surface;
// 5 post-v1 quality-of-life tools (conf_list_spaces / conf_list_pages
// / conf_get_page_body / conf_search / conf_help) added in the
// 2026-07-10 audit closure; 3 v2 markdown round-trip tools
// (conf_post_markdown / conf_put_markdown / conf_get_page_markdown)
// added in Phase 14/15; 3 v3 attachment tools (conf_upload_attachment
// / conf_list_attachments / conf_delete_attachment); 1 v3 drawio
// orchestrator (conf_upload_drawio); and 1 v1.x page-tree tool
// (conf_get_page_tree, added 2026-07-14). Do not edit the 10
// upstream-aligned names without re-running the byte-comparison test
// in descriptions_test.go. server_test.go's
// TestNew_RegistersAllEighteenTools and
// TestNew_RegistersExactlyEighteenTools enforce the set membership
// at the mcp-golang introspection layer.
//
// Parameter validation:
//
//   - srv == nil → error.
//   - client == nil → error.
//
// Both checks happen before any RegisterTool call so a partial
// registration never leaks to the caller.
func RegisterAll(srv *mcp.Server, client *atlassian.Client) error {
	if srv == nil {
		return fmt.Errorf("tools.RegisterAll: srv is nil")
	}
	if client == nil {
		return fmt.Errorf("tools.RegisterAll: client is nil")
	}

	// The 5 registrations. Each adapter closure:
	//
	//   1. re-marshals the typed args struct back to JSON bytes
	//      (Phase 7's handlers expect json.RawMessage)
	//   2. invokes the safeHandler-wrapped HandleX function
	//   3. on err, returns (nil, err) so mcp-golang marks the
	//      tool result as isError:true
	//   4. on success, returns a *ToolResponse containing the
	//      encoded body as a single text content block
	//
	// The phase name passed to safeHandler is the tool name
	// itself (e.g. "conf_get") so the operator logs identify the
	// tool that panicked. The phase name is the TOOL name (not
	// "phase 8" or similar) because safeHandler is also used in
	// the future for test-time injection (Phase 7's panic test
	// uses "test-panic"); the tool name is the stable label
	// operators will recognise.

	type reg struct {
		name        string
		description string
		handler     any
	}

	registrations := []reg{
		{
			name:        "conf_get",
			description: CONF_GET_DESCRIPTION,
			handler: func(ctx context.Context, args GetArgs) (*mcp.ToolResponse, error) {
				return invokeTool(ctx, "conf_get", HandleGet, client, args)
			},
		},
		{
			name:        "conf_post",
			description: CONF_POST_DESCRIPTION,
			handler: func(ctx context.Context, args PostArgs) (*mcp.ToolResponse, error) {
				return invokeTool(ctx, "conf_post", HandlePost, client, args)
			},
		},
		{
			name:        "conf_put",
			description: CONF_PUT_DESCRIPTION,
			handler: func(ctx context.Context, args PutArgs) (*mcp.ToolResponse, error) {
				return invokeTool(ctx, "conf_put", HandlePut, client, args)
			},
		},
		{
			name:        "conf_patch",
			description: CONF_PATCH_DESCRIPTION,
			handler: func(ctx context.Context, args PatchArgs) (*mcp.ToolResponse, error) {
				return invokeTool(ctx, "conf_patch", HandlePatch, client, args)
			},
		},
		{
			name:        "conf_delete",
			description: CONF_DELETE_DESCRIPTION,
			handler: func(ctx context.Context, args DeleteArgs) (*mcp.ToolResponse, error) {
				return invokeTool(ctx, "conf_delete", HandleDelete, client, args)
			},
		},
		// Post-v1 quality-of-life tools (added per audit 2026-07-10).
		// Each wraps one or two HTTP calls through the standard
		// executeRequest helper, so the 9-step TOON / JMESPath /
		// truncation pipeline is shared with the CRUD tools.
		{
			name:        "conf_list_spaces",
			description: CONF_LIST_SPACES_DESCRIPTION,
			handler: func(ctx context.Context, args ListSpacesArgs) (*mcp.ToolResponse, error) {
				return invokeTool(ctx, "conf_list_spaces", HandleListSpaces, client, args)
			},
		},
		{
			name:        "conf_list_pages",
			description: CONF_LIST_PAGES_DESCRIPTION,
			handler: func(ctx context.Context, args ListPagesArgs) (*mcp.ToolResponse, error) {
				return invokeTool(ctx, "conf_list_pages", HandleListPages, client, args)
			},
		},
		{
			name:        "conf_get_page_body",
			description: CONF_GET_PAGE_BODY_DESCRIPTION,
			handler: func(ctx context.Context, args GetPageBodyArgs) (*mcp.ToolResponse, error) {
				return invokeTool(ctx, "conf_get_page_body", HandleGetPageBody, client, args)
			},
		},
		// v1.x — Page-tree index tool (added 2026-07-14). Three
		// v2 endpoints merged into one envelope. Wire shape
		// documented in specs/13-page-tree-index/. Distinct from
		// conf_list_pages (which is across-space) and conf_search
		// (which is text-based).
		{
			name:        "conf_get_page_tree",
			description: CONF_GET_PAGE_TREE_DESCRIPTION,
			handler: func(ctx context.Context, args GetPageTreeArgs) (*mcp.ToolResponse, error) {
				return invokeTool(ctx, "conf_get_page_tree", HandleGetPageTree, client, args)
			},
		},
		{
			name:        "conf_search",
			description: CONF_SEARCH_DESCRIPTION,
			handler: func(ctx context.Context, args SearchArgs) (*mcp.ToolResponse, error) {
				return invokeTool(ctx, "conf_search", HandleSearch, client, args)
			},
		},
		{
			name:        "conf_help",
			description: CONF_HELP_DESCRIPTION,
			handler: func(ctx context.Context, args HelpArgs) (*mcp.ToolResponse, error) {
				return invokeTool(ctx, "conf_help", HandleHelp, client, args)
			},
		},
		// v2 — Markdown round-trip tools (Phase 14/15, local
		// additions — the upstream has no markdown tools). Each
		// takes a markdown source (inline OR file path), converts
		// it to/from Confluence storage XHTML inside the binary
		// via internal/markdown, and delegates to the existing
		// CRUD handlers so the 9-step TOON / JMESPath /
		// truncation pipeline is shared.
		{
			name:        "conf_post_markdown",
			description: CONF_POST_MARKDOWN_DESCRIPTION,
			handler: func(ctx context.Context, args PostMarkdownArgs) (*mcp.ToolResponse, error) {
				return invokeTool(ctx, "conf_post_markdown", HandlePostMarkdown, client, args)
			},
		},
		{
			name:        "conf_put_markdown",
			description: CONF_PUT_MARKDOWN_DESCRIPTION,
			handler: func(ctx context.Context, args PutMarkdownArgs) (*mcp.ToolResponse, error) {
				return invokeTool(ctx, "conf_put_markdown", HandlePutMarkdown, client, args)
			},
		},
		{
			name:        "conf_get_page_markdown",
			description: CONF_GET_PAGE_MARKDOWN_DESCRIPTION,
			handler: func(ctx context.Context, args GetPageMarkdownArgs) (*mcp.ToolResponse, error) {
				return invokeTool(ctx, "conf_get_page_markdown", HandleGetPageMarkdown, client, args)
			},
		},
		// v3 — Attachment tools. conf_upload_attachment hits the
		// v1 REST endpoint (POST .../child/attachment with
		// multipart/form-data + X-Atlassian-Token: no-check);
		// conf_list_attachments and conf_delete_attachment use
		// the v2 endpoints. See
		// specs/11-attachments/01-research-and-surface.md for
		// the v1/v2 split rationale.
		{
			name:        "conf_upload_attachment",
			description: CONF_UPLOAD_ATTACHMENT_DESCRIPTION,
			handler: func(ctx context.Context, args UploadAttachmentArgs) (*mcp.ToolResponse, error) {
				return invokeTool(ctx, "conf_upload_attachment", HandleUploadAttachment, client, args)
			},
		},
		{
			name:        "conf_list_attachments",
			description: CONF_LIST_ATTACHMENTS_DESCRIPTION,
			handler: func(ctx context.Context, args ListAttachmentsArgs) (*mcp.ToolResponse, error) {
				return invokeTool(ctx, "conf_list_attachments", HandleListAttachments, client, args)
			},
		},
		{
			name:        "conf_delete_attachment",
			description: CONF_DELETE_ATTACHMENT_DESCRIPTION,
			handler: func(ctx context.Context, args DeleteAttachmentArgs) (*mcp.ToolResponse, error) {
				return invokeTool(ctx, "conf_delete_attachment", HandleDeleteAttachment, client, args)
			},
		},
		// v3 — drawio attachment orchestrator. Uploads a
		// .drawio / .drawio.png / .drawio.svg file AND embeds
		// it on the page in one call. See
		// specs/12-drawio-attachments/01-research-and-surface.md
		// for the 2-step wire flow (v1 upload + v2 page PUT)
		// and the macro envelope shape.
		{
			name:        "conf_upload_drawio",
			description: CONF_UPLOAD_DRAWIO_DESCRIPTION,
			handler: func(ctx context.Context, args UploadDrawioArgs) (*mcp.ToolResponse, error) {
				return invokeTool(ctx, "conf_upload_drawio", HandleUploadDrawio, client, args)
			},
		},
	}

	for _, r := range registrations {
		if err := srv.RegisterTool(r.name, r.description, r.handler); err != nil {
			return fmt.Errorf("tools.RegisterAll: register %s: %w", r.name, err)
		}
	}
	return nil
}

// invokeTool is the per-call bridge between the mcp-golang
// adapter-closure world (typed args + *ToolResponse return) and
// the Phase 7 Handler world (json.RawMessage args + string
// return). The bridge:
//
//  1. re-marshals the typed args struct to JSON bytes so the
//     Phase 7 handlers' json.Unmarshal call (in HandleX) can
//     decode them again. This is a double-encoding round trip,
//     but the args are tiny (a path, an optional query map, an
//     optional body) so the cost is negligible — sub-microsecond
//     for the worst case.
//  2. wraps a closure (binding the *atlassian.Client) in
//     safeHandler with the tool's name as the panic-recovery
//     phase label. The resulting Handler matches the
//     `func(ctx, json.RawMessage) (string, error)` signature
//     that safeHandler expects.
//  3. on err, propagates the error to mcp-golang's
//     createWrappedToolHandler, which encodes it as a tool
//     result with isError:true.
//  4. on success, wraps the encoded body string in a single
//     text content block.
//
// T is the typed args struct (GetArgs, PostArgs, etc.); the lib's
// generic JSON-schema reflection produces the input schema from
// its exported fields. We use a `func` parameter for the per-method
// HandleX entry point (rather than the Handler type) because the
// HandleX functions take an extra *atlassian.Client argument that
// we close over from the enclosing RegisterAll scope. After
// safeHandler wraps a closure that pre-binds the client, the
// signature collapses to the canonical Handler shape.
func invokeTool[T any](
	ctx context.Context,
	name string,
	handle func(context.Context, *atlassian.Client, json.RawMessage) (string, error),
	client *atlassian.Client,
	args T,
) (*mcp.ToolResponse, error) {
	raw, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("%s: encode args: %w", name, err)
	}
	// Bind the client to a closure that matches the Handler
	// type, then wrap in safeHandler. safeHandler returns a
	// Handler — we call it with (ctx, raw) to get the encoded
	// response string (or an error).
	bound := func(ctx context.Context, raw json.RawMessage) (string, error) {
		return handle(ctx, client, raw)
	}
	result, err := safeHandler(name, bound)(ctx, raw)
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResponse(mcp.NewTextContent(result)), nil
}
