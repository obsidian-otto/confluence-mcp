// Phase 6 — executeRequest tests.
//
// These tests pin the 9-step shared handler from the implementation plan:
//
//  1. parse args (must accept one of Get/Post/Put/Patch/DeleteArgs;
//     use a discriminator interface)
//  2. set query params on the URL
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
// The tests use a real *atlassian.Client (with HTTPClient pointed at an
// httptest.Server) and a counter-instrumented jmespath parser, following
// the patterns established by the existing Phase 2 / Phase 4 tests.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	gj "github.com/jmespath/go-jmespath"

	"github.com/bennie/mcp-confluence/internal/atlassian"
	"github.com/bennie/mcp-confluence/internal/config"
	mjp "github.com/bennie/mcp-confluence/internal/jmespath"
)

// newServer builds an httptest server + a Client pointed at it. The returned
// recorder lets each test set the response status/body before invoking
// executeRequest.
func newTestClient(t *testing.T) (*httptest.Server, *requestRecorder) {
	t.Helper()
	rec := &requestRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		w.Header().Set("Content-Type", "application/json")
		if rec.respStatus != 0 {
			w.WriteHeader(rec.respStatus)
		} else {
			w.WriteHeader(http.StatusOK)
		}
		if rec.respBody != nil {
			_, _ = w.Write(rec.respBody)
		} else {
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

type requestRecorder struct {
	method     string
	path       string
	rawQuery   string
	body       []byte
	respStatus int
	respBody   []byte
}

func (r *requestRecorder) record(req *http.Request) {
	r.method = req.Method
	r.path = req.URL.Path
	r.rawQuery = req.URL.RawQuery
	if req.Body != nil {
		// Read all of the body so callers can assert on it. Closing
		// the body is the stdlib http package's job (httptest server
		// does it for us after the handler returns).
		buf := make([]byte, 0, 256)
		tmp := make([]byte, 256)
		for {
			n, err := req.Body.Read(tmp)
			if n > 0 {
				buf = append(buf, tmp[:n]...)
			}
			if err != nil {
				break
			}
		}
		r.body = buf
	}
}

func (r *requestRecorder) setResponse(status int, body string) {
	r.respStatus = status
	r.respBody = []byte(body)
}

// withCountingParser swaps the package's jmespath parser fn with a counter,
// runs fn, then restores the original. This mirrors the pattern from
// internal/jmespath/apply_test.go:Phase 4, lifted here so the empty-jq
// short-circuit can be verified from the executeRequest call site.
//
// The counter wraps the real upstream parser directly (gj.Search). The
// empty-expr short-circuit in jmespath.Apply happens *before* the
// package-level parser fn is invoked, so a count of 0 for an empty JQ
// proves the short-circuit works at the integration boundary.
func withCountingParser(t *testing.T) (counter func() int32, restore func()) {
	t.Helper()
	var n int32
	prev := mjp.SwapParser(func(expr string, data any) (any, error) {
		atomic.AddInt32(&n, 1)
		return gj.Search(expr, data)
	})
	return func() int32 { return atomic.LoadInt32(&n) },
		func() { mjp.SwapParser(prev) }
}

// TestExecuteRequest_HappyPath_TOONDefault — the most common case: 200 OK
// with a JSON body, no jq filter, no outputFormat. The response is
// TOON-encoded (the project's default per the spec).
func TestExecuteRequest_HappyPath_TOONDefault(t *testing.T) {
	srv, rec := newTestClient(t)
	rec.setResponse(http.StatusOK, `{"results":[{"id":"1","name":"a"}],"_links":{}}`)

	c := testClient(t, srv)

	got, err := executeRequest(context.Background(), c, GetArgs{Path: "/wiki/api/v2/spaces"}, "GET", nil)
	if err != nil {
		t.Fatalf("executeRequest: %v", err)
	}
	if got == "" {
		t.Fatal("executeRequest returned empty string")
	}
	// TOON output of a flat object with an array of objects: it must
	// mention the keys "results" and "_links" in some form. We don't
	// pin the exact byte layout (the TOON encoder may evolve), but
	// we do assert it's NOT raw JSON.
	if strings.HasPrefix(strings.TrimSpace(got), "{") {
		t.Errorf("default output should be TOON, not JSON: %q", firstN(got, 80))
	}
	if !strings.Contains(got, "results") {
		t.Errorf("TOON output missing 'results' key: %q", firstN(got, 200))
	}
	if rec.method != "GET" {
		t.Errorf("method = %q, want GET", rec.method)
	}
	if rec.path != "/wiki/api/v2/spaces" {
		t.Errorf("path = %q", rec.path)
	}
}

// TestExecuteRequest_HappyPath_WithJQ — the 7th step applies a JMESPath
// expression and re-encodes the filtered result as TOON.
func TestExecuteRequest_HappyPath_WithJQ(t *testing.T) {
	srv, rec := newTestClient(t)
	rec.setResponse(http.StatusOK, `{"results":[{"id":"1","name":"a"},{"id":"2","name":"b"}]}`)

	c := testClient(t, srv)
	counter, restore := withCountingParser(t)
	defer restore()

	got, err := executeRequest(context.Background(), c,
		GetArgs{Path: "/wiki/api/v2/spaces", JQ: "results[*].id"},
		"GET", nil)
	if err != nil {
		t.Fatalf("executeRequest: %v", err)
	}
	// The TOON output of ["1","2"] should contain both ids.
	if !strings.Contains(got, "1") || !strings.Contains(got, "2") {
		t.Errorf("jq-filtered output missing ids: %q", got)
	}
	// Should NOT contain the names (they were filtered out).
	if strings.Contains(got, "alpha-name") || strings.Contains(got, "name:") {
		t.Errorf("jq filter did not strip names: %q", got)
	}
	// The parser must have been invoked exactly once (for the non-empty
	// expr). Not 0 (no filter) and not 2+ (recursive/repeated).
	if got := counter(); got != 1 {
		t.Errorf("jmespath parser invoked %d times, want 1", got)
	}
}

// TestExecuteRequest_HappyPath_OutputFormatJSON — when the caller
// explicitly asks for JSON, the output is encoding/json of the
// (possibly jq-filtered) data.
func TestExecuteRequest_HappyPath_OutputFormatJSON(t *testing.T) {
	srv, rec := newTestClient(t)
	rec.setResponse(http.StatusOK, `{"results":[{"id":"1","name":"a"}]}`)

	c := testClient(t, srv)

	got, err := executeRequest(context.Background(), c,
		GetArgs{Path: "/wiki/api/v2/spaces", OutputFormat: "json"},
		"GET", nil)
	if err != nil {
		t.Fatalf("executeRequest: %v", err)
	}
	// JSON output: must be parseable back into the original shape.
	var roundtrip map[string]any
	if err := json.Unmarshal([]byte(got), &roundtrip); err != nil {
		t.Fatalf("outputFormat=json but result is not valid JSON: %v\nbody: %q", err, firstN(got, 200))
	}
	res, ok := roundtrip["results"].([]any)
	if !ok || len(res) != 1 {
		t.Fatalf("JSON roundtrip lost structure: %+v", roundtrip)
	}
	row := res[0].(map[string]any)
	if row["id"] != "1" || row["name"] != "a" {
		t.Errorf("JSON row wrong: %+v", row)
	}
}

// TestExecuteRequest_EmptyJQ_ShortCircuits — the 7th step must NOT invoke
// the jmespath parser when args.JQ is empty (Phase 4 short-circuit).
// We verify this with a counter; if executeRequest called the parser,
// the counter would be > 0.
func TestExecuteRequest_EmptyJQ_ShortCircuits(t *testing.T) {
	srv, rec := newTestClient(t)
	rec.setResponse(http.StatusOK, `{"results":[]}`)

	c := testClient(t, srv)
	counter, restore := withCountingParser(t)
	defer restore()

	if _, err := executeRequest(context.Background(), c,
		GetArgs{Path: "/wiki/api/v2/spaces", JQ: ""},
		"GET", nil); err != nil {
		t.Fatalf("executeRequest: %v", err)
	}
	if n := counter(); n != 0 {
		t.Errorf("jmespath parser invoked %d times for empty JQ; want 0 (short-circuit)", n)
	}
}

// TestExecuteRequest_OutputFormatEmpty_DefaultsToTOON — the 8th step's
// default branch: args.OutputFormat == "" → TOON. (Same coverage as the
// HappyPath test, but the assertion is on the *absence* of JSON syntax
// in the output and the *presence* of TOON-marker keys.)
func TestExecuteRequest_OutputFormatEmpty_DefaultsToTOON(t *testing.T) {
	srv, rec := newTestClient(t)
	rec.setResponse(http.StatusOK, `{"results":[{"id":"1","name":"a"}]}`)

	c := testClient(t, srv)

	got, err := executeRequest(context.Background(), c,
		GetArgs{Path: "/wiki/api/v2/spaces", OutputFormat: ""},
		"GET", nil)
	if err != nil {
		t.Fatalf("executeRequest: %v", err)
	}
	// TOON output of a non-empty object: at least one `key: value` line.
	if !strings.Contains(got, ":") {
		t.Errorf("default output should be TOON (key: value lines), got: %q", firstN(got, 200))
	}
	// And it must NOT be a JSON object literal.
	if strings.HasPrefix(strings.TrimSpace(got), "{") {
		t.Errorf("default output should be TOON, not JSON: %q", firstN(got, 80))
	}
}

// TestExecuteRequest_APIError_401 — the 4th step wraps a 401 into the
// literal "<METHOD> <path>: <status> <text> - <body>" shape from
// specs/09-anti-patterns/03-error-shapes.md.
func TestExecuteRequest_APIError_401(t *testing.T) {
	srv, rec := newTestClient(t)
	rec.setResponse(http.StatusUnauthorized, `{"code":"AUTHENTICATION_FAILED","message":"Authentication failed"}`)

	c := testClient(t, srv)

	_, err := executeRequest(context.Background(), c, GetArgs{Path: "/wiki/api/v2/spaces"}, "GET", nil)
	if err == nil {
		t.Fatal("executeRequest expected error for 401, got nil")
	}
	want := `GET /wiki/api/v2/spaces: 401 Unauthorized - {"code":"AUTHENTICATION_FAILED","message":"Authentication failed"}`
	if err.Error() != want {
		t.Errorf("error shape mismatch\n  got:  %q\n  want: %q", err.Error(), want)
	}
}

// TestExecuteRequest_APIError_404 — same shape, different status.
func TestExecuteRequest_APIError_404(t *testing.T) {
	srv, rec := newTestClient(t)
	rec.setResponse(http.StatusNotFound, `{"code":"NOT_FOUND","message":"Page not found"}`)

	c := testClient(t, srv)

	_, err := executeRequest(context.Background(), c, GetArgs{Path: "/wiki/api/v2/pages/999"}, "GET", nil)
	if err == nil {
		t.Fatal("executeRequest expected error for 404, got nil")
	}
	want := `GET /wiki/api/v2/pages/999: 404 Not Found - {"code":"NOT_FOUND","message":"Page not found"}`
	if err.Error() != want {
		t.Errorf("error shape mismatch\n  got:  %q\n  want: %q", err.Error(), want)
	}
}

// TestExecuteRequest_APIError_500 — same shape, server error.
func TestExecuteRequest_APIError_500(t *testing.T) {
	srv, rec := newTestClient(t)
	rec.setResponse(http.StatusInternalServerError, `{"message":"boom"}`)

	c := testClient(t, srv)

	_, err := executeRequest(context.Background(), c, GetArgs{Path: "/wiki/api/v2/spaces"}, "GET", nil)
	if err == nil {
		t.Fatal("executeRequest expected error for 500, got nil")
	}
	want := `GET /wiki/api/v2/spaces: 500 Internal Server Error - {"message":"boom"}`
	if err.Error() != want {
		t.Errorf("error shape mismatch\n  got:  %q\n  want: %q", err.Error(), want)
	}
}

// TestExecuteRequest_Truncation_LargeResponse — the 6th step: when the
// encoded response exceeds 40000 chars, executeRequest truncates and
// appends a "<truncated>Full response saved to /tmp/mcp/<session-id>.json</truncated>"
// marker. The full output is also written to disk so the operator can
// inspect it. The test cleans up the file it created.
func TestExecuteRequest_Truncation_LargeResponse(t *testing.T) {
	// Build a JSON response whose TOON encoding comfortably exceeds
	// 40k chars. 1000 objects with a long string field is enough.
	rows := make([]string, 0, 1000)
	for i := 0; i < 1000; i++ {
		rows = append(rows, fmt.Sprintf(`{"id":"%d","name":"%s"}`, i, strings.Repeat("x", 80)))
	}
	body := `{"results":[` + strings.Join(rows, ",") + `]}`
	if len(body) < 40000 {
		t.Fatalf("test fixture body too small (%d bytes) — bump row count or string length", len(body))
	}
	srv, rec := newTestClient(t)
	rec.setResponse(http.StatusOK, body)

	c := testClient(t, srv)

	out, err := executeRequest(context.Background(), c, GetArgs{Path: "/wiki/api/v2/spages"}, "GET", nil)
	if err != nil {
		t.Fatalf("executeRequest: %v", err)
	}
	// The output must include a truncation marker naming a session id
	// and a /tmp/mcp/...json path.
	if !strings.Contains(out, "truncated") {
		t.Errorf("expected truncation marker in output, got: %q", firstN(out, 200))
	}
	if !strings.Contains(out, "/tmp/mcp/") {
		t.Errorf("expected /tmp/mcp/ pointer in output, got: %q", firstN(out, 200))
	}
	if !strings.Contains(out, ".json") {
		t.Errorf("expected .json pointer in output, got: %q", firstN(out, 200))
	}
	// The output body should be ≤ 40 000 chars (we don't pin the
	// exact threshold to allow for the notice itself, but the bulk
	// of the response must have been cut).
	if len(out) > 41000 {
		t.Errorf("output length = %d, want <= 41000 (truncation should have kicked in)", len(out))
	}
	// The file pointed to by the marker must exist. We extract the
	// path and stat it. The notice shape is
	// "<truncated>Full response saved to /tmp/mcp/<id>.json</truncated>"
	// so the path is the substring between "saved to " and
	// "</truncated>".
	markerIdx := strings.Index(out, "/tmp/mcp/")
	if markerIdx < 0 {
		t.Fatal("no /tmp/mcp/ path found in output")
	}
	tail := out[markerIdx:]
	end := strings.Index(tail, "</truncated>")
	var filePath string
	if end < 0 {
		// The notice shape drifted; fall back to whitespace-bounded.
		end = strings.IndexAny(tail, " 	\n\"'")
		if end < 0 {
			filePath = tail
		} else {
			filePath = tail[:end]
		}
	} else {
		filePath = tail[:end]
	}
	if filePath == "" {
		t.Fatalf("could not extract a file path from: %q", firstN(out, 200))
	}
	t.Cleanup(func() { _ = os.Remove(filePath) })

	data, ferr := os.ReadFile(filePath)
	if ferr != nil {
		t.Fatalf("read full response file %q: %v", filePath, ferr)
	}
	if len(data) == 0 {
		t.Errorf("truncated file %q is empty", filePath)
	}
	// The /tmp/mcp directory should exist (createRequest creates it).
	if _, derr := os.Stat(filepath.Dir(filePath)); derr != nil {
		t.Errorf("/tmp/mcp directory missing: %v", derr)
	}
}

// TestExecuteRequest_QueryParams_BuildURL — the 2nd step sets query
// params on the URL. The test asserts the test server received the
// expected query string.
func TestExecuteRequest_QueryParams_BuildURL(t *testing.T) {
	srv, rec := newTestClient(t)
	rec.setResponse(http.StatusOK, `{"results":[]}`)

	c := testClient(t, srv)

	_, err := executeRequest(context.Background(), c,
		GetArgs{Path: "/wiki/api/v2/spaces", Query: map[string]string{"limit": "5", "cursor": "abc"}},
		"GET", nil)
	if err != nil {
		t.Fatalf("executeRequest: %v", err)
	}
	if !strings.Contains(rec.rawQuery, "limit=5") {
		t.Errorf("query missing limit=5: %q", rec.rawQuery)
	}
	if !strings.Contains(rec.rawQuery, "cursor=abc") {
		t.Errorf("query missing cursor=abc: %q", rec.rawQuery)
	}
}

// TestExecuteRequest_BodyForwarded_ForPostArgs — the 3rd step's body
// argument is forwarded verbatim to the upstream. This pins the
// contract that executeRequest does NOT re-marshal the body — the
// caller (Phase 7 handler) is responsible for JSON-encoding args.Body.
func TestExecuteRequest_BodyForwarded_ForPostArgs(t *testing.T) {
	srv, rec := newTestClient(t)
	rec.setResponse(http.StatusOK, `{"id":"42"}`)

	c := testClient(t, srv)

	body := []byte(`{"spaceId":"123","title":"Hi"}`)
	_, err := executeRequest(context.Background(), c, PostArgs{Path: "/wiki/api/v2/pages"}, "POST", body)
	if err != nil {
		t.Fatalf("executeRequest: %v", err)
	}
	if rec.method != "POST" {
		t.Errorf("method = %q, want POST", rec.method)
	}
	if string(rec.body) != string(body) {
		t.Errorf("body mismatch\n  got:  %q\n  want: %q", rec.body, body)
	}
}

// TestExecuteRequest_Discriminator_AllFiveArgTypes — executeRequest must
// accept any of the 5 args types via the discriminator interface.
// This is the type-system test: if the interface signature changes,
// the test stops compiling.
func TestExecuteRequest_Discriminator_AllFiveArgTypes(t *testing.T) {
	var _ reqArgs = GetArgs{}
	var _ reqArgs = PostArgs{}
	var _ reqArgs = PutArgs{}
	var _ reqArgs = PatchArgs{}
	var _ reqArgs = DeleteArgs{}
}

// TestExecuteRequest_DoesNotLeakToken — the response path must never
// include the API token, even if the upstream body somehow contains it
// (e.g. a misconfigured proxy echoes the Authorization header). We
// install a synthetic token (the test fixture's APIKey), run
// executeRequest on a body that contains the token string, and assert
// the token is NOT in the returned string.
//
// The leak check is on the test client fixture's APIKey
// (testAPITokenSentinel) — NOT on a body field. This is the
// integration-boundary check: the Phase 2 atlassian.Client already
// asserts it never logs the token; this test confirms the
// executeRequest pipeline doesn't surface the token from the
// Authorization header into the response text.
//
// We deliberately omit the token from the response body so the test
// can isolate the leak to the header path; if the body contained
// the same string, a successful assertion would be tautological.
func TestExecuteRequest_DoesNotLeakToken(t *testing.T) {
	srv, rec := newTestClient(t)
	rec.setResponse(http.StatusOK, `{"id":"1","name":"plain"}`)

	c := testClient(t, srv)

	out, err := executeRequest(context.Background(), c, GetArgs{Path: "/x"}, "GET", nil)
	if err != nil {
		t.Fatalf("executeRequest: %v", err)
	}
	// The testAPITokenSentinel is the value of c.Auth.APIToken. It
	// must not appear in the response text under any circumstance.
	if strings.Contains(out, testAPITokenSentinel) {
		t.Errorf("executeRequest output leaked token: %q", firstN(out, 200))
	}
}

// firstN returns up to n leading bytes of s as a string. Used in
// failure messages to keep the diff small.
func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// testAPITokenSentinel is the synthetic API token value used by
// testClient. It is intentionally NOT a real token; it is a string
// the leak-check tests can grep for to confirm the formatter never
// echoes it.
const testAPITokenSentinel = "test-token-do-not-leak"

// testClient builds a *atlassian.Client wired to the supplied
// httptest server. The base URL is rewritten to the server's URL so
// Client.Do hits the test recorder instead of a real Atlassian host.
// The SiteName/UserEmail/APIKey are all synthetic — no real secrets.
func testClient(t *testing.T, srv *httptest.Server) *atlassian.Client {
	t.Helper()
	c, err := atlassian.New(atlassianConfigFixture())
	if err != nil {
		t.Fatalf("atlassian.New: %v", err)
	}
	c.BaseURL = srv.URL
	c.HTTPClient = srv.Client()
	return c
}

// atlassianConfigFixture returns a config.Config with all required
// fields populated with synthetic (non-secret) values. The APIKey
// matches testAPITokenSentinel so the leak-check test can grep for
// it across both the client config and the formatted output.
func atlassianConfigFixture() *config.Config {
	return &config.Config{
		SiteName:  "test.atlassian.net",
		UserEmail: "tester@example.com",
		APIKey:    testAPITokenSentinel,
		Debug:     false,
	}
}
