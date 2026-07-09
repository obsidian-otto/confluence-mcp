# 01.1 — Cloud vs Data Center

## Overview

The Go MCP server targets **Confluence Cloud** exclusively.
Confluence Data Center (the self-hosted product) is intentionally
not in scope at v1 because (a) the upstream
`@aashari/mcp-server-atlassian-confluence` is Cloud-only, (b)
`ctreminiom/go-atlassian` is built around Cloud endpoints first,
and (c) the user runs Confluence Cloud (per the
`confluence-sync` Python spec set's primary-target assertion in
`~/Desktop/hermes/confluence/specs/confluence-sync/01-foundations/03-cloud-vs-datacenter.md`).

## Sources

- Atlassian docs — Cloud vs Data Center:
  https://support.atlassian.com/confluence-cloud/docs/what-is-confluence/
  (Cloud is the SaaS product; Data Center is the self-hosted
  product).
- `go-atlassian` README — supports both Cloud and Server/DC
  clients, but the Confluence v1 service signatures match Cloud
  conventions (e.g. `pageID int`, not `pageID string`).
- `aashari/mcp-server-atlassian-confluence` README — every
  example assumes Cloud (`your-company.atlassian.net`).

## Spec

### Target product

| Property | Confluence Cloud | Confluence Data Center | This spec set |
| -------- | ---------------- | ---------------------- | ------------- |
| Hostname shape | `*.atlassian.net` | customer-managed | **Cloud** |
| Auth (API token) | email + token | PAT (different shape) | **Cloud (basic auth)** |
| Auth (OAuth 2.0 3LO) | Atlassian-hosted `auth.atlassian.com` | customer-managed auth | **Cloud (v2 work)** |
| REST API base | `/wiki/api/v2/...` (v2 GA) | `/rest/api/...` (v1 only at GA-time) | **Cloud** |
| `version.number` semantics | optimistic-lock integer | optimistic-lock integer (same shape) | same |
| Rate limit model | points-based hourly quota | self-managed | Cloud |

### Decision: Cloud-only at v1

The Go MCP server **only supports Cloud**. If a future user needs
Data Center, the path is:

1. Add a `CONFLUENCE_BASE_URL` env var so the binary can point
   at a self-hosted instance (`https://confluence.example.com`
   instead of `https://your-company.atlassian.net`).
2. Switch from basic-auth (`Auth.SetBasicAuth(email, token)`) to
   PAT (`Auth.SetBearerToken(pat)`).
3. Decide whether to wrap the v1 REST API or the v2 API. DC
   historically only exposes v1, but Atlassian has been
   backporting v2 endpoints.

Surfaced as gap **Q4**.

### What this rules out

- No `*-release/*` DC-only endpoints.
- No `crowd` or `saml` integration (those are DC-side).
- No Atlassian Access / Guard admin API.

### What still works

Every Cloud endpoint the upstream calls
(`/wiki/api/v2/spaces`, `/wiki/api/v2/pages`, etc.) is a direct
match. The `go-atlassian` Confluence v1 services map cleanly onto
the Cloud REST v1 surface; for v2 REST endpoints (the primary
ones the upstream uses), the Go port uses `Client.Call()`
directly (see `03-go-atlassian/03-client-call-raw-http.md`).

## Verification

A reader of this spec should be able to:

1. Confirm the upstream README's examples all use
   `*.atlassian.net` hostnames.
2. Confirm `go-atlassian`'s `confluence.New(nil, site)`
   constructor expects a Cloud site URL (the README example is
   `"INSTANCE_HOST"`, which Cloud users fill with
   `your-company.atlassian.net`).
3. Grep this spec set for any DC-specific endpoint and find
   none.