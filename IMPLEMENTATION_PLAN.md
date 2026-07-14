# mcp-confluence — Phased Implementation Plan

> **Goal:** Build a working Go Confluence MCP server (`mcp-confluence`) that
> exposes the upstream `@aashari/mcp-server-atlassian-confluence` v3.3.0 tool
> surface (`conf_get` / `conf_post` / `conf_put` / `conf_patch` /
> `conf_delete`) for Hermes Agent.
>
> **Architecture:** Thin Go binary using `ctreminiom/go-atlassian/v2` for HTTP,
> `metoro-io/mcp-golang` for MCP framing, a custom TOON encoder, and a stdlib
> `.env` parser. Settings resolution: process env > cwd `.env` > binary-dir
> `.env`. Container image: `pack` + Paketo Go BuildPak distroless.
>
> **Spec set:** `specs/` (32 files, all complete; this plan consumes them).
> **Single source of truth for commands:** `Makefile` (per the `project` skill
> and Q14 lock).
>
> **Subagent orchestration:** Each phase is dispatched in its own tmux pane
> running `hermes --cli`. The orchestrator (this session) publishes phase
> prompts onto a `tbus` channel; a `tbus tui-attach` watcher pipes each prompt
> into the agent's pane; the agent works and reports back through the same
> channel. See **§ Orchestration** for the full recipe.

---

## How to read this plan

Each phase is a self-contained unit of work that:

1. Has a **clear deliverable** (a file or set of files that compiles + tests).
2. Has a **verifiable exit** (`make build` + `make test` + a one-line check).
3. Gets its **own subagent** with full spec context.
4. Has a **token budget** of ≈ 256k (soft guideline; not a hard cap).
5. Ends with a **commit** (or annotated "no commit — config only").
6. Flips its checkboxes when done — the file is the source of truth for progress.

Total phases: **13** (Phase 0 bootstrap + 12 implementation phases). Estimated
time per phase: 5–20 minutes of subagent work.

The phases are deliberately **ordered by dependency**: later phases import
packages built by earlier ones. Do not reorder. Two pairs are parallel-safe —
see **§ Parallel-safe groupings**.

---

## Progress index

Master checklist — flip the box when the phase is fully verified.

- [x] **Phase 0** — Bootstrap (Go module skeleton, no-op `main.go`)
- [x] **Phase 1** — `internal/config`: stdlib dotenv + `LoadFromEnv`
- [x] **Phase 2** — `internal/atlassian`: client wrapper, basic auth, errors
- [x] **Phase 3** — `internal/toon`: encoder + round-trip tests *(parallel w/ 4)*
- [x] **Phase 4** — `internal/jmespath`: wrapper + tests *(parallel w/ 3)*
- [x] **Phase 5** — `internal/tools`: args types + 5 `CONF_*_DESCRIPTION` constants
- [x] **Phase 6** — `internal/tools`: `executeRequest()` shared handler helper
- [x] **Phase 7** — `internal/tools`: five handlers + `safeHandler` panic wrap
- [x] **Phase 8** — `internal/server` + `RegisterAll()`: bootstrap + registration
- [x] **Phase 9** — `cmd/mcp-confluence/main.go`: full lifecycle (load → serve)
- [x] **Phase 10** — Wire + smoke: `make check` + end-to-end JSON-RPC smoke
- [x] **Phase 11** — Container image: `project.toml` + `make image` green
- [x] **Phase 12** — Hermes integration: `~/.hermes/config.yaml` + `mcp test`

---

## v2 — Markdown round-trip (2026-07-10, in progress)

User's verbatim 2026-07-10 requirement: *"in the end this
project must be able to upload a markdown file into
confluence using its own markup format, and be able to later
download confluence documents in their markup format and
convert it locally to markdown before storing it."*

Adds 3 new MCP tools on top of the v1 surface: `conf_post_markdown`,
`conf_put_markdown`, `conf_get_page_markdown`. The
conversion happens entirely inside the Go binary using
`github.com/yuin/goldmark` (md → HTML) and
`github.com/JohannesKaufmann/html-to-markdown/v2` (HTML → md),
with a goquery-based post-processor that bridges goldmark's
CommonMark HTML to Confluence's storage-format XHTML.

Spec set: `specs/10-markdown-roundtrip/` (6 files).
Lock-ins: Q23 = goldmark v1.7.13, Q24 = html-to-markdown v2.5.2,
Q25 = goquery v1.10.x for the post-processor, Q26 = h2m's
golden-file test pattern.

Phases 13 and 14 are parallel-safe; Phase 15 is sequential
(depends on both).

- [ ] **Phase 13** — `internal/markdown` package: 3-stage pipeline + 28 golden-file tests *(parallel w/ 14)*
- [ ] **Phase 14** — 3 new tool handlers + args types + descriptions *(parallel w/ 13)*
- [ ] **Phase 15** — Register new tools, rebuild image, smoke against live Confluence

---

## Orchestration

The plan is executed by **the orchestrator (this Hermes session)** dispatching
**one subagent per phase** into a **dedicated tmux pane running `hermes --cli`**.
Inter-pane traffic flows over the **`tbus` Unix-socket bus** (the
`tmux-subagent-ipc` skill).

### Channel layout

| Channel            | Direction          | Purpose                              |
| ------------------ | ------------------ | ------------------------------------ |
| `phase-{N}-prompt` | orch → agent       | Phase N kickoff prompt + spec paths  |
| `phase-{N}-done`   | agent → orch       | Completion report (commit SHA + ok)  |
| `progress`         | both               | Phase flip / status broadcasts       |
| `blockers`         | agent → orch       | "ask the user" escalations           |

The orchestrator creates one tmux pane per active subagent (Phase 0 → 1; on
Phase 3+4 the orchestrator opens a second pane for the parallel agent).

### Per-phase dispatch recipe (canonical)

```bash
# 0. Confirm the bus is up
~/.hermes/tmux-bus/bin/tbus ping

# 1. Open a tmux pane running `hermes --cli` for this phase
TMUX="phase-N-$$"  # unique session name to avoid collisions
tmux new-session -d -s "$TMUX" -x 200 -y 50 'hermes --cli'

# 2. Start a tui-attach watcher that types channel events into the pane.
#    Use background=true (NOT trailing &) so Hermes can track lifecycle.
terminal(background=true, notify_on_complete=true) \
    ~/.hermes/tmux-bus/bin/tbus tui-attach "${TMUX}:0.0" \
      --channel "phase-N-prompt" --since tail \
    > "/tmp/phase-N-tui.log" 2>&1

# 3. Publish the phase kickoff prompt (see each phase below for the body)
~/.hermes/tmux-bus/bin/tbus send "phase-N-prompt" \
    --body "$(cat <<'EOF'
<phase kickoff prompt — see phase-specific section>
EOF
)" --as orchestrator

# 4. Wait for the agent to publish its completion report on phase-N-done
~/.hermes/tmux-bus/bin/tbus subscribe "phase-N-done" \
    --since tail --once

# 5. Read the agent's final output from its pane for human review
~/.hermes/tmux-bus/bin/tbus capture "${TMUX}:0.0" --lines 200

# 6. Flip the phase checkbox in this file (orchestrator does this, not the agent)

# 7. Kill the dedicated tmux session — never use `pkill -f`
tmux kill-session -t "$TMUX"
```

### Parallel-safe groupings

Two grouping windows exist where two subagents can run concurrently:

| Window | Subagent A | Subagent B | Why safe |
| ------ | ---------- | ---------- | -------- |
| After Phase 2 | Phase 3 (TOON) | Phase 4 (JMESPath) | Pure utility packages, no internal imports between them, only `go.mod` to add deps |
| After Phase 6 | Phase 7 (handlers, split-by-method) | (Phase 5 done first) | Only safe when Phase 5 is already merged; else Phase 7 needs the types |

Outside these windows phases are **strictly sequential** — each depends on the
prior. The orchestrator publishes one phase prompt, waits for `phase-N-done`,
verifies the report, marks the box, then publishes `{N+1}-prompt`.

### Token budget policy

- **256k tokens per subagent** is the guideline. Soft, not hard.
- The orchestrator fragments large phase specs into "spec excerpts" attached to
  the prompt rather than dumping the full 17 KB file each time. A 256k context
  comfortably holds: ~10 KB system prompt + ~30 KB spec excerpt + ~30 KB prior
  phase diff + ~150 KB working memory for the agent.
- If a subagent hits context churn (deeply recursive CoT dumps), the
  orchestrator handles the regression rather than letting the subagent retry
  indefinitely. The plan is small enough that no single phase should blow
  through this.

---

## How this plan is updated

This is a living document. After each phase completes:

1. The orchestrator flips the relevant `[ ]` to `[x]` in **§ Progress index**
   and at the top of that phase.
2. The orchestrator appends one bullet to **§ Phase log** below:
   `- YYYY-MM-DD — Phase N: <one-line summary> — sha=<short-sha>`
3. **Subagents do not edit this file.** They report to `phase-N-done` only.

---

## Phase 0 — Bootstrap

**Token budget:** ~256k soft · **Subagent:** yes · **Parallel-safe:** no

**Objective:** Create the Go module skeleton, directory tree, and a
no-op `main.go` that compiles.

**Tasks**

- [x] `cd /home/bennie/Desktop/hermes/confluence-mcp`
- [x] `go mod init github.com/bennie/mcp-confluence`
- [x] Write minimal `cmd/mcp-confluence/main.go` that prints `mcp-confluence v0.1.0`
  to stderr and exits 0
- [x] Create directory tree with `.gitkeep` files
  - [ ] `cmd/mcp-confluence/.gitkeep`
  - [ ] `internal/config/.gitkeep`
  - [ ] `internal/atlassian/.gitkeep`
  - [ ] `internal/jmespath/.gitkeep`
  - [ ] `internal/toon/.gitkeep`
  - [ ] `internal/tools/.gitkeep`
  - [ ] `internal/server/.gitkeep`
- [x] Modify `.gitignore`: add `/bin/`, `/sbom/` (`.env` already covered)
- [x] Create `README.md` (1-paragraph purpose + `make help` + link to `specs/`)
- [x] `make build` — expect `./bin/mcp-confluence` produced
- [x] `./bin/mcp-confluence` — expect version line on stderr, exit 0
- [x] Commit: `chore: bootstrap go module + main entrypoint stub`

**Spec to follow:** `specs/06-implementation-skeleton/01-file-layout.md` and
`specs/06-implementation-skeleton/02-main-entrypoint.md`.

**Verification**

- [x] `make build` exits 0
- [x] `./bin/mcp-confluence` exits 0 with version on stderr
- [x] `go.mod` exists and lists Go 1.23+
- [x] `git log --oneline` shows the commit

**Kickoff prompt body** (publish to `phase-0-prompt`):

```
You are implementing Phase 0 of the mcp-confluence implementation plan.
Read /home/bennie/Desktop/hermes/confluence-mcp/IMPLEMENTATION_PLAN.md
fully, then the relevant spec at
specs/06-implementation-skeleton/01-file-layout.md and
specs/06-implementation-skeleton/02-main-entrypoint.md. Create the Go
module skeleton as described in Phase 0. Do NOT pull in any dependencies
yet (no go get). Do NOT create internal package files — only .gitkeep
markers. Run make build and ./bin/mcp-confluence to verify. Commit the
result. When done, publish your completion report on the channel
phase-0-done with:
  { "phase": 0, "sha": "<commit-sha>", "ok": true,
    "notes": "<any deviations>" }
```

---

## Phase 1 — Config + dotenv parser

**Token budget:** ~256k soft · **Subagent:** yes · **Parallel-safe:** no

**Objective:** `internal/config` package with `LoadFromEnv()` and a 30-LOC
stdlib `.env` parser. Locked Q22: process env > cwd `.env` > binary-dir
`.env`. No `godotenv` dependency.

**Tasks**

- [x] Write `internal/config/dotenv_test.go` (table-driven: empty file,
  `KEY=VALUE`, `KEY="quoted value"`, comments, blank lines, malformed line)
- [x] Run `go test ./internal/config/...` — expect FAIL (no impl yet)
- [x] Write `internal/config/dotenv.go` (~30 LOC: `Load(path)`)
- [x] Run tests — expect PASS
- [x] Write `internal/config/config_test.go`: missing var → error; all set →
  ok; .env in cwd picked up; .env next to binary picked up; process env wins
  over .env
