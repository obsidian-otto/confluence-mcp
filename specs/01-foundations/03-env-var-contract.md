# 01.3 — Environment Variable Contract

> **LOCKED 2026-07-09:** User confirmed that the MCP server must
> load settings from environment variables **or** a `.env` file
> inside the container or CLI. See
> `99-gap-questions/02-partial-answers.md` Q22 for the lock
> rationale. The "Settings resolution order" section below
> captures the lock.

## Overview

The Go MCP server uses **exactly three required environment
variables** at v1, matching the upstream
`@aashari/mcp-server-atlassian-confluence` 1:1. The upstream's
env-var story is the de-facto standard (because the upstream is
the most-installed Confluence MCP server in the npm registry),
and changing it would force every existing user to reconfigure.
The Go port inherits the contract and **adds** a `.env` file
fallback for CLI / container runs where the user wants a
project-local override without `export`-ing each shell session.

## Sources

- `aashari/mcp-server-atlassian-confluence` README — env-var
  section, lines 38-42 and 76-78 of the upstream README.
- `aashari/mcp-server-atlassian-confluence` source:
  `src/utils/config.util.ts` and `src/utils/transport.util.ts`
  (the upstream reads `ATLASSIAN_SITE_NAME`,
  `ATLASSIAN_USER_EMAIL`, `ATLASSIAN_API_TOKEN` from
  `process.env`).
- Atlassian basic-auth docs:
  https://developer.atlassian.com/cloud/confluence/basic-auth-for-rest-apis/
  (the `email` + `api_token` pair is the documented basic-auth
  shape).
- Hermes native-mcp skill (env-var filtering rule):
  `~/.hermes/skills/mcp/native-mcp/SKILL.md` — "For stdio
  servers, Hermes does NOT pass your full shell environment to
  MCP subprocesses. ... All other environment variables ... are
  excluded unless you explicitly add them via the `env` config
  key."
- User OOB message (2026-07-09) — explicit `.env` requirement.

## Spec

### The three required env vars

| Variable | Required | Example | What it is |
| -------- | -------- | ------- | ---------- |
| `ATLASSIAN_SITE_NAME` | yes | `your-company` | The site prefix for `<your-company>.atlassian.net`. The server builds the base URL as `https://${ATLASSIAN_SITE_NAME}.atlassian.net/wiki`. |
| `ATLASSIAN_USER_EMAIL` | yes | `you@example.com` | The Atlassian account email used to issue the API token. |
| `ATLASSIAN_API_TOKEN` | yes | `ATATT3xFfGF0...` (44-char opaque string) | The Atlassian API token. Treated as a secret — never logged. |

### Optional env vars

| Variable | Default | What it does |
| -------- | ------- | ------------ |
| `DEBUG` | `false` | When `true`, logs each tool call to **stderr** with the request path, method, and (for write ops) body keys. Never logs the body values. |
| `TRANSPORT_MODE` | `stdio` | Reserved for a future HTTP mode; ignored at v1. Surfaced as gap **Q3**. |
| `ATLASSIAN_API_BASE_URL` | (unset) | Reserved for Data Center support (gap **Q4**). When set, overrides the default `https://${ATLASSIAN_SITE_NAME}.atlassian.net/wiki` base URL. |

### Validation behavior

At startup, the binary runs **Settings resolution** (see next
section) then validates. After resolution:

1. Reads `ATLASSIAN_SITE_NAME`. If unset or empty → exits with
   a clear error message on **stderr** (NOT stdout — see
   `09-anti-patterns/01-stdout-pollution.md`):

   ```
   FATAL: ATLASSIAN_SITE_NAME is not set. Set it to your site prefix
   (the part before ".atlassian.net"). Example:
     ATLASSIAN_SITE_NAME=your-company
   ```

2. Reads `ATLASSIAN_USER_EMAIL`. If unset → same FATAL exit
   pattern.

