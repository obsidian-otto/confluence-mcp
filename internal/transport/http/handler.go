// handler.go — the net/http HandlerFunc that bridges an
// HTTP request to the mcp.Server via a custom transport.
//
// The HTTP contract is intentionally narrow: a single endpoint
// `POST /mcp` accepting a JSON-RPC 2.0 body and returning a
// JSON-RPC 2.0 response. Anything outside that contract is a
// 404. This is the same envelope shape the stdio transport
// speaks, so the same MCP client (Hermes, curl, anything that
// speaks JSON-RPC) can use either transport with no adapter
// glue at the application layer.
//
// The handler does NOT log the request body, the response body,
// the headers, or any Authorization value. The per-request log
// line is `serve <METHOD> <path> <status> <bytes>` — that's
// it. The Atlassian API token is held inside the binary (it
// was loaded at startup by config.LoadFromEnv); it never
// crosses the HTTP boundary, so there's nothing to redact
// in the request body.
package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/metoro-io/mcp-golang/transport"
)

// requestLogLine is the per-request log line written to stderr.
// The format is stable so an operator's log parser can grep for
// it:
//
//	serve <METHOD> <path> <status> <bytes>
//
// Example:
//
//	serve POST /mcp 200 4123
//
// The bytes field is the size of the JSON-RPC response body
// we wrote back, not the request body. It is computed AFTER
// the response is encoded so a parse failure (which would
// produce an error envelope of a different size) is also
// accurately counted.
const requestLogLine = "serve %s %s %d %d"

// jsonContentType is the Content-Type set on every successful
// response and on the error envelopes. The MCP specification
// (https://modelcontextprotocol.io/) requires application/json
// for the JSON-RPC payload.
const jsonContentType = "application/json"

// errResponse is the JSON-RPC 2.0 error envelope returned when
// the bridge itself fails to process a request (parse error,
// method-not-found, internal error). The shape matches the
// JSON-RPC 2.0 spec:
//
//	{"jsonrpc":"2.0","id":<id>,"error":{"code":<code>,"message":"<msg>"}}
//
// Standard error codes (per JSON-RPC 2.0):
//
//	-32700  Parse error     — invalid JSON
//	-32600  Invalid request — non-conforming JSON-RPC envelope
//	-32601  Method not found
//	-32602  Invalid params
//	-32603  Internal error
//
// We only emit Parse error, Method not found, and Internal
// error from the bridge itself. Tool-handler errors are wrapped
// by the protocol layer (via p.sendErrorResponse) and arrive
// here as regular BaseJSONRPCResponse/Error objects.
type errResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Error   errBody     `json:"error"`
}

// errBody is the inner error object inside errResponse. We use
// a struct (not a map) so the JSON tags are explicit and the
// field order is stable.
type errBody struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// errCodeParse is the JSON-RPC 2.0 "Parse error" code. Emitted
// when the request body is not valid JSON or does not have a
// recognizable JSON-RPC 2.0 shape.
const errCodeParse = -32700

// errCodeInternal is the JSON-RPC 2.0 "Internal error" code.
// Emitted when the bridge itself fails to dispatch the
// request — e.g. context cancellation, bridge closed, or an
// unexpected nil response.
const errCodeInternal = -32603

// readBodyLimit is the maximum request body size we'll read.
// MCP JSON-RPC frames are small (a tools/call with arguments
// is typically < 4 KB; even a conf_post with a large markdown
// payload is well under 1 MB). 4 MiB is a generous ceiling
// that prevents a malicious caller from OOM-ing the process
// by streaming an unbounded body. Requests larger than this
// get a 413 Payload Too Large.
const readBodyLimit = 4 * 1024 * 1024

// writeTimeout is the per-response Write timeout. It bounds
// the time we spend writing a single response back to the
// client. If the client is slow to drain, we'd rather drop
// the response than tie up a goroutine forever. 30 seconds
// is comfortable for a tools/call that may take a while to
// process.
const writeTimeout = 30 * time.Second

