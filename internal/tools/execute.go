// Package tools — execute.go: the 9-step shared handler used by all
// five MCP tools (conf_get / conf_post / conf_put / conf_patch /
// conf_delete). The handler is implemented as executeRequest below
// and is called by per-method wrappers in Phase 7.
//
// The 9 steps (per IMPLEMENTATION_PLAN.md §Phase 6):
//
//  1. parse args (must accept one of Get/Post/Put/Patch/DeleteArgs;
//     use a discriminator interface)
//  2. set query params on the URL  (done inside atlassian.Client.Do)
//  3. call c.Call(ctx, method, path, query, body)
//  4. on 4xx/5xx, return *APIError formatted as
//     "<METHOD> <path>: <status> <text> - <body>" (2000-char body truncate)
//  5. on 200: decode JSON body into map[string]any (already provided by c.Call)
//  6. on response > 40000 chars: truncate to 40000 chars, append a notice
//     "<truncated>Full response saved to /tmp/mcp/<session-id>.json</truncated>"
//  7. if args.JQ is non-empty: apply jmespath.Apply(args.JQ, decodedData)
//     — short-circuits internally
//  8. if args.OutputFormat == "json": use encoding/json.Marshal; otherwise
//     use toon.Marshal
//  9. return the bytes as string
//
// The function does NOT log the API token at any point. The token lives
// only in the Authorization header inside atlassian.Client.Do; the
// data path (response body → JMESPath → encoder) never touches it.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/bennie/mcp-confluence/internal/atlassian"
	"github.com/bennie/mcp-confluence/internal/jmespath"
	"github.com/bennie/mcp-confluence/internal/toon"
)

// truncationThreshold is the encoded response size above which
// executeRequest writes the full body to /tmp/mcp/<session>.json and
// truncates the in-flight response. The value matches the upstream
// @aashari/mcp-server-atlassian-confluence README's "Large Response
// Truncation" threshold (≈ 10k tokens / 40k chars).
const truncationThreshold = 40000

// rawResponseDir is where full responses are written when the
// encoded body exceeds truncationThreshold. Matches the spec at
// specs/02-upstream-aashari/03-lessons-and-quirks.md §40k truncation.
const rawResponseDir = "/tmp/mcp"

// reqArgs is the unexported discriminator interface satisfied by all
// five arg types (GetArgs / PostArgs / PutArgs / PatchArgs /
// DeleteArgs). It exposes the four fields the 9-step pipeline reads:
// Path, Query, JQ, OutputFormat.
//
// Phase 7's per-method handlers are the only callers of
// executeRequest; they pass a typed args value (e.g. GetArgs{}) which
// is implicitly converted to reqArgs at the call site. This keeps the
// executeRequest signature stable while letting the five handlers
// preserve their per-method arg types (and the per-method jsonschema
// the upstream demands).
type reqArgs interface {
	// Path is the API path (e.g. "/wiki/api/v2/spaces"). The
	// atlassian.Client.Do helper validates the leading slash.
	GetPath() string
	// GetQuery returns the optional query-string parameters.
	// Empty-valued entries are dropped by atlassian.Client.Do.
	GetQuery() map[string]string
	// GetJQ returns the optional JMESPath expression. Empty
	// short-circuits the filter step (Phase 4 contract).
	GetJQ() string
	// GetOutputFormat returns "json" for raw-JSON output, "" for
	// the TOON default. Any other value is treated as TOON —
	// matches the upstream's tolerant behavior.
	GetOutputFormat() string
}

// compile-time assertions: each of the five args types must satisfy
// reqArgs. If a new field is added to args.go (e.g. a "Headers" map)
// that the 9-step pipeline must read, the interface must be extended
// here and the implementations below must grow a matching accessor.
var (
	_ reqArgs = GetArgs{}
	_ reqArgs = PostArgs{}
	_ reqArgs = PutArgs{}
	_ reqArgs = PatchArgs{}
	_ reqArgs = DeleteArgs{}
)

