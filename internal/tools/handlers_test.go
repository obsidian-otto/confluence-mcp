// Phase 7 — handler-level tests. The 5 handlers (HandleGet / Post /
// Put / Patch / Delete) are thin wrappers around executeRequest that
// decode JSON args into the per-method struct, optionally marshal the
// body, and forward to Phase 6's helper. safeHandler wraps any
// handler with a defer/recover that surfaces a non-leaking
// "internal error" message in place of the panic value.
//
// Test layout (per IMPLEMENTATION_PLAN.md Phase 7 — Tasks 1):
//
//   - TestHandleGet_HappyPath        decodes GetArgs, GET, no body
//   - TestHandlePost_HappyPath       decodes PostArgs, marshals body
//   - TestHandlePut_HappyPath        decodes PutArgs, marshals body
//   - TestHandlePatch_HappyPath      decodes PatchArgs, body is JSON-encoded array
//   - TestHandleDelete_HappyPath     decodes DeleteArgs, no body
//   - TestSafeHandler_PanicRecovery  replaces executeRequest with a panicker
//   - TestSafeHandler_DoesNotLeakPanicValue
//     panic message must NOT be in the
//     returned string (only the generic
//     "internal error" label)
//   - TestSafeHandler_PropagatesNonPanicError
//     non-panic errors flow through
//   - TestHandlers_BadJSONArgs       malformed JSON → error
//   - TestHandlers_PreservesEndpointContract
//     method, path, query, body all
//     forwarded to the upstream
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bennie/mcp-confluence/internal/atlassian"
)

// handlerRecorder wraps the existing requestRecorder from
// execute_test.go (same package — so we re-use the type) and lets a
// test substitute the *atlassian.Client with one whose Call can be
// overridden to panic or return a fabricated error. The happy-path
// tests use the real httptest recorder to exercise the full
// pipeline.
type handlerHarness struct {
	srv    *httptest.Server
	rec    *requestRecorder
	client *atlassian.Client
}

// newHandlerHarness wires a httptest server + a real atlassian
// Client pointed at it. The returned harness exposes the request
// recorder so tests can assert on the upstream call shape.
func newHandlerHarness(t *testing.T) *handlerHarness {
	t.Helper()
	srv, rec := newTestClient(t)
	_ = srv // newTestClient registers t.Cleanup(srv.Close)
	return &handlerHarness{
		srv:    nil,
		rec:    rec,
		client: testClient(t, srv),
	}
}

// TestHandleGet_HappyPath — decodes the args into GetArgs, no body,
// forwards to executeRequest with method=GET. The upstream returns
// a small JSON object; we assert the handler's returned string is
// non-empty and method/path/body match expectations.
func TestHandleGet_HappyPath(t *testing.T) {
	h := newHandlerHarness(t)
	h.rec.setResponse(http.StatusOK, `{"id":"space-1","key":"DEV"}`)

	raw := json.RawMessage(`{"path":"/wiki/api/v2/spaces/DEV","query":{}}`)
	got, err := HandleGet(context.Background(), h.client, raw)
	if err != nil {
		t.Fatalf("HandleGet: %v", err)
	}
	if got == "" {
		t.Fatal("HandleGet returned empty string")
	}
	if h.rec.method != "GET" {
		t.Errorf("method = %q, want GET", h.rec.method)
	}
	if h.rec.path != "/wiki/api/v2/spaces/DEV" {
		t.Errorf("path = %q, want /wiki/api/v2/spaces/DEV", h.rec.path)
	}
	if len(h.rec.body) != 0 {
		t.Errorf("GET should send no body, got %q", string(h.rec.body))
	}
}

// TestHandlePost_HappyPath — decodes into PostArgs, JSON-marshals
// args.Body, forwards method=POST + body to executeRequest.
func TestHandlePost_HappyPath(t *testing.T) {
	h := newHandlerHarness(t)
	h.rec.setResponse(http.StatusOK, `{"id":"page-1"}`)

	raw := json.RawMessage(`{
		"path":"/wiki/api/v2/pages",
		"body":{"spaceId":"123","title":"Hi","body":{"representation":"storage","value":"<p>x</p>"}}
	}`)
	got, err := HandlePost(context.Background(), h.client, raw)
	if err != nil {
		t.Fatalf("HandlePost: %v", err)
	}
	if got == "" {
		t.Fatal("HandlePost returned empty string")
	}
	if h.rec.method != "POST" {
		t.Errorf("method = %q, want POST", h.rec.method)
	}
	if h.rec.path != "/wiki/api/v2/pages" {
		t.Errorf("path = %q", h.rec.path)
	}
	// Body must be the JSON-encoded args.Body. We don't pin the
	// exact marshaling (key order, whitespace) — we just check
	// that the server saw a non-empty JSON body with the expected
	// fields.
	bodyStr := string(h.rec.body)
	if bodyStr == "" {
		t.Fatal("POST sent empty body")
	}
	var roundtrip map[string]any
	if err := json.Unmarshal(h.rec.body, &roundtrip); err != nil {
		t.Fatalf("body is not valid JSON: %v\nbody: %q", err, bodyStr)
	}
	if roundtrip["spaceId"] != "123" {
		t.Errorf("body.spaceId = %v, want 123", roundtrip["spaceId"])
	}
	if roundtrip["title"] != "Hi" {
		t.Errorf("body.title = %v, want Hi", roundtrip["title"])
	}
}

