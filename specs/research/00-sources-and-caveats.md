# 00 — Sources, Caveats, Followup, Verification

## SOURCES

This file is the **provenance** document for the
`confluence-mcp/specs/` spec set. It records every URL cited
across the spec set, grouped by topic, with counts and
verification status.

### Group 1 — Upstream Node.js server (`aashari/mcp-server-atlassian-confluence`)

| URL | Status | Used in |
| --- | ------ | ------- |
| https://github.com/aashari/mcp-server-atlassian-confluence | live | all of `02-upstream-aashari/` |
| https://raw.githubusercontent.com/aashari/mcp-server-atlassian-confluence/main/README.md | live (626 lines) | `02-upstream-aashari/01-architecture.md`, `02-five-tools.md` |
| https://raw.githubusercontent.com/aashari/mcp-server-atlassian-confluence/main/package.json | live (version 3.3.0) | `02-upstream-aashari/01-architecture.md` |
| https://raw.githubusercontent.com/aashari/mcp-server-atlassian-confluence/main/src/index.ts | live (221 lines) | `02-upstream-aashari/01-architecture.md` |
| https://raw.githubusercontent.com/aashari/mcp-server-atlassian-confluence/main/src/controllers/atlassian.api.controller.ts | live (158 lines) | `02-upstream-aashari/01-architecture.md` |
| https://raw.githubusercontent.com/aashari/mcp-server-atlassian-confluence/main/src/services/vendor.atlassian.api.service.ts | live (261 lines) | `02-upstream-aashari/01-architecture.md` |
| https://raw.githubusercontent.com/aashari/mcp-server-atlassian-confluence/main/src/tools/atlassian.api.tool.ts | live (294 lines) | `02-upstream-aashari/02-five-tools.md` |
| https://api.github.com/repos/aashari/mcp-server-atlassian-confluence/contents/src | live (file inventory) | `02-upstream-aashari/01-architecture.md` |
| https://www.npmjs.com/package/@aashari/mcp-server-atlassian-confluence | live | `02-upstream-aashari/01-architecture.md` |

**Subtotal: 9 URLs, all live at survey time.**

### Group 2 — Atlassian API client (`ctreminiom/go-atlassian`)

| URL | Status | Used in |
| --- | ------ | ------- |
| https://github.com/ctreminiom/go-atlassian | live | all of `03-go-atlassian/` |
| https://raw.githubusercontent.com/ctreminiom/go-atlassian/main/README.md | live (461 lines) | `03-go-atlassian/01-package-layout.md`, `02-auth-options.md` |
| https://raw.githubusercontent.com/ctreminiom/go-atlassian/main/go.mod | live (v2, Go 1.23) | `03-go-atlassian/01-package-layout.md` |
| https://raw.githubusercontent.com/ctreminiom/go-atlassian/main/LICENSE | live (MIT) | `03-go-atlassian/01-package-layout.md` |
| https://raw.githubusercontent.com/ctreminiom/go-atlassian/main/confluence/api_client_impl.go | live (303 lines) | `03-go-atlassian/02-auth-options.md`, `03-client-call-raw-http.md` |
| https://raw.githubusercontent.com/ctreminiom/go-atlassian/main/confluence/v2/api_client_impl.go | live (stub) | `03-go-atlassian/01-package-layout.md` |
| https://raw.githubusercontent.com/ctreminiom/go-atlassian/main/confluence/internal/page_impl.go | live | `03-go-atlassian/01-package-layout.md` |
| https://raw.githubusercontent.com/ctreminiom/go-atlassian/main/confluence/internal/content_impl.go | live | `03-go-atlassian/01-package-layout.md` |
| https://raw.githubusercontent.com/ctreminiom/go-atlassian/main/confluence/internal/space_v2_impl.go | live | `03-go-atlassian/01-package-layout.md` |
| https://raw.githubusercontent.com/ctreminiom/go-atlassian/main/examples/multi_service_oauth2_example.go | live (306 lines) | `03-go-atlassian/02-auth-options.md` |
| https://api.github.com/repos/ctreminiom/go-atlassian/git/trees/main?recursive=1 | live (58 files under confluence/) | `03-go-atlassian/01-package-layout.md` |

**Subtotal: 11 URLs, all live at survey time.**

### Group 3 — MCP server framework (`metoro-io/mcp-golang`)

