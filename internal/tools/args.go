// Package tools provides the five MCP tool argument types and
// (in later phases) their handlers. The argument shapes mirror
// the upstream `src/tools/atlassian.api.types.ts` zod schemas
// `GetApiToolArgs`, `RequestWithBodyArgs`, and `DeleteApiToolArgs`.
//
// Phase 5 ships only the types and description constants. Handlers
// are added in Phase 7 (see IMPLEMENTATION_PLAN.md).
package tools

// GetArgs is the argument set for the `conf_get` tool (HTTP GET).
// Mirrors upstream `GetApiToolArgs` (no body).
type GetArgs struct {
	Path         string            `json:"path"`
	Query        map[string]string `json:"query,omitempty"`
	JQ           string            `json:"jq,omitempty"`
	OutputFormat string            `json:"outputFormat,omitempty"`
}

// PostArgs is the argument set for the `conf_post` tool (HTTP POST).
// Mirrors upstream `RequestWithBodyArgs` / `PostApiToolArgs`. The
// request body is a JSON object.
type PostArgs struct {
	Path         string            `json:"path"`
	Query        map[string]string `json:"query,omitempty"`
	Body         map[string]any    `json:"body,omitempty"`
	JQ           string            `json:"jq,omitempty"`
	OutputFormat string            `json:"outputFormat,omitempty"`
}

// PutArgs is the argument set for the `conf_put` tool (HTTP PUT —
// full replacement). Mirrors upstream `PutApiToolArgs`.
type PutArgs struct {
	Path         string            `json:"path"`
	Query        map[string]string `json:"query,omitempty"`
	Body         map[string]any    `json:"body,omitempty"`
	JQ           string            `json:"jq,omitempty"`
	OutputFormat string            `json:"outputFormat,omitempty"`
}

// PatchArgs is the argument set for the `conf_patch` tool
// (HTTP PATCH — partial update). The upstream API accepts the
// patch operations as a JSON array (RFC 6902-style), so Body is
// a slice of objects rather than a single object.
type PatchArgs struct {
	Path         string            `json:"path"`
	Query        map[string]string `json:"query,omitempty"`
	Body         []map[string]any  `json:"body,omitempty"`
	JQ           string            `json:"jq,omitempty"`
	OutputFormat string            `json:"outputFormat,omitempty"`
}

// DeleteArgs is the argument set for the `conf_delete` tool
// (HTTP DELETE — no body). Mirrors upstream `DeleteApiToolArgs`
// which is identical in shape to `GetApiToolArgs`.
type DeleteArgs struct {
	Path         string            `json:"path"`
	Query        map[string]string `json:"query,omitempty"`
	JQ           string            `json:"jq,omitempty"`
	OutputFormat string            `json:"outputFormat,omitempty"`
}
