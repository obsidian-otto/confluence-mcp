// Package tools — convenience.go: the quality-of-life wrappers that
// hide Confluence v1/v2 API path memory behind tool names with
// friendlier argument shapes.
//
// Each handler in this file:
//
//   - Decodes its typed args struct (ListSpacesArgs / ListPagesArgs
//     / GetPageBodyArgs / SearchArgs / HelpArgs).
//   - Translates the friendly args into a (path, query map, body)
//     triple that the existing executeRequest() helper consumes.
//   - Calls executeRequest() under the hood so the 9-step TOON /
//     JMESPath / truncation pipeline is shared with the CRUD
//     tools — no duplication of output encoding.
//
// The four network-touching wrappers (HandleListSpaces,
// HandleListPages, HandleGetPageBody, HandleSearch) each delegate
// to executeRequest with the appropriate HTTP method and request
// path. HandleHelp does NOT touch the network; it returns a
// hard-coded surface map derived from the registered CONF_*_DESCRIPTION
// constants so the MCP server can describe itself without depending
// on Confluence being reachable.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/bennie/mcp-confluence/internal/atlassian"
	"github.com/bennie/mcp-confluence/internal/templates"
	"github.com/bennie/mcp-confluence/internal/toon"
)

// HandleListSpaces is the `conf_list_spaces` handler. It maps a
// typed `ListSpacesArgs` into the v2 API path + query, then
// delegates to executeRequest so the truncation / JMESPath / TOON
// pipeline handles the rest.
//
// Path: GET /wiki/api/v2/spaces
// Query: limit, cursor, type, status (when non-empty)
func HandleListSpaces(ctx context.Context, client *atlassian.Client, args json.RawMessage) (string, error) {
	var a ListSpacesArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("conf_list_spaces: decode args: %w", err)
	}
	limit := strconv.Itoa(defaultLimit(a.Limit, 25, 250))
	query := map[string]string{
		"limit":  limit,
		"cursor": a.Cursor,
		"type":   a.Type,
		"status": a.Status,
	}
	// The upstream API rejects empty-string query params silently
	// but they appear in the URL which is wasteful; strip them.
	stripEmpty(query)
	// Re-add the output format so executeRequest sees it.
	query["limit"] = limit
	// OutputFormat is on the args struct but executeRequest reads it
	// off GetArgs — we synthesise that here.
	return executeRequest(ctx, client, GetArgs{
		Path:         "/wiki/api/v2/spaces",
		Query:        query,
		OutputFormat: a.OutputFormat,
	}, "GET", nil)
}

// HandleListPages is the `conf_list_pages` handler. Filters by
// space-id (preferred) or space-key, optionally narrows by title
// substring and status, and forwards to the v2 pages endpoint with
// the appropriate sort and body-format.
//
// Path: GET /wiki/api/v2/pages
// Query (per Confluence v2 docs): space-id, space-key, title,
// status, limit, cursor, sort, body-format.
func HandleListPages(ctx context.Context, client *atlassian.Client, args json.RawMessage) (string, error) {
	var a ListPagesArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("conf_list_pages: decode args: %w", err)
	}
	limit := strconv.Itoa(defaultLimit(a.Limit, 25, 250))
	query := map[string]string{
		"limit":       limit,
		"cursor":      a.Cursor,
		"space-id":    a.SpaceID,
		"space-key":   a.SpaceKey,
		"title":       a.Title,
		"status":      a.Status,
		"sort":        a.SortField,
		"body-format": a.BodyFormat,
	}
	stripEmpty(query)
	query["limit"] = limit
	return executeRequest(ctx, client, GetArgs{
		Path:         "/wiki/api/v2/pages",
		Query:        query,
		OutputFormat: a.OutputFormat,
	}, "GET", nil)
}

