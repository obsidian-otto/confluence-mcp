# 06 — Troubleshooting

> Goal: a fast diagnostic walkthrough when something doesn't
> work. Most failures fall into one of seven categories below;
> each has a 60-second test that finds the cause.

## Quick diagnostic flowchart

```
what are you trying to do?
├─ run `./bin/mcp-confluence --help`           → §1
├─ run `./bin/mcp-confluence stdio`             → §2
├─ run `./bin/mcp-confluence serve`             → §3
├─ register the binary with Hermes              → §4
├─ get a Confluence tool call to return data    → §5
├─ debug a slow or rate-limited tool call        → §6
└─ upgrade the binary or roll back to v0.1       → §7
```

## §1 — "running --help prints to stdout"

The load-bearing invariant is "help goes to stderr, not stdout".
If you see the help text on stdout, the cobra `SetOut` /
`SetErr` override has regressed. To diagnose:

```sh
./bin/mcp-confluence --help </dev/null | head -1     # must be empty
./bin/mcp-confluence --help 2>&1 | grep "USAGE:"    # must find a line
```

If the first returns a non-empty string, that's the
regression. Run `git log` on `cmd/mcp-confluence/main.go` and
confirm:

- `rootCmd.SetOut(io.Discard)` is called **before** `Execute()`
- `rootCmd.SetErr(os.Stderr)` is called **before** `Execute()`

The structural test `cli_test.go::TestRoot_Help_NoStdout`
enforces this; if it's been deleted, that's the regression.

## §2 — "stdio JSON-RPC returns nothing (or hangs)"

| Symptom | Likely cause | Test |
| ------- | ------------ | ---- |
| Process exits immediately with no stdout | missing env vars | see §5 |
| Process hangs forever | binary is waiting on stdout being closed; the parent never closes stdin | ensure your stdin pipe closes (e.g. `<<<"EOF"` rather than `printf … | tail` ) |
| Process returns JSON with `error.code = -32602` | `tools/call` arguments missing `params.arguments` wrapper | see [03-ai-agent-config.md §"The `jq` filter" etc.](03-ai-agent-config.md) |
| `error.code = -32601` | method name typo (`tools/lists` instead of `tools/list`) | — |

Quick smoke:

```sh
printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' \
  | ./bin/mcp-confluence stdio 2>err.log >out.json
echo "exit=$?"
echo "out:"; head -3 out.json
echo "err:"; cat err.log
```

`exit=0` + `out:` with a JSON-RPC response = working. `exit=1`
+ `err:` with `FATAL: ATLASSIAN_SITE_NAME is not set` =
missing env vars (see §5).

## §3 — "serve mode refuses to bind to my port"

```sh
./bin/mcp-confluence serve --listen=9000  # custom port
```

If you see `bind: address already in use`, find and kill the
other process:

```sh
ss -tlnp | grep 9000
# →    LISTEN 0  4096  *:9000  users:(("python3",pid=1234,...))
kill 1234
```

If `--listen=not-a-listener`:

```
FATAL: --listen="not-a-listener": parse error: ...
```

This is **fail-closed** behavior — locked design. The binary
does not silently fall back to `127.0.0.1:8080` or any other
default; missing or malformed `--listen` is a hard error.

If `--listen=0.0.0.0:8080`, the binary will bind successfully.
**The SECURITY warning in the help text still applies:** bind
to all interfaces is fine in a dev container behind a
trusted reverse proxy, but exposes your Atlassian API token
boundary to anyone who can reach that IP.

## §4 — "Hermes registers 0 tools (or doesn't see the binary)"

```sh
hermes mcp list                  # does confluence appear?
hermes mcp test confluence       # is the spawn clean?
hermes mcp restart confluence    # force a restart if not
```

`hermes mcp test confluence` invokes the binary directly and
prints both stdout and stderr. Look for:

- **Missing startup banner on stderr** → spawn failure.
  Almost always the binary path is wrong or permissions are
  wrong. Common mistakes:
  - `command: bin/mcp-confluence` (relative path; Hermes may
    spawn from a different cwd). Use an **absolute** path:
    `command: /home/<user>/bin/mcp-confluence`.
  - `command: ./bin/mcp-confluence` (also relative).
  - Missing `+x` bit on the binary (`chmod +x bin/mcp-confluence`).

- **Startup banner present + tools/list returns 0 tools** →
  registration mismatch. Run the binary's own `--version`
  and verify it matches the version Hermes sees (Hermes
  fingerprints the binary by content, not by name).

- **`FATAL: … is not set`** → env vars not reaching the binary.
  Even if `~/.hermes/.env` has them, they need to flow
  through Hermes into the spawned process. Re-check
  `~/.hermes/config.yaml` — the `env:` block must mirror the
  variable names from [04-configuration-reference.md §"Environment variables"](04-configuration-reference.md).

If `hermes mcp test confluence` is clean but
`hermes mcp list` shows nothing: Hermes requires restart. Run
`hermes restart` (or whatever this Hermes version names it).

## §5 — "FAILS — missing ATLASSIAN_* env"

```
$ ATLASSIAN_API_TOKEN=ATATT... ./bin/mcp-confluence stdio
2026/07/14 09:48:12 mcp-confluence v0.1.0 starting (site=smartergroup, email=…)
2026/07/14 09:48:12 Note: API token value not logged for security
FATAL: ATLASSIAN_USER_EMAIL is not set. Set it to your Atlassian account email.
```

The binary exits 1 on a missing required setting. Three ways
to fix:

