# 00.1 — Purpose and Scope

## Overview

This spec set describes how to build a **Confluence MCP server in Go**
that Hermes Agent can register as a stdio MCP server and use to read,
write, search, and comment on Confluence Cloud pages. The target
binary is a single statically-linked Go executable exposed via
`mcp_servers:` in `~/.hermes/config.yaml`. The user invokes
`mcp_confluence_conf_get`, `mcp_confluence_conf_post`, etc. just like
they would call any other Hermes tool.

The reference design **ports the upstream Node.js server
`@aashari/mcp-server-atlassian-confluence` v3.3.0** to Go — same five
CRUD tools, same JMESPath filtering, same TOON-vs-JSON output switch,
same three env-var auth scheme (plus an optional `.env` file
fallback). The port uses **`github.com/ctreminiom/go-atlassian/v2`**
for the underlying Atlassian HTTP work and
**`github.com/metoro-io/mcp-golang`** for the MCP framing. The
container image is built with **`pack` CLI + Paketo Go BuildPak** so
the resulting OCI image is reproducible and SBOM-billable without a
hand-written Dockerfile. The project Makefile is the single source
of truth for all commands (per the `project` skill rules).

This is a **research** deliverable. No `.go` files have been written.
The downstream implementer reads the spec set top-to-bottom and
produces the binary. The spec set is **decision-ready** — every
real design choice is captured as a `Q-N` in
`99-gap-questions/01-questions.md` with a recommended default; locked
decisions are recorded in `99-gap-questions/02-partial-answers.md`.

## Sources

- User request: "do web research how to convert
  https://github.com/aashari/mcp-server-atlassian-confluence to an
  golang confluence MCP server that hermes can use. Use
  https://github.com/ctreminiom/go-atlassian/ as the atlassian api
  and https://github.com/metoro-io/mcp-golang for the MCP server.
  Make use of cloud native buildpacks paketo Go BuildPak via the
  `pack` command already installed. Store the output under
  specs/."
- User follow-up: "the MCP server should load it's settings from
  the environmental variables or from the `.env` file inside the
  container or cli. Make sure to add a Makefile as per the project
  skill as a single source of truth for all commands to run in
  this project."

## Spec

### What we are building

A Go binary named **`mcp-confluence`** (single executable, no runtime
deps) that:

1. Exposes the **MCP stdio transport** so Hermes Agent can launch
   it as a subprocess via `command:` + `args:` in
   `~/.hermes/config.yaml`.
2. Registers **five MCP tools** matching the upstream design
   exactly: `conf_get`, `conf_post`, `conf_put`, `conf_patch`,
   `conf_delete`. Each takes a Confluence REST path (e.g.
   `/wiki/api/v2/spaces/123`), optional `queryParams`, optional
   `jq` (JMESPath) expression, optional `outputFormat` (`"toon"`
   default, `"json"` opt-in), and (for POST/PUT/PATCH) a `body`
   map. The handler calls the Atlassian API via the underlying
   Go client and returns the response.
3. Authenticates to Atlassian via three env vars —
   `ATLASSIAN_SITE_NAME`, `ATLASSIAN_USER_EMAIL`,
   `ATLASSIAN_API_TOKEN` — exactly mirroring the upstream. The
   binary also reads a `.env` file from the current working
   directory and from the directory next to the binary itself
   (see `01-foundations/03-env-var-contract.md` for the priority
   order). Process env always wins; `.env` provides defaults.
4. Is packaged as an **OCI image** built with `pack` + the Paketo
   Go BuildPak so the same source builds an image that can be
   `docker run`-ed, deployed to any CNCF-conformant runtime, and
   SBOM-attested.

### Why

| Reason | Detail |
| ------ | ------ |
| **Hermes-integration parity** | The current Confluence MCP story is `npx -y @aashari/mcp-server-atlassian-confluence` — every time Hermes starts, it `npm install`s the upstream package, then runs it on Node 18+. A pre-built Go binary has **<10 ms cold start, zero install**, and no Node dependency on the host. |
| **Single-binary deploy** | `mcp-confluence` is one static binary. `cp` it onto the box, register it in `mcp_servers:`, done. No `node_modules/`, no `npm` runtime. |
| **Reproducible container image** | `pack build` + Paketo Go BuildPak produces an OCI image with a verified SBOM (`sbom.cdx.json`), reproducible builds (Pinned builder SHA), and a tiny distroless run image. The same `pack` invocation works locally and in CI. |
| **Sync semantics stay where they belong** | The five CRUD tools are intentionally **sync-unaware** — no `version.number + 1` math, no conflict markers, no filename-version scheme. The `confluence-sync` Python skill (in `~/Desktop/hermes/confluence/specs/confluence-sync/`) owns that surface. The Go MCP server is the **raw CRUD substrate** that the Python skill can call if it ever wants to. |
| **Cost optimization parity with upstream** | The upstream's `jq` (JMESPath) and TOON-output defaults reduce LLM token cost 30-60% versus raw JSON. We keep that exact behavior so existing upstream-trained prompts work unchanged. |
| **Project-skill single source of truth** | The `Makefile` at the project root is the only entry point for all commands (build, test, lint, image, run, clean, check). No scattered shell scripts. Per the `~/.hermes/skills/project/project/` skill rules. |

