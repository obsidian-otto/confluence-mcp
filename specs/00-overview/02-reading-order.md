# 00.2 — Reading Order and Status

## Overview

Reading order for `specs/` is **top-down, numbered folders first**.
The reading order matches the dependency chain: who we are (00) →
what we are integrating against (01) → the upstream we are porting
(02) → the libraries we are using (03, 04) → the design choices (05,
06) → packaging (07) → Hermes wiring (08) → pitfalls (09) → open
decisions (99). `SOURCES.md` is the URL ledger;
`research/00-sources-and-caveats.md` is the provenance file with
the VERIFICATION REPORT at the bottom.

## Sources

This file is the project's reading guide. It cross-references each
spec file by path. Source URLs for individual claims live in the
spec files themselves.

## Spec

### Reading order (recommended path)

1. **`00-overview/01-purpose-and-scope.md`** — what we are building
   and why.
2. **`00-overview/02-reading-order.md`** — this file.
3. **`01-foundations/01-cloud-vs-datacenter.md`** — Confluence
   Cloud is the target; Data Center is not.
4. **`01-foundations/02-confluence-v2-rest-recap.md`** — the
   surface we are wrapping (the v2 REST endpoints the upstream
   uses).
5. **`01-foundations/03-env-var-contract.md`** — the three
   auth env vars + `.env` file fallback that drive the binary.
6. **`02-upstream-aashari/01-architecture.md`** — full
   architecture review of `@aashari/mcp-server-atlassian-confluence`
   v3.3.0.
7. **`02-upstream-aashari/02-five-tools.md`** — the five CRUD
   tools with their exact input shapes and tool descriptions.
8. **`02-upstream-aashari/03-lessons-and-quirks.md`** — pitfalls
   the upstream has already encountered and we inherit (TOON,
   truncation, debug logging paths) plus what we deliberately do
   not port.
9. **`03-go-atlassian/01-package-layout.md`** — the
   `ctreminiom/go-atlassian/v2` package tree, where the v1
   services live, why `confluence/v2` is a stub.
10. **`03-go-atlassian/02-auth-options.md`** — `WithBasicAuth`,
    `WithOAuth`, `WithAutoRenewalToken`; what to call for v1
    API-token mode.
11. **`03-go-atlassian/03-client-call-raw-http.md`** — how to
    call Confluence REST v2 endpoints via `Client.Call()` since
    `confluence/v2/` has no services.
12. **`04-mcp-golang-framework/01-server-api.md`** —
    `NewServer`, `RegisterTool`, options, return types.
13. **`04-mcp-golang-framework/02-stdio-transport.md`** — the
    stdio transport's `NewStdioServerTransport()` lifecycle.
14. **`04-mcp-golang-framework/03-content-types.md`** — the
    `NewTextContent` / `NewToolResponse` / error-content shape.
15. **`05-tool-surface-design/01-tool-mapping.md`** — which Go
    function backs which MCP tool.
16. **`05-tool-surface-design/02-jmespath-and-toon.md`** —
    JMESPath library choice (`jmespath/go-jmespath`) and TOON
    encoder decision.
17. **`06-implementation-skeleton/01-file-layout.md`** — Go
    package layout with exact file responsibilities.
18. **`06-implementation-skeleton/02-main-entrypoint.md`** —
    `cmd/mcp-confluence/main.go` skeleton.
19. **`06-implementation-skeleton/03-tool-handlers.md`** —
    `internal/tools/` skeleton for each of the five tools.
20. **`06-implementation-skeleton/04-makefile.md`** — the
    Makefile target set, `.PHONY` declaration, `.env.example`,
    `.gitignore` discipline, and the `make help` rendered output.
21. **`07-paketo-buildpack/01-project-toml.md`** — `project.toml`
    shape for the Paketo Go buildpack.
22. **`07-paketo-buildpack/02-pack-command.md`** — the
    `pack build` invocation, including `--builder
    paketobuildpacks/builder-jammy-tiny`.
23. **`07-paketo-buildpack/03-verification.md`** — five commands
    that prove the image is buildable, runnable, and SBOM'd.
24. **`08-deployment-hermes/01-config-yaml.md`** — the
    `mcp_servers:` block for `~/.hermes/config.yaml`.
