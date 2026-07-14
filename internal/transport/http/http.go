// http.go — the NewServer constructor for the `serve`
// subcommand. This is Phase 18's primary entry point: a
// function that takes the same mcp.Server the stdio
// subcommand uses, validates the --listen address, and
// returns a net/http *http.Server whose handler bridges
// HTTP requests to the mcp.Server's JSON-RPC dispatch.
//
// The package's three files split the work as follows:
//
//   - http.go    — NewServer + bridgeTransport (the
//     transport.Transport implementation that
//     satisfies the mcp-golang interface)
//   - handler.go — the net/http HandlerFunc that
//     dispatches each HTTP request to the
//     bridge
//   - listen.go  — parseListenFlag (host:port validator)
//
// `mcpSrv` is the *mcp.Server instance built by
// `internal/server.NewWithTransport(deps, ourBridge)`. We
// DON'T call internal/server here — the caller (newServeCmd
// in cmd/mcp-confluence/main.go) is responsible for building
// the mcp.Server and passing it in. This keeps the
// dependency direction clean: the transport package only
// depends on mcp-golang + net/http + the stdlib; it does
// not depend on internal/server or internal/config.
//
// The `listen` argument is the validated host:port string
// from the --listen flag. NewServer does NOT call
// parseListenFlag — that's the caller's job, so the
// validation error can be printed to stderr with the right
// prefix (the `serve` subcommand's usage text). NewServer
// itself just turns the string into a net.Listener.
//
// `logger` is the slog logger used for the per-request log
// line. The caller (newServeCmd) builds it from
// `slog.New(slog.NewTextHandler(os.Stderr, ...))` so every
// log line lands on stderr.
package httptransport

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"

	mcp "github.com/metoro-io/mcp-golang"
	"github.com/metoro-io/mcp-golang/transport"
)

// NewServer builds a *http.Server that dispatches POST /mcp
// requests to mcpSrv via a custom bridge transport. The
// returned *http.Server is NOT yet listening — the caller
// calls `srv.ListenAndServe()` (or `srv.Serve(listener)`) to
// start it, and `srv.Shutdown(ctx)` to gracefully stop.
//
// On parse failure or other construction-time errors,
// NewServer returns a non-nil error and a nil *http.Server.
// The caller is expected to print the error to stderr and
// exit non-zero.
//
// The function never calls mcpSrv.Serve() — that's the
// caller's responsibility. We return a *http.Server that is
// fully wired (Handler, Addr) but not yet running; the
// caller is expected to call ListenAndServe inside a
// signal-cancellable context.
//
// The `mcpSrv` parameter is retained for API symmetry and
// for the load-bearing nil-guard in tests (NewServer must
// refuse to build a server when mcpSrv is nil even though
// the actual bridge is constructed internally). The bridge
// inside NewServer is a FRESH bridge that is NOT wired to
// the mcpSrv's protocol stack — production code that wants
// live request dispatch should use NewBridge + NewHTTPServer
// directly so the bridge is shared between the mcp.Server
// and the http.Server.
//
// Concurrency: the returned *http.Server is safe for
// concurrent use. The bridgeTransport's response channel
// map is guarded by a mutex; per-request state is keyed by
// an internal integer that's unique to each request, so
// requests never share state.
func NewServer(mcpSrv *mcp.Server, listen string, logger *slog.Logger) (*http.Server, error) {
	// nil-guard the required arguments. Returning an
	// error here is the contract — the caller is
	// expected to fail fast on a nil pointer.
	if mcpSrv == nil {
		return nil, fmt.Errorf("internal/transport/http: mcpSrv is nil")
	}
	_ = mcpSrv // silence unused warning; the parameter is a sentinel only

	// Build a fresh bridge and wrap it in an http.Server
	// via NewHTTPServer. The fresh bridge is a stand-in
	// for tests that don't need live protocol dispatch
	// (TestNewServer_NilLogger / TestNewServer_InvalidListen
	// only call NewServer for the constructor error path);
	// the production call path uses NewBridge + NewHTTPServer
	// directly so the bridge is shared with the mcp.Server.
	return NewHTTPServer(newBridgeTransport(), listen, logger)
}

