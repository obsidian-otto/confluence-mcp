# 09.1 — Stdout Pollution (The JSON-RPC Invariant)

## Overview

The MCP stdio transport expects **every byte on stdout to be
a valid JSON-RPC message** — one JSON object per line,
newline terminated. **Nothing else may be written to stdout.**
Any stray byte — a `fmt.Println("got request")`, a panic
stack trace, a debug log line — breaks the parser on the
client side (Hermes) and the connection drops. This is the
single most common bug in stdio MCP servers and the one that
takes the longest to debug because the symptom (silent
disconnect) is not obviously a stdout-pollution bug.

This file documents the invariant, the patterns that satisfy
it, the patterns that violate it, and a one-line grep check
the implementer can run to verify the rule is upheld.

## Sources

- MCP Inspector "Why does my stdio MCP server disconnect"
  troubleshooting guide:
  https://www.augmentcode.com/mcp/mcp-inspector (the source
  quote: "Your stdio server disconnects because any non-JSON
  output to stdout breaks the JSON-RPC parser. Remove all
  print statements, console logs, and debug output that write
  to stdout. Redirect diagnostic logging to stderr instead,
  or write to a separate log file.").
- MCP spec:
  https://modelcontextprotocol.io/specification/2025-06-18/architecture/

## Spec

### The invariant (verbatim)

> Every byte written to `os.Stdout` must be a valid JSON-RPC
> message. One JSON object per line, newline terminated.

### What MAY be written to stdout

| Content | Allowed? |
| ------- | -------- |
| A JSON-RPC response from `server.Serve()` | **YES** — the only allowed source |
| A `fmt.Println("got request")` for debugging | **NO** |
| A `log.Print("...")` | **DEPENDS** — stdlib `log` defaults to stderr, but check `log.SetOutput(...)` |
| A `slog.Info("...")` | **DEPENDS** — stdlib `slog` defaults to stderr if configured correctly |
| A `panic(...)` from a handler | **NO** — panics write to stderr by default (not stdout); safe |
| A `os.Stdout.Write([]byte("..."))` | **NO** — direct write |
| A `fmt.Fprintf(os.Stdout, "...")` | **NO** — direct write |
| A `fmt.Fprintln(os.Stdout, "...")` | **NO** — direct write |

### What MUST be written to stderr

| Content | Where |
| ------- | ----- |
| Debug logs (when `DEBUG=true`) | stderr |
| Startup info (version, config loaded) | stderr |
| Fatal errors (missing env vars, build failures) | stderr |
| Panic stack traces | stderr (default Go behavior) |
| Request/response logging | stderr |

### The grep check

```bash
# Should return ZERO matches (or only matches with os.Stderr as the writer)
grep -rn 'fmt\.Print\|fmt\.Fprint\|os\.Stdout\|log\.SetOutput(os\.Stdout)' \
  --include='*.go' \
  ./cmd ./internal
```

Acceptable patterns:

```go
fmt.Fprintln(os.Stderr, "...")
log.Printf("...")               // default is stderr
slog.Info("...")                // when configured with stderr handler
```

### Patterns to use

#### Debug logging via stdlib `log`

```go
// log.Print / log.Printf go to stderr by default (safe).
log.Printf("got request: %s %s", method, path)
```

#### Structured logging via `log/slog` to stderr

```go
import "log/slog"

logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
slog.SetDefault(logger)

slog.Info("server starting", "version", version, "site", siteName)
```

#### Errors that need to go to stderr (not stdout, not a log file)

```go
fmt.Fprintf(os.Stderr, "FATAL: ATLASSIAN_SITE_NAME is not set\n")
os.Exit(1)
```

#### Panic recovery (writes to stderr automatically)

```go
defer func() {
    if r := recover(); r != nil {
        // debug.Stack() writes to stderr
        log.Printf("PANIC: %v\n%s", r, debug.Stack())
    }
}()
```

### Patterns to AVOID

#### ❌ Direct stdout write

```go
// BREAKS THE INVARIANT — Hermes sees "got request\n" as JSON, fails to parse, disconnects.
fmt.Println("got request")

// ALSO BAD — same issue.
fmt.Fprintln(os.Stdout, "got request")
```

#### ❌ Log package reconfigured to stdout

```go
// BREAKS THE INVARIANT — every log.Print call now writes to stdout.
log.SetOutput(os.Stdout)
log.Print("got request")
```

#### ❌ Tempting debug helper

```go
// BREAKS THE INVARIANT — even with a "DEBUG" guard, the writes go to stdout.
func debugPrint(args ...any) {
    if os.Getenv("DEBUG") == "true" {
        fmt.Println(args...)  // stdout!
    }
}
```

### How to verify

```bash
# 1. Build the binary
make build    # produces ./bin/mcp-confluence

# 2. Set fake env vars and capture stdout+stderr
DEBUG=true \
  ATLASSIAN_SITE_NAME=fake \
  ATLASSIAN_USER_EMAIL=fake@example.com \
  ATLASSIAN_API_TOKEN=fake \
  ./bin/mcp-confluence \
  > /tmp/stdout.log 2> /tmp/stderr.log &
PID=$!

# 3. Send a list_tools request, capture stdout
echo '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' | nc -U /proc/$PID/fd/0 2>/dev/null || true
sleep 0.5
kill $PID

# 4. Verify stdout is empty (or contains exactly one JSON line per request)
wc -l /tmp/stdout.log
# Expected: 1 (one JSON-RPC response)

# 5. Verify stderr has the debug logs
wc -l /tmp/stderr.log
# Expected: >= 1 (debug output)
```

If `wc -l /tmp/stdout.log > 1` when only one request was sent,
there's a stdout-pollution bug. Find and fix it.

### What MCP Inspector does

The MCP Inspector (`npx @modelcontextprotocol/inspector`) parses
stdout strictly. When the server violates the invariant, the
Inspector shows:

```
[ERROR] Failed to parse message: Unexpected token...
```

… and the connection drops. This is the same behavior as
Hermes. **Test against the Inspector early** — it surfaces
the bug in seconds vs minutes of Hermes-side debugging.

## Verification

A reader of this spec should be able to:

1. Run the grep check on the Go source and confirm no
   stdout-polluting patterns.
2. Run the binary under `DEBUG=true` and confirm stderr has
   the debug output but stdout has only JSON-RPC messages.
3. Connect via MCP Inspector and confirm `tools/list`
   returns five tools cleanly.