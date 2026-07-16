# 05 — Architecture decisions

> Goal: explain the WHY behind how `mcp-confluence` is shaped,
> so a future maintainer doesn't drift the architecture for the
> wrong reasons.

## Why two transports (stdio + TCP/HTTP)?

`mcp-confluence` exposes the **same 18 tools** on both:

- `stdio` — JSON-RPC over stdin/stdout, the MCP-host convention
- `serve` — JSON-RPC over `POST /mcp` HTTP, the dev-loop-friendly
  transport

### The dev-velocity rationale (verbatim user)

> *"having the mcp server as an cli app excercising the same code
> as the MCP server will speed up the development as I do not
> need to restart hermes every time for tests, but only for the
> MCP tests."*

Translating: when iterating on a tool handler in
`internal/tools/`, the dev loop used to be:

```
edit → make build → restart Hermes → her stdio → call tool
```

That's ~5-10 seconds of Hermes-restart cost per iteration.

With the CLI added, it becomes:

```
edit → make build → ./bin/mcp-confluence serve --listen=… &
curl http://127.0.0.1:8080/mcp -d '{"jsonrpc":"2.0",…}'
```

Hermes only restarts for the **MCP tests** — when you actually
verify the wire against the agent. The bulk of dev iterations
are `make build` + curl, with the agent round-trip as the
final smoke.

### Why two transports rather than "just HTTP"

HTTP is the dev-friendly choice; stdio is the canonical
integration choice. Forcing both onto HTTP would mean breaking
the MCP-host convention; forcing both onto stdio would mean
adding a Python-style JSON-RPC server in the same Go process
just to keep curl-style tests ergonomic.

Two transports at the dispatch layer with **the same
`mcp.Server` instance** is the minimum change that:

- preserves the existing Hermes integration (zero changes to
  `~/.hermes/config.yaml` other than maybe `args:`)
- gives the dev loop a curl-friendly HTTP surface
- shares all 18 tools, the JMESPath filter, the TOON encoder,
  the 40k-char truncation, the panic recovery, the error
  envelope between the two transports

See
[../specs/14-cobra-viper-golang/01-research-and-surface.md](../specs/14-cobra-viper-golang/01-research-and-surface.md)
§"What each library does" for the upstream-tooling survey
that informed this choice. Cross-reference: the four surveyed
MCP-server reference projects (`modelcontextprotocol/go-sdk`,
`mark3labs/mcp-go`, `metoro-io/mcp-golang`,
`metoro-io/metoro-mcp-server`) all chose stdio-only; we
deliberately add HTTP because the user's stated goal is dev
velocity.

## Why a CLI at all (cobra + viper)?

Three reasons:

1. **Subcommand separation of concerns** — `--help`,
   `--version`, `stdio`, `serve`, and the future surface
   (e.g. `version`, `doctor`) all share the same persistent
   flags (`--site`, `--email`, `--api-token`, `--debug`,
   `--config`). Cobra is the standard Go library for that
   shape; viper is the standard library for the
   flag ⇄ env ⇄ config-file precedence dance.
2. **Help text doubles as config documentation** — every
   subcommand's `--help` text contains an MCP HOST REGISTRATION
   YAML block that's copy-pasteable into any MCP host's server
   config (`mcp_servers:` for Hermes / Continue / VS Code;
   `mcpServers:` for Claude Desktop / Cursor). This is the
   load-bearing piece: the operator's docs and
   the agent's config are **the same file**.
3. **Dev-iteration speed** — typing
   `./bin/mcp-confluence serve --listen=127.0.0.1:8080` is one
   shell line; launching a Python MCP-host wrapper to do the
   same thing is ~50 LOC of glue.

### Why cobra (not stdlib `flag`, not kong, not urfave/cli)

`metoro-io/mcp-golang` already pulls `cobra` as a transitive
dep — adding cobra directly doesn't grow the binary's
import graph. cobra (vs stdlib `flag`):

- ✓ Generates help text the same way every Go program does
- ✓ Auto-adds `--help` and `--version` flags when the struct
  fields are present
- ✓ Subcommand tree maps naturally to MCP-host's `args: []`

Cobra + viper is the canonical Go CLI idiom (every 2024-2026
surveyed blog/source agrees; see the
[research/00-cobra-viper-idioms-2024-2026.md](../specs/14-cobra-viper-golang/research/00-cobra-viper-idioms-2024-2026.md)
for the 8-pattern summary).

## Why the JSON-RPC stdout invariant

Every byte that lands on stdout is one of:

- a JSON-RPC stdio message (in `stdio` mode)
- an HTTP response body (in `serve` mode, equivalent to a
  single-line JSON-RPC response)

That is **all**. Help text, version text, error text, log text —
all on stderr.

Two reasons for the strict separation:

1. **MCP-host wire compatibility.** Hermes / Claude Desktop /
   Cursor pipe the binary's stdout directly into a JSON-RPC
   parser. Any non-JSON-RPC byte on stdout (a startup banner,
   a log line, even a stray `\n`) breaks the parser silently —
   the JSON-RPC decoding fails, the agent gets a tool timeout,
   and the operator has no obvious cause.
2. **HTTP framing correctness.** In `serve` mode, the
   `net/http` response body is what the caller parses;
   anything that lands on stdout but not in the response body
   is invisible to curl. So the invariant is "stdout = JSON-RPC
   body" in both transports; logs go to stderr either way.