| URL | Status | Used in |
| --- | ------ | ------- |
| https://github.com/metoro-io/mcp-golang | live | all of `04-mcp-golang-framework/` |
| https://raw.githubusercontent.com/metoro-io/mcp-golang/main/README.md | live (245 lines) | `04-mcp-golang-framework/01-server-api.md`, `02-stdio-transport.md` |
| https://raw.githubusercontent.com/metoro-io/mcp-golang/main/go.mod | live (Go 1.21+) | `04-mcp-golang-framework/01-server-api.md` |
| https://raw.githubusercontent.com/metoro-io/mcp-golang/main/LICENSE | live (MIT) | `04-mcp-golang-framework/01-server-api.md` |
| https://raw.githubusercontent.com/metoro-io/mcp-golang/main/server.go | live (1045 lines) | `04-mcp-golang-framework/01-server-api.md` |
| https://raw.githubusercontent.com/metoro-io/mcp-golang/main/transport/stdio/stdio_server.go | live (173 lines) | `04-mcp-golang-framework/02-stdio-transport.md` |
| https://raw.githubusercontent.com/metoro-io/mcp-golang/main/tool_api.go | live (13 lines) | `04-mcp-golang-framework/01-server-api.md` |
| https://raw.githubusercontent.com/metoro-io/mcp-golang/main/examples/readme_server/main.go | live | `04-mcp-golang-framework/01-server-api.md`, `06-implementation-skeleton/02-main-entrypoint.md` |
| https://api.github.com/repos/metoro-io/mcp-golang/contents/transport/stdio | live (file inventory) | `04-mcp-golang-framework/02-stdio-transport.md` |
| https://api.github.com/repos/metoro-io/mcp-golang/contents/examples | live (10 example subdirs) | `04-mcp-golang-framework/01-server-api.md` |

**Subtotal: 10 URLs, all live at survey time.**

### Group 4 — Paketo Buildpacks

| URL | Status | Used in |
| --- | ------ | ------- |
| https://paketo.io/docs/howto/go/ | live | all of `07-paketo-buildpack/` |
| https://buildpacks.io/docs/app-developer-guide/using-project-descriptor/ | live | `07-paketo-buildpack/01-project-toml.md` |
| https://paketo.io/docs/reference/go-reference | live | `07-paketo-buildpack/01-project-toml.md` |
| https://github.com/paketo-buildpacks/go | live | `07-paketo-buildpack/01-project-toml.md` |
| https://github.com/paketo-buildpacks/go-build | live | `07-paketo-buildpack/01-project-toml.md` |

**Subtotal: 5 URLs, all live at survey time.**

### Group 5 — Hermes Agent MCP integration

| URL | Status | Used in |
| --- | ------ | ------- |
| https://hermes-agent.nousresearch.com/docs/user-guide/features/mcp/ | live | all of `08-deployment-hermes/` |
| `~/.hermes/skills/mcp/native-mcp/SKILL.md` (local) | live | `08-deployment-hermes/01-config-yaml.md`, `02-manifest-yaml.md` |
| https://github.com/NousResearch/hermes-agent | live | `08-deployment-hermes/02-manifest-yaml.md` |

**Subtotal: 3 URLs (including 1 local skill).**

### Group 6 — MCP spec / Inspector / debug aids

| URL | Status | Used in |
| --- | ------ | ------- |
| https://www.augmentcode.com/mcp/mcp-inspector | live | `09-anti-patterns/01-stdout-pollution.md` |
| https://modelcontextprotocol.io/specification/2025-06-18/architecture/ | live | `04-mcp-golang-framework/02-stdio-transport.md`, `09-anti-patterns/01-stdout-pollution.md` |
| https://modelcontextprotocol.io/docs/tools/inspector | live | `04-mcp-golang-framework/02-stdio-transport.md` |

**Subtotal: 3 URLs, all live at survey time.**

### Group 7 — Local skills (Hermes profile)

| Path | Status | Used in |
| ---- | ------ | ------- |
| `~/.hermes/skills/mcp/native-mcp/SKILL.md` | live | `08-deployment-hermes/01-config-yaml.md`, `02-manifest-yaml.md` |
| `~/.hermes/skills/spec-file-section-shape/SKILL.md` | live | README conventions, Variant B detection |
| `~/.hermes/skills/project/project/SKILL.md` | live | `06-implementation-skeleton/04-makefile.md` (single source of truth rules) |
| `~/.hermes/skills/project/project/references/rules.md` | live | `06-implementation-skeleton/04-makefile.md` (required standard commands list) |
| `~/.hermes/skills/project/project/templates/makefile-ada-alire.mk` | live | `06-implementation-skeleton/04-makefile.md` (template reference; adapted to Go) |

**Subtotal: 5 local skills.**

### Total counts

| Group | URLs |
| ----- | ---- |
| Upstream Node.js server | 9 |
| Atlassian API client | 11 |
| MCP server framework | 10 |
| Paketo Buildpacks | 5 |
| Hermes Agent MCP | 3 |
| MCP spec / Inspector | 3 |
| Local skills | 5 (skill paths, not URLs) |
| **Total external URLs** | **41** |
| **Total local skill paths** | **5** |