3. Reads `ATLASSIAN_API_TOKEN`. If unset → same FATAL exit
   pattern. The error message **never** echoes the token's
   value even if it were set, and the variable is read via
   `os.Getenv("ATLASSIAN_API_TOKEN")` only — never bound to a
   variable named `token` that could be logged.

4. Calls `confluence.New(nil, "INSTANCE_HOST")` with the site
   name resolved to `<site>.atlassian.net`, then calls
   `client.Auth.SetBasicAuth(email, token)`.

The startup validation runs **once**, in the entrypoint. No
per-request env reads.

### Settings resolution order (LOCKED 2026-07-09)

The binary resolves each setting in this priority order
(**first non-empty wins**), evaluated at process start:

| Priority | Source | Used when |
| -------- | ------ | --------- |
| 1 (highest) | **Process environment** (`os.Getenv`) | Always — every other source is only consulted if the var is unset or empty after this step |
| 2 | **`.env` file in the current working directory** | `make run` / CLI invocations where the user wants a project-local override without `export`-ing each shell session |
| 3 (lowest) | **`.env` file next to the binary** (`$0` resolves to the executable path; `.env` is `filepath.Dir(executable) + "/.env"`) | Containerized runs where the cwd is `/workspace` and the secret is shipped alongside the binary |

**Implementation notes (LOCKED):**

- **No external dependency.** Use only the Go stdlib. Parse
  the `.env` file line-by-line in `internal/config/dotenv.go`
  (~30 lines of Go). Do **not** add
  `github.com/joho/godotenv` — the user explicitly rejected
  adding dependencies for a feature this small.
- **`.env` format.** Standard `KEY=VALUE` per line. Comments
  start with `#`. Empty lines are skipped. Quoted values have
  their quotes stripped (`KEY="VALUE"` → `VALUE`). No variable
  expansion (the upstream `dotenv` library supports
  `${OTHER_VAR}` interpolation; we don't — keeps the parser
  trivial).
- **Token redaction.** If a `.env` parse error occurs, the
  error message includes the offending line's key name and
  line number, but **never the value**. A line like
  `ATLASSIAN_API_TOKEN=ATATT3x...` that fails to parse produces
  the error `"invalid .env line 7 (ATLASSIAN_API_TOKEN=<value redacted>)"`.
- **Missing `.env` is not an error.** The file may not exist;
  we silently fall through. Only parse errors are fatal.
- **Search order:** cwd `.env` first, then binary-dir `.env`.
  Process env (priority 1) always wins, so the `.env` files
  only provide defaults.
- **Hermes passthrough still works.** When the binary is
  launched by Hermes via `mcp_servers.confluence.env:`, the
  three `ATLASSIAN_*` vars are set in the subprocess env, and
  the `.env` files are never consulted. This preserves the
  Hermes-side wiring documented below.

**Example `.env` (for CLI / `make run` use):**

```bash
# .env — local dev settings for mcp-confluence
# (NEVER commit this file if it contains a real API token;
# .gitignore excludes it.)
ATLASSIAN_SITE_NAME=your-company
ATLASSIAN_USER_EMAIL=you@example.com
ATLASSIAN_API_TOKEN=ATATT3xFfGF0...
DEBUG=true
```

**Example invocation with `.env`:**

```bash
cd /path/to/confluence-mcp
make build             # produce ./bin/mcp-confluence
echo "ATLASSIAN_API_TOKEN=ATATT3xFfGF0..." > .env
echo "ATLASSIAN_SITE_NAME=your-company" >> .env
echo "ATLASSIAN_USER_EMAIL=you@example.com" >> .env
./bin/mcp-confluence   # picks up settings from .env automatically
```

### `.gitignore` discipline

The `.env` file must be in the project's `.gitignore`:

```
.env
.env.local
.env.*.local
```

The `.gitignore` ships at the project root alongside the
Makefile (see `06-implementation-skeleton/04-makefile.md`).

### Why no `~/.mcp/configs.json`?

The upstream's third-tier source (`~/.mcp/configs.json`, a
YAML/JSON file the user pre-populates) is **dropped** in the
Go port. Rationale:

