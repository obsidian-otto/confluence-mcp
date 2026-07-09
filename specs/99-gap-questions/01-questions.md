# 99.1 — Open Design Questions

This file lists every design question that the upstream code +
the Go libraries + the buildpack docs cannot answer. Each
question references the spec files that are affected.

Numbering is stable; please reference **Q-N** when answering.
Reply format: `Q1 → a, Q2 → c, Q3 → custom-answer`. Locked
decisions are recorded in `02-partial-answers.md` (per
gap-decision-log workflow).

---

## Q1 — Single-site vs multi-site per binary

The v1 binary supports **one** Atlassian site per process
(via `ATLASSIAN_SITE_NAME`). A user with multiple Confluence
sites (e.g. one for work, one for personal) would need two
binary instances registered as `mcp_servers.work_confluence`
and `mcp_servers.personal_confluence`.

- **a)** Keep v1 single-site. (Upstream behavior; simplest.)
- **b)** Allow multiple sites via `ATLASSIAN_SITES` (JSON
  array) + tool wrappers per site.
- **c)** Add a new tool `conf_set_site` to switch the active
  site at runtime.

**Affected files:** `01-foundations/03-env-var-contract.md`,
`06-implementation-skeleton/02-main-entrypoint.md`,
`08-deployment-hermes/01-config-yaml.md`.

**Recommendation:** **a** (single-site). Matches upstream;
multi-site is rarely needed in practice; surface a v2 gap if
the user asks.

---

## Q2 — OAuth 2.0 (3LO) support

The upstream only does basic-auth via API token. OAuth 2.0
3LO is the right auth for "access on behalf of a user" but
requires an OAuth flow, callback handling, and token
storage.

- **a)** Defer OAuth 2.0 to v2. v1 is API-token-only.
- **b)** Add OAuth 2.0 in v1.1 using `go-atlassian`'s
  `WithOAuth` + `WithAutoRenewalToken` options.
- **c)** Support both: env-var selects auth mode
  (`ATLASSIAN_AUTH_MODE=basic|oauth`).

**Affected files:** `01-foundations/03-env-var-contract.md`,
`03-go-atlassian/02-auth-options.md`.

**Recommendation:** **a** (defer). 3LO is a non-trivial
undertaking (callback server, refresh-token rotation,
scope catalogue) and the upstream itself doesn't do it.

---

## Q3 — HTTP transport at v1

`mcp-golang` supports `stdio`, HTTP, and Gin transports. The
upstream supports stdio + Streamable HTTP. Hermes Agent
supports both stdio and HTTP MCP servers (per
`~/.hermes/skills/mcp/native-mcp/SKILL.md`).

- **a)** Stdio only at v1 (matches the most common deploy).
- **b)** Add HTTP transport at v1 (allow `TRANSPORT_MODE=http`).
- **c)** Add HTTP at v1.1, gated behind a flag.

**Affected files:** `02-upstream-aashari/01-architecture.md`,
`04-mcp-golang-framework/02-stdio-transport.md`,
`06-implementation-skeleton/02-main-entrypoint.md`.

**Recommendation:** **a** (stdio only). Hermes launches
MCP servers as subprocesses by default; HTTP is a
remote-shared use case the user hasn't asked for.

---

## Q4 — Data Center support

The Go MCP server targets Confluence Cloud exclusively. Data
Center is the self-hosted Atlassian product with a different
auth model (PAT vs API token) and a partly different REST
surface.

- **a)** Defer DC to v2. v1 is Cloud-only.
- **b)** Add DC support at v1.1 via `ATLASSIAN_BASE_URL` env
  var override.
- **c)** Add DC support now.

**Affected files:** `01-foundations/01-cloud-vs-datacenter.md`,
`01-foundations/03-env-var-contract.md`,
`03-go-atlassian/02-auth-options.md`.

**Recommendation:** **a** (defer). The user runs Confluence
Cloud; DC has different endpoints (`/rest/api/` instead of
`/wiki/api/v2/`) and a different auth model.

---

## Q5 — Should the server refuse updates with stale `version.number`?

