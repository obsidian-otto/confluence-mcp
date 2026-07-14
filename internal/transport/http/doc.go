// Package httptransport — TCP/HTTP JSON-RPC transport for the `serve`
// subcommand. This is Phase 18 of the mcp-confluence CLI refactor
// (see IMPLEMENTATION_PLAN.md around line 1460). The package wraps
// the SAME mcp.Server instance the stdio subcommand uses; the only
// thing that changes is the framing — JSON-RPC 2.0 over
// `Content-Type: application/json` HTTP request/response instead of
// newline-delimited JSON over a stdio fd.
//
// Architecture:
//
//	+----------------+    POST /mcp     +-----------------+
//	|  curl / Hermes | ---------------> |  http.Server    |
//	+----------------+     body          |  (net/http std) |
//	                                      +-----------------+
//	                                               |
//	                                               v
//	                                    +-------------------------+
//	                                    |  handler.go HandlerFunc |
//	                                    |  (path/method validate, |
//	                                    |   body read, dispatch)  |
//	                                    +-------------------------+
//	                                               |
//	                                               v
//	                                    +-------------------------+
//	                                    |  bridgeTransport        |
//	                                    |  (transport.Transport)  |
//	                                    +-------------------------+
//	                                               |
//	                                               v
//	                                    +-------------------------+
//	                                    |  mcp.Server (shared)    |
//	                                    |  -> tools/list          |
//	                                    |  -> tools/call          |
//	                                    +-------------------------+
//
// The bridgeTransport satisfies the metoro-io/mcp-golang
// `transport.Transport` interface, so it slots directly into
// `server.NewWithTransport(deps, ourBridge)` exactly the same way
// the stdio transport does in runLifecycle(). The single endpoint
// is `POST /mcp`; everything else returns 404.
//
// Why a custom transport instead of the built-in
// `mcp-golang/transport/http.HTTPTransport`: the library's
// HTTPTransport owns its own *http.Server internally and exposes
// only `Start(ctx)` / `Close()`. We need to return a net/http
// *http.Server to the caller so they can wire signal-driven
// graceful shutdown (httpServer.Shutdown(ctx)). A custom
// bridgeTransport is the minimum plumbing that lets us hand back
// a real *http.Server with a HandlerFunc of our own composition —
// and lets us log per-request method/path/status/bytes to stderr
// without monkey-patching the library.
//
// All logs go to stderr; stdout is reserved for HTTP body bytes
// only. The API token is NEVER passed to a logger — the package
// only ever sees the (listen, logger) parameters and the
// per-request bytes (which never include Authorization headers
// because the binary holds the credential, not the caller).
package httptransport
