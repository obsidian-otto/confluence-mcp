// cmd/mcp-confluence/main.go
//
// Entrypoint for the mcp-confluence MCP server.
//
// The lifecycle is:
//
//	load config (process env > cwd .env > binary-dir .env)
//	  -> build atlassian HTTP client
//	  -> wire stdin through an io.Pipe so EOF can cancel the context
//	  -> build mcp.Server with a pipe-backed stdio transport
//	  -> serve (blocks until ctx is cancelled — by signal OR stdin EOF)
//
// All logging goes to stderr — stdout is reserved for the JSON-RPC
// stream that the stdio MCP transport consumes (see
// specs/09-anti-patterns/01-stdout-pollution.md). Every log call is
// either log.Printf (defaults to stderr) or fmt.Fprintf(os.Stderr, ...).
//
// Secret handling: the API token is read once by config.LoadFromEnv
// and held in cfg.APIKey. It is NEVER passed to a logger or
// formatter; the debug log line explicitly includes only the
// non-secret site name and email, plus a "value not logged" note.
//
// The binary is built with CGO_ENABLED=0 (set in the Makefile) so it
// is fully statically linked — a prerequisite for the Paketo
// distroless run image (see specs/07-paketo-buildpack/01-project-toml.md).
//
// Architecture note: run() and runLifecycle() are intentionally
// split. run() owns the signal wiring (SIGINT/SIGTERM cancel the
// context); runLifecycle(ctx) is the testable core that loads
// config, builds deps, serves, and returns ctx.Err() on shutdown.
// Tests call runLifecycle directly so they can cancel an injected
// context without sending real OS signals.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

	mcp "github.com/metoro-io/mcp-golang"
	"github.com/metoro-io/mcp-golang/transport/stdio"

	"github.com/bennie/mcp-confluence/internal/atlassian"
	"github.com/bennie/mcp-confluence/internal/config"
	"github.com/bennie/mcp-confluence/internal/server"
)

// version is settable via -ldflags "-X main.version=<x>" so the
// container image can stamp a real build SHA / semver at pack time.
// Today it is hard-coded; Phase 11 wires the project.toml BP_GO_VERSION
// metadata that injects this via Paketo.
const version = "v0.1.0"