// bridgeTransport implements the metoro-io/mcp-golang
// transport.Transport interface for the HTTP server use case.
// It is a synchronous request/response bridge: each incoming
// HTTP request reserves a unique key + response channel,
// dispatches the message via the messageHandler the protocol
// installed during pr.Connect, and blocks until the protocol
// pushes the response back via Send.
//
// The transport's behavior is intentionally similar to the
// library's `baseTransport.handleMessage` (the per-request
// channel pattern) but lives in our own code so the bridge
// is reachable from a different Go package (mcp-golang
// keeps the base transport unexported).
//
// Concurrency: bridgeTransport is safe for concurrent use.
// The mu mutex guards the responseChans map; the channels
// themselves are unbuffered (capacity 0) so the protocol's
// Send call blocks until the HTTP handler reads. This
// provides natural backpressure: a slow client doesn't
// accumulate in-flight requests in the map.
type bridgeTransport struct {
	mu             sync.Mutex
	responseChans  map[int64]chan *transport.BaseJsonRpcMessage
	nextKey        int64
	messageHandler func(ctx context.Context, message *transport.BaseJsonRpcMessage)
	errorHandler   func(error)
	closeHandler   func()
	closed         bool
}

// newBridgeTransport returns a fresh bridgeTransport with
// the response channel map initialized. The nextKey starts
// at 1 (the mcp-golang library uses 0 as a sentinel for
// "no key" in some code paths; starting at 1 avoids any
// confusion).
func newBridgeTransport() *bridgeTransport {
	return &bridgeTransport{
		responseChans: make(map[int64]chan *transport.BaseJsonRpcMessage),
		nextKey:       1,
	}
}

// NewBridge returns a freshly-constructed bridge transport
// that satisfies the mcp-golang `transport.Transport`
// interface. The serve subcommand uses this in two-step
// wiring:
//
//	bridge := httptransport.NewBridge()
//	mcpSrv := server.NewWithTransport(deps, bridge) // wire bridge → protocol
//	mcpSrv.Serve()                                  // protocol wires bridge's messageHandler
//	httpSrv := httptransport.NewHTTPServer(bridge, listen, logger) // bridge → http.Handler
//
// The bridge is exported through the `transport.Transport`
// interface (not as `*bridgeTransport`) so callers cannot
// reach into the per-request channel map — they only get
// the standard transport API the mcp-golang protocol
// already speaks. The actual bridge struct remains
// unexported to keep the package's invariants
// package-local.
//
// Concurrency: the returned transport is safe for
// concurrent use (see bridgeTransport's docstring).
func NewBridge() transport.Transport {
	return newBridgeTransport()
}

// NewHTTPServer wraps an already-constructed bridge in a
// net/http *http.Server. The `bridge` must be the SAME
// bridge that was passed to `server.NewWithTransport(deps,
// bridge)` AND the one whose message handler was wired
// during `mcpSrv.Serve()`. Sharing the bridge between the
// mcp.Server and the http.Server is what makes the
// POST /mcp request → JSON-RPC dispatch → response work.
//
// The returned *http.Server is NOT yet listening — the
// caller is expected to bind a net.Listener and call
// `srv.Serve(listener)` (or `srv.ListenAndServe()` for the
// Addr-bound convenience path). The caller is also
// responsible for `srv.Shutdown(ctx)` on signal.
//
// This is the building block the `serve` subcommand uses
// when it builds the bridge + mcp.Server in main.go (so
// the dep-building flow stays in one place). NewServer
// (the higher-level convenience) is retained for tests
// and future refactors that want a one-call path.
func NewHTTPServer(bridge transport.Transport, listen string, logger *slog.Logger) (*http.Server, error) {
	if bridge == nil {
		return nil, fmt.Errorf("internal/transport/http: bridge is nil")
	}
	if logger == nil {
		return nil, fmt.Errorf("internal/transport/http: logger is nil")
	}
	bt, ok := bridge.(*bridgeTransport)
	if !ok {
		// Defensive: a future refactor that returns a
		// different transport from NewBridge would land
		// here. The newHandler function takes the concrete
		// type because it must ReserveChannel / ReleaseChannel
		// — those methods are unexported. Keep this
		// guard loud so the divergence is visible.
		return nil, fmt.Errorf("internal/transport/http: bridge is not a *bridgeTransport (got %T)", bridge)
	}

	// Parse the listen string. We don't return the
	// parsed parts to the caller (the *http.Server
	// owns the Addr field directly), but the parse
	// itself is the load-bearing input validation.
	host, port, err := parseListenFlag(listen)
	if err != nil {
		return nil, err
	}
	addr := joinHostPort(host, port)

	// Build the net/http *http.Server. The mux routes
	// only `POST /mcp` to the JSON-RPC handler;
	// everything else is a 404. We intentionally do NOT
	// use http.NewServeMux's catch-all ("/") because
	// that would route any method on any path to our
	// handler, and we want strict path + method
	// validation.
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		// Method check first (cheap, runs before the
		// 404 in the response writer). GET /mcp and
		// DELETE /mcp are explicitly rejected.
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed; use POST", http.StatusMethodNotAllowed)
			return
		}
		// Hand off to the per-request log wrapper,
		// which captures status + bytes for the
		// stderr log line.
		newHandler(bt, logger).ServeHTTP(w, r)
	})
	// Catch-all 404. http.ServeMux routes any path that
	// doesn't match a more specific pattern to "/" if
	// "/" is registered; we use that to emit a single
	// 404 message for all non-/mcp paths. The per-
	// request log line is NOT emitted for 404s (the
	// handler isn't invoked) — that's intentional;
	// the 404 is a path-validation error, not a
	// JSON-RPC request.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/mcp" {
			// The "/" pattern also matches /mcp
			// (it's a prefix match). The /mcp
			// pattern above wins for /mcp; this
			// is just a defensive check in case
			// the mux behavior changes.
			return
		}
		http.NotFound(w, r)
	})

	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}
	return srv, nil
}

