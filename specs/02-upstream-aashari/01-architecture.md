# 02.1 — Upstream Architecture (`@aashari/mcp-server-atlassian-confluence`)

## Overview

The upstream server `aashari/mcp-server-atlassian-confluence`
v3.3.0 (last verified 2026-07-09) is a **TypeScript / Node.js
≥18** MCP server published as
`@aashari/mcp-server-atlassian-confluence` on npm. It exposes
five CRUD tools (`conf_get`, `conf_post`, `conf_put`,
`conf_patch`, `conf_delete`) over **stdio** by default and
**Streamable HTTP** as an opt-in transport. The Go port
preserves the exact tool surface, parameter shape, response
shaping (TOON + JMESPath), and auth scheme; only the language,
runtime, and HTTP client change.

This file documents the upstream's **architecture** —
directory layout, layering, and request flow — so the Go
port's `internal/` package boundaries map 1:1 onto the
upstream's `src/` directories.

## Sources

- Repository: https://github.com/aashari/mcp-server-atlassian-confluence
- README: https://github.com/aashari/mcp-server-atlassian-confluence/blob/main/README.md
  (626 lines; the file fetched to
  `/tmp/mcp-research/aashari-README.md` during research)
- `package.json`:
  https://github.com/aashari/mcp-server-atlassian-confluence/blob/main/package.json
  (version, deps, entry point, bin)
- Source tree (verified via GitHub API):
  https://github.com/aashari/mcp-server-atlassian-confluence/tree/main/src
  - `src/index.ts` (entrypoint, 221 lines)
  - `src/cli/index.ts` (CLI mode dispatch — not in Go port)
  - `src/tools/atlassian.api.tool.ts` (tool registration, 294
    lines)
  - `src/tools/atlassian.api.types.ts` (zod input schemas, 117
    lines)
  - `src/controllers/atlassian.api.controller.ts` (controller
    layer, 158 lines)
  - `src/services/vendor.atlassian.api.service.ts` (HTTP
    layer, 261 lines)
  - `src/utils/transport.util.ts` (auth + fetch wrapper)
  - `src/utils/config.util.ts` (env + `.env` + `~/.mcp/configs.json`
    loader)
  - `src/utils/jq.util.ts` (JMESPath wrapper)
  - `src/utils/formatter.util.ts` (TOON encoder + 40k
    truncation)
  - `src/utils/logger.util.ts` (contextual logger)
  - `src/utils/error-handler.util.ts` (MCP error → response)
  - `src/types/common.types.ts` (shared types)
- npm: https://www.npmjs.com/package/@aashari/mcp-server-atlassian-confluence
  (verified 3.3.0 is the current release at survey time).

## Spec

### License / package metadata

| Property | Value |
| -------- | ----- |
| License | **MIT** (verified via LICENSE file fetch attempt returned 404; confirmed MIT in `package.json` and the `LICENSE` symbol match) |
| Version | **3.3.0** (at survey time, 2026-07-09) |
| `engines.node` | `>=18.0.0` |
| `bin` | `mcp-atlassian-confluence` → `./dist/index.js` |
| Dependencies | `@modelcontextprotocol/sdk`, `express`, `cors`, `dotenv` (and a JMESPath lib, a YAML lib for `~/.mcp/configs.json`, etc.) |

**License note.** The LICENSE file was not at the repo root at
the time of survey (404 on direct fetch); the README and
`package.json` both declare MIT. For the Go port, we do **not**
copy upstream source code — we re-implement the same interface
in Go. The MIT license is therefore not load-bearing for the
port itself; it only affects any verbatim code reuse (which is
none in v1).

### Repository layout (src/)

```
src/
├── index.ts                    Entry point (stdio vs http mode)
├── cli/                        CLI dispatcher (we DO NOT port this)
│   └── (not in Go port)
├── controllers/
│   └── atlassian.api.controller.ts   handleGet / handlePost / handlePut / handlePatch / handleDelete
├── services/
│   └── vendor.atlassian.api.service.ts  Thin wrapper around transport.fetchAtlassian
├── tools/
│   ├── atlassian.api.tool.ts    registerTools(server) — wires 5 tools
│   └── atlassian.api.types.ts   zod schemas for each tool's input
├── types/
│   └── common.types.ts         ControllerResponse, etc.
└── utils/
    ├── transport.util.ts       fetchAtlassian() — auth header + fetch
    ├── config.util.ts          config.load() — env + .env + ~/.mcp/configs.json
    ├── jq.util.ts              applyJqFilter(data, expr)
    ├── formatter.util.ts       toOutputString(data, useToon), truncateForAI
    ├── logger.util.ts          contextual logger
    └── error-handler.util.ts   handleControllerError / formatErrorForMcpTool
```

### Layered architecture

The upstream uses a **clean 3-layer split** with utils on the
side:

```
                 ┌─────────────────────────────────┐
   MCP request → │ tools/atlassian.api.tool.ts     │  registerTools()
                 │ (zod validation + tool defs)   │
                 └────────────────┬────────────────┘
                                  │ handler(args)
                                  ▼
                 ┌─────────────────────────────────┐
                 │ controllers/atlassian.api.      │  handleGet / handlePost / ...
                 │ controller.ts                   │
                 │ (orchestration: jq + format)    │
                 └────────────────┬────────────────┘
                                  │ service.request(path, opts)
                                  ▼
                 ┌─────────────────────────────────┐
                 │ services/vendor.atlassian.      │  request / get / post / ...
                 │ api.service.ts                  │  validateCredentials, normalizePath
                 └────────────────┬────────────────┘
                                  │ fetchAtlassian(creds, path, opts)
                                  ▼
                 ┌─────────────────────────────────┐
                 │ utils/transport.util.ts         │  Basic auth header + fetch
                 │ utils/config.util.ts            │  Env + .env + ~/.mcp/configs.json
                 └─────────────────────────────────┘
```

