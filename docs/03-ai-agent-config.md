# 03 — AI agent configuration

> !!! WARNING — EDITING FUNCTIONS MIGHT NOT BE COMPLETELY FINISHED !!!
> !!!                  USE AT YOUR OWN RISK                       !!!
>
> The 8 write-side tools (post, put, patch, delete, post_markdown,
> put_markdown, upload_attachment, delete_attachment, upload_drawio)
> can mutate Confluence content. They are reported as "complete"
> by `make test`, but they have **NOT** been end-to-end validated
> against every edge case of the Confluence Cloud REST API.
> Always dry-run on a test page first.

> Goal: make `mcp-confluence` available as a tool source for an
> AI agent (Hermes Agent, Claude Desktop, Cursor, or any MCP
> host). The contract is the same across hosts: the host
> spawns the binary, communicates JSON-RPC, and the binary
> returns one of the 18 tools on each call.

## What every MCP host does

Every MCP host (Hermes, Claude Desktop, Cursor, Open Interpreter,
etc.) does the same three things:

1. **Spawn** the binary with a list of arguments and a process env.
2. **Talk** to it over a transport (the host picks stdio or HTTP).
3. **Translate** tool calls from the agent's "actions" into
   JSON-RPC `tools/call` requests, and JSON-RPC responses back
   into "tool outputs".

The `mcp_servers:` block in the host config is a dict of
`{server_name → {command, args, env}}`. The host reads this block,
spawns one process per server, and aggregates the tools across
all servers into the agent's prompt.

## Hermes Agent

Hermes is the canonical host for this project. Its config lives
at `~/.hermes/config.yaml`. Three transport options:

### stdio mode (canonical — same as the v0.1 binary)

```yaml
# ~/.hermes/config.yaml — confluence MCP server (stdio)
mcp_servers:
  confluence:
    command: /path/to/bin/mcp-confluence
    args: ["stdio"]                 # or [] — `stdio` is default
    env:
      ATLASSIAN_SITE_NAME: ${WORKSPACE_SITE}    # e.g. smartergroup
      ATLASSIAN_USER_EMAIL: ${WORKSPACE_EMAIL}
      ATLASSIAN_API_TOKEN:  ${WORKSPACE_API_TOKEN}
```

Notice: zero literal tokens in the YAML. `${VAR}` expansion is
evaluated by Hermes against the user's `~/.hermes/.env` at
config-load time. `grep ATATT ~/.hermes/config.yaml` returns
**zero hits** — the literal API token never lands in the file.

After the YAML edit:

```sh
hermes mcp restart confluence
hermes mcp test confluence   # should list 18 tools
```

### serve mode (TCP/HTTP — new in v4)

For LAN deployment, dev containers, or shared-network use:

```yaml
# ~/.hermes/config.yaml — confluence MCP server (TCP/HTTP)
mcp_servers:
  confluence:
    command: /path/to/bin/mcp-confluence
    args: ["serve", "--listen=127.0.0.1:8080"]
    env:
      ATLASSIAN_SITE_NAME: ${WORKSPACE_SITE}
      ATLASSIAN_USER_EMAIL: ${WORKSPACE_EMAIL}
      ATLASSIAN_API_TOKEN:  ${WORKSPACE_API_TOKEN}
```

> Hermes will spawn `serve` and reuse the same JSON-RPC tool
> surface (18 tools, identical schemas) — only the framing
> differs. The agent sees no difference. See
> [05-architecture-decisions.md](05-architecture-decisions.md) §"Why
> two transports" for the rationale.

### Flag-driven overrides (the dev-velocity override)

For one-off dev runs where you want to override the env vars
without touching `~/.hermes/.env`:

```yaml
mcp_servers:
  confluence:
    command: /path/to/bin/mcp-confluence
    args:
      - "stdio"
      - "--debug"
    env:
      ATLASSIAN_SITE_NAME: smartergroup
      ATLASSIAN_USER_EMAIL: "you@example.com"
      ATLASSIAN_API_TOKEN: "${WORKSPACE_API_TOKEN}"
```

`--debug` makes the binary log every tool call to stderr
(method, path, latency). Useful when an agent is making
unexpected calls and you want to know which.

## Claude Desktop (the cross-platform reference)

Claude Desktop uses a JSON config at:

- macOS: `~/Library/Application Support/Claude/claude_desktop_config.json`
- Linux: `~/.config/Claude/claude_desktop_config.json`
- Windows: `%APPDATA%\Claude\claude_desktop_config.json`

```json
{
  "mcpServers": {
    "confluence": {
      "command": "/path/to/bin/mcp-confluence",
      "args": ["stdio"],
      "env": {
        "ATLASSIAN_SITE_NAME": "smartergroup",
        "ATLASSIAN_USER_EMAIL": "you@example.com",
        "ATLASSIAN_API_TOKEN": "${ATLASSIAN_API_TOKEN}"
      }
    }
  }
}
```