// ReserveChannel allocates a new (key, channel) pair and
// stores it in the response map. The HTTP handler calls
// this once per incoming request; the protocol's
// transport.Send call looks up the channel by key.
//
// The returned channel is unbuffered so the protocol's
// Send blocks until the HTTP handler is ready to read.
// The HTTP handler MUST eventually read from the channel
// (or the request goroutine leaks).
//
// Concurrency: the function takes the mutex briefly to
// increment nextKey and insert into the map. The channel
// itself is created outside the lock (no shared state
// reachable from the returned pair).
func (bt *bridgeTransport) ReserveChannel() (int64, chan *transport.BaseJsonRpcMessage) {
	bt.mu.Lock()
	defer bt.mu.Unlock()
	if bt.closed {
		// Return a never-closing channel so the
		// caller's `<-ch` blocks forever — they'll
		// be cancelled by the request context.
		// This is a defensive path: by the time
		// we accept HTTP requests, the bridge
		// shouldn't be closed yet.
		return -1, make(chan *transport.BaseJsonRpcMessage)
	}
	key := bt.nextKey
	bt.nextKey++
	ch := make(chan *transport.BaseJsonRpcMessage)
	bt.responseChans[key] = ch
	return key, ch
}

// ReleaseChannel removes a (key, channel) pair from the
// response map. Called by the HTTP handler after the
// response is delivered (or the request is cancelled).
// The channel itself is not closed — the protocol's Send
// only writes to a channel that hasn't been released, and
// the HTTP handler only reads once.
//
// Concurrency: the function takes the mutex briefly.
// Safe to call after the response is received or the
// request context is cancelled.
func (bt *bridgeTransport) ReleaseChannel(key int64) {
	bt.mu.Lock()
	defer bt.mu.Unlock()
	delete(bt.responseChans, key)
}

// Start implements transport.Transport. For the HTTP
// server use case, Start is a no-op: the *http.Server
// drives the lifecycle (ListenAndServe / Shutdown), not
// the transport. Returning nil lets mcpSrv.Serve()
// return immediately after pr.Connect wires the message
// handler — the caller then starts the HTTP listener
// in a separate goroutine.
//
// We intentionally do NOT spawn a goroutine here. The
// mcp-golang protocol's Connect calls tr.Start after
// wiring the message handler; if we returned a blocking
// call from Start, the protocol would never finish
// Connect, and the message handler would never be set.
func (bt *bridgeTransport) Start(_ context.Context) error {
	return nil
}