// newHandler returns the http.HandlerFunc that serves the
// single `POST /mcp` endpoint. The handler is the boundary
// between the net/http request lifecycle and the bridge
// transport's synchronous request/response channel — the
// body is parsed once, the request is dispatched via the
// bridge, and the response (or an error envelope) is
// written back as JSON.
//
// `logger` is the slog logger wired in by NewServer. The
// handler logs one line per request through it; the caller's
// per-request format is fixed (see requestLogLine above) so
// log parsers can rely on the structure.
//
// `br` is the request/response correlation primitive
// that the bridgeTransport in http.go exposes. It pairs
// each incoming HTTP request with a unique channel key so
// the protocol's asynchronous response-delivery path can
// route the response back to the originating HTTP request.
func newHandler(br *bridgeTransport, logger *slog.Logger) http.Handler {
	// serveMCP is the inner handler — defined as a
	// closure that captures `br` so dispatch() has
	// access to the bridge without going through a
	// package-level variable.
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Method check (the mux returns 405 via the
		// outer /mcp handler; this is defensive).
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Limit the body read to readBodyLimit.
		// http.MaxBytesReader is the stdlib helper
		// that returns a 413 on overflow and closes
		// the connection.
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, readBodyLimit))
		if err != nil {
			writeError(w, nil, errCodeParse, "request body too large or unreadable: "+err.Error())
			return
		}
		if len(body) == 0 {
			writeError(w, nil, errCodeParse, "request body is empty")
			return
		}

		// Dispatch through the bridge. The bridge
		// returns the raw JSON-RPC response bytes;
		// we forward them as-is with application/json.
		// On bridge error (context cancelled, bridge
		// closed), we return a JSON-RPC internal-
		// error envelope with a nil id — there's no
		// way to extract an id from a body we
		// couldn't parse.
		ctx := r.Context()
		respBytes, status, err := dispatch(ctx, br, body)
		if err != nil {
			// Attempt to recover the request id
			// from the body so the error envelope
			// is well-formed. If the body wasn't
			// JSON we have no id; use nil (the
			// JSON-RPC 2.0 spec allows null id on
			// parse errors).
			id := extractID(body)
			writeError(w, id, errCodeInternal, err.Error())
			return
		}

		// On the wire: status from dispatch (200 for
		// normal request/response, 202 for notifications),
		// Content-Type application/json, body = the
		// JSON-RPC response bytes the bridge returned.
		w.Header().Set("Content-Type", jsonContentType)
		w.WriteHeader(status)
		_, _ = w.Write(respBytes)
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Per-request deadline: the response Write
		// timeout applies to the whole handler — if
		// the bridge takes longer than writeTimeout,
		// the response is abandoned and the client
		// sees a connection reset. We intentionally
		// do NOT cap the bridge dispatch time (a
		// tools/call to Confluence may legitimately
		// take many seconds); the timeout is on the
		// wire-write, not the dispatch.
		//
		// We use http.TimeoutHandler so the deadline
		// is applied uniformly to header writes,
		// body writes, and flushes.
		timed := http.TimeoutHandler(inner, writeTimeout, "mcp-confluence: response timeout")

		// Wrap the inner handler in a recorder so
		// we can capture status + bytes for the
		// per-request log line, even when the inner
		// handler writes directly to w. We track
		// bytes via a countingResponseWriter.
		crw := &countingResponseWriter{ResponseWriter: w, status: http.StatusOK}
		timed.ServeHTTP(crw, r)

		// Per-request log line. The status defaults
		// to 200 (set on the response writer at
		// construction); if the inner handler called
		// WriteHeader(code) the
		// countingResponseWriter captured that. The
		// byte count is the response body size.
		logger.Info(
			fmt.Sprintf(requestLogLine, r.Method, r.URL.Path, crw.status, crw.bytes),
		)
	})
}

