# 03.1 — `ctreminiom/go-atlassian` Package Layout

## Overview

`github.com/ctreminiom/go-atlassian/v2` (current `go.mod`: `v2`,
requires Go 1.23) provides the **Atlassian Cloud + Server/DC
HTTP client** used by the Go MCP server. The package tree is
large (58 files under `confluence/` alone at survey time) but
the **Confluence side has a notable quirk**: the package import
path `confluence/v2` is a **stub** — only the OAuth/client-
options boilerplate lives there, with **no service
implementations**. The real Confluence services all live in
`confluence/internal/...` (a paradoxically-named subdirectory).
This file documents the package layout so the implementer
knows which import path to use for which operation.

## Sources

- Repository: https://github.com/ctreminiom/go-atlassian
- README: https://github.com/ctreminiom/go-atlassian/blob/main/README.md
- `go.mod`: `module github.com/ctreminiom/go-atlassian/v2` (Go
  1.23, pulled at survey time 2026-07-09; verified via raw
  `go.mod` fetch).
- Tree (`api.github.com/repos/ctreminiom/go-atlassian/git/trees/main?recursive=1`,
  filtered to `confluence/`): 58 files under `confluence/`.
- License: MIT (verified via raw LICENSE fetch).

## Spec

### Package tree (Confluence-relevant)

```
github.com/ctreminiom/go-atlassian/v2/
├── confluence/                          ← v1 Confluence client (FULL)
│   ├── api_client_impl.go               Client + ClientOption + New()
│   └── internal/                        Service implementations
│       ├── analytics_impl.go
│       ├── attachment_content_impl.go
│       ├── attachment_impl.go
│       ├── attachment_version_impl.go
│       ├── authentication_impl.go       Basic-auth + Bearer helpers
│       ├── children_descendants_content_impl.go
│       ├── comment_content_impl.go
│       ├── content_impl.go              ContentService (CRUD on classic content)
│       ├── custom_content_impl.go
│       ├── folder_impl.go
│       ├── label_content_impl.go
│       ├── label_impl.go
│       ├── page_impl.go                 PageService (CRUD on pages — v1)
│       ├── permission_content_impl.go
│       ├── permission_space_impl.go
│       ├── properties_content_impl.go
│       ├── restriction_content_impl.go
│       ├── restriction_operation_content_impl.go
│       ├── restriction_operation_group_content_impl.go
│       ├── restriction_operation_user_content_impl.go
│       ├── search_impl.go               SearchService (CQL)
│       ├── space_impl.go                SpaceService (v1)
│       ├── space_v2_impl.go             SpaceV2Service (v2 spaces)
│       ├── task_impl.go
│       ├── template_impl.go
│       └── version_content.go
└── confluence/v2/                       ← v2 Confluence client (STUB)
    └── api_client_impl.go               ONLY Client + OAuth options
                                          (no service subdirectories)
```

### Key finding: `confluence/v2/` is a stub

The `confluence/v2/` package contains only the client
constructor and OAuth option helpers — `New()`,
`WithOAuth(...)`, `WithAutoRenewalToken(...)`,
`WithOAuthWithAutoRenewal(...)`. There are **no** service
subdirectories, no `internal/` folder, no `page_impl.go` or
`space_v2_impl.go`. The README mentions "Confluence v2" but
the v2 API surface is not implemented as typed services.

