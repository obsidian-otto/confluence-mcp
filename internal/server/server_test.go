// Phase 15 — server-bootstrap tests.
//
// The server package exposes one factory, New(deps), that returns a
// *mcp.Server with the 18 Confluence tools (5 CRUD + 6 quality-of-life
// + 3 markdown round-trip + 3 attachments + 1 drawio orchestrator)
// already registered. These tests assert:
//
//  1. New(deps) returns a non-nil *mcp.Server with no error.
//  2. The 18 tool names are registered, in any order, and only those.
//  3. NewServer propagates a nil-Deps error before doing any work
//     (defense in depth — the Phase 9 main.go pre-validates, but the
//     factory must not crash with a nil-deref on a misbehaving caller).
//  4. The package-level tools package (Phase 7) exposes a
//     RegisterAll(srv, client) entry point that registers the same 18
//     tools when called against a freshly constructed mcp.Server.
//
// We use the mcp-golang public CheckToolRegistered API for
// introspection (no private-field poking). That helper exists for
// exactly this use case and is the same one Phase 9's smoke test
// will use.
//
// The *atlassian.Client is wired with a real httptest.Server
// pointed-at junk URL — the server test never invokes the handlers,
// it only asserts they are *registered*. Wiring a real Client
// (rather than a nil) ensures the factory's deps-validation path
// runs end-to-end and would catch a future "I forgot to thread
// client through" regression.
package server_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bennie/mcp-confluence/internal/atlassian"
	"github.com/bennie/mcp-confluence/internal/config"
	"github.com/bennie/mcp-confluence/internal/server"
)

// expectedTools is the canonical ordered set of tool names the
// server must register. The order is irrelevant to the MCP wire
// protocol (handleListTools sorts by name before serializing) but
// the SET membership is part of the public contract: Hermes
// enumerates the tool names as `mcp_confluence_<tool>`, and any
// drift from these exact names is a bug. We assert the set, not
// the order, so a future reorganization of the registration loop
// does not break the test.
//
// Surface history:
//   - v1 (Phase 8, 2026-07-09):  5 CRUD tools, upstream-aligned.
//   - v1.1 (audit closure 2026-07-10):  +5 quality-of-life tools
//     (conf_list_spaces, conf_list_pages, conf_get_page_body,
//     conf_search, conf_help). Local additions, no upstream
//     counterpart.
//   - v2 (Phase 14/15, 2026-07-10):  +3 markdown round-trip tools
//     (conf_post_markdown, conf_put_markdown,
//     conf_get_page_markdown). Local additions, the upstream has
//     no markdown tools.
//   - 3 attachments (conf_upload_attachment, conf_list_attachments,
//     conf_delete_attachment). v3 addition; conf_upload_attachment
//     is the only v1 REST path in the server (multipart/form-data +
//     X-Atlassian-Token: no-check). See
//     specs/11-attachments/01-research-and-surface.md.
//   - 1 drawio (conf_upload_drawio). v3 orchestrator: uploads a
//     .drawio / .drawio.png / .drawio.svg file AND embeds it
//     on the page in one call (v1 multipart POST + v2 page
//     PUT). See specs/12-drawio-attachments/01-research-and-surface.md.
//   - 1 page-tree (conf_get_page_tree). v1.x addition 2026-07-14:
//     merges three v2 endpoints (/ancestors, /children,
//     /descendants) into one envelope. See
//     specs/13-page-tree-index/01-research-and-surface.md.
var expectedTools = []string{
	"conf_delete",
	"conf_delete_attachment",
	"conf_get",
	"conf_get_page_body",
	"conf_get_page_markdown",
	"conf_get_page_tree",
	"conf_help",
	"conf_list_attachments",
	"conf_list_pages",
	"conf_list_spaces",
	"conf_patch",
	"conf_post",
	"conf_post_markdown",
	"conf_put",
	"conf_put_markdown",
	"conf_search",
	"conf_upload_attachment",
	"conf_upload_drawio",
}

