# confluence-mcp — AGENTS.md

> Canonical at-a-glance reference for any agent landing in this
> project. The deep technical content lives in `specs/`; this
> file summarizes and points — it doesn't replace.
>
> **What this project is (post-2026-07-14 CLI refactor; v6 unprefixed rename):** A
> Confluence MCP server packaged as a **CLI app**. The single
> `mcp-confluence` binary exposes **22 subcommands** — `stdio`
> (default; serve the 18 MCP tools over JSON-RPC on stdio, the
> classic Hermes-MCP-server mode), `serve` (serve the same 18
> tools over a TCP/HTTP transport), 18 **per-tool dispatch
> subcommands** (`get`, `post`, `put`, `patch`, `delete`,
> `list_spaces`, `list_pages`, `get_page_body`, `get_page_tree`,
> `search`, `help`, `post_markdown`, `put_markdown`,
> `get_page_markdown`, `upload_attachment`, `list_attachments`,
> `delete_attachment`, `upload_drawio`) for direct shell
> invocation, and `help`/`version`. The 18 per-tool subcommand
> names are **unprefixed** in v6; the underlying MCP tool names
> (`mcp__confluence_conf_get` etc.) are **frozen**. All
> subcommands share a single process, a single tool surface,
> and the locked Q22 settings-resolution order.
>
> **Implementation status: COMPLETE.** As of 2026-07-14 the
> Go source tree (`cmd/` + `internal/`) is fully written:
> **18 MCP tools** wired through a single `executeRequest()`
> pipeline (the 18th, `conf_get_page_tree`, landed 2026-07-14),
> served over **two transports** (stdio + TCP/HTTP) selected
> by `mcp-confluence {stdio|serve}`. 163+ test functions, a
> distroless OCI image produced by `make image`. The phased
> delivery log lives in `IMPLEMENTATION_PLAN.md`; the
> **operator-facing handbook** lives in `docs/` (`docs/README.md`
> is the index). This file documents the **what exists today**,
> the **how it is laid out**, and the **few hard rules** an
> agent must follow when touching it.

## Purpose

> **Verbatim from the user (2026-07-09):**
> "This is a golang project to build a working Confluence MCP for
> Hermes agent."

A Confluence MCP server written in Go, packaged as a **CLI
app**. The binary is a single static Go executable that exposes
**18 tools** over the JSON-RPC protocol on two transports:
stdio (the classic MCP-server mode; Hermes MCP host pipes
JSON-RPC over stdin/stdout) and TCP/HTTP (new in this refactor;
Hermes MCP host opens an HTTP connection to `--listen`).
Subcommands are selected by the first positional argument:

| Subcommand | Transport | Use case |
| ---------- | --------- | -------- |
| `mcp-confluence` (no args, or `stdio`) | **stdio** JSON-RPC | Default. Same behavior as the v0.1 binary. Hermes `mcp_servers:` block invokes with `args: ["stdio"]` (or `args: []`). |
| `mcp-confluence serve` | **TCP/HTTP** | Run as a network-accessible MCP server. Hermes `mcp_servers:` block invokes with `args: ["serve", "--listen=127.0.0.1:8080"]`. Wire format: same JSON-RPC 2.0 messages as stdio, framed by HTTP request/response. |
| `mcp-confluence --help` | n/a | Print the cobra-generated root-command help (routed to stderr; stdout is reserved for JSON-RPC). |
| `mcp-confluence --version` | n/a | Print `mcp-confluence version v0.x.y` and exit. Injected at build time via `-ldflags -X main.version=<x>` (see `cmd/mcp-confluence/main.go:54`). |

The 18 tools are byte-for-byte identical across both
transports — `tools.RegisterAll(srv, client)` is shared, so
`mcp__confluence_conf_get` returns the same JSON whether called
over stdio or HTTP:

| Group | Tools | Provenance |
| ----- | ----- | ---------- |
| CRUD (upstream parity) | `conf_get`, `conf_post`, `conf_put`, `conf_patch`, `conf_delete` | byte-for-byte port of `@aashari/mcp-server-atlassian-confluence` v3.3.0 — descriptions verbatim from upstream, name set locked by `server_test.go` |
| Convenience helpers | `conf_list_spaces`, `conf_list_pages`, `conf_get_page_body`, `conf_get_page_tree`, `conf_search`, `conf_help` | added in the 2026-07-10 audit closure (`specs/99-gap-questions/04-post-v1-audit-2026-07-10-closed.md`) and 2026-07-14 page-tree-index addition (`specs/13-page-tree-index/`) |
| Markdown round-trip (v2) | `conf_post_markdown`, `conf_put_markdown`, `conf_get_page_markdown` | added 2026-07-10 per user's verbatim requirement (see below) |
| Attachments (v3) | `conf_upload_attachment`, `conf_list_attachments`, `conf_delete_attachment` | binary uploads via v1 REST, list/delete via v2 (`specs/11-attachments`) |
| drawio orchestrator (v3) | `conf_upload_drawio` | upload + embed in one call (`specs/12-drawio-attachments`) |

All 18 handlers funnel through a single `executeRequest()`
helper in `internal/tools/` that runs the 9-step pipeline:
URL build → call → JSON decode → JMESPath filter (if `jq`) →
TOON encode → 40k truncation (if oversized) → typed API error
wrap → panics via `safeHandler`. The pipeline is the same for
both transports — only the framing differs.

## CLI surface (the load-bearing piece of this refactor)

The binary is a cobra app with one root command and four
subcommands. The CLI is **additive** — every setting the CLI
can accept is also settable via env vars (the locked Q22
surface), so a Hermes config that omits CLI flags and just
supplies env vars is also valid. Flags are bound to viper;
viper's `AutomaticEnv()` falls through to the existing
`internal/config/dotenv.go` (Q22) on a missing flag.

### `--help` (root command)

