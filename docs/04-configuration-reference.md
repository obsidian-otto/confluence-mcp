# 04 — Configuration reference

> Goal: the full matrix of CLI flags, environment variables,
> config-file keys, and their interaction rules.

## Resolution order (locked Q22)

Settings resolve in priority order, highest first:

1. **CLI flags** (e.g. `--site=foo`)
2. **Process environment** (e.g. `ATLASSIAN_SITE_NAME=foo`)
3. **`.env` file in the current working directory** (`./.env`)
4. **`.env` file next to the binary** (`./bin/.env` or whatever resolves from `os.Executable()`)

When `--config /path/to/file.yaml` is supplied, that file's
keys are read by viper and participate at the same tier as
**tier 2.5** — viper's `BindPFlag` and `AutomaticEnv` consume it
on top of the stdlib `LoadFromEnv` call. Documented in detail in
[05-architecture-decisions.md §"The CLI composition path"](../../specs/14-cobra-viper-golang/02-design.md) (which is in the spec set).

## CLI flags — the load-bearing set

These are defined on the **root command** so they apply to
every subcommand (`stdio`, `serve`, `--help`, `--version`):

| Flag | Type | Overrides | Default | Notes |
| ---- | ---- | --------- | ------- | ----- |
| `--site <string>` | string | `ATLASSIAN_SITE_NAME` | empty (required) | Confluence site prefix (the part before `.atlassian.net`) |
| `--email <string>` | string | `ATLASSIAN_USER_EMAIL` | empty (required) | Atlassian account email |
| `--api-token <string>` | string | `ATLASSIAN_API_TOKEN` | empty (required) | Atlassian API token. **Never logged.** See below for the token-redaction contract. |
| `--debug` | bool | `DEBUG=true` | `false` | Toggle verbose stderr logging. When on, every tool call emits a `<TIMESTAMP> conf_<method> <path> <status> <bytes>` line. |
| `--config <path>` | string | (no env override) | empty | Path to a viper-compatible config file (YAML, JSON, TOML, INI, envfile). When set, viper reads it on top of env vars. |
| `-h`, `--help` | bool | n/a | `false` | Print help text to **stderr**; exit 0. |
| `-v`, `--version` | bool | n/a | `false` | Print `mcp-confluence version <ver>` to **stderr**; exit 0. |

### Subcommand-local flags

| Flag | Subcommand | Type | Default | Notes |
| ---- | ---------- | ---- | ------- | ----- |
| `--listen <host:port>` | `serve` | string | `127.0.0.1:8080` | Where to bind. Refuses to silently fall back if the port is busy. **Fails closed** on parse error. |

No other subcommand-local flags exist today. The `-h`/`--help`/`-v`/`--version` flags are inherited from the root.

## Environment variables

| Variable | Required | Default | Notes |
| -------- | -------- | ------- | ----- |
| `ATLASSIAN_SITE_NAME` | yes | empty | Confluence site prefix (matches the `--site` flag) |
| `ATLASSIAN_USER_EMAIL` | yes | empty | Atlassian account email (matches `--email`) |
| `ATLASSIAN_API_TOKEN` | yes | empty | Atlassian API token. **Never logged.** (matches `--api-token`) |
| `DEBUG` | no | empty / falsy | Set to `"true"` to enable verbose stderr logging (matches `--debug`) |
| `ATLASSIAN_LISTEN` | no | `127.0.0.1:8080` | `serve` mode binding. Set via env rather than `--listen` flag. |

#### The token-redaction contract (locked)

The `ATLASSIAN_API_TOKEN` value is **never**:

- logged (no `log.Printf`, no `fmt.Fprintf(os.Stderr, ...)`,
  no `os.Setenv`-eavesdropping)
- printed in `--help` text
- echoed back in error messages
- included in any HTTP response body
- written to `/tmp/mcp/<id>.json` truncation dumps (those
  hold the upstream response body, not the request; for token
  scrubbing, the API call authenticates via Basic Auth header,
  not as a query/body parameter)

`make verify-env` prints the token **length** but not its
value. The startup banner prints `Note: API token value not
logged for security`.

The `verify-env` Makefile target doesn't read token contents
either — it counts bytes. The only place the literal token is
ever materialised is the `Authorization: Basic <base64>` header
on outbound requests, and that's outbound-bound over HTTPS.

## `.env` file format

`.env.example` is the canonical template; copy it to `.env` and
edit in place:

```sh
cp .env.example .env
```

The supported format is the standard `.env` shape:

```sh
# Comments start with #
ATLASSIAN_SITE_NAME=smartergroup
ATLASSIAN_USER_EMAIL=you@example.com
ATLASSIAN_API_TOKEN=ATATT3xFfGF0...
DEBUG=false
```

Rules (per locked Q22):

- One key=value pair per line.
- Empty lines and `#`-prefixed comments are ignored.
- Values may be wrapped in matching single or double quotes;
  quotes are stripped.
- No variable expansion (`$VAR` is treated as literal `$VAR`).
- Malformed lines produce an error that redacts the value as
  `<value redacted>`.
- The binary looks for `.env` in **two places**, in priority
  order: cwd first, then next to the binary itself.