// GetPath / GetQuery / GetJQ / GetOutputFormat — interface adapters
// for the five args types. The adapters are deliberately small and
// explicit (rather than a single struct with embedded methods) so
// each args type remains a plain data carrier visible in args.go.

// GetPath returns args.Path (the API path with leading slash).
func (a GetArgs) GetPath() string             { return a.Path }
func (a GetArgs) GetQuery() map[string]string { return a.Query }
func (a GetArgs) GetJQ() string               { return a.JQ }
func (a GetArgs) GetOutputFormat() string     { return a.OutputFormat }

// PostArgs satisfies reqArgs. The Body field is intentionally NOT in
// the interface — Phase 7's handler is responsible for marshalling
// args.Body to JSON bytes and passing them as the body parameter.
func (a PostArgs) GetPath() string             { return a.Path }
func (a PostArgs) GetQuery() map[string]string { return a.Query }
func (a PostArgs) GetJQ() string               { return a.JQ }
func (a PostArgs) GetOutputFormat() string     { return a.OutputFormat }

// PutArgs satisfies reqArgs. Same caveat as PostArgs regarding Body.
func (a PutArgs) GetPath() string             { return a.Path }
func (a PutArgs) GetQuery() map[string]string { return a.Query }
func (a PutArgs) GetJQ() string               { return a.JQ }
func (a PutArgs) GetOutputFormat() string     { return a.OutputFormat }

// PatchArgs satisfies reqArgs. Body is a []map[string]any (RFC 6902
// patch operations); the handler marshals it before calling Do.
func (a PatchArgs) GetPath() string             { return a.Path }
func (a PatchArgs) GetQuery() map[string]string { return a.Query }
func (a PatchArgs) GetJQ() string               { return a.JQ }
func (a PatchArgs) GetOutputFormat() string     { return a.OutputFormat }

// DeleteArgs satisfies reqArgs. No body.
func (a DeleteArgs) GetPath() string             { return a.Path }
func (a DeleteArgs) GetQuery() map[string]string { return a.Query }
func (a DeleteArgs) GetJQ() string               { return a.JQ }
func (a DeleteArgs) GetOutputFormat() string     { return a.OutputFormat }

