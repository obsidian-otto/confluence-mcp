// Package server — the mcp-confluence server bootstrap.
//
// This package is the single composition root for the MCP wire
// protocol. It glues together:
//
//   - github.com/metoro-io/mcp-golang (the MCP framework; stdio transport)
//   - internal/tools  (the 5 Confluence tool handlers + safeHandler wrapper)
//   - internal/atlassian (the HTTP client used by every handler)
//   - internal/config (the resolved settings)
//
// The factory New(deps) is the ONLY entry point. Phase 9's main.go
// builds a Config + atlassian.Client, calls New, then calls
// server.Serve() to block on stdin/stdout JSON-RPC traffic.
//
// Architectural notes:
//
//  1. New() does NOT serve. The returned *mcp.Server is fully
//     constructed (transport wired, tools registered) but the
//     Serve() method — which blocks on the transport — must be
//     called separately. This separation is what makes the
//     registration testable without spawning a goroutine or
//     touching real stdio.
//
//  2. New() does NOT call tools.RegisterAll directly. Instead it
//     delegates the registration to the tools package via the
//     RegisterAll(srv, client) function. This keeps the tools
//     package self-contained (it owns the 5 tool names, the 5
//     descriptions, and the 5 handlers) while this package owns
//     the "server exists" concern.
//
//  3. The error return from New() is currently always nil. It
//     exists as a forward-compatibility hook — the brief in
//     IMPLEMENTATION_PLAN.md § Phase 8 specifies the
//     `func New(deps ServerDeps) (*mcp.Server, error)` shape, and
//     future phases may add a config-validation step that can
//     fail before any tool is registered. Today the only error
//     path is the nil-deps short-circuit (TestNew_NilDepsReturnsError).
package server

import (
	"fmt"

	mcp "github.com/metoro-io/mcp-golang"
	"github.com/metoro-io/mcp-golang/transport"
	"github.com/metoro-io/mcp-golang/transport/stdio"

	"github.com/bennie/mcp-confluence/internal/atlassian"
	"github.com/bennie/mcp-confluence/internal/config"
	"github.com/bennie/mcp-confluence/internal/tools"
)

// ServerName is the name reported in the MCP `initialize` response.
// Surface-level contract: Hermes (and any other MCP client) uses
// this string to identify the server in its tool namespace
// (mcp_<server>_<tool>). The value "mcp-confluence" matches the
// module path's final segment so a `make build` and a `mcp test`
// round-trip produce the same tool prefix.
const ServerName = "mcp-confluence"

// ServerVersion is the version reported in the MCP `initialize`
// response. Today it is hard-coded to v1.0.0 — the first cut of
// the server. Phase 9's main.go and Phase 11's container build
// inject a real version via ldflags (or via project.toml's
// BP_GO_VERSION metadata), but those hooks are out of scope for
// Phase 8. The constant is a single source of truth so a future
// "-X main.version" rewrite has one place to substitute.
const ServerVersion = "1.0.0"

// ServerDeps bundles the constructor-time dependencies New() needs.
// The struct is small and explicit-by-design: every field is
// required, every field is set by Phase 9's main.go from a real
// runtime value. Tests can construct one trivially (see
// server_test.go:newDeps).
//
// We use a struct rather than positional arguments for two
// reasons:
//
//  1. As the dependency set grows (e.g. a logger, a metrics sink,
//     a debug-mode flag from cfg.Debug), adding a new parameter
//     is a non-breaking change to the public API. Positional
//     args would force every caller to update.
//  2. Tests can construct one with zero-value fields for the
//     things they don't care about.
type ServerDeps struct {
	// Config is the resolved settings (after .env resolution
	// per the Q22 lock). New() does not read it directly today,
	// but tools.RegisterAll may consult cfg.Debug in a future
	// phase, and Phase 9's smoke test asserts the field is
	// non-nil so we surface a misconfiguration here rather than
	// inside a tool handler.
	Config *config.Config

	// Client is the wired atlassian HTTP client. Every Confluence
	// tool handler closes over this value. It is the ONLY
	// transport dependency the tools have; tests can swap in a
	// httptest-backed client without touching the registration
	// code.
	Client *atlassian.Client
}

