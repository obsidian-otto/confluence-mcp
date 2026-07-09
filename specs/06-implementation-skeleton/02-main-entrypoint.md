# 06.2 — `cmd/mcp-confluence/main.go` Skeleton

## Overview

The entrypoint is intentionally tiny: load config, build
atlassian client, build server, register tools, serve. All
non-trivial logic lives in `internal/`. This file documents
the entrypoint's exact structure.

## Sources

- `mcp-golang` README example:
  `examples/readme_server/main.go`.
- Go signal handling: `signal.NotifyContext` from the
  `os/signal` stdlib package.

## Spec

### Full `main.go` skeleton

```go
// cmd/mcp-confluence/main.go
package main

import (
    "context"
    "fmt"
    "log"
    "os"
    "os/signal"
    "syscall"

    "github.com/<owner>/mcp-confluence/internal/atlassian"
    "github.com/<owner>/mcp-confluence/internal/config"
    "github.com/<owner>/mcp-confluence/internal/server"
)

const version = "1.0.0"

func main() {
    if err := run(); err != nil {
        // run() handles its own logging; this is a fallback
        fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
        os.Exit(1)
    }
}

func run() error {
    // 1. Load config from env + .env file (fail-fast on missing)
    //    Settings resolution order (LOCKED 2026-07-09, see
    //    01-foundations/03-env-var-contract.md):
    //      1. Process env (highest priority)
    //      2. .env in cwd
    //      3. .env next to the binary
    //    The .env parser is in internal/config/dotenv.go.
    cfg, err := config.LoadFromEnv()
    if err != nil {
        return err
    }

    // 2. Debug log (stderr only)
    if cfg.Debug {
        log.Printf("mcp-confluence v%s starting (site=%s, email=%s)",
            version, cfg.SiteName, cfg.UserEmail)
        log.Printf("Note: API token value not logged for security")
    }

    // 3. Build Atlassian client
    client, err := atlassian.New(cfg)
    if err != nil {
        return fmt.Errorf("build atlassian client: %w", err)
    }

    // 4. Build MCP server
    srv, err := server.New(cfg, client, version)
    if err != nil {
        return fmt.Errorf("build mcp server: %w", err)
    }

    // 5. Wire SIGINT/SIGTERM for graceful shutdown
    ctx, cancel := signal.NotifyContext(context.Background(),
        syscall.SIGINT, syscall.SIGTERM)
    defer cancel()

    // 6. Serve (blocks until ctx is canceled or stdin closes)
    errCh := make(chan error, 1)
    go func() {
        errCh <- srv.Serve()
    }()

    select {
    case <-ctx.Done():
        if cfg.Debug {
            log.Printf("shutting down on signal")
        }
        return nil
    case err := <-errCh:
        return err
    }
}
```

### Key behaviors

1. **Fail-fast on missing env.** `config.LoadFromEnv()`
   returns an error if any of the three required env vars
   is missing or empty (after consulting process env + cwd
   `.env` + binary-dir `.env`). The error message is
   human-readable and lists which var is missing (see
   `01-foundations/03-env-var-contract.md` for the message
   format).

2. **No `fmt.Println` to stdout.** Every log line is
   `log.Printf` (which goes to stderr by default) or
   `fmt.Fprintf(os.Stderr, ...)`. `fmt.Println(...)` would
   corrupt the JSON-RPC stream.

3. **No token logging.** The debug log at step 2
   deliberately does **not** include the token. The
   `cfg.Debug` field is `bool`; the API token field is
   `string` and is **never** passed to a logger.

4. **Signal handling.** SIGINT/SIGTERM cancel the context;
   `srv.Serve()` returns when the transport closes (which
   happens when stdin EOFs). The stdio transport doesn't
   expose a Close() — Hermes closing the subprocess pipe is
   the close signal.

5. **`run()` separation.** The `run() error` pattern (vs.
   inline `main()` body) makes the entrypoint testable: a
   test can call `run()` with a custom `os.Environ()` and
   verify the error path.

### Build and version stamping

```bash
go build -ldflags "-X main.version=1.0.0" -o bin/mcp-confluence ./cmd/mcp-confluence
```

(Or equivalently: `make build` — see
`06-implementation-skeleton/04-makefile.md`.)

The `version` constant is **settable via `-ldflags`** so the
container image's `mcp-confluence --version` (gap Q16) can
report the build's commit SHA.

## Verification

A reader of this spec should be able to:

1. Build the binary with `make build` and confirm a single
   static binary is produced in `./bin/mcp-confluence`.
2. Run `./bin/mcp-confluence` with no env vars and no `.env`
   and see a FATAL error on stderr.
3. Run `DEBUG=true ./bin/mcp-confluence` with valid env vars
   and see the debug log on stderr (the binary will then
   block waiting on stdin — kill with Ctrl-C).
4. Confirm `ldd ./bin/mcp-confluence` shows it is
   **statically linked** (no libpthread, libc, etc.) —
   required for the Paketo distroless image.
5. **Main entrypoint wires both sources.** Confirm
   `cmd/mcp-confluence/main.go` calls `config.LoadFromEnv()`
   (which internally walks process-env → cwd `.env` →
   binary-dir `.env`) and that the `.env` parsing happens
   before any `confluence.New(...)` call (otherwise env
   values would be unused).