// executeRequest is the 9-step shared handler called by all five
// MCP tools. The body argument is opaque bytes — the caller (Phase 7
// handler) is responsible for JSON-encoding args.Body before calling
// executeRequest. For methods without a body (GET, DELETE), pass nil.
//
// The function is the single point where:
//
//   - the upstream call is made (step 3, via atlassian.Client.Call)
//   - 4xx/5xx responses are surfaced as *APIError with the literal
//     "<METHOD> <path>: <status> <text> - <body>" shape (step 4)
//   - the response is optionally filtered by JMESPath (step 7)
//   - the response is optionally truncated to 40k chars (step 6)
//   - the response is encoded as TOON or JSON (step 8)
//
// On success, executeRequest returns the encoded body as a string
// ready for the MCP tool response. On any error, it returns a
// non-nil error with the message format required by
// specs/09-anti-patterns/03-error-shapes.md.
//
// IMPORTANT: executeRequest does NOT log the API token at any point.
// The token never enters this function's scope; it is held by the
// atlassian.Client and used only to set the Authorization header.
func executeRequest(
	ctx context.Context,
	c *atlassian.Client,
	args reqArgs,
	method string,
	body []byte,
) (string, error) {
	// Step 1 — args is already parsed at the call site (the handler
	// unmarshaled JSON into a typed args struct). The discriminator
	// interface gives us uniform access to the four fields the
	// pipeline reads: path, query, jq, outputFormat.

	// Step 2 — query params are passed through to atlassian.Client.Call,
	// which composes them into the URL via url.Values (see
	// internal/atlassian/client.go:buildURL).

	// Step 3 — make the HTTP call. On success, decoded is the JSON
	// body as map[string]any (or empty map for 204 No Content). On
	// 4xx/5xx, err is *APIError; on network failure, err is a
	// "method path: network error: ..." error; on bad JSON, err is
	// a "method path: N NN: invalid JSON response: ..." error.
	decoded, err := c.Call(ctx, method, args.GetPath(), args.GetQuery(), body)
	if err != nil {
		// Step 4 — surface 4xx/5xx as the spec-mandated error shape.
		// atlassian.Client.Call returns a *APIError on 4xx/5xx whose
		// Error() method already produces the literal
		// "<METHOD> <path>: <status> <text> - <body>" format with
		// the 2000-char body truncate (see internal/atlassian/errors.go).
		// We pass it through unchanged for Class 1 errors. Network
		// (Class 2) and invalid-JSON (Class 3) errors come pre-formatted
		// from atlassian.Client.Call too.
		//
		// If a *APIError is returned, we also return it directly so
		// the Phase 7 handler can errors.As(err, &atlassian.APIError{})
		// if it wants to log/inspect structured fields. The error
		// message is identical via the Error() method.
		var apiErr *atlassian.APIError
		if errors.As(err, &apiErr) {
			return "", apiErr
		}
		return "", err
	}

	// Step 5 — decoded is map[string]any. (c.Call already JSON-decoded.)
	//
	// data is the `any` shape the encoder consumes. We use a
	// dedicated variable (not the concrete map) so step 7's filter
	// can replace it with any shape — list, scalar, nil — without
	// a type assertion back to map[string]any. The encoder handles
	// every Go type we care about (map[string]any, []any, string,
	// float64, bool, nil — see internal/toon/encode.go:normalize).
	data := any(decoded)

	// Step 7 — apply JMESPath filter if requested. Apply() short-circuits
	// on an empty expression (Phase 4 contract).
	if args.GetJQ() != "" {
		filtered, ferr := jmespath.Apply(args.GetJQ(), data)
		if ferr != nil {
			// Class 4 error per specs/09-anti-patterns/03-error-shapes.md:
			// "<METHOD> <path>: jq filter error: <jmespath-error>".
			return "", fmt.Errorf("%s %s: jq filter error: %v",
				method, args.GetPath(), ferr)
		}
		data = filtered
	}

	// Step 8 — encode the (possibly filtered) data. The default is
	// TOON; explicit args.OutputFormat == "json" switches to raw JSON.
	encoded, eerr := encodeOutput(data, args.GetOutputFormat())
	if eerr != nil {
		// Class 5 error per specs/09-anti-patterns/03-error-shapes.md.
		return "", fmt.Errorf("%s %s: encode error: %v",
			method, args.GetPath(), eerr)
	}

	// Step 6 — if the encoded body exceeds 40k chars, truncate it
	// and append a pointer to the full response on disk.
	final, ferr := truncateForAI(encoded, method, args.GetPath())
	if ferr != nil {
		// truncateForAI's own I/O errors are not fatal: the LLM still
		// gets the truncated (but usable) output. We log to stderr
		// and proceed with the in-memory truncation.
		fmt.Fprintf(os.Stderr,
			"tools: failed to persist full response: %v\n", ferr)
	}

	// Step 9 — return the (possibly truncated) bytes as a string.
	return final, nil
}

// encodeOutput marshals v to TOON by default; when format == "json"
// it uses encoding/json. The result is always a byte slice; the
// caller is responsible for any further stringification.
//
// The function tolerates unknown format values by falling back to
// TOON — this matches the upstream's tolerant behavior (the upstream
// README documents "toon" and "json" but the code does not panic on
// a typo).
func encodeOutput(v any, format string) ([]byte, error) {
	switch format {
	case "json":
		return json.Marshal(v)
	case "", "toon":
		return toon.Marshal(v)
	default:
		// Unknown format → TOON (matches upstream tolerance).
		return toon.Marshal(v)
	}
}

