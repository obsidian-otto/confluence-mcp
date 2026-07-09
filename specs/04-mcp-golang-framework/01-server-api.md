# 04.1 — `mcp-golang` Server API

## Overview

`github.com/metoro-io/mcp-golang` (MIT licensed; current
`go.mod` requires Go 1.21) provides the **MCP server runtime**
for Go. The Go MCP server uses the **stdio transport** for v1;
the `mcp-golang` server API is small enough to document in one
page. This file documents the constructor, options, and tool
registration API so the implementer knows what to call.

## Sources

- Repository: https://github.com/metoro-io/mcp-golang
- README: https://github.com/metoro-io/mcp-golang/blob/main/README.md
  (245 lines).
- Server source: `server.go` (1045 lines).
- Tool response types: `tool_response_types.go`, `tool_api.go`.
- License: MIT (verified via raw LICENSE fetch).
- `go.mod`: `module github.com/metoro-io/mcp-golang`, Go 1.21+.

## Spec

### Constructor

```go
import (
    "github.com/metoro-io/mcp-golang"
    "github.com/metoro-io/mcp-golang/transport/stdio"
)

server := mcp_golang.NewServer(stdio.NewStdioServerTransport())
```

`NewServer` takes:

| Param | Type | Required | Notes |
| ----- | ---- | -------- | ----- |
| `transport` | `transport.Transport` | yes | An interface; we use `stdio.NewStdioServerTransport()` |
| `options` | `...ServerOption` | no | Functional options (see below) |

### Options

From `server.go`:

```go
func WithProtocol(protocol *protocol.Protocol) ServerOptions
func WithPaginationLimit(limit int) ServerOptions
func WithName(name string) ServerOptions
func WithVersion(version string) ServerOptions
func WithInstructions(instructions string) ServerOptions
```

The Go MCP server uses:

```go
server := mcp_golang.NewServer(
    stdio.NewStdioServerTransport(),
    mcp_golang.WithName("mcp-confluence"),
    mcp_golang.WithVersion("1.0.0"),
    // WithInstructions is optional; not used at v1
)
```

The `name` and `version` are surfaced in the MCP `initialize`
response and visible to the LLM. Convention:
`"mcp-confluence"` + semver.

### Tool registration

```go
err := server.RegisterTool(
    "conf_get",                                     // tool name
    "Read any Confluence data. Returns TOON...",   // tool description (LLM-visible)
    func(arguments ConfGetArgs) (*mcp_golang.ToolResponse, error) {
        // handler body
        return mcp_golang.NewToolResponse(
            mcp_golang.NewTextContent("...response text..."),
        ), nil
    },
)
if err != nil { panic(err) }
```

`RegisterTool` takes:

| Param | Type | Notes |
| ----- | ---- | ----- |
| `name` | `string` | The tool's name (e.g. `"conf_get"`). Becomes `mcp_confluence_conf_get` after Hermes prefixes. |
| `description` | `string` | The tool's description. Visible to the LLM at `tools/list` time. **Important**: this is where the upstream's `CONF_*_DESCRIPTION` strings go. |
| `handler` | `any` | A function with signature `func(T) (*ToolResponse, error)` where `T` is a struct with `jsonschema` tags. The library introspects the parameter type and generates the JSON schema automatically. |

The library panics if `RegisterTool` fails (e.g. duplicate
name). The Go MCP server wraps the registration in a helper
that collects errors and exits cleanly on failure.

### Tool response shape

From `tool_response_types.go`:

```go
type ToolResponse struct {
    Content []*Content
}

type Content struct {
    Type string  // "text"
    Text string  // the text content
}

func NewToolResponse(content ...*Content) *ToolResponse
func NewTextContent(text string) *Content
```

There are also image / audio / resource content constructors
(`NewImageContent`, `NewAudioContent`,
`NewEmbeddedResource`) but the Go MCP server uses **only
text content** at v1 (the upstream is text-only).

### Error responses

The library supports error responses via `IsError` on a
wrapper struct. The upstream pattern (from
`src/utils/error-handler.util.ts`) returns an `isError: true`
content block. The Go MCP server does the same:

```go
return &mcp_golang.ToolResponse{
    Content: []*mcp_golang.Content{{
        Type: "text",
        Text: fmt.Sprintf("GET %s: %d %s - %s", path, status, statusText, body),
    }},
}, nil  // note: nil error — the error is in the content
```

**Note:** `IsError` is a wrapper-level flag, not a
content-level flag in some MCP server libs. The mcp-golang
library uses the content's text + the handler returning
`(response, nil)` pattern to surface errors. We document the
exact pattern in
`09-anti-patterns/03-error-shapes.md`.

### Handler signature detail

The handler's argument type is introspected:

```go
type ConfGetArgs struct {
    Path         string            `json:"path" jsonschema:"required,description=..."`
    QueryParams  map[string]string `json:"queryParams,omitempty" jsonschema:"description=..."`
    JQ           string            `json:"jq,omitempty" jsonschema:"description=..."`
    OutputFormat string            `json:"outputFormat,omitempty" jsonschema:"description=...,enum=toon,enum=json"`
}

func handler(args ConfGetArgs) (*mcp_golang.ToolResponse, error) { ... }
```

The library reads the struct's `jsonschema` tags and builds
the input schema automatically. Required fields must be
tagged `required`; optional fields are omitted from
`required`. Enum constraints use `enum=val1,enum=val2`.

### Serving

After all tools are registered:

```go
if err := server.Serve(); err != nil {
    log.Fatalf("server.Serve: %v", err)  // stderr is safe
}
```

`Serve()` blocks until the transport closes (SIGINT/SIGTERM,
or the parent process closes stdin). It does not return on
success.

### Cleanup

`server.Serve()` is the long-running call. Before `Serve`,
the Go MCP server does:

```go
// Graceful shutdown on SIGINT / SIGTERM
ctx, cancel := signal.NotifyContext(context.Background(),
    syscall.SIGINT, syscall.SIGTERM)
defer cancel()

go func() {
    <-ctx.Done()
    log.Println("shutting down...")
    // stdio transport has no explicit Close() needed; Serve() returns
    // when stdin closes, which happens when the parent process exits
}()
```

In practice, the stdio transport doesn't have an explicit
close — `Serve()` returns when stdin EOFs (Hermes disconnects)
or when the process is signaled.

## Verification

A reader of this spec should be able to:

1. Write a 30-line Go program using the
   `readme_server.go` example pattern with one tool `hello`
   that returns `"Hello, world!"`.
2. Run `hermes mcp test confluence` (with the binary
   registered) and see `mcp_confluence_hello` in the tool
   list.
3. Call `mcp_confluence_hello` from a Hermes chat and see
   `"Hello, world!"` returned.