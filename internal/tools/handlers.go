// Package tools — handlers.go: the five public MCP tool handlers
// (`conf_get` / `conf_post` / `conf_put` / `conf_patch` /
// `conf_delete`) plus the safeHandler panic-recovery wrapper.
//
// Each handler is a thin layer over Phase 6's executeRequest:
//
//  1. decode the JSON args into the typed struct (GetArgs / etc.)
//  2. optionally JSON-marshal args.Body to bytes
//  3. delegate to executeRequest(ctx, client, args, method, body)
//  4. return the resulting encoded body as a string
//
// The handlers do NOT register with the MCP server. Phase 8
// (server bootstrap) consumes the wrapped Handler values produced
// by safeHandler and registers them with metoro-io/mcp-golang.
//
// safeHandler wraps a Handler with a deferred recover() so any
// panic in the inner function (or any function it calls, all the
// way down through executeRequest → atlassian.Client.Do → http
// transport) is converted into a clean "internal error" envelope
// rather than a goroutine crash. This is Class 6 of the spec's
// error shapes (specs/09-anti-patterns/03-error-shapes.md).
//
// The panic message returned to the caller is the literal string
// "internal error (phase <name>)" — deliberately NOT the panic
// value. The phase name lets the operator correlate the error
// with a tool (e.g. "phase conf_get") without leaking the panic
// value, stack trace, or any tool data. The full panic details
// (value + stack) are logged to stderr via log.Printf so the
// operator can diagnose without the LLM seeing the details.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"runtime/debug"

	"github.com/bennie/mcp-confluence/internal/atlassian"
)

// Handler is the function signature every public tool handler in
// this package implements. It accepts the JSON-encoded arguments
// block from the MCP framework as json.RawMessage and returns the
// encoded response body as a string plus an optional error.
//
// The signature intentionally uses json.RawMessage (rather than a
// typed struct) at the boundary because:
//
//  1. Phase 8's metoro-io/mcp-golang registration API takes a
//     closure factory whose first parameter is the JSON-encoded
//     args. Keeping the type at the package boundary avoids a
//     double-encoding round trip inside this package.
//  2. The per-method handlers (HandleGet etc.) do the json.Unmarshal
//     into the typed struct themselves, so a malformed JSON args
//     payload returns a clean error envelope rather than a Go
//     type-error from a decoding pre-check.
type Handler func(ctx context.Context, args json.RawMessage) (string, error)

// HandleGet is the `conf_get` tool handler. It decodes args into
// GetArgs (a body-less struct), then forwards to executeRequest
// with method=GET and body=nil.
func HandleGet(ctx context.Context, client *atlassian.Client, args json.RawMessage) (string, error) {
	var a GetArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("conf_get: decode args: %w", err)
	}
	return executeRequest(ctx, client, a, "GET", nil)
}

// HandlePost is the `conf_post` tool handler. It decodes args into
// PostArgs, JSON-marshals args.Body, then forwards with method=POST
// and the body as bytes.
func HandlePost(ctx context.Context, client *atlassian.Client, args json.RawMessage) (string, error) {
	var a PostArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("conf_post: decode args: %w", err)
	}
	body, err := json.Marshal(a.Body)
	if err != nil {
		return "", fmt.Errorf("conf_post: encode body: %w", err)
	}
	return executeRequest(ctx, client, a, "POST", body)
}

// HandlePut is the `conf_put` tool handler. Same shape as POST
// (body required), but method=PUT.
func HandlePut(ctx context.Context, client *atlassian.Client, args json.RawMessage) (string, error) {
	var a PutArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("conf_put: decode args: %w", err)
	}
	body, err := json.Marshal(a.Body)
	if err != nil {
		return "", fmt.Errorf("conf_put: encode body: %w", err)
	}
	return executeRequest(ctx, client, a, "PUT", body)
}