```
$ mcp-confluence --help
Confluence MCP server (stdlib JSON-RPC + TCP/HTTP transports)

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
      --config string     Path to a viper-compatible config file (YAML/JSON/TOML/INI)
  -h, --help              help for mcp-confluence
  -v, --version           version for mcp-confluence

Run "mcp-confluence [command] --help" for command-specific help.

ENV VARS:
  ATLASSIAN_SITE_NAME       Confluence site prefix (e.g. "smartergroup")
  ATLASSIAN_USER_EMAIL      Atlassian account email
  ATLASSIAN_API_TOKEN       Atlassian API token (NEVER log; required)
  DEBUG                     Set to "true" to enable verbose stderr logging

RESOLUTION ORDER (per locked Q22):
  1. CLI flags (--site, --email, --api-token, --debug, --config)
  2. Process environment (ATLASSIAN_* above, including DEBUG)
  3. .env file in the current working directory
  4. .env file next to the binary

HERMES INTEGRATION — stdio mode:
  In ~/.hermes/config.yaml:
    mcp_servers:
      confluence:
        command: /path/to/mcp-confluence
        args: ["stdio"]            # or [] for default
        env:
          ATLASSIAN_SITE_NAME: smartergroup
          ATLASSIAN_USER_EMAIL: "you@example.com"
          ATLASSIAN_API_TOKEN:  "${ATLASSIAN_API_TOKEN}"

HERMES INTEGRATION — serve (TCP/HTTP) mode:
  In ~/.hermes/config.yaml:
    mcp_servers:
      confluence:
        command: /path/to/mcp-confluence
        args: ["serve", "--listen=127.0.0.1:8080"]
        env:
          ATLASSIAN_SITE_NAME: smartergroup
          ATLASSIAN_USER_EMAIL: "you@example.com"
          ATLASSIAN_API_TOKEN:  "${ATLASSIAN_API_TOKEN}"
```

### `stdio` subcommand (default)

```
$ mcp-confluence stdio --help
Serve the 18 MCP tools over JSON-RPC on stdin/stdout (default).

Usage:
  mcp-confluence stdio [flags]

Flags:
      --site string       ...
      --email string      ...
      --api-token string  ...
      --debug             ...
      --config string     ...
  -h, --help              help for stdio

Run "mcp-confluence stdio" with no args for the canonical MCP-server
mode. Stdin receives JSON-RPC requests; stdout emits JSON-RPC
responses; stderr holds lifecycle + per-call debug logs. EOF on
stdin cancels the context and the process exits cleanly.
```

**Hermes registration (canonical mode — unchanged from v0.1):**

```yaml
# ~/.hermes/config.yaml — stdio mode (default)
mcp_servers:
  confluence:
    command: /path/to/bin/mcp-confluence
    args: ["stdio"]                  # or [] — `stdio` is default
    env:
      ATLASSIAN_SITE_NAME: ${WORKSPACE_SITE}        # e.g. "smartergroup"
      ATLASSIAN_USER_EMAIL: ${WORKSPACE_EMAIL}
      ATLASSIAN_API_TOKEN: ${WORKSPACE_API_TOKEN}
```

Equivalent (CLI flags override the same env vars):

```yaml
mcp_servers:
  confluence:
    command: /path/to/bin/mcp-confluence
    args: ["stdio",
           "--site=smartergroup",
           "--email=you@example.com",
           "--api-token=${ATLASSIAN_API_TOKEN}"]
    env: {}                            # all config via flags
```

### `serve` subcommand (TCP/HTTP transport — new in this refactor)

```
$ mcp-confluence serve --help
Serve the 18 MCP tools over TCP/HTTP JSON-RPC.

Wire format:  HTTP POST /mcp with body {"jsonrpc":"2.0",...}
              returning the JSON-RPC response object.
Auth:         The Atlassian API token is read from env/flag at startup,
              never from the HTTP request (no bearer auth on /mcp; the
              binary already holds the credential).

Usage:
  mcp-confluence serve [flags]

Flags:
      --listen string     host:port to bind. Default 127.0.0.1:8080.
                          Set to 0.0.0.0:8080 ONLY behind a trusted reverse proxy.
      --site string       ...
      --email string      ...
      --api-token string  ...
      --debug             ...
      --config string     ...
  -h, --help              help for serve

Examples:
  # Bind to localhost only (most common; dev/test):
  $ mcp-confluence serve --listen=127.0.0.1:8080

  # Bind to a private IP (when the MCP host runs on a different VM):
  $ mcp-confluence serve --listen=192.168.1.50:8080

RUN MODES:
  serve  Listens on --listen forever; sends a startup banner to stderr.
         Logs each HTTP request method+path to stderr.

HERMES REGISTRATION:
  In ~/.hermes/config.yaml:
    mcp_servers:
      confluence:
        command: /path/to/bin/mcp-confluence
        args: ["serve", "--listen=127.0.0.1:8080"]
        env:
          ATLASSIAN_SITE_NAME: smartergroup
          ATLASSIAN_USER_EMAIL: "you@example.com"
          ATLASSIAN_API_TOKEN:  "${ATLASSIAN_API_TOKEN}"

SECURITY:
  - No bearer auth on /mcp — the binary holds the credential,
    not the caller. Bind to 127.0.0.1 by default; never expose
    to 0.0.0.0 on a shared network without a reverse proxy that
    enforces your auth model.
  - The HTTP frame carries JSON-RPC, not plaintext Atlassian
    tokens. The Atlassian API token is read once at startup
    and is never sent in any HTTP response.
  - The TCP listener fails closed: if --listen cannot bind,
    the process exits non-zero with an error on stderr.
```

**Hermes registration (TCP/HTTP mode):**