25. **`08-deployment-hermes/02-manifest-yaml.md`** — the
    `optional-mcps/confluence/manifest.yaml` for one-click
    `hermes mcp install confluence`.
26. **`08-deployment-hermes/03-sample-invocation.md`** — a
    sample `hermes chat` exchange proving the tools are wired.
27. **`09-anti-patterns/01-stdout-pollution.md`** — the JSON-RPC
    stdout invariant and why every log must go to stderr.
28. **`09-anti-patterns/02-secret-handling.md`** — API-token
    handling: never log, never echo, never include in error
    strings.
29. **`09-anti-patterns/03-error-shapes.md`** — what to put in
    the MCP `isError: true` response and what to surface as data.
30. **`99-gap-questions/01-questions.md`** — every open design
    decision, with recommendations.
31. **`99-gap-questions/02-partial-answers.md`** — the locked
    decisions log (Q14 Makefile, Q22 .env loading).
32. **`research/00-sources-and-caveats.md`** — provenance +
    VERIFICATION REPORT.

### Per-spec-set status table

| Folder | Files | Status | Notes |
| ------ | ----- | ------ | ----- |
| 00-overview | 2 | research complete | purpose, scope, reading order, status |
| 01-foundations | 3 | research complete | Cloud vs DC, REST recap, env-var contract (with `.env` resolution) |
| 02-upstream-aashari | 3 | research complete | upstream architecture, tools, lessons |
| 03-go-atlassian | 3 | research complete | package layout, auth, raw HTTP via Client.Call |
| 04-mcp-golang-framework | 3 | research complete | server API, stdio transport, content types |
| 05-tool-surface-design | 2 | research complete | tool mapping, JMESPath + TOON decisions |
| 06-implementation-skeleton | 4 | research complete | file layout, main.go, tool handlers, Makefile |
| 07-paketo-buildpack | 3 | research complete | project.toml, pack command, verification |
| 08-deployment-hermes | 3 | research complete | config.yaml, manifest.yaml, sample invocation |
| 09-anti-patterns | 3 | research complete | stdout pollution, secret handling, error shapes |
| 99-gap-questions | 2 | research complete | 22 decisions + locked partial-answers log (Q14 Makefile, Q22 .env) |
| research | 1 | research complete | provenance + verification report |

**Total spec files (under numbered sub-folders): 29.**

### Status values

- **research complete** — every section has sources and a
  concrete spec; no `GAP-RESEARCH` markers remain.
- **partial** — main content is in but at least one sub-decision
  is open in `99-gap-questions/` (cross-ref to `Q-N`).
- **blocked** — cannot complete until an external dependency or
  user decision.

This spec set is fully `research complete` at the time of writing.
All open items are **user-preference** questions in
`99-gap-questions/`, not research gaps.

### Notes on the project's working context

This spec set lives at `confluence-mcp/specs/`. The project root
contains the Makefile, `.env.example`, `.gitignore`, and
`project.toml` (sibling files, not under `specs/`). The Go
source code (when written) will live in `cmd/` and `internal/`
alongside the spec directory.

A separate (unrelated) Python `confluence-sync` project lives at
`~/Desktop/hermes/confluence/specs/confluence-sync/`. That spec
set covers the full Confluence Cloud REST v2 surface, storage
format conversion, sync semantics (versioning, conflict
detection), and the existing-Python-MCP survey. The two spec
sets are **intentionally separate** and do not cross-reference
each other — the Go MCP server is the CRUD substrate; the Python
sync skill is the higher-level sync orchestrator. If a future
session wants to bridge them, a separate spec set
`specs/confluence-go-sync/` would be the right place.

### Cross-references within this spec set

When this spec set needs to point at another file in the same
set, it uses the **spec-set-root-relative** path (e.g.
`01-foundations/03-env-var-contract.md` from anywhere under
`specs/`). This is the convention enforced by the
`spec-file-section-shape` skill for the project family.

## Verification

A reader of this spec should be able to:

1. Confirm the spec file has exactly four H2 sections in Variant-B order: Overview, Sources, Spec, Verification.
2. Confirm the load-bearing claims have inline source URLs.
3. Confirm any cross-references resolve to files that exist in this spec set.
