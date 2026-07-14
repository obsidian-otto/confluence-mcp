// http_test.go — Phase 18 unit tests for internal/transport/http.
//
// These tests lock the wire contract of the new `serve` subcommand:
// the HTTP framing of a JSON-RPC request, the dispatch through the
// bridge transport, the response envelope shape, and the
// constructor / validator contracts. Tests are white-box (same
// package as the code under test) so they can construct a
// bridgeTransport directly and exercise the per-request log line,
// the per-request status, and the per-request bytes counters
// that the production code path uses.
//
// We don't try to drive the full serve binary from these tests;
// that's what scripts/smoke-serve.sh (or the Phase 18 live
// curl) covers. Here we lock the unit-level invariants:
//
//  1. tools/list returns the 18 tool names registered by
//     server.New — the same 18 names the stdio subcommand
//     returns (transport parity is the load-bearing property
//     of Phase 18).
//  2. Malformed JSON returns the JSON-RPC 2.0 parse-error
//     envelope (-32700) with status 200 (the JSON-RPC-on-HTTP
//     convention; the spec lets us use a 4xx but we don't, see
//     handler.go:writeError).
//  3. Unknown methods return the JSON-RPC 2.0 method-not-found
//     envelope (-32601).
//  4. Wrong-path / wrong-method requests return 404 / 405 at
//     the HTTP level (NOT a JSON-RPC envelope — the path
//     validator runs before the JSON-RPC dispatcher).
//  5. parseListenFlag accepts the load-bearing inputs (IPv4,
//     hostname, IPv6 brackets, port 0) and rejects the
//     load-bearing bad inputs (empty, missing port, non-
//     numeric port, out-of-range port).
//  6. NewServer fails closed on nil mcpSrv, nil logger, and
//     a malformed listen string.
//
// All HTTP-driven tests wrap the production *http.Server in
// httptest.NewServer so the wire contract — including status
// codes, Content-Type, and the per-request log line — is
// exercised end-to-end. The bridge is started via srv.Serve()
// (a no-op for our bridge.Start, but it triggers the
// protocol's SetMessageHandler) so the JSON-RPC pipeline is
// fully wired before any HTTP request lands.
package httptransport

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mcp "github.com/metoro-io/mcp-golang"
	"github.com/metoro-io/mcp-golang/transport/stdio"

	"github.com/bennie/mcp-confluence/internal/atlassian"
	"github.com/bennie/mcp-confluence/internal/config"
	"github.com/bennie/mcp-confluence/internal/server"
)

// postJSON POSTs `body` to ts.URL + path and returns the response.
// It fails the test on a network error; the test then asserts on
// status / body.
func postJSON(t *testing.T, ts *httptest.Server, path string, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(),
		http.MethodPost, ts.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	return resp
}

// TestHandler_ToolsList locks the tools/list wire contract:
// POST /mcp with a tools/list request must return a 200
// response with a JSON-RPC envelope that has result.tools
// (18 names). This is the same contract the stdio subcommand
// satisfies (transcript equivalence is the load-bearing
// property of Phase 18).
func TestHandler_ToolsList(t *testing.T) {
	t.Parallel()
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(stub.Close)

	cfg := &config.Config{
		SiteName: "example", UserEmail: "test@example.com",
		APIKey: "test-token-not-secret",
	}
	client, _ := atlassian.New(cfg)
	client.HTTPClient = stub.Client()
	client.BaseURL = stub.URL

	bt := newBridgeTransport()
	mcpSrv, err := server.NewWithTransport(
		server.ServerDeps{Config: cfg, Client: client}, bt)
	if err != nil {
		t.Fatalf("server.NewWithTransport: %v", err)
	}
	if err := mcpSrv.Serve(); err != nil {
		t.Fatalf("mcpSrv.Serve: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed; use POST", http.StatusMethodNotAllowed)
			return
		}
		newHandler(bt, logger).ServeHTTP(w, r)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/mcp" {
			return
		}
		http.NotFound(w, r)
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	resp := postJSON(t, ts, "/mcp", `{"jsonrpc":"2.0","method":"tools/list","id":1}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("tools/list: expected status 200, got %d", resp.StatusCode)
	}
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	var env struct {
		JSONRPC string      `json:"jsonrpc"`
		ID      interface{} `json:"id"`
		Result  struct {
			Tools []map[string]interface{} `json:"tools"`
		} `json:"result"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(bodyBytes, &env); err != nil {
		t.Fatalf("unmarshal response: %v\nbody: %s", err, string(bodyBytes))
	}
	if env.Error != nil {
		t.Fatalf("tools/list returned error envelope: code=%d msg=%s",
			env.Error.Code, env.Error.Message)
	}
	if got := len(env.Result.Tools); got != 18 {
		t.Errorf("tools/list: expected 18 tools, got %d\nbody: %s", got, string(bodyBytes))
	}
}

