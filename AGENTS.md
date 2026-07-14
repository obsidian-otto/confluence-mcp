# confluence-mcp — AGENTS.md

> Canonical at-a-glance reference for any agent landing in this
> project. The deep technical content lives in `specs/`; this
> file summarizes and points — it doesn't replace.
>
> **Implementation status: COMPLETE.** As of 2026-07-13 the
> Go source tree (`cmd/` + `internal/`) is fully written:
> 17 MCP tools wired through a single `executeRequest()`
> pipeline, 156 test functions, a distroless OCI image
> produced by `make image`. The phased delivery log lives in
> `IMPLEMENTATION_PLAN.md`; this file documents the **what
> exists today**, the **how it is laid out**, and the **few
> hard rules** an agent must follow when touching it.

## Purpose

> **Verbatim from the user (2026-07-09):**
> "This is a golang project to build a working Confluence MCP for
> Hermes agent."

A Confluence MCP server in Go that Hermes Agent can register as
a stdio MCP server. The binary is a single static Go
executable that exposes **17 tools** over the JSON-RPC stdio
transport:

| Group | Tools | Provenance |
| ----- | ----- | ---------- |
| CRUD (upstream parity) | `conf_get`, `conf_post`, `conf_put`, `conf_patch`, `conf_delete` | byte-for-byte port of `@aashari/mcp-server-atlassian-confluence` v3.3.0 — descriptions verbatim from upstream, name set locked by `server_test.go` |
| Convenience helpers | `conf_list_spaces`, `conf_list_pages`, `conf_get_page_body`, `conf_search`, `conf_help` | added in the 2026-07-10 audit closure (`specs/99-gap-questions/04-post-v1-audit-2026-07-10-closed.md`) |
| Markdown round-trip (v2) | `conf_post_markdown`, `conf_put_markdown`, `conf_get_page_markdown` | added 2026-07-10 per user's verbatim requirement (see below) |
| Attachments (v3) | `conf_upload_attachment`, `conf_list_attachments`, `conf_delete_attachment` | binary uploads via v1 REST, list/delete via v2 (`specs/11-attachments`) |
| drawio orchestrator (v3) | `conf_upload_drawio` | upload + embed in one call (`specs/12-drawio-attachments`) |

All 17 handlers funnel through a single `executeRequest()`
helper in `internal/tools/` that runs the 9-step pipeline:
URL build → call → JSON decode → JMESPath filter (if `jq`) →
TOON encode → 40k truncation (if oversized) → typed API error
wrap → panics via `safeHandler`.

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
7. **Tool name set is frozen.** The 17 tool names registered in
   `internal/tools/register.go` are the EXACT names Hermes and
   any other MCP client will see in `tools/list` / `tools/call`.
   After the `mcp_confluence_` server prefix the wire identifiers
   are `mcp_confluence_conf_get`, etc.
   `server_test.go`'s `TestNew_RegistersAllSeventeenTools` and
   `TestNew_RegistersExactlySeventeenTools` enforce the set.

## Tech Stack