## RESEARCH CAVEATS

Known limitations of this spec set, in descending impact
order.

### C1 — `pack build` was not run during research

The most consequential gap: the `pack build` invocation in
`07-paketo-buildpack/02-pack-command.md` was specified from
documentation but not actually executed. The verification
commands in `03-verification.md` are the implementer's
first end-to-end test. If `pack build` fails with an error
not anticipated by the spec (e.g. builder image drift,
buildpack compatibility issue), the implementer should:

1. Note the failure in this file.
2. Update `02-pack-command.md` with the corrected invocation.
3. Bump the spec set version (gap Q17 will cover builder
   pinning).

### C2 — TOON encoder has no Go library

The `internal/toon/` encoder is a **custom re-implementation**
following the upstream's encoder output. There is no
production TOON library for Go at survey time. The
implementer must:

1. Run the upstream server against a test instance with no
   `jq` filter to capture the upstream's TOON output for
   common JSON shapes.
2. Compare the Go encoder's output byte-for-byte.
3. If differences exist, update `internal/toon/encode.go`
   until the outputs match.

### C3 — `mcp-golang` `RegisterTool` introspection not exhaustively tested

The library's auto-schema generation from Go structs +
`jsonschema` tags is documented in the README but not
exhaustively tested. The implementer should write a small
test program with a complex struct (nested objects, enums,
required vs optional) and verify the schema in the
`tools/list` response matches expectations.

### C4 — Confluence v2 OpenAPI spec not downloaded

The Atlassian v2 OpenAPI spec
(`openapi-v2.v3.json`) was not fetched. The endpoint
shapes in `01-foundations/02-confluence-v2-rest-recap.md`
are taken from the per-page Atlassian docs and the upstream
README. For strict schema validation, download the OpenAPI
JSON from
`https://dac-static.atlassian.com/cloud/confluence/openapi-v2.v3.json`.

### C5 — Upstream LICENSE file not present

`https://raw.githubusercontent.com/aashari/mcp-server-atlassian-confluence/main/LICENSE`
returns 404. The `package.json` declares MIT. Not load-
bearing for the Go port (no source-code reuse), but
documented for transparency.

### C6 — Hermes catalog manifest not yet submitted

The `manifest.yaml` in
`08-deployment-hermes/02-manifest-yaml.md` is the spec for
what would be submitted to the hermes-agent catalog. It
has not been submitted (gap Q20). If the user wants the
catalog entry, a PR to
`optional-mcps/confluence/manifest.yaml` in the
hermes-agent repo is the next step.

### C7 — HTTP transport mode not exercised

The Go MCP server v1 is stdio-only (gap Q3). The
`http.NewHTTPTransport("/mcp")` and `http.NewGinTransport()`
APIs from `mcp-golang` are documented but not exercised. If
the user later wants HTTP mode, the implementer must add
a `TRANSPORT_MODE=http` branch and verify against a remote
MCP client.

## FOLLOWUP WORK

Out of scope for this spec set, but the next person should
consider doing these things.

### F1 — Resolve gap questions in `99-gap-questions/01-questions.md`

19 questions have a recommended default; user must confirm
or override. The lock-tracking workflow (per the
`lock-tracking-workflow` reference in the spec skill) is
the recommended pattern for multi-batch locking.

### F2 — Implement and ship v1

The deliverable after this spec set is approved:

1. `confluence-mcp/` Go module per
   `06-implementation-skeleton/01-file-layout.md`.
2. `Makefile` per
   `06-implementation-skeleton/04-makefile.md` (already
   written at the project root).
3. `project.toml` per
   `07-paketo-buildpack/01-project-toml.md`.
4. `make image` produces a working OCI image per
   `07-paketo-buildpack/02-pack-command.md`.
5. `~/.hermes/config.yaml` snippet from
   `08-deployment-hermes/01-config-yaml.md` registers the
   server.
6. Five tool calls from `hermes chat` exercise the full
   surface per
   `08-deployment-hermes/03-sample-invocation.md`.

### F3 — Submit catalog `manifest.yaml` (gap Q20)

PR against `NousResearch/hermes-agent` to add
`optional-mcps/confluence/manifest.yaml`. ~80 lines of YAML.

### F4 — Verify build with the user's actual `pack` and Docker

The `pack` and `docker` versions on this machine are
`0.40.7+git-2df3b8c.build-6959` and `29.5.2` respectively
(verified at survey time). Both are recent enough for the
Paketo Go buildpack chain. If the implementer's environment
differs significantly, they should:

1. Run `pack --version` and confirm ≥ 0.27.
2. Run `docker --version` and confirm ≥ 20.10.
3. Run `pack builder inspect paketobuildpacks/builder-jammy-tiny`
   to confirm the Go buildpack is included.