1. Set all three env vars (preferred): `source .env`
2. Pass flags (dev only): `./bin/mcp-confluence stdio --site=... --email=... --api-token=...`
3. Use a config file (advanced):
   `./bin/mcp-confluence stdio --config=./confluence.yaml`

`make verify-env` (in the Makefile) prints all three settings,
redacting the token to its length. Use it as a sanity check
before invoking the binary:

```sh
make verify-env
# Required settings (resolution order: env > .env):
#   ATLASSIAN_SITE_NAME=smartergroup
#   ATLASSIAN_USER_EMAIL=you@example.com
#   ATLASSIAN_API_TOKEN=<set (length=36)>
#   DEBUG=<unset>
```

If `verify-env` shows all three are set but the binary still
fails to find them, check your shell's environment-propagation
behavior:

- `command: ./bin/mcp-confluence` — Hermes spawns from its
  cwd; `command:` should be **absolute**.
- The `${WORKSPACE_API_TOKEN}` form in `~/.hermes/config.yaml`
  is evaluated against `~/.hermes/.env`. If that file doesn't
  exist, the variable is undefined.

## §6 — tool call returns 429 / 503 / "rate limited"

The Confluence Cloud REST API is rate-limited per tier.
[See specs/shared/04-rate-limits.md](../specs/research/) for the
tier model and rate-limit headers. The `executeRequest` pipeline
**does not retry on 429 today** (deliberate choice — automated
retries on a multi-tool workflow can mask rate-limit-underrun bugs).

Solutions:

- Wait and retry: most rate limits reset within 60 seconds.
  The error envelope (`<METHOD> <path>: 429 Too Many Requests - <body>`)
  includes `Retry-After` if the upstream returned one.
- Switch to a higher tier (Confluence Cloud Enterprise plans
  have higher rate limits).
- Batch by tool: a single `conf_list_pages` with `limit=25` is
  cheaper than 25 individual calls.

## §7 — the binary needs to roll back

The v0.1 binary is intact in `bin/mcp-confluence` (or, after
re-clone, buildable from `git checkout pre-v4`). To roll back:

```sh
git checkout pre-v4   # if tagged — otherwise pick a SHA
make build
./bin/mcp-confluence    # the v0.1 stdio binary
```

Or, more surgical:

```sh
# Just kill the v4 daemon and run the v0.1 binary directly
pkill -f 'mcp-confluence'
git checkout <v0.1-sha>
make build
./bin/mcp-confluence    # v0.1 stdio-only
```

The v0.1 binary had no CLI; the `mcp_servers:` block used
`args: []`. After rollback, restore `args: []` in
`~/.hermes/config.yaml` (or omit `args:` entirely).

## §8 — The "Hermes never sees the binary" loop

A common operational trap: after Hermes restarts, the
`mcp_servers:` entries from the previous session are NOT
restored automatically — Hermes itself persists them (it
writes `~/.hermes/config.yaml` to disk), but if you rebuilt
the binary path or moved the user's `.hermes` directory,
Hermes may not find what you expect.

Quickest test:

```sh
which mcp-confluence || ls -la /path/to/mcp-confluence
hermes mcp list
hermes mcp test confluence
```

If `mcp test confluence` is clean but `mcp list` shows
nothing, Hermes has the binary in its cold cache. A
`hermes restart` (whichever form your version uses) flushes
the cache.

## §9 — `--debug` produces too much stderr

`--debug` logs every tool call to stderr. For a workflow
that makes hundreds of calls per minute, the stderr stream
becomes noisy. To suppress:

```sh
# Run the daemon with logs going to a file
./bin/mcp-confluence serve --listen=127.0.0.1:8080 2>debug.log
```

Then to grep just the failure-mode tools:

```sh
tail -f debug.log | grep -E 'status: [45]'
```

For Hermes-mode, the daemonized binary inherits stderr the
same way; pipe it through a logger if you want to keep it
out of the Hermes log viewer.

## §10 — "what if I want a third transport?"

The internal/server.go `NewWithTransport(deps, tr)` factory
takes any `transport.Transport`. Today two transports are
wired:

- `stdio.NewStdioServerTransportWithIO(stdin, stdout)`
- `internal/transport/http.NewServer(srv, listen, logger)`
  (returns a `*http.Server` that wraps `mcp.Server.Handle()`)

Adding a third transport (e.g. WebSocket, gRPC, Unix socket)
is a one-package PR with no changes to `internal/tools/`. See
[05-architecture-decisions.md §"Why a single `mcp.Server`
instance"](05-architecture-decisions.md) for the architectural
invariant.

To add a transport:

1. Create `internal/transport/<name>/<name>.go` with a
   constructor matching the
   `func NewServer(srv *mcp.Server, ...) *Server` shape.
2. Register the subcommand in `cmd/mcp-confluence/main.go`
   (`func new<X>Cmd() *cobra.Command`).
3. Add an integration test in `cmd/mcp-confluence/cli_test.go`
   using the same `--help` → MCP HOST REGISTRATION block
   discipline.
4. Update [04-configuration-reference.md](04-configuration-reference.md)
   with the new subcommand's flag.

The dev-velocity rationale (per
[05-architecture-decisions.md §"Why a CLI at all"](05-architecture-decisions.md))
favors short-lived process-spawn transports (stdio) for
host integration and HTTP for everything else. A third
transport is only justified if there's a clear MCP-host-side
constraint that stdio and HTTP can't satisfy.