// New constructs a *mcp.Server with the stdio transport and the 5
// Confluence tools registered. It does NOT call Serve(); the caller
// (Phase 9's main.go) is responsible for that.
//
// Return values:
//
//   - (*mcp.Server, nil) on success.
//   - (nil, error)       when deps is empty / nil (the only
//     error path today). The error message
//     names the missing field so a Phase 9
//     stack trace points at the right env
//     var or construction site.
//
// The transport is stdio.NewStdioServerTransport() — the only
// transport Phase 8 ships. An HTTP transport could be added later
// (see specs/04-mcp-golang-framework/01-server-api.md §"Cleanup")
// but is out of scope.
//
// The server name and version are set via the WithName/WithVersion
// options. Both surface in the MCP `initialize` response and are
// visible to the LLM via Hermes' tool-listing UI. Changing them
// after release is a breaking change for any client that pins to
// a specific server identity.
// New constructs a *mcp.Server with the stdio transport (reading from
// os.Stdin and writing to os.Stdout) and the 5 Confluence tools
// registered. It does NOT call Serve(); the caller (Phase 9's
// main.go) is responsible for that.
//
// For tests / custom stdin/stdout wiring, see NewWithTransport — that
// variant accepts a caller-supplied transport and is what main.go
// uses so the lifecycle can detect stdin EOF (the stdio transport's
// readLoop exits silently on EOF without invoking the server's
// shutdown path, so a separate goroutine that owns the stdin end is
// needed for clean shutdown).
//
// Return values:
//
//   - (*mcp.Server, nil) on success.
//   - (nil, error)       when deps is empty / nil. The error message
//     names the missing field so a Phase 9 stack trace points at
//     the right env var or construction site.
//
// The server name and version are set via the WithName/WithVersion
// options. Both surface in the MCP `initialize` response and are
// visible to the LLM via Hermes' tool-listing UI. Changing them
// after release is a breaking change for any client that pins to
// a specific server identity.
func New(deps ServerDeps) (*mcp.Server, error) {
	return NewWithTransport(deps, stdio.NewStdioServerTransport())
}

// NewWithTransport is the explicit-transport constructor. main.go
// uses it to inject a stdio transport whose input reader is wired
// to the WRITE end of an io.Pipe that main.go owns — that gives
// main.go a clean stdin-EOF signal (closing the pipe writer end)
// without competing with the transport's internal bufio.Reader for
// bytes on os.Stdin. Tests can pass a transport bound to a
// bytes.Buffer or a net.Conn without touching os.Stdin.
//
// All other behavior is identical to New: the same ServerDeps
// validation, the same ServerName/ServerVersion, the same
// RegisterAll delegation.
func NewWithTransport(deps ServerDeps, tr transport.Transport) (*mcp.Server, error) {
	// Validate deps. We check each field in turn so the error
	// message names the FIRST missing field; subsequent checks
	// run only after earlier ones pass.
	switch {
	case deps.Config == nil:
		return nil, fmt.Errorf("server.New: Config is required")
	case deps.Client == nil:
		return nil, fmt.Errorf("server.New: Client is required")
	}

	srv := mcp.NewServer(
		tr,
		mcp.WithName(ServerName),
		mcp.WithVersion(ServerVersion),
	)

	// Delegate registration to the tools package. The tools
	// package owns the 5 names, the 5 descriptions, and the 5
	// handlers — keeping the server package free of business
	// logic. RegisterAll is the single point where
	// metoro-io/mcp-golang's RegisterTool is called.
	if err := tools.RegisterAll(srv, deps.Client); err != nil {
		return nil, fmt.Errorf("server.New: register tools: %w", err)
	}

	return srv, nil
}