// HandlePatch is the `conf_patch` tool handler. PATCH takes a JSON
// array of operations (RFC 6902-style) as its body — Body is
// []map[string]any — so we marshal it to a JSON byte slice. The
// upstream sees the resulting JSON array verbatim.
func HandlePatch(ctx context.Context, client *atlassian.Client, args json.RawMessage) (string, error) {
	var a PatchArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("conf_patch: decode args: %w", err)
	}
	body, err := json.Marshal(a.Body)
	if err != nil {
		return "", fmt.Errorf("conf_patch: encode body: %w", err)
	}
	return executeRequest(ctx, client, a, "PATCH", body)
}

// HandleDelete is the `conf_delete` tool handler. Same shape as
// GET (no body), method=DELETE.
func HandleDelete(ctx context.Context, client *atlassian.Client, args json.RawMessage) (string, error) {
	var a DeleteArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("conf_delete: decode args: %w", err)
	}
	return executeRequest(ctx, client, a, "DELETE", nil)
}

// safeHandler wraps an inner Handler with a deferred recover() so
// that any panic — from the handler itself, from executeRequest, or
// from anything deeper (atlassian.Client.Do, http.Transport, etc.) —
// is converted into a clean error envelope instead of crashing the
// MCP server's goroutine.
//
// The wrapped Handler's contract is the same as the inner Handler's:
//
//   - On success: returns (result, nil) unchanged.
//   - On a non-panic error from the inner handler: returns
//     ("", innerErr) — no double-wrapping, no swallowing.
//   - On a panic: returns ("", "internal error (phase <name>)").
//
// The phase name argument is a short, human-readable label for the
// tool (e.g. "conf_get", "conf_post", "test-panic"). It is
// included in the returned error message so an operator looking at
// MCP logs can correlate the safe-mode response with the tool
// without exposing any tool data. The phase name MUST NOT include
// user-supplied data (paths, query strings, bodies) — that data
// stays inside the panic context, which we deliberately discard.
//
// The panic value and stack trace are logged to stderr via
// log.Printf so an operator running the binary directly (or via
// `make dev`) can diagnose. We never log to stdout (the JSON-RPC
// stream) and we never include the panic value in the response.
//
// Implementation notes:
//
//   - The wrapper uses a named return value `(result string, err
//     error)` so the deferred function can replace `err` after a
//     panic. The result is reset to "" in the panic branch — the
//     LLM must not see a partial / corrupted result.
//   - log.Printf writes to stderr by default (the stdlib log
//     package's default logger uses os.Stderr). This satisfies the
//     no-stdout-pollution invariant in
//     specs/09-anti-patterns/01-stdout-pollution.md.
//   - We deliberately do NOT recover on a different goroutine (no
//     `recover()` in a spawned goroutine). That wouldn't catch the
//     panic anyway. The MCP framework calls each handler on its
//     own goroutine so the defer/recover at the function boundary
//     is sufficient.
func safeHandler(name string, inner Handler) Handler {
	if inner == nil {
		// Defensive: returning a Handler that errors on every call
		// rather than a nil Handler keeps the registry code simple
		// (no nil checks at every registration site).
		return func(ctx context.Context, args json.RawMessage) (string, error) {
			return "", fmt.Errorf("internal error (phase %s): handler not configured", name)
		}
	}
	return func(ctx context.Context, args json.RawMessage) (result string, err error) {
		defer func() {
			r := recover()
			if r == nil {
				// Normal return path: nothing to do. The named
				// returns already carry (result, nil).
				return
			}
			// Panic path: log value + stack to stderr for the
			// operator, then replace the return values with the
			// non-leaking "internal error (phase <name>)" envelope.
			//
			// We intentionally use log.Printf (not fmt.Fprintln)
			// because log's default logger writes to stderr. The
			// format string is fixed; %v values are the panic
			// value and the stack trace. The MPC caller never
			// sees either.
			log.Printf("PANIC in handler %s: %v\n%s", name, r, debug.Stack())
			result = ""
			err = fmt.Errorf("internal error (phase %s)", name)
		}()
		return inner(ctx, args)
	}
}
