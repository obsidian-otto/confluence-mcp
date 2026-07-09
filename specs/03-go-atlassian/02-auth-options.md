# 03.2 — Authentication Options

## Overview

`ctreminiom/go-atlassian/v2/confluence` provides a rich
`Client` option set for auth. At v1 the Go MCP server uses
**only basic auth via API token**, matching the upstream. This
file documents the option set so the implementer knows what's
available for the OAuth 3LO future (gap **Q2**) and which
options are load-bearing for v1.

## Sources

- Source: `confluence/api_client_impl.go` (303 lines; the
  `ClientOption` type and its implementations).
- README OAuth section: lines 100-200 of the upstream
  `go-atlassian` README (the multi-service OAuth example).
- Atlassian basic-auth docs:
  https://developer.atlassian.com/cloud/confluence/basic-auth-for-rest-apis/

## Spec

### v1 path: Basic Auth (API token)

The minimal client for v1:

```go
import (
    "github.com/ctreminiom/go-atlassian/v2/confluence"
)

client, err := confluence.New(nil, "your-company.atlassian.net")
if err != nil { /* fatal */ }
client.Auth.SetBasicAuth("you@example.com", "ATATT3xFfGF0...")

// ... then use client.HTTP, client.Call, or typed services
```

`confluence.New(httpClient, site, ...)` takes:

| Param | v1 use | Notes |
| ----- | ------ | ----- |
| `httpClient` | `nil` (defaults to `http.DefaultClient`) | Could be customized for timeout / proxy |
| `site` | `"<site>.atlassian.net"` | The full hostname, not the prefix |

**Quirk:** the `go-atlassian` README's basic-auth example
uses `"INSTANCE_HOST"` as the second argument and does not
say what to put there. The Confluence Cloud convention is
the **full hostname** (e.g. `your-company.atlassian.net`),
not just the prefix `your-company`. The Go MCP server
concatenates `${ATLASSIAN_SITE_NAME}.atlassian.net`
internally.

### The `ClientOption` set (for future OAuth work)

`confluence.New` accepts variadic `ClientOption` functions:

```go
func WithOAuth(config *common.OAuth2Config) ClientOption
func WithAutoRenewalToken(token *common.OAuth2Token) ClientOption
func WithOAuthWithAutoRenewal(config *common.OAuth2Config, token *common.OAuth2Token) ClientOption
func WithTokenStore(store oauth2.TokenStore) ClientOption
func WithTokenCallback(callback oauth2.TokenCallback) ClientOption
```

For v1, **none** of these are used. They are documented here
so the implementer can extend v2 cleanly without re-reading
the `go-atlassian` source.

### Auth API surface

The `client.Auth` field is a `common.Authentication`
interface:

```go
type Authentication interface {
    SetBasicAuth(email, token string)   // for API token
    SetBearerToken(token string)        // for PAT / OAuth bearer
    // ... others for OAuth callbacks
}
```

For v1, only `SetBasicAuth` is called. The token string is
the **API token** issued by
https://id.atlassian.com/manage-profile/security/api-tokens,
not a Cloud PAT (those are different — PATs are issued from
the admin console and use `SetBearerToken`).

### What the Go MCP server does NOT touch

- `client.OAuth` — nil at v1.
- `client.HTTP` — uses `http.DefaultClient`. No custom
  transport in v1. Surfaced as gap **Q11** (should we set a
  longer timeout / a proxy / a retry policy on
  `http.DefaultClient`?).
- `client.Auth.SetBearerToken` — only used for PAT, not API
  token. v1 uses `SetBasicAuth`.

## Verification

A reader of this spec should be able to:

1. Write a 5-line Go program that calls
   `confluence.New(nil, "your-company.atlassian.net")` +
   `client.Auth.SetBasicAuth(...)` + `client.HTTP.Get(...)`
   against `/wiki/api/v2/spaces` and see a 200 response.
2. Confirm that the test program's stderr output contains
   **no** token echo (the go-atlassian library does not log
   credentials; the Go MCP server must follow suit).
3. Confirm that the Atlassian API token is the same opaque
   string issued by the API token page (vs a Cloud PAT).