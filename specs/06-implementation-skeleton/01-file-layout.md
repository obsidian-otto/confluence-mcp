# 06.1 — File Layout (`internal/` Package Boundaries)

## Overview

This file pins the Go package layout. The layout mirrors the
upstream's 3-layer split (`tools` → `controllers` → `services`
→ `utils`) but collapses `controllers` into the `tools`
package (Go convention favors fewer packages for small
projects) and splits the upstream's `utils/` into per-concern
packages (`config`, `jmespath`, `toon`).

## Sources

- Upstream layout: `src/` directory tree (documented in
  `02-upstream-aashari/01-architecture.md`).
- Go standard project layout:
  https://github.com/golang-standards/project-layout (the
  `cmd/` + `internal/` convention).
- `mcp-golang` examples:
  https://github.com/metoro-io/mcp-golang/tree/main/examples
  (the `readme_server/` example uses a single `main.go` with
  no package split).

## Spec

### Directory tree

```
confluence-mcp/                              (Go module root)
├── Makefile                                  (single source of truth — see 06-implementation-skeleton/04-makefile.md)
├── project.toml                              (Paketo build descriptor — see 07-paketo-buildpack/)
├── .gitignore                                (excludes .env, .env.local, /bin/, /sbom/)
├── .env.example                              (template — copy to .env for local dev; .env itself is gitignored)
├── go.mod                                    module github.com/<owner>/mcp-confluence
├── go.sum
├── README.md
├── LICENSE                                   (MIT, matching upstream)
├── cmd/
│   └── mcp-confluence/
│       └── main.go                           (entrypoint: load config, build server, serve)
└── internal/
    ├── config/
    │   ├── config.go                         LoadFromEnv() + validation
    │   ├── dotenv.go                         Stdlib .env parser (~30 LOC; LOCKED 2026-07-09)
    │   └── config_test.go
    ├── atlassian/
    │   ├── client.go                         Wrapper around go-atlassian confluence.Client
    │   ├── auth.go                           SetBasicAuth + token handling
    │   ├── errors.go                         Typed errors: AuthMissingError, APIError
    │   └── client_test.go
    ├── jmespath/
    │   ├── jmespath.go                       Apply(expr, data) wrapper
    │   └── jmespath_test.go
    ├── toon/
    │   ├── encode.go                         Encode(any) -> string
    │   ├── encode_test.go                    Round-trip + edge cases
    │   └── format.go                         Helpers for indentation, quoting
    ├── tools/
    │   ├── descriptions.go                   Five CONF_*_DESCRIPTION constants (verbatim from upstream)
    │   ├── args.go                           Five ConfXxxArgs types
    │   ├── execute.go                        executeRequest() shared helper
    │   ├── handlers.go                       handleGet/Post/Put/Patch/Delete
    │   ├── register.go                       RegisterAll(server) — wires 5 tools
    │   ├── safe_handler.go                   safeHandler() panic-recovery wrapper
    │   └── tools_test.go
    └── server/
        ├── server.go                         NewServer(config, client) -> *mcp_golang.Server
        └── server_test.go
```

### Per-package responsibility

| Package | Responsibility | Imports |
| ------- | -------------- | ------- |
| `cmd/mcp-confluence` | Entrypoint: parse env, build config, build server, run | `internal/config`, `internal/server`, `mcp-golang` |
| `internal/config` | Load + validate env vars (with `.env` fallback per Q22 lock) | stdlib only |
| `internal/atlassian` | HTTP client wrapper + auth + errors | `ctreminiom/go-atlassian/v2/confluence` |
| `internal/jmespath` | JMESPath wrapper | `jmespath/go-jmespath` |
| `internal/toon` | TOON encoder | stdlib only |
| `internal/tools` | Tool handlers + arg types + descriptions + registration | `internal/atlassian`, `internal/jmespath`, `internal/toon`, `mcp-golang` |
| `internal/server` | Server bootstrap (config + client + register) | `internal/config`, `internal/atlassian`, `internal/tools`, `mcp-golang` |

### Why `internal/`?

`internal/` is a Go convention: packages under `internal/`
can only be imported by packages at or below the parent of
`internal/`. This prevents accidental external imports of
the server's implementation. Hermes and the `hermes mcp
add` flow import only the **binary** (`cmd/mcp-confluence`),
never `internal/`.

### Why no `pkg/` directory?

The Go standard layout recommends `pkg/` for libraries
intended for external consumption. This project is a binary,
not a library — the only consumer is `cmd/mcp-confluence/`.
`internal/` is the right choice.

### What we do NOT have

- **No `vendor/`** — Go modules handle vendoring via
  `go mod vendor` if needed; default is the module cache.
- **No `third_party/`** — upstream has no Go code we copy.
- **No `Makefile`-at-v1.1** — the Makefile is **now part of
  v1** (locked Q14). The Makefile is the single source of
  truth for all commands; see
  `06-implementation-skeleton/04-makefile.md`.
- **No `.github/workflows/`** — CI is gap **Q15** for v1.1.

## Verification

A reader of this spec should be able to:

1. Run `go build ./cmd/mcp-confluence` from the module root
   and produce a `mcp-confluence` binary.
2. Run `go test ./internal/...` and see all package tests
   pass.
3. Run `go vet ./...` and see no warnings.
4. Confirm no file under `internal/` is imported by any
   package outside `internal/` or `cmd/mcp-confluence/`
   (the Go compiler enforces this).
5. Confirm the `Makefile` at the project root lists all
   the standard commands per the project skill.