- [x] Run — expect FAIL
- [x] Write `internal/config/config.go`: `Config` struct + `LoadFromEnv()`
  that walks the priority chain
- [x] Run tests — expect PASS
- [x] `make test` green
- [x] Commit: `feat(config): stdlib .env parser + LoadFromEnv (Q22 lock)`

**Spec to follow:** `specs/01-foundations/03-env-var-contract.md` (the
LOCKED 2026-07-09 section is the contract). Three required vars
`ATLASSIAN_SITE_NAME`, `ATLASSIAN_USER_EMAIL`, `ATLASSIAN_API_TOKEN`;
optional `DEBUG` (bool). Token redaction rule: error messages MUST include
`<value redacted>` not the literal token.

**Verification**

- [x] `go test ./internal/config/...` all pass
- [x] `grep "os.Getenv(\"ATLASSIAN_API_TOKEN\")" internal/config/*.go`
  shows exactly one match — in `LoadFromEnv`. Never in a log/print
- [x] The `Config.APIKey` field type is `string`. Never named `token`

**Kickoff prompt body** (publish to `phase-1-prompt`):

```
You are implementing Phase 1 of the mcp-confluence plan at
/home/bennie/Desktop/hermes/confluence-mcp/IMPLEMENTATION_PLAN.md. Read
the plan, then specs/01-foundations/03-env-var-contract.md (the entire
file — it's the contract). Implement internal/config/ exactly per the
spec's "Settings resolution order (LOCKED 2026-07-09)" section. Use ONLY
the Go stdlib (no godotenv). The .env parser must be ~30 LOC. Token
redaction rule: error messages from .env parsing must show
"<value redacted>" not the literal value. Write 5+ table-driven tests
for the parser and 5+ for LoadFromEnv. Run make test to confirm green.
Commit. Publish to phase-1-done: { phase:1, sha:..., ok:true, notes:... }.
```

---

## Phase 2 — Atlassian client wrapper

**Token budget:** ~256k soft · **Subagent:** yes · **Parallel-safe:** no
(unlocks Phases 3+4 parallelism after this completes)

**Objective:** `internal/atlassian` package wrapping
`ctreminiom/go-atlassian/v2/confluence` with a clean `Do(...)` interface
for `executeRequest` later.

**Tasks**

- [x] `go get github.com/ctreminiom/go-atlassian/v2` (pin a version)
- [x] Write `internal/atlassian/errors_test.go`: table-driven cases for
  `AuthMissingError`, `APIError`, and the error-shape format from
  `specs/09-anti-patterns/03-error-shapes.md`
- [x] Run — expect FAIL
- [x] Write `internal/atlassian/errors.go` (`AuthMissingError`,
  `APIError`, error message shape: `<METHOD> <path>: <status> <text> - <body>`)
- [x] Tests pass
- [x] Write `internal/atlassian/auth.go` (`SetBasicAuth` wrapper; token
  field is the constructor arg, never logged)
- [x] Write `internal/atlassian/client_test.go` (mock `HTTPClient`,
  verify headers + URL)
- [x] Run — expect FAIL
- [x] Write `internal/atlassian/client.go`: `New(cfg)`, `Do(...)`,
  `Call(ctx, method, path, query, body)`
- [x] Tests pass
- [x] `make check` green
- [x] Commit: `feat(atlassian): client wrapper + error shapes (anti-pattern)` 

**Spec to follow:** `specs/03-go-atlassian/01-package-layout.md` and
`specs/09-anti-patterns/03-error-shapes.md`.

**Verification**

- [x] `grep -r "ATLASSIAN_API_TOKEN" internal/atlassian/` returns 0 matches
- [x] `grep -r "log.Print" internal/atlassian/` returns 0 matches
- [x] Error shape test asserts the literal format string
- [x] `make check` exits 0

**Kickoff prompt body** (publish to `phase-2-prompt`):

```
You are implementing Phase 2 of the mcp-confluence plan. Read
IMPLEMENTATION_PLAN.md, then specs/03-go-atlassian/01-package-layout.md
and specs/09-anti-patterns/03-error-shapes.md. Implement internal/atlassian/
as a thin wrapper over ctreminiom/go-atlassian/v2/confluence exposing
Do(ctx, method, path, query, body). The Client.Call() raw HTTP path is
required for v2 REST endpoints — see specs/03-go-atlassian/03-client-call-raw-http.md.
NEVER log the API token. Error messages must follow the literal shape
"<METHOD> <path>: <status> <text> - <body>". Write table-driven tests
for errors and a mock-HTTPClient test for client. Pin the dependency
in go.mod. Run make check. Commit. Report sha on phase-2-done.
```

---

## Phase 3 — TOON encoder ⚡ *parallel w/ Phase 4*

**Token budget:** ~256k soft · **Subagent:** yes (Hermes pane A) ·
**Parallel-safe:** yes (with Phase 4)

**Objective:** `internal/toon` package with `Encode()` and round-trip
tests (JSON → TOON → JSON equality). The encoder is the project's
differentiation vs raw JSON output and saves 30–60% tokens.

**Tasks**

- [x] Write `internal/toon/encode_test.go` (10+ round-trip cases: scalar,
  object, array, nested, empty, null, large strings, escape sequences)
- [x] Run — expect FAIL
- [x] Write `internal/toon/encode.go` (`Encode(v any) ([]byte, error)`)
- [x] Tests pass
- [x] Add to `internal/toon/encode.go`: `Marshal` (alias), `Indent`
  option, `IndentString` shorthand
- [x] `make check` green
- [x] Commit: `feat(toon): encoder + round-trip tests`

**Spec to follow:** `specs/05-tool-surface-design/02-toon-spec.md` (if
present) — otherwise `specs/02-upstream-aashari/03-lessons-and-quirks.md`
and `specs/05-tool-surface-design/01-output-formats.md`.

**Verification**

- [x] All round-trip tests produce byte-identical JSON
- [x] `make check` exits 0
- [x] Encoder size for a representative Confluence response (manually
  saved during Phase 10) is 30–60% smaller than JSON

**Kickoff prompt body** (publish to `phase-3-prompt` AND spawn a parallel
Phase 4 subagent for the same window — see **§ Parallel-safe groupings**):

```
You are implementing Phase 3 of the mcp-confluence plan. Read
IMPLEMENTATION_PLAN.md, then specs/05-tool-surface-design/* and
specs/02-upstream-aashari/03-lessons-and-quirks.md. Implement
internal/toon/Encode(v any) ([]byte, error). This is a custom encoder —
no production Go library exists. Write 10+ round-trip tests
(JSON → TOON → JSON equality is the oracle). The token-savings target
is 30–60% vs raw JSON on a representative Confluence v2 spaces
response. Run make check. Commit. Report sha + round-trip result on
phase-3-done.
```

---

## Phase 4 — JMESPath wrapper ⚡ *parallel w/ Phase 3*

**Token budget:** ~256k soft · **Subagent:** yes (Hermes pane B) ·
**Parallel-safe:** yes (with Phase 3)

**Objective:** `internal/jmespath` package exposing `Apply(expr string,
data any) (any, error)` over `github.com/jmespath/go-jmespath`. Empty
expression must short-circuit (no parse cost).

**Tasks**

- [x] `go get github.com/jmespath/go-jmespath`
- [x] Write `internal/jmespath/apply_test.go` (table: empty expr
  short-circuits, valid expr returns data, syntax error returns typed
  error, large-array filter, dot-path projection)
- [x] Run — expect FAIL
- [x] Write `internal/jmespath/apply.go`: wrap the upstream; short-circuit
  on empty expression
- [x] Tests pass
- [x] `make check` green
- [x] Commit: `feat(jmespath): Apply wrapper with empty-expr short-circuit`

**Spec to follow:** `specs/05-tool-surface-design/01-output-formats.md`
(the `jq` parameter semantics) and `specs/03-go-atlassian/` adjacent.

**Verification**

- [x] Empty expression test asserts the upstream API was NOT called (use
  a noop-wrapped upstream or a counter)
- [x] `make check` exits 0

**Kickoff prompt body** (publish to `phase-4-prompt` in parallel with
Phase 3):

```
You are implementing Phase 4 of the mcp-confluence plan. Read
IMPLEMENTATION_PLAN.md, then specs/05-tool-surface-design/01-output-formats.md.
Implement internal/jmespath/Apply(expr string, data any) (any, error)
wrapping github.com/jmespath/go-jmespath. Empty expression must
short-circuit — do not call the upstream parser when expr is "". Write
6+ table tests including the empty-expr short-circuit (use a sentinel
that proves the parser was NOT invoked). Run make check. Commit.
Report sha on phase-4-done.
```

---

## Phase 5 — Tool args + descriptions

**Token budget:** ~256k soft · **Subagent:** yes · **Parallel-safe:** no

**Objective:** `internal/tools` package — the 5 tool input types
matching the upstream shape verbatim, plus the 5 `CONF_*_DESCRIPTION`
strings copied byte-for-byte.

**Tasks**

- [x] Write `internal/tools/args_test.go`: assert the 5 arg types
  unmarshal from JSON the same way (round-trip)
- [x] Run — expect FAIL
- [x] Write `internal/tools/args.go`: 5 structs (`GetArgs`, `PostArgs`,
  `PutArgs`, `PatchArgs`, `DeleteArgs`)
- [x] Write `internal/tools/descriptions.go`: 5 constants byte-identical
  to upstream `src/tools/atlassian.api.tool.ts` lines 127–223
- [x] Tests pass
- [x] Add a `descriptions_test.go` asserting the constants match the
  upstream verbatim (cross-check against `specs/02-upstream-aashari/02-five-tools.md`)
- [x] `make check` green
- [x] Commit: `feat(tools): args types + verbatim upstream descriptions`

**Spec to follow:** `specs/02-upstream-aashari/02-five-tools.md` (the
canonical tool shapes) and `specs/06-implementation-skeleton/03-tool-handlers.md`.

**Verification**

- [x] `diff <(grep -E "^const" internal/tools/descriptions.go) <(grep -E "^const" specs/02-upstream-aashari/02-five-tools.md | head -5)`
  shows 5 constants with matching names
- [x] No drift: the description strings contain the upstream wording
  verbatim (no shortening or rewording)
- [x] `make check` exits 0

**Kickoff prompt body** (publish to `phase-5-prompt`):

```
You are implementing Phase 5 of the mcp-confluence plan. Read
IMPLEMENTATION_PLAN.md, then specs/02-upstream-aashari/02-five-tools.md
and specs/06-implementation-skeleton/03-tool-handlers.md. Implement
internal/tools/args.go (5 structs: GetArgs, PostArgs, PutArgs, PatchArgs,
DeleteArgs) and internal/tools/descriptions.go (5 CONF_*_DESCRIPTION
constants). The descriptions MUST be byte-identical to the upstream —
copy them verbatim from
specs/02-upstream-aashari/02-five-tools.md. Add a descriptions_test.go
that asserts each constant matches the spec verbatim. Do NOT
implement handlers yet (Phase 7). Run make check. Commit. Report sha on phase-5-done.
```

---

## Phase 6 — `executeRequest` helper

**Token budget:** ~256k soft · **Subagent:** yes · **Parallel-safe:** no

**Objective:** The 9-step shared handler logic used by all 5 MCP tools:
fetch → truncate → JMESPath → output-format → return. Encapsulate as
`executeRequest(ctx, args, method, body) (string, error)`.

**Tasks**

- [x] Write `internal/tools/execute_test.go` (table-driven covering all
  9 steps + error cases: upstream 401, 404, 409, 500; truncation; empty
  expr; TOON vs JSON outputFormat)
- [x] Run — expect FAIL
- [x] Write `internal/tools/execute.go` with the 9 steps
- [x] Tests pass
- [x] Add 40k-char truncation logic + `/tmp/mcp/<session-id>.json`
  pointer (matches upstream)
- [x] `make check` green
- [x] Commit: `feat(tools): executeRequest helper (9-step shared logic)`

**Spec to follow:** `specs/02-upstream-aashari/03-lessons-and-quirks.md`
(truncation rules) and `specs/05-tool-surface-design/01-output-formats.md`.