// truncateForAI implements the upstream's 40k truncation behavior.
// When encoded exceeds truncationThreshold, the function:
//
//  1. Writes the full encoded body to /tmp/mcp/<session>.json so
//     the operator (or the LLM on a follow-up call) can read the
//     complete response from disk.
//  2. Truncates the in-memory encoded body to fit (the threshold
//     is hard, not a soft cap — anything over 40k chars is cut).
//  3. Appends a notice pointing at the saved file.
//
// The session id is the process PID + the current nanosecond
// timestamp per the spec's "PID + boot-time nanosecond timestamp"
// note in specs/02-upstream-aashari/03-lessons-and-quirks.md. There
// is one binary instance at a time per Hermes subprocess, so we
// don't need a real UUID — PID + nanos is unique enough.
//
// The /tmp/mcp/ directory is created if missing. A failure to
// create the directory or write the file is returned to the caller
// (which logs to stderr and proceeds with the in-memory truncation).
func truncateForAI(encoded []byte, method, path string) (string, error) {
	if len(encoded) <= truncationThreshold {
		// Under the threshold — pass through verbatim. The empty
		// "notice" branch is the common case.
		return string(encoded), nil
	}

	// Over the threshold. Build the session id and the full path.
	sessionID := newSessionID()
	filename := sessionID + ".json"
	fullPath := filepath.Join(rawResponseDir, filename)

	// Best-effort create the directory. If this fails, the caller
	// logs to stderr and proceeds with the in-memory truncation.
	if err := os.MkdirAll(rawResponseDir, 0o755); err != nil {
		return truncateWithNotice(encoded, fullPath), err
	}

	// Write the full response. Mode 0600 — the response may contain
	// sensitive user data (page bodies, comments, etc.) and the
	// file is in /tmp which is world-readable on some hosts.
	if err := os.WriteFile(fullPath, encoded, 0o600); err != nil {
		return truncateWithNotice(encoded, fullPath), err
	}

	return truncateWithNotice(encoded, fullPath), nil
}

// truncateWithNotice cuts encoded to truncationThreshold and appends
// the pointer-to-full-response notice. The notice shape is
// "<truncated>Full response saved to /tmp/mcp/<session-id>.json</truncated>"
// per the implementation plan; the full upstream-spec shape is
// documented in specs/02-upstream-aashari/03-lessons-and-quirks.md
// and reproduced below for traceability:
//
//	[truncated 40,123 / 80,456 chars — full response at /tmp/mcp/<session-id>.json]
//
// The implementation-plan wording is the one we use here, because
// the plan's wording is the contract Phase 6 was specified against.
// The method/path is included so the operator can correlate the
// truncated output with the original API call.
func truncateWithNotice(encoded []byte, fullPath string) string {
	const (
		noticeOpen  = "<truncated>Full response saved to "
		noticeClose = "</truncated>"
	)
	cut := truncationThreshold
	truncated := encoded[:cut]
	// The notice is appended AFTER the cut payload, not as part of
	// it. The MCP tool response text becomes "<first 40k chars of
	// encoded><notice>". Phase 7 wraps the whole string in a
	// TextContent for the LLM.
	notice := noticeOpen + fullPath + noticeClose
	// Build a single string with no intermediate allocations.
	out := make([]byte, 0, len(truncated)+len(notice))
	out = append(out, truncated...)
	out = append(out, notice...)
	return string(out)
}

// newSessionID returns a process-unique identifier for a single
// executeRequest call. The shape is "<pid>-<nanos>" — short, sortable
// by time, and unique per process invocation. The spec calls for
// "PID + boot-time nanosecond timestamp" but at run time we don't
// have a clean "boot time" — we use the call-time nanos instead,
// which is monotonic per-process and unique enough for the
// /tmp/mcp/ directory. This matches the spirit of the spec note.
func newSessionID() string {
	pid := os.Getpid()
	nanos := time.Now().UnixNano()
	return strconv.Itoa(pid) + "-" + strconv.FormatInt(nanos, 10)
}