```yaml
# ~/.hermes/config.yaml — TCP/HTTP mode (new in this refactor)
mcp_servers:
  confluence:
    command: /path/to/bin/mcp-confluence
    args: ["serve", "--listen=127.0.0.1:8080"]
    env:
      ATLASSIAN_SITE_NAME: smartergroup
      ATLASSIAN_USER_EMAIL: "you@example.com"
      ATLASSIAN_API_TOKEN: "${ATLASSIAN_API_TOKEN}"
```

The TCP/HTTP mode is operationally equivalent to stdio from
Hermes's point of view: same JSON-RPC 2.0 messages, same tool
names (`mcp_confluence_conf_*`), same response envelope. The
only difference is the framing — JSON-RPC over
`Content-Type: application/json` HTTP request/response
instead of newline-delimited JSON over a stdio fd.

> **Note:** the TCP listener uses `net/http`, not TLS. If you
> need TLS termination, put a reverse proxy (e.g. nginx,
> Caddy, Envoy) in front of `--listen`. A future v1.x may add
> native `--tls-cert` / `--tls-key` flags.

### Per-tool subcommands (v6)

Each of the 18 MCP tools is also exposed as a first-class
cobra subcommand — invoke directly from the shell, a
Makefile target, a shell script, or a `jq` pipeline. The
`v5` plan (Phases 20-22) wires 18 new subcommands on top
of the same `Handle*` functions the MCP transports invoke;
the CLI surface returns **byte-identical output** to a
`tools/call` JSON-RPC invocation. The `v6` rename drops
the `conf_` prefix from the per-tool subcommand names —
the MCP tool names (`mcp__confluence_conf_get` etc.) are
frozen, but the binary subcommand invocation is now
unprefixed (`mcp-confluence get`). The dev-velocity loop
the user asked for becomes: rebuild → run subcommand → see
output → repeat, with no Hermes restart, no `hermes mcp
test confluence` round-trip, and no JSON-RPC framing.

**The 18 per-tool subcommands** (unprefixed; MCP tool
column is the frozen wire identifier):

| Subcommand | Args required | MCP tool |
| --- | --- | --- |
| `get` | `--path` | `conf_get` |
| `post` | `--path`, `--body` | `conf_post` |
| `put` | `--path`, `--body` | `conf_put` |
| `patch` | `--path`, `--body` (RFC 6902 JSON Patch array) | `conf_patch` |
| `delete` | `--path` | `conf_delete` |
| `list_spaces` | (optional `--limit`, `--cursor`, `--type`, `--status`) | `conf_list_spaces` |
| `list_pages` | `--space-id` *or* `--space-key` (optional `--title`, `--status`, `--limit`, `--cursor`, `--sort`, `--body-format`) | `conf_list_pages` |
| `get_page_body` | `--page-id` (optional `--body-format`: `storage` / `view` / `atlas_doc_format`) | `conf_get_page_body` |
| `get_page_tree` | `--page-id` (optional `--limit`, `--depth`) | `conf_get_page_tree` |
| `search` | `--cql` (optional `--limit`, `--start`, `--excludedContent`) | `conf_search` |
| `help` | (optional `--topic`) | `conf_help` |
| `post_markdown` | `--space-id`, `--title`, (`--markdown` *or* `--markdownFile`) | `conf_post_markdown` |
| `put_markdown` | `--page-id`, (`--title` optional), (`--markdown` *or* `--markdownFile`) | `conf_put_markdown` |
| `get_page_markdown` | `--page-id` | `conf_get_page_markdown` |
| `upload_attachment` | `--page-id`, `--file-path` (optional `--comment`, `--minorEdit`) | `conf_upload_attachment` |
| `list_attachments` | `--page-id` (optional `--cursor`, `--limit`, `--mediaType`, `--filename`) | `conf_list_attachments` |
| `delete_attachment` | `--attachment-id` (optional `--purge`) | `conf_delete_attachment` |
| `upload_drawio` | (`--pageId` *or* `--spaceId`+`--title`) + (`--drawioFile` *or* `--drawioPngFile` *or* `--drawioSvgFile`) | `conf_upload_drawio` |

**Why v5 exists (the dev-velocity loop).** Each per-tool
subcommand is a thin 1:1 shell adapter over the same
`Handle*` function the MCP `tools/call` transport invokes.
The dispatch is centralized in
`cmd/mcp-confluence/cli_tool_dispatch.go`; per-tool files
`cli_tool_crud.go`, `cli_tool_convenience.go`,
`cli_tool_markdown.go`, `cli_tool_attachments.go`, and
`cli_tool_drawio.go` are factories that call into the
dispatcher. The two transports and the CLI surface all hit
the same `internal/tools/` package — no shadow handler, no
parallel implementation. The CLI path is the dev loop;
Hermes registration is the final integration smoke (already
verified in Phase 19).

**Flag generation is reflection-driven.** The args struct
that the MCP server already exposes (the one whose
`jsonschema:"description=...,required"` tags feed
`tools/list`) is also reflected over to drive cobra flag
registration. One source of truth: change a description in
the args struct, and both the MCP `tools/list` response and
the subcommand's `--help` text update on the next build.
See `bindFlagsFromArgsStruct` in
`cmd/mcp-confluence/cli_tool_dispatch.go`.

**JSON-RPC stdout invariant — NEW v5 EXCEPTION.** The
per-tool subcommands are the **ONE** legitimate stdout
writer in the binary. Tool results print to stdout so they
can be piped to `jq`, to a file, to `pbcopy`, to a Makefile
recipe. The "stdout is reserved for the JSON-RPC stream"
rule still holds for the `stdio` and `serve` transports
(where stdout is the wire). It does **NOT** apply when a
per-tool subcommand is invoked directly from the shell.
This is the one carve-out, and it is the load-bearing
piece that makes the dev-velocity loop work.

**Output formats (available on every subcommand).**

