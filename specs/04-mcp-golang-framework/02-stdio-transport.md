# 04.2 — Stdio Transport

## Overview

The Go MCP server uses the **stdio transport** exclusively at
v1. This is the upstream's default transport and the one
Hermes Agent launches as a subprocess via `command:` + `args:`
in `mcp_servers:`. This file documents the stdio transport's
lifecycle, the JSON-RPC framing invariant, and the patterns
the Go MCP server uses.

## Sources

- Source: `transport/stdio/stdio_server.go` (173 lines).
- Source: `transport/stdio/internal/stdio/` (the read buffer).
- MCP spec:
  https://modelcontextprotocol.io/specification/2025-06-18/architecture/
- MCP Inspector docs:
  https://modelcontextprotocol.io/docs/tools/inspector

## Spec

### Lifecycle

```go
transport := stdio.NewStdioServerTransport()  // os.Stdin / os.Stdout
server := mcp_golang.NewServer(transport)
server.RegisterTool(...)                      // 5 times
server.Serve()                                // blocks
```

`NewStdioServerTransport()` reads from `os.Stdin` and writes
to `os.Stdout`. `NewStdioServerTransportWithIO(in, out)`
accepts custom readers/writers for testing.

The transport starts a goroutine that reads line-delimited
JSON from stdin and dispatches each line as an MCP JSON-RPC
message.

### The JSON-RPC framing invariant

**Every byte written to `os.Stdout` must be a valid JSON-RPC
message. Nothing else.** This is the load-bearing rule. The
MCP client (Hermes) reads stdout line-by-line and parses each
line as JSON. Anything else on stdout — a debug log, a panic
stack trace, a `fmt.Println("got request")` — breaks the
parser and the connection drops.

The upstream explicitly mentions this in its README (line 14
of the upstream `index.ts`): "The MCP server module loaded"
goes to a contextual logger that writes to stderr
(`indexLogger.debug(...)` in the upstream defaults to stderr,
not stdout).

**The Go MCP server's rules:**

| Channel | Used for |
| ------- | -------- |
| **stdout** | MCP JSON-RPC messages **only**. No log lines, no `fmt.Println`, no debug output. |
| **stderr** | Everything else: debug logs (when `DEBUG=true`), startup errors, fatal errors. The Hermes `hermes mcp test` command captures stderr and displays it for debugging. |
| **log files** | Not used at v1 (gap Q10). |

### Read loop

The transport's read loop (from `stdio_server.go`):

```go
func (t *StdioServerTransport) readLoop(ctx context.Context) {
    for {
        // Read a line from stdin (delimited by '\n')
        line, err := t.readBuf.ReadLine()
        if err == io.EOF {
            return  // parent closed stdin
        }
        // ... parse as JSON-RPC, dispatch to handler
    }
}
```

The library handles line framing, JSON parsing, and message
routing. The Go MCP server only needs to provide handlers
(`RegisterTool` callbacks).

### Send path

For outgoing messages:

```go
func (t *StdioServerTransport) Send(ctx context.Context, message *transport.BaseJsonRpcMessage) error {
    // Marshal message to JSON
    data, err := json.Marshal(message)
    if err != nil { return err }
    // Write data + newline to stdout
    _, err = t.writer.Write(append(data, '\n'))
    return err
}
```

The newline-delimited framing is **required**. Each message
ends with `\n`. The transport handles this automatically.

### Why stderr, not a log file?

The upstream writes debug logs to `~/.mcp/data/*.log`. The
Go port writes to stderr because:

1. **No filesystem state.** A log file requires creating
   `~/.mcp/data/`, handling permissions, and rotating. Stderr
   has none of these concerns.
2. **Hermes already captures stderr.** When `hermes mcp test`
   runs, stderr is shown to the user. When `hermes chat`
   runs, stderr is suppressed but still available via
   `hermes mcp logs <name>` (if implemented; not at v1).
3. **`2>`-redirectable.** The user can `2>debug.log` from
   any shell if they want to capture debug output.

If a future v1.1 adds a log file (gap Q10), it must use a
path under `~/.mcp/data/` (matching upstream convention) or
`~/.local/state/mcp-confluence/` (XDG-state convention).

### Anti-pattern: any `fmt.Println` or `log.Print` to stdout

A common bug: a developer adds `fmt.Println("got request")`
during debugging and forgets to remove it. The MCP client
parses it as JSON, fails, and the server appears to hang.

**Rule:** `grep -rn "fmt.Print\|log.Print\|slog.Info" --include='*.go'
internal/ cmd/` and confirm **every** match is followed by
`"stderr"` or uses a logger that defaults to stderr.

The stdlib `log` package writes to **stderr by default**, so
`log.Print(...)` is safe. `fmt.Print*` writes to **stdout** —
must be avoided or replaced with `fmt.Fprintln(os.Stderr, ...)`.

The structured logger `log/slog` defaults to **stderr** when
configured with `slog.NewTextHandler(os.Stderr, nil)`.

### Anti-pattern: panic in a handler

If a tool handler panics, the panic propagates up `Serve()`,
which crashes the process. Hermes sees the process die, and
the tool call returns an error to the LLM. This is
acceptable behavior — Hermes retries — but it loses
diagnostic info.

**Rule:** every tool handler must `defer recover()` and
convert panics to error responses. Pattern:

```go
func safeHandler[T any](inner func(T) (*mcp_golang.ToolResponse, error)) func(T) (*mcp_golang.ToolResponse, error) {
    return func(args T) (resp *mcp_golang.ToolResponse, err error) {
        defer func() {
            if r := recover(); r != nil {
                log.Printf("PANIC in handler: %v", r)
                resp = &mcp_golang.ToolResponse{
                    Content: []*mcp_golang.Content{{
                        Type: "text",
                        Text: fmt.Sprintf("internal server error: %v", r),
                    }},
                }
                err = nil
            }
        }()
        return inner(args)
    }
}
```

Every `RegisterTool` call wraps its handler in `safeHandler`.

## Verification

A reader of this spec should be able to:

1. Confirm the upstream's `src/index.ts` uses
   `Logger.forContext(...)` (which writes to stderr by
   default) for debug logging — no stdout writes.
2. Write a Go test that captures `os.Stdout` to a buffer,
   calls `server.Serve()` with a one-shot mock stdin, and
   confirms the buffer contains exactly one newline-
   terminated JSON message per request.
3. Run the binary with `DEBUG=true` and confirm that all log
   output appears on stderr (`./mcp-confluence 2>debug.log`
   should produce a populated log file, but `1>debug.log`
   should produce an empty log file because stdout has no
   logs).