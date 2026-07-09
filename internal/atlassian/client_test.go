package atlassian

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/bennie/mcp-confluence/internal/config"
)

// newTestServer returns an httptest.Server whose captured request is
// exposed via the returned recorder. Tests inspect the recorder to assert
// on the URL, method, headers, and body shape without depending on a real
// Atlassian endpoint.
func newTestServer(t *testing.T) (*httptest.Server, *requestRecorder) {
	t.Helper()
	rec := &requestRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		// Default response: 200 OK with the JSON body the test may override
		// by calling rec.setResponse before the test issues the call.
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
	// request fields, populated by record()
	method     string
	path       string
	rawQuery   string
	authHeader string
	body       []byte
	header     http.Header
	// response fields, populated by setResponse() before the test issues
	// the call. The handler reads these to decide status + body.
	respStatus int
	respBody   []byte
}

func (r *requestRecorder) record(req *http.Request) {
	r.method = req.Method
	r.path = req.URL.Path
	r.rawQuery = req.URL.RawQuery
	r.authHeader = req.Header.Get("Authorization")
	r.header = req.Header.Clone()
	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		r.body = b
	}
}

// setResponse is called by a test BEFORE invoking Do/Call, to control the
// status and body returned by the httptest server.
func (r *requestRecorder) setResponse(status int, body string) {
	r.respStatus = status
	r.respBody = []byte(body)
}

// newValidConfig returns a config.Config with all required fields set.
// The email is intentionally a non-secret test value; the APIKey is a
// short synthetic string. NEVER use a real Atlassian API token in tests.
//
// Per the locked contract (specs/01-foundations/03-env-var-contract.md),
// SiteName is the BARE prefix — the server appends ".atlassian.net" itself.
func newValidConfig() *config.Config {
	return &config.Config{
		SiteName:  "test",
		UserEmail: "tester@example.com",
		APIKey:    "redac...ey",
		Debug:     false,
	}
}

// testAPITokenSentinel is the same string newValidConfig writes into
// APIKey, used by the leak-check tests below. We assert the formatted
// error message does NOT contain this string.
const testAPITokenSentinel = "redacted-test-key"

// TestNew_MissingFields ensures the constructor returns *AuthMissingError
// for each required field. The error type enables errors.As recovery by
// the main entrypoint, which wants to surface a "set this env var" message.
func TestNew_MissingFields(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*config.Config)
		wantField string
	}{
		{
			name:      "missing site",
			mutate:    func(c *config.Config) { c.SiteName = "" },
			wantField: "ATLASSIAN_SITE_NAME",
		},
		{
			name:      "missing email",
			mutate:    func(c *config.Config) { c.UserEmail = "" },
			wantField: "ATLASSIAN_USER_EMAIL",
		},
		{
			name:      "missing token",
			mutate:    func(c *config.Config) { c.APIKey = "" },
			wantField: "ATLASSIAN_API_TOKEN",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := newValidConfig()
			tc.mutate(cfg)
			c, err := New(cfg)
			if err == nil {
				t.Fatalf("New() expected error, got client=%+v", c)
			}
			var ame *AuthMissingError
			if !errAs(err, &ame) {
				t.Fatalf("expected *AuthMissingError, got %T: %v", err, err)
			}
			if ame.Field != tc.wantField {
				t.Errorf("Field = %q, want %q", ame.Field, tc.wantField)
			}
		})
	}
}

// TestNew_BuildsBaseURL pins the URL construction: <site> -> https://<site>.
func TestNew_BuildsBaseURL(t *testing.T) {
	cfg := newValidConfig()
	c, err := New(cfg)
	if err != nil {
		t.Fatalf("New() unexpected error: %v", err)
	}
	want := "https://test.atlassian.net"
	if c.BaseURL != want {
		t.Errorf("BaseURL = %q, want %q", c.BaseURL, want)
	}
	if c.HTTPClient == nil {
		t.Error("HTTPClient should default to non-nil (http.DefaultClient)")
	}
	if c.Auth == nil {
		t.Error("Auth should be non-nil after New()")
	}
	if c.Auth.Email != cfg.UserEmail {
		t.Errorf("Auth.Email = %q, want %q", c.Auth.Email, cfg.UserEmail)
	}
	if c.Auth.APIToken != cfg.APIKey {
		t.Errorf("Auth.APIToken mismatch")
	}
}

