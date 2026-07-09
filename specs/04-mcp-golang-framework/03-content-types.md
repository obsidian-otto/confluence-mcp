# 04.3 — Content Types and Response Helpers

## Overview

`mcp-golang`'s `ToolResponse` carries a slice of `*Content`
values. At v1 the Go MCP server produces **only text
content**; image / audio / embedded-resource content are not
used (matches the upstream). This file documents the response
helper functions and the exact patterns for success vs error
responses.

## Sources

- Source: `tool_response_types.go` (`ToolResponse`, `Content`,
  `NewToolResponse`, `NewTextContent`).
- Source: `server.go` (`createWrappedToolHandler` — the
  library's internal handler that wraps user-supplied
  handlers).
- MCP spec:
  https://modelcontextprotocol.io/specification/2025-06-18/server/tools/

## Spec

### Success response shape

```go
return mcp_golang.NewToolResponse(
    mcp_golang.NewTextContent("<encoded-response-text>"),
), nil
```

Always exactly **one** `*Content` element of `Type: "text"`.
The `Text` field is the encoded response (TOON by default,
JSON if `outputFormat: "json"`).

### Error response shape

```go
return &mcp_golang.ToolResponse{
    Content: []*mcp_golang.Content{{
        Type: "text",
        Text: fmt.Sprintf("%s %s: %d %s - %s",
            method, path, statusCode, statusText, body),
    }},
}, nil
```

Same shape (one Text content) but with an **error message**
as the text. The handler returns `nil` for the error so the
library treats it as a successful MCP call; the LLM sees
the error text in the content.

**Rationale:** the `mcp-golang` library's handler wrapper
does not have a first-class `IsError` flag — errors are
surfaced through the content text. This matches the
upstream's behavior (`formatErrorForMcpTool` returns a text
content with the error message).

### The full handler pattern

```go
func handleConfluenceGet(args ConfGetArgs) (*mcp_golang.ToolResponse, error) {
    // 1. Validate args
    if !strings.HasPrefix(args.Path, "/") {
        return mcp_golang.NewToolResponse(
            mcp_golang.NewTextContent(fmt.Sprintf(
                "ERROR: path %q must start with /", args.Path)),
        ), nil
    }
    // 2. Apply JMESPath filter (if jq is set)
    // 3. Encode response (TOON or JSON)
    // 4. Truncate if >40k chars
    // 5. Return
}
```

### Content type catalog (for completeness)

| Helper | Content type | When used at v1 |
| ------ | ------------ | --------------- |
| `NewTextContent(text)` | `type: "text"` | **All v1 responses** |
| `NewImageContent(...)` | `type: "image"` | Not used (gap Q12) |
| `NewAudioContent(...)` | `type: "audio"` | Not used |
| `NewEmbeddedResource(...)` | `type: "resource"` | Not used |

The image / audio / resource content types would be relevant
if the Go MCP server ever needed to surface attachment
binaries or rich resources. For v1, attachment access is out
of scope (gap Q8).

### Tool description conventions

The Go MCP server copies the upstream's
`CONF_*_DESCRIPTION` strings verbatim. They are multi-line
strings with Markdown formatting (`**IMPORTANT**`, backtick-
quoted examples). The mcp-golang library's
`RegisterTool(name, description, handler)` takes the
description as a plain string and surfaces it as-is in the
tool list response.

Format note: descriptions can include newlines. They are
displayed to the LLM as a single text field but newlines are
preserved. The upstream's descriptions use `\n\n` for
paragraph breaks and backticks for code examples.

## Verification

A reader of this spec should be able to:

1. Confirm
   `mcp_golang.NewToolResponse(mcp_golang.NewTextContent("..."))`
   returns a `*ToolResponse` with `Content: []*Content{...}`.
2. Confirm the upstream's `formatErrorForMcpTool` returns a
   text content with the error message (no `isError` flag).
3. Run the Go MCP server with `DEBUG=true` and confirm no
   `fmt.Println` or `log.Println` writes to stdout.