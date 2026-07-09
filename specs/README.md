# confluence-mcp — Research Specs

Research deliverable describing how to build a **Confluence MCP
server in Go** that Hermes Agent can register and use as a
stdio MCP server. The design is a **port** of the upstream
Node.js server `@aashari/mcp-server-atlassian-confluence`
v3.3.0 to Go, using **`github.com/ctreminiom/go-atlassian/v2`**
for the Atlassian HTTP work, **`github.com/metoro-io/mcp-golang`**
for the MCP framing, and **`pack` CLI + Paketo Go BuildPak**
for the container image.

The project's Makefile is the single source of truth for all
commands (per the `project` skill), and the MCP server loads
its settings from environment variables **or** a `.env` file
(per the user's locked Q22 decision).

Reading order is suggested (and matches the numbered
sub-folders): top-to-bottom, `00-overview` first, `99-gap-questions`
last.

| Folder | Topic |
| ------ | ----- |
| 00-overview | purpose, scope, reading order, status table |
| 01-foundations | Cloud vs DC, Confluence REST v2 recap, env-var contract (with `.env` resolution) |
| 02-upstream-aashari | full architecture review of the Node.js upstream |
| 03-go-atlassian | `ctreminiom/go-atlassian/v2` package layout + auth + raw HTTP via `Client.Call()` |
| 04-mcp-golang-framework | `mcp-golang` Server API + stdio transport + content types |
| 05-tool-surface-design | tool mapping (Go function → MCP tool) + JMESPath + TOON encoder decisions |
| 06-implementation-skeleton | Go package layout + `cmd/mcp-confluence/main.go` skeleton + tool handlers + Makefile |
| 07-paketo-buildpack | `project.toml` + `pack build` command + five-command verification |
| 08-deployment-hermes | `mcp_servers:` block in `~/.hermes/config.yaml` + catalog `manifest.yaml` + sample invocation |
| 09-anti-patterns | stdout-pollution, secret-handling, error-message shapes |
| 99-gap-questions | 22 open design decisions + locked partial-answers log (Q14 Makefile, Q22 .env) |
| research | provenance + VERIFICATION REPORT |

This is a **research** deliverable. No `.go` files have been
written. Implementation depends on resolving the gap
questions in `99-gap-questions/`.

Conventions:
- **Inline source URLs** precede every load-bearing claim.
- **Tables** for endpoints, request/response shapes, and
  method comparisons.
- **Go code skeletons** appear where they show the API
  shape (struct definitions, handler signatures, error
  helpers).
- **Cross-references** within this spec set use the
  **spec-set-root-relative** path (e.g.
  `01-foundations/03-env-var-contract.md` from anywhere
  under `specs/`).
- **Open questions** are tagged `Q-N` in spec bodies and
  rolled up in `99-gap-questions/01-questions.md`.

Variant: this spec set uses the **four-section shape**
(Variant B) per the project's convention
(`## Overview / ## Sources / ## Spec / ## Verification`).
README.md, SOURCES.md, and
`research/00-sources-and-caveats.md` are the documented
exceptions to that rule.

Related artifacts (not in this project):
- `SOURCES.md` — canonical URL index, version-dated.
- `research/00-sources-and-caveats.md` — what was read, what
  failed, what was inferred; VERIFICATION REPORT at the
  bottom.
- The Python `confluence-sync` skill (in a separate project
  at `~/Desktop/hermes/confluence/specs/confluence-sync/`)
  covers the full Confluence Cloud REST v2 surface, storage
  format conversion, sync semantics (versioning, conflict
  detection), and the existing-Python-MCP survey. The two
  spec sets are **intentionally separate** and do not
  cross-reference each other — the Go MCP server is the
  CRUD substrate; the Python sync skill is the higher-level
  sync orchestrator.