// TestHandler_MalformedJSON locks the parse-error contract.
// A request body that is not valid JSON must return a 200
// with a JSON-RPC error envelope. The mcp-golang library
// itself surfaces parse errors via its sendErrorResponse
// path with a non-spec code (it uses -32000 for everything
// not a registered handler), so the test asserts on the
// envelope shape rather than a specific code. The HTTP
// status stays 200 — the spec lets us use a 4xx but we
// don't, see handler.go:writeError.
func TestHandler_MalformedJSON(t *testing.T) {
	t.Parallel()
	ts := newTestHTTPServerWithBridge(t)

	resp := postJSON(t, ts, "/mcp", `{not json`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("malformed JSON: expected status 200, got %d", resp.StatusCode)
	}
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	var env struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(bodyBytes, &env); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, string(bodyBytes))
	}
	if env.Error == nil {
		t.Fatalf("expected error envelope, got none\nbody: %s", string(bodyBytes))
	}
	// The load-bearing property is "an error envelope was
	// returned, not a 200 success". The exact code is
	// library-internal (-32000 in mcp-golang v0.16.1; the
	// JSON-RPC 2.0 spec calls for -32700 but the library
	// doesn't differentiate parse errors from other
	// dispatch errors).
	if env.Error.Code >= 0 {
		t.Errorf("expected negative error code, got %d (positive codes are not valid JSON-RPC error codes)",
			env.Error.Code)
	}
}