func main() {
	if err := run(); err != nil {
		// Cancellation (SIGINT, SIGTERM, stdin EOF) is NOT an error
		// for orchestrator purposes — the parent is gracefully
		// shutting us down. Exit 0 silently; no stderr noise.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			os.Exit(0)
		}
		// Anything else is an actual failure. The lifecycle's own
		// logging (config.validate, atlassian.New, server.New) has
		// already produced the human-readable detail on stderr; we
		// just print the wrapped error here. We do NOT prepend
		// "FATAL:" — the inner error may already start with
		// "FATAL:" (config.validate does) and a doubled prefix is
		// noise.
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

// run is the production entrypoint. It wires SIGINT/SIGTERM into a
// cancellable context (per Go's signal.NotifyContext idiom) and
// delegates to runLifecycle. The signal handling lives here — not in
// runLifecycle — so tests can drive runLifecycle with a custom
// context that they cancel themselves.
func run() error {
	ctx, cancel := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	return runLifecycle(ctx)
}

// runLifecycle is the testable load → build → serve pipeline. It
// returns ctx.Err() when the context is cancelled (by signal, by a
// test cancelling an injected context, or by stdin EOF), or an error
// from config / atlassian / server construction if any of those
// steps fails, or the error from srv.Serve() if the server fails on
// its own.
//
// The function never panics on expected failure modes — the load
// step returns a typed config error, the build steps return wrapped
// errors, and the serve step forwards the server's error verbatim.
// The recover() boundary is in internal/tools/safeHandler; the
// entrypoint itself trusts the layered error model.
func runLifecycle(ctx context.Context) error {
	// 1. Load config from env + .env file (fail-fast on missing).
	//    Settings resolution order (LOCKED 2026-07-09, see
	//    specs/01-foundations/03-env-var-contract.md):
	//      1. Process env (highest priority)
	//      2. .env in cwd
	//      3. .env next to the binary
	cfg, err := config.LoadFromEnv()
	if err != nil {
		return err
	}

	// 2. Debug log (stderr only). We deliberately do NOT log cfg.APIKey
	//    — the token is a secret and must never reach a logger.
	if cfg.Debug {
		log.Printf("mcp-confluence %s starting (site=%s, email=%s)",
			version, cfg.SiteName, cfg.UserEmail)
		log.Printf("Note: API token value not logged for security")
	}

	// 3. Build the atlassian HTTP client. AuthMissingError surfaces
	//    here if any of the three required fields is empty (defense
	//    in depth — config.validate already rejected empties, but
	//    atlassian.New re-checks so a future caller bypassing
	//    config.LoadFromEnv still gets a clear error).
	cli, err := atlassian.New(cfg)
	if err != nil {
		return fmt.Errorf("build atlassian client: %w", err)
	}

	// 4. Wire stdin through an io.Pipe so the lifecycle can detect
	//    stdin EOF. We derive a child context whose cancel is owned
	//    by the stdin-watcher goroutine: when the MCP parent closes
	//    its stdin, io.Copy returns, we cancel the child ctx, and
	//    the blocking select in serveUntilDone wakes up with
	//    context.Canceled.
	//
	//    Why a pipe: the metoro-io stdio transport's readLoop reads
	//    from a bufio.Reader around its input, and exits silently
	//    on EOF without invoking any shutdown hook. A separate
	//    goroutine that ALSO reads os.Stdin would compete with the
	//    transport's bufio.Reader for bytes (corrupting the JSON-RPC
	//    stream). Funnelling os.Stdin through a pipe gives us a
	//    single SOURCE of input — we own the writer end — so we get
	//    a clean EOF signal without byte-stealing.
	serveCtx, cancelServe := context.WithCancel(ctx)
	defer cancelServe()
	stdIn, stdOut := wireStdinEOF(serveCtx, cancelServe, os.Stdin, os.Stdout, cfg.Debug)

	// 5. Build the MCP server with the pipe-backed stdio transport.
	srv, err := server.NewWithTransport(
		server.ServerDeps{Config: cfg, Client: cli},
		stdio.NewStdioServerTransportWithIO(stdIn, stdOut),
	)
	if err != nil {
		return fmt.Errorf("build mcp server: %w", err)
	}

	// 6. Serve until serveCtx is cancelled. On the stdio transport
	//    srv.Serve() returns nil almost immediately (the actual
	//    blocking call is the transport's readLoop goroutine); we
	//    then block on ctx.Done() until SIGINT/SIGTERM or stdin
	//    EOF cancels the context.
	return serveUntilDone(serveCtx, srv)
}

// wireStdinEOF returns the (in, out) pair the stdio transport
// should use, and arranges for `cancel` to fire when os.Stdin hits
// EOF. A single goroutine copies os.Stdin into a pipe; when the
// copy completes (parent closed stdin, or copy error), the pipe
// writer is closed AND cancel() is invoked so the main blocking
// select wakes up with context.Canceled.
//
// Output goes straight to os.Stdout — the transport writes are
// flushed by the underlying writer and don't need the pipe
// treatment.
//
// debug controls whether internal errors are logged; pass cfg.Debug
// so the function honors the same flag as the rest of the lifecycle.
//
// On pipe creation failure (effectively impossible on Linux, but
// guarded so a weird runtime env doesn't crash the entrypoint), we
// fall back to the raw FDs — the process will then block on stdin
// until killed externally, which matches the legacy (Phase 0)
// behavior.
func wireStdinEOF(
	ctx context.Context,
	cancel context.CancelFunc,
	stdin *os.File,
	stdout *os.File,
	debug bool,
) (io.Reader, io.Writer) {
	pr, pw, err := os.Pipe()
	if err != nil {
		if debug {
			log.Printf("stdin pipe creation failed: %v; falling back to raw os.Stdin", err)
		}
		return stdin, stdout
	}

	go func() {
		defer func() {
			_ = pw.Close()
		}()
		_, copyErr := io.Copy(pw, stdin)
		// EOF is the expected case (parent closed stdin); not an
		// error. Any other error is worth surfacing in debug mode
		// but is non-fatal — the pipe close below still triggers
		// context cancellation and clean shutdown.
		if copyErr != nil && copyErr != io.EOF && debug {
			log.Printf("stdin copy ended with error: %v", copyErr)
		}
		// Cancel the lifecycle context. This wakes serveUntilDone's
		// <-ctx.Done() select, which returns ctx.Err() ==
		// context.Canceled. Idempotent and safe to call from a
		// goroutine: signal.NotifyContext's cancel has already been
		// deferred by run() and is also safe.
		cancel()
		_ = ctx // keep ctx in scope for the docstring's "honor upstream cancellation"
	}()

	return pr, stdout
}

// serveUntilDone blocks until ctx is cancelled, returning ctx.Err()
// on shutdown. It first calls srv.Serve() to wire up the protocol
// handlers and start the transport's readLoop goroutine; on the
// stdio transport that call returns nil almost immediately (the
// readLoop is the actual blocking call, and it reads from the
// transport's input in a separate goroutine). After Serve returns
// we block on ctx.Done() — the process exit triggered by
// SIGINT/SIGTERM, stdin EOF, or a test cancelling its injected
// context is the shutdown signal we honor here.
//
// If srv.Serve() returns an error (e.g. transport.Start failed),
// we surface it immediately rather than blocking on a context that
// may never fire.
func serveUntilDone(ctx context.Context, srv *mcp.Server) error {
	if err := srv.Serve(); err != nil {
		return err
	}
	<-ctx.Done()
	return ctx.Err()
}