| Aspect | Detail |
| ------ | ------ |
| Implementation language | **Go** (see `go.mod`) |
| Module path | `github.com/bennie/mcp-confluence` |
| Binary name | `mcp-confluence` |
| Entry point | `cmd/mcp-confluence/main.go` |
| Build system | Go modules + **Makefile** (single source of truth) |
| Container image | `pack build` + **Paketo Go BuildPak** (`paketobuildpacks/builder-jammy-tiny`, distroless) — `make image` |
| Atlassian HTTP | Raw `Client.HTTP.Do` against the v1 + v2 REST surfaces (`specs/03-go-atlassian/01-package-layout.md` explains why `confluence/v2/` typed services are a stub) |
| MCP framing | `github.com/metoro-io/mcp-golang` v0.16.1 (stdio transport at v1) |
| JSON-schema reflection (MCP) | `github.com/invopop/jsonschema` v0.12.0 |
| JMESPath | `github.com/jmespath/go-jmespath` v0.4.0 |
| TOON encoder | Custom ~150 LOC encoder in `internal/toon/encode.go` (no production Go library exists) |
| Markdown → HTML | `github.com/yuin/goldmark` v1.8.2 (CommonMark + GFM) |
| HTML → Markdown | `github.com/JohannesKaufmann/html-to-markdown/v2` v2.5.2 (base, commonmark, strikethrough, table plugins) |
| Storage XHTML normalizer | `github.com/PuerkitoBio/goquery` v1.12.0 |
| drawio PNG encoding | `internal/drawio/` (custom: PNG tEXt chunk with `mxfile` keyword + URL-encoded inner XML) — see `specs/12-drawio-attachments/` |
| External CLIs (for image) | `pack` + `docker` |
| Hermes integration | `mcp_servers:` block in `~/.hermes/config.yaml` + stdio transport |
| Settings source | env vars **or** `.env` file (per Q22 lock) |
| License | MIT (goldmark + html-to-markdown both MIT; closure clean) |

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
├── IMPLEMENTATION_PLAN.md                    # 16-phase delivery log (Phases 0-15, all checked)
├── cmd/
│   └── mcp-confluence/
│       ├── main.go                           # entrypoint: load config, build client, build server, serve
│       └── main_test.go                      # 4 lifecycle tests (no env, valid env, missing env, token never logged)
├── internal/
│   ├── config/                               # LoadFromEnv() + stdlib dotenv.go + 30 LOC parser
│   ├── atlassian/                            # Client wrapper (raw HTTP, basic auth, multipart upload) + APIError + auth.applyAuthHeader
│   ├── jmespath/                             # Apply wrapper with empty-expr short-circuit
│   ├── toon/                                 # TOON encoder (~150 LOC)
│   ├── markdown/                             # v2 — markdown ↔ storage XHTML bidirectional converter
│   │   ├── markdown_to_storage.go            # goldmark → HTML → storage XHTML post-processor
│   │   ├── storage_to_markdown.go            # html-to-markdown wrapper
│   │   └── *_test.go                         # golden-file round-trip tests
│   ├── templates/                            # compiled text/template helpers (AtlassianBaseURL, PageBodyPath, Backticked)
│   ├── drawio/                               # drawio PNG encoding (PNG + tEXt "mxfile" chunk + URL-encoded inner XML)
│   ├── server/                               # mcp.Server constructor (transport + version options + RegisterAll)
│   └── tools/                                # 17 tool handlers + args + descriptions + executeRequest pipeline + safeHandler panic recovery + register
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
| Test functions | ~156 |
| Internal packages | 10 (`config`, `atlassian`, `jmespath`, `toon`, `markdown`, `templates`, `drawio`, `server`, `tools`, plus the `cmd/` entrypoint) |
| MCP tools registered | 17 |
| CRUD tool descriptions locked by `descriptions_test.go` byte equality | 5 |
| Spec topic folders | 14 |

## Architecture (one-paragraph summary)

The binary is a thin, JSON-aware wrapper over Confluence Cloud
REST v1 + v2. The 17 MCP tools each register through
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
`safeHandler`. Settings resolve in env > cwd `.env` >
binary-dir `.env`; lifecycle lives in `cmd/mcp-confluence/main.go`
(`runLifecycle(ctx)` → `serveUntilDone(ctx, srv)`). The container
image is built with `pack build` against
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
| 17 tools + their input shapes | `specs/05-tool-surface-design/` (CRUD) + `specs/10-markdown-roundtrip/04-tool-surface.md` (markdown) + `specs/11-attachments/` (attachments) + `specs/12-drawio-attachments/` (drawio) |
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
   Then add a `TestNew_RegistersAllSeventeenTools`-style
   assertion in `server_test.go` that updates the count.
   Today it asserts **exactly 17** — bump it to 18 if you
   add the 18th.

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
- **Tool name set is frozen.** The 17 names registered in
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
make test                # run all 156 tests
make check               # lint + test (pre-commit gate)
make image               # build the OCI image via pack + Paketo
make docker-build        # plain-docker fallback when pack is unavailable
make verify-env          # print env status (token redacted; length only)
make info                # show project + tool versions
```

## Verification

**Current state (2026-07-13):**

| Item | Result | How verified |
| ---- | ------ | ------------ |
| 17 tools registered | ✅ | `internal/server/server_test.go` — `TestNew_RegistersAllSeventeenTools` + `TestNew_RegistersExactlySeventeenTools` |
| All tests green | ✅ | `make test` — 156 test functions across 10 packages |
| `make build` produces a working binary | ✅ | `bin/mcp-confluence` exists, prints lifecycle startup on run |
| `make check` (lint + test) | ✅ | `go vet ./...` clean, `gofmt -l .` returns nothing |
| `make image` produces distroless OCI image | ✅ | pack + Paketo Go BuildPak pipeline (`project.toml`) |
| Hermes registers the server and lists 17 tools | ✅ | `hermes mcp test confluence` against the running container |
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