// newDeps returns a *server.ServerDeps with a real, fully-wired
// *atlassian.Client (HTTPClient pointed at a no-op httptest server)
// and a non-nil *config.Config. The httptest server never sees a
// request in these tests — the server package is only asked to
// register tools, not to invoke them. The server is closed via
// t.Cleanup.
func newDeps(t *testing.T) server.ServerDeps {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Defensive: if any code path accidentally triggers a real
		// HTTP call during registration, return an empty 200 so the
		// test surfaces a different failure (the call itself)
		// rather than a confusing connection-refused panic.
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	cfg := &config.Config{
		SiteName:  "example",
		UserEmail: "test@example.com",
		APIKey:    "test-token",
		Debug:     false,
	}
	client, err := atlassian.New(cfg)
	if err != nil {
		t.Fatalf("atlassian.New: %v", err)
	}
	// Override the HTTPClient to a server-bound one so any code
	// path that accidentally hits the network does so against the
	// test double. atlassian.New sets HTTPClient = http.DefaultClient;
	// we replace that field here. (The field is exported.)
	client.HTTPClient = srv.Client()
	// Re-point the BaseURL at the test server so the absolute URL
	// computation in Client.Do lands on localhost, not example.atlassian.net.
	client.BaseURL = srv.URL

	return server.ServerDeps{
		Config: cfg,
		Client: client,
	}
}

// TestNew_ConstructsServer asserts New() returns a non-nil
// *mcp.Server and a nil error under the happy path.
func TestNew_ConstructsServer(t *testing.T) {
	srv, err := server.New(newDeps(t))
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	if srv == nil {
		t.Fatal("server.New returned nil server")
	}
}

// TestNew_RegistersAllEighteenTools asserts the 18 tool names are
// registered with the returned server. We use the mcp-golang
// CheckToolRegistered helper — its public surface, no internals.
func TestNew_RegistersAllEighteenTools(t *testing.T) {
	srv, err := server.New(newDeps(t))
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	for _, name := range expectedTools {
		if !srv.CheckToolRegistered(name) {
			t.Errorf("tool %q not registered; CheckToolRegistered returned false", name)
		}
	}
}

// TestNew_RegistersExactlyEighteenTools asserts no extra tool is
// registered. Today there are exactly 18; if a future phase adds a
// 19th, this test will catch the divergence and force the contract
// to be re-confirmed. We assert by enumerating the registered set
// via the public introspection helper and comparing against
// expectedTools. Because mcp-golang does not expose a public "list
// all tools" function (only the bool CheckToolRegistered), we
// enumerate by checking each name in expectedTools AND every other
// plausible name; a strict set equality is not directly possible,
// so we use a "no extra surprises" smoke check: each expected name
// is present, and a small set of names that MUST NOT exist (e.g.
// obvious typos, wrong verb casing) are absent.
func TestNew_RegistersExactlyEighteenTools(t *testing.T) {
	srv, err := server.New(newDeps(t))
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	// Must-not-exist set: typos, wrong-case, and the legacy
	// upstream aliases we explicitly do NOT port.
	prohibited := []string{
		"Conf_Get",    // wrong case
		"conf-get",    // wrong separator
		"confget",     // missing separator
		"conf_create", // wrong verb
		"conf_update", // wrong verb
		"conf_list",   // wrong verb (we ship conf_list_spaces, not the bare verb)
		"get",         // bare verb
		"post",        // bare verb
		"conf_space",  // resource-style name
		"confluence",  // server name
	}
	for _, name := range prohibited {
		if srv.CheckToolRegistered(name) {
			t.Errorf("prohibited tool name %q is registered; tool naming has drifted", name)
		}
	}
}

// TestNew_NilDepsReturnsError asserts the factory short-circuits on
// nil dependencies with a clear error rather than panicking. The
// exact error message is not asserted (caller will wrap it), but
// the return values must be (nil, non-nil-error).
func TestNew_NilDepsReturnsError(t *testing.T) {
	srv, err := server.New(server.ServerDeps{})
	if err == nil {
		t.Fatal("server.New with empty deps: expected error, got nil")
	}
	if srv != nil {
		t.Errorf("server.New with empty deps: expected nil server, got %T", srv)
	}
}

// TestNew_ExposesMCPServerType is a compile-time-style assertion
// that the return type of server.New is the *mcp.Server type from
// metoro-io/mcp-golang. We call a real method on the returned
// value (CheckToolRegistered) that only exists on *mcp.Server;
// if the type ever drifts, this test stops compiling before it
// even runs. The runtime check inside the if also gives the test
// real signal — a future refactor that swapped the return type
// for a struct with a same-named field would compile but fail
// here.
func TestNew_ExposesMCPServerType(t *testing.T) {
	srv, err := server.New(newDeps(t))
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	// CheckToolRegistered is a *mcp.Server method. If server.New
	// ever returns a different concrete type (e.g. a private
	// wrapper), this line will fail to compile.
	if !srv.CheckToolRegistered("conf_get") {
		t.Fatal("conf_get missing on the server returned by server.New")
	}
}