// dispatch is the synchronous bridge from an HTTP request body
// to the bridge transport's response channel. It mirrors the
// `baseTransport.handleMessage` pattern from mcp-golang (parse
// body, reserve a key, call messageHandler, wait on the response
// channel) but in our own code so we can return the raw response
// bytes synchronously to the HTTP handler.
//
// The function is the boundary between the HTTP request
// goroutine and the mcp-golang protocol's internal goroutine.
// The protocol runs `handler(ctx, request)` in a goroutine and
// then calls `transport.Send(ctx, response)` to push the
// response back; our bridge's `Send` writes the response into a
// per-request channel that this function is blocked on.
//
// For notifications (no id), the protocol's handleNotification
// is fire-and-forget — no response is ever sent. The function
// returns 202 Accepted (HTTP convention for "request received,
// no response body") to the caller; the per-request log line
// still fires.
func dispatch(ctx context.Context, br *bridgeTransport, body []byte) (respBytes []byte, status int, err error) {
	// Parse the body into a BaseJsonRpcMessage. The body
	// is one of: request, notification, response, or error.
	// We only handle request and notification here —
	// responses and errors are server-to-server (this is a
	// server) and don't make sense from an HTTP client.
	//
	// We try the request type first (the common case);
	// fall back to notification. We use a type-switch on
	// the JSON shape (presence of "id" + "method" = request,
	// presence of "method" without "id" = notification).

	msg, err := parseMessage(body)
	if err != nil {
		return nil, http.StatusOK, err
	}

	// Notifications are fire-and-forget — the protocol
	// won't push a response to our channel, so we don't
	// reserve one. We deliver the notification to the
	// message handler and return 202 Accepted.
	if msg.Type == transport.BaseMessageTypeJSONRPCNotificationType {
		br.mu.Lock()
		mh := br.messageHandler
		br.mu.Unlock()
		if mh == nil {
			return nil, http.StatusOK, errors.New("bridge not connected: messageHandler is nil")
		}
		mh(ctx, msg)
		return []byte(""), http.StatusAccepted, nil
	}

	// Request: reserve a response channel. The protocol
	// reassigns the request's Id to this internal key so
	// the response is routed back to us through the
	// channel map. The original id is preserved in
	// `originalID` so we can reassign it before returning
	// the response bytes.
	originalID := msg.JsonRpcRequest.Id
	key, ch := br.ReserveChannel()
	defer br.ReleaseChannel(key)

	// Reassign the id to the internal key. The protocol's
	// handler reads request.Id when building the response
	// envelope, so the response's JsonRpcResponse.Id will
	// be the internal key. We restore the original id at
	// the end.
	msg.JsonRpcRequest.Id = transport.RequestId(key)

	// Call the message handler. The protocol's
	// `Connect` wired messageHandler to p.handleRequest,
	// which spawns an internal goroutine and eventually
	// calls transport.Send(ctx, response). We invoke it
	// from THIS goroutine — but the protocol's internal
	// goroutine will still handle the response delivery;
	// we just need to get the message INTO the protocol.
	br.mu.Lock()
	mh := br.messageHandler
	br.mu.Unlock()
	if mh == nil {
		return nil, http.StatusOK, errors.New("bridge not connected: messageHandler is nil (call bridge.Start first)")
	}
	mh(ctx, msg)

	// Block on the response channel. The protocol's
	// internal goroutine will call transport.Send → our
	// bridge writes the response here.
	select {
	case resp := <-ch:
		// Restore the original id on the response so
		// the caller sees the id they sent.
		if resp.JsonRpcResponse != nil {
			resp.JsonRpcResponse.Id = originalID
		}
		// Marshal the response to JSON.
		bytes, mErr := json.Marshal(resp)
		if mErr != nil {
			return nil, http.StatusOK, fmt.Errorf("marshal response: %w", mErr)
		}
		return bytes, http.StatusOK, nil
	case <-ctx.Done():
		return nil, http.StatusOK, fmt.Errorf("request cancelled: %w", ctx.Err())
	}
}

