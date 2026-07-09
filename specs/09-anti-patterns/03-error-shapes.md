# 09.3 — Error Shapes (what `isError` content looks like)

## Overview

When a tool handler encounters an error (4xx/5xx response from
Atlassian, network failure, JMESPath parse error, etc.), it
returns a `*ToolResponse` with **error text in the content**
and `nil` for the error return. The LLM sees the error text
and can react. This file documents the **exact error message
shape** for each error class and the patterns for surfacing
errors to the LLM.

## Sources

- Upstream
  `src/controllers/atlassian.api.controller.ts` (the
  `handleControllerError` function's output format).
- MCP spec:
  https://modelcontextprotocol.io/specification/2025-06-18/server/tools/
  (the `isError` flag on tool responses).

## Spec

### The error response shape

Every error returns:

```go
return mcp_golang.NewToolResponse(
    mcp_golang.NewTextContent("<error-message>"),
), nil
```

The library does not have an `isError: true` flag (unlike
some other MCP server libraries); the error is in the
content text. The LLM treats text content as informational
and will react accordingly.

### Error message shapes by class

#### Class 1: Atlassian API 4xx/5xx

```
<METHOD> <path>: <statusCode> <statusText> - <response-body>
```

Example (page not found):

```
GET /wiki/api/v2/pages/999: 404 Not Found - {"code":"NOT_FOUND","message":"Page not found"}
```

Example (version conflict):

```
PUT /wiki/api/v2/pages/1234567: 409 Conflict - {"code":"VERSION_MISMATCH","message":"Current version is 3, not 2"}
```

Example (auth failure):

```
GET /wiki/api/v2/spaces: 401 Unauthorized - {"code":"AUTHENTICATION_FAILED","message":"Authentication failed"}
```

#### Class 2: Network / DNS / TLS

```
<METHOD> <path>: network error: <error-type>: <error-message>
```

Example:

```
GET /wiki/api/v2/spaces: network error: Get "https://fake.atlassian.net/wiki/api/v2/spaces": dial tcp: lookup fake.atlassian.net: no such host
```

#### Class 3: JSON unmarshal (server returned non-JSON)

```
<METHOD> <path>: <statusCode> <statusText>: invalid JSON response: <first-200-chars-of-body>
```

Example:

```
GET /wiki/api/v2/spaces: 502 Bad Gateway: invalid JSON response: <html><body><h1>502 Bad Gateway</h1></body></html>
```

#### Class 4: JMESPath parse / execution

```
<METHOD> <path>: jq filter error: <jmespath-error>
```

Example:

```
GET /wiki/api/v2/spaces: jq filter error: invalid jmespath expression: Unexpected token at position 12: results[*].id_extra
```

#### Class 5: TOON encode failure (rare; only on deeply nested or cyclic data)

```
<METHOD> <path>: encode error: <toon-error>
```

#### Class 6: Handler panic (caught by `safeHandler`)

```
internal server error: <panic-value>
```

Panic stack traces go to **stderr** (via `log.Printf`), not
into the MCP response. The LLM sees a short message; the
operator sees the stack trace in the captured stderr.

### Helper: the error formatter

```go
// internal/atlassian/errors.go
func FormatAPIError(method, path string, status int, statusText string, body []byte) string {
    bodyStr := string(body)
    // Truncate body if huge
    if len(bodyStr) > 2000 {
        bodyStr = bodyStr[:2000] + "... (truncated)"
    }
    return fmt.Sprintf("%s %s: %d %s - %s", method, path, status, statusText, bodyStr)
}

func FormatNetworkError(method, path string, err error) string {
    return fmt.Sprintf("%s %s: network error: %v", method, path, err)
}
```

### Why this shape?

| Choice | Reason |
| ------ | ------ |
| `<METHOD> <path>:` prefix | The LLM can identify which call failed without re-reading the conversation |
| `<statusCode> <statusText>:` body | Mirrors HTTP semantics; the LLM can pattern-match (e.g. "all 401s → re-auth") |
| Include the response body verbatim | The Confluence API error body has `code` and `message` fields the LLM needs |
| Truncate body at 2000 chars | Prevents huge error responses from filling the LLM context |
| No `isError: true` flag | The mcp-golang library doesn't have one; text content is the convention |

### What we deliberately don't surface

| Content | Why not |
| ------- | ------- |
| Stack traces | Too verbose; the LLM can't act on them |
| Internal error codes | The user doesn't have a support contract with us |
| Token values (sanitized) | Tokens are redacted upstream (Hermes' `mcp_security.py` strips `token=` patterns too) |
| Sensitive request/response bodies | For write ops, the body may contain user content the LLM doesn't need to see |

## Verification

A reader of this spec should be able to:

1. Run `go test ./internal/atlassian/... -run TestFormatAPIError`
   and see all error-class tests pass.
2. Trigger a real 404 (against a test instance) and confirm
   the tool response matches the Class 1 shape.
3. Trigger a network failure (by setting `ATLASSIAN_SITE_NAME`
   to a bogus hostname) and confirm the Class 2 shape.
4. Pass a syntactically invalid `jq` expression and confirm
   the Class 4 shape.