- Hermes already handles env-var passthrough via
  `mcp_servers.<name>.env:`. The use case for `configs.json`
  (running the binary outside Hermes without `export`-ing
  vars) is now served by `.env` files.
- `configs.json` requires a YAML parser dependency. `.env` is
  parseable in 30 lines of stdlib Go.
- The user's locked decision (Q22) explicitly cited "inside
  the container or CLI" — both contexts where a local `.env`
  file is natural.

### Hermes-side wiring

Hermes passes the env vars to the subprocess via the `env:`
block in `mcp_servers.<name>.env:` (per the `native-mcp` skill):

```yaml
mcp_servers:
  confluence:
    command: "mcp-confluence"
    args: []
    env:
      ATLASSIAN_SITE_NAME: "your-company"
      ATLASSIAN_USER_EMAIL: "you@example.com"
      ATLASSIAN_API_TOKEN: "${ATLASSIAN_API_TOKEN}"
    timeout: 60
    connect_timeout: 30
```

Two important Hermes-side details:

1. **`${ATLASSIAN_API_TOKEN}` is the safe form.** Hermes
   resolves `${VAR}` at server-connect time from the **server
   process environment** (which includes `~/.hermes/.env`).
   The literal `ATATT3xFfGF0...` value never appears in
   `config.yaml`. Documented in
   `08-deployment-hermes/01-config-yaml.md`.
2. **Hermes filters env vars by default.** Only safe-baseline
   variables are inherited (`PATH`, `HOME`, `USER`, `LANG`,
   `LC_ALL`, `TERM`, `SHELL`, `TMPDIR`, `XDG_*`). The three
   `ATLASSIAN_*` vars **must** be explicit in the `env:`
   block; they will not leak from the host shell into the
   subprocess.

### Why not OAuth 2.0 (3LO)?

The upstream also does not implement 3LO — only basic-auth via
API token. 3LO is a non-trivial flow (authorize URL, callback
handler, token storage, refresh) that does not fit the
upstream's "five-tool CRUD wrapper" shape. Surfaced as gap
**Q2** for a v2 that adds `WithOAuth` /
`WithAutoRenewalToken` from `go-atlassian`'s option set.

## Verification

A reader of this spec should be able to:

1. Confirm the upstream README documents these three env vars.
2. Confirm the `mcp__confluence__*` tools fail to start when
   any of the three is missing.
3. Grep the Go source for `ATLASSIAN_API_TOKEN` and confirm
   the only usages are `os.Getenv` (or `.env` parser) for the
   resolution step and `client.Auth.SetBasicAuth` for the
   auth call — no `fmt.Println`, no `log.Print`, no
   `slog.Info`, no test fixture echoing the value.
4. **Settings resolution order.** Run `./bin/mcp-confluence`
   with no env vars and no `.env` and confirm it exits with
   the FATAL error for missing `ATLASSIAN_SITE_NAME`. Then
   create a `.env` with the three vars set and confirm the
   same `./bin/mcp-confluence` starts cleanly. Then `export
   ATLASSIAN_SITE_NAME=other-site` and confirm the binary
   prefers the env var (logs `site=other-site` in DEBUG
   mode).
5. **`.env` redaction.** Create a `.env` with a deliberately
   malformed value (e.g. `ATLASSIAN_API_TOKEN="unterminated`),
   run the binary, and confirm the parse error message does
   **not** include the token value (it should say
   `"invalid .env line N (ATLASSIAN_API_TOKEN=<value redacted>)"`).
6. **`.gitignore` discipline.** Confirm the project's
   `.gitignore` lists `.env`, `.env.local`, `.env.*.local`
   at the project root.
7. **Makefile as single source of truth.** Run `make help`
   from the project root and confirm the listed commands
   include `build`, `test`, `lint`, `image`, `run`, `clean`,
   `check` (per the project skill's required standard
   commands). Run `make build && make test && make lint` in
   sequence and confirm each succeeds.