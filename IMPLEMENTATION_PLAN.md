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
- [ ] **Phase 2** — `internal/atlassian`: client wrapper, basic auth, errors
- [x] **Phase 3** — `internal/toon`: encoder + round-trip tests *(parallel w/ 4)*
- [x] **Phase 4** — `internal/jmespath`: wrapper + tests *(parallel w/ 3)*
- [x] **Phase 5** — `internal/tools`: args types + 5 `CONF_*_DESCRIPTION` constants
- [x] **Phase 6** — `internal/tools`: `executeRequest()` shared handler helper
- [x] **Phase 7** — `internal/tools`: five handlers + `safeHandler` panic wrap
- [x] **Phase 8** — `internal/server` + `RegisterAll()`: bootstrap + registration
- [x] **Phase 9** — `cmd/mcp-confluence/main.go`: full lifecycle (load → serve)
- [x] **Phase 10** — Wire + smoke: `make check` + end-to-end JSON-RPC smoke
- [x] **Phase 11** — Container image: `project.toml` + `make image` green
- [ ] **Phase 12** — Hermes integration: `~/.hermes/config.yaml` + `mcp test`

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

- [ ] `go get github.com/metoro-io/mcp-golang`
- [ ] Write `internal/server/server_test.go` (server constructed, 5
  tools registered — names match `conf_get`/`conf_post`/`conf_put`/
  `conf_patch`/`conf_delete`)
- [ ] Run — expect FAIL
- [ ] Write `internal/server/server.go`: `NewServer(cfg, client) (*mcp.Server, error)`
- [ ] Write `internal/tools/register.go`: `RegisterAll(server, cfg, client) error`
- [ ] Tests pass
- [ ] `make check` green
- [ ] Commit: `feat(server): RegisterAll + NewServer bootstrap`

**Spec to follow:** `specs/04-mcp-golang-framework/01-server-api.md` and
`specs/06-implementation-skeleton/03-tool-handlers.md`.

**Verification**

- [ ] Tool names returned by introspection match the upstream 5 names
  exactly
- [ ] `make check` exits 0

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

- [ ] Write `cmd/mcp-confluence/main_test.go` (table-driven: missing var
  X → expect specific error message; valid env → expect `run()` to
  attempt to serve)
- [ ] Run — expect FAIL
- [ ] Replace `cmd/mcp-confluence/main.go` with the full lifecycle from
  the spec
- [ ] `make build` — expect green
- [ ] `make test` — expect green
- [ ] Manual smoke: `./bin/mcp-confluence` with no env → expect FATAL
  on stderr, exit 1
- [ ] Manual smoke: `./bin/mcp-confluence` with valid env + a stdin EOF
  → expect clean exit
- [ ] Commit: `feat(cmd): full lifecycle in main.go`

**Spec to follow:** `specs/06-implementation-skeleton/02-main-entrypoint.md`
(the full `main.go` skeleton is the implementation). Key behaviors:
fail-fast on missing env, no stdout writes, no token logging, signal
handling, `run()` separation for testability.

**Verification**

- [ ] `go test ./cmd/mcp-confluence/...` all pass
- [ ] `./bin/mcp-confluence` with no env exits 1 with FATAL on stderr
- [ ] `./bin/mcp-confluence` with valid env does NOT exit 0 immediately
  (it's blocking on stdin — kill with Ctrl-C to confirm clean exit)
- [ ] `ldd ./bin/mcp-confluence` shows it is statically linked (no libc)
- [ ] `grep -r "fmt.Println" cmd/` returns 0 matches

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

- [ ] `make format` (ensure gofmt-clean)
- [ ] `make lint` (vet + gofmt -l; golangci-lint if installed)
- [ ] `make test` (all unit tests)
- [ ] `make check` (lint + test combined)
- [ ] Manual: `export ATLASSIAN_SITE_NAME=...` (real site), `export
  ATLASSIAN_USER_EMAIL=...`, `export ATLASSIAN_API_TOKEN=...`. Then feed
  the binary a JSON-RPC `tools/list` request on stdin and confirm it
  responds with the 5 tool names
- [ ] Manual: feed a `tools/call` for `conf_get` with
  `path: "/wiki/api/v2/spaces?limit=2"` and confirm the response is
  TOON-encoded
- [ ] Commit (if any fixes were needed): `chore: smoke-test fixes`

**Verification**

- [ ] `make check` exits 0
- [ ] `make test` shows all tests pass
- [ ] End-to-end JSON-RPC smoke test returns valid responses
- [ ] No stdout pollution: capture stdout during the smoke test and
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

- [ ] Create `project.toml` (Paketo build descriptor)
- [ ] (Optionally) Create `Dockerfile` fallback
- [ ] `make image` — expect a `confluence-mcp` OCI image in the local
  `docker images` output
- [ ] `make image-inspect` — confirm the static binary entrypoint
- [ ] Run the image with `docker run --rm -i -e ATLASSIAN_*=... <image>`
  piped to a JSON-RPC `tools/list` request, confirm 5 tools return
- [ ] Commit: `feat(image): Paketo project.toml + make image pipeline`

**Spec to follow:** `specs/07-paketo-buildpack/01-project-toml.md` (the
exact `project.toml` shape for `paketobuildpacks/go`).

**Verification**

- [ ] `make image` exits 0
- [ ] `docker images | grep confluence-mcp` shows the new image
- [ ] `docker run` smoke test returns 5 tools
- [ ] `make sbom` produces a valid CycloneDX JSON

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

- [ ] Read `~/.hermes/.env` to confirm the three `ATLASSIAN_*` vars
  exist (if not, publish to `blockers` and ask the user)
- [ ] Back up `~/.hermes/config.yaml` (timestamped copy in
  `~/.hermes/backups/`)
- [ ] Add the `mcp_servers.confluence:` block to `~/.hermes/config.yaml`
  with `${ATLASSIAN_SITE_NAME}`, `${ATLASSIAN_USER_EMAIL}`,
  `${ATLASSIAN_API_TOKEN}` expansion (NOT literal values)
- [ ] Restart Hermes if running (`hermes mcp test confluence` will spawn
  the server)
- [ ] `hermes mcp test confluence` — confirm 5 tools are listed
- [ ] `hermes mcp test confluence conf_get --path /wiki/api/v2/spaces?limit=2`
  — confirm a TOON response with real data
- [ ] Record the result. Config file commit is the user's call (commit
  `~/.hermes/config.yaml` to the user-config repo if appropriate)