// HandleGetPageBody is the `conf_get_page_body` handler. Reads
// the body of a page. Path note: the Confluence Cloud v2 API does
// NOT support a `/wiki/api/v2/pages/{id}/body` sub-endpoint — the
// body is inlined into the GET-page response when the caller
// supplies `body-format=<storage|view|atlas_doc_format>` as a
// query-string parameter. Calling the absent /body endpoint
// returns 404 (we propagate that as a normal APIError). This
// handler therefore GETs the parent page with the body-format
// filter and returns the body portion. If the caller wants the
// full page metadata too, they should use conf_get with the
// same path.
func HandleGetPageBody(ctx context.Context, client *atlassian.Client, args json.RawMessage) (string, error) {
	var a GetPageBodyArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("conf_get_page_body: decode args: %w", err)
	}
	if a.PageID == "" {
		return "", fmt.Errorf("conf_get_page_body: page-id is required")
	}
	bodyFormat := a.BodyFormat
	if bodyFormat == "" {
		bodyFormat = "storage"
	}
	// The path itself doesn't carry the query — the body-format
	// is appended here so the upstream URL is well-formed. The
	// template literal in internal/templates makes the prefix,
	// the page-id slot, and the body-format query skeleton
	// explicit at a glance; an accidental missing slash or
	// missing '?' would show up in the literal rather than
	// hiding in a %s placeholder.
	path := templates.PageBodyPath(a.PageID, bodyFormat)
	return executeRequest(ctx, client, GetArgs{
		Path:         path,
		OutputFormat: a.OutputFormat,
	}, "GET", nil)
}

// HandleSearch is the `conf_search` handler. The CQL parameter is
// forwarded as a URL query parameter on the v1 search endpoint;
// pagination uses start/limit instead of cursor because v1 search
// predates cursor pagination.
//
// Path: GET /wiki/rest/api/search
// Query: cql, limit, start.
func HandleSearch(ctx context.Context, client *atlassian.Client, args json.RawMessage) (string, error) {
	var a SearchArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("conf_search: decode args: %w", err)
	}
	if a.CQL == "" {
		return "", fmt.Errorf("conf_search: cql is required")
	}
	limit := strconv.Itoa(defaultLimit(a.Limit, 25, 100))
	// Confluence v1 search expects `start` as a numeric string when
	// non-zero; omit otherwise.
	start := ""
	if a.Start > 0 {
		start = strconv.Itoa(a.Start)
	}
	query := map[string]string{
		"cql":     a.CQL,
		"limit":   limit,
		"start":   start,
		"excerpt": a.ExcludedContent,
	}
	stripEmpty(query)
	// re-set limit after stripEmpty (it was non-empty by definition above)
	query["limit"] = limit
	return executeRequest(ctx, client, GetArgs{
		Path:         "/wiki/rest/api/search",
		Query:        query,
		OutputFormat: a.OutputFormat,
	}, "GET", nil)
}

// HandleHelp is the `conf_help` handler. It does NOT call executeRequest
// or touch the network — the response is built locally from the
// already-loaded CONF_*_DESCRIPTION constants and a small args map.
// That makes it useful even when Confluence is unreachable (e.g.
// during initial setup, or when credentials are missing).
//
// Output shape:
//
//	{
//	  "tools": {
//	    "<tool-name>": {
//	      "description": "<first paragraph of the description>",
//	      "args": {"<arg>": "<short hint>", ...},
//	      "example": "<one-line canonical invocation>"
//	    },
//	    ...
//	  },
//	  "topic_filter": "<the requested topic, or 'all'>",
//	  "output_format": "toon" | "json"
//	}
func HandleHelp(_ context.Context, _ *atlassian.Client, args json.RawMessage) (string, error) {
	var a HelpArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("conf_help: decode args: %w", err)
	}

	surface := helpSurface()
	topic := a.Topic
	if topic == "" {
		topic = "all"
	}

	// Note is self-reporting — `len(surface)` is always the live
	// count, so adding or removing a tool from helpSurface() can
	// never make this string lie.
	out := map[string]any{
		"topic": topic,
		"tools": filterSurfaceByTopic(surface, topic),
		"note":  fmt.Sprintf("All %d tools return TOON format by default; set outputFormat=\"json\" to get plain JSON.", len(surface)),
		"defaults": map[string]any{
			"limit":        "25 (max 250 for /v2/ endpoints; max 100 for search)",
			"outputFormat": "",
		},
	}

	if a.OutputFormat == "json" {
		return jsonMarshal(out)
	}
	// Default: TOON. Delegated to the same Marshal call executeRequest
	// uses so the format is identical to the network-round-tripping
	// tools.
	return toonMarshal(out)
}

// defaultLimit returns the effective limit, applying the supplied
// default and an optional cap. Zero / negative / exceeding-cap
// values fall back to the default; values above the cap are
// clamped down to the cap.
func defaultLimit(requested, def, cap int) int {
	switch {
	case requested <= 0:
		return def
	case requested > cap:
		return cap
	default:
		return requested
	}
}

