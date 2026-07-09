# 08.1 — `~/.hermes/config.yaml` Registration

## Overview

The Go MCP server integrates with Hermes via the
**`mcp_servers:` block** in `~/.hermes/config.yaml`. The
block declares the binary's path, the three required env vars
(passed via `env:`), and optional timeouts. This file
documents the canonical config snippet and the `${ENV_VAR}`
substitution pattern.

## Sources

- Hermes native-mcp skill:
  `~/.hermes/skills/mcp/native-mcp/SKILL.md` (the
  `mcp_servers:` block schema and `${VAR}` substitution
  behavior).
- Hermes docs:
  https://hermes-agent.nousresearch.com/docs/user-guide/features/mcp/

## Spec

### The canonical config snippet

```yaml
# ~/.hermes/config.yaml
mcp_servers:
  confluence:
    command: "mcp-confluence"        # The Go binary (must be on $PATH)
    args: []                         # No args; the binary reads env vars
    env:
      ATLASSIAN_SITE_NAME: "your-company"
      ATLASSIAN_USER_EMAIL: "you@example.com"
      ATLASSIAN_API_TOKEN: "${ATLASSIAN_API_TOKEN}"  # resolved at server-connect time
    timeout: 60                      # Per-tool-call timeout (seconds)
    connect_timeout: 30              # Initial connection timeout (seconds)
```

### Where the binary lives

`command: "mcp-confluence"` assumes the binary is on Hermes'
`$PATH`. Three options:

| Option | Setup | Trade-off |
| ------ | ----- | --------- |
| **Symlink in `~/.local/bin`** | `ln -s /opt/mcp-confluence ~/.local/bin/mcp-confluence` | Clean; standard XDG location |
| **Absolute path** | `command: "/opt/mcp-confluence"` | Most explicit; works regardless of `$PATH` |
| **Container invocation** | `command: "docker"`, `args: ["run", "--rm", "-i", ...]` | Most portable; pulls image every call (slow) |

For the "container invocation" case:

```yaml
mcp_servers:
  confluence:
    command: "docker"
    args:
      - "run"
      - "--rm"
      - "-i"               # keep stdin open
      - "--env-file" "/home/user/.config/mcp-confluence/.env"
      - "mcp-confluence:latest"
    timeout: 90            # Docker startup adds 1-2s overhead
    connect_timeout: 60
```

The `command: "docker"` + `args: ["run", "--rm", "-i", ...]`
form is how Hermes would invoke the **container image**
built by `pack build` (or `make image`). This is the
recommended path for **shared / remote** MCP servers (e.g.
team-shared Confluence instance) — gap **Q19** documents a
future `--read-only` flag for safer shared deployment.

For **local dev**, the binary path option is recommended.

### `${ATLASSIAN_API_TOKEN}` — the safe form

The literal value of the API token (`ATATT3xFfGF0...`) must
**never** appear in `config.yaml`. Instead:

```yaml
env:
  ATLASSIAN_API_TOKEN: "${ATLASSIAN_API_TOKEN}"
```

Hermes resolves `${VAR}` at **server-connect time** from the
**Hermes process environment** (which includes
`~/.hermes/.env`). The literal token goes in
`~/.hermes/.env`:

```bash
# ~/.hermes/.env (mode 0600)
ATLASSIAN_API_TOKEN=ATATT3xFfGF0...
```

The token never appears in any file that might be committed,
backed up, or read by another tool. This is the same pattern
the `native-mcp` skill recommends for all secret env vars.

### Tool naming after registration

Hermes registers the MCP tools with the prefix pattern:

```
mcp_{server_name}_{tool_name}
```

For `server_name: confluence`:

| MCP tool | Hermes-registered name |
| -------- | ---------------------- |
| `conf_get` | `mcp_confluence_conf_get` |
| `conf_post` | `mcp_confluence_conf_post` |
| `conf_put` | `mcp_confluence_conf_put` |
| `conf_patch` | `mcp_confluence_conf_patch` |
| `conf_delete` | `mcp_confluence_conf_delete` |

Hyphens in server names become underscores. The user calls
these from a Hermes chat with the prefixed name:

```
> Use mcp_confluence_conf_get to list all Confluence spaces.
```

### Timeouts

| Timeout | Default | When to adjust |
| ------- | ------- | -------------- |
| `timeout` | 120s | Per-tool-call. Confluence API calls are usually <5s. 60s is a reasonable cap. |
| `connect_timeout` | 60s | Initial connection (binary startup + `list_tools`). 30s is enough for a static Go binary. |

If the binary takes >30s to connect, check:

1. **DNS resolution** — `confluence.atlassian.net` must
   resolve.
2. **TCP handshake** — outbound 443 must be open.
3. **Tool registration** — should be <1s for 5 tools; if
   not, there's a bug in `RegisterAll`.

## Verification

A reader of this spec should be able to:

1. Add the config snippet to `~/.hermes/config.yaml`.
2. Run `hermes mcp test confluence` and see the five tools
   listed with names `mcp_confluence_conf_get`, etc.
3. From a Hermes chat, call `mcp_confluence_conf_get` with
   `path: "/wiki/api/v2/spaces", limit: "5"` and see a
   TOON-encoded list of spaces returned.
4. Confirm `grep ATATT ~/.hermes/config.yaml` returns
   nothing (the token is in `~/.hermes/.env`, not in the
   config).