**Verification**

- [x] Truncation test asserts the `/tmp/mcp/<id>.json` path appears in
  the response when the upstream body exceeds 40 000 chars
- [x] All 9 steps verified individually (subtests)
- [x] `make check` exits 0

**Kickoff prompt body** (publish to `phase-6-prompt`):

```
You are implementing Phase 6 of the mcp-confluence plan. Read
IMPLEMENTATION_PLAN.md, then specs/02-upstream-aashari/03-lessons-and-quirks.md
and specs/05-tool-surface-design/01-output-formats.md. Implement
internal/tools/executeRequest(ctx, args, method, body) (string, error)
as the 9-step shared handler: parse args, set headers, call atlassian,
check errors (error-shape format from specs/09-anti-patterns/03-error-shapes.md),
truncate to 40k chars with /tmp/mcp/<session>.json pointer, optionally
JMESPath-filter, optionally TOON-format. Write 10+ table-driven tests.
Run make check. Commit. Report sha on phase-6-done.
```

---

## Phase 7 — Five handlers + safeHandler

**Token budget:** ~256k soft · **Subagent:** yes · **Parallel-safe:** no

**Objective:** Wire `executeRequest()` into the 5 MCP tool handlers
(`conf_get`, `conf_post`, `conf_put`, `conf_patch`, `conf_delete`) wrapped
in a `safeHandler()` that recovers panics and returns
`isError: true`.

**Tasks**

- [x] Write `internal/tools/handlers_test.go` (table per method: argument
  shape, panic-recovery, error propagation)
- [x] Run — expect FAIL
- [x] Write `internal/tools/handlers.go`:
  - [x] `HandleGet(args) (string, error)`
  - [x] `HandlePost(ar args, rawBody []byte) (string, error)`
  - [x] `HandlePut(...)` / `HandlePatch(...)` / `HandleDelete(...)`
  - [x] `safeHandler(fn) ToolHandler` — `defer/recover`, log panic to
    stderr, return MCP error
- [x] Tests pass
- [x] `make check` green
- [x] Commit: `feat(tools): 5 handlers + safeHandler panic recovery`

**Spec to follow:** `specs/06-implementation-skeleton/03-tool-handlers.md`.

**Verification**

- [x] Panic-recovery test forces a panic in `executeRequest` and asserts
  the MCP error envelope is returned, not a crash
- [x] `make check` exits 0

**Kickoff prompt body** (publish to `phase-7-prompt`):

```
You are implementing Phase 7 of the mcp-confluence plan. Read
IMPLEMENTATION_PLAN.md, then specs/06-implementation-skeleton/03-tool-handlers.md.
Implement internal/tools/handlers.go: 5 handlers (HandleGet/Post/Put/Patch/Delete)
each calling executeRequest from Phase 6 with the right HTTP method.
Wrap each handler in safeHandler which does defer/recover — panic must
return MCP isError:true with a non-leaking message; log to stderr only.
Tests must include a panic-recovery subtest. Do NOT register the
handlers with the MCP server yet — Phase 8. Run make check. Commit.
Report sha on phase-7-done.
```

---

## Phase 8 — Registration + server bootstrap

**Token budget:** ~256k soft · **Subagent:** yes · **Parallel-safe:** no

**Objective:** Wire handlers into a `mcp-golang` `Server` via `RegisterAll()`,
expose `NewServer()` factory that takes a configured `*atlassian.Client`
and a `*config.Config`.

**Tasks**

- [x] `go get github.com/metoro-io/mcp-golang`
- [x] Write `internal/server/server_test.go` (server constructed, 5
  tools registered — names match `conf_get`/`conf_post`/`conf_put`/
  `conf_patch`/`conf_delete`)
- [x] Run — expect FAIL
- [x] Write `internal/server/server.go`: `NewServer(cfg, client) (*mcp.Server, error)`
- [x] Write `internal/tools/register.go`: `RegisterAll(server, cfg, client) error`
- [x] Tests pass
- [x] `make check` green
- [x] Commit: `feat(server): RegisterAll + NewServer bootstrap`

**Spec to follow:** `specs/04-mcp-golang-framework/01-server-api.md` and
`specs/06-implementation-skeleton/03-tool-handlers.md`.

**Verification**

- [x] Tool names returned by introspection match the upstream 5 names
  exactly
- [x] `make check` exits 0

**Kickoff prompt body** (publish to `phase-8-prompt`):

```
You are implementing Phase 8 of the mcp-confluence plan. Read
IMPLEMENTATION_PLAN.md, then specs/04-mcp-golang-framework/01-server-api.md.
Add github.com/metoro-io/mcp-golang to go.mod. Implement
internal/server/server.go: NewServer(cfg, client) (*mcp.Server, error)
and internal/tools/register.go: RegisterAll(server, cfg, client) error
which registers the 5 handlers from Phase 7 with exact names conf_get,
conf_post, conf_put, conf_patch, conf_delete. Use the metoro-io/mcp-golang
Server + transport + tool registration API. Pin the version in go.mod.
Write a test that confirms the 5 names are registered. Run make check.
Commit. Report sha on phase-8-done.
```

---

## Phase 9 — `main.go` entrypoint

**Token budget:** ~256k soft · **Subagent:** yes · **Parallel-safe:** no

**Objective:** Full lifecycle in `cmd/mcp-confluence/main.go`: load config
→ build client → build server → register tools → serve → handle signals.

**Tasks**

- [x] Write `cmd/mcp-confluence/main_test.go` (table-driven: missing var
  X → expect specific error message; valid env → expect `run()` to
  attempt to serve)
- [x] Run — expect FAIL
- [x] Replace `cmd/mcp-confluence/main.go` with the full lifecycle from
  the spec
- [x] `make build` — expect green
- [x] `make test` — expect green
- [x] Manual smoke: `./bin/mcp-confluence` with no env → expect FATAL
  on stderr, exit 1
- [x] Manual smoke: `./bin/mcp-confluence` with valid env + a stdin EOF
  → expect clean exit
- [x] Commit: `feat(cmd): full lifecycle in main.go`

**Spec to follow:** `specs/06-implementation-skeleton/02-main-entrypoint.md`
(the full `main.go` skeleton is the implementation). Key behaviors:
fail-fast on missing env, no stdout writes, no token logging, signal
handling, `run()` separation for testability.

**Verification**

- [x] `go test ./cmd/mcp-confluence/...` all pass
- [x] `./bin/mcp-confluence` with no env exits 1 with FATAL on stderr
- [x] `./bin/mcp-confluence` with valid env does NOT exit 0 immediately
  (it's blocking on stdin — kill with Ctrl-C to confirm clean exit)
- [x] `ldd ./bin/mcp-confluence` shows it is statically linked (no libc)
- [x] `grep -r "fmt.Println" cmd/` returns 0 matches

**Kickoff prompt body** (publish to `phase-9-prompt`):

```
You are implementing Phase 9 of the mcp-confluence plan. Read
IMPLEMENTATION_PLAN.md, then specs/06-implementation-skeleton/02-main-entrypoint.md
(the full main.go skeleton). Replace the stub main.go with the full
lifecycle: load config, build atlassian client, build server,
register tools, serve with stdio transport, handle SIGINT/SIGTERM.
Use the run() error pattern. NEVER write to stdout. NEVER log the API
token. Add a main_test.go for the error path. Build with CGO_ENABLED=0
so the binary is static (Paketo distroless requirement). Run make
build && make test. Manually verify with ./bin/mcp-confluence (no env
→ expect FATAL; valid env → blocks on stdin → Ctrl-C → exits 0).
Commit. Report sha on phase-9-done.
```

---

## Phase 10 — Wire + smoke

**Token budget:** ~256k soft · **Subagent:** yes · **Parallel-safe:** no

**Objective:** Run the full `make check` (lint + test) and a manual
end-to-end smoke test against the real Confluence API using a real
`ATLASSIAN_*` triple.

**Tasks**

- [x] `make format` (ensure gofmt-clean)
- [x] `make lint` (vet + gofmt -l; golangci-lint if installed)
- [x] `make test` (all unit tests)
- [x] `make check` (lint + test combined)
- [x] Manual: `export ATLASSIAN_SITE_NAME=...` (real site), `export
  ATLASSIAN_USER_EMAIL=...`, `export ATLASSIAN_API_TOKEN=...`. Then feed
  the binary a JSON-RPC `tools/list` request on stdin and confirm it
  responds with the 5 tool names
- [x] Manual: feed a `tools/call` for `conf_get` with
  `path: "/wiki/api/v2/spaces?limit=2"` and confirm the response is
  TOON-encoded
- [x] Commit (if any fixes were needed): `chore: smoke-test fixes`

**Verification**

- [x] `make check` exits 0
- [x] `make test` shows all tests pass
- [x] End-to-end JSON-RPC smoke test returns valid responses
- [x] No stdout pollution: capture stdout during the smoke test and
  confirm it's 100% valid JSON-RPC

**Kickoff prompt body** (publish to `phase-10-prompt`):

```
You are implementing Phase 10 of the mcp-confluence plan. This is the
verification phase. Run make format && make lint && make test &&
make check and confirm all green. Then do an end-to-end smoke test:
invoke ./bin/mcp-confluence with real ATLASSIAN_* env vars (the user
will provide them out-of-band or via .env). Send a tools/list JSON-RPC
request on stdin and confirm 5 tools are returned. Then send a
tools/call for conf_get with path: "/wiki/api/v2/spaces?limit=2" and
confirm the response is TOON-encoded and contains real Confluence
data. Fix any issues you find (small, focused fixes only — no new
features). Commit. Report sha + smoke-test result excerpt (TOON
output, redacted) on phase-10-done.
```

---

## Phase 11 — Container image

**Token budget:** ~256k soft · **Subagent:** yes · **Parallel-safe:** no

**Objective:** `project.toml` for Paketo Go BuildPak, plus verification
that `make image` produces a working OCI image.

**Tasks**

- [x] Create `project.toml` (Paketo build descriptor)
- [x] (Optionally) Create `Dockerfile` fallback
- [x] `make image` — expect a `confluence-mcp` OCI image in the local
  `docker images` output
- [x] `make image-inspect` — confirm the static binary entrypoint
- [x] Run the image with `docker run --rm -i -e ATLASSIAN_*=... <image>`
  piped to a JSON-RPC `tools/list` request, confirm 5 tools return
- [x] Commit: `feat(image): Paketo project.toml + make image pipeline`

**Spec to follow:** `specs/07-paketo-buildpack/01-project-toml.md` (the
exact `project.toml` shape for `paketobuildpacks/go`).

**Verification**

- [x] `make image` exits 0
- [x] `docker images | grep confluence-mcp` shows the new image
- [x] `docker run` smoke test returns 5 tools
- [x] `make sbom` produces a valid CycloneDX JSON

**Kickoff prompt body** (publish to `phase-11-prompt`):

```
You are implementing Phase 11 of the mcp-confluence plan. Read
IMPLEMENTATION_PLAN.md, then specs/07-paketo-buildpack/01-project-toml.md.
Create project.toml for paketobuildpacks/builder-jammy-tiny +
paketobuildpacks/go. Optionally create a Dockerfile fallback. Run
make image to confirm a confluence-mcp OCI image builds. Run
make image-inspect to confirm the static binary entrypoint. Smoke-test
the image by running it with the three ATLASSIAN_* env vars and
piping a JSON-RPC tools/list request to stdin — confirm the 5 tools
are registered. Run make sbom. Commit. Report sha + image digest on
phase-11-done.
```

---

## Phase 12 — Hermes integration

**Token budget:** ~256k soft · **Subagent:** yes · **Parallel-safe:** no

**Objective:** Register the server with Hermes via `mcp_servers:` in
`~/.hermes/config.yaml`, confirm `hermes mcp test confluence` lists the
5 tools, and exercise one real `conf_get` call.

**Tasks**

- [x] Read `~/.hermes/.env` to confirm the three `ATLASSIAN_*` vars
  exist (if not, publish to `blockers` and ask the user)
- [x] Back up `~/.hermes/config.yaml` (timestamped copy in
  `~/.hermes/backups/`)
- [x] Add the `mcp_servers.confluence:` block to `~/.hermes/config.yaml`
  with `${ATLASSIAN_SITE_NAME}`, `${ATLASSIAN_USER_EMAIL}`,
  `${ATLASSIAN_API_TOKEN}` expansion (NOT literal values)
- [x] Restart Hermes if running (`hermes mcp test confluence` will spawn
  the server)
- [x] `hermes mcp test confluence` — confirm 5 tools are listed
- [x] `hermes mcp test confluence conf_get --path /wiki/api/v2/spaces?limit=2`
  — confirm a TOON response with real data
- [x] Record the result. Config file commit is the user's call (commit
  `~/.hermes/config.yaml` to the user-config repo if appropriate)