// stripEmpty removes entries whose value is ""; this prevents empty
// query params from cluttering the URL and matching things like
// `type=` which some upstreams reject.
func stripEmpty(q map[string]string) {
	for k, v := range q {
		if v == "" {
			delete(q, k)
		}
	}
}

// helpEntry is one tool's row in the surface map exposed by
// `conf_help`. The fields are deliberately short — a `conf_help`
// response should fit comfortably in a single LLM context window,
// not reproduce the full description text.
type helpEntry struct {
	Description string            `json:"description"`
	Args        map[string]string `json:"args"`
	Example     string            `json:"example"`
}

// helpSurface returns the full tool surface (currently 17 tools —
// the 5 original CRUD tools, 5 convenience tools, 3 markdown
// round-trip tools, 3 attachment tools, and 1 drawio orchestrator)
// in tool-name sort order. The descriptions are derived by
// truncating the registered CONF_*_DESCRIPTION strings to their
// first paragraph — every description in descriptions.go starts
// with a single-line summary that the help response can reuse
// directly.
//
// If you add or remove a tool here, also update the want-list in
// TestHandleHelp_ReturnsSurface (convention_test.go).
func helpSurface() map[string]helpEntry {
	surface := map[string]helpEntry{
		"conf_get": {
			Description: firstParagraph(CONF_GET_DESCRIPTION),
			Args: map[string]string{
				"path":         "Confluence REST API path (e.g. /wiki/api/v2/spaces)",
				"query":        "Optional URL query parameters",
				"jq":           "Optional JMESPath filter",
				"outputFormat": "empty = TOON; 'json' for plain JSON",
			},
			Example: `conf_get path="/wiki/api/v2/spaces?limit=5"`,
		},
		"conf_post": {
			Description: firstParagraph(CONF_POST_DESCRIPTION),
			Args: map[string]string{
				"path":         "Confluence REST path (POST target)",
				"query":        "Optional URL query parameters",
				"body":         "JSON object for the request body",
				"jq":           "Optional JMESPath filter",
				"outputFormat": "empty = TOON; 'json' for plain JSON",
			},
			Example: `conf_post path="/wiki/api/v2/pages" body={"spaceId":"123","status":"current","title":"Hi"}`,
		},
		"conf_put": {
			Description: firstParagraph(CONF_PUT_DESCRIPTION),
			Args: map[string]string{
				"path":         "Confluence REST path (PUT target); version.number incremented by 1",
				"query":        "Optional URL query parameters",
				"body":         "JSON object — the full replacement resource",
				"jq":           "Optional JMESPath filter",
				"outputFormat": "empty = TOON; 'json' for plain JSON",
			},
			Example: `conf_put path="/wiki/api/v2/pages/42" body={"id":"42","version":{"number":2}}`,
		},
		"conf_patch": {
			Description: firstParagraph(CONF_PATCH_DESCRIPTION),
			Args: map[string]string{
				"path":         "Confluence REST path (PATCH target)",
				"query":        "Optional URL query parameters",
				"body":         "JSON array of patch ops (RFC 6902 style)",
				"jq":           "Optional JMESPath filter",
				"outputFormat": "empty = TOON; 'json' for plain JSON",
			},
			Example: `conf_patch path="/wiki/api/v2/spaces/42" body=[{"op":"replace","path":"/name","value":"New"}]`,
		},
		"conf_delete": {
			Description: firstParagraph(CONF_DELETE_DESCRIPTION),
			Args: map[string]string{
				"path":         "Confluence REST path (DELETE target)",
				"query":        "Optional URL query parameters",
				"jq":           "Optional JMESPath filter",
				"outputFormat": "empty = TOON; 'json' for plain JSON",
			},
			Example: `conf_delete path="/wiki/api/v2/pages/42"`,
		},
		"conf_list_spaces": {
			Description: firstParagraph(CONF_LIST_SPACES_DESCRIPTION),
			Args: map[string]string{
				"limit":        "Default 25, max 250",
				"cursor":       "Pagination cursor from a prior call",
				"type":         "'personal' or 'global'",
				"status":       "'current' or 'archived'",
				"outputFormat": "empty = TOON; 'json' for plain JSON",
			},
			Example: `conf_list_spaces type="personal" limit=10`,
		},
		"conf_list_pages": {
			Description: firstParagraph(CONF_LIST_PAGES_DESCRIPTION),
			Args: map[string]string{
				"space-id":     "Numeric space id (recommended)",
				"space-key":    "Space key (alternative to space-id)",
				"title":        "Substring filter on page titles",
				"status":       "'current' or 'archived'",
				"limit":        "Default 25, max 250",
				"cursor":       "Pagination cursor",
				"sort":         "Default id; e.g. '-modified-date'",
				"body-format":  "Omit for light responses; 'storage' for full",
				"outputFormat": "empty = TOON; 'json' for plain JSON",
			},
			Example: `conf_list_pages space-id="780763211" limit=10 sort="-modified-date"`,
		},
		"conf_get_page_body": {
			Description: firstParagraph(CONF_GET_PAGE_BODY_DESCRIPTION),
			Args: map[string]string{
				"page-id":      "Numeric page id (required)",
				"body-format":  "'storage' (default) | 'view' | 'atlas_doc_format'",
				"outputFormat": "empty = TOON; 'json' for plain JSON",
			},
			Example: `conf_get_page_body page-id="163935" body-format="storage"`,
		},
		"conf_search": {
			Description: firstParagraph(CONF_SEARCH_DESCRIPTION),
			Args: map[string]string{
				"cql":             "Confluence Query Language expression (required)",
				"limit":           "Default 25, max 100",
				"start":           "Pagination offset (v1 search)",
				"excludedContent": "Optional excerpt selector",
				"outputFormat":    "empty = TOON; 'json' for plain JSON",
			},
			Example: `conf_search cql="type=page AND text~mcp-confluence" limit=10`,
		},
		"conf_help": {
			Description: firstParagraph(CONF_HELP_DESCRIPTION),
			Args: map[string]string{
				"topic":        "Optional tool name to filter; 'all' (default) for the full surface",
				"outputFormat": "empty = TOON; 'json' for plain JSON",
			},
			Example: `conf_help topic="conf_search"`,
		},
		// v2 — Markdown round-trip tools. Accept a markdown source
		// inline or from a file path; the handler converts to/from
		// Confluence storage XHTML inside the binary. The
		// markdown/markdownFile argument names match the JSON
		// schema on the registered MCP tool.
		"conf_post_markdown": {
			Description: firstParagraph(CONF_POST_MARKDOWN_DESCRIPTION),
			Args: map[string]string{
				"spaceId":      "Numeric space id where the new page will live (required)",
				"title":        "Title for the new page (required)",
				"markdown":     "Inline markdown source (alternative to markdownFile)",
				"markdownFile": "Absolute path to a markdown file on disk (alternative to markdown)",
				"parentId":     "Optional parent page id; omit for a top-level page",
				"status":       "'current' (default) for an active page",
				"outputFormat": "empty = TOON; 'json' for plain JSON",
			},
			Example: `conf_post_markdown spaceId="163842" title="Hello" markdown="# Heading"`,
		},
		"conf_put_markdown": {
			Description: firstParagraph(CONF_PUT_MARKDOWN_DESCRIPTION),
			Args: map[string]string{
				"pageId":       "Numeric page id of the page to update (required)",
				"title":        "New page title (omit to keep the existing title)",
				"markdown":     "Inline markdown source (alternative to markdownFile)",
				"markdownFile": "Absolute path to a markdown file on disk (alternative to markdown)",
				"outputFormat": "empty = TOON; 'json' for plain JSON",
			},
			Example: `conf_put_markdown pageId="163935" markdown="## Updated section"`,
		},
		"conf_get_page_markdown": {
			Description: firstParagraph(CONF_GET_PAGE_MARKDOWN_DESCRIPTION),
			Args: map[string]string{
				"page-id":      "Numeric page id (required)",
				"outputFormat": "empty = TOON; 'json' for plain JSON",
				"jq":           "Optional JMESPath filter on the {pageId, title, markdown} envelope",
			},
			Example: `conf_get_page_markdown page-id="163935" jq="markdown"`,
		},
		// v3 — Attachment tools. Upload hits v1 (multipart),
		// list/delete hit v2. Argument keys match the registered
		// MCP JSON schema.
		"conf_upload_attachment": {
			Description: firstParagraph(CONF_UPLOAD_ATTACHMENT_DESCRIPTION),
			Args: map[string]string{
				"pageId":       "Numeric page id where the attachment will live (required)",
				"filePath":     "Absolute path to the file on disk (required); PNG/PDF/drawio/any binary",
				"comment":      "Optional changelog message",
				"minorEdit":    "Mark new attachment version as a minor edit (default true)",
				"outputFormat": "empty = TOON; 'json' for plain JSON",
				"jq":           "Optional JMESPath filter on the v1 ContentPageScheme response",
			},
			Example: `conf_upload_attachment pageId="163935" filePath="/tmp/diagram.drawio" comment="initial upload"`,
		},
		"conf_list_attachments": {
			Description: firstParagraph(CONF_LIST_ATTACHMENTS_DESCRIPTION),
			Args: map[string]string{
				"pageId":       "Numeric page id whose attachments to list (required)",
				"cursor":       "Opaque pagination cursor",
				"limit":        "Default 25; max 100",
				"mediaType":    "Substring filter (e.g. 'image')",
				"filename":     "Exact filename filter",
				"outputFormat": "empty = TOON; 'json' for plain JSON",
				"jq":           "Optional JMESPath filter",
			},
			Example: `conf_list_attachments pageId="163935" mediaType="image"`,
		},
		"conf_delete_attachment": {
			Description: firstParagraph(CONF_DELETE_ATTACHMENT_DESCRIPTION),
			Args: map[string]string{
				"attachmentId": "Numeric attachment id (required)",
				"purge":        "Set true to permanently delete instead of moving to trash",
				"outputFormat": "empty = TOON; 'json' for plain JSON",
				"jq":           "Optional JMESPath filter (most deletes return 204 No Content)",
			},
			Example: `conf_delete_attachment attachmentId="att219152391"`,
		},
		// v3 — drawio orchestrator. Accepts a .drawio file
		// (auto-wrapped into .drawio.png) or a pre-prepared
		// .drawio.png / .drawio.svg. The three mutually-
		// exclusive input flags are drawioFile / drawioPngFile
		// / drawioSvgFile.
		"conf_upload_drawio": {
			Description: firstParagraph(CONF_UPLOAD_DRAWIO_DESCRIPTION),
			Args: map[string]string{
				"pageId":             "Numeric id of an existing page to embed on (mutually exclusive with spaceId)",
				"spaceId":            "Numeric space id for creating a new page (mutually exclusive with pageId)",
				"title":              "Title for the new page (required when spaceId is set)",
				"drawioFile":         "Path to a standalone .drawio XML file (auto-wrapped into .drawio.png)",
				"drawioPngFile":      "Path to an already-prepared .drawio.png (uploaded verbatim)",
				"drawioSvgFile":      "Path to a .drawio.svg (uploaded verbatim; drawio XML is in the root 'content' attribute)",
				"diagramDisplayName": "Display name (defaults to input filename without extension)",
				"width":              "Macro width in pixels (default 1151)",
				"height":             "Macro height in pixels (default 911)",
				"comment":            "Optional attachment changelog",
				"outputFormat":       "empty = TOON; 'json' for plain JSON",
				"jq":                 "Optional JMESPath filter on the response envelope",
			},
			Example: `conf_upload_drawio pageId="163935" drawioFile="/tmp/architecture.drawio" diagramDisplayName="architecture"`,
		},
	}
	return surface
}