### What is IN scope

- The five CRUD tools (`conf_get/post/put/patch/delete`) with the
  same parameter shape and TOON/JMESPath behavior as the upstream.
- Basic-auth via API token (the upstream's only auth mode).
- Stdio transport (the upstream's recommended transport for Claude
  Desktop / Hermes).
- Single-confluence-instance per binary (one site name per
  process; multi-site is out of scope for v1 — see gap Q1).
- Settings loaded from process env **or** `.env` file (cwd +
  binary-dir) per the locked Q22 decision.
- Container build via `pack` CLI + Paketo Go BuildPak
  (`paketobuildpacks/builder-jammy-tiny` for distroless run
  image).
- Hermes `config.yaml` registration snippet + a one-click install
  path via `hermes mcp add`.
- Token-redacting structured logging to **stderr** (stdout is
  reserved for the JSON-RPC stream — see
  `09-anti-patterns/01-stdout-pollution.md`).
- The `Makefile` as the single source of truth for all commands,
  per the project skill.

### What is OUT of scope

- **OAuth 2.0 (3LO) flow.** The upstream also does not implement
  3LO; only API-token basic-auth. Surfaced as a gap (Q2) for a
  future v2.
- **Storage-format ↔ markdown conversion.** Out of scope; the Go
  MCP server passes storage-format strings through verbatim.
  Markdown↔storage lives in the `confluence-sync` Python spec
  set (sibling project at `~/Desktop/hermes/confluence/specs/confluence-sync/`).
- **Sync semantics** (version bumping, conflict detection,
  content hashing, filename versioning). Out of scope; lives in
  `confluence-sync/`.
- **Jira support.** The upstream is Confluence-only; the Go port
  follows suit.
- **HTTP transport** at v1. Stdio is sufficient for Hermes
  stdio-launch. Surfaced as gap (Q3) for a future HTTP-mode build.
- **MCP resources / prompts.** Upstream exposes tools only;
  prompts and resources are not part of the upstream surface and
  not part of v1.

### Audience

- The user (Bennie) — wants a fast, single-binary Confluence MCP
  server that integrates with Hermes with no Node dependency.
- A downstream coding agent — reads this spec set top-to-bottom
  and produces a Go module under this `confluence-mcp/` directory.
- Future maintainers — want clear separation between the
  CRUD-tool surface (this spec set) and the sync semantics
  (the `confluence-sync` Python spec set).

### Deliverable shape

```
confluence-mcp/                                  (Go module root)
├── Makefile                                      (single source of truth for all commands)
├── .env.example                                  (template; copy to .env for local dev)
├── .gitignore                                    (excludes .env, bin/, sbom/)
├── project.toml                                  (Paketo project descriptor)
├── README.md
├── cmd/mcp-confluence/main.go                    (entrypoint)
└── internal/
    ├── config/                                   (env-var + .env loader)
    ├── atlassian/                                (Client wrapper, auth, raw HTTP)
    ├── jmespath/                                 (JMESPath wrapper)
    ├── toon/                                     (TOON encoder)
    ├── tools/                                    (5 tool handlers + registration)
    └── server/                                   (mcp-golang server boot)

confluence-mcp/specs/                             (this spec set)
├── README.md
├── SOURCES.md
├── 00-overview/                                  (purpose, scope, reading order, status)
├── 01-foundations/                               (Cloud vs DC, REST recap, env-var contract incl. .env)
├── 02-upstream-aashari/                          (full upstream architecture review)
├── 03-go-atlassian/                              (package layout, v1 vs v2 stub, Client.Call, auth)
├── 04-mcp-golang-framework/                      (Server API, stdio transport, content types)
├── 05-tool-surface-design/                       (tool mapping + JMESPath + TOON decisions)
├── 06-implementation-skeleton/                   (file layout, main.go, tool handlers, Makefile)
├── 07-paketo-buildpack/                          (project.toml + pack command + verification)
├── 08-deployment-hermes/                         (config.yaml + manifest.yaml + sample invoke)
├── 09-anti-patterns/                             (stdout pollution, secret handling, error shapes)
├── 99-gap-questions/                             (22 open decisions, locked + open)
└── research/00-sources-and-caveats.md            (provenance + VERIFICATION REPORT)
```

## Verification

A reader of this spec should be able to:

1. Confirm the spec file has exactly four H2 sections in Variant-B order: Overview, Sources, Spec, Verification.
2. Confirm the load-bearing claims have inline source URLs.
3. Confirm any cross-references resolve to files that exist in this spec set.
