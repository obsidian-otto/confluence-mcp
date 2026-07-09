# 05.1 — Tool Mapping (Go Function → MCP Tool)

## Overview

This file pins the **exact** mapping from each MCP tool name
(`conf_get`, `conf_post`, `conf_put`, `conf_patch`,
`conf_delete`) to the Go function that implements it, the
HTTP method, and the body semantics. The mapping is
deliberately **1:1 with the upstream** so existing
upstream-trained prompts and workflows port over
byte-for-byte.

## Sources

- Upstream source: `src/tools/atlassian.api.tool.ts` (the
  `CONF_*_DESCRIPTION` constants and the `registerTools`
  function).
- Sibling spec: `02-upstream-aashari/02-five-tools.md`
  (full per-tool input/output shape).
- `04-mcp-golang-framework/01-server-api.md` (the
  `RegisterTool` API).

## Spec

### The mapping

| MCP tool | Go handler | HTTP method | Body source | Output |
| -------- | ---------- | ----------- | ----------- | ------ |
| `conf_get` | `internal/tools.handleGet(args ConfGetArgs)` | GET | none | TOON/JSON of response |
| `conf_post` | `internal/tools.handlePost(args ConfPostArgs)` | POST | `args.Body` (map) | TOON/JSON of response |
| `conf_put` | `internal/tools.handlePut(args ConfPutArgs)` | PUT | `args.Body` (map) | TOON/JSON of response |
| `conf_patch` | `internal/tools.handlePatch(args ConfPatchArgs)` | PATCH | `args.Body` (map) | TOON/JSON of response |
| `conf_delete` | `internal/tools.handleDelete(args ConfDeleteArgs)` | DELETE | none | TOON/JSON of response (or empty for 204) |

The `ConfPostArgs`, `ConfPutArgs`, `ConfPatchArgs` types
are identical (same shape — body + path + queryParams + jq +
outputFormat). They could be a single `ConfWriteArgs` type
but are kept as separate types to mirror the upstream's
distinction (and to give each tool its own `jsonschema`).
**Surfaced as gap Q13**: should we use one shared type or
five?

### Shared handler logic

All five handlers go through a single private helper:

```go
func executeRequest(
    ctx context.Context,
    method string,
    args baseArgs,  // path, queryParams, jq, outputFormat
    body map[string]any,
) (*mcp_golang.ToolResponse, error) {
    // 1. Normalize path (strip leading slash for go-atlassian URL convention)
    path := strings.TrimPrefix(args.Path, "/")

    // 2. Build URL with query params
    u := buildURL(path, args.QueryParams)

    // 3. Make the HTTP call via internal/atlassian
    rawBytes, status, statusText, err := atlassianClient.Do(ctx, method, u, body)
    if err != nil {
        return errorResponse(method, args.Path, 0, "network error", []byte(err.Error())), nil
    }

    // 4. If 4xx/5xx, return error response
    if status >= 400 {
        return errorResponse(method, args.Path, status, statusText, rawBytes), nil
    }

    // 5. If 204 No Content, return empty success
    if status == 204 {
        return mcp_golang.NewToolResponse(
            mcp_golang.NewTextContent("(204 No Content)")),
        nil
    }

    // 6. Parse JSON for JMESPath
    var data any
    if err := json.Unmarshal(rawBytes, &data); err != nil {
        return errorResponse(method, args.Path, status, "invalid JSON response", rawBytes), nil
    }

    // 7. Apply JMESPath if requested
    if args.JQ != "" {
        data, err = jmespath.Search(args.JQ, data)
        if err != nil {
            return errorResponse(method, args.Path, status, "jq filter error: " + err.Error(), nil), nil
        }
    }

    // 8. Encode (TOON default, JSON if outputFormat=json)
    encoded, err := encodeOutput(data, args.OutputFormat)
    if err != nil {
        return errorResponse(method, args.Path, status, "encode error: " + err.Error(), nil), nil
    }

    // 9. Truncate if >40k chars
    finalText := truncateForAI(encoded, rawResponsePath())

    return mcp_golang.NewToolResponse(mcp_golang.NewTextContent(finalText)), nil
}
```

The five public handlers are thin wrappers:

```go
func handleGet(args ConfGetArgs) (*mcp_golang.ToolResponse, error) {
    return executeRequest(ctx, "GET", args.base, nil)
}

func handlePost(args ConfPostArgs) (*mcp_golang.ToolResponse, error) {
    return executeRequest(ctx, "POST", args.base, args.Body)
}
// ... etc
```

### Tool registration

```go
// internal/tools/register.go
func RegisterAll(server *mcp_golang.Server) error {
    type reg struct {
        name string
        desc string
        fn   any
    }
    // The actual handlers are bound to a *atlassian.Client and
    // context. We resolve them at register time using a closure
    // factory. See internal/server/server.go for how the client
    // is plumbed in.
    ...
}
```

The actual `RegisterAll` body plumbs the `*atlassian.Client`
into each handler via a closure factory. See
`internal/server/server.go` for the wiring.

### Tool description constants

The five `CONF_*_DESCRIPTION` constants are stored as
exported package-level strings in
`internal/tools/descriptions.go`, copied verbatim from the
upstream's `src/tools/atlassian.api.tool.ts` lines 127-223
(see `02-upstream-aashari/02-five-tools.md` for the full
text).

## Verification

A reader of this spec should be able to:

1. Confirm the five tool names match the upstream's README.
2. Confirm the per-tool input shapes match
   `02-upstream-aashari/02-five-tools.md`.
3. Run `hermes mcp test confluence` and see five tools
   listed with names `mcp_confluence_conf_get`,
   `mcp_confluence_conf_post`, `mcp_confluence_conf_put`,
   `mcp_confluence_conf_patch`,
   `mcp_confluence_conf_delete`.
4. Call `mcp_confluence_conf_get` with
   `path: "/wiki/api/v2/spaces"` and confirm the response
   is TOON-encoded.