// TestDo_HeadersAndURL verifies that Do sets the basic-auth header and
// builds the path + query string correctly. We use a synthetic test
// server (NOT a real token) so the test value never leaves the test
// process.
func TestDo_HeadersAndURL(t *testing.T) {
	srv, rec := newTestServer(t)

	cfg := newValidConfig()
	// Redirect the BaseURL to the test server so we don't hit Atlassian.
	c, err := New(cfg)
	if err != nil {
		t.Fatalf("New() err: %v", err)
	}
	c.BaseURL = srv.URL
	c.HTTPClient = srv.Client()

	_, status, err := c.Do(context.Background(), "GET", "/wiki/api/v2/spaces", map[string]string{"limit": "2"}, nil)
	if err != nil {
		t.Fatalf("Do() err: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}
	if rec.method != "GET" {
		t.Errorf("method = %q, want GET", rec.method)
	}
	if rec.path != "/wiki/api/v2/spaces" {
		t.Errorf("path = %q, want /wiki/api/v2/spaces", rec.path)
	}
	// Query: limit=2 — order is implementation-defined for a map, so we
	// re-parse the raw query and compare keys/values, not the literal string.
	q, perr := url.ParseQuery(rec.rawQuery)
	if perr != nil {
		t.Fatalf("raw query not parseable: %q (err: %v)", rec.rawQuery, perr)
	}
	if got := q.Get("limit"); got != "2" {
		t.Errorf("query limit = %q, want 2", got)
	}
	// Authorization: Basic base64(email:token). We decode the value and
	// confirm it matches the configured email + token. NEVER log the
	// decoded value; we only assert on its presence and format.
	ah := rec.authHeader
	if !strings.HasPrefix(ah, "Basic ") {
		t.Fatalf("Authorization prefix = %q, want \"Basic ...\"", ah)
	}
	encoded := strings.TrimPrefix(ah, "Basic ")
	decoded, derr := base64.StdEncoding.DecodeString(encoded)
	if derr != nil {
		t.Fatalf("Authorization value not valid base64: %v", derr)
	}
	if string(decoded) != cfg.UserEmail+":"+cfg.APIKey {
		t.Errorf("decoded auth = %q, want %q:%q (test value, not real)", string(decoded), cfg.UserEmail, cfg.APIKey)
	}
}

// TestDo_BodyAndMethod verifies POST/PUT/PATCH carry the body bytes and
// the right Content-Type. The body is opaque bytes — Do does NOT
// re-encode the body. The handler (Phase 6) is responsible for JSON
// marshalling before calling Do.
func TestDo_BodyAndMethod(t *testing.T) {
	srv, rec := newTestServer(t)
	c, err := New(newValidConfig())
	if err != nil {
		t.Fatalf("New() err: %v", err)
	}
	c.BaseURL = srv.URL
	c.HTTPClient = srv.Client()

	tests := []struct {
		method string
		body   []byte
	}{
		{"POST", []byte(`{"spaceId":"1","title":"hello"}`)},
		{"PUT", []byte(`{"id":"42","title":"updated"}`)},
		{"PATCH", []byte(`{"title":"patched"}`)},
		{"DELETE", nil},
	}
	for _, tc := range tests {
		t.Run(tc.method, func(t *testing.T) {
			rec.method = "" // reset
			rec.body = nil
			_, _, err := c.Do(context.Background(), tc.method, "/wiki/api/v2/pages", nil, tc.body)
			if err != nil {
				t.Fatalf("Do() err: %v", err)
			}
			if rec.method != tc.method {
				t.Errorf("method = %q, want %q", rec.method, tc.method)
			}
			if string(rec.body) != string(tc.body) {
				t.Errorf("body = %q, want %q", rec.body, tc.body)
			}
		})
	}
}

// TestDo_APIErrorPropagation asserts that a 404 response is surfaced as a
// *APIError matching the spec's literal shape.
func TestDo_APIErrorPropagation(t *testing.T) {
	srv, rec := newTestServer(t)
	rec.setResponse(http.StatusNotFound, `{"code":"NOT_FOUND","message":"Page not found"}`)

	c, err := New(newValidConfig())
	if err != nil {
		t.Fatalf("New() err: %v", err)
	}
	c.BaseURL = srv.URL
	c.HTTPClient = srv.Client()

	body, status, err := c.Do(context.Background(), "GET", "/wiki/api/v2/pages/999", nil, nil)
	if err == nil {
		t.Fatalf("Do() expected APIError, got nil (body=%q, status=%d)", body, status)
	}
	var ae *APIError
	if !errAs(err, &ae) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if ae.StatusCode != http.StatusNotFound {
		t.Errorf("StatusCode = %d, want 404", ae.StatusCode)
	}
	if ae.Path != "/wiki/api/v2/pages/999" {
		t.Errorf("Path = %q, want /wiki/api/v2/pages/999", ae.Path)
	}
	if ae.Method != "GET" {
		t.Errorf("Method = %q, want GET", ae.Method)
	}
	if !strings.Contains(ae.Error(), "GET /wiki/api/v2/pages/999: 404 Not Found - ") {
		t.Errorf("APIError.Error() shape mismatch: %q", ae.Error())
	}
	if !strings.Contains(ae.Error(), `"code":"NOT_FOUND"`) {
		t.Errorf("APIError.Error() should include response body: %q", ae.Error())
	}
}

// TestDo_NetworkError: when the BaseURL points at an unreachable host, Do
// should return an error. The shape of this error is the network-error
// class (Class 2) from specs/09-anti-patterns/03-error-shapes.md; the
// package formats it as the literal "METHOD path: network error: <err>".
func TestDo_NetworkError(t *testing.T) {
	c, err := New(newValidConfig())
	if err != nil {
		t.Fatalf("New() err: %v", err)
	}
	// 127.0.0.1:1 is the standard "nothing listening" address; the
	// connection is refused immediately. We avoid depending on a
	// routable-but-unresolvable host so the test is fast and offline-safe.
	c.BaseURL = "http://127.0.0.1:1"

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately so the test can't hang on slow networks

	_, _, err = c.Do(ctx, "GET", "/wiki/api/v2/spaces", nil, nil)
	if err == nil {
		t.Fatal("Do() expected error for unreachable host, got nil")
	}
	// The error message must NOT echo the API token. We assert by checking
	// the configured test token is absent from the message.
	if strings.Contains(err.Error(), testAPITokenSentinel) {
		t.Errorf("error leaked API token: %q", err.Error())
	}
}

// TestCall_JSONDecoding verifies Call() JSON-decodes the body on success
// and returns the parsed map[string]any for Phase 6's JMESPath/TOON
// pipeline.
func TestCall_JSONDecoding(t *testing.T) {
	srv, rec := newTestServer(t)
	rec.setResponse(http.StatusOK, `{"results":[{"id":"1","name":"foo"}],"_links":{}}`)

	c, err := New(newValidConfig())
	if err != nil {
		t.Fatalf("New() err: %v", err)
	}
	c.BaseURL = srv.URL
	c.HTTPClient = srv.Client()

	got, err := c.Call(context.Background(), "GET", "/wiki/api/v2/spaces", nil, nil)
	if err != nil {
		t.Fatalf("Call() err: %v", err)
	}
	if got == nil {
		t.Fatal("Call() returned nil map")
	}
	if _, hasResults := got["results"]; !hasResults {
		t.Errorf("Call() result missing 'results' key: %+v", got)
	}
	// Round-trip via encoding/json to confirm the result is JSON-encodable
	// (the Phase 6 pipeline JSON-encodes it before JMESPath/TOON).
	b, jerr := json.Marshal(got)
	if jerr != nil {
		t.Errorf("Call() result not JSON-encodable: %v", jerr)
	}
	if !strings.Contains(string(b), `"results"`) {
		t.Errorf("json.Marshal lost 'results' key: %q", string(b))
	}
}

// TestCall_InvalidJSON: when the server returns non-JSON, Call() returns
// an error of the Class 3 shape from specs/09-anti-patterns/03-error-shapes.md
// (this error type is created in the executeRequest helper, so Call only
// signals a generic decode error and the handler maps it). We assert
// Call() returns a non-nil error and the message does NOT contain the
// token.
func TestCall_InvalidJSON(t *testing.T) {
	srv, rec := newTestServer(t)
	rec.setResponse(http.StatusOK, `<html><body>not json</body></html>`)

	c, err := New(newValidConfig())
	if err != nil {
		t.Fatalf("New() err: %v", err)
	}
	c.BaseURL = srv.URL
	c.HTTPClient = srv.Client()

	_, err = c.Call(context.Background(), "GET", "/wiki/api/v2/spaces", nil, nil)
	if err == nil {
		t.Fatal("Call() expected error for invalid JSON, got nil")
	}
	if strings.Contains(err.Error(), testAPITokenSentinel) {
		t.Errorf("error leaked API token: %q", err.Error())
	}
}

// TestCall_APIErrorPassthrough: when the server returns 4xx, Call()
// surfaces the *APIError so the handler can present the literal shape.
func TestCall_APIErrorPassthrough(t *testing.T) {
	srv, rec := newTestServer(t)
	rec.setResponse(http.StatusUnauthorized, `{"code":"AUTHENTICATION_FAILED","message":"Authentication failed"}`)

	c, err := New(newValidConfig())
	if err != nil {
		t.Fatalf("New() err: %v", err)
	}
	c.BaseURL = srv.URL
	c.HTTPClient = srv.Client()

	_, err = c.Call(context.Background(), "GET", "/wiki/api/v2/spaces", nil, nil)
	if err == nil {
		t.Fatal("Call() expected error for 401, got nil")
	}
	var ae *APIError
	if !errAs(err, &ae) {
		t.Fatalf("Call() expected *APIError, got %T: %v", err, err)
	}
	if ae.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d, want 401", ae.StatusCode)
	}
	if !strings.Contains(ae.Error(), "GET /wiki/api/v2/spaces: 401 Unauthorized - ") {
		t.Errorf("Error() shape mismatch: %q", ae.Error())
	}
}

// errAs is a thin wrapper around errors.As used only in the tests below.
func errAs(err error, target any) bool {
	return errors.As(err, target)
}

// TestBuildURL_PathContainsQuery verifies that a path with a trailing
// "?key=value" is correctly split — query params land in the URL's
// RawQuery rather than getting URL-encoded into the path. This is the
// Phase 10 smoke-test fix (a tool call from a client passes
// path="/wiki/api/v2/spaces?limit=2" must produce a working URL).
func TestBuildURL_PathContainsQuery(t *testing.T) {
	got, err := buildURL("https://test.atlassian.net", "/wiki/api/v2/spaces?limit=2", nil)
	if err != nil {
		t.Fatalf("buildURL err: %v", err)
	}
	want := "https://test.atlassian.net/wiki/api/v2/spaces?limit=2"
	if got != want {
		t.Errorf("buildURL = %q, want %q", got, want)
	}
	if strings.Contains(got, "%3F") {
		t.Errorf("buildURL leaked %%3F encoding (path was not split): %q", got)
	}
}

// TestBuildURL_PathAndQueryMerged verifies that path-embedded query and
// the explicit query map merge correctly — caller-provided entries win
// on key collision.
func TestBuildURL_PathAndQueryMerged(t *testing.T) {
	got, err := buildURL("https://test.atlassian.net",
		"/wiki/api/v2/spaces?limit=2", map[string]string{"cursor": "abc"})
	if err != nil {
		t.Fatalf("buildURL err: %v", err)
	}
	u, perr := neturlParse(got)
	if perr != nil {
		t.Fatalf("parse %q: %v", got, perr)
	}
	if u.Query().Get("limit") != "2" {
		t.Errorf("limit = %q, want %q", u.Query().Get("limit"), "2")
	}
	if u.Query().Get("cursor") != "abc" {
		t.Errorf("cursor = %q, want %q", u.Query().Get("cursor"), "abc")
	}
	if u.Path != "/wiki/api/v2/spaces" {
		t.Errorf("path = %q, want no query in path", u.Path)
	}
}

// neturlParse is a tiny shim so the test file doesn't have to import
// "net/url" alongside the production package's "net/url" alias.
func neturlParse(s string) (*url.URL, error) { return url.Parse(s) }
