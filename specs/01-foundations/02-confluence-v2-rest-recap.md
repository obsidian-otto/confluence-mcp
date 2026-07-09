# 01.2 — Confluence REST v2 Recap (what we wrap)

## Overview

The Go MCP server is a **thin CRUD wrapper** over the Confluence
Cloud REST v2 API. It does not implement any of the higher-level
semantics (storage format conversion, version-number math,
markdown round-trip) — those live in the `confluence-sync` Python
spec set. This file is a **one-page recap** of the v2 surface the
Go MCP server needs to know about.

## Sources

- Atlassian v2 docs:
  https://developer.atlassian.com/cloud/confluence/rest/v2/intro/

## Spec

### Endpoints the upstream exposes

The upstream `@aashari/mcp-server-atlassian-confluence` v3.3.0
exposes **any** v2 (or v1) endpoint through the same five CRUD
tools. The common paths it documents in its README are:

| Method | Path | Notes |
| ------ | ---- | ----- |
| GET | `/wiki/api/v2/spaces` | list spaces |
| GET | `/wiki/api/v2/spaces/{id}` | space detail |
| GET | `/wiki/api/v2/pages` | list pages (`?space-id=`) |
| GET | `/wiki/api/v2/pages/{id}` | page detail |
| GET | `/wiki/api/v2/pages/{id}/body` | page body (`?body-format=storage`) |
| GET | `/wiki/api/v2/pages/{id}/children` | child pages |
| GET | `/wiki/api/v2/pages/{id}/labels` | page labels |
| GET | `/wiki/api/v2/pages/{id}/footer-comments` | comments |
| POST | `/wiki/api/v2/pages` | create page |
| POST | `/wiki/api/v2/pages/{id}/footer-comments` | add comment |
| POST | `/wiki/api/v2/pages/{id}/labels` | add label |
| PUT | `/wiki/api/v2/pages/{id}` | update page (must include `version.number+1`) |
| PUT | `/wiki/api/v2/blogposts/{id}` | update blog post |
| PATCH | `/wiki/api/v2/spaces/{id}` | partial update |
| PATCH | `/wiki/api/v2/footer-comments/{id}` | partial comment update |
| DELETE | `/wiki/api/v2/pages/{id}` | delete page |
| DELETE | `/wiki/api/v2/blogposts/{id}` | delete blog post |
| DELETE | `/wiki/api/v2/pages/{id}/labels/{label-id}` | remove label |
| DELETE | `/wiki/api/v2/footer-comments/{id}` | delete comment |
| DELETE | `/wiki/api/v2/attachments/{id}` | delete attachment |
| GET | `/wiki/rest/api/search?cql=...` | CQL search (v1 endpoint, used for search) |

The Go MCP server does not need to know these endpoints by heart
— it just forwards whatever path the LLM provides. The list above
is documented here so the implementer can sanity-check that the
five CRUD tools cover the common paths.

### Body shape — page create / update

```json
{
  "spaceId": "123456",
  "status": "current",
  "title": "Page Title",
  "parentId": "789",
  "body": {
    "representation": "storage",
    "value": "<p>Content here</p>"
  }
}
```

For update, an additional `version` field:

```json
{
  "id": "789",
  "version": {"number": 2}
}
```

The `version.number + 1` requirement is **the LLM's
responsibility** when calling the Go MCP server's `conf_put` —
the server is not sync-aware. Surfaced as gap **Q5** (should
the server refuse updates with `version.number <= current`?).

### Body shape — space partial update (PATCH)

```json
{
  "name": "New Name",
  "description": {"plain": {"value": "Desc", "representation": "plain"}}
}
```

### Search (v1 endpoint, used by upstream)

The v1 search endpoint at `/wiki/rest/api/search?cql=...` is
**still the supported search path** as of mid-2026. The v2
endpoints do not yet expose a search resource. The Go MCP
server forwards `GET /wiki/rest/api/search` with a `cql` query
param exactly as the upstream does.

### Response format

The Confluence v2 REST API returns **JSON**. The Go MCP server
returns the JSON in two shapes:

1. **TOON** (default) — Token-Oriented Object Notation, the
   upstream's default. Saves 30-60% tokens vs JSON. See
   `05-tool-surface-design/02-jmespath-and-toon.md` for the
   encoder choice.
2. **JSON** (`outputFormat: "json"`) — raw JSON passthrough,
   when the caller wants standard JSON.

### Error shape

v2 errors return:

```json
{
  "code": "INVALID_INPUT",
  "message": "Page title is required.",
  "errors": [{"message": "..."}]
}
```

The Go MCP server passes this through verbatim in the
`isError: true` MCP response (with HTTP status prepended for
context — see `09-anti-patterns/03-error-shapes.md`).

### Pagination

v2 endpoints use **cursor-based pagination**:

```
GET /wiki/api/v2/pages?limit=25&cursor=<token>
```

The upstream does not abstract pagination — it passes `limit`
and `cursor` through as `queryParams`. The Go MCP server
follows the same approach (cursor passthrough). Surfaced as gap
**Q6** — should the server auto-paginate when a `limit` higher
than the default is requested?

### Rate limits

Cloud rate limit is **points-based, hourly quota**, with a
**429** response and `Retry-After` header. The upstream does
not implement retry. The Go MCP server v1 also does not — it
surfaces 429 to the LLM verbatim, which is the upstream's
behavior. Surfaced as gap **Q7** for a v1.1 enhancement.

### What we deliberately do not implement

| Feature | Why deferred |
| ------- | ------------ |
| Storage format ↔ markdown conversion | Lives in the `confluence-sync` Python spec set's `04-storage-format/` folder |
| Version-number auto-increment | Sync semantics — lives in `confluence-sync/` |
| Conflict detection / 409 handling | Sync semantics |
| CQL query builder | The upstream forwards CQL as a string; we follow |
| Attachment **upload** (POST multipart) | v2 attachments-create is not yet exposed; v1 `/wiki/rest/api/content/{id}/child/attachment` still works. The Go MCP server v1 does **not** wrap the v1 upload endpoint — surfaced as gap **Q8**. |

## Verification

A reader of this spec should be able to:

1. Cross-reference every endpoint listed above against the
   Atlassian v2 docs at
   https://developer.atlassian.com/cloud/confluence/rest/v2/intro/
   and confirm the method/path/body shape matches.
2. Confirm `ctreminiom/go-atlassian`'s `confluence.New(nil,
   "INSTANCE_HOST")` produces a client whose `HTTP` field's
   `NewRequest` method can hit
   `https://INSTANCE_HOST/wiki/api/v2/...` with the right
   base-URL conventions.
3. Run the upstream server against a test instance and see the
   same five-tool surface.