This is **not** a bug — it is the current state of the library
(the `v2` of go-atlassian's *module path*, not the v2 of
Atlassian's REST API). The v2 Confluence REST endpoints
(`/wiki/api/v2/...`) must be called via `Client.Call()` for
raw HTTP, which is documented in
`03-go-atlassian/03-client-call-raw-http.md`.

### Client construction

The `confluence.New(httpClient, site, opts...)` constructor
returns a `*confluence.Client`. The Client has these notable
fields (from `confluence/api_client_impl.go` lines 116-303):

```go
type Client struct {
    HTTP  common.HTTPClient   // injectable; nil → http.DefaultClient
    Auth  common.Authentication  // SetBasicAuth / SetBearerToken
    OAuth *oauth2.Service     // populated by WithOAuth
    // ... and service fields attached via sub-packages
    Content *ContentService
    Page    *PageService
    Space   *SpaceService
    Search  *SearchService
    // ... etc
}
```

For v1 (API-token basic-auth), only `HTTP` and `Auth` matter:

```go
client, err := confluence.New(nil, "your-company.atlassian.net")
if err != nil { /* handle */ }
client.Auth.SetBasicAuth("you@example.com", "ATATT3xFfGF0...")
```

### Service method shape (example: PageService)

From `confluence/internal/page_impl.go`:

```go
type PageService struct {
    internal *internalPageImpl
    // ...
}

func (p *PageService) Get(ctx context.Context, pageID int,
    format string, draft bool, version int) (*model.PageScheme,
    *model.ResponseScheme, error)

func (p *PageService) Gets(ctx context.Context,
    options *model.PageOptionsScheme, cursor string,
    limit int) (*model.PageChunkScheme, *model.ResponseScheme,
    error)

func (p *PageService) Create(ctx context.Context,
    payload *model.PageCreatePayloadScheme) (*model.PageScheme,
    *model.ResponseScheme, error)

func (p *PageService) Update(ctx context.Context, pageID int,
    payload *model.PageUpdatePayloadScheme) (*model.PageScheme,
    *model.ResponseScheme, error)
// ... etc
```

Notable: v1 APIs use **`int` IDs**, not strings. Confluence v2
REST uses string IDs (`"789"`). The v1 services are wired to
v1 REST endpoints (`/wiki/rest/api/content/{id}`).

### Mapping our needs to the library

| Go MCP server need | Library API |
| ------------------ | ----------- |
| **GET /wiki/api/v2/spaces** (list v2 spaces) | `client.Call(req, &result)` with `req.URL = "/wiki/api/v2/spaces"` — see `03-go-atlassian/03-client-call-raw-http.md` |
| **GET /wiki/api/v2/pages?space-id=X** | `Client.Call()` raw |
| **GET /wiki/api/v2/pages/{id}** | `Client.Call()` raw |
| **POST /wiki/api/v2/pages** (create v2 page) | `Client.Call()` raw |
| **PUT /wiki/api/v2/pages/{id}** (update v2 page with version.number) | `Client.Call()` raw |
| **DELETE /wiki/api/v2/pages/{id}** | `Client.Call()` raw |
| **GET /wiki/rest/api/search?cql=...** | `client.Search.Search(ctx, cql, ...)` (v1 API, the library has this) |
| **Basic auth header** | `client.Auth.SetBasicAuth(email, token)` |
| **GET /wiki/api/v1/space/{key}** (legacy) | `client.Space.Get(ctx, key)` (v1 API) |

The pattern: **the Go MCP server's "tool surface" maps onto
`Client.Call()` for v2 endpoints, and onto the typed services
for v1 endpoints**. Since the upstream exposes v2 endpoints
as the default path (per the README), the bulk of the work
is `Client.Call()`.

### Why not just use `client.Page.Get` etc.?

Two reasons:

1. **v2 REST uses string IDs, v1 services use int IDs.** The
   `PageService.Get(pageID int, ...)` signature doesn't accept
   string IDs.
2. **The upstream's tools forward *any* path**, including
   paths the typed services don't cover (e.g.
   `/wiki/api/v2/labels`, `/wiki/api/v2/comments`,
   `/wiki/api/v2/blogposts`). A generic `Client.Call()` lets
   the tool handler forward *anything* without writing a typed
   wrapper for each endpoint.

This is the same architectural choice the upstream makes: it
treats the tool surface as a generic HTTP forwarder, not as
typed service wrappers.

## Verification

A reader of this spec should be able to:

1. Confirm
   `github.com/ctreminiom/go-atlassian/v2/confluence/v2/` has
   only `api_client_impl.go` (via `go doc
   github.com/ctreminiom/go-atlassian/v2/confluence/v2`).
2. Confirm
   `github.com/ctreminiom/go-atlassian/v2/confluence/` has
   `api_client_impl.go` + an `internal/` subdirectory with 28
   service files.
3. Write a 3-line Go program that imports `confluence`, calls
   `confluence.New(nil, "<site>.atlassian.net")`, calls
   `client.Auth.SetBasicAuth(...)`, and successfully GETs
   `/wiki/api/v2/spaces` via `Client.Call()`.