When the LLM calls `conf_put` with `version.number` lower than
or equal to the current page version, Confluence returns 409.
The LLM then retries. Should the server **detect** this before
sending and return a clearer error?

- **a)** Pass through (matches upstream). LLM gets 409, retries.
- **b)** Server pre-checks `version.number` and returns a
  friendly error before hitting Atlassian.
- **c)** Server has an "auto-increment" mode where it reads
  the current version, increments, and sends.

**Affected files:**
`01-foundations/02-confluence-v2-rest-recap.md`,
`05-tool-surface-design/01-tool-mapping.md`.

**Recommendation:** **a** (pass through). Pre-checking
adds a round-trip and breaks the generic-CRUD-wrapper shape;
the 409 is the upstream's behavior and the LLM handles it
fine.

---

## Q6 — Auto-paginate on `limit > default`?

v2 endpoints are cursor-paginated (default `limit=25`). If
the LLM passes `limit: "500"`, should the server follow
cursors and concatenate results?

- **a)** Single-page pass-through. Caller paginates.
- **b)** Auto-paginate up to N pages.
- **c)** Auto-paginate up to N pages with a single `jq` filter
  applied across all pages.

**Affected files:**
`01-foundations/02-confluence-v2-rest-recap.md`,
`05-tool-surface-design/01-tool-mapping.md`.

**Recommendation:** **a** (single-page). Matches upstream;
the LLM is good at pagination and the 40k truncation kicks
in before huge pages matter.

---

## Q7 — Implement 429 retry with exponential backoff?

Cloud rate limits are points-based (hourly quota); the 429
response includes `Retry-After`. The upstream does NOT retry.

- **a)** No retry. LLM gets 429 and decides.
- **b)** Retry once after `Retry-After` seconds.
- **c)** Exponential backoff up to 3 retries.

**Affected files:**
`01-foundations/02-confluence-v2-rest-recap.md`.

**Recommendation:** **a** (no retry). Matches upstream;
Hermes' tool call timeout (60s) makes retry less useful;
additive complexity for an edge case.

---

## Q8 — Attachment upload support

v2 attachments-create is not yet exposed; the v1 attachments
endpoint (`/wiki/rest/api/content/{id}/child/attachment`)
still works for multipart upload.

- **a)** No attachment upload at v1. v1 supports only GET,
  DELETE on attachments.
- **b)** Add v1 multipart upload via raw HTTP.
- **c)** Add v1.1 attachment upload with a typed Go service.

**Affected files:**
`01-foundations/02-confluence-v2-rest-recap.md`,
`04-mcp-golang-framework/03-content-types.md`.

**Recommendation:** **a** (no upload). Attachment upload is
rarely the use case for an MCP server (LLMs don't usually
attach files); the v1 endpoint requires multipart parsing
which adds dependency complexity.

---

## Q9 — CLI mode

The upstream has a CLI mode (`npx @aashari/mcp-server-atlassian-confluence
get --path /...`). The Go port could mirror this for
terminal use without an MCP client.

- **a)** No CLI at v1. Use the MCP binary for MCP; if you want
  CLI, build a thin wrapper later.
- **b)** Add a CLI mode at v1 (dispatch on argv[1]).

**Affected files:** `02-upstream-aashari/01-architecture.md`,
`06-implementation-skeleton/02-main-entrypoint.md`.

**Recommendation:** **a** (no CLI). The use case is
MCP-via-stdio. A CLI is a v1.1 nice-to-have.

---

## Q10 — Log file path (vs stderr-only)

> **LOCKED 2026-07-09:** User directed that the MCP server
> must load settings from environment variables **or** a
> `.env` file. This re-shapes Q10 (the log-file question is
> now folded into the `.env` decision: stderr is still the
> log channel, but a `.env` file replaces the upstream's
> `~/.mcp/configs.json` tier). See
> `99-gap-questions/02-partial-answers.md` Q10 for the lock
> rationale.

Upstream logs to `~/.mcp/data/*.log`. The Go port logs to
stderr only.

- **a)** Stderr only (current decision).
- **b)** Add `--log-file PATH` flag for opt-in file logging.
- **c)** Always log to `~/.local/state/mcp-confluence/server.log`
  + stderr.