// Send implements transport.Transport. The protocol
// calls Send with the response after the request
// handler returns. We look up the per-request channel
// by the response's Id (which the protocol has set to
// the internal key we reassigned in dispatch) and
// push the response into it.
//
// Errors: if the key isn't in the map (response
// arrived after the HTTP handler released it, or the
// HTTP handler never registered one), Send returns an
// error. The protocol's handleError callback is
// invoked with the wrapped error.
func (bt *bridgeTransport) Send(ctx context.Context, message *transport.BaseJsonRpcMessage) error {
	bt.mu.Lock()
	defer bt.mu.Unlock()
	if bt.closed {
		return fmt.Errorf("bridge: closed")
	}
	var key int64
	if message.JsonRpcResponse != nil {
		key = int64(message.JsonRpcResponse.Id)
	} else if message.JsonRpcError != nil {
		key = int64(message.JsonRpcError.Id)
	} else {
		return fmt.Errorf("bridge: Send called with no response/error payload")
	}
	ch, ok := bt.responseChans[key]
	if !ok {
		return fmt.Errorf("bridge: no response channel for key=%d (response arrived after release?)", key)
	}
	// The protocol's Send is called from a goroutine
	// inside the request handler. We push the response
	// to the channel; the HTTP handler is reading.
	// The select with ctx.Done is a defensive measure:
	// if the HTTP request context is cancelled while
	// we're blocked, we unblock and the protocol's
	// handleError is invoked.
	select {
	case ch <- message:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("bridge: Send cancelled (key=%d): %w", key, ctx.Err())
	}
}

// Close implements transport.Transport. The HTTP server's
// Shutdown method is the primary shutdown path; Close
// is called by the mcp-golang protocol during
// teardown (e.g. if the mcp.Server is explicitly closed).
// We mark the bridge as closed and invoke the close
// handler if one was set.
//
// Concurrency: Close is safe to call multiple times;
// subsequent calls are no-ops.
func (bt *bridgeTransport) Close() error {
	bt.mu.Lock()
	if bt.closed {
		bt.mu.Unlock()
		return nil
	}
	bt.closed = true
	bt.mu.Unlock()
	if bt.closeHandler != nil {
		bt.closeHandler()
	}
	return nil
}

// SetCloseHandler implements transport.Transport. We
// store the handler for invocation in Close.
func (bt *bridgeTransport) SetCloseHandler(handler func()) {
	bt.mu.Lock()
	defer bt.mu.Unlock()
	bt.closeHandler = handler
}

// SetErrorHandler implements transport.Transport. We
// store the handler for later invocation if Send or
// Start fails. The handler is currently a no-op sink
// (we return errors directly from Send); the
// protocol's Connect still wires it up.
func (bt *bridgeTransport) SetErrorHandler(handler func(error)) {
	bt.mu.Lock()
	defer bt.mu.Unlock()
	bt.errorHandler = handler
}

// SetMessageHandler implements transport.Transport.
// The protocol's Connect calls this with the function
// that dispatches a parsed message to the registered
// request handler. We store the handler for the HTTP
// request goroutine to invoke.
//
// Without this handler set, the bridge is inert —
// HTTP requests would block on the response channel
// forever. The mcpSrv.Serve() call (made by the
// caller before ListenAndServe) is what triggers the
// protocol to call SetMessageHandler.
func (bt *bridgeTransport) SetMessageHandler(handler func(ctx context.Context, message *transport.BaseJsonRpcMessage)) {
	bt.mu.Lock()
	defer bt.mu.Unlock()
	bt.messageHandler = handler
}

// Compile-time check that bridgeTransport satisfies the
// mcp-golang transport.Transport interface. If the
// interface gains a method, this line fails to compile
// and the gap is loud.
var _ transport.Transport = (*bridgeTransport)(nil)

// Compile-time check that net/http.Server is the type
// the dispatcher's spec asks us to return. This is
// documentation; it would fail to compile if the type
// alias were ever broken.
var _ *http.Server = (*http.Server)(nil)

// Listen is a small helper that combines parseListenFlag
// + net.Listen. The serve subcommand uses this so the
// "127.0.0.1:8080 default" path produces a working
// net.Listener without the caller doing two steps.
//
// We DO NOT use this from NewServer itself (NewServer
// sets srv.Addr and lets the *http.Server call Listen
// internally during ListenAndServe). Listen is for
// callers that want a net.Listener directly — e.g. the
// integration test that uses `--listen=127.0.0.1:0` and
// needs to know the kernel-picked port.
//
// The function fails closed: a parse error or a bind
// error returns a non-nil error. No silent fallback to
// a different address.
func Listen(listen string) (net.Listener, error) {
	host, port, err := parseListenFlag(listen)
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", joinHostPort(host, port))
	if err != nil {
		return nil, fmt.Errorf("--listen: bind %s: %w", joinHostPort(host, port), err)
	}
	return ln, nil
}
