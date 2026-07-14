# mcp-confluence

A Go CLI app that ships an MCP server. A single static
`mcp-confluence` binary exposes the **18 Confluence MCP tools**
(ported from `@aashari/mcp-server-atlassian-confluence` v3.3.0)
over either of two transports:

- **`stdio` (default)** — newline-delimited JSON-RPC over
  stdin/stdout. The canonical Hermes MCP-host integration.
- **`serve`** — HTTP `POST /mcp` with the JSON-RPC envelope as
  the body. LAN / dev-container / curl-friendly; bind via
  `--listen=127.0.0.1:8080`.

Built on `ctreminiom/go-atlassian/v2` for HTTP, `metoro-io/mcp-golang`
for MCP framing, `spf13/cobra` + `spf13/viper` for the CLI. A
**dev-velocity rationale drives both transports**: you can
iterate tool handlers in `internal/tools/`, rebuild, and
immediately smoke via `curl` against `serve` — without
restarting Hermes. Hermes registration is the final
integration smoke, not the primary dev loop.

## Quick start

```sh
make build                  # build the binary
./bin/mcp-confluence --help  # read the auto-generated help
cp .env.example .env        # set credentials (see docs/04-…)
$EDITOR .env
./bin/mcp-confluence stdio   # run as a stdio MCP server
./bin/mcp-confluence serve --listen=127.0.0.1:8080  # or as TCP/HTTP
```

- Each of the 18 MCP tools is also exposed as a first-class
  cobra subcommand — invoke directly from the shell
  (`./bin/mcp-confluence get --path=...` → TOON-encoded
  Confluence data on stdout). The v6 rename drops the `conf_`
  prefix from the per-tool subcommand names; the underlying
  MCP tool names (e.g. `mcp__confluence_conf_get`) are
  frozen. Full table in
  [AGENTS.md §"Per-tool subcommands (v6)"](AGENTS.md).

See [docs/01-cli-quick-start.md](docs/01-cli-quick-start.md)
for the full walkthrough, [docs/02-automation-scripts.md](docs/02-automation-scripts.md)
for shell/Python examples, and [docs/03-ai-agent-config.md](docs/03-ai-agent-config.md)
for the Hermes / Claude Desktop / Cursor MCP-host configurations.

## Tool surface

| Group | Count | Examples |
| ----- | ----- | -------- |
| CRUD (upstream parity) | 5 | `conf_get`, `conf_post`, `conf_put`, `conf_patch`, `conf_delete` |
| Convenience helpers | 6 | `conf_list_spaces`, `conf_list_pages`, `conf_get_page_body`, `conf_get_page_tree`, `conf_search`, `conf_help` |
| Markdown round-trip | 3 | `conf_post_markdown`, `conf_put_markdown`, `conf_get_page_markdown` |
| Attachments | 3 | `conf_upload_attachment`, `conf_list_attachments`, `conf_delete_attachment` |
| drawio | 1 | `conf_upload_drawio` |

Full surface: `mcp_confluence_<name>` after Hermes' server
prefix. See [AGENTS.md §"## Purpose"](AGENTS.md) for the
per-tool description matrix, or run
`mcp__confluence__conf_help` from your agent.

## Configuration resolution order (locked Q22)

Settings resolve highest-priority first:

1. **CLI flags** — `--site`, `--email`, `--api-token`,
   `--debug`, `--config=…`
2. **Process environment** — `ATLASSIAN_SITE_NAME`,
   `ATLASSIAN_USER_EMAIL`, `ATLASSIAN_API_TOKEN`, `DEBUG`
3. **`.env` in cwd**
4. **`.env` next to the binary**

A viper-compatible config file (YAML/JSON/TOML/INI) loaded via
`--config` participates at tier ~2.5 (on top of env, below
flag). Full matrix: [docs/04-configuration-reference.md](docs/04-configuration-reference.md).

The `.env` parser is a 30-LOC stdlib function
(`internal/config/dotenv.go`) covered by 171 lines of tests —
no godotenv dependency. **The API token is never logged.**
`make verify-env` prints its length, not its value.

## Architecture (one-paragraph)