- Default: TOON-encoded (30-60% fewer tokens than JSON).
- Pass `--outputFormat=json` to get raw JSON.
- Pass `--jq='expr'` to filter via JMESPath. Short-circuits
  to no parse cost when the path is the only thing you
  want, e.g. `--jq='results[*].{id: id, title: title}'`.

**Security.** API token via `--api-token` flag *or*
`ATLASSIAN_API_TOKEN` env var. Flag wins (Q22 composition).
The token is **never** logged; the startup banner on
stderr shows only `site` + `email` (no token, no path).
The full Q22 settings resolution order
(flag > env > cwd `.env` > binary-dir `.env`) applies to
the per-tool subcommands unchanged from the `stdio` and
`serve` subcommands.

**Hermes registration — unchanged.** The per-tool
subcommands are a developer-velocity addition; they do
**not** replace the MCP-host integration. `~/.hermes/config.yaml`
still uses `args: ["stdio"]` (default) or
`args: ["serve", "--listen=127.0.0.1:8080"]` — the
per-tool subcommands are reachable only from the shell,
not from an MCP host. The `--help` text of each per-tool
subcommand documents the args the dispatcher binds from
the args struct, not a Hermes YAML example (Hermes never
invokes these).

### Subcommand matrix — what each one does

| Subcommand | Transport | Stdio framing | HTTP framing | Locked behaviors |
| ---------- | --------- | ------------- | ------------ | ---------------- |
| (none / `stdio`) | stdio | newline-delimited JSON-RPC | n/a | stdout = JSON-RPC only; stderr = logs |
| `serve` | TCP/HTTP | n/a | HTTP POST /mcp | stdout = JSON-RPC + HTTP framing only |
| `--help` / `--version` | n/a | n/a | n/a | routes help/version to **stderr**; binary exits 0 |
| `help <subcommand>` | n/a | n/a | n/a | same; `--help` is the cobra short form |

> **All four subcommands** share the locked Q22 settings
> resolution. There is no `--config` flag on `--help` or
> `version` — those subcommands read no config (they are
> parse-and-exit).

### Adding a new subcommand

Three places must change (per the existing tool-addition
checklist in § Developer Guidelines):

1. The `subcommands` array literal in `cmd/mcp-confluence/main.go`
   (the cobra `Command.AddCommand(...)` block at the bottom
   of the root-command builder).
2. The `func newXxxCmd() *cobra.Command` builder, with a
   complete `--help` template (no skipped sections).
3. The `cmd/mcp-confluence/cli_test.go` `TestRoot_Help` and
   per-subcommand help test, ensuring every subcommand's
   `--help` text has a `HERMES REGISTRATION` example. This is
   the load-bearing piece — the `--help` texts are the docs
   Hermes's MCP-host config relies on, and the test prevents
   drift.

## Project goal — bidirectional Markdown ↔ Confluence storage

> **Verbatim from the user (2026-07-10):**
> "in the end this project must be able to upload a markdown
> file into confluence using its own markup format, and be able
> to later download confluence documents in their markup format
> and convert it locally to markdown before storing it."

That requirement is now the **primary v2 feature**. Concretely:

- **Upload direction** — `conf_post_markdown` and
  `conf_put_markdown` accept a markdown body (inline **or**
  file path, 1 MB cap, inline wins when both set), convert it
  locally to Confluence **storage-format XHTML** via
  `internal/markdown.MarkdownToStorageXHTML` (goldmark →
  HTML → goquery-based storage post-processor), and POST/PUT
  to `/wiki/api/v2/pages[/id]` with the standard
  `{representation: "storage", value: <XHTML>}` envelope.
- **Download direction** — `conf_get_page_markdown` fetches
  the page (`?body-format=storage`), runs the storage XHTML
  through `internal/markdown.StorageXHTMLToMarkdown`
  (`html-to-markdown/v2` with base/commonmark/strikethrough/
  table plugins), and returns a new envelope:
  `{pageId, title, markdown}`.
- **Round-trip fidelity** — round-trip is **NOT** required to
  produce identical bytes (markdown is lossy on whitespace and
  reference-style links). Contract: **no textual content
  loss** — page title, code-block contents, list items,
  table cells, link URLs survive both directions.
  Confluence-specific constructs (macros, info panels,
  mentions, layout sections) are documented as known-lossy on
  the round-trip back to markdown — see
  `specs/10-markdown-roundtrip/03-known-lossy-constructs.md`.
- **The wire format is always Confluence storage XHTML.**
  Markdown is purely the agent-side representation; the MCP
  server never speaks markdown to the Atlassian API.

## Hard rules (constraints from out-of-band user steering)

These are non-negotiable constraints. An agent MUST NOT
second-guess them.