// filterSurfaceByTopic returns either the whole surface (when
// topic is "" or "all") or the single entry whose key matches the
// requested topic. Unknown topics return an empty map; the
// surrounding response still includes the requested topic name so
// the caller can tell their filter was a miss.
func filterSurfaceByTopic(surface map[string]helpEntry, topic string) map[string]helpEntry {
	if topic == "" || topic == "all" {
		return surface
	}
	if e, ok := surface[topic]; ok {
		return map[string]helpEntry{topic: e}
	}
	return map[string]helpEntry{}
}

// firstParagraph returns the first non-empty "paragraph" (a run of
// non-blank lines) of s. Used to derive a one-line summary for
// `conf_help` from the multi-paragraph CONF_*_DESCRIPTION
// constants without duplicating the wording.
func firstParagraph(s string) string {
	lines := strings.Split(s, "\n")
	var out strings.Builder
	seen := 0
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" {
			if out.Len() > 0 {
				break
			}
			continue
		}
		if out.Len() > 0 {
			out.WriteByte(' ')
		}
		out.WriteString(trim)
		seen++
		if seen >= 3 { // cap at three lines worth of summary
			break
		}
	}
	return out.String()
}

// jsonMarshal is a thin wrapper over json.Marshal that errors out
// the encoding instead of panicking. The output is canonical
// (sorted keys, no indent) for compactness.
func jsonMarshal(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// toonMarshal delegates to the same encoder used by the
// network-round-tripping tools so the output format is identical.
// See internal/toon/encode.go for the encoder reference.
func toonMarshal(v any) (string, error) {
	b, err := toon.Encode(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