// parseMessage parses a JSON-RPC 2.0 body into a
// BaseJsonRpcMessage. We support requests (have an "id" and
// "method") and notifications (have a "method" but no "id").
// Responses and errors from a client are not expected on a
// server endpoint and are rejected with a parse error.
//
// The function is intentionally lenient: it accepts both
// numeric and string ids (JSON-RPC 2.0 allows either) by
// decoding into interface{} for the id field. The
// mcp-golang library then marshals it back to its
// canonical numeric form on the response.
//
// Errors here are returned as-is; the caller is expected
// to wrap them in a JSON-RPC error envelope.
func parseMessage(body []byte) (*transport.BaseJsonRpcMessage, error) {
	// First, look for a top-level discriminator: an "id"
	// field means request, no "id" but a "method" means
	// notification, a "result" means response (rejected),
	// an "error" means error response (rejected).
	//
	// We use json.RawMessage and a quick shape check to
	// avoid unmarshaling twice.

	var probe struct {
		Method string          `json:"method"`
		ID     json.RawMessage `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return nil, fmt.Errorf("malformed JSON: %w", err)
	}

	// Server-side reject: incoming responses/errors.
	if probe.Method == "" {
		if len(probe.Result) > 0 || len(probe.Error) > 0 {
			return nil, errors.New("incoming response/error not allowed on a server endpoint")
		}
		return nil, errors.New("JSON-RPC envelope missing required 'method' field")
	}

	msg := &transport.BaseJsonRpcMessage{}

	if len(probe.ID) > 0 && !isJSONNull(probe.ID) {
		// Request: has method and id.
		var req transport.BaseJSONRPCRequest
		if err := json.Unmarshal(body, &req); err != nil {
			return nil, fmt.Errorf("malformed JSON-RPC request: %w", err)
		}
		msg.Type = transport.BaseMessageTypeJSONRPCRequestType
		msg.JsonRpcRequest = &req
		return msg, nil
	}

	// Notification: has method but no id. The
	// mcp-golang protocol's handleNotification does not
	// send a response (notifications are fire-and-forget),
	// so the response channel will never fire. We still
	// need to consume the request — return a special
	// error so the HTTP handler can return 204 No Content
	// (the JSON-RPC convention for accepted notifications).
	var notif transport.BaseJSONRPCNotification
	if err := json.Unmarshal(body, &notif); err != nil {
		return nil, fmt.Errorf("malformed JSON-RPC notification: %w", err)
	}
	msg.Type = transport.BaseMessageTypeJSONRPCNotificationType
	msg.JsonRpcNotification = &notif
	return msg, nil
}

// extractID attempts to recover the JSON-RPC id from a
// (possibly malformed) body. Used by the error path: if the
// bridge fails to dispatch, we want to return a JSON-RPC
// error envelope with the caller's id so they can correlate
// the failure with their request. If the body isn't JSON or
// has no id, this returns nil (allowed by JSON-RPC 2.0 for
// parse errors).
func extractID(body []byte) interface{} {
	var probe struct {
		ID json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return nil
	}
	if len(probe.ID) == 0 || isJSONNull(probe.ID) {
		return nil
	}
	// Try to return the id as a number first (the
	// common case for JSON-RPC 2.0), then a string.
	var n int64
	if err := json.Unmarshal(probe.ID, &n); err == nil {
		return n
	}
	var s string
	if err := json.Unmarshal(probe.ID, &s); err == nil {
		return s
	}
	// Fall back to the raw JSON.
	return json.RawMessage(probe.ID)
}

// isJSONNull reports whether a json.RawMessage is the JSON
// literal `null`. Used to distinguish notifications (id=null
// is allowed but treated as no id) from requests (id=null
// is also valid per JSON-RPC 2.0, but the mcp-golang
// protocol treats null id as a request).
func isJSONNull(raw json.RawMessage) bool {
	// json.RawMessage of "null" has the bytes "null" (4 bytes).
	// Trim whitespace to be safe.
	for _, b := range raw {
		if b == ' ' || b == '\t' || b == '\n' || b == '\r' {
			continue
		}
		return b == 'n' && len(raw) >= 4 && string(raw[:4]) == "null"
	}
	return false
}

// writeError writes a JSON-RPC 2.0 error envelope to w. The
// status is always 200 per the JSON-RPC 2.0 spec — the
// error semantics are in the envelope, not the HTTP status.
// (Some implementations return 4xx for parse errors; we
// follow the spec's "always 200 unless transport failed"
// convention so clients only need to parse one envelope
// shape.)
func writeError(w http.ResponseWriter, id interface{}, code int, msg string) {
	envelope := errResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: errBody{
			Code:    code,
			Message: msg,
		},
	}
	w.Header().Set("Content-Type", jsonContentType)
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(envelope)
}

// countingResponseWriter is a thin wrapper around
// http.ResponseWriter that records the status code and the
// number of bytes written. Used by the outer handler to
// surface the per-request log line.
//
// Defaults status to 200 (the standard "implicit OK") so
// handlers that don't call WriteHeader at all are still
// logged correctly.
type countingResponseWriter struct {
	http.ResponseWriter
	status      int
	bytes       int
	wroteHeader bool
}

// WriteHeader captures the status code. Subsequent writes
// are counted via Write.
func (c *countingResponseWriter) WriteHeader(code int) {
	if c.wroteHeader {
		// Defensive: net/http itself panics on double
		// WriteHeader. We pass through to the underlying
		// writer to keep the behavior aligned with
		// stdlib semantics.
		c.ResponseWriter.WriteHeader(code)
		return
	}
	c.status = code
	c.wroteHeader = true
	c.ResponseWriter.WriteHeader(code)
}

// Write counts the bytes written. We don't change the bytes
// the underlying writer sees — just count for the log line.
func (c *countingResponseWriter) Write(b []byte) (int, error) {
	if !c.wroteHeader {
		// Implicit WriteHeader(200) per net/http
		// contract. Match stdlib behavior.
		c.wroteHeader = true
	}
	n, err := c.ResponseWriter.Write(b)
	c.bytes += n
	return n, err
}