The binary is a thin, JSON-aware wrapper over Confluence
Cloud REST v1 + v2. The 18 MCP tools each register through
`tools.RegisterAll(srv, client)` — registration is the only
place where adapter closures live, and the same registration
covers both transports. Every handler delegates to a 9-step
`executeRequest` pipeline (URL build → call → JSON decode →
JMESPath filter → TOON encode → 40k truncation → typed
APIError wrap → panic recovery via `safeHandler`). Settings
resolve env > `.env` > binary-dir `.env`; lifecycle lives in
`cmd/mcp-confluence/main.go` and dispatches via a cobra root
command to `stdio` (run the existing `runLifecycle`) or `serve`
(wrap the same `mcp.Server` in `internal/transport/http/`).
The container image is built with `pack build` against
`paketobuildpacks/builder-jammy-tiny`, producing a distroless
run image that contains the single `mcp-confluence` static
binary.

Full architecture: [AGENTS.md §"## Architecture"](AGENTS.md).
Design rationale: [docs/05-architecture-decisions.md](docs/05-architecture-decisions.md).

## Project layout

```
confluence-mcp/
├── AGENTS.md          # canonical at-a-glance reference
├── README.md          # this file (project overview)
├── IMPLEMENTATION_PLAN.md   # 16-phase delivery log + Phases 16-19 (v4 CLI refactor)
├── Makefile           # single source of truth (22 targets)
├── .env.example       # template (commit; copy to .env locally)
├── docs/              # operator handbook (NEW — see docs/README.md)
├── cmd/mcp-confluence/       # CLI entrypoint
├── internal/
│   ├── config/        # LoadFromEnv() + stdlib dotenv.go
│   ├── atlassian/     # raw HTTP client + APIError
│   ├── jmespath/      # `jq` filter wrapper
│   ├── toon/          # TOON encoder
│   ├── markdown/      # markdown ↔ storage XHTML
│   ├── templates/     # URL/path template helpers
│   ├── drawio/        # drawio PNG encoding
│   ├── server/        # mcp.Server factory + RegisterAll
│   ├── transport/     # HTTP transport for `serve` subcommand
│   └── tools/         # 18 tool handlers + executeRequest pipeline
└── specs/             # 14 topic folders, 30+ files (Variant B shape)
```

## Where to read more

- **You want to run the binary** → [docs/01-cli-quick-start.md](docs/01-cli-quick-start.md)
- **You want to script against it** → [docs/02-automation-scripts.md](docs/02-automation-scripts.md)
- **You want to wire it into an MCP host** → [docs/03-ai-agent-config.md](docs/03-ai-agent-config.md)
- **You want the full config matrix** → [docs/04-configuration-reference.md](docs/04-configuration-reference.md)
- **You want to know WHY the design works this way** → [docs/05-architecture-decisions.md](docs/05-architecture-decisions.md)
- **Something doesn't work** → [docs/06-troubleshooting.md](docs/06-troubleshooting.md)
- **You want the canonical hard-rules and tool inventory** → [AGENTS.md](AGENTS.md)
- **You want the implementation phase log** → [IMPLEMENTATION_PLAN.md](IMPLEMENTATION_PLAN.md)
- **You want to read the spec set** → [specs/](specs/) (14 folders)
- **You want to contribute** → [CONTRIBUTING.md (planned)](.github/CONTRIBUTING.md) — not yet, see [docs/05-architecture-decisions.md](docs/05-architecture-decisions.md) for the load-bearing invariants first.

## License

MIT — see [LICENSE](LICENSE).

## Acknowledgements

- Upstream tool surface: [@aashari/mcp-server-atlassian-confluence](https://github.com/aashari/mcp-server-atlassian-confluence) v3.3.0
- Atlassian HTTP client: [ctreminiom/go-atlassian](https://github.com/ctreminiom/go-atlassian) v2
- MCP framework: [metoro-io/mcp-golang](https://github.com/metoro-io/mcp-golang) v0.16.1
- CLI: [spf13/cobra](https://github.com/spf13/cobra) + [spf13/viper](https://github.com/spf13/viper)
- Markdown ↔ storage: [yuin/goldmark](https://github.com/yuin/goldmark), [JohannesKaufmann/html-to-markdown](https://github.com/JohannesKaufmann/html-to-markdown)
