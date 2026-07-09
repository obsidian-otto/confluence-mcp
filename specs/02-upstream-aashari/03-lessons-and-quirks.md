# 02.3 — Lessons and Quirks (Inherited from the Upstream)

## Overview

The upstream `@aashari/mcp-server-atlassian-confluence` v3.3.0
has been in production since 2024 and has accumulated a set of
design choices that the Go port **inherits** rather than
redesigns. This file enumerates the load-bearing quirks — TOON
encoding, 40k truncation, debug log file path, env-var
precedence — and notes any deviation the Go port makes.

## Sources

- Upstream source: `src/utils/formatter.util.ts` (TOON encoder
  + truncation logic).
- Upstream source: `src/utils/config.util.ts` (env + .env +
  `~/.mcp/configs.json` precedence).
- Upstream README (lines 339-384): "Large Response
  Truncation" and "Debug Logging" sections.

## Spec

### TOON output (token savings)

The upstream defaults to **TOON** (Token-Oriented Object
Notation) which is "30-60% fewer tokens than JSON" per the
upstream README. The format is custom and lives in
`src/utils/formatter.util.ts`. The Go port implements a
**TOON-compatible encoder** in `internal/toon/` with the same
default behavior:

```yaml
# TOON example
results:
  - id: 123
    title: My Page
  - id: 456
    title: Other Page
```

vs the JSON equivalent:

```json
{"results": [{"id": "123", "title": "My Page"}, {"id": "456", "title": "Other Page"}]}
```

**Go port deviation:** the Go encoder targets the same byte
output for common cases. Edge cases (deeply nested objects,
unicode escapes, very long strings) may differ slightly —
the spec is informal. See
`05-tool-surface-design/02-jmespath-and-toon.md` for the
encoder-decision rationale.

### 40k truncation

When a response is >40,000 chars (≈10k tokens), the upstream:

1. Truncates the response to fit.
2. Appends a notice:

   ```
   [truncated 40,123 / 80,456 chars — full response at /tmp/mcp/<session-id>.json]
   ```

3. Writes the full response to `/tmp/mcp/<session-id>.json`.

The Go port implements the same threshold (40,000) and the
same notice shape. The `/tmp/mcp/` directory is created if
missing. The session-id in the upstream is a UUID; the Go port
uses the PID + boot-time nanosecond timestamp as the "session
id" (no need for UUID — there is one binary instance at a
time per Hermes subprocess).

### Env-var precedence

The upstream's `config.util.ts` reads credentials from three
sources in this order (first non-empty wins):

1. **Process environment** (`process.env.ATLASSIAN_API_TOKEN`).
2. **`.env` file in cwd** (loaded via `dotenv`).
3. **`~/.mcp/configs.json`** (a YAML/JSON file the user can
   pre-populate).

The Go port **drops tier 3 (`configs.json`) and uses the same
two tiers** — process env + `.env` file. The behavior is
functionally identical for the Hermes use case
(env-passthrough) and the local-dev use case (`.env` file).
The user's locked decision (Q22) explicitly cited "inside
the container or CLI", both contexts where a local `.env`
file is natural.

If a user runs the Go binary outside Hermes (e.g. `make run`
or as a CLI), they create a `.env` file at the project root
(see `01-foundations/03-env-var-contract.md` for the exact
format and `.gitignore` discipline).

### Debug log path

Upstream debug logs go to:
```
~/.mcp/data/@aashari-mcp-server-atlassian-confluence.<session-id>.log
```

The Go port writes debug logs to **stderr** as JSON-lines. No
log file. The decision:

- Stderr is always available, never collides with the
  JSON-RPC stdout stream, and Hermes captures it for
  `hermes mcp test` output.
- A log file requires creating `~/.mcp/data/`, handling
  permissions, handling disk-full, and rotating — out of
  scope for v1.
- A future v1.1 could add `--log-file PATH` if the user asks
  (gap **Q10**).

### The "ALWAYS use `jq`" guidance

The upstream's tool descriptions repeatedly stress:

> ALWAYS use `jq` param to filter response fields.
> Unfiltered responses are very expensive!

The Go port preserves this exact wording in each tool's
description. The descriptions are part of the tool's
`inputSchema.description` field — visible to the LLM at
`tools/list` time.

### The schema-discovery pattern

The upstream's tool description includes:

> **Schema Discovery Pattern:**
> 1. First call: `path: "/wiki/api/v2/spaces", queryParams:
>    {"limit": "1"}` (no jq) - explore available fields
> 2. Then use: `jq: "results[*].{id: id, key: key, name:
>    name}"` - extract only what you need

This pattern is preserved verbatim. The "explore with
`limit:1`, then narrow with `jq`" workflow is documented in
the upstream's `conf_get` description and copied into the Go
port's `conf_get` description.

### Error messages — explicit HTTP status

The upstream's controller error handler prepends the HTTP
status to error messages:

```
GET /wiki/api/v2/spaces/999: 404 Not Found - {"code":"NOT_FOUND","message":"..."}
```

The Go port does the same — see
`09-anti-patterns/03-error-shapes.md` for the exact error
format.

### Default response when no `jq` and no `outputFormat`

| `jq` | `outputFormat` | Behavior |
| ---- | -------------- | -------- |
| unset | unset | TOON of the full JSON response |
| unset | `"toon"` | same |
| unset | `"json"` | raw JSON passthrough |
| set | unset | TOON of the JMESPath-filtered result |
| set | `"toon"` | same |
| set | `"json"` | JSON of the JMESPath-filtered result |

### What we did NOT inherit

| Upstream behavior | Go port decision | Reason |
| ----------------- | ---------------- | ------ |
| `~/.mcp/configs.json` loader | **Dropped**; `.env` files (cwd + binary-dir) replace it | User locked Q22 to `.env`-based resolution; no YAML dependency |
| `~/.mcp/data/*.log` debug file | Skipped | stderr-only (simpler) |
| Express HTTP transport | Skipped | v1 is stdio-only (gap Q3) |
| CLI mode (`get`, `post`, `put`, `patch`, `delete` subcommands) | Skipped | out of scope (gap Q9) |
| Express graceful shutdown | Re-implemented | SIGINT/SIGTERM handler in Go |
| `dotenv` `.env` loader | Re-implemented in 30 lines of stdlib Go (`internal/config/dotenv.go`) | No external dep |
| `truncateForAI` session-id UUID | Skipped | use PID + boot time |

## Verification

A reader of this spec should be able to:

1. Confirm the upstream's `src/utils/formatter.util.ts`
   exports a TOON encoder.
2. Confirm the upstream README documents the 40k truncation
   threshold.
3. Confirm the upstream README documents the three-tier
   env-var precedence.
4. Run the upstream against a test instance and trigger a
   >40k-char response (e.g. listing all pages in a large
   space without `jq`) to see the truncation notice.