> **Note on `${ATLASSIAN_API_TOKEN}` in JSON config:** Claude Desktop
> does NOT do env-var interpolation; you must write the literal
> token here. This is a host-side limitation. The recommended
> pattern is: store the token in your shell's `.envrc` (or your
> shell's startup file), pass it as `envp` to the Claude
> Desktop process via the OS, and read it from there. **Do NOT
> paste the literal token into the JSON config** — `claude_desktop_config.json`
> is often synced to cloud and is the wrong place for a secret.
>
> The Hermes recommendation (above) is the secure pattern;
> Claude Desktop's config file is the insecure legacy.

## Cursor

Cursor reads `~/.cursor/mcp.json` (same shape as Claude
Desktop). Use the same block. Cursor also supports per-project
MCP configs at `<project>/.cursor/mcp.json` — useful for repos
that want a locally-bundled confluence server.

## Configuring an agent's behavior with the tool surface

The 18 MCP tools are documented in
[../AGENTS.md §"## Purpose"](../AGENTS.md). A few notes specific
to driving an AI agent:

### Tools the agent should know about

The agent enumerates all 18 tools at startup via `tools/list`. Two
of them are particularly important for agents to use correctly:

- `conf_help` — returns the full surface map; the agent should
  call this first if uncertain which tool fits a task.
- `conf_search` — full-text search via CQL; the agent's
  "search across all of Confluence" tool.

The other 16 are CRUD + helpers + markdown round-trip; see
[../AGENTS.md](../AGENTS.md) for the per-group table.

### The `outputFormat` knob

Every tool call accepts `arguments.outputFormat`. Default is
`"toon"` (token-efficient for the LLM); for non-LLM consumers
(anything that wants machine-parseable output), pass
`"json"`. An agent working in a Bash loop or generating a CSV
report should always pass `"json"` because the TOON encoder is
optimised for round-tripping through a language model, not for
parsing with `jq` / Python `json` / etc.

### The `jq` filter (JMESPath, not jq-the-tool)

`params.arguments.jq` is a JMESPath expression applied to the
decoded response **before** encoding. Use it to trim responses
that would otherwise blow up the context window. Example:

```
mcp__confluence_conf_list_pages(
  space-id="780763211",
  limit=50,
  jq="{results: results[*].{id: id, title: title}}"
)
```

The LLM only sees `results: [...]`, not the per-page metadata
that would otherwise pad the response. See
[04-configuration-reference.md](04-configuration-reference.md) §"`jq`
filter" for the JMESPath cheatsheet.

### Idempotency and `version`

`conf_put` requires an incremented `version.number` in the body.
Without it, Confluence returns 409 Conflict. The MCP tool
documents this in its `jsonschema:"description"`; the agent
should:

1. Get the page with `conf_get` (or `conf_get_page_body`).
2. Read the current `version.number`.
3. Pass `body.version.number = current + 1`.

The error envelope (`<METHOD> <path>: 409 Conflict - <body>`)
shows up in the LLM's context window as a clear retriable error
— no silent failures.

### Rate limiting and retry

The Confluence Cloud REST API is rate-limited per
[04-rate-limits.md reference](../specs/) (see the
Rate-Limit cheatsheet for tier model). The 9-step `executeRequest`
pipeline does **not** retry on 429 today. An agent making
high-throughput calls should monitor the response envelope for
status codes and back off accordingly.

## Multi-server setups

Hermes is multi-server — you can register multiple `mcp_servers:`
blocks. The confluence server's 18 tools are namespaced as
`mcp__confluence__*`. If you also have (say) a Jira MCP server,
its tools are `mcp__jira__*`. There is no cross-contamination —
each server is a separate process with its own transport.

## Removing or replacing the server

```sh
hermes mcp remove confluence             # deregisters
hermes mcp remove confluence --purge     # also deletes cached state
```

After `hermes mcp remove confluence`, a `hermes mcp list` no
longer shows it. The next `hermes` restart won't try to spawn it.

## Troubleshooting

If the agent reports "tool not found" or "spawning failed":

1. `hermes mcp list` — is the entry registered?
2. `hermes mcp test confluence` — does the binary spawn cleanly?
3. Check the binary's stderr (visible in `hermes mcp test`)
   for the startup banner — missing banner = spawn failure,
   not transport failure.
4. If using `serve` mode, confirm the port is reachable
   (`ss -tln | grep 8080` on the host running the binary).
5. See [06-troubleshooting.md](06-troubleshooting.md) for
   diagnostic walkthroughs.