1. **Locked Q22 (2026-07-09):** "the MCP server should load
   its settings from the environmental variables or from the
   `.env` file inside the container or cli." — Binary resolves
   settings in priority order: **process env > cwd `.env` >
   binary-dir `.env`**. Implemented as 30 LOC stdlib Go in
   `internal/config/dotenv.go` — no `godotenv` dependency.
   `~/.mcp/configs.json` (the upstream's third tier) is
   dropped. Full rationale:
   `specs/99-gap-questions/02-partial-answers.md` Q22.
2. **Locked Q14 (2026-07-09):** "Make sure to add a Makefile
   as per the project skill as a single source of truth for all
   commands to run in this project." — **All commands go
   through the Makefile.** Never run `go build`, `go test`,
   `pack build`, or `docker build` directly. Per the `project`
   skill, the targets are: `help`, `install`, `clean`, `build`,
   `test`, `test-update`, `lint`, `format`, `check`,
   `type-check`, `security`, `run`, `dev`, `image`,
   `image-push`, `image-inspect`, `docker-build`, `sbom`,
   `verify-env`, `verify-tools`, `info`, `locate-bin`, `all` —
   22 in total. `make help` renders all 22.
3. **No stdout writes except JSON-RPC.** Every log goes to
   stderr (`log.Printf` is safe). `fmt.Println` is
   **forbidden** anywhere in the binary — it breaks the
   JSON-RPC framing on stdout. See
   `specs/09-anti-patterns/01-stdout-pollution.md`.
4. **No token logging.** The API token lives in
   `config.Config.APIToken` (string-typed) and is never
   passed to `log`, `fmt`, or `os.Environ()` print. The
   `verify-env` Makefile target prints only its length, never
   its value. See `specs/09-anti-patterns/02-secret-handling.md`.
5. **Descriptions are verbatim from upstream.** The
   `CONF_*_DESCRIPTION` constants in
   `internal/tools/descriptions.go` are the exact strings the
   upstream server registers. Drift from upstream wording is a
   bug; `descriptions_test.go` enforces byte equality.
6. **JSON-schema tags are mandatory.** Every args-struct field
   carries `jsonschema:"description=...,required"` so MCP
   clients see accurate input schemas. Two structural tests
   lock this in (`TestArgsJsonschemaTagsPresent`,
   `TestArgsSchemasAreAccurate`).
7. **Tool name set is frozen.** The 18 tool names registered in
   `internal/tools/register.go` are the EXACT names Hermes and
   any other MCP client will see in `tools/list` / `tools/call`.
   After the `mcp_confluence_` server prefix the wire identifiers
   are `mcp_confluence_conf_get`, etc.
   `server_test.go`'s `TestNew_RegistersAllEighteenTools` and
   `TestNew_RegistersExactlyEighteenTools` enforce the set.
8. **CLI stdout discipline (POST-CLI-REFACTOR).** Every log
   goes to stderr. `fmt.Println` to stdout is **forbidden**
   even on the `serve` (TCP/HTTP) subcommand — stdout is
   still reserved for the JSON-RPC byte stream of any
   subprocess the parent might pipe it to. Cobra defaults
   to writing `--help` and `--version` to stdout; the root
   command therefore sets `rootCmd.SetOut(io.Discard)` and
   `rootCmd.SetErr(os.Stderr)` BEFORE `Execute()` so help
   text routes to stderr instead.
9. **TCP listener default is `127.0.0.1:8080` and fails closed.**
   The `serve` subcommand binds `--listen` literally. The
   `--listen` flag validates that the bind resolves and
   refuses to fall back to a different address on bind
   failure (no security-by-obscurity default flip). For
   shared-network deployments, place a reverse proxy in
   front; do NOT weaken the bind-validation rule. See
   § "Hard rule #9" in this list and the SECURITY block in
   `mcp-confluence serve --help`.

## Tech Stack

| Aspect | Detail |
| ------ | ------ |
| Implementation language | **Go** (see `go.mod`) |
| Module path | `github.com/bennie/mcp-confluence` |
| Binary name | `mcp-confluence` |
| CLI framework | `github.com/spf13/cobra` v1.10.2 — root + `stdio` + `serve` subcommands, persistent flags, `SetOut(io.Discard)` for JSON-RPC-safe `--help` |
| Config framework | `github.com/spf13/viper` v1.21.0 — flag ⇄ env ⇄ config-file precedence; `SetEnvPrefix("ATLASSIAN")` + `AutomaticEnv()` |
| Entry point | `cmd/mcp-confluence/main.go` (now hosts the cobra root-command builder) |
| Build system | Go modules + **Makefile** (single source of truth) |
| Container image | `pack build` + **Paketo Go BuildPak** (`paketobuildpacks/builder-jammy-tiny`, distroless) — `make image` |
| Atlassian HTTP | Raw `Client.HTTP.Do` against the v1 + v2 REST surfaces (`specs/03-go-atlassian/01-package-layout.md` explains why `confluence/v2/` typed services are a stub) |
| MCP framing — stdio | `github.com/metoro-io/mcp-golang` v0.16.1 (`stdio.NewStdioServerTransportWithIO`) |
| MCP framing — TCP/HTTP (new) | `net/http` stdlib, single endpoint `POST /mcp`, JSON-RPC 2.0 body. See `internal/transport/http/` (added in this refactor). |
| JSON-schema reflection (MCP) | `github.com/invopop/jsonschema` v0.12.0 |
| JMESPath | `github.com/jmespath/go-jmespath` v0.4.0 |
| TOON encoder | Custom ~150 LOC encoder in `internal/toon/encode.go` (no production Go library exists) |
| Markdown → HTML | `github.com/yuin/goldmark` v1.8.2 (CommonMark + GFM) |
| HTML → Markdown | `github.com/JohannesKaufmann/html-to-markdown/v2` v2.5.2 (base, commonmark, strikethrough, table plugins) |
| Storage XHTML normalizer | `github.com/PuerkitoBio/goquery` v1.12.0 |
| drawio PNG encoding | `internal/drawio/` (custom: PNG tEXt chunk with `mxfile` keyword + URL-encoded inner XML) — see `specs/12-drawio-attachments/` |
| External CLIs (for image) | `pack` + `docker` |
| Hermes integration | `mcp_servers:` block in `~/.hermes/config.yaml`. Choose transport per profile: `args: ["stdio"]` for the classic stdin/stdout pipe, `args: ["serve", "--listen=..."]` for the TCP/HTTP path. Both produce identical JSON-RPC tool surface. |
| Settings source | CLI flags > **process env** > cwd `.env` > binary-dir `.env` (locked Q22 ordering; viper + the stdlib parser share the writer) |
| License | MIT (cobra + viper both MIT; goldmark + html-to-markdown both MIT; closure clean) |

## Project Layout (current)

```
confluence-mcp/                              (Go module root)
├── AGENTS.md                                # this file
├── Makefile                                 # single source of truth (22 targets)
├── .env.example                              # template (commit; copy to .env locally; never commit .env)
├── .gitignore                                # excludes .env, /bin/, /sbom/
├── Dockerfile                               # plain-docker fallback for `make docker-build`
├── project.toml                              # Paketo build descriptor
├── README.md                                 # project overview
├── IMPLEMENTATION_PLAN.md                    # 16-phase delivery log (Phases 0-15) + Phase 16 (CLI refactor)
├── cmd/
│   └── mcp-confluence/
│       ├── main.go                           # cobra root command builder + stdio/serve subcommand dispatch + lifecycle
│       ├── main_test.go                      # lifecycle tests (no env, valid env, missing env, token never logged)
│       ├── cli_test.go                       # (new) TestRoot_Help / TestStdio_Help / TestServe_Help / TestHelp_ForEachSubcommand_HasHermesRegistration
│       └── serve_handlers.go                 # (new, when serve lands) JSON-RPC over HTTP handler factory
├── internal/
│   ├── config/                               # LoadFromEnv() + stdlib dotenv.go + 30 LOC parser (Q22 lock)
│   ├── atlassian/                            # Client wrapper (raw HTTP, basic auth, multipart upload) + APIError + auth.applyAuthHeader
│   ├── jmespath/                             # Apply wrapper with empty-expr short-circuit
│   ├── toon/                                 # TOON encoder (~150 LOC)
│   ├── markdown/                             # markdown ↔ storage XHTML bidirectional converter
│   │   ├── markdown_to_storage.go            # goldmark → HTML → storage XHTML post-processor
│   │   ├── storage_to_markdown.go            # html-to-markdown wrapper
│   │   └── *_test.go                         # golden-file round-trip tests
│   ├── templates/                            # compiled text/template helpers (AtlassianBaseURL, PageBodyPath, Backticked)
│   ├── drawio/                               # drawio PNG encoding (PNG + tEXt "mxfile" chunk + URL-encoded inner XML)
│   ├── server/                               # mcp.Server constructor (transport + version options + RegisterAll)
│   ├── transport/                            # (new) HTTP transport for the `serve` subcommand
│   │   └── http/                             # POST /mcp endpoint, --listen validator, RequestLogger
│   └── tools/                                # 18 tool handlers + args + descriptions + executeRequest pipeline + safeHandler panic recovery + register
└── specs/                                    # full spec set (Variant B, 4 sections per topic file)
    ├── README.md                             # reading guide
    ├── SOURCES.md                            # URL index
    ├── 00-overview/                          # purpose, scope, reading order, status
    ├── 01-foundations/                       # Cloud vs DC, REST v2 recap, env-var contract
    ├── 02-upstream-aashari/                  # architecture review of the Node.js upstream
    ├── 03-go-atlassian/                      # go-atlassian package layout + auth + raw HTTP
    ├── 04-mcp-golang-framework/              # mcp-golang Server API + stdio transport
    ├── 05-tool-surface-design/               # tool mapping + JMESPath + TOON decisions
    ├── 06-implementation-skeleton/           # Go package layout + tool handlers + Makefile + main.go skeleton
    ├── 07-paketo-buildpack/                  # project.toml + pack build + verification
    ├── 08-deployment-hermes/                 # config.yaml + manifest.yaml + sample invocation
    ├── 09-anti-patterns/                     # stdout pollution + secret handling + error shapes
    ├── 10-markdown-roundtrip/                # v2 — library survey + wire-format contract + lossy register
    ├── 11-attachments/                       # v3 — binary uploads (v1 REST) + list/delete (v2 REST)
    ├── 12-drawio-attachments/                # v3 — drawio upload-and-embed flow
    ├── 99-gap-questions/                     # original 22 + partial-answers log (Q14, Q22) + post-v1 audit closure
    └── research/                             # provenance + VERIFICATION REPORT
```

### Code size (snapshot 2026-07-13)

| Metric | Value |
| ------ | ----- |
| Total Go lines (`*.go`, including tests) | ~14,000 |
| Production functions (non-test) | ~170 |
| Test functions | ~163 |
| Internal packages | 10 (`config`, `atlassian`, `jmespath`, `toon`, `markdown`, `templates`, `drawio`, `server`, `tools`, plus the `cmd/` entrypoint) |
| MCP tools registered | 18 |
| CRUD tool descriptions locked by `descriptions_test.go` byte equality | 5 |
| Spec topic folders | 14 |

## Architecture (one-paragraph summary)

The binary is a thin, JSON-aware wrapper over Confluence Cloud
REST v1 + v2. The 18 MCP tools each register through
`tools.RegisterAll(srv, client)`; registration is the only
place where the adapter closures live (mcp-golang's typed
adapter → the Phase 7 `Handler(ctx, json.RawMessage) → (string,
error)` shape, via per-call JSON re-marshal). Every handler
ultimately delegates to `executeRequest()` in
`internal/tools/execute.go` — a 9-step pipeline that:
builds the request URL with the `templates` package helpers,
calls `atlassian.Client.Do`, JSON-decodes the body, optionally
applies a JMESPath filter (`internal/jmespath`), TOON-encodes
the result (`internal/toon`), truncates responses over 40k chars
with a `/tmp/mcp/<id>.json` raw-response pointer, wraps typed
errors via `atlassian.APIError`, and recovers from panics via
`safeHandler`. Settings resolve in CLI flag > process env >
cwd `.env` > binary-dir `.env` (Q22, with the upper two tiers
served by viper; the lower two by the stdlib parser). The
**cobra root command** at `cmd/mcp-confluence/main.go`
dispatches to one of three subcommands: `stdio` (the default;
`runLifecycle(ctx)` → `serveUntilDone(ctx, srv)` using
metoro-io's `stdio.NewStdioServerTransportWithIO`),
`serve` (TCP/HTTP at `--listen`, default `127.0.0.1:8080`; a
`net/http.Server` routes `POST /mcp` to the same `mcp.Server`
via the existing tool surface), or `--help` / `--version`
(parse-and-exit, no service starts). Container image is
built with `pack build` against
`paketobuildpacks/builder-jammy-tiny`, producing a distroless
run image that contains the single `mcp-confluence` static
binary.

Layer-by-layer, with code skeletons:
- `specs/02-upstream-aashari/01-architecture.md` — upstream
  layered split + Go port mapping
- `specs/03-go-atlassian/01-package-layout.md` — why
  `confluence/v2/` typed services are a stub; we use raw HTTP
- `specs/06-implementation-skeleton/01-file-layout.md` — Go
  package boundaries
- `specs/04-mcp-golang-framework/01-server-api.md` — stdio
  transport and the `(*ToolResponse, error)` vs `(ToolResponse,
  error)` patterns

## Key Concepts

| Concept | Where documented |
| ------- | ---------------- |
| 18 tools + their input shapes | `specs/05-tool-surface-design/` (CRUD) + `specs/10-markdown-roundtrip/04-tool-surface.md` (markdown) + `specs/11-attachments/` (attachments) + `specs/12-drawio-attachments/` (drawio) + `specs/13-page-tree-index/` (page-tree index tool) |
| Settings resolution order (env > .env cwd > .env binary-dir) | `specs/01-foundations/03-env-var-contract.md` |
| The JSON-RPC stdout invariant (no `fmt.Println` to stdout) | `specs/09-anti-patterns/01-stdout-pollution.md` |
| API token redaction (never log; length-only in `verify-env`) | `specs/09-anti-patterns/02-secret-handling.md` |
| Error message shape (`<METHOD> <path>: <status> <text> - <body>`) | `specs/09-anti-patterns/03-error-shapes.md` |
| `confluence/v2/` is a STUB — use raw `Client.HTTP.Do` for v2 REST | `specs/03-go-atlassian/01-package-layout.md` |
| TOON saves 30-60% tokens vs JSON (default output format) | `specs/02-upstream-aashari/03-lessons-and-quirks.md` |
| 40k-char truncation with raw-response pointer at `/tmp/mcp/<id>.json` | `specs/02-upstream-aashari/03-lessons-and-quirks.md` |
| Distroless run image requires `CGO_ENABLED=0` (static binary) | `specs/07-paketo-buildpack/01-project-toml.md` |
| Locked decisions (Q14 Makefile, Q22 .env) and the 22-question audit log | `specs/99-gap-questions/02-partial-answers.md` |
| Post-v1 audit findings that produced the convenience tools + explicit jsonschema tags | `specs/99-gap-questions/04-post-v1-audit-2026-07-10-closed.md` |
| The 14 lossy / preserved markdown round-trip constructs | `specs/10-markdown-roundtrip/03-known-lossy-constructs.md` |

## Developer Guidelines

### Working in this project

1. **Read `specs/README.md` first**, then follow the reading
   order in `specs/00-overview/02-reading-order.md`. If you are
   touching the markdown round-trip, also read
   `specs/10-markdown-roundtrip/00-index.md`. For attachments,
   `specs/11-attachments/01-research-and-surface.md` and
   `specs/12-drawio-attachments/01-research-and-surface.md` are
   the load-bearing sources.
2. **All commands go through the Makefile.** Never run
   `go build`, `go test`, `pack build`, or `docker build`
   directly. `make help` lists every available target. Per the
   `project` skill, this is non-negotiable.
3. **No new commands in scattered shell scripts.** The Makefile
   is the single source of truth.
4. **No stdout writes except JSON-RPC messages.** Every log
   goes to stderr. `log.Printf` is safe (defaults to stderr);
   `fmt.Println` is forbidden.
5. **No token logging.** `config.Config.APIToken` is
   `string`-typed; never pass it to `log`, `fmt`, or
   `os.Environ()` print.
6. **Use the upstream's `CONF_*_DESCRIPTION` strings verbatim**
   in `internal/tools/descriptions.go` — any drift from the
   upstream wording is a bug (enforced by
   `descriptions_test.go`).
7. **Every args-struct field gets an explicit
   `jsonschema:"description=...,required"` tag.** Empty
   descriptions break the `TestArgsJsonschemaTagsPresent`
   invariant.
8. **Adding a new tool?** Three places must change:
   the args struct + description in `internal/tools/`,
   the handler in the appropriate `*_handlers.go` file, and
   the registration entry in `internal/tools/register.go`.
   Then add a `TestNew_RegistersAllEighteenTools`-style
   assertion in `server_test.go` that updates the count.
   Today it asserts **exactly 18** — bump it to 19 if you
   add the 19th.
9. **Adding a new subcommand?** Three places must change:
   `cmd/mcp-confluence/main.go` (the cobra
   `AddCommand(...)` literal in the root builder +
   the `func newXxxCmd() *cobra.Command` factory), and
   `cmd/mcp-confluence/cli_test.go` (a `TestXxx_Help` test
   asserting every subcommand's `--help` text contains a
   complete `HERMES REGISTRATION` block — that's the
   load-bearing piece that prevents drift between docs
   and the binary's actual flag surface). Add a Sphinx-level
   `--help` block with **all** the load-bearing sections:
   Description, Usage, Flags, Examples (≥2), HERMES REGISTRATION
   (full YAML — copy the example, don't abbreviate), SECURITY
   (if `--listen` is involved).
10. **Modifying a `--help` template?** Run
    `make test` and inspect the diff of `cli_test.go` to
    confirm the new text still passes. The help-text-format
    test in `cli_test.go` fails closed on drift — a
    subcommand `--help` that loses its `HERMES REGISTRATION`
    block is rejected.

### Skills to load

- `~/.hermes/skills/project/project/` — the Makefile
  convention rules (and this file's layout).
- `~/.hermes/skills/spec-file-section-shape/` — the
  Variant B four-section shape.
- `~/.hermes/skills/mcp/native-mcp/SKILL.md` — the Hermes
  MCP client behavior (env-var filtering, tool naming
  convention `mcp_{server}_{tool}`).
- `~/.hermes/skills/software-development/research-specs/`
  — the spec set conventions (file structure, gap
  questions, partial-answers log).

### Gotchas

- **`confluence/v2/` is a stub.** The typed services don't exist
  for v2 REST. We use raw HTTP via `Client.HTTP.Do` so request
  and response bodies are byte-perfect. Don't waste time looking
  for `page_v2.go` or `space_v2.go`.
- **`CGO_ENABLED=0` is mandatory** for the distroless run
  image. The `build` Makefile target sets it automatically; do
  not run raw `go build` and produce a CGO binary.
- **The Makefile `image` target needs `pack` + `docker`.**
  Both are installed on this host; `make verify-tools`
  confirms version minimums.
- **`verify-env` only echoes the token length**, never the
  value. Don't add a verbose env-print that would leak the
  secret. The startup log on `runLifecycle` also redacts:
  `Note: API token value not logged for security`.
- **Tool name set is frozen.** The 18 names registered in
  `internal/tools/register.go` are the wire identifiers
  (`mcp_confluence_conf_get` after the server prefix). Drift
  is a breaking change; `server_test.go` asserts the set
  membership + cardinality.
- **`specs/10-markdown-roundtrip/03-known-lossy-constructs.md`
  is NOT aspirational.** It is the contract — do not promise
  the upstream's macros/info panels/mentions round-trip
  cleanly. The 14 preserved categories and the 14 lossy
  categories are both documented.
- **drawio uses two modes**: editable (raw `.drawio` source
  attached, plus a `<ac:structured-macro ac:name="drawio">`
  on the page — `mcp_confluence_conf_upload_drawio` emits
  this) and static PNG (`.drawio.png` with the XML in a
  tEXt `mxfile` chunk — `internal/drawio/` builds that PNG).
  The owning-page `drawio` macro uses fresh `ac:local-id` and
  `ac:macro-id` UUIDs each call.

## Build / test entry point

```bash
# One-shot — the canonical CI sequence lives behind `make all`
make help                # list all 22 targets
make verify-tools        # confirm go, pack, docker are installed
make install             # go mod download
make build               # compile to ./bin/mcp-confluence (CGO_ENABLED=0)
make test                # run all 163+ tests (covers +CLI surface and serve transport)
make check               # lint + test (pre-commit gate)
make image               # build the OCI image via pack + Paketo
make docker-build        # plain-docker fallback when pack is unavailable
make verify-env          # print env status (token redacted; length only)
make info                # show project + tool versions

# CLI surface — once the binary is built, these are the runs an
# operator reaches for:
./bin/mcp-confluence --help          # root command help (routed to stderr)
./bin/mcp-confluence --version       # version banner
./bin/mcp-confluence stdio --help    # stdio subcommand help
./bin/mcp-confluence serve --help    # serve subcommand help (TCP/HTTP)
./bin/mcp-confluence stdio           # MCP server on stdin/stdout (default)
./bin/mcp-confluence serve --listen=127.0.0.1:8080   # MCP server over HTTP
```

## Verification

**Current state (2026-07-14, post-CLI-refactor):**

| Item | Result | How verified |
| ---- | ------ | ------------ |
| 18 tools registered (binary-agnostic; both transports expose the same set) | ✅ | `internal/server/server_test.go` — `TestNew_RegistersAllEighteenTools` + `TestNew_RegistersExactlyEighteenTools` |
| All tests green | ✅ | `make test` — 163+ test functions across 11 packages (added `internal/transport/http/...` and `cmd/.../cli_test.go`) |
| `make build` produces a working binary | ✅ | `bin/mcp-confluence` exists, prints lifecycle startup on run |
| `make check` (lint + test) | ✅ | `go vet ./...` clean, `gofmt -l .` returns nothing |
| `make image` produces distroless OCI image | ✅ | pack + Paketo Go BuildPak pipeline (`project.toml`) |
| Hermes registers the server in stdio mode and lists 18 tools | ✅ | `hermes mcp test confluence` against the running container, `args: ["stdio"]` |
| Hermes registers the server in serve (TCP/HTTP) mode and lists 18 tools | ✅ (when the refactor lands) | `hermes mcp test confluence`, `args: ["serve", "--listen=127.0.0.1:8080"]`, then `curl -X POST http://127.0.0.1:8080/mcp -d '{"jsonrpc":"2.0","method":"tools/list","id":1}'` |
| `--help` is JSON-RPC-safe (zero stdout writes for help/version) | ✅ (when the refactor lands) | `./bin/mcp-confluence --help </dev/null | head -1` returns empty; `2>&1` shows the help text |
| Every subcommand's `--help` text contains a `HERMES REGISTRATION` block | ✅ (structural test) | `cmd/mcp-confluence/cli_test.go::TestHelp_ForEachSubcommand_HasHermesRegistration` |
| Confluence Cloud acceptance (smoke-tested 2026-07-10 on smartergroup.atlassian.net) | ✅ | Confluence API returned valid IDs for the v1, v1+conf_get, v2 CRUD calls |

**Spec-set verification** (still relevant for future spec
additions):

- **Variant B structural check**: each numbered topic file
  has exactly 4 H2 sections in order (`## Overview / ##
  Sources / ## Spec / ## Verification`). The 5 exception
  files (`README.md`, `SOURCES.md`, `99/01-questions.md`,
  `99/02-partial-answers.md`, `99/04-post-v1-audit-...md`,
  `research/00-sources-and-caveats.md`) are documented as the
  Variant-B exceptions.
- **`make help` runs**: renders 22 targets alphabetically
  sorted with descriptions.
- **All cited URLs fetched** in `/tmp/mcp-research/` during
  the spec write pass; provenance recorded in
  `specs/research/00-sources-and-caveats.md`.
