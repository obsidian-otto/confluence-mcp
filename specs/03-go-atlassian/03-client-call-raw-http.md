# 03.3 — Raw HTTP via `Client.Call()` (covering Confluence REST v2)

## Overview

Since `confluence/v2/` has no typed services, every call to
a v2 REST endpoint (`/wiki/api/v2/...`) goes through
`Client.Call()`, which is a generic HTTP wrapper around
`client.HTTP`. This file documents the `Client.Call()` API
and the patterns the Go MCP server uses for the five CRUD
tools.

## Sources

- Source: `confluence/api_client_impl.go` — the `NewRequest`,
  `Call`, and `Transform` helpers on the `Client` type.
- Source: `go-atlassian/README.md` lines 363-419 — "Call a
  RAW API Endpoint" example.
- Confluence v2 docs:
  https://developer.atlassian.com/cloud/confluence/rest/v2/intro/

## Spec

### `Client.NewRequest`

Signature (from the README):

```go
request, err := atlassian.NewRequest(
    context.Background(),
    http.MethodGet,
    apiEndpoint,   // e.g. "rest/api/3/issue/createmeta/KP/issuetypes"
    "",
    nil,           // payload (struct, map, or nil)
)
```

The `apiEndpoint` is **relative to the site base URL** (the
second arg to `confluence.New(...)`). For example, with
`site = "your-company.atlassian.net"` and `apiEndpoint =
"rest/api/3/issue/KP-2"`, the full URL is
`https://your-company.atlassian.net/rest/api/3/issue/KP-2`.

For Confluence v2, `apiEndpoint` would be
`"wiki/api/v2/spaces"`, `"wiki/api/v2/pages"`, etc.

### `Client.Call`

Signature:

```go
response, err := atlassian.Call(request, &resultStruct)
// response is *model.ResponseScheme; resultStruct is *YourStruct
```

`response` carries:

- `response.Status` — HTTP status code (string)
- `response.StatusCode` — HTTP status code (int)
- `response.Bytes` — raw response bytes
- `response.Method` / `response.Endpoint` /
  `response.Headers`

### Putting it together — `conf_get` for spaces

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "io"
    "log"

    "github.com/ctreminiom/go-atlassian/v2/confluence"
)

func listSpaces(site, email, token string) ([]map[string]any, error) {
    c, err := confluence.New(nil, site)
    if err != nil { return nil, err }
    c.Auth.SetBasicAuth(email, token)

    req, err := c.NewRequest(context.Background(), "GET", "wiki/api/v2/spaces", "", nil)
    if err != nil { return nil, err }

    // Use raw HTTP because we want the response map for JMESPath/TOON
    resp, err := c.HTTP.Do(req.Request)
    if err != nil { return nil, err }
    defer resp.Body.Close()

    body, err := io.ReadAll(resp.Body)
    if err != nil { return nil, err }

    if resp.StatusCode >= 400 {
        return nil, fmt.Errorf("GET /wiki/api/v2/spaces: %d %s", resp.StatusCode, body)
    }

    var result struct {
        Results []map[string]any `json:"results"`
    }
    if err := json.Unmarshal(body, &result); err != nil {
        return nil, err
    }
    return result.Results, nil
}
```

**Pattern summary:**

1. `c.NewRequest(ctx, method, apiEndpoint, "", payload)` to
   build the request (with `c.Auth.SetBasicAuth` already
   set).
2. `c.HTTP.Do(req.Request)` to send it.
3. `io.ReadAll(resp.Body)` to read raw bytes (for JMESPath
   + TOON processing).
4. On 4xx/5xx, build an error message of the form
   `"<METHOD> <apiEndpoint>: <status> <statusText> - <body>"`.

### Why `c.HTTP.Do` and not `c.Call`?

`c.Call` expects a typed destination struct and returns a
`*model.ResponseScheme`. The Go MCP server needs the **raw
response body** for two reasons:

1. **JMESPath filtering.** JMESPath works on the parsed JSON
   tree; we need to preserve the exact JSON to apply JMESPath
   expressions correctly.
2. **TOON encoding.** TOON encoding happens *after* any
   JMESPath filter; both stages need the full raw JSON.

If we used `c.Call`, we'd have to JSON-marshal the typed
struct back to JSON to apply JMESPath — a lossy round-trip
(e.g. optional fields, omitempty, nil vs zero).

### The 5-tool mapping to `Client.Call()`

| MCP tool | Method | Body | `c.HTTP.Do` URL shape |
| -------- | ------ | ---- | --------------------- |
| `conf_get` | GET | none | `wiki/api/v2/{path}?...` |
| `conf_post` | POST | JSON | `wiki/api/v2/{path}?...` (body via `c.NewRequest` payload) |
| `conf_put` | PUT | JSON | `wiki/api/v2/{path}?...` |
| `conf_patch` | PATCH | JSON | `wiki/api/v2/{path}?...` |
| `conf_delete` | DELETE | none | `wiki/api/v2/{path}?...` |

The `path` argument from the LLM starts with `/` (e.g.
`/wiki/api/v2/spaces/123`). The Go MCP server strips the
leading `/` before passing to `c.NewRequest` so the relative
URL `wiki/api/v2/spaces/123` is correct (the `wiki/` prefix
is included in the path the LLM passes).

### Body handling for POST/PUT/PATCH

The LLM passes the body as `map[string]any`. The Go MCP
server:

1. Marshals the map to JSON.
2. Sets it as the request body via `c.NewRequest(ctx, method,
   endpoint, "", payload)` — the `payload` argument can be a
   `map[string]any` which the library JSON-encodes.

If `c.NewRequest` doesn't accept `map[string]any` directly,
the fallback is to use `c.NewRequest` with a typed struct
built from the map (the library's `payload any` parameter
accepts both).

## Verification

A reader of this spec should be able to:

1. Write a 20-line Go program that calls `listSpaces(...)`
   from the example above against a real Atlassian site and
   sees a JSON array of space objects.
2. Confirm `c.HTTP.Do(req.Request)` is the actual HTTP-call
   primitive (the `req.Request` field is `*http.Request`).
3. Confirm that for a 404 response, the program produces an
   error of the form `"GET /wiki/api/v2/spaces/999: 404 Not
   Found - {...}"`.