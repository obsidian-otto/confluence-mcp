# 08.3 — Sample Invocation (Hermes Chat End-to-End)

## Overview

This file documents a **complete end-to-end test scenario** —
from `hermes mcp test confluence` to a Hermes chat that
lists spaces, fetches a page body, and updates a page. The
scenario proves the full wiring: build → register → connect
→ tool discovery → tool call → response.

## Sources

- Hermes docs:
  https://hermes-agent.nousresearch.com/docs/user-guide/features/mcp/
- Upstream README (the example prompts at lines 239-270 of
  `/tmp/mcp-research/aashari-README.md`).

## Spec

### Step 0 — Pre-flight

```bash
# 1. Build the binary (per 06-implementation-skeleton/02-main-entrypoint.md)
make build      # produce ./bin/mcp-confluence
make image      # build the OCI image (optional, for container deploy)

# 2. Install the binary on $PATH (or use the container form per 01-config-yaml.md)
install -m 0755 ./bin/mcp-confluence ~/.local/bin/mcp-confluence

# 3. Configure ~/.hermes/.env (mode 0600) — OR set the three vars via
#    the project-local .env file (per Q22 lock). Both work; the
#    .hermes/.env form is canonical for Hermes.
cat >> ~/.hermes/.env <<'EOF'
ATLASSIAN_SITE_NAME=your-company
ATLASSIAN_USER_EMAIL=you@example.com
ATLASSIAN_API_TOKEN=ATATT3xFfGF0...
EOF
chmod 0600 ~/.hermes/.env

# 4. Configure ~/.hermes/config.yaml (per 01-config-yaml.md)
# Add the mcp_servers.confluence block.

# 5. Test the connection
hermes mcp test confluence
```

Expected output of `hermes mcp test confluence`:

```
✓ Connected to confluence
  Tools (5):
    - conf_get
    - conf_post
    - conf_put
    - conf_patch
    - conf_delete
  Server: mcp-confluence v1.0.0
  Transport: stdio
```

### Step 1 — List spaces

```
$ hermes chat
> List my Confluence spaces. Show me just the key, name, and type.
```

Behind the scenes, the model calls:

```
mcp_confluence_conf_get
  arguments: {
    "path": "/wiki/api/v2/spaces",
    "queryParams": {"limit": "25"},
    "jq": "results[*].{key: key, name: name, type: type}",
    "outputFormat": "toon"
  }
```

Expected tool response (TOON):

```yaml
results:
  - key: DEV
    name: Development
    type: site
  - key: HR
    name: Human Resources
    type: site
  - key: MKT
    name: Marketing
    type: site
```

Expected Hermes reply:

> Your Confluence has 3 spaces:
> - **DEV** (Development) — site
> - **HR** (Human Resources) — site
> - **MKT** (Marketing) — site

### Step 2 — Fetch a page body

```
> Get the body of page 1234567 in storage format.
```

Behind the scenes:

```
mcp_confluence_conf_get
  arguments: {
    "path": "/wiki/api/v2/pages/1234567/body",
    "queryParams": {"body-format": "storage"}
  }
```

Expected response (TOON):

```yaml
id: 1234567
status: current
title: My Page
body:
  storage:
    value: <p>Page content here.</p>
    representation: storage
_version: 1
```

### Step 3 — Update a page (PUT with version increment)

```
> Update page 1234567 with the title "Updated Title" and body
> "<p>New content.</p>". Use the current version number + 1.
```

Behind the scenes, the model:

1. First calls `conf_get` for the page to read its current
   `version.number`.
2. Then calls `conf_put` with `version.number = current + 1`.

```
mcp_confluence_conf_put
  arguments: {
    "path": "/wiki/api/v2/pages/1234567",
    "body": {
      "id": "1234567",
      "status": "current",
      "title": "Updated Title",
      "spaceId": "123456",
      "body": {
        "representation": "storage",
        "value": "<p>New content.</p>"
      },
      "version": {"number": 2}
    },
    "jq": "{id: id, title: title, version: version.number}"
  }
```

Expected response (TOON):

```yaml
id: "1234567"
title: Updated Title
version: 2
```

If the model forgot to increment `version.number`, the
Confluence API returns **409 Conflict**:

```
PUT /wiki/api/v2/pages/1234567: 409 Conflict - {"code":"VERSION_MISMATCH","message":"..."}
```

The model then re-reads the page and retries with
`version.number = current + 1`.

### Step 4 — Search

```
> Search for pages containing "deployment" in the DEV space.
```

Behind the scenes:

```
mcp_confluence_conf_get
  arguments: {
    "path": "/wiki/rest/api/search",
    "queryParams": {"cql": "type=page AND space=DEV AND text~deployment"},
    "jq": "results[*].{id: id, title: title, excerpt: excerpt}",
    "outputFormat": "toon"
  }
```

Expected response (TOON):

```yaml
results:
  - id: "1234567"
    title: Deployment Guide
    excerpt: This page describes the deployment process for...
  - id: "2345678"
    title: CI/CD Pipeline
    excerpt: Continuous integration and deployment using...
```

### Step 5 — Truncation handling

```
> List all pages in the DEV space.
```

If DEV has 500 pages, the response exceeds 40k chars and is
truncated:

```
[truncated 40,123 / 250,789 chars — full response at
/tmp/mcp/confluence-<pid>-<ts>.json]
results:
  - id: "1234567"
    title: Deployment Guide
  ... (truncated)
```

The model can either:

1. **Narrow with `jq`** — re-call with
   `jq: "results[*].{id: id, title: title}"` to skip
   `excerpt` etc.
2. **Paginate** — re-call with `queryParams: {"limit": "25",
   "cursor": "<token>"}` for smaller batches.
3. **Read the full file** — use `mcp_filesystem_read_file`
   (a different MCP server) to read
   `/tmp/mcp/confluence-<pid>-<ts>.json`.

## Verification

A reader of this spec should be able to:

1. Follow Steps 0-4 in order and see each succeed.
2. Confirm the tool calls in Hermes' debug log match the
   expected arguments above (Hermes logs all MCP tool calls
   at debug level).
3. Confirm TOON output by piping any tool response through
   `head -c 200` and seeing the YAML-like indentation.
4. Trigger a 409 in Step 3 and confirm the error response
   shape matches `09-anti-patterns/03-error-shapes.md`.