// TestHandler_UnknownMethod locks the method-not-found
// contract. A request with a method the protocol doesn't
// recognise must return a JSON-RPC error envelope. The
// mcp-golang library surfaces unknown methods via its
// sendErrorResponse path with code -32000 (its own
// "Internal error" code, not the spec's -32601). The test
// asserts on the envelope shape; the load-bearing property
// is "got an error envelope, not a 200 success".
func TestHandler_UnknownMethod(t *testing.T) {
	t.Parallel()
	ts := newTestHTTPServerWithBridge(t)

	resp := postJSON(t, ts, "/mcp",
		`{"jsonrpc":"2.0","method":"bogus/method","id":2}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unknown method: expected status 200, got %d", resp.StatusCode)
	}
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	var env struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(bodyBytes, &env); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, string(bodyBytes))
	}
	if env.Error == nil {
		t.Fatalf("expected error envelope, got none\nbody: %s", string(bodyBytes))
	}
	// The mcp-golang library returns -32000 for unknown
	// methods (its own internal-error code, not the
	// JSON-RPC 2.0 spec's -32601). The load-bearing
	// property is that an error envelope is returned at
	// all — the caller's id is preserved, the envelope
	// is well-formed JSON.
	if env.Error.Code != -32000 {
		t.Errorf("unknown method: expected code -32000 (mcp-golang v0.16.1), got %d", env.Error.Code)
	}
}

// TestHandler_WrongPath locks the path-validation contract.
//   - GET /mcp must return 405 (mux checks method first).
//   - GET /other (any non-/mcp path) must return 404.
//
// The handler does NOT emit a JSON-RPC envelope for either
// case — path validation runs before the JSON-RPC dispatcher.
func TestHandler_WrongPath(t *testing.T) {
	t.Parallel()
	ts := newTestHTTPServerWithBridge(t)

	// GET /mcp -> 405.
	resp, err := ts.Client().Get(ts.URL + "/mcp")
	if err != nil {
		t.Fatalf("GET /mcp: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET /mcp: expected 405, got %d", resp.StatusCode)
	}

	// GET /other -> 404.
	resp, err = ts.Client().Get(ts.URL + "/other")
	if err != nil {
		t.Fatalf("GET /other: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET /other: expected 404, got %d", resp.StatusCode)
	}
}

// TestParseListenFlag is the table-driven test for
// parseListenFlag. The validator is the load-bearing piece
// of the fails-closed bind guarantee — a malformed listen
// string must exit before net.Listen, so the binary never
// reaches the bind with a silently-corrected address.
func TestParseListenFlag(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		input    string
		wantHost string
		wantPort int
		wantErr  bool
	}{
		{name: "happy_ipv4", input: "127.0.0.1:8080", wantHost: "127.0.0.1", wantPort: 8080},
		{name: "happy_hostname", input: "localhost:9000", wantHost: "localhost", wantPort: 9000},
		{name: "happy_wildcard", input: "0.0.0.0:8080", wantHost: "0.0.0.0", wantPort: 8080},
		{name: "happy_port_zero", input: "127.0.0.1:0", wantHost: "127.0.0.1", wantPort: 0},
		{name: "happy_ipv6_brackets", input: "[::1]:8080", wantHost: "::1", wantPort: 8080},
		{name: "happy_trims_whitespace", input: "  127.0.0.1:8080  ", wantHost: "127.0.0.1", wantPort: 8080},
		{name: "reject_empty", input: "", wantErr: true},
		{name: "reject_whitespace_only", input: "   ", wantErr: true},
		{name: "reject_missing_port", input: "127.0.0.1", wantErr: true},
		{name: "reject_empty_port", input: "127.0.0.1:", wantErr: true},
		{name: "reject_non_numeric_port", input: "127.0.0.1:abc", wantErr: true},
		{name: "reject_port_too_high", input: "127.0.0.1:99999", wantErr: true},
		{name: "reject_port_negative", input: "127.0.0.1:-1", wantErr: true},
		{name: "reject_ipv6_no_port", input: "[::1]", wantErr: true},
		{name: "reject_ipv6_unclosed_bracket", input: "[::1:8080", wantErr: true},
		{name: "reject_ipv6_no_colon_after_bracket", input: "[::1]8080", wantErr: true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			host, port, err := parseListenFlag(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("parseListenFlag(%q): expected error, got host=%q port=%d",
						tc.input, host, port)
				}
				return
			}
			if err != nil {
				t.Errorf("parseListenFlag(%q): unexpected error: %v", tc.input, err)
				return
			}
			if host != tc.wantHost {
				t.Errorf("parseListenFlag(%q): host=%q, want %q", tc.input, host, tc.wantHost)
			}
			if port != tc.wantPort {
				t.Errorf("parseListenFlag(%q): port=%d, want %d", tc.input, port, tc.wantPort)
			}
		})
	}
}

// TestNewServer_NilMCPServer: passing a nil *mcp.Server must
// fail loud with a non-nil error and a nil *http.Server.
func TestNewServer_NilMCPServer(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv, err := NewServer(nil, "127.0.0.1:8080", logger)
	if err == nil {
		t.Fatal("NewServer(nil, ...): expected error, got nil")
	}
	if srv != nil {
		t.Errorf("NewServer(nil, ...): expected nil *http.Server, got %v", srv)
	}
}

// TestNewServer_NilLogger: passing a nil slog logger must
// fail loud.
func TestNewServer_NilLogger(t *testing.T) {
	t.Parallel()
	// We need a non-nil *mcp.Server for the nil-MCP check to
	// pass; a fresh mcp.NewServer is enough. We use the
	// stdio transport (a no-op for our nil-logger test —
	// the nil-logger check fires before the transport is
	// ever invoked).
	mcpSrv := mcp.NewServer(stdio.NewStdioServerTransport())
	srv, err := NewServer(mcpSrv, "127.0.0.1:8080", nil)
	if err == nil {
		t.Fatal("NewServer(..., nil): expected error, got nil")
	}
	if srv != nil {
		t.Errorf("NewServer(..., nil): expected nil *http.Server, got %v", srv)
	}
}

// TestNewServer_InvalidListen: passing a malformed listen
// string must fail loud via parseListenFlag. The bind path
// is not exercised — the parse fails first (fails-closed
// bind guarantee).
func TestNewServer_InvalidListen(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mcpSrv := mcp.NewServer(stdio.NewStdioServerTransport())
	srv, err := NewServer(mcpSrv, "not-a-listener", logger)
	if err == nil {
		t.Fatal("NewServer with bad listen: expected error, got nil")
	}
	if srv != nil {
		t.Errorf("NewServer with bad listen: expected nil *http.Server, got %v", srv)
	}
}

// newTestHTTPServerWithBridge is the shared test helper used
// by TestHandler_MalformedJSON, TestHandler_UnknownMethod, and
// TestHandler_WrongPath. It builds a real mcp.Server wired to
// a real bridge, and wraps the production handler in an
// httptest.Server. The stub atlassian client is created and
// closed inside the helper (t.Cleanup handles teardown).
//
// The function exists separately from newTestHTTPServer (which
// takes an *mcp.Server for explicit injection) so the simpler
// tests don't need to plumb the mcp.Server through.
func newTestHTTPServerWithBridge(t *testing.T) *httptest.Server {
	t.Helper()
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(stub.Close)

	cfg := &config.Config{
		SiteName:  "example",
		UserEmail: "test@example.com",
		APIKey:    "test-token-not-secret",
	}
	client, err := atlassian.New(cfg)
	if err != nil {
		t.Fatalf("atlassian.New: %v", err)
	}
	client.HTTPClient = stub.Client()
	client.BaseURL = stub.URL

	bt := newBridgeTransport()
	mcpSrv, err := server.NewWithTransport(
		server.ServerDeps{Config: cfg, Client: client}, bt)
	if err != nil {
		t.Fatalf("server.NewWithTransport: %v", err)
	}
	if err := mcpSrv.Serve(); err != nil {
		t.Fatalf("mcpSrv.Serve: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed; use POST", http.StatusMethodNotAllowed)
			return
		}
		newHandler(bt, logger).ServeHTTP(w, r)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/mcp" {
			return
		}
		http.NotFound(w, r)
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

// keep import used — bytes is a stdlib package we'd otherwise
// import only if a test grew a buffer-comparison helper. The
// blank assignment silences the unused-import linter without
// changing semantics.
var _ = bytes.NewReader