// TestHandlePut_HappyPath — same as POST but with method=PUT.
func TestHandlePut_HappyPath(t *testing.T) {
	h := newHandlerHarness(t)
	h.rec.setResponse(http.StatusOK, `{"id":"page-1"}`)

	raw := json.RawMessage(`{
		"path":"/wiki/api/v2/pages/1",
		"body":{"spaceId":"123","title":"Renamed","version":{"number":2}}
	}`)
	got, err := HandlePut(context.Background(), h.client, raw)
	if err != nil {
		t.Fatalf("HandlePut: %v", err)
	}
	if got == "" {
		t.Fatal("HandlePut returned empty string")
	}
	if h.rec.method != "PUT" {
		t.Errorf("method = %q, want PUT", h.rec.method)
	}
	if h.rec.path != "/wiki/api/v2/pages/1" {
		t.Errorf("path = %q", h.rec.path)
	}
	if len(h.rec.body) == 0 {
		t.Error("PUT should send a body, got empty")
	}
}

// TestHandlePatch_HappyPath — the spec calls out that PATCH takes a
// JSON array (RFC 6902-style ops). The handler MUST JSON-marshal the
// args.Body []map[string]any to a byte slice.
func TestHandlePatch_HappyPath(t *testing.T) {
	h := newHandlerHarness(t)
	h.rec.setResponse(http.StatusOK, `{"id":"page-1"}`)

	raw := json.RawMessage(`{
		"path":"/wiki/api/v2/pages/1",
		"body":[
			{"op":"replace","path":"/title","value":"Renamed"},
			{"op":"add","path":"/version","value":{"number":2}}
		]
	}`)
	got, err := HandlePatch(context.Background(), h.client, raw)
	if err != nil {
		t.Fatalf("HandlePatch: %v", err)
	}
	if got == "" {
		t.Fatal("HandlePatch returned empty string")
	}
	if h.rec.method != "PATCH" {
		t.Errorf("method = %q, want PATCH", h.rec.method)
	}
	// Body is a JSON array.
	bodyStr := string(h.rec.body)
	if !strings.HasPrefix(strings.TrimSpace(bodyStr), "[") {
		t.Errorf("PATCH body should be a JSON array, got: %q", bodyStr)
	}
	var ops []map[string]any
	if err := json.Unmarshal(h.rec.body, &ops); err != nil {
		t.Fatalf("PATCH body is not a valid JSON array: %v\nbody: %q", err, bodyStr)
	}
	if len(ops) != 2 {
		t.Errorf("PATCH body ops count = %d, want 2", len(ops))
	}
	if ops[0]["op"] != "replace" {
		t.Errorf("first op = %v, want replace", ops[0]["op"])
	}
}

// TestHandleDelete_HappyPath — same as GET: no body, DELETE method.
func TestHandleDelete_HappyPath(t *testing.T) {
	h := newHandlerHarness(t)
	// 204 No Content is the typical DELETE success status. The
	// httptest recorder's handler returns 200 by default but with
	// no body; the JSON-decoded Call returns an empty map which the
	// executeRequest pipeline encodes successfully.
	h.rec.setResponse(http.StatusOK, ``)

	raw := json.RawMessage(`{"path":"/wiki/api/v2/pages/1"}`)
	got, err := HandleDelete(context.Background(), h.client, raw)
	if err != nil {
		t.Fatalf("HandleDelete: %v", err)
	}
	// got may be empty (zero-length TOON output of an empty map);
	// the contract is "no error, method=DELETE, path correct".
	if h.rec.method != "DELETE" {
		t.Errorf("method = %q, want DELETE", h.rec.method)
	}
	if h.rec.path != "/wiki/api/v2/pages/1" {
		t.Errorf("path = %q", h.rec.path)
	}
	if len(h.rec.body) != 0 {
		t.Errorf("DELETE should send no body, got %q", string(h.rec.body))
	}
	_ = got
}

