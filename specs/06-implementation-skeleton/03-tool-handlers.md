# 06.3 — Tool Handlers Skeleton (`internal/tools/`)

## Overview

This file documents the skeleton of each tool handler file
in `internal/tools/`. The handlers are thin wrappers around
a single `executeRequest` helper that does the actual work.

## Sources

- Upstream: `src/tools/atlassian.api.tool.ts` (the
  `createReadHandler` and `createWriteHandler` patterns).
- `05-tool-surface-design/01-tool-mapping.md` (the tool
  mapping table).

## Spec

### `args.go` — argument types

```go
package tools

type ConfGetArgs struct {
    Path         string            `json:"path" jsonschema:"required,description=The API endpoint path (e.g. /wiki/api/v2/spaces). Must start with /"`
    QueryParams  map[string]string `json:"queryParams,omitempty" jsonschema:"description=Query parameters as key-value pairs"`
    JQ           string            `json:"jq,omitempty" jsonschema:"description=JMESPath expression to filter/transform the response. ALWAYS use to reduce token cost."`
    OutputFormat string            `json:"outputFormat,omitempty" jsonschema:"description=Output format: 'toon' (default) or 'json',enum=toon,enum=json"`
}

type ConfPostArgs struct {
    Path         string            `json:"path" jsonschema:"required,description=API endpoint path"`
    QueryParams  map[string]string `json:"queryParams,omitempty" jsonschema:"description=Query parameters"`
    Body         map[string]any    `json:"body" jsonschema:"required,description=Request body as JSON object"`
    JQ           string            `json:"jq,omitempty" jsonschema:"description=JMESPath filter"`
    OutputFormat string            `json:"outputFormat,omitempty" jsonschema:"description=Output format: 'toon' or 'json',enum=toon,enum=json"`
}

// ConfPutArgs, ConfPatchArgs — same shape as ConfPostArgs
// ConfDeleteArgs — same shape as ConfGetArgs

type baseArgs struct {
    Path         string
    QueryParams  map[string]string
    JQ           string
    OutputFormat string
}

// asBase extracts the shared base fields from any Conf*Args.
func (a ConfGetArgs) asBase() baseArgs    { return baseArgs{a.Path, a.QueryParams, a.JQ, a.OutputFormat} }
func (a ConfPostArgs) asBase() baseArgs   { return baseArgs{a.Path, a.QueryParams, a.JQ, a.OutputFormat} }
// ... etc
```

### `descriptions.go` — tool descriptions

```go
package tools

const CONF_GET_DESCRIPTION = `Read any Confluence data. Returns TOON format by default
... (full upstream string from src/tools/atlassian.api.tool.ts line 127) ...
API reference: https://developer.atlassian.com/cloud/confluence/rest/v2/`

const CONF_POST_DESCRIPTION = `Create Confluence resources. Returns TOON format by default ...`
const CONF_PUT_DESCRIPTION = `Replace Confluence resources (full update) ...`
const CONF_PATCH_DESCRIPTION = `Partially update Confluence resources ...`
const CONF_DELETE_DESCRIPTION = `Delete Confluence resources ...`
```

These strings are copied **verbatim** from the upstream. Any
drift from the upstream's wording is a bug.

### `safe_handler.go` — panic recovery

```go
package tools

import (
    "fmt"
    "log"

    "github.com/metoro-io/mcp-golang"
)

// safeHandler wraps a tool handler with panic recovery so
// that any panic in the handler returns a clean error
// response instead of crashing the server.
func safeHandler[T any](
    inner func(T) (*mcp_golang.ToolResponse, error),
) func(T) (*mcp_golang.ToolResponse, error) {
    return func(args T) (resp *mcp_golang.ToolResponse, err error) {
        defer func() {
            if r := recover(); r != nil {
                log.Printf("PANIC in handler: %v\n%v", r, debug.Stack())
                resp = mcp_golang.NewToolResponse(
                    mcp_golang.NewTextContent(fmt.Sprintf(
                        "internal server error: %v", r)),
                )
                err = nil
            }
        }()
        return inner(args)
    }
}
```

### `execute.go` — the shared helper

See `05-tool-surface-design/01-tool-mapping.md` for the full
`executeRequest` skeleton. Key responsibilities:

1. Build the URL from path + queryParams.
2. Call `atlassianClient.Do(ctx, method, url, body)`.
3. Handle 4xx/5xx → error response.
4. Handle 204 → empty success.
5. Unmarshal JSON for JMESPath.
6. Apply JMESPath if `args.JQ` set.
7. Encode (TOON or JSON).
8. Truncate if >40k chars; write raw response to
   `/tmp/mcp/`.
9. Return text content.

### `handlers.go` — the five public handlers

```go
package tools

import (
    "context"
    "github.com/metoro-io/mcp-golang"
)

func HandleGet(ctx context.Context, client *atlassian.Client,
    args ConfGetArgs) (*mcp_golang.ToolResponse, error) {
    return executeRequest(ctx, client, "GET", args.asBase(), nil)
}

func HandlePost(ctx context.Context, client *atlassian.Client,
    args ConfPostArgs) (*mcp_golang.ToolResponse, error) {
    return executeRequest(ctx, client, "POST", args.asBase(), args.Body)
}

func HandlePut(ctx context.Context, client *atlassian.Client,
    args ConfPutArgs) (*mcp_golang.ToolResponse, error) {
    return executeRequest(ctx, client, "PUT", args.asBase(), args.Body)
}

func HandlePatch(ctx context.Context, client *atlassian.Client,
    args ConfPatchArgs) (*mcp_golang.ToolResponse, error) {
    return executeRequest(ctx, client, "PATCH", args.asBase(), args.Body)
}

func HandleDelete(ctx context.Context, client *atlassian.Client,
    args ConfDeleteArgs) (*mcp_golang.ToolResponse, error) {
    return executeRequest(ctx, client, "DELETE", args.asBase(), nil)
}
```

### `register.go` — wiring

```go
package tools

import (
    "fmt"
    "github.com/metoro-io/mcp-golang"
)

// RegisterAll registers the five Confluence CRUD tools with the
// given server. Returns the first registration error, if any.
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

## Verification

A reader of this spec should be able to:

1. Confirm each handler's argument type matches
   `02-upstream-aashari/02-five-tools.md`.
2. Confirm the `safeHandler` wrapper is applied to every
   `RegisterTool` call.
3. Run `go test ./internal/tools/...` and confirm tests for
   each handler pass:
   - `TestHandleGet_Success` — mock atlassian client
     returns 200 + JSON; verify TOON-encoded text content.
   - `TestHandleGet_404` — mock returns 404; verify error
     response with status code.
   - `TestHandleGet_JQFilter` — mock returns JSON; verify
     JMESPath filter applied before TOON encoding.
   - `TestHandleGet_Truncation` — mock returns >40k
     response; verify truncation notice +
     `/tmp/mcp/` raw-response path.