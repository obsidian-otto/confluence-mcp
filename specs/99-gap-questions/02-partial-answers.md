# 99.2 — Partial Gap Answers (locked 2026-07-09)

## Decisions log

| Q | Decision | Recorded | Lock source |
| - | -------- | -------- | ----------- |
| Q10 | Stderr-only logs (unchanged); `.env` file replaces upstream's `~/.mcp/configs.json` tier | 2026-07-09 (this update) | User OOB message: "MCP server should load its settings from the environmental variables or from the `.env` file inside the container or cli" |
| Q14 | Add a Makefile at v1 (not v1.1) as single source of truth | 2026-07-09 (this update) | User OOB message: "Make sure to add a Makefile as per the project skill as a single source of truth for all commands to run in this project" |
| Q22 | (new question — added this update) | 2026-07-09 | Same user OOB message; surfaced as Q22 with the resolved priority order |

**Open questions remaining:** 19 (Q1, Q2, Q3, Q4, Q5, Q6, Q7,
Q8, Q9, Q11, Q12, Q13, Q15, Q16, Q17, Q18, Q19, Q20, Q21).

---

## Q22 — `.env` file loading (LOCKED 2026-07-09)

### User's exact words

> "the MCP server should load it's settings from the
> environmental variables or from the `.env` file inside the
> container or cli."

(Verbatim; typo "it's" preserved per the literal-text-
preservation rule in the spec conventions reference.)

### Locked decision

The binary resolves each setting (the three required
`ATLASSIAN_*` vars + the optional `DEBUG`, `TRANSPORT_MODE`,
`ATLASSIAN_API_BASE_URL`) in this priority order
(**first non-empty wins**), evaluated once at process start:

| Priority | Source | Used when |
| -------- | ------ | --------- |
| 1 (highest) | **Process environment** (`os.Getenv`) | Always — every other source is only consulted if the var is unset or empty after this step |
| 2 | **`.env` file in the current working directory** | `make run` / CLI invocations where the user wants a project-local override without `export`-ing each shell session |
| 3 (lowest) | **`.env` file next to the binary** (`$0` resolves to the executable path; `.env` is `filepath.Dir(executable) + "/.env"`) | Containerized runs where the cwd is `/workspace` and the secret is shipped alongside the binary |

### Implementation rules (LOCKED)

- **No external dependency.** Use only the Go stdlib. Parse
  the `.env` file line-by-line in `internal/config/dotenv.go`
  (~30 lines of Go). Do **not** add
  `github.com/joho/godotenv` — the user explicitly rejected
  adding dependencies for a feature this small.
- **`.env` format.** Standard `KEY=VALUE` per line. Comments
  start with `#`. Empty lines are skipped. Quoted values have
  their quotes stripped (`KEY="VALUE"` → `VALUE`). No
  variable expansion.
- **Token redaction.** If a `.env` parse error occurs, the
  error message includes the offending line's key name and
  line number, but **never the value**. A line like
  `ATLASSIAN_API_TOKEN=ATATT3x...` that fails to parse
  produces the error `"invalid .env line 7
  (ATLASSIAN_API_TOKEN=<value redacted>)"`.
- **Missing `.env` is not an error.** The file may not exist;
  we silently fall through. Only parse errors are fatal.
- **Search order:** cwd `.env` first, then binary-dir
  `.env`. Process env (priority 1) always wins, so the
  `.env` files only provide defaults.
- **Hermes passthrough still works.** When the binary is
  launched by Hermes via `mcp_servers.confluence.env:`,
  the three `ATLASSIAN_*` vars are set in the subprocess
  env, and the `.env` files are never consulted.

### Implications (what changes downstream)

- The `internal/config/dotenv.go` file is added (new code
  path).
- The `cmd/mcp-confluence/main.go` calls
  `config.LoadFromEnv()` which internally walks the three
  sources (no API change at the call site).
