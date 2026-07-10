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
	"strings"

	"github.com/bennie/mcp-confluence/internal/atlassian"
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
	query := map[string]string{
		"limit":  fmt.Sprintf("%d", defaultLimit(a.Limit, 25, 250)),
		"cursor": a.Cursor,
		"type":   a.Type,
		"status": a.Status,
	}
	// The upstream API rejects empty-string query params silently
	// but they appear in the URL which is wasteful; strip them.
	stripEmpty(query)
	// Re-add the output format so executeRequest sees it.
	query["limit"] = fmt.Sprintf("%d", defaultLimit(a.Limit, 25, 250))
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
	limit := defaultLimit(a.Limit, 25, 250)
	query := map[string]string{
		"limit":       fmt.Sprintf("%d", limit),
		"cursor":      a.Cursor,
		"space-id":    a.SpaceID,
		"space-key":   a.SpaceKey,
		"title":       a.Title,
		"status":      a.Status,
		"sort":        a.SortField,
		"body-format": a.BodyFormat,
	}
	stripEmpty(query)
	query["limit"] = fmt.Sprintf("%d", limit)
	return executeRequest(ctx, client, GetArgs{
		Path:         "/wiki/api/v2/pages",
		Query:        query,
		OutputFormat: a.OutputFormat,
	}, "GET", nil)
}

// HandleGetPageBody is the `conf_get_page_body` handler. Reads
// /wiki/api/v2/pages/{id}/body with the chosen body-format. The
// upstream response contains a `value` field whose contents are
// format-specific — XHTML for storage, rendered HTML for view, ADF
// JSON for atlas_doc_format — and we trust the layer above to
// surface that to the caller.
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
	return executeRequest(ctx, client, GetArgs{
		Path: "/wiki/api/v2/pages/" + a.PageID + "/body",
		Query: map[string]string{
			"body-format": bodyFormat,
		},
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
	limit := defaultLimit(a.Limit, 25, 100)
	// Confluence v1 search expects `start` as a numeric string when
	// non-zero; omit otherwise.
	start := ""
	if a.Start > 0 {
		start = fmt.Sprintf("%d", a.Start)
	}
	query := map[string]string{
		"cql":     a.CQL,
		"limit":   fmt.Sprintf("%d", limit),
		"start":   start,
		"excerpt": a.ExcludedContent,
	}
	stripEmpty(query)
	// re-set limit after stripEmpty (it was non-empty by definition above)
	query["limit"] = fmt.Sprintf("%d", limit)
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

	out := map[string]any{
		"topic": topic,
		"tools": filterSurfaceByTopic(surface, topic),
		"note":  "All ten tools return TOON format by default; set outputFormat=\"json\" to get plain JSON.",
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

// helpSurface returns the full ten-tool surface in tool-name sort
// order. The descriptions are derived by truncating the registered
// CONF_*_DESCRIPTION strings to their first paragraph — every
// description in descriptions.go starts with a single-line summary
// that the help response can reuse directly.
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