**Mapping to Go port:**

| Upstream layer | Go port equivalent |
| -------------- | ------------------ |
| `src/index.ts` (entrypoint) | `cmd/mcp-confluence/main.go` |
| `src/tools/*` (MCP tool definitions) | `internal/tools/` (one file per tool + a `register.go`) |
| `src/controllers/*` (orchestration) | `internal/tools/handler.go` (single handler that does jq + format) |
| `src/services/*` (HTTP layer) | `internal/atlassian/client.go` |
| `src/utils/transport.util.ts` | `internal/atlassian/auth.go` + a thin `do()` helper |
| `src/utils/config.util.ts` | `internal/config/config.go` + `internal/config/dotenv.go` |
| `src/utils/jq.util.ts` | `internal/jmespath/` (wraps `github.com/jmespath/go-jmespath`) |
| `src/utils/formatter.util.ts` | `internal/toon/` (TOON encoder) |
| `src/utils/logger.util.ts` | `log/slog` (stdlib) writing to stderr |
| `src/utils/error-handler.util.ts` | `internal/atlassian/errors.go` (typed errors → MCP `isError`) |

### Request flow (one tool call)

For a `conf_get` call to `/wiki/api/v2/spaces`:

1. **MCP framing** — the MCP SDK reads the JSON-RPC message
   from stdin (`{"jsonrpc":"2.0","method":"tools/call","params":{"name":"conf_get","arguments":{"path":"/wiki/api/v2/spaces","limit":"25"}}}`).
2. **Tool dispatch** — `registerTools`' `conf_get` handler is
   called with the parsed args.
3. **Zod validation** — `GetApiToolArgs.parse(args)` checks
   `path` is a string starting with `/`, `queryParams` is
   `Record<string,string>`, `jq` is optional string,
   `outputFormat` is optional `"toon" | "json"`.
4. **Controller call** — `handleGet({path, queryParams, jq,
   outputFormat})`.
5. **Service call** — `atlassianApiService.request(path,
   {method:"GET", queryParams})`.
6. **HTTP call** — `fetchAtlassian(creds,
   "/wiki/api/v2/spaces?limit=25", {method:"GET"})` — builds
   the URL `https://${site}.atlassian.net${path}${queryString}`,
   sets `Authorization: Basic <base64(email:token)>`, sets
   `Accept: application/json`.
7. **Response handling** — fetch returns JSON; `applyJqFilter`
   is called if `jq` is set; `toOutputString(data, useToon)`
   encodes to TOON or JSON.
8. **Truncation** — `truncateForAI(content, rawResponsePath)`
   if the response is >40k chars (writes the full response to
   `/tmp/mcp/<session>.json` and embeds a notice in the
   response).
9. **MCP response** — `return { content: [{ type: 'text',
   text: content }] }`.

### Transport modes

The upstream supports two transports:

| Mode | How | Used by |
| ---- | --- | ------- |
| **stdio** (default) | `new StdioServerTransport()` | Claude Desktop, Cursor, Hermes stdio launch |
| **Streamable HTTP** (opt-in) | `new StreamableHTTPServerTransport({sessionIdGenerator: undefined})` over Express | Self-hosted / shared server |

Selection is via `TRANSPORT_MODE=http` env var (defaults to
`stdio`). The Go port **only implements stdio at v1** (gap
**Q3** covers HTTP).

### CLI mode

The upstream also has a CLI mode for direct terminal use:
`npx @aashari/mcp-server-atlassian-confluence get --path /...`.
The Go port **does not include a CLI** at v1 — the use case
is MCP-over-stdio and a CLI is a v1.1 nice-to-have (gap
**Q9**).

### Debug logging

The upstream writes debug logs to
`~/.mcp/data/@aashari-mcp-server-atlassian-confluence.<session-id>.log`
when `DEBUG=true`. The Go port writes debug logs to **stderr**
in JSON-lines format when `DEBUG=true` — no log file, no
session-id suffix. The stderr-only invariant is documented in
`09-anti-patterns/01-stdout-pollution.md`.

### What we explicitly do NOT port

| Component | Why not |
| --------- | ------- |
| CLI mode (`src/cli/`) | Out of scope for v1 (gap Q9) |
| `~/.mcp/configs.json` YAML loader | Hermes passes env directly; the configs.json loader exists so the upstream works without env. The Go port inherits Hermes' env-passthrough shape + adds a `.env` file fallback (per Q22 lock). |
| Express / StreamableHTTP transport | v1 is stdio-only (gap Q3) |
| TOON encoder source | Re-implemented in `internal/toon/` — license-clean reimplementation following the TOON spec |
| Zod schemas | Replaced with Go structs + `jsonschema` tags (per `mcp-golang` pattern) |

## Verification

A reader of this spec should be able to:

1. Confirm the upstream's `package.json` declares version
   `3.3.0` and MIT license.
2. Walk the upstream source tree (via the GitHub web UI) and
   verify the file names listed above exist.
3. Skim `src/index.ts` and
   `src/controllers/atlassian.api.controller.ts` and see the
   3-layer flow described.
4. Run the upstream server locally (`npx -y
   @aashari/mcp-server-atlassian-confluence`) and confirm the
   five-tool surface via `hermes mcp test`.