The cobra default of "help text writes to stdout" is overridden
in this project with `rootCmd.SetOut(io.Discard) + SetErr(os.Stderr)`
(see `cmd/mcp-confluence/main.go`). The override is tested
closed in `cli_test.go::TestRoot_Help_NoStdout`.

## Why the Q22 .env pre-order, not godotenv

The locked Q22 contract (see
[../specs/99-gap-questions/02-partial-answers.md](../specs/99-gap-questions/02-partial-answers.md)) is
"process env > cwd `.env` > binary-dir `.env`". The
implementation lives in `internal/config/dotenv.go` — a
30-LOC stdlib parser that runs **before** viper reads anything.

Why not switch to `github.com/joho/godotenv`:

- godotenv has its own implicit `.env` loading, which would
  conflict with the Q22 explicit path precedence.
- godotenv is one more direct dep for a 30-LOC stdlib
  function.
- The 171-line `internal/config/dotenv_test.go` is the
  project-best-tested utility module and is the canonical
  evidence the contract holds.

The CLI composition path (per
[02-design.md](../specs/14-cobra-viper-golang/02-design.md)
§"Reference implementation outline"): flags re-inject via
`os.Setenv("ATLASSIAN_SITE_NAME", site)` so the stdlib parser
sees flags at **tier 1** (process env) and continues to own
cwd/binary-dir `.env` at tiers 3-4. Q22 is preserved by
composition, not modification.

## Why the dependency tree (cobra + viper + metoro-io/mcp-golang only)

After the v4 refactor lands:

| Direct dep | Pin (target) | Use |
| ---------- | ------------ | --- |
| `github.com/spf13/cobra` | v1.10.2 | CLI parser + help text |
| `github.com/spf13/viper` | v1.21.0 | flag ⇄ env ⇄ config-file precedence |
| `github.com/invopop/jsonschema` | v0.12.0 | MCP `tools/list` schema reflection |
| `github.com/jmespath/go-jmespath` | v0.4.0 | `jq` filter |
| `github.com/yuin/goldmark` | v1.8.2 | markdown → storage XHTML |
| `github.com/JohannesKaufmann/html-to-markdown/v2` | v2.5.2 | storage XHTML → markdown |
| `github.com/PuerkitoBio/goquery` | v1.12.0 | storage-XHTML post-processor |
| `github.com/ctreminiom/go-atlassian/v2` | v2.12.0 | Atlassian HTTP (Confluence v1 + v2) |
| `github.com/metoro-io/mcp-golang` | v0.16.1 | MCP framing + tool registration |

What's NOT in the deps:

- ❌ `godotenv` (per locked Q22, stdlib parser is canonical)
- ❌ `yaml.v3` / `json-iterator` / etc. (stdlib)
- ❌ `gorilla/mux` or any router (the HTTP listener uses
  `net/http` stdlib; one POST /mcp endpoint = zero router
  configuration needed)
- ❌ TLS libraries (`crypto/tls` is stdlib; v4 ships with
  plaintext TCP only and lets a reverse proxy handle TLS)

The dependency closure is MIT-licensed throughout.

## Why the binary's name is `mcp-confluence` (not `confluence-mcp`)

Cosmetic, but consistent with the `mcp__confluence__*`
server-prefix convention that Hermes uses on the wire: the
binary is "the MCP server for Confluence", so the leading
`mcp-` aligns with how it's addressed through Hermes.

The repo directory (`confluence-mcp/`) and the Go module path
(`github.com/bennie/mcp-confluence`) follow the same pattern.

## Why a single bin, not multiple binaries

The 18 tools share the same `internal/tools/`, `internal/server/`,
`internal/atlassian/` packages. Splitting into multiple binaries
(e.g. `mcp-confluence-pages`, `mcp-confluence-attachments`) would:

- duplicate the connection pool, the auth, the dotenv parser
- force an operator to register 5+ `mcp_servers:` entries in
  `~/.hermes/config.yaml` instead of one
- break the cross-tool integration (e.g. `conf_upload_drawio`
  needs to call `conf_put` to update the page body)

The single-binary shape matches the upstream
`@aashari/mcp-server-atlassian-confluence` reference and the
standard MCP-server pattern.

## What "behaviour-preserving" means for the stdio subcommand

When Phase 17 lands, `mcp-confluence stdio` is verified to
produce byte-identical responses to the v0.1 binary running
with `args: []`. The verification uses
[scripts/smoke-page_tree.py](../scripts/smoke-page_tree.py)
and asserts the same four-key merged envelope returned.

The Q22 settings-resolution order is also preserved exactly —
flags funnel into `os.Setenv` so the stdlib parser sees them
at tier 1. The composition is invisible to `internal/config/`,
which never sees cobra or viper.

## Why two transports share a single `mcp.Server` instance

The HTTP handler in `internal/transport/http/` does **not**
construct a new `mcp.Server`; it receives the same instance
that `internal/server.NewWithTransport(deps, stdio.Transport)`
would build. Each HTTP request calls
`mcpSrv.Handle(ctx, raw) (raw, error)` — the same entry point
that `RegisterTool` populated in `internal/server/server.go`.

Consequence: every tool, every handler, every panic-recovery,
every JMESPath filter, every TOON encoder, every 40k-char
truncation — all run **once** at server-build time and apply
identically to both transports.

This is the architectural pattern that lets `make build`
followed by either `bin/mcp-confluence stdio` OR
`bin/mcp-confluence serve` produce the **same tool surface**.
No transport-specific code in `internal/tools/`, ever.