// TestSafeHandler_PanicRecovery — the killer test for Phase 7:
// replace the inner handler with a function that panics. The
// safeHandler wrapper MUST catch the panic and return a
// non-nil error of a known shape WITHOUT crashing the goroutine.
//
// We do this without going through executeRequest at all — the
// safeHandler contract is independent of what the inner handler
// does; any panic in the inner handler is caught. That keeps this
// test focused on the safeHandler itself, not on the underlying
// pipeline. (Phase 6's tests already exercise the pipeline.)
func TestSafeHandler_PanicRecovery(t *testing.T) {
	const phaseName = "test-phase-panic"
	panicValue := "simulated handler explosion with secret: AKIA1234567890ABCDEF"

	wrapped := safeHandler(phaseName, func(ctx context.Context, args json.RawMessage) (string, error) {
		panic(panicValue)
	})

	got, err := wrapped(context.Background(), json.RawMessage(`{}`))
	if err == nil {
		t.Fatalf("safeHandler returned no error for a panic; got result %q", got)
	}
	// The error message must be present (this is what the LLM
	// sees) and MUST start with the canonical "internal error"
	// prefix so the LLM recognizes it as a safe-mode response.
	if !strings.HasPrefix(err.Error(), "internal error") {
		t.Errorf("error message = %q, want it to start with %q", err.Error(), "internal error")
	}
	// The returned string MUST be empty — the panic yielded no
	// usable result.
	if got != "" {
		t.Errorf("safeHandler returned a non-empty string %q for a panic; want \"\"", got)
	}
}

// TestSafeHandler_DoesNotLeakPanicValue — the kickoff prompt's hard
// constraint: the panic message must NOT appear in the returned
// error. The panic in the previous test contained the literal
// string "AKIA..."; we assert it is NOT in err.Error().
//
// This is the spec's "non-leaking" contract: the LLM should never
// see the panic value, stack trace contents, or any panic-time data.
func TestSafeHandler_DoesNotLeakPanicValue(t *testing.T) {
	// Embed an obviously-fake "secret" value in the panic. If the
	// safeHandler surfaces it, the test fails.
	panicValue := "PANIC-SECRET-DO-NOT-LEAK-AKIA9999"

	wrapped := safeHandler("test-leak-check", func(ctx context.Context, args json.RawMessage) (string, error) {
		panic(panicValue)
	})

	_, err := wrapped(context.Background(), json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("safeHandler returned no error for a panic")
	}
	if strings.Contains(err.Error(), panicValue) {
		t.Errorf("safeHandler leaked panic value: %q", err.Error())
	}
	if strings.Contains(err.Error(), "AKIA9999") {
		t.Errorf("safeHandler leaked panic secret substr: %q", err.Error())
	}
}

// TestSafeHandler_PropagatesNonPanicError — non-panic errors from
// the inner handler MUST flow through safeHandler unchanged (no
// double-wrapping, no swallowing).
func TestSafeHandler_PropagatesNonPanicError(t *testing.T) {
	innerErr := errors.New("regular not-a-panic error")
	wrapped := safeHandler("test-error", func(ctx context.Context, args json.RawMessage) (string, error) {
		return "ok-output", innerErr
	})

	out, err := wrapped(context.Background(), json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("safeHandler swallowed the inner error")
	}
	if err.Error() != innerErr.Error() {
		t.Errorf("safeHandler wrapped the error: got %q, want %q", err.Error(), innerErr.Error())
	}
	if out != "ok-output" {
		t.Errorf("safeHandler dropped the output: got %q, want %q", out, "ok-output")
	}
}

// TestSafeHandler_HappyPath_NoPanic — the wrapper is a no-op when
// the inner handler doesn't panic. Useful regression test to ensure
// safeHandler doesn't accidentally double-wrap a successful result.
func TestSafeHandler_HappyPath_NoPanic(t *testing.T) {
	wrapped := safeHandler("test-happy", func(ctx context.Context, args json.RawMessage) (string, error) {
		return "success", nil
	})

	got, err := wrapped(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("safeHandler returned an error for a non-panicking handler: %v", err)
	}
	if got != "success" {
		t.Errorf("safeHandler altered the result: got %q, want %q", got, "success")
	}
}