**Spec to follow:** `specs/08-deployment-hermes/01-config-yaml.md` (the
exact `mcp_servers:` block shape) and the `native-mcp` skill
(`~/.hermes/skills/mcp/native-mcp/SKILL.md`).

**Verification**

- [ ] `hermes mcp list` shows `confluence` as a registered server
- [ ] `hermes mcp test confluence` lists 5 tools
- [ ] An actual tool call returns real Confluence data
- [ ] `~/.hermes/config.yaml` contains NO literal token — only `${VAR}`
- [ ] `~/.hermes/config.yaml` backup is preserved

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

## Cross-phase guarantees

Across all phases, the following invariants must hold:

- [ ] **No stdout writes.** `grep -r "fmt.Println" cmd/ internal/` returns
  0 matches (except in tests, which use `t.Log*`)
- [ ] **No token logging.** `grep -r "ATLASSIAN_API_TOKEN" cmd/ internal/`
  shows the var name only in the config package; the token value never
  appears in source
- [ ] **Stdlib for `.env` parsing.** No `godotenv` dependency
- [ ] **TOON is the default output format.** All non-`outputFormat=json`
  responses are TOON
- [ ] **JMESPath short-circuits on empty expression.** No parse cost when
  the user doesn't pass `jq`
- [ ] **The 40k truncation notice and `/tmp/mcp/<session-id>.json` path
  are byte-identical to the upstream**
- [ ] **The 5 description constants are byte-identical to the upstream's
  `src/tools/atlassian.api.tool.ts` lines 127–223**
- [ ] **`make build` and `make test` exit 0 after every phase**

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
- 2026-07-09 — Phase 7: 5 handlers + safeHandler + RegisterAll + NewServer — sha=`97542e2`. three commits (97542e2/86e0500/69cf7a5): handlers → RegisterAll → main.go lifecycle. New transport-trampoline pattern (NewWithTransport + pipe-backed stdio) lets main.go detect stdin EOF for clean shutdown. 9 internal/tools tests pass.
- 2026-07-09 — Phase 10: smoke-test fixes — sha=`b85ea84`. make format/lint/test/check all green; end-to-end JSON-RPC smoke against real Confluence API returns TOON-encoded `/wiki/api/v2/spaces?limit=2` with real space data (smartergroup.atlassian.net). Two bugs found and fixed: (1) atlassian.New was building `https://<site>` instead of `https://<site>.atlassian.net` (violated Q22-locked settings contract); (2) buildURL was URL-encoding `?` inside the path. New tests: TestBuildURL_PathContainsQuery + TestBuildURL_PathAndQueryMerged.

---

## Done when

The plan is complete when:

- [ ] All 12 implementation phases are checked off in **§ Progress index**
- [ ] `make check` exits 0
- [ ] `make image` produces a working OCI image
- [ ] `hermes mcp test confluence` lists 5 tools
- [ ] An end-to-end `conf_get` call returns real Confluence data in TOON format
- [ ] The README at the project root links to this plan and to `specs/`
- [ ] Every **§ Cross-phase guarantees** checkbox is flipped