- The `~/.mcp/configs.json` tier of the upstream is
  **dropped** (the user's `.env` decision replaces it).
  The Go port no longer needs a YAML parser dependency.
- `.gitignore` excludes `.env` (mode 0600 secrets never go
  in git). `.env.example` is committed as a template.
- The Makefile (Q14 lock) gains a `verify-env` target that
  prints the three vars (with the token length only, not
  the value) for quick debugging.
- The upstream's `dotenv` npm package is replaced by 30
  lines of stdlib Go in `dotenv.go`.

### Files updated in this lock

- `01-foundations/03-env-var-contract.md` — added "Settings
  resolution order (LOCKED)" section, updated Verification
  checklist.
- `02-upstream-aashari/03-lessons-and-quirks.md` — "Env-var
  precedence" section updated; the "What we did NOT inherit"
  table updated (`configs.json` dropped, `dotenv`
  reimplemented).
- `06-implementation-skeleton/01-file-layout.md` — added
  `internal/config/dotenv.go` to the file tree; added
  `.gitignore` and `.env.example` at the project root.
- `06-implementation-skeleton/02-main-entrypoint.md` —
  main.go entrypoint updated to document the resolution
  order in a comment; Verification checklist updated.

---

## Q14 — Makefile (LOCKED 2026-07-09)

### User's exact words

> "Make sure to add a Makefile as per the project skill as a
> single source of truth for all commands to run in this
> project."

### Locked decision

Add a Makefile at the Go module root with the full target
set documented in
`06-implementation-skeleton/04-makefile.md`. The Makefile is
now part of v1 (not v1.1 as the original gap Q14
recommendation suggested).

### Implications

- `06-implementation-skeleton/04-makefile.md` is the
  authoritative reference for every target, the `.PHONY`
  declaration, the configurable variables, and the
  expected invocation.
- The Makefile wraps `go build`, `go test`, `go vet`,
  `gofmt`, and `pack build` into a single interface.
- The Makefile's `help` target is the project's command
  index — any new command goes in the Makefile first, not
  in a scattered shell script.
- The CI pipeline (future gap Q15) will call `make all` as
  its primary build step.

### Files updated in this lock

- `06-implementation-skeleton/01-file-layout.md` — Makefile
  added to file tree; the "What we do NOT have" list
  updated.
- `06-implementation-skeleton/04-makefile.md` — NEW spec
  file authored.
- `01-foundations/03-env-var-contract.md` — Verification
  checklist item 7 added ("Makefile as single source of
  truth").

---

## What remains open (impact-ranked)

After this lock batch, 19 questions remain open. The
implementation phase does **not** block on any of them — the
recommendations in `01-questions.md` are the safe defaults.

| Q | Topic | Impact if wrong |
| - | ----- | --------------- |
| Q3 | HTTP transport at v1 | Low — stdio is the default Hermes transport |
| Q2 | OAuth 2.0 (3LO) | High — but explicitly deferred to v2 |
| Q4 | Data Center support | Medium — Cloud-only is the safe default |
| Q5 | Refuse stale `version.number` | Low — passthrough is upstream behavior |
| Q1 | Single-site vs multi-site | Low — single-site matches upstream |
| Q6 | Auto-pagination | Low — caller paginates is upstream behavior |
| Q7 | 429 retry | Low — pass-through is upstream behavior |
| Q8 | Attachment upload | Medium — but downstream rare for MCP |
| Q9 | CLI mode | Low — MCP is the use case |
| Q11 | Custom HTTP client (timeout/proxy) | Low — stdlib defaults work |
| Q12 | Image / audio content | Low — text-only is upstream |
| Q13 | Shared `ConfWriteArgs` type | Low — three types mirrors upstream |
| Q15 | CI / GitHub Actions | Medium — manual build works for now |
| Q16 | `--version` flag | Low — internal version is enough |
| Q17 | Builder SHA pinning | Low — tag pinning works |
| Q18 | Multi-arch builds | Low — amd64 is the user's platform |
| Q19 | Shared / remote deployment | Low — local is the use case |
| Q20 | Catalog `manifest.yaml` submission | Low — manual install works |
| Q21 | Test coverage reporting | Low — `go test` is enough at v1 |

### Next prompt

If the user wants to lock more decisions, reply with
`Q-N → option letter` for any of the open questions above.
The remaining decisions can wait until the v1 implementation
is running and the user has actual usage to inform
preferences.