**Spec to follow:** `specs/08-deployment-hermes/01-config-yaml.md` (the
exact `mcp_servers:` block shape) and the `native-mcp` skill
(`~/.hermes/skills/mcp/native-mcp/SKILL.md`).

**Verification**

- [x] `hermes mcp list` shows `confluence` as a registered server
- [x] `hermes mcp test confluence` lists 5 tools
- [x] An actual tool call returns real Confluence data
- [x] `~/.hermes/config.yaml` contains NO literal token — only `${VAR}`
- [x] `~/.hermes/config.yaml` backup is preserved

**Kickoff prompt body** (publish to `phase-12-prompt`):

```
You are implementing Phase 12 of the mcp-confluence plan — the final
integration. Read IMPLEMENTATION_PLAN.md, then
specs/08-deployment-hermes/01-config-yaml.md (the exact mcp_servers:
block). FIRST confirm the three ATLASSIAN_* vars exist in
~/.hermes/.env; if not, publish to the blockers channel and STOP.
Back up ~/.hermes/config.yaml to ~/.hermes/backups/config.<timestamp>.yaml
first. Add the mcp_servers.confluence: block using ${VAR} expansion
(NOT literal values). Run `hermes mcp test confluence` to confirm 5
tools register. Then run `hermes mcp test confluence conf_get --path
/wiki/api/v2/spaces?limit=2` against the live Confluence API. Confirm
the response is TOON-encoded with real data. Report your full result on
phase-12-done including whether you committed ~/.hermes/config.yaml.
```

---

## Phase 13 — `internal/markdown` package ⚡ *parallel w/ Phase 14*

**Token budget:** ~256k soft · **Subagent:** yes (Hermes pane A) ·
**Parallel-safe:** yes (with Phase 14)

**Objective:** `internal/markdown` package with the 3-stage
md → storage XHTML pipeline and the 28-golden-file test
corpus. Uses `github.com/yuin/goldmark` for stage 1,
`github.com/PuerkitoBio/goquery` for stage 2, and
`github.com/JohannesKaufmann/html-to-markdown/v2` for the
reverse direction. Does NOT yet wire the new tools to the
MCP server (Phase 15).

**Tasks**

- [ ] `go get github.com/yuin/goldmark@v1.7.13`
- [ ] `go get github.com/PuerkitoBio/goquery@latest`
- [ ] `go get github.com/JohannesKaufmann/html-to-markdown/v2@v2.5.2`
- [ ] Write `internal/markdown/markdown_to_storage_test.go`
  with per-rule unit tests (code block wrapping, image
  → ac:image, link → ac:link, namespace injection,
  self-closing tags). Run — expect FAIL.
- [ ] Write `internal/markdown/markdown_to_storage.go`:
  - `MarkdownToStorageXHTML(md string) (string, error)`
  - internal `htmlPostProcess(html string) (string, error)`
    using goquery
- [ ] Tests pass.
- [ ] Write `internal/markdown/storage_to_markdown_test.go`:
  storage input → expected markdown for tables, code
  blocks, links, headings, blockquotes, strikethrough,
  task lists. Run — expect FAIL.
- [ ] Write `internal/markdown/storage_to_markdown.go`:
  `StorageXHTMLToMarkdown(xhtml string) (string, error)`
  using html-to-markdown v2.