**Affected files:**
`02-upstream-aashari/03-lessons-and-quirks.md`,
`04-mcp-golang-framework/02-stdio-transport.md`,
`06-implementation-skeleton/02-main-entrypoint.md`.

**Recommendation:** **a** (stderr only). Matches the upstream
behavior for the `DEBUG=false` case; simpler; no filesystem
state.

**Locked answer:** **a** (stderr only). The `.env` decision
(Q22 in the locked decision log) does **not** change this —
the `.env` is for **config**, not for logs.

---

## Q11 — Custom HTTP client (timeout, proxy, retry)

`confluence.New(nil, ...)` uses `http.DefaultClient`. Should
v1 configure a custom client with timeouts / proxy?

- **a)** Use `http.DefaultClient` (matches upstream's
  effective behavior).
- **b)** Configure a 30s timeout on the HTTP client.
- **c)** Configure timeout + proxy from env vars
  (`HTTP_PROXY`, `HTTPS_PROXY`).

**Affected files:**
`03-go-atlassian/02-auth-options.md`,
`06-implementation-skeleton/02-main-entrypoint.md`.

**Recommendation:** **c**. `HTTPS_PROXY` is a standard env
var; the Go stdlib `http.ProxyFromEnvironment` honors it
automatically. A 30s timeout on the HTTP client prevents
hung connections from blocking a tool call indefinitely
(Hermes' 60s timeout is the outer bound).

---

## Q12 — Image / audio / embedded-resource content

`mcp-golang` supports image / audio / embedded-resource
content types. The upstream is text-only. The Go port
follows suit.

- **a)** Text content only at v1.
- **b)** Add image content for page attachments (preview).

**Affected files:**
`04-mcp-golang-framework/03-content-types.md`.

**Recommendation:** **a** (text only). Attachment
browse/preview is not in the upstream; v2 feature.

---

## Q13 — One shared `ConfWriteArgs` type vs three

`ConfPostArgs`, `ConfPutArgs`, `ConfPatchArgs` are
identical. The Go port could use one type or three.

- **a)** Three separate types (current decision; matches
  upstream's `RequestWithBodyArgs` per-method).
- **b)** One shared `ConfWriteArgs` type reused for all
  three write methods.

**Affected files:**
`05-tool-surface-design/01-tool-mapping.md`,
`06-implementation-skeleton/03-tool-handlers.md`.

**Recommendation:** **a** (three types). Matches upstream;
the per-tool `jsonschema` is generated from the struct, and
three types give three slightly different `description`
fields.

---

## Q14 — Makefile vs `go build` only

> **LOCKED 2026-07-09:** User explicitly required a Makefile
> as the single source of truth for all commands, per the
> `project` skill rules (`~/.hermes/skills/project/project/`).
> The Makefile is now part of v1, not v1.1. See
> `99-gap-questions/02-partial-answers.md` Q14 for the lock.

The Go MCP server has a few common commands (`build`, `test`,
`run`, `image`). A Makefile standardizes these.

- **a)** No Makefile at v1; document commands in README.
- **b)** Add a Makefile with `build`, `test`, `image`,
  `run`, `clean` targets.

**Affected files:**
`06-implementation-skeleton/01-file-layout.md`.

**Recommendation:** **b** (add Makefile).

**Locked answer:** **b** with the full target set documented
in `06-implementation-skeleton/04-makefile.md` (`help`,
`install`, `clean`, `build`, `test`, `lint`, `format`,
`type-check`, `security`, `check`, `run`, `dev`, `image`,
`image-inspect`, `sbom`, `verify-env`, `verify-tools`,
`info`, `locate-bin`, `all`).

---

## Q15 — CI / GitHub Actions

CI builds the image, runs tests, signs the SBOM.

- **a)** No CI at v1; document `make image` invocation
  for users.
- **b)** Add GitHub Actions workflow that builds the image
  on every push to main and pushes to ghcr.io.

**Affected files:**
`06-implementation-skeleton/01-file-layout.md`,
`07-paketo-buildpack/02-pack-command.md`.

**Recommendation:** **b**. CI is small (~30 lines of YAML)
and provides reproducibility + free SBOM history.

