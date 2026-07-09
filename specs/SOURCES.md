# Sources — confluence-mcp

Snapshot date: 2026-07-09. URLs verified live via `curl` to
`/tmp/mcp-research/` and direct GitHub API calls on that date
unless flagged otherwise.

## Anchor sources (read in full)

| URL | Read on | Type | Used in (spec file) |
| --- | ------- | ---- | ------------------- |
| https://github.com/aashari/mcp-server-atlassian-confluence | 2026-07-09 | Open-source repo — the upstream we are porting | all of `02-upstream-aashari/` |
| https://raw.githubusercontent.com/aashari/mcp-server-atlassian-confluence/main/README.md | 2026-07-09 | Upstream README (626 lines, fetched to `/tmp/mcp-research/aashari-README.md`) | `02-upstream-aashari/01-architecture.md`, `02-five-tools.md` |
| https://raw.githubusercontent.com/aashari/mcp-server-atlassian-confluence/main/package.json | 2026-07-09 | Upstream package metadata (version 3.3.0, MIT, Node 18+) | `02-upstream-aashari/01-architecture.md` |
| https://raw.githubusercontent.com/aashari/mcp-server-atlassian-confluence/main/src/index.ts | 2026-07-09 | Upstream entrypoint (221 lines) | `02-upstream-aashari/01-architecture.md` |
| https://raw.githubusercontent.com/aashari/mcp-server-atlassian-confluence/main/src/controllers/atlassian.api.controller.ts | 2026-07-09 | Upstream controller layer (158 lines) | `02-upstream-aashari/01-architecture.md` |
| https://raw.githubusercontent.com/aashari/mcp-server-atlassian-confluence/main/src/services/vendor.atlassian.api.service.ts | 2026-07-09 | Upstream service layer (261 lines) | `02-upstream-aashari/01-architecture.md` |
| https://raw.githubusercontent.com/aashari/mcp-server-atlassian-confluence/main/src/tools/atlassian.api.tool.ts | 2026-07-09 | Upstream tool registration (294 lines; the CONF_*_DESCRIPTION strings) | `02-upstream-aashari/02-five-tools.md`, `05-tool-surface-design/01-tool-mapping.md` |
| https://github.com/ctreminiom/go-atlassian | 2026-07-09 | Open-source repo — the Atlassian API client | all of `03-go-atlassian/` |
| https://raw.githubusercontent.com/ctreminiom/go-atlassian/main/README.md | 2026-07-09 | `go-atlassian` README (461 lines) | `03-go-atlassian/01-package-layout.md`, `02-auth-options.md` |
| https://raw.githubusercontent.com/ctreminiom/go-atlassian/main/go.mod | 2026-07-09 | `go-atlassian` module (`v2`, Go 1.23) | `03-go-atlassian/01-package-layout.md` |
| https://raw.githubusercontent.com/ctreminiom/go-atlassian/main/confluence/api_client_impl.go | 2026-07-09 | `confluence.New(...)` + ClientOption definitions (303 lines) | `03-go-atlassian/02-auth-options.md`, `03-client-call-raw-http.md` |
| https://raw.githubusercontent.com/ctreminiom/go-atlassian/main/confluence/v2/api_client_impl.go | 2026-07-09 | The `confluence/v2/` STUB (only OAuth boilerplate, no services) | `03-go-atlassian/01-package-layout.md` |
| https://raw.githubusercontent.com/ctreminiom/go-atlassian/main/confluence/internal/page_impl.go | 2026-07-09 | Confluence v1 PageService (int IDs, v1 REST) | `03-go-atlassian/01-package-layout.md` |
| https://api.github.com/repos/ctreminiom/go-atlassian/git/trees/main?recursive=1 | 2026-07-09 | Repo file tree (filtered to `confluence/` — 58 files) | `03-go-atlassian/01-package-layout.md` |
| https://github.com/metoro-io/mcp-golang | 2026-07-09 | Open-source repo — the MCP server framework | all of `04-mcp-golang-framework/` |
| https://raw.githubusercontent.com/metoro-io/mcp-golang/main/README.md | 2026-07-09 | `mcp-golang` README (245 lines) | `04-mcp-golang-framework/01-server-api.md`, `02-stdio-transport.md` |
| https://raw.githubusercontent.com/metoro-io/mcp-golang/main/go.mod | 2026-07-09 | `mcp-golang` module (Go 1.21+) | `04-mcp-golang-framework/01-server-api.md` |
| https://raw.githubusercontent.com/metoro-io/mcp-golang/main/server.go | 2026-07-09 | mcp-golang Server source (1045 lines) | `04-mcp-golang-framework/01-server-api.md` |
| https://raw.githubusercontent.com/metoro-io/mcp-golang/main/transport/stdio/stdio_server.go | 2026-07-09 | Stdio transport source (173 lines) | `04-mcp-golang-framework/02-stdio-transport.md` |
| https://raw.githubusercontent.com/metoro-io/mcp-golang/main/examples/readme_server/main.go | 2026-07-09 | The canonical `mcp-golang` server example | `04-mcp-golang-framework/01-server-api.md`, `06-implementation-skeleton/02-main-entrypoint.md` |
| https://paketo.io/docs/howto/go/ | 2026-07-09 | Official Paketo Go how-to | all of `07-paketo-buildpack/` |
| https://buildpacks.io/docs/app-developer-guide/using-project-descriptor/ | 2026-07-09 | Buildpacks project descriptor spec | `07-paketo-buildpack/01-project-toml.md` |
| https://github.com/paketo-buildpacks/go | 2026-07-09 | Paketo Go buildpack repo | `07-paketo-buildpack/01-project-toml.md` |
| https://hermes-agent.nousresearch.com/docs/user-guide/features/mcp/ | 2026-07-09 | Hermes MCP integration docs | all of `08-deployment-hermes/` |
| https://www.augmentcode.com/mcp/mcp-inspector | 2026-07-09 | MCP Inspector troubleshooting guide ("any non-JSON output to stdout breaks the JSON-RPC parser") | `09-anti-patterns/01-stdout-pollution.md` |

