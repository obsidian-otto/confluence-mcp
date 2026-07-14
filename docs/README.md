# mcp-confluence — `docs/`

> **This is the operator-facing handbook for the `mcp-confluence`
> CLI app.** The project overview lives at the top-level
> [README.md](../README.md); the canonical reference for the 18
> Confluence MCP tools lives in
> [AGENTS.md](../AGENTS.md); the spec set and design documents live
> in [specs/](../specs/).

This folder is the **run-time + integration docs**, not the design
docs. Six files, indexed below.

| File | Read when… |
| ---- | ---------- |
| [01-cli-quick-start.md](01-cli-quick-start.md) | you want to run `./bin/mcp-confluence --help` and see what actually happens |
| [02-automation-scripts.md](02-automation-scripts.md) | you're writing a Bash / Python / Make / CI script that drives the binary over JSON-RPC |
| [03-ai-agent-config.md](03-ai-agent-config.md) | you're wiring the binary into Hermes / Claude Desktop / Cursor / any MCP-host config |
| [04-configuration-reference.md](04-configuration-reference.md) | you need the full flag + env-var + config-file reference (per locked Q22) |
| [05-architecture-decisions.md](05-architecture-decisions.md) | you want to know WHY the design works the way it does (stdio vs HTTP, Q22 composition path, locked invariants) |
| [06-troubleshooting.md](06-troubleshooting.md) | something doesn't work and you want a fast diagnostic walkthrough |

## TL;DR for the impatient

```sh
# Build (Makefile is the only source of build commands)
make build

# Read help — note the four subcommands
./bin/mcp-confluence --help

# stdio mode (the canonical Hermes integration)
./bin/mcp-confluence stdio

# serve mode (TCP/HTTP — for `curl` smoke tests + LAN MCP hosts)
./bin/mcp-confluence serve --listen=127.0.0.1:8080 &
curl http://127.0.0.1:8080/mcp -d '{"jsonrpc":"2.0","method":"tools/list","id":1}'
```

See [01-cli-quick-start.md](01-cli-quick-start.md) for the full walkthrough.

## How these docs relate to the rest of the project

- **Project overview / what it is** → [README.md](../README.md)
- **Tool surface / hard rules / architecture (one-paragraph)** → [AGENTS.md](../AGENTS.md)
- **Phase-by-phase implementation log** → [IMPLEMENTATION_PLAN.md](../IMPLEMENTATION_PLAN.md)
- **Spec set (design rationale, 14 topic folders)** → [specs/](../specs/)
- **Operator handbook (this folder)**

If you only have 5 minutes: read this doc, then [01-cli-quick-start.md](01-cli-quick-start.md), then [03-ai-agent-config.md](03-ai-agent-config.md) — that's everything you need to drive the binary.