---

## Q16 — `--version` flag

The `mcp-confluence` binary currently has no `--version`
flag. The version is set via `-ldflags` but only visible to
the MCP `initialize` response.

- **a)** No `--version` flag; version is internal.
- **b)** Add `--version` that prints to stdout and exits.
- **c)** Add `--version` and `--help`.

**Affected files:**
`06-implementation-skeleton/02-main-entrypoint.md`.

**Recommendation:** **c**. Cheap, standard CLI convention.

---

## Q17 — Builder SHA pinning

For reproducible CI builds, the builder image is pinned by
SHA digest.

- **a)** Use the tag `paketobuildpacks/builder-jammy-tiny`
  (latest).
- **b)** Pin the SHA digest in `infra/pinned-digests.txt`.

**Affected files:**
`07-paketo-buildpack/02-pack-command.md`.

**Recommendation:** **b**. Pin for CI; allow tag for local
dev.

---

## Q18 — Multi-arch builds (linux/arm64)

Paketo supports `linux/amd64` and `linux/arm64`. v1 is
amd64 only (matches the user's hardware).

- **a)** amd64 only at v1.
- **b)** Add arm64 support via `pack build --platform
  linux/amd64,linux/arm64`.

**Affected files:**
`07-paketo-buildpack/02-pack-command.md`.

**Recommendation:** **a** (amd64 only).

---

## Q19 — Shared / remote deployment (HTTP mode + auth)

The catalog manifest's binary-only install assumes local
deployment. For a **shared / team** Confluence server, the
binary would run on a remote host and Hermes would connect
via HTTP.

- **a)** Local only at v1.
- **b)** Add HTTP transport + bearer-token auth at v1.1.

**Affected files:**
`08-deployment-hermes/01-config-yaml.md`,
`08-deployment-hermes/02-manifest-yaml.md`.

**Recommendation:** **a**. Shared deployment is a v2
concern; HTTP transport is gated on Q3.

---

## Q20 — Catalog `manifest.yaml` submission

The `optional-mcps/confluence/manifest.yaml` lives in the
hermes-agent repo. Submitting it requires a PR.

- **a)** Don't submit to the catalog. Distribute via the
  `mcp-confluence` repo's README instead.
- **b)** Submit to the catalog at v1.

**Affected files:**
`08-deployment-hermes/02-manifest-yaml.md`.

**Recommendation:** **b** if the user wants one-click
installs; **a** otherwise.

---

## Q21 — Test coverage reporting

The project skill lists `coverage` as a strongly-recommended
quality target. The current Makefile spec includes `test`
but not `coverage`.

- **a)** Skip `coverage` at v1; add at v1.1.
- **b)** Add a `coverage` target that runs `go test -coverprofile`
  and prints a textual summary (`go tool cover -func`).
- **c)** Add `coverage` + HTML report (`go tool cover -html`)
  + badge for the README.

**Affected files:** `06-implementation-skeleton/04-makefile.md`.

**Recommendation:** **b**. Coverage is useful for spotting
uncovered code paths but the HTML report is overkill for a
v1 MCP server (the project skill says "strongly
recommended", not "required").

---

## Q22 — `.env` file loading (LOCKED 2026-07-09)

User confirmed: "the MCP server should load it's settings
from the environmental variables **or** from the `.env` file
inside the container or cli". See
`99-gap-questions/02-partial-answers.md` Q22 for the lock
rationale and the resolved priority order.

This question is locked (see `02-partial-answers.md`).

**Affected files:** `01-foundations/03-env-var-contract.md`
(Settings resolution order section),
`02-upstream-aashari/03-lessons-and-quirks.md`,
`06-implementation-skeleton/01-file-layout.md`
(`dotenv.go` file), `06-implementation-skeleton/02-main-entrypoint.md`,
`06-implementation-skeleton/04-makefile.md`.

---

## How to answer

Reply with `Q-N → option letter` (e.g. `Q1 → a, Q2 → a,
Q3 → a`) or `Q-N → custom-answer` for free text. Each locked
answer will be appended to `02-partial-answers.md` and the
relevant spec files will be updated with a `LOCKED <date>`
marker.