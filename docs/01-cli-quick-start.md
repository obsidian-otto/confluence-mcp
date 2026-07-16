# 01 — CLI quick start

> Goal: get from a fresh clone to a working `--help` in under
> five minutes, then have a working stdio and serve subcommand by
> the end of the page.

## Prerequisites

| Tool | Minimum | Check |
| ---- | ------- | ----- |
| Go | 1.23+ (per `go.mod`'s `go 1.26.4`) | `go version` |
| Make | GNU Make 4.x | `make --version` |
| GNU coreutils | `cat`, `env`, `true` | built-in |
| (for the distroless image) `pack` + `docker` | 0.40 + 20.10 | `make verify-tools` |

The binary is built with `CGO_ENABLED=0` so no libc or system
shared libraries are needed at runtime — the binary runs from
anywhere on the filesystem.

## Step 1 — build the binary

```sh
make build
```

This produces `./bin/mcp-confluence` (a single static Go binary,
~11 MB on linux/amd64 today).

> **Don't** run `go build` or `go test` directly — the Makefile is
> the single source of truth for builds and tests (ProjectLock +
> user's locked Q14).

## Step 2 — read the help

```sh
./bin/mcp-confluence --help
```

Expected output (you should see something close to this — exact
text depends on the cobra-generated templates):

```
Confluence MCP server (stdio JSON-RPC + TCP/HTTP transports)

Usage:
  mcp-confluence [command] [flags]

Commands:
  stdio        Serve the 18 MCP tools over JSON-RPC on stdin/stdout (default)
  serve        Serve the 18 MCP tools over TCP/HTTP JSON-RPC
  help         Help about any command

Flags:
      --site string       Confluence site prefix (overrides ATLASSIAN_SITE_NAME)
      --email string      Atlassian account email (overrides ATLASSIAN_USER_EMAIL)
      --api-token string  Atlassian API token (overrides ATLASSIAN_API_TOKEN)
      --debug             Enable verbose stderr logging
      --config string     Path to a viper-compatible config file
  -h, --help              help for mcp-confluence
  -v, --version           version for mcp-confluence

Run "mcp-confluence [command] --help" for command-specific help.
```

> If the help text appears on **stderr** (not stdout) you're
> seeing the right behavior. If it appears on stdout, the
> `rootCmd.SetOut(io.Discard) + SetErr(os.Stderr)` discipline
> has regressed — report it.

## Step 3 — read each subcommand's help

```sh
./bin/mcp-confluence stdio --help
./bin/mcp-confluence serve --help
./bin/mcp-confluence --version
```

Each subcommand's `--help` text contains an **MCP HOST REGISTRATION**
YAML block. That's the canonical example your MCP-host config
should be based on.

## Step 4 — set up credentials

Create `.env` from the template:

```sh
cp .env.example .env
$EDITOR .env
```

Two required fields:

```sh
ATLASSIAN_SITE_NAME=your-workspace          # e.g. smartergroup
ATLASSIAN_USER_EMAIL=you@example.com
ATLASSIAN_API_TOKEN=ATATT3xFfGF0...        # https://id.atlassian.com/manage-profile/security/api-tokens
```

(`DEBUG` is optional; `true` enables per-call stderr logging.)

Per locked Q22, settings are resolved in priority order:

1. CLI flags (e.g. `--site=... --api-token=...`)
2. **Process environment** (the three vars above plus `DEBUG`)
3. `.env` in the current working directory
4. `.env` next to the binary

See [04-configuration-reference.md](04-configuration-reference.md) for the full
flag/env/config file matrix.

## Step 5 — try the stdio subcommand

```sh
echo '{"jsonrpc":"2.0","method":"tools/list","id":1}' | ./bin/mcp-confluence stdio
```

You should see a JSON-RPC response on stdout listing all 18
tools:

```json
{"id":1,"jsonrpc":"2.0","result":{"tools":[{"name":"conf_get",...},...]}}
```

The startup banner (`mcp-confluence v0.1.0 starting (site=…, email=…)`)
goes to **stderr** — that's intentional. Stderr carries lifecycle +
per-call logs; stdout is reserved for the JSON-RPC byte stream
that any MCP host pipes into the process.

## Step 6 — try the serve subcommand

```sh
./bin/mcp-confluence serve --listen=127.0.0.1:8080 &
SERVED=$!
sleep 1

curl -X POST http://127.0.0.1:8080/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","method":"tools/list","id":1}'
```

You should see the same 18 tools returned as a JSON document.
Hit Ctrl-C (or `kill $SERVED`) when done; the binary shuts down
gracefully.

> **Same JSON-RPC envelope, different framing.** Stdio returns
> `{"id":1,"jsonrpc":"2.0","result":{...}}` separated by `\n`;
> HTTP returns it as the response body with
> `Content-Type: application/json`. The tool surface, the
> TOON-encoded response, the error envelope — all byte-identical.

## Step 7 — wire it into your MCP host

Most MCP hosts read a YAML/JSON config to know how to spawn the
binary. The canonical example for each major host lives in
[03-ai-agent-config.md](03-ai-agent-config.md). The minimum is:

```yaml
mcp_servers:
  confluence:
    command: /path/to/bin/mcp-confluence
    args: ["stdio"]                 # or ["serve", "--listen=127.0.0.1:8080"]
    env:
      ATLASSIAN_SITE_NAME: ${WORKSPACE_SITE}
      ATLASSIAN_USER_EMAIL: ${WORKSPACE_EMAIL}
      ATLASSIAN_API_TOKEN: ${WORKSPACE_API_TOKEN}
```

The MCP HOST REGISTRATION block in each subcommand's `--help`
text produces this YAML verbatim.

## Next

- For Bash / Python automation → [02-automation-scripts.md](02-automation-scripts.md)
- For Hermes / Claude Desktop / Cursor configs → [03-ai-agent-config.md](03-ai-agent-config.md)
- For the full flag + env + config-file reference → [04-configuration-reference.md](04-configuration-reference.md)
- For the design rationale → [05-architecture-decisions.md](05-architecture-decisions.md)
- For what to do when it doesn't work → [06-troubleshooting.md](06-troubleshooting.md)