### F5 — Cross-verify the v2 endpoint list against the latest Atlassian docs

Atlassian's v2 API surface is GA but evolves. A quarterly
check against
`https://developer.atlassian.com/cloud/confluence/changelog/`
should confirm the endpoint list in
`01-foundations/02-confluence-v2-rest-recap.md` is current.

## VERIFICATION REPORT

Metrics for the spec set as a whole.

| Metric | Value | How measured |
| ------ | ----: | ------------ |
| Spec files (under numbered sub-folders 00-99) | 29 | `find specs -mindepth 2 -name "*.md" -type f -not -path "*/99-gap-questions/*" -not -path "*/research/*" \| wc -l` |
| Total .md files in spec set | 34 | `find specs -name "*.md" -type f \| wc -l` (29 spec + gap-questions + partial-answers + research + README + SOURCES) |
| Total lines | 5,628 | `find specs -name "*.md" -type f -exec wc -l {} + \| tail -1 \| awk '{print $1}'` |
| Total bytes | 216,580 (~212 KB) | `find specs -name "*.md" -type f -exec wc -c {} + \| tail -1 \| awk '{print $1}'` |
| Total URLs cited | 41 (external) + 5 (local skill paths) | manual count from SOURCES.md |
| Gap questions | 22 (Q1-Q22) | `grep -c "^## Q" specs/99-gap-questions/01-questions.md` |
| Locked decisions | 3 (Q10 re-shaped, Q14, Q22) | `grep -c "^## Q" specs/99-gap-questions/02-partial-answers.md` |
| Variant-B files (4-section shape) | 29 | `grep -lE "^## Overview$" specs/**/*.md \| wc -l` |
| README/SOURCES/research/gap exceptions | 5 | README.md + SOURCES.md + research/00-sources-and-caveats.md + 99-gap-questions/01-questions.md + 99-gap-questions/02-partial-answers.md |
| Implementation code files written | 0 | `find . -name "*.go" -type f 2>/dev/null \| wc -l` (no Go source yet) |
| **Project artifacts (Makefile, .gitignore, .env.example)** | **3 files** | `ls {Makefile,.gitignore,.env.example}` |
| **`make help` works** | **✓** | Verified during spec write; renders 20 targets alphabetically sorted |

### How to reproduce

```sh
cd /path/to/confluence-mcp

# Spec file count and size
echo "Files:"
find specs -mindepth 2 -name "*.md" -type f | wc -l
echo "Lines:"
find specs -name "*.md" -type f -exec wc -l {} + | tail -1
echo "Bytes:"
find specs -name "*.md" -type f -exec wc -c {} + | tail -1

# Gap question count
echo "Gap questions:"
grep -c "^## Q" specs/99-gap-questions/01-questions.md

# Variant-B structural check (every spec file must have exactly 4 H2 sections in order)
for f in $(find specs -mindepth 2 -name "*.md" -type f); do
  if [[ "$f" =~ (99-gap-questions/01-questions|99-gap-questions/02-partial-answers|SOURCES|research/00-sources-and-caveats|README) ]]; then continue; fi
  count=$(grep -cE '^## (Overview|Sources|Spec|Verification)$' "$f")
  if [ "$count" -ne 4 ]; then
    echo "FAIL: $f has $count sections (expected 4)"
  fi
done

# Confirm no implementation code (research-only deliverable)
echo "Go files in module:"
find . -name "*.go" -type f 2>/dev/null | wc -l

# Project artifacts
echo "Project artifacts at root:"
ls Makefile .gitignore .env.example 2>/dev/null

# Makefile renders help correctly
make help | head -25
```

### Acceptance status

| Check | Status |
| ----- | ------ |
| Every source URL fetched and recorded | ✓ (41 external URLs + 5 local skills) |
| Every topic has at least one .md file | ✓ (12 numbered folders + research/) |
| `README.md` and `SOURCES.md` exist | ✓ |
| `research/00-sources-and-caveats.md` exists with the four required sections | ✓ (SOURCES / RESEARCH CAVEATS / FOLLOWUP WORK / VERIFICATION REPORT) |
| Every spec file has the four H2 sections (or deviation documented) | ✓ (per the structural grep above; 0 expected failures across 29 files) |
| Gap questions file well-populated | ✓ (22 questions, Q1-Q22; 3 locked in `02-partial-answers.md`) |
| Makefile as single source of truth | ✓ (LOCKED Q14; Makefile renders 20 targets via `make help`; spec file `06-implementation-skeleton/04-makefile.md`) |
| Settings resolution order (env + `.env`) | ✓ (LOCKED Q22; spec section in `01-foundations/03-env-var-contract.md`) |
| Gitter commit in (per global rule) | n/a (no git repo on this host; spec set is delivered as files on disk) |