A missing `.env` is not an error (env vars may already cover
everything). A present-but-malformed `.env` **is** an error and
the binary exits with code 1.

## viper config file (`--config <path>`)

The `--config` flag points to a viper-compatible file in any
of viper's supported formats (extension-sniffed at runtime):

| Format | Extension | Example |
| ------ | --------- | ------- |
| YAML | `.yaml` / `.yml` | see below |
| JSON | `.json` | — |
| TOML | `.toml` | — |
| INI | `.ini` | — |
| envfile | `.env` (treated as viper config) | — |
| Java properties | `.properties` | — |

A YAML example (`/etc/mcp-confluence.yaml`):

```yaml
ATLASSIAN_SITE_NAME: smartergroup
ATLASSIAN_USER_EMAIL: you@example.com
ATLASSIAN_API_TOKEN: ATATT3xFfGF0...
DEBUG: false
listen: "127.0.0.1:8080"
```

Notice:

- Top-level keys match the env-var names **without** the
  `ATLASSIAN_` prefix. (viper's `SetEnvPrefix` strips it.)
- `listen` is the viper key for `--listen`; viper calls
  `BindPFlag` only after parsing the YAML, so the YAML
  value participates in the standard precedence ladder:
  flag > config-file > env > default.
- Values may NOT include the literal token in CI configs.
  Use `${ATLASSIAN_API_TOKEN}` substitution via your shell
  pre-processor, or pass the secrets via a secrets manager.

The binary reads viper's file with `viper.ReadInConfig()`,
which silently ignores a missing file. A present-but-broken
YAML is fatal (yaml parse error → exit 1).

## The `jq` filter (JMESPath, not `jq`-the-CLI)

Every `tools/call` accepts a `params.arguments.jq` argument.
This is a JMESPath expression evaluated against the **decoded**
Confluence response **before** encoding. Useful for trimming
large responses:

```
mcp__confluence__conf_list_pages(
  space-id="780763211",
  limit=50,
  jq="{results: results[*].{id: id, title: title, status: status}}"
)
```

What's emitted on the LLM side is a small envelope with just
those three fields, not the full per-page record with
timestamps, version history, etc.

### JMESPath cheatsheet (the parts that come up in this codebase)

```
# Root-level access
.foo
.foo.bar.baz

# Wildcard
results[*].id

# Filter on a condition
results[?status=='current'].id

# Pipe (current-element → next expression)
results[*].{id: id, title: title}

# Length
length(results)

# Sort + slice
sort_by(results, &createdAt)[*].{id: id, createdAt: createdAt}
```

For the full grammar see
[the JMESPath spec](https://jmespath.org/specification.html).
Test expressions in the [JMESPath playground](https://jmespath.org/),
then bake into a tool call.

## Token storage and propagation in automation scripts

For automation scripts (CI, Make recipes, shell loops), the
recommended pattern is:

```sh
# Read the token from ~/.mcp/.env (out-of-band, NOT in the
# repo's .env).
set -a
source ~/.mcp/.env        # exports ATLASSIAN_API_TOKEN
set +a

./bin/mcp-confluence stdio <<EOF
{"jsonrpc":"2.0","id":1,"method":"tools/list"}
EOF
```

Or via a Kubernetes secret:

```yaml
# k8s/deployment.yaml
spec:
  containers:
  - name: mcp-confluence
    envFrom:
    - secretRef:
        name: atlassian-api-token
```

In both cases the literal token never lands in the project repo,
the agent's MCP-host config, or the script's command-line args.

## What `.env.example` looks like in v4

```sh
# .env.example — copy to .env (in cwd or next to the binary) and fill in.

# Required: Atlassian Cloud credentials.
# Generate the API token at https://id.atlassian.com/manage-profile/security/api-tokens
ATLASSIAN_SITE_NAME=your-company
ATLASSIAN_USER_EMAIL=you@example.com
ATLASSIAN_API_TOKEN=ATATT3xFfGF0...

# Optional: verbose stderr logging for `--debug` or `DEBUG=true`
DEBUG=false

# Optional: `serve` mode bind address (matches --listen flag).
# Default 127.0.0.1:8080. Use 0.0.0.0:8080 ONLY behind a trusted reverse proxy.
# ATLASSIAN_LISTEN=127.0.0.1:8080

# Optional: viper-compatible config file (YAML/JSON/TOML/INI/etc).
# CLI flag form: --config=/path/to/confluence.yaml
# ATLASSIAN_CONFIG=/path/to/confluence.yaml

# NEVER commit .env itself. It's in .gitignore. The token never logs.
```

## Conflict-resolution summary

If multiple sources set the same key, here's the resolved
order (highest priority wins):

1. CLI flag (`--site=foo`)
2. Process env (`ATLASSIAN_SITE_NAME=foo`)
3. viper config file (loaded last in the read sequence)
4. cwd `.env`
5. binary-dir `.env`
6. Hardcoded default (empty / false / `127.0.0.1:8080`)

The order is **per-key** — `ATLASSIAN_SITE_NAME` from
`--site` beats `ATLASSIAN_SITE_NAME` from cwd `.env`, but
that doesn't affect `DEBUG` from a different source.
