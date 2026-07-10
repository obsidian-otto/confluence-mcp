# confluence-mcp — AGENTS.md

> This file is the canonical at-a-glance reference for any agent
> (or human) landing in this project. For deep technical content,
> see `specs/` — this file summarizes and points, it does not
> replace.

## Purpose

> **Verbatim from the user (2026-07-09):**
> "This is a golang project to build a working Confluence MCP for
> Hermes agent."

This project builds a **Confluence MCP server in Go** that
Hermes Agent can register and use as a stdio MCP server. The
binary is a single static Go executable that exposes the
upstream `@aashari/mcp-server-atlassian-confluence` v3.3.0
tool surface (`conf_get`, `conf_post`, `conf_put`,
`conf_patch`, `conf_delete`) — the same five CRUD tools with
TOON-default output and JMESPath filtering.

The Go port uses `github.com/ctreminiom/go-atlassian/v2` for
the Atlassian HTTP work, `github.com/metoro-io/mcp-golang` for
the MCP framing, and `pack` CLI + Paketo Go BuildPak for the
container image.

The project's Makefile is the single source of truth for all
commands (per the `project` skill), and the MCP server loads
its settings from environment variables **or** a `.env` file
(per the user's locked Q22 decision).

## Project goal — bidirectional Markdown ↔ Confluence storage

**Verbatim from the user (2026-07-10):**
"in the end this project must be able to upload a markdown
file into confluence using its own markup format, and be able
to later download confluence documents in their markup format
and convert it locally to markdown before storing it."

That requirement is now the **primary v2 feature**. Concretely:

  - **Upload direction** — `conf_post_markdown` (and `conf_put_markdown`)
    accept a markdown body (string or file path), convert it
    locally to Confluence **storage format XHTML** using
    `github.com/yuin/goldmark`, and POST/PUT to the existing
    v2 REST endpoint with the same `{representation: "storage", value: <XHTML>}`
    envelope the CRUD tools already produce. No round-trip
    through any external service.

  - **Download direction** — `conf_get_page_body` (already
    exists for raw storage) gains a sibling
    `conf_get_page_markdown` that fetches the page, runs the
    storage XHTML through
    `github.com/JohannesKaufmann/html-to-markdown/v2`, and
    returns the markdown text so the caller can write it to
    disk.

  - **Round-trip fidelity** — the upload path and the
    download path are NOT required to produce the literal
    same bytes for the same logical content (markdown is
    lossy on whitespace and reference-style links). The
    contract is **no textual content loss**: the markdown
    text, the page title, code-block contents, list items,
    table cell contents, and link URLs must survive both
    directions. Confluence-specific constructs (macros, info
    panels, mentions, layout sections) are documented as
    known-lossy on the round-trip back to markdown — see
    `specs/10-markdown-roundtrip/03-known-lossy-constructs.md`.

  - **The wire format is always Confluence storage XHTML.**
    Markdown is purely the agent-side representation. The
    MCP server never speaks markdown to the Atlassian API.

## Out-of-band user instructions honored

These are the design decisions that came from iterative
mid-pass steering messages. They are constraints, not
suggestions — an agent MUST NOT second-guess them.

1. **2026-07-09: "the MCP server should load it's settings
   from the environmental variables or from the `.env` file
   inside the container or cli."** — Locked Q22. The binary
   resolves settings in priority order: process env >
   cwd `.env` > binary-dir `.env`. Implementation uses
   30 LOC stdlib Go in `internal/config/dotenv.go` — no
   `godotenv` dependency. `~/.mcp/configs.json` (the
   upstream's third tier) is dropped. Full rationale in
   `specs/99-gap-questions/02-partial-answers.md` Q22.

2. **2026-07-09: "Make sure to add a Makefile as per the
   project skill as a single source of truth for all
   commands to run in this project."** — Locked Q14. The
   Makefile is part of v1 (not v1.1). Per the `project` skill
   (`~/.hermes/skills/project/project/`): `help`, `install`,
   `clean`, `build`, `test`, `lint`, `format`, `check`,
   `type-check`, `security`, `run`, `dev`, `image`,
   `image-inspect`, `sbom`, `verify-env`, `verify-tools`,
   `info`, `locate-bin`, `all`. The Makefile already exists
   at the project root and `make help` works.

## Tech Stack

| Aspect | Detail |
| ------ | ------ |
| Implementation language | **Go 1.23+** (per `ctreminiom/go-atlassian/v2` `go.mod` requirement) |
| Module path (planned) | `github.com/<owner>/mcp-confluence` |
| Binary name | `mcp-confluence` |
| Entry point | `cmd/mcp-confluence/main.go` |
| Build system | Go modules + **Makefile** (single source of truth for all commands) |
| Container image | `pack build` + **Paketo Go BuildPak** (`paketobuildpacks/builder-jammy-tiny`, distroless) |
| Atlassian API client | `github.com/ctreminiom/go-atlassian/v2` (Confluence v1 services + raw `Client.Call()` for v2 REST) |
| MCP framework | `github.com/metoro-io/mcp-golang` (stdio transport at v1) |
| JMESPath library | `github.com/jmespath/go-jmespath` (canonical Go JMESPath) |
| TOON encoder | Custom 150-LOC encoder in `internal/toon/` (no production Go library exists) |
| Markdown → HTML (v2) | `github.com/yuin/goldmark v1.7.13` (CommonMark + GFM, MIT, 35k+ dependents, used by `grantcarthew/acon`) |
| HTML → Markdown (v2) | `github.com/JohannesKaufmann/html-to-markdown/v2 v2.5.2` (MIT, 3.7k stars, plugin-based, golden-file test corpus) |
| Storage-format XHTML normalizer | `github.com/PuerkitoBio/goquery` (tokenised HTML walk for the Confluence-specific post-processing pass — see `specs/10-markdown-roundtrip/02-post-processing.md`) |
| External CLI for build | `pack` 0.27+, `docker` 20.10+, `go` 1.23+ (verified on this host) |
| Hermes integration | `mcp_servers:` block in `~/.hermes/config.yaml` + stdio transport |
| Settings source | env vars **or** `.env` file (per Q22 lock; 30-LOC stdlib parser) |
| License | MIT (matches upstream; goldmark and html-to-markdown are both MIT, so dependency closure is clean) |

## Project Layout

```
confluence-mcp/                              (Go module root — this directory)
├── AGENTS.md                                # this file
├── Makefile                                 # single source of truth for all commands
├── .env.example                              # template (commit; copy to .env for local dev)
├── .gitignore                                # excludes .env, /bin/, /sbom/
├── project.toml                              # Paketo build descriptor (planned, see specs/07-paketo-buildpack/01-project-toml.md)
├── README.md                                 # (planned) project overview
├── LICENSE                                   # (planned) MIT
├── cmd/
│   └── mcp-confluence/
│       └── main.go                           # (planned) entrypoint: load config, build server, serve
└── internal/                                 # (planned) Go packages
    ├── config/                               # LoadFromEnv() + dotenv.go (~30 LOC stdlib parser)
    ├── atlassian/                            # Wrapper around go-atlassian confluence.Client
    ├── jmespath/                             # JMESPath wrapper
    ├── toon/                                 # TOON encoder (~150 LOC)
    ├── markdown/                             # v2 — markdown ↔ storage XHTML bidirectional converter
    │   ├── markdown_to_storage.go           # goldmark → HTML → storage XHTML post-processor
    │   ├── storage_to_markdown.go           # html-to-markdown wrapper
    │   ├── storage_normalize.go             # shared: html.Parse + namespace stripping for ac: / ri:
    │   └── testdata/                         # gold-file fixtures; golden-file lock pattern from html-to-markdown
    ├── tools/                                # 5 tool handlers + registration
    └── server/                               # mcp-golang server bootstrap
└── specs/                                    # this project's spec set (Variant B, 4 sections)
    ├── README.md                             # reading guide
    ├── SOURCES.md                            # URL index
    ├── 00-overview/                          # purpose, scope, reading order, status
    ├── 01-foundations/                       # Cloud vs DC, REST recap, env-var contract (with .env)
    ├── 02-upstream-aashari/                  # full architecture review of the Node.js upstream
    ├── 03-go-atlassian/                      # go-atlassian package layout + auth + Client.Call()
    ├── 04-mcp-golang-framework/              # mcp-golang Server API + stdio transport + content types
    ├── 05-tool-surface-design/               # tool mapping + JMESPath + TOON decisions
    ├── 06-implementation-skeleton/           # file layout + main.go + tool handlers + Makefile
    ├── 07-paketo-buildpack/                  # project.toml + pack build + verification
    ├── 08-deployment-hermes/                 # config.yaml + manifest.yaml + sample invocation
    ├── 09-anti-patterns/                     # stdout pollution + secret handling + error shapes
    ├── 10-markdown-roundtrip/                # v2 — library survey + wire-format contract + lossy-constructs register
    ├── 99-gap-questions/                     # 22 open decisions + locked partial-answers log
    └── research/                             # provenance + VERIFICATION REPORT
```

The Go source tree (`cmd/` + `internal/`) is **not yet
written** — the deliverable at this point is the spec set
that drives the implementation. The Makefile, `.env.example`,
and `.gitignore` are the only root-level artifacts so far.

## Status (as of 2026-07-09)

| Spec set | Topic-spec files | Total size | Status |
| -------- | ----------------: | ---------: | ------ |
| `00-overview/` | 2 | ~17 KB | Spec complete |
| `01-foundations/` | 3 | ~21 KB | Spec complete |
| `02-upstream-aashari/` | 3 | ~27 KB | Spec complete |
| `03-go-atlassian/` | 3 | ~18 KB | Spec complete |
| `04-mcp-golang-framework/` | 3 | ~17 KB | Spec complete |
| `05-tool-surface-design/` | 2 | ~11 KB | Spec complete |
| `06-implementation-skeleton/` | 4 | ~34 KB | Spec complete (incl. Makefile) |
| `07-paketo-buildpack/` | 3 | ~15 KB | Spec complete |
| `08-deployment-hermes/` | 3 | ~16 KB | Spec complete |
| `09-anti-patterns/` | 3 | ~17 KB | Spec complete |
| `99-gap-questions/` | 2 (Q1-Q22 + partial-answers) | ~23 KB | 22 open, 2 locked (Q14, Q22) |
| `research/` | 1 | ~17 KB | Provenance + VERIFICATION REPORT |
| **Total** | **32** | **~308 KB** | **All spec complete; implementation pending** |

Notes on the count:
- "Topic-spec files" includes every `.md` file under the
  numbered sub-folders plus `99-gap-questions/01-questions.md`,
  `99-gap-questions/02-partial-answers.md`, and
  `research/00-sources-and-caveats.md` (32 total).
- The 4-section Variant-B structural check
  (`## Overview / ## Sources / ## Spec / ## Verification`)
  passes for the 29 numbered-topic files; the gap-questions
  and research files are the documented exceptions.

## Architecture (one-paragraph summary)

The binary is a thin CRUD wrapper over the Confluence Cloud
REST v2 API. Five MCP tools (`conf_get` / `post` / `put` /
`patch` / `delete`) each take a path + optional query
params + optional `jq` (JMESPath) expression + optional
`outputFormat` + optional body, and forward to Atlassian via
`go-atlassian`'s `Client.Call()` (since `confluence/v2/` has
no typed services — the bulk of v2 endpoints are raw HTTP).
The five handlers funnel through a single
`executeRequest()` helper in `internal/tools/`. The MCP
framing uses `mcp-golang`'s stdio transport; settings are
loaded from `process env > cwd .env > binary-dir .env`; the
container image is built with `pack` + Paketo Go BuildPak's
distroless `builder-jammy-tiny`.

Full architecture, layer-by-layer, with code skeletons:
`specs/02-upstream-aashari/01-architecture.md` (upstream
layered split + Go port mapping) and
`specs/06-implementation-skeleton/01-file-layout.md` (Go
package boundaries).

## Key Concepts

| Concept | Where documented |
| ------- | ---------------- |
| Five CRUD tools + their input shapes (verbatim from upstream) | `specs/02-upstream-aashari/02-five-tools.md` |
| Settings resolution order (env > .env cwd > .env binary-dir) | `specs/01-foundations/03-env-var-contract.md` |
| The JSON-RPC stdout invariant (no `fmt.Println` to stdout) | `specs/09-anti-patterns/01-stdout-pollution.md` |
| API token redaction (never log; redact in `.env` errors) | `specs/09-anti-patterns/02-secret-handling.md` |
| Error message shape (`<METHOD> <path>: <status> <text> - <body>`) | `specs/09-anti-patterns/03-error-shapes.md` |
| `confluence/v2/` is a STUB — use `Client.Call()` for v2 REST | `specs/03-go-atlassian/01-package-layout.md` |
| TOON saves 30-60% tokens vs JSON (default output format) | `specs/02-upstream-aashari/03-lessons-and-quirks.md` |
| 40k-char truncation with raw-response pointer at `/tmp/mcp/` | `specs/02-upstream-aashari/03-lessons-and-quirks.md` |
| Distroless run image requires `CGO_ENABLED=0` (static binary) | `specs/07-paketo-buildpack/01-project-toml.md` |
| Locked decisions (Q14 Makefile, Q22 .env, Q10 re-shaped) | `specs/99-gap-questions/02-partial-answers.md` |

## Developer Guidelines

### Working in this project

1. **Read `specs/README.md` first**, then follow the reading
   order in `specs/00-overview/02-reading-order.md`.
2. **All commands go through the Makefile.** Never run
   `go build` or `pack build` directly. `make help` lists
   every available target.
3. **No new commands in scattered shell scripts.** The
   Makefile is the single source of truth (per the
   `project` skill, which the user explicitly required).
4. **No stdout writes except JSON-RPC messages.** Every log
   goes to stderr. `log.Printf` is safe (defaults to
   stderr); `fmt.Println` is forbidden.
5. **No token logging.** The API token field is
   `string`-typed and is never passed to a logger,
   formatter, or `os.Environ()` print.
6. **Use the upstream's `CONF_*_DESCRIPTION` strings verbatim**
   in `internal/tools/descriptions.go` — any drift from the
   upstream wording is a bug.

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

- **`confluence/v2/` is a stub.** It contains only OAuth
  boilerplate, no service implementations. The v2 REST
  endpoints (`/wiki/api/v2/...`) are called via
  `Client.Call()` raw HTTP. Don't waste time looking for
  `page_v2.go` or `space_v2.go` — they don't exist; the
  `space_v2_impl.go` file under `confluence/internal/` is
  v1's space service with a v2-flavored name.
- **The Makefile `image` target needs `pack` + `docker`.**
  Both are installed on this host (`pack 0.40.7`,
  `docker 29.5.2`) but the implementer should `make
  verify-tools` first to confirm.
- **`CGO_ENABLED=0` is mandatory** for the distroless run
  image. A CGO binary will fail with `exec format error`
  or missing-library errors at runtime.
- **The Makefile's `verify-env` target never echoes the
  token**, only its length. Don't add a verbose env-print
  that would leak the secret.
- **GO is `1.23+`** (per `ctreminiom/go-atlassian/v2`
  `go.mod`). Older Go will fail to build the module.
- **No source files exist yet.** The `cmd/` and `internal/`
  directories are **planned**, not present. The next
  implementation session creates them from the
  `specs/06-implementation-skeleton/` skeletons.

## Build / test entry point

```bash
# One-shot
make help                # list all commands
make verify-tools        # confirm go, pack, docker are installed
make install             # go mod download
make build               # compile to ./bin/mcp-confluence
make test                # run all tests
make check               # lint + test (pre-commit gate)
make image               # build the OCI image via pack

# Per-spec-set verification
find specs -mindepth 2 -name "*.md" -type f | wc -l
grep -cE '^## (Overview|Sources|Spec|Verification)$' specs/00-overview/01-purpose-and-scope.md
# Expected: 4 (Variant B)
```

## Verification

The spec set has been verified by:

- **Variant B structural check**: 29 numbered sub-folder
  files all have exactly 4 H2 sections in order. The 5
  exception files (README.md, SOURCES.md, gap-questions,
  partial-answers, research) are the documented exceptions.
- **`make help` runs**: renders 20 targets alphabetically
  sorted with descriptions.
- **Repo metadata scan**: `pack`, `docker`, `go` are all
  installed and meet version minimums (verified 2026-07-09).
- **All cited URLs fetched** in `/tmp/mcp-research/` during
  the spec write pass; provenance recorded in
  `specs/research/00-sources-and-caveats.md` (C1-C7 caveats
  noted).

For implementation-phase verification, the canonical
five-command test sequence is documented in
`specs/07-paketo-buildpack/03-verification.md`.
