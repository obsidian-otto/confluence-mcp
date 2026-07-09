# 08.2 — Catalog `manifest.yaml` (one-click `hermes mcp install`)

## Overview

Hermes has a **catalog** of pre-approved MCP servers at
`optional-mcps/<name>/manifest.yaml` in the hermes-agent
repo. Users install catalog entries with one click via
`hermes mcp install confluence`. This file documents the
canonical `manifest.yaml` for the Go MCP server so a future
PR can add it to the catalog.

## Sources

- Hermes MCP catalog docs:
  https://hermes-agent.nousresearch.com/docs/user-guide/features/mcp/#catalog-one-click-install-for-nous-approved-mcps
- Catalog directory:
  https://github.com/NousResearch/hermes-agent/tree/main/optional-mcps

## Spec

### The canonical `manifest.yaml`

```yaml
# optional-mcps/confluence/manifest.yaml
name: confluence
display_name: Confluence (Go MCP server)
description: |
  Read, write, search, and comment on Atlassian Confluence
  Cloud pages via MCP. Five generic CRUD tools (conf_get,
  conf_post, conf_put, conf_patch, conf_delete) over the
  Confluence REST v2 API, with TOON-default output and
  JMESPath filtering for token efficiency. Single static
  Go binary, no Node dependency. Source-ported from
  @aashari/mcp-server-atlassian-confluence v3.3.0.
homepage: https://github.com/<owner>/mcp-confluence
source: https://github.com/<owner>/mcp-confluence
manifest_version: 1

# Auth: API token (basic auth)
auth:
  type: api_key
  env_var: ATLASSIAN_API_TOKEN
  prompt: |
    Enter your Atlassian API token. Generate one at
    https://id.atlassian.com/manage-profile/security/api-tokens
    (44-character opaque string starting with "ATATT").
  write_to: ~/.hermes/.env
  required: true

# Non-secret values written to ~/.hermes/.env too
env:
  ATLASSIAN_SITE_NAME:
    prompt: "Atlassian site name (the part before '.atlassian.net', e.g. 'your-company')"
    required: true
  ATLASSIAN_USER_EMAIL:
    prompt: "Atlassian account email (e.g. 'you@example.com')"
    required: true

# Transport: stdio (binary on $PATH)
transport:
  type: stdio
  command: "mcp-confluence"
  args: []
  timeout: 60
  connect_timeout: 30

# Tool selection defaults (pre-checked at install time)
tools:
  default_enabled:
    - conf_get
    - conf_post
    - conf_put
    - conf_patch
    - conf_delete

# Optional: detect if the binary is installed
bootstrap:
  - name: check-binary
    command: "command -v mcp-confluence"
    on_missing: |
      The mcp-confluence binary was not found on $PATH.

      To install:
        1. Download from https://github.com/<owner>/mcp-confluence/releases
        2. Place at ~/.local/bin/mcp-confluence (or anywhere on $PATH)
        3. chmod +x ~/.local/bin/mcp-confluence

      Or build from source:
        git clone https://github.com/<owner>/mcp-confluence
        cd mcp-confluence
        go build -o ~/.local/bin/mcp-confluence ./cmd/mcp-confluence
```

### What the manifest does at install time

When the user runs `hermes mcp install confluence`, Hermes:

1. **Reads the manifest** to determine the install steps.
2. **Runs `bootstrap[].command`** to verify the binary is
   installed. If `command -v mcp-confluence` fails, the
   `on_missing` message is shown.
3. **Prompts for credentials** (`ATLASSIAN_SITE_NAME`,
   `ATLASSIAN_USER_EMAIL`, `ATLASSIAN_API_TOKEN`).
4. **Writes the non-secret values** to `~/.hermes/.env`.
5. **Writes the `mcp_servers:` block** to
   `~/.hermes/config.yaml` with `${VAR}` references for the
   secret values.
6. **Probes the server** (`hermes mcp test confluence`-style
   connection check) to list available tools.
7. **Presents the tool selection checklist** (pre-checked
   with `tools.default_enabled`).
8. **Persists the selection** to
   `mcp_servers.confluence.tools.include` (or omits the
   filter if all tools selected).

### Why the manifest uses `bootstrap` instead of `pip install` / `npm install`

The upstream catalogs install Node.js or Python dependencies
(`bootstrap: [{command: "npm install -g ..."}]`). The Go
MCP server is a **single static binary** — no runtime
dependency. The `bootstrap` step is just a `command -v` check
that points the user at the right install path if missing.

This is faster (no install), safer (no supply chain), and
matches the catalog's `trust` model: "you should still read
the manifest before installing, especially the `source:`
field's repository, the entry's `bootstrap` commands, and
any `transport.command` invocation."

### Why we don't include the container option in the manifest

The catalog manifest is **declarative** — it doesn't support
"either binary OR container" routing. The binary path is
the canonical install. Container-based deployment is
documented in `08-deployment-hermes/01-config-yaml.md` as a
manual configuration (the user edits `~/.hermes/config.yaml`
directly).

### What we deliberately do NOT include

| Manifest field | Why not |
| -------------- | ------- |
| `pip install` / `npm install` bootstrap | No runtime dep for a Go binary |
| `sampling` block | v1 doesn't use MCP sampling |
| `prompts` / `resources` blocks | v1 is tools-only |
| Multiple `transport` entries | stdio is the only v1 transport |

## Verification

A reader of this spec should be able to:

1. Place `manifest.yaml` at
   `optional-mcps/confluence/manifest.yaml` in the
   hermes-agent repo.
2. Run `hermes mcp install confluence` (from a fork) and see
   the install wizard.
3. Confirm the `mcp_servers:` block in `~/.hermes/config.yaml`
   after install matches the canonical snippet in
   `01-config-yaml.md`.
4. Confirm `~/.hermes/.env` contains the three `ATLASSIAN_*`
   variables after install.