## Local skills (Hermes profile)

| Path | Used in |
| ---- | ------- |
| `~/.hermes/skills/mcp/native-mcp/SKILL.md` | `08-deployment-hermes/01-config-yaml.md`, `02-manifest-yaml.md` |
| `~/.hermes/skills/spec-file-section-shape/SKILL.md` | README conventions, Variant B detection |
| `~/.hermes/skills/project/project/SKILL.md` | `06-implementation-skeleton/04-makefile.md` (single source of truth rules) |
| `~/.hermes/skills/project/project/references/rules.md` | `06-implementation-skeleton/04-makefile.md` (required standard commands list) |
| `~/.hermes/skills/project/project/templates/makefile-ada-alire.mk` | `06-implementation-skeleton/04-makefile.md` (template reference; adapted to Go) |

## Background sources (skimmed)

| URL | Read on | Why included |
| --- | ------- | ------------ |
| https://www.atlassian.com/blog/announcements/remote-mcp-server | 2026-07-09 | Atlassian's own Remote MCP Server — the alternative we're not building |
| https://github.com/sooperset/mcp-atlassian | 2026-07-09 | The de-facto Python MCP standard; surveyed in the user's other project (`~/Desktop/hermes/confluence/specs/confluence-sync/13-mcp-server/`) |
| https://github.com/metoro-io/metoro-mcp-server | 2026-07-09 | Reference Go MCP server for Kubernetes, an existence proof that `mcp-golang` works in production |
| https://github.com/buildpacks/pack | 2026-07-09 | The `pack` CLI repo (we already have it installed) |
| https://www.npmjs.com/package/@aashari/mcp-server-atlassian-confluence | 2026-07-09 | Upstream npm package page — confirms 3.3.0 is current |
| https://modelcontextprotocol.io/specification/2025-06-18/architecture/ | 2026-07-09 | MCP spec — JSON-RPC framing invariant |

## Failed sources

| URL | Date | Reason | Fallback |
| --- | ---- | ------ | -------- |
| https://raw.githubusercontent.com/aashari/mcp-server-atlassian-confluence/main/LICENSE | 2026-07-09 | Returns 404; no LICENSE file at repo root | MIT is declared in `package.json` `license` field; sufficient for the re-implementation |
| https://www.google.com/search?q=%22mcp-golang%22+%22confluence%22 | 2026-07-09 | Empty results | No existing Go Confluence MCP server found; this is the first |
| https://www.google.com/search?q=hermes+agent+mcp+server+config+toolsets+native | 2026-07-09 | Returned general results; specific page not in top 5 | Used the Hermes docs page (https://hermes-agent.nousresearch.com/docs/user-guide/features/mcp/) and the local `native-mcp` skill instead |

## Verification gaps

- The upstream's `LICENSE` file is missing from the repo
  root at the time of survey. The `package.json` declares
  `MIT`. This is not load-bearing for the Go port (no
  source-code reuse), but documented here for transparency.
- The Atlassian v2 REST OpenAPI spec
  (`openapi-v2.v3.json`) was not fetched in this pass. The
  v2 endpoint shapes are documented per-page on the
  Atlassian developer site, which is what the upstream and
  the Go port rely on. If a future session wants strict
  schema validation, download from
  `https://dac-static.atlassian.com/cloud/confluence/openapi-v2.v3.json`.
- The `mcp-golang` library's `RegisterTool` introspection
  behavior (how it builds the input schema from a Go struct
  + `jsonschema` tags) is documented in the README but not
  exhaustively tested. The Go implementer should run a
  small test program with a custom struct and verify the
  schema in the `tools/list` response before relying on it
  for complex arg shapes.
- The `pack` CLI was verified installed (`pack --version`
  returns `0.40.7+git-2df3b8c.build-6959`) but a `pack build`
  invocation was not run during research. The verification
  commands in `07-paketo-buildpack/03-verification.md` are
  the implementer's first chance to confirm the build
  pipeline end-to-end.
- The TOON encoder spec is informal — there is no ISO
  standard for TOON. The upstream's encoder is the
  de-facto reference; the Go port's encoder in
  `internal/toon/` matches the upstream's output for common
  cases but may differ slightly for edge cases (deeply
  nested objects, unicode escapes). The verification step
  is to run both encoders on the same JSON fixture and
  diff.