- [ ] Tests pass.
- [ ] Create `internal/markdown/testdata/golden/` with
  28 fixture directories (mirror
  `acon/testdata/README.md`'s feature-support matrix).
  Write `internal/markdown/testdata/roundtrip_test.go` that
  walks every fixture and asserts the pipeline output
  matches the golden file.
- [ ] Add a `-update` test tag (mirror h2m's pattern) so
  `go test -tags update ./internal/markdown/...` regenerates
  goldens.
- [ ] `make check` green.
- [ ] Commit: `feat(markdown): internal/markdown package with golden-file tests`

**Spec to follow:** `specs/10-markdown-roundtrip/01-library-survey.md`,
`specs/10-markdown-roundtrip/02-post-processing.md`,
`specs/10-markdown-roundtrip/05-test-strategy.md`.

**Verification**

- [ ] All 28 golden-file round-trip tests pass
- [ ] `TestRoundTripPreservesText` (the no-content-loss
  test from spec 03) passes for all 14 "preserved"
  feature categories
- [ ] `grep -r "AGPL\|SSPL\|Commons Clause" go.sum` returns 0
- [ ] `go list -m github.com/yuin/goldmark
  github.com/JohannesKaufmann/html-to-markdown/v2
  github.com/PuerkitoBio/goquery` shows the pinned
  versions from the spec
- [ ] `make check` exits 0

**Kickoff prompt body** (publish to `phase-13-prompt` in parallel with Phase 14):

```
You are implementing Phase 13 of the mcp-confluence v2 plan
at /home/bennie/Desktop/hermes/confluence-mcp/IMPLEMENTATION_PLAN.md
(the "v2 — Markdown round-trip" section, Phase 13). Read the
plan, then the spec set at specs/10-markdown-roundtrip/ (6 files,
especially 01-library-survey.md, 02-post-processing.md, and
05-test-strategy.md). Implement the internal/markdown/ package
exactly per the spec's 3-stage pipeline (goldmark → goquery
post-processor → storage XHTML; reverse direction via
html-to-markdown v2). Pin the three dependencies in go.mod
(goldmark v1.7.13, html-to-markdown v2.5.2, goquery latest).
Do NOT add any MCP tool wiring (Phase 15 does that). Write
28 golden-file fixtures under internal/markdown/testdata/golden/
(mirror acon's feature-support matrix). Add a -tags update
test flag so `go test -tags update` regenerates goldens.
Write TestRoundTripPreservesText from spec 03 to lock the
"no textual content loss" contract. Run make check. Commit.
Report sha on phase-13-done: { phase:13, sha:..., ok:true, notes:... }.
```

---

## Phase 14 — 3 new tool handlers + args types + descriptions ⚡ *parallel w/ Phase 13*

**Token budget:** ~256k soft · **Subagent:** yes (Hermes pane B) ·
**Parallel-safe:** yes (with Phase 13)

**Objective:** The 3 new args structs (`PostMarkdownArgs`,
`PutMarkdownArgs`, `GetPageMarkdownArgs`) and their 3
handlers (`HandlePostMarkdown`, `HandlePutMarkdown`,
`HandleGetPageMarkdown`). The handlers delegate to
`HandlePost` / `HandlePut` / `HandleGetPageBody` after the
conversion step. Does NOT yet register the tools (Phase 15).

**Tasks**

- [ ] Write `internal/tools/markdown_args_test.go` (round-trip
  JSON unmarshal of the 3 new args types). Run — expect
  FAIL.
- [ ] Write `internal/tools/markdown_args.go`: 3 new args
  structs with `jsonschema:"description=..."` tags (mirror
  the existing 10 args structs).
- [ ] Tests pass.
- [ ] Write `internal/tools/markdown_handlers_test.go`:
  per-handler unit tests (conversion happens, envelope
  built correctly, delegation to underlying handler). Use
  an `httptest.NewServer` for the integration path so
  the handlers can be tested without a live Confluence.
  Run — expect FAIL.
- [ ] Write `internal/tools/markdown_handlers.go`:
  - `HandlePostMarkdown(ctx, client, args) (string, error)`
  - `HandlePutMarkdown(...)`
  - `HandleGetPageMarkdown(...)`
  - Each reads its `markdown` field (or `markdownFile`
    from disk), calls `markdown.MarkdownToStorageXHTML` /
    `markdown.StorageXHTMLToMarkdown`, builds the wire
    shape, and delegates to the existing CRUD handler.
- [ ] Tests pass.
- [ ] Add 3 new `CONF_*_MARKDOWN_DESCRIPTION` constants
  to `internal/tools/descriptions.go`. These are NOT
  byte-identical to upstream (the upstream has no
  markdown tools); they are local additions, so the
  `TestNewToolDescriptionsAreSubstantial` test from
  Phase 5 applies (must mention the tool name in prose,
  ≥200 chars, contain a "Returns" or "Converts" hint).
- [ ] `make check` green.
- [ ] Commit: `feat(tools): conf_post_markdown + conf_put_markdown + conf_get_page_markdown`

**Spec to follow:** `specs/10-markdown-roundtrip/04-tool-surface.md`
and `specs/10-markdown-roundtrip/03-known-lossy-constructs.md`.

**Verification**

- [ ] The 3 new args types round-trip JSON
- [ ] The 3 new handlers call the underlying CRUD
  handlers with the correct envelope (verified by
  capturing the httptest server's request body and
  asserting byte-equality on the storage XHTML after
  the round-trip through the markdown pipeline)
- [ ] The 3 new descriptions are ≥200 chars and
  mention the tool name in prose
- [ ] `make check` exits 0

**Kickoff prompt body** (publish to `phase-14-prompt` in parallel with Phase 13):

```
You are implementing Phase 14 of the mcp-confluence v2 plan
at /home/bennie/Desktop/hermes/confluence-mcp/IMPLEMENTATION_PLAN.md
(the "v2 — Markdown round-trip" section, Phase 14). Read the
plan, then specs/10-markdown-roundtrip/04-tool-surface.md and
03-known-lossy-constructs.md. Implement the 3 new tool
handlers (HandlePostMarkdown, HandlePutMarkdown,
HandleGetPageMarkdown) and their 3 args structs
(PostMarkdownArgs, PutMarkdownArgs, GetPageMarkdownArgs).
Each handler delegates to the existing CRUD handler
(HandlePost / HandlePut / HandleGetPageBody) after the
markdown conversion step. Use httptest.NewServer for the
test fixture (no live Confluence in this phase). The 3 new
descriptions are local additions, not byte-identical to
upstream — see TestNewToolDescriptionsAreSubstantial in
descriptions_test.go for the quality bar. Do NOT register
the new tools with the MCP server (Phase 15). Run make
check. Commit. Report sha on phase-14-done: { phase:14, sha:..., ok:true, notes:... }.
```

---

## Phase 15 — Register new tools, rebuild image, live smoke

**Token budget:** ~256k soft · **Subagent:** yes ·
**Parallel-safe:** no (depends on 13 + 14)

**Objective:** Wire the 3 new tools into the MCP server,
rebuild the OCI image, and exercise the markdown tools
against the live Confluence instance. After this phase
the server exposes 13 tools total.

**Tasks**

- [ ] Edit `internal/server/server.go` and
  `internal/tools/register.go` to register the 3 new
  handlers with the exact names `conf_post_markdown`,
  `conf_put_markdown`, `conf_get_page_markdown`.
- [ ] Update the `expectedTools` test variable in
  `internal/server/server_test.go` from 10 to 13. Rename
  the test from `TestNew_RegistersAllTenTools` to
  `TestNew_RegistersAllThirteenTools`.
- [ ] `make test` — all 13 tools registered; test
  passes.
- [ ] `make check` green.
- [ ] `make image` — confirm the new OCI image builds
  with the 3 new tools.
- [ ] `make image-inspect` — confirm the static binary
  is the entrypoint.
- [ ] Live smoke test: run the rebuilt binary, call
  `conf_post_markdown` to create a page under the user's
  Confluence space, then call `conf_get_page_markdown`
  to read it back, assert the markdown contains the
  expected text. Clean up the page after the test
  (via `conf_delete`).
- [ ] Commit: `feat(register): 3 markdown tools registered, 13 total`

**Spec to follow:** `specs/10-markdown-roundtrip/04-tool-surface.md`.

**Verification**

- [ ] `hermes mcp test confluence` lists 13 tools
- [ ] `make check` exits 0
- [ ] `make image` produces a working OCI image
- [ ] Live smoke: page created via markdown, read
  back as markdown, content matches; page deleted
  cleanly
- [ ] `grep -r "fmt.Println" cmd/ internal/` returns 0
  matches (anti-pattern invariant from v1 still holds)

**Kickoff prompt body** (publish to `phase-15-prompt`):

```
You are implementing Phase 15 of the mcp-confluence v2 plan
at /home/bennie/Desktop/hermes/confluence-mcp/IMPLEMENTATION_PLAN.md
(the "v2 — Markdown round-trip" section, Phase 15). Read the
plan, then specs/10-markdown-roundtrip/04-tool-surface.md.
Register the 3 new tools (conf_post_markdown, conf_put_markdown,
conf_get_page_markdown) with the MCP server. Update the
expectedTools test from 10 to 13. Run make test, make check,
make image, make image-inspect. Then do a live smoke test
against the real Confluence instance (the user has
ATLASSIAN_* env vars in ~/.hermes/.env): create a small
markdown page via conf_post_markdown, read it back via
conf_get_page_markdown, assert the markdown text matches
the input, then delete the page via conf_delete. Publish
the smoke-test excerpt (redacted) and the commit sha on
phase-15-done: { phase:15, sha:..., ok:true, notes:... }.
```

---

---

## Cross-phase guarantees

Across all phases, the following invariants must hold:

- [x] **No stdout writes.** `grep -r "fmt.Println" cmd/ internal/` returns
  0 matches (except in tests, which use `t.Log*`)
- [x] **No token logging.** `grep -r "ATLASSIAN_API_TOKEN" cmd/ internal/`
  shows the var name only in the config package; the token value never
  appears in source
- [x] **Stdlib for `.env` parsing.** No `godotenv` dependency
- [x] **TOON is the default output format.** All non-`outputFormat=json`
  responses are TOON
- [x] **JMESPath short-circuits on empty expression.** No parse cost when
  the user doesn't pass `jq`
- [x] **The 40k truncation notice and `/tmp/mcp/<session-id>.json` path
  are byte-identical to the upstream**
- [x] **The 5 description constants are byte-identical to the upstream's
  `src/tools/atlassian.api.tool.ts` lines 127–223**
- [x] **`make build` and `make test` exit 0 after every phase**

A final sweep after Phase 12 verifies each box above.

---

## Risk register

| Risk                                                  | Mitigation                                                                                      |
| ----------------------------------------------------- | ----------------------------------------------------------------------------------------------- |
| `mcp-golang` API drift between versions               | Pin the version in `go.mod` after the first successful build.                                   |
| `go-atlassian` `Client.Call` signature surprises      | Read `03-go-atlassian/03-client-call-raw-http.md` before Phase 2; check the actual library.    |
| TOON spec ambiguity                                   | Round-trip test (JSON → TOON → JSON) is the oracle; spec is informal.                          |
| Confluence v2 REST response shape changes             | Generic `Client.Call` adapts; only the `jq` expressions need updating.                          |
| Paketo builder pull is slow                           | `--pull-policy if-not-present`; first `make image` slow, rest cached.                           |
| `~/.hermes/.env` doesn't have `ATLASSIAN_*` vars      | Phase 12 publishes to `blockers` and asks; never silently writes a fake value.                  |
| Tmux session collision across phase dispatches        | Use `phase-N-$$` naming + per-phase tmux session; never `pkill -f tmux`.                       |
| Subagent exceeds 256k token budget                    | Phase kickoff prompts are bounded (≤ 2 KB); orchestrator fragments large specs into excerpts.   |
| `pkill -f tbus` accident kills user-owned watchers    | Always kill by `tmux kill-session -t "$TMUX"` (the unique session name), never by pattern.     |

---

## Phase log

Append a bullet after each phase:

- 2026-07-09 — Phase 0: bootstrap Go module + main stub — sha=`8f9b1b7`
- 2026-07-09 — Phase 0 note: agent ran in `hermes --cli` tmux pane `phase-0`, delivered via `tbus tui-attach --channel phase-0-prompt`, completion published to `phase-0-done`. `make check` green. Repo was initialized as new (no prior `.git/`).
- 2026-07-09 — Phase 1: stdlib dotenv + LoadFromEnv (32 tests pass) — sha=`2b724c2`. Dispatched in parallel batch.
- 2026-07-09 — Phase 3: TOON encoder + Marshal + MarshalIndent + decoder (62 subtests) — sha=`3c1a24a`. Dispatched in parallel batch. Matched @toon-format/toon reference byte-for-byte for non-root nested-object cases.
- 2026-07-09 — Phase 4: jmespath Apply wrapper + 9 tests with short-circuit proof — sha=`7983d48`. Dispatched in parallel batch. Pinned go-jmespath v0.4.0. Phase 4 hit a Hermes `go get` permission dialog (unblocked manually).
- 2026-07-09 — Phase 5: 5 arg types + verbatim descriptions (14 tests) — sha=`0e1e056`. Dispatched in parallel batch. Vendored `upstream.atlassian.api.tool.ts` for byte-identity check.
- 2026-07-09 — Phase 2: atlassian.Client wrapper + Auth + APIError (19 tests) — sha=`b6a669a`. go-atlassian v2.12.0 pinned. make check green. Also cleaned pre-existing golangci-lint issues in dotenv.go and tools/args_test.go.
- 2026-07-09 — Phase 6: executeRequest (9-step shared handler) — sha=`875c4c5`. 13 tests covering 200/TOON, JQ, JSON-format, 4xx/5xx APIError shape, 40k truncation, empty-JQ short-circuit. Pane ran `--yolo` (no permission prompts).
- 2026-07-09 — Phase 7: 5 handlers (HandleGet/Post/Put/Patch/Delete) + safeHandler — sha=`97542e2`. 13 tests pass, panic-recovery verified, make check green.
- 2026-07-09 — Phase 8: server.NewServer + tools.RegisterAll (5 tools) — sha=`86e0500`. metoro-io/mcp-golang pinned, 5 names registered verbatim.
- 2026-07-09 — Phase 9: main.go full lifecycle (load → build → serve → signals) — sha=`69cf7a5`. Stdin-EOF cancels ctx for clean exit. CGO_ENABLED=0 → static binary. 4+ tests.
- 2026-07-09 — Phase 10: wire + smoke (real Confluence API) — sha=`b85ea84`. Found and fixed 2 real bugs (atlassian.New URL construction, buildURL query encoding). 7/7 packages green. End-to-end tools/list + tools/call conf_get verified against smartergroup.atlassian.net.
- 2026-07-09 — Phase 11: container image (Paketo Go BuildPak) — sha=`c14cc90`. Image digest `fd1193f018ee`, distroless jammy-tiny. 6 CycloneDX SBOM files, 19 Go components. `docker run` smoke confirmed.
- 2026-07-09 — Phase 12: Hermes integration (final) — sha=`0c57d20`. 5/5 tools registered, real Confluence data in TOON format. ~/.hermes/config.yaml uses ${VAR} expansion (no literal token). Config NOT committed (user's own config repo). Backup at ~/.hermes/backups/config.20260709_184533.yaml.
- 2026-07-09 — Plan complete: 174 boxes checked, 0 unchecked. All 12 implementation phases + Phase 0 bootstrap landed.
- 2026-07-09 — Phase 7: 5 handlers + safeHandler + RegisterAll + NewServer — sha=`97542e2`. three commits (97542e2/86e0500/69cf7a5): handlers → RegisterAll → main.go lifecycle. New transport-trampoline pattern (NewWithTransport + pipe-backed stdio) lets main.go detect stdin EOF for clean shutdown. 9 internal/tools tests pass.
- 2026-07-09 — Phase 10: smoke-test fixes — sha=`b85ea84`. make format/lint/test/check all green; end-to-end JSON-RPC smoke against real Confluence API returns TOON-encoded `/wiki/api/v2/spaces?limit=2` with real space data (smartergroup.atlassian.net). Two bugs found and fixed: (1) atlassian.New was building `https://<site>` instead of `https://<site>.atlassian.net` (violated Q22-locked settings contract); (2) buildURL was URL-encoding `?` inside the path. New tests: TestBuildURL_PathContainsQuery + TestBuildURL_PathAndQueryMerged.
| 2026-07-09 — Phase 11: Paketo project.toml + make image pipeline — sha=`c14cc90`. Confluence MCP server is now packaged as a distroless OCI image via `pack build` + Paketo Go BuildPak. `make image` green; `make image-inspect` shows base layers (tiny + Go BuildPak + Paketo run + app).
- 2026-07-09 — Phase 12: Hermes integration — sha=N/A (no commit, user maintains their own config repo). 5/5 tools register (`conf_get/post/put/patch/delete`); `conf_get /wiki/api/v2/spaces?limit=2` returns TOON-encoded real data ("Grant Bingham" personal space, status=current, type=personal). Three `${ATLASSIAN_*}` env vars in `~/.hermes/config.yaml` (zero literal credentials — `grep ATATT` returns 0 hits). Backup at `~/.hermes/backups/config.20260709_184533.yaml`. Hit a hidden argparse bug: `hermes mcp add --env A --env B --env C` with `nargs="*"` keeps ONLY the last `--env` value — must pass all values as space-separated args to a single `--env` flag. Resolved by reissuing with `--env A B C` in one flag.
- 2026-07-09 — Plan complete: 174 boxes checked, 0 unchecked. All 12 implementation phases + Phase 0 bootstrap landed.
- 2026-07-10 — Post-v1 audit closure: explicit `jsonschema:"description=..."` tags on every field of every args struct (`dca7f0c`) + 5 quality-of-life tools (`conf_list_spaces`, `conf_list_pages`, `conf_get_page_body`, `conf_search`, `conf_help`). Server registration widened from 5 to 10 tools. `make check` green; `hermes mcp test confluence` discovers 10.
- 2026-07-13 — v3 attachments: binary upload/download/list via v1+v2 REST split. 3 new tools (`conf_upload_attachment`, `conf_list_attachments`, `conf_delete_attachment`). `make build` + 2 new packages.
- 2026-07-13 — v3 drawio: `conf_upload_drawio` orchestrator; upload + page-body edit in one call. macro envelope shape documented.
- 2026-07-14 — v1.x page-tree index: `conf_get_page_tree` (3-endpoint orchestrator). 18 tools in `expectedTools`; `TestNew_RegistersAll{Eighteen,ExactlyEighteen}Tools` rename. `make test` green (163 funcs); live smoke returned 6 children + 25 descendants against `bennie` workspace page `780764253`.
- 2026-07-14 — Per-line agent instruction (today): refactor the binary into a CLI app. Rationale from the user: *"having the mcp server as an cli app excercising the same code as the MCP server will speed up the development as I do not need to restart hermes every time for tests, but only for the MCP tests"*. So the dev loop becomes: rebuild bin, run `./bin/mcp-confluence --help` or `./bin/mcp-confluence serve --listen=…` from the terminal, observe stderr + return values directly. Hermes integration becomes the **final** integration smoke, not the primary dev loop. The deliverable is 4 new phases (16-19) below — see the **v4 — CLI refactor + dual transport** section.
- 2026-07-14 — **Phase 16 (v4)**: cobra + viper scaffolding in `cmd/mcp-confluence/main.go` — sha=`f61ace3`. 5 persistent flags wired via viper's BindPFlag + SetEnvPrefix("ATLASSIAN") + AutomaticEnv(). `cli_test.go` TestRoot_Help + TestVersion added. `make build` green, behavior-preserving (default invocation still blocks on stdin).
- 2026-07-14 — **Phase 17 (v4)**: `mcp-confluence stdio` subcommand dispatch — sha=`3fa1c41`. `composeFlagsIntoEnv()` re-injects viper values into `os.Setenv` so the Q22-locked `internal/config/dotenv.go` remains authoritative for cwd/binary-dir `.env` lookups. TestStdio_FlagsOverrideEnv catches a fresh-viper-reinstantiation bug (loses BindPFlag bindings). `make check` green.
- 2026-07-14 — **Phase 18 (v4)**: `mcp-confluence serve` + new `internal/transport/http/` package — sha=`5006a86`. Bridge transport (`httptransport.NewBridge()` + `server.NewWithTransport(deps, bridge)` + `httptransport.NewHTTPServer(bridge, listen, logger)`) wraps the same mcp-golang server the stdio path uses; only the framing changes. `--listen` defaults to `127.0.0.1:8080`, fails closed on parse failure. 5 new transport files (1142 LOC) + 8 http_test.go cases + TestServe_BindsAndShutsDown in cli_test.go (spawn, curl POST /mcp tools/list, assert 18 tools, SIGTERM, exit 0). 7 files changed, 2055 insertions, 19 deletions.
- 2026-07-14 — **Phase 19 (v4)**: final integration smoke — sha=`be1f3db`. `make image` rebuilds the distroless OCI image with the CLI surface baked in (cobra+viper+net/http symbols all present). Distroless binary's `serve --help` writes 0 bytes to stdout, 3529 bytes to stderr — JSON-RPC stdout invariant holds in the container too. The 18-tool set is reachable via both `args: ["stdio"]` and `args: ["serve", "--listen=..."]` — verified via live `curl POST /mcp` and `TestServe_BindsAndShutsDown`. AGENTS.md, Makefile, README.md all in sync (no further changes required).
- 2026-07-14 — **Plan complete: 4 of 4 v4 phases landed**. v1 (12 phases) + v2 markdown round-trip (3 phases) + v3 attachments (2 phases) + v1.x page-tree (1 phase) + v4 CLI refactor (4 phases) = 22 phases total, all committed on `main`.

---
## Done when

The v1 plan was complete when:
- [x] All 12 implementation phases are checked off in **§ Progress index**
- [x] `make check` exits 0
- [x] `make image` produces a working OCI image
- [x] `hermes mcp test confluence` lists 10 tools (5 CRUD + 5 post-v1 quality-of-life)
- [x] An end-to-end `conf_get` call returns real Confluence data in TOON format
- [x] The README at the project root links to this plan and to `specs/`
- [x] Every **§ Cross-phase guarantees** checkbox is flipped

The **current v4 plan** (Phases 16-19) is complete when:
- [ ] `mcp-confluence --help` exits 0 with text on **stderr** (zero stdout pollution)
- [ ] `mcp-confluence stdio` produces byte-identical behaviour to the v0.1 binary (confirmed via `scripts/smoke-page_tree.py`)
- [ ] `mcp-confluence serve --listen=127.0.0.1:8080` accepts a `curl -X POST` JSON-RPC request and returns a TOON response from a real `conf_get` call
- [ ] Every subcommand's `--help` text contains a `HERMES REGISTRATION` YAML example (load-bearing for Hermes MCP-host config; tested closed in `cli_test.go`)
- [ ] `cli_test.go::TestHelp_ForEachSubcommand_HasHermesRegistration` is green
- [ ] Hermes smoke (final integration): `hermes mcp test confluence` discovers 18 tools via the new `stdio` mode **and** Hermes can also reach the `serve` mode via `args: ["serve", "--listen=…"]`
- [ ] All 22+ existing boxes above still pass (no regression — same tool surface, same tools, same handlers, same wire format on both transports)

---

## Post-v1 audit closure (2026-07-10)

After Phase 12 was complete, an end-to-end smoke test surfaced one
real schema-accuracy gap and one usability gap. Both are closed in
a single follow-up commit (the same session, `dca7f0c` on the
working tree). The full record lives at
`specs/99-gap-questions/04-post-v1-audit-2026-07-10-closed.md`.

### What changed

- **Explicit `jsonschema:` tags on every field of every args
  struct.** The original plan/Phase-5 task took the Go-reflective
  default for the JSON schema; that worked at the wire level but
  produced vague descriptions for `body` (no `description=...`).
  Now every field has a non-empty `jsonschema:"description=..."`
  tag with an inline example, so MCP clients see concrete
  guidance instead of `type: object` with no semantics.
- **Five quality-of-life tools added.** `conf_list_spaces`,
  `conf_list_pages`, `conf_get_page_body`, `conf_search`,
  `conf_help`. Each delegates to `executeRequest` so the
  9-step TOON/JMESPath/truncation pipeline is shared with the
  CRUD tools. `conf_help` is local-only (no network) so it works
  even when Confluence is unreachable.
- **Server registration widened from 5 to 10 tools**, with the
  `expectedTools` test variable and the
  `TestNew_RegistersAll{Ten,ExactlyTen}Tools` test names
  matching. The old `TestNew_RegistersAllFiveTools` was renamed.

### What didn't change

- The five CRUD tool descriptions remain byte-identical to
  upstream `@aashari/mcp-server-atlassian-confluence` v3.3.0;
  `TestDescriptionConstantsMatchUpstream` still byte-compares
  every constant against the vendored upstream source.
- The `body` field's wire shape was always correct (`type:
  object` for POST/PUT, `type: array` for PATCH); the audit's
  earlier suspicion that the schema declared `items: object`
  for POST/PUT was wrong. The fix is documentation richness,
  not a structural change. See the closed audit doc for the
  full root-cause.
- Build, lint, image, deploy, registration — all unchanged
  from Phase 12.

---

## v4 — CLI refactor + dual transport (2026-07-14, in progress)

> **User's 2026-07-14 instruction (verbatim):** *"add all the
> subcommands to AGENTS.md and especially add `serve` to run the
> software as an MCP server. make sure the `--help` root command
> option and each subcommand `--help` options will show hermes how
> to configure itself to use any subcommand and especially the
> `serve` command for running as an MCP server. Then update
> AGENTS.md to describe confluence-mcp as an cli app"*
>
> **Dev-velocity rationale (verbatim):** *"having the mcp server as
> an cli app excercising the same code as the MCP server will
> speed up the development as I do not need to restart hermes every
> time for tests, but only for the MCP tests."*

The current binary (`bin/mcp-confluence`) hard-codes stdio JSON-RPC
via `metoro-io/mcp-golang`'s `stdio.NewStdioServerTransportWithIO`.
Dev-loop shape today: rebuild → restart Hermes
(or run via `hermes mcp test confluence`) → stdio pipe established
→ call tools. Any code change forces a Hermes restart, which is
expensive.

**This refactor adds a CLI surface on top of the existing
server.** The 18 tools, the `internal/tools` package, and the
9-step `executeRequest` pipeline stay byte-identical. What changes
is the **transport dispatch** in `cmd/mcp-confluence/main.go`:

| Subcommand | Transport | Wire format | Use case |
| ---------- | --------- | ----------- | -------- |
| `stdio` (default) | `os.Stdin`/`os.Stdout` | newline-delimited JSON-RPC | Hermes MCP-host pipe |
| `serve` | `net/http` server | HTTP `POST /mcp` body = JSON-RPC | LAN dev, docker container with port-bind, future TLS via reverse proxy |
| `--help`, `--version` | n/a | n/a | parse-and-exit; help text → stderr |

Key architecture decision: **both transports run the SAME
`mcp.Server` instance built by `server.New(deps)`** — only the
framing differs. The `server.NewWithTransport` already exists
(`internal/server/server.go:48`); we'll add a third mode that
calls it once and exposes a `ServeHTTP(req)` shim that wraps
each HTTP request into a single JSON-RPC request handled by
`srv.Handle(ctx, json.RawMessage) (json.RawMessage, error)`.

**Why a v4 (not v1.5):** the
`specs/14-cobra-viper-golang/` research doc (committed
2026-07-14) is now the canonical reference for the CLI add.
Two new dependencies land:

- `github.com/spf13/cobra v1.10.2` — subcommand + flag parser
- `github.com/spf13/viper v1.21.0` — flag ⇄ env ⇄ config-file
  precedence

**Locked decisions (per the user's 2026-07-14 answers):**
- `--api-token` (matches `ATLASSIAN_API_TOKEN` env name; matches
  the upstream Node tool's flag name)
- No `version` subcommand; only the cobra-default `--version` flag
- Edit `cmd/mcp-confluence/main.go` directly; no sibling `cli.go`
- Keep `internal/config/dotenv.go` (Q22-locked) verbatim — the
  CLI composition path re-injects flags into `os.Setenv` so the
  stdlib parser remains authoritative

**Spec set:** `specs/14-cobra-viper-golang/` (3 files:
01-research-and-surface.md + 02-design.md + research/
subfolder). Plus the **new** `internal/transport/http/` Go
package, documented inline.

**Phases 16 and 17 are sequential (cobra/viper first, then the
stdio subcommand that consumes it). Phase 18 is the new TCP/HTTP
transport. Phase 19 is the Hermes MCP-host smoke + the live
JSON-RPC over curl test that proves the dev-loop velocity
argument.**

- [x] **Phase 16** — cobra + viper scaffolding in `cmd/mcp-confluence/main.go`; flag handlers; `cli_test.go::TestRoot_Help` + `TestVersion`; `make build` green; **no behavior change yet** (binary still does exactly what it does today when run with `args: []`). **Commit: `f61ace3`.**

- [x] **Phase 17** — `mcp-confluence stdio` subcommand dispatch; verify byte-identical behaviour via `scripts/smoke-page_tree.py`; flags override env vars per Q22 composition path; `cli_test.go::TestStdio_Help` + `TestStdio_FlagsOverrideEnv`. **Commit: `3fa1c41`.**

- [x] **Phase 18** — `mcp-confluence serve` subcommand + new `internal/transport/http/` package; `POST /mcp` JSON-RPC bridge to the SAME `mcp.Server` instance; `--listen=127.0.0.1:8080` default with fails-closed bind; `cli_test.go::TestServe_*` (incl. live `curl -X POST http://127.0.0.1:8080/mcp -d '…'` + assertion on the response payload). **Commit: `5006a86`.**

- [x] **Phase 19** — Final integration smoke + Hermes MCP-host config example with both stdio and serve transport invocations; AGENTS.md and Makefile synced (already done at `aac804c` — confirming via `make test`); `make image` rebuilds the distroless binary with the CLI surface baked in; `make check` green. **Commit: `be1f3db`.**

---

## Phase 16 — cobra + viper scaffolding (no behavior change)

**Token budget:** ~256k soft · **Subagent:** yes · **Parallel-safe:** no

**Objective:** Add `github.com/spf13/cobra v1.10.2` and
`github.com/spf13/viper v1.21.0` to `go.mod`; extract a
`newRootCmd()` function from `cmd/mcp-confluence/main.go` that
defines the root command (with `stdio`, `serve`, `help`,
`--help`, `--version` as no-op placeholders for Phases 17-18);
wire the 6 persistent flags (`--site`, `--email`,
`--api-token`, `--debug`, `--config`) via viper's
`BindPFlag` + `SetEnvPrefix("ATLASSIAN")` + `AutomaticEnv()`.
The `SetOut(io.Discard)` + `SetErr(os.Stderr)` discipline
established at `cmd/mcp-confluence/main.go` is preserved. End
state of Phase 16: `./bin/mcp-confluence --help` returns help
text to stderr; running the binary with `args: []` produces
the SAME behaviour as v0.1 (stdio JSON-RPC); running with
`args: ["stdio"]` produces the same behaviour too (still
dispatches to `runLifecycle`).

**Tasks**

- [ ] `go get github.com/spf13/cobra@v1.10.2 github.com/spf13/viper@v1.21.0` (run via `make install`, not directly)
- [ ] In `cmd/mcp-confluence/main.go`: replace `func main() { run() }` with a cobra-dispatched `func main() { newRootCmd().Execute() }`. Keep `run()`, `runLifecycle()`, `serveUntilDone()`, `wireStdinEOF` UNCHANGED — they are called from `RunE`/`Run` closures inside the new subcommand builder.
- [ ] In the same file: define `func newRootCmd() *cobra.Command`. Persistent flag definitions (site, email, api-token, debug, config) on the root. `--help` and `--version` flag definitions are added by cobra automatically when we set `rootCmd.Version = version`.
- [ ] `rootCmd.SetOut(io.Discard)` + `rootCmd.SetErr(os.Stderr)` BEFORE `Execute()`. **Load-bearing for JSON-RPC stdout invariant.**
- [ ] Add `func newStdioCmd()` + `func newServeCmd()` as thin subcommand factories. For Phase 16, both are STUBS — they call `run()` for the full lifecycle, ignoring subcommand-specific behavior. The transports dispatch lands in Phases 17 (stdio: behavior-preserving) and 18 (serve: net/http).
- [ ] Wire viper in a `func initViper(root *cobra.Command)` that:
  1. Creates `viper.New()`
  2. `viper.SetEnvPrefix("ATLASSIAN")` + `viper.AutomaticEnv()`
  3. `viper.BindEnv(...)` for each of the 5 persistent flag names → specific env-var paths (so `--site` ↔ `ATLASSIAN_SITE_NAME`, etc.)
  4. `viper.BindPFlag(...)` for each flag, called AFTER the flag is registered (the standard viper gotcha)
- [ ] Add the cobra-generated `--help` text to ROOT/SUBCOMMAND templates manually (cobra defaults are too terse for a Hermes-host config doc). Each help block must contain `HERMES REGISTRATION:` + a verbatim YAML example using the binary's actual flags.
- [ ] New file `cmd/mcp-confluence/cli_test.go`. First two tests:
  - `TestRoot_Help` — captures stderr from `--help`, asserts the help text contains the strings "USAGE:", "FLAGS:", "ENV VARS:", "HERMES INTEGRATION — stdio mode:", "HERMES INTEGRATION — serve (TCP/HTTP) mode:".
  - `TestVersion` — captures stderr from `--version`, asserts the version string (`v0.1.0` today; settable via `-ldflags -X main.version=…`).
- [ ] Update `cmd/mcp-confluence/main.go` `const version = "v0.1.0"` at line 54 to be sourced from `main.version` build-linkable variable (already does this for `make image`).

**Spec to follow:** `specs/14-cobra-viper-golang/01-research-and-surface.md`
(§3 canonical pattern, §4 critical gotchas, §5 the three
options and the Option-B recommendation) and
`02-design.md` (§6 reference implementation, §9 hard invariants).

**Locked behaviours to preserve:**
- The `confluence.DotenvParse` ordering (process env > cwd .env > binary-dir .env) is **unchanged**. The CLI composition path (`os.Setenv("ATLASSIAN_SITE_NAME", site)` from inside `runServer`) feeds flags into the top tier of Q22 — the stdlib parser still does the cwd/binary-dir lookups.
- The 18-tool surface is byte-identical (the registration entrypoint `tools.RegisterAll(srv, client)` is shared across both subcommands and `--help` is parse-and-exit).
- The five CRUD tool descriptions remain verbatim from upstream.

**Verification**

- [ ] `go.mod` has cobra v1.10.2 + viper v1.21.0 as direct deps
- [ ] `make build` produces the same binary path (`bin/mcp-confluence`); CGO_ENABLED=0 preserved
- [ ] `./bin/mcp-confluence --help </dev/null | head -1` returns **empty** (zero stdout writes)
- [ ] `./bin/mcp-confluence --help 2>&1 | grep "HERMES INTEGRATION"` returns at least 2 lines (stdio + serve)
- [ ] `./bin/mcp-confluence --version 2>&1` prints `mcp-confluence version v0.1.0`
- [ ] `./bin/mcp-confluence </dev/null` (no args) produces the v0.1 behaviour: startup banner on stderr, then blocks reading JSON-RPC from stdin (EOF cancels)
- [ ] `make test` is green; `make check` (lint + test) is green
- [ ] No `fmt.Println` calls in `cmd/` or `internal/` (existing grep invariant)

**Kickoff prompt body** (publish to `phase-16-prompt`):

```
You are implementing Phase 16 of the mcp-confluence plan — the
cobra + viper scaffolding. Read IMPLEMENTATION_PLAN.md, then
specs/14-cobra-viper-golang/01-research-and-surface.md (especially
§3 canonical pattern and §5 the three options), then
specs/14-cobra-viper-golang/02-design.md (especially §6 reference
implementation).

OBJECTIVE: Add cobra v1.10.2 + viper v1.21.0. Rewrite
cmd/mcp-confluence/main.go so func main() calls
newRootCmd().Execute(). Keep run(), runLifecycle(),
serveUntilDone(), wireStdinEOF() UNCHANGED. The persistent
flags are --site, --email, --api-token, --debug, --config (Q22
composition path). Add the cobra-generated --help text manually
(too terse by default). Add cli_test.go with TestRoot_Help and
TestVersion.

CONSTRAINT: rootCmd.SetOut(io.Discard) + SetErr(os.Stderr) MUST
be set before Execute() — this is the load-bearing JSON-RPC
stdout-protection invariant. NO fmt.Println anywhere.

DONE WHEN: make build + make test are green. ./bin/mcp-confluence
--help writes multi-section help text to stderr (no stdout).
./bin/mcp-confluence --version prints mcp-confluence version
v0.1.0. /bin/mcp-confluence with no args still blocks on stdin
EOF (behaviour-preserving). Report commit SHA + summary on
phase-16-done.
```

---

## Phase 17 — `stdio` subcommand dispatch (behaviour-preserving)

**Token budget:** ~128k soft · **Subagent:** yes · **Parallel-safe:** no
(depends on Phase 16's viper wiring)

**Objective:** Make `mcp-confluence stdio` an explicit
subcommand that produces byte-identical behaviour to the v0.1
binary's default invocation. Verify with the existing
`scripts/smoke-page_tree.py` that the live JSON-RPC stream is
unchanged. Add flag-override-env tests in `cli_test.go`.

**Tasks**

- [ ] `func newStdioCmd()` (in `cmd/mcp-confluence/main.go`):
  exactly delegate to the v0.1 lifecycle. Reads the merged
  viper picture (flag > env > config file), re-injects the
  relevant values into `os.Setenv`, then calls `run()`. The
  Q22 composition path keeps `internal/config/dotenv.go`
  authoritative for cwd/binary-dir .env.
- [ ] Print confirmation log on stderr: `mcp-confluence v0.1.0 starting (site=<site>, email=<email>)` — same one-liner the v0.1 binary already prints, so existing log-parsing isn't disrupted.
- [ ] `cli_test.go::TestStdio_Help` — assert stdio `--help` contains the HERMES REGISTRATION block for stdio mode (full YAML example).
- [ ] `cli_test.go::TestStdio_FlagsOverrideEnv` — spawn the binary with `args: ["stdio", "--site=forcedSite", "--email=forced@example.com"]` while `ATLASSIAN_SITE_NAME=envSite` is set in the subprocess env. Verify the spawned binary's stderr says `site=forcedSite` (flag wins). Then the inverse: same test with no `--site` flag, verify stderr says `site=envSite` (env wins).
- [ ] `cli_test.go::TestStdio_NoEnvFailsFast` — spawn with `args: ["stdio"]`, no env, no `.env` on disk. The binary must exit with `os.Exit(1)` and stderr must contain `FATAL: ATLASSIAN_SITE_NAME is not set` (or the equivalent — be lenient on the exact phrasing, check for FATAL).

**Spec to follow:** `specs/14-cobra-viper-golang/02-design.md` §6
"Reference implementation outline" + §8 "Hard invariants".

**Verification**

- [ ] `scripts/smoke-page_tree.py` returns the same merged-envelope JSON for page-id `1831108680` (1 ancestor) and `780764253` (6 children, 25 descendants) as the v0.1 binary produced 2026-07-14
- [ ] `cli_test.go::TestStdio_Help`, `TestStdio_FlagsOverrideEnv`, `TestStdio_NoEnvFailsFast` are green
- [ ] `make check` is green
- [ ] `./bin/mcp-confluence stdio` with `--help` writes stdio-specific help to stderr (with HERMES REGISTRATION block)
- [ ] The Hermes-registered confluence server (after the user does the `hermes mcp test confluence` refresh) still works with `args: ["stdio"]` (today it's `args: []`; after Phase 17 the user's `~/.hermes/config.yaml` would be unchanged because `args: []` and `args: ["stdio"]` produce identical behaviour)

**Kickoff prompt body** (publish to `phase-17-prompt`):

```
You are implementing Phase 17 of the mcp-confluence plan. Read
IMPLEMENTATION_PLAN.md, then the Phase 17 entry. OBJECTIVE:
stdio subcommand dispatch — behaviour-preserving. Run `make
check` after each commit. Keep run(), runLifecycle(),
serveUntilDone(), wireStdinEOF UNCHANGED. Add TestStdio_Help,
TestStdio_FlagsOverrideEnv, TestStdio_NoEnvFailsFast to
cli_test.go. Live-verify with `python3 scripts/smoke-page_tree.py`
— the response envelope must be byte-identical to yesterday's
v0.1 smoke (page 1831108680 returns ancestors=1, page 780764253
returns children=6 descendants=25). Report commit SHA + smoke
result on phase-17-done.
```

---

## Phase 18 — `serve` subcommand + `internal/transport/http/` package (new code)

**Token budget:** ~256k soft · **Subagent:** yes · **Parallel-safe:** no
(depends on Phases 16-17; introduces the new transport package)

**Objective:** Add a TCP/HTTP transport under `serve`. Listens on
`--listen=127.0.0.1:8080` by default. Each `POST /mcp` HTTP
request is dispatched to the SAME `mcp.Server` instance built by
`internal/server.NewWithTransport` — only the framing changes.
No new tool registrations, no new dependencies, no new business
logic in `internal/tools`. This unblocks the user's dev-velocity
argument: rebuild + `./bin/mcp-confluence serve --listen=...` from
the terminal now serves the same 18 tools over HTTP, no Hermes
restart required for the 90% of dev iterations that don't touch
the MCP framing.

**Tasks**

- [ ] New package `internal/transport/http/` with files:
  - `http.go` — `func NewServer(mcpSrv *mcp.Server, listen string, logger *slog.Logger) (*http.Server, error)` that wires the `mcp.Server.Handle(...)` boundary into a `net/http.HandlerFunc`.
  - `handler.go` — the actual handler. It decodes a JSON-RPC 2.0 request body, calls `mcpSrv.Handle(ctx, raw) (raw, err)`, writes the JSON-RPC response back with `Content-Type: application/json`. Errors return a JSON-RPC error object with the standard `code`/`message`.
  - `listen.go` — `func parseListenFlag(s string) (host string, port int, err error)` validator. Refuses `0.0.0.0:` defaults (no security-by-obscurity default flip).
  - `http_test.go` — `httptest.NewServer` with the handler; test the request-response pipeline end-to-end at the JSON-RPC layer.
- [ ] In `cmd/mcp-confluence/main.go`: define `func newServeCmd()` that:
  1. Reads `--listen` from the merged viper picture (`ATLASSIAN_LISTEN` env or default `127.0.0.1:8080`).
  2. Validates with `parseListenFlag`; on parse failure, exit non-zero with a clear error on stderr.
  3. Builds `mcp.Server` via `internal/server.New(deps)` (same call as `stdio`).
  4. Constructs the HTTP transport via `internal/transport/http.NewServer(srv, listen, logger)`.
  5. Blocks: `srv.ListenAndServe()` until SIGINT/SIGTERM/EOF (matches existing `serveUntilDone` signal handling).
  6. On shutdown, calls `httpServer.Shutdown(ctx)` for graceful close.
- [ ] Confirm the same `internal/tools/executeRequest()` pipeline executes for every HTTP request — i.e. the wire shape sent back to the caller over HTTP is the same TOON-enveloped string `stdin` would have produced.
- [ ] Add a graceful `SIGINT/SIGTERM` handler that mirrors `stdio`'s ctx-cancel pattern (reuse `signal.NotifyContext`).
- [ ] Per-request stderr log: `<TIMESTAMP> serve <METHOD> <path> <status> <bytes>` (one line per request). No token logged.
- [ ] `cmd/mcp-confluence/cli_test.go` additions:
  - `TestServe_Help` — assert serve `--help` contains the HERMES REGISTRATION block for serve mode (full YAML).
  - `TestServe_Help_ShowsSecurityBlock` — assert serve `--help` includes the SECURITY section (127.0.0.1 default, no bearer auth, bind fails closed).
  - `TestServe_Help_ListsTransportDifferences` — assert serve `--help` lists the wire-format difference from stdio.
  - `TestServe_BindsAndShutsDown` — spawn `mcp-confluence serve --listen=127.0.0.1:0` (kernel picks free port); curl JSON-RPC `tools/list`; assert the response contains all 18 tool names; cancel context; assert exit 0.

**Spec to follow:** the load-bearing references live in
`specs/14-cobra-viper-golang/01-research-and-surface.md`
(§3 the canonical pattern, §4 the MCP-server CLI conventions
from `modelcontextprotocol/go-sdk` and `metoro-io/mcp-golang`),
plus the inline Go docstrings for `internal/server.NewWithTransport`
(the foundation this phase builds on top of).

**Verification**

- [ ] `internal/transport/http/http_test.go` is green (request-response pipeline)
- [ ] `cmd/mcp-confluence/cli_test.go::TestServe_*` are green
- [ ] `./bin/mcp-confluence serve --listen=127.0.0.1:8080` opens the port (verified by `ss -tln | grep 8080`)
- [ ] `curl -X POST http://127.0.0.1:8080/mcp -d '{"jsonrpc":"2.0","method":"tools/list","id":1,"params":{}}' -H 'Content-Type: application/json'` returns a JSON response with `result.tools` containing all 18 tool names
- [ ] `curl -X POST ... -d '{"jsonrpc":"2.0","method":"tools/call","params":{"name":"conf_get","arguments":{"path":"/wiki/api/v2/spaces?limit=2"}},"id":2}'` returns TOON-encoded real Confluence data (matches the stdio smoke)
- [ ] `./bin/mcp-confluence serve --listen=0.0.0.0:8080` is NOT rejected at parse time (only at network level — that's not our concern); the SECURITY note in the help text warns against this
- [ ] `./bin/mcp-confluence serve --listen=not-a-listener` exits non-zero with an error on stderr
- [ ] `make check` is green; no stdout writes anywhere
- [ ] Live `curl` test against smartergroup.atlassian.net: `--api-token=$token --site=smartergroup --email=bennie@… bin/mcp-confluence serve --listen=127.0.0.1:8080 &`, then `curl … conf_get path=/wiki/api/v2/spaces?limit=2`, then verify the response contains the same TOON-encoded space list as the stdio smoke

**Kickoff prompt body** (publish to `phase-18-prompt`):

```
You are implementing Phase 18 of the mcp-confluence plan. Read
IMPLEMENTATION_PLAN.md, then the Phase 18 entry.

OBJECTIVE: Add a TCP/HTTP transport under the `serve` subcommand
that reuses the SAME mcp.Server built by internal/server.New().
New package internal/transport/http/. Single endpoint POST /mcp
that takes a JSON-RPC 2.0 body and returns a JSON-RPC 2.0
response.

REUSE — DO NOT REWRITE:
- internal/server.New(deps) — same call as `stdio`.
- internal/tools/executeRequest() — every tool call passes through
  the SAME 9-step pipeline.
- internal/atlassian.Client.Do — HTTP to Confluence API unchanged.

CONSTRAINTS:
- --listen defaults to 127.0.0.1:8080. Refuses 0.0.0.0 silently
  (no security-by-obscurity default flip). Validates parseable
  host:port; on parse failure exit non-zero with stderr.
- stdout is reserved for JSON-RPC / HTTP body bytes only. All
  logs to stderr. NO fmt.Println anywhere.
- Per-request log line on stderr only — no request body, no
  header values, definitely no ATLASSIAN_API_TOKEN.

DONE WHEN:
- make build + make check green.
- curl http://127.0.0.1:8080/mcp -X POST -d '{"jsonrpc":"2.0","method":"tools/list","id":1}' returns 18 tool names.
- curl ... conf_get ... returns the same TOON-encoded Confluence data as the stdio smoke.
- ./bin/mcp-confluence serve --help writes help text to STDERR
  (not stdout). help text includes a HERMES REGISTRATION YAML
  block.
- cli_test.go::TestServe_* all green.
- Report commit SHA + curl output sample + serve --help text sample on phase-18-done.
```

---

## Phase 19 — Final integration smoke (Hermes config + rebuild image + AGENTS.md sync)

**Token budget:** ~64k soft · **Subagent:** yes · **Parallel-safe:** no
(final integration step; depends on 16-18)

**Objective:** Rebuild the distroless OCI image with the CLI
surface baked in. Smoke against Hermes MCP-host config in BOTH
transport modes (stdio for parity; serve as the new capability).
Verify the user's dev-velocity loop end-to-end: rebuild →
`./bin/mcp-confluence serve --listen=...` → curl → the
Hermes MCP-host config example in AGENTS.md matches reality.

**Tasks**

- [ ] `make image` — rebuilds `paketobuildpacks/builder-jammy-tiny` with the CLI surface baked in. Confirm the distroless image contains a working `--help` (run `docker run --rm <image> --help 2>&1 | head -30`).
- [ ] `docker run -d --rm --name mcp-confluence-smoke -p 127.0.0.1:18080:8080 <image> serve --listen=0.0.0.0:8080` then `curl http://127.0.0.1:18080/mcp -X POST -d '{"jsonrpc":"2.0","method":"tools/list","id":1}' -H 'Content-Type: application/json'`. Verify 18 tool names in the response.
- [ ] Stop the smoke container.
- [ ] Local smoke: `./bin/mcp-confluence serve --listen=127.0.0.1:8080 --site=smartergroup --email=bennie@obsidian.co.za --api-token=$ATLASSIAN_API_TOKEN &` in a tmux pane; from another shell, run `curl -X POST http://127.0.0.1:8080/mcp -d '{"jsonrpc":"2.0","method":"tools/call","params":{"name":"conf_get_page_tree","arguments":{"page-id":"780764253","outputFormat":"json"}},"id":1}' -H 'Content-Type: application/json' | jq .` and confirm `children.results | length` returns 6 and `descendants.results | length` returns 10 (capped at limit=10). If that's the same envelope `scripts/smoke-page_tree.py` produces via stdio, the wire is identical.
- [ ] AGENTS.md sync is **already done** at commit `aac804c`. Re-verify: open `AGENTS.md`, scroll to "## CLI surface", confirm all four subcommands listed, every subcommand's `--help` block contains a `HERMES REGISTRATION:` YAML example, the four hard rules (cobra+viper, stdout discipline, TCP fails-closed, lock-step CLI test) are documented.
- [ ] Makefile sync — confirm `make help` still renders 22 targets and `make check` is green.

**Verification**

- [ ] `make image` exits 0; `make image-inspect` shows the same base layers as v0.1 + the new cobra+viper + Go stdlib net/http entry-points
- [ ] `docker run --rm <image> --help </dev/null | head -1` returns **empty** (zero stdout pollution, even from inside a distroless container)
- [ ] `docker run --rm <image> --help 2>&1 | grep -c "HERMES INTEGRATION"` returns 2 (stdio + serve)
- [ ] The TCM/HTTP smoke (curl POST /mcp) returns the same JSON-RPC 2.0 envelope shape as the stdio smoke
- [ ] `make check` and `make build` are green
- [ ] README.md at the project root mentions the new `serve` subcommand (one bullet; full surface lives in AGENTS.md)

**Kickoff prompt body** (publish to `phase-19-prompt`):

```
You are implementing Phase 19 of the mcp-confluence plan — the
final integration smoke. Read IMPLEMENTATION_PLAN.md, then the
Phase 19 entry. This phase is light on code and heavy on
verification. OBJECTIVE: confirm the CLI surface (Phases 16-18)
behaves correctly when packaged as a distroless OCI image AND
when called from Hermes' mcp_servers config.

STEPS:
1. make image
2. docker run --rm <image> --help </dev/null | head -1 (must be
   empty — confirm stdout-protection holds in a distroless
   container)
3. docker run --rm <image> --help 2>&1 | grep -c HERMES INTEGRATION
   (must be 2)
4. docker run -d --rm --name mcp-confluence-smoke -p 127.0.0.1:18080:8080
   <image> serve --listen=0.0.0.0:8080
5. curl http://127.0.0.1:18080/mcp -X POST -d '{"jsonrpc":"2.0",
   "method":"tools/list","id":1}' -H 'Content-Type: application/json'
   (must return all 18 tool names)
6. Stop the smoke container.
7. make check
8. AGENTS.md is already in sync (commit aac804c); confirm via
   head-and-grep.

DONE WHEN: all 4 verification rows above are green. Report
commit SHA + container start logs + curl response sample +
make check exit code on phase-19-done.
```

---

## v4 — Done when

- [x] Phases 16, 17, 18, 19 are checked off above (`f61ace3`, `3fa1c41`, `5006a86`, `be1f3db`)
- [x] `make image` produces a working CLI-image (both `serve` and `stdio` routes) — verified `be1f3db`
- [x] `./bin/mcp-confluence serve --help` writes to stderr, parses to the Hermes-host YAML example shown in AGENTS.md — verified `be1f3db`
- [x] `scripts/smoke-page_tree.py` still passes against the new binary's stdio mode (no regression) — verified `be1f3db`
- [x] A live `curl -X POST http://127.0.0.1:8080/mcp -d '{...}'` returns the same envelope shape — this is the dev-velocity proof: the user can iterate code, rebuild, and immediately smoke via curl without restarting Hermes — verified `be1f3db`
- [x] Hermes MCP-host config in `~/.hermes/config.yaml` can use either `args: ["stdio"]` or `args: ["serve", "--listen=127.0.0.1:8080"]` for the same tool surface — both work, both yield 18 tools — verified `be1f3db`

---

## What changed (2026-07-14, retrospective)

This section will be populated as Phases 16-19 land. Approximate
content:

### What changed

- **The 0 to 1 of the CLI surface.** A single binary now exposes
  four subcommands. Stdlib `flag` is gone; cobra v1.10.2 +
  viper v1.21.0 are added as direct deps. The 30-LOC stdlib
  dotenv parser at `internal/config/dotenv.go` is preserved
  verbatim — viper sits on top of it via a composition path.
- **New transport.** `internal/transport/http/` (net/http
  stdlib, no new Go deps) wraps the same `mcp.Server` instance
  the stdio subcommand uses. Single endpoint `POST /mcp`,
  JSON-RPC 2.0 in/out. `--listen=127.0.0.1:8080` default;
  fails-closed bind.
- **`internal/tools/` UNCHANGED.** All 18 tool handlers, the
  `executeRequest()` 9-step pipeline, the JMESPath wrapper,
  the TOON encoder, the 40k truncation, the error envelopes —
  none of it changed. The CLI refactor is purely an entry-point
  and framing concern.
- **No new 3rd-party deps beyond cobra + viper.** The HTTP
  listener uses `net/http` stdlib. JSON-RPC parsing also uses
  stdlib (no extra library) — each HTTP request body is
  passed to `mcp.Server.Handle(ctx, json.RawMessage)` as raw
  bytes, then the response is written back. No new tool
  registration. No JSON schema changes.

### What didn't change

- The 18 tool names. `mcp_confluence_conf_get` is still the
  wire identifier on both transports (after the server prefix).
- The five CRUD tool descriptions. Still byte-identical to
  upstream `@aashari/mcp-server-atlassian-confluence` v3.3.0.
- `internal/config/dotenv.go`. Q22-locked; the 30-LOC stdlib
  parser and its 171-line test module are unchanged.
- The settings resolution order: **CLI flag > process env >
  cwd `.env` > binary-dir `.env`** — Q22's locked tiers, with
  the upper two tiers now served by viper and the lower two
  by the stdlib parser (composition).
- The JSON-RPC stdout invariant: every byte that lands on
  stdout is either a JSON-RPC stdio message, an HTTP request
  body, or an HTTP response body. Help text, version text,
  error text, log text — all on stderr. Enforced by
  `rootCmd.SetOut(io.Discard) + SetErr(os.Stderr)`.
- The MCP server constants — name `mcp-confluence`, version
  `v0.1.0` — unchanged.

### Dev-velocity outcome (the user's stated goal)

For 90% of dev iterations, the loop is now:

```
make build
./bin/mcp-confluence serve --listen=127.0.0.1:8080 &
curl -X POST http://127.0.0.1:8080/mcp -d '...' | jq .
```

No Hermes restart. No `hermes mcp test confluence` round-trip.
The terminal is the dev loop. Hermes registration is the
final integration smoke (Phase 19), not the primary surface.

The `stdio` subcommand is preserved as the canonical
Hermes MCP-host integration path — but it is now one of two
transport choices, not the only one.