// TestSafeHandler_NonStringPanicValue — panic values in Go can be
// of any type. The wrapper must handle non-string panics (e.g. a
// struct) without crashing. The generic "internal error" message
// is still produced.
func TestSafeHandler_NonStringPanicValue(t *testing.T) {
	type panicStruct struct {
		Code int
		Tag  string
	}
	panicValue := panicStruct{Code: 42, Tag: "deep-failure"}

	wrapped := safeHandler("test-nonstring", func(ctx context.Context, args json.RawMessage) (string, error) {
		panic(panicValue)
	})

	_, err := wrapped(context.Background(), json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("safeHandler returned no error for a non-string panic")
	}
	if !strings.HasPrefix(err.Error(), "internal error") {
		t.Errorf("error message = %q, want it to start with %q", err.Error(), "internal error")
	}
	// Defensive: even with a struct panic, no panic-time data
	// ("Code 42", "Tag deep-failure") should leak.
	if strings.Contains(err.Error(), "42") || strings.Contains(err.Error(), "deep-failure") {
		t.Errorf("safeHandler leaked non-string panic contents: %q", err.Error())
	}
}

// TestHandlers_BadJSONArgs — when a caller sends malformed JSON
// in the args, the handler must return an error rather than panic.
// The safeHandler wrapper is the second line of defense; the
// handler itself should fail cleanly at json.Unmarshal.
func TestHandlers_BadJSONArgs(t *testing.T) {
	h := newHandlerHarness(t)

	tests := []struct {
		name    string
		handler func(context.Context, *atlassian.Client, json.RawMessage) (string, error)
		raw     string
	}{
		{"Get", HandleGet, `{"path": /missing-quotes}`},
		{"Post", HandlePost, `{"path": /missing-quotes}`},
		{"Put", HandlePut, `{`},
		{"Patch", HandlePatch, `not even close to json`},
		{"Delete", HandleDelete, `{"path": "x"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := tt.handler(context.Background(), h.client, json.RawMessage(tt.raw))
			if err == nil {
				t.Fatalf("%s: bad JSON returned no error; got result %q", tt.name, out)
			}
			if out != "" {
				t.Errorf("%s: bad JSON returned non-empty result %q", tt.name, out)
			}
		})
	}
}

// TestHandlers_UnknownFieldsSilentlyIgnored — the args structs
// don't forbid unknown JSON fields, so extra keys from the LLM are
// silently ignored rather than causing an error. This is the
// permissive behavior the upstream relies on.
func TestHandlers_UnknownFieldsSilentlyIgnored(t *testing.T) {
	h := newHandlerHarness(t)
	h.rec.setResponse(http.StatusOK, `{"ok":true}`)

	raw := json.RawMessage(`{
		"path":"/x",
		"unknownField":"ignored",
		"extraNested":{"a":1}
	}`)
	if _, err := HandleGet(context.Background(), h.client, raw); err != nil {
		t.Errorf("unknown fields caused an error: %v", err)
	}
}

// TestHandlers_PreservesEndpointContract — the wrapped handlers
// must preserve the per-method args' fields verbatim. The upstream
// sees the exact path, query, body, jq, and outputFormat that the
// LLM sent.
func TestHandlers_PreservesEndpointContract(t *testing.T) {
	h := newHandlerHarness(t)
	h.rec.setResponse(http.StatusOK, `{"results":[{"id":"1"}]}`)

	raw := json.RawMessage(`{
		"path":"/wiki/api/v2/spaces",
		"query":{"limit":"3","cursor":"CURSOR-XYZ"},
		"jq":"results[*].id",
		"outputFormat":"json"
	}`)
	if _, err := HandleGet(context.Background(), h.client, raw); err != nil {
		t.Fatalf("HandleGet: %v", err)
	}
	// The query must have made it through.
	if !strings.Contains(h.rec.rawQuery, "limit=3") {
		t.Errorf("limit=3 missing from upstream query: %q", h.rec.rawQuery)
	}
	if !strings.Contains(h.rec.rawQuery, "cursor=CURSOR-XYZ") {
		t.Errorf("cursor=CURSOR-XYZ missing from upstream query: %q", h.rec.rawQuery)
	}
}

// TestSafeHandler_NilArgs — defensive: even if a caller passes a
// nil/empty raw JSON message, the inner handler is invoked with
// those bytes. safeHandler MUST still recover panics from such
// edge cases.
func TestSafeHandler_NilArgs(t *testing.T) {
	wrapped := safeHandler("nil-args", func(ctx context.Context, args json.RawMessage) (string, error) {
		panic(fmt.Sprintf("panicked with args of length %d", len(args)))
	})
	_, err := wrapped(context.Background(), nil)
	if err == nil {
		t.Fatal("safeHandler returned no error for a panic with nil args")
	}
	if !strings.HasPrefix(err.Error(), "internal error") {
		t.Errorf("error message = %q", err.Error())
	}
}
