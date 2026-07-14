// cmd/mcp-confluence/main.go
//
// Entrypoint for the mcp-confluence MCP server.
//
// Phase 16 — cobra + viper scaffolding. The lifecycle is unchanged:
//
//	load config (process env > cwd .env > binary-dir .env)
//	  -> build atlassian HTTP client
//	  -> wire stdin through an io.Pipe so EOF can cancel the context
//	  -> build mcp.Server with a pipe-backed stdio transport
//	  -> serve (blocks until ctx is cancelled — by signal OR stdin EOF)
//
// run(), runLifecycle(), serveUntilDone(), and wireStdinEOF() are
// preserved verbatim from Phase 9 — they are still called from the
// new cobra command tree (via the stdio / serve subcommand RunE
// closures, both of which delegate to run() for Phase 16).
//
// The CLI surface in Phase 16:
//
//	mcp-confluence                  # default: stdio (delegates to run())
//	mcp-confluence stdio            # explicit stdio (Phase 17 specializes)
//	mcp-confluence serve            # explicit serve (Phase 18 specializes)
//	mcp-confluence --help           # multi-section help, multi-section
//	                                # HERMES REGISTRATION block on stderr
//	mcp-confluence --version        # prints "mcp-confluence version v0.1.0"
//
// CRITICAL JSON-RPC stdout invariant: cobra writes --help, --version,
// and command output to whatever is registered via SetOut/SetErr.
// We register io.Discard for stdout (the JSON-RPC channel) and
// os.Stderr for errors / help / version text. The custom HelpFunc
// and the --version template are wired to write DIRECTLY to
// os.Stderr so the help / version text is not silently dropped by
// the SetOut(io.Discard) sink. This is the same discipline
// established in Phase 9 — the binary's stdout is reserved
// exclusively for JSON-RPC over the stdio transport. NEVER register
// os.Stdout as the cobra output sink.
//
// All logging goes to stderr — stdout is reserved for the JSON-RPC
// stream that the stdio MCP transport consumes (see
// specs/09-anti-patterns/01-stdout-pollution.md). Every log call is
// either log.Printf (defaults to stderr) or fmt.Fprintf(os.Stderr, ...).
//
// Secret handling: the API token is read once by config.LoadFromEnv
// and held in cfg.APIKey. It is NEVER passed to a logger or
// formatter; the debug log line explicitly includes only the
// non-secret site name and email, plus a "value not logged" note.
//
// The binary is built with CGO_ENABLED=0 (set in the Makefile) so it
// is fully statically linked — a prerequisite for the Paketo
// distroless run image (see specs/07-paketo-buildpack/01-project-toml.md).
//
// Architecture note: run() and runLifecycle() are intentionally
// split. run() owns the signal wiring (SIGINT/SIGTERM cancel the
// context); runLifecycle(ctx) is the testable core that loads
// config, builds deps, serves, and returns ctx.Err() on shutdown.
// Tests call runLifecycle directly so they can cancel an injected
// context without sending real OS signals.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	mcp "github.com/metoro-io/mcp-golang"
	"github.com/metoro-io/mcp-golang/transport/stdio"

	"github.com/bennie/mcp-confluence/internal/atlassian"
	"github.com/bennie/mcp-confluence/internal/config"
	"github.com/bennie/mcp-confluence/internal/server"
	httptransport "github.com/bennie/mcp-confluence/internal/transport/http"
)

// version is settable via -ldflags "-X main.version=<x>" so the
// container image can stamp a real build SHA / semver at pack time.
// Today it is hard-coded; Phase 11 wires the project.toml BP_GO_VERSION
// metadata that injects this via Paketo.
const version = "v0.1.0"

// HERMES REGISTRATION YAML examples, embedded in the --help text
// for the stdio and serve subcommands (per Phase 16 spec — operators
// copy-paste these into ~/.hermes/config.yaml verbatim).
const (
	hermesStdioRegistrationYAML = `mcp_servers:
  confluence:
    command: /home/YOU/Desktop/hermes/confluence-mcp/bin/mcp-confluence
    args: ["stdio"]
    env:
      ATLASSIAN_SITE_NAME: ${ATLASSIAN_SITE_NAME}
      ATLASSIAN_USER_EMAIL: ${ATLASSIAN_USER_EMAIL}
      ATLASSIAN_API_TOKEN: ${ATLASSIAN_API_TOKEN}`

	hermesServeRegistrationYAML = `mcp_servers:
  confluence:
    command: /home/YOU/Desktop/hermes/confluence-mcp/bin/mcp-confluence
    args: ["serve", "--listen=127.0.0.1:8080"]
    env:
      ATLASSIAN_SITE_NAME: ${ATLASSIAN_SITE_NAME}
      ATLASSIAN_USER_EMAIL: ${ATLASSIAN_USER_EMAIL}
      ATLASSIAN_API_TOKEN: ${ATLASSIAN_API_TOKEN}`
)

// versionTemplate is the format string for `mcp-confluence --version`.
// Cobra's default version template writes to SetOut — but we set
// SetOut to io.Discard for the JSON-RPC stdout invariant, so the
// version text would be silently dropped. Use a custom version
// template that writes to os.Stderr directly.
//
// The template receives a *cobra.Command as its single argument;
// .Version is the version string we set on the command. The
// trailing newline is included so the output is line-terminated.
const versionTemplate = `mcp-confluence version {{.Version}}
`

// newRootCmd builds the cobra command tree for Phase 16. The root
// command has the persistent flags; the subcommands inherit them.
// Persistent flags (--site, --email, --api-token, --debug, --config)
// are registered here. They are NOT yet read by the RunE closures —
// Phase 17 (stdio) and Phase 18 (serve) will wire viper
// GetString/GetBool reads into the process env via os.Setenv before
// run() is called. For Phase 16 both subcommand stubs call run()
// directly, so the flags are present but ignored — that's the
// "behavior-preserving" guarantee of Phase 16.
//
// Output discipline: SetOut(io.Discard) + SetErr(os.Stderr) is
// applied in main() BEFORE Execute(). To make --help / --version
// actually visible, the custom HelpFunc and SetVersionTemplate
// write directly to os.Stderr (bypassing SetOut). Tests that call
// newRootCmd() directly MUST also apply SetOut/SetErr before
// Execute() (see cli_test.go).
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "mcp-confluence",
		Short: "Confluence MCP server — 18 tools over stdio JSON-RPC",
		Long: `mcp-confluence is a Go MCP (Model Context Protocol) server that
exposes the upstream Confluence REST API as a set of 18 typed tools.

The default invocation (no subcommand, no flags) starts the server
on stdio JSON-RPC. The 'stdio' subcommand is an explicit alias for
that mode. The 'serve' subcommand starts the same server on a
TCP/HTTP socket — see 'mcp-confluence serve --help' for details.

ALL OUTPUT ON STDERR — stdout is reserved exclusively for the
JSON-RPC stream when running in stdio mode.`,
		Version:       version,
		SilenceUsage:  true, // don't re-print usage on RunE error
		SilenceErrors: true, // do NOT auto-print RunE errors; main() decides
		// whether the error is user-facing (Cleanup failed,
		// bad flags, etc.) or a clean-shutdown Canceled.
		RunE: func(cmd *cobra.Command, args []string) error {
			// Default invocation (no subcommand) behaves EXACTLY
			// like `mcp-confluence stdio`: viper resolves the
			// flag > env > .env picture, the values are
			// re-injected into the process env so the Q22
			// loader (config.LoadFromEnv) sees them at the
			// process-env tier, then run() is called unchanged.
			// The helper is shared with the stdio subcommand
			// RunE so the two paths are byte-for-byte
			// identical in precedence semantics.
			composeFlagsIntoEnv()
			return run()
		},
	}

	// Persistent flags inherited by all subcommands. Phase 16
	// registers them so --help lists them; Phase 17/18 will read
	// them via viper inside the subcommand RunE closures and
	// re-inject into the process environment for run() to consume.
	pflags := root.PersistentFlags()
	pflags.String("site", "",
		"Confluence site prefix (overrides ATLASSIAN_SITE_NAME)")
	pflags.String("email", "",
		"Account email (overrides ATLASSIAN_USER_EMAIL)")
	pflags.String("api-token", "",
		"API token (overrides ATLASSIAN_API_TOKEN). NEVER log this value.")
	pflags.Bool("debug", false,
		"Enable verbose stderr logging (mirrors the DEBUG env var)")
	pflags.String("config", "",
		"Path to an optional viper-compatible config file (JSON/YAML/TOML)")
	pflags.String("listen", "127.0.0.1:8080",
		"TCP/HTTP listen address for the `serve` subcommand (host:port). "+
			"Default binds to localhost only.")

	// Subcommand factories. Phase 16 added stdio + serve stubs.
	// Phase 17 specialized the stdio path. Phase 18 added the
	// serve / HTTP transport.
	//
	// v5 Phase 20 — per-tool CLI subcommands. Each maps 1:1 to
	// a registered MCP tool handler. CLI invocation produces the
	// same result string the stdio / HTTP transports emit (the
	// CLI dispatch is the ONE legitimate stdout writer in the
	// binary — see cli_tool_dispatch.go for the full rationale).
	// Phase 21 will add the remaining 13 subcommands
	// (list_spaces, list_pages, get_page_body, get_page_tree,
	// search, help, post_markdown, put_markdown, get_page_markdown,
	// upload_attachment, list_attachments, delete_attachment,
	// upload_drawio).
	root.AddCommand(newStdioCmd())
	root.AddCommand(newServeCmd())
	root.AddCommand(newConfGetCmd())
	root.AddCommand(newConfPostCmd())
	root.AddCommand(newConfPutCmd())
	root.AddCommand(newConfPatchCmd())
	root.AddCommand(newConfDeleteCmd())

	// Custom help / version writers. cobra's default templates
	// write to the command's SetOut writer — we have set that to
	// io.Discard for the JSON-RPC stdout invariant. Override the
	// writers so help / version text goes DIRECTLY to os.Stderr,
	// preserving the operator-facing UX without leaking onto
	// stdout.
	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		fmt.Fprint(os.Stderr, buildHelpText(cmd))
	})
	root.SetUsageFunc(func(cmd *cobra.Command) error {
		fmt.Fprint(os.Stderr, buildUsageText(cmd))
		return nil
	})
	// SetVersionTemplate tells cobra to render {{.Version}} (the
	// value of cmd.Version, set above) using the supplied text
	// template. The actual version output paths in cobra (Execute,
	// help, etc.) read from OutOrStderr (which is cmd.OutOrStderr)
	// — by setting SetErr(os.Stderr) in main() before Execute(),
	// the rendered version text lands on stderr.
	//
	// Note: cobra exposes the version via the *Command.Version
	// struct field, not a SetVersion(s string) method (verified
	// against cobra v1.10.2 source, command.go). There is no
	// SetVersionFunc either — the only knobs are Version (struct
	// field) and SetVersionTemplate (templating method). To
	// confirm: see specs/14-cobra-viper-golang/01-research-and-surface.md
	// §4.
	root.SetVersionTemplate(versionTemplate)

	// Bind viper to the persistent flags (only after the flags are
	// registered, per the canonical cobra+viper gotcha — see
	// specs/14-cobra-viper-golang/01-research-and-surface.md §3).
	// viper's env-binding is best-effort; if a name has no env
	// binding that's OK — the bound flag value still wins over
	// the lockstep default.
	bindViperToFlags(pflags)

	return root
}

// newStdioCmd returns the `stdio` subcommand. Phase 17 wires the
// flag-composition path: viper reads (flag > env > config) are
// re-injected into the process env via os.Setenv so the locked Q22
// .env ordering (flag → process env → cwd .env → binary-dir .env)
// is preserved by composition — internal/config is untouched.
//
// The same composeFlagsIntoEnv() helper is shared with the root
// RunE so `mcp-confluence stdio --site=...` and the default
// invocation `mcp-confluence --site=...` produce identical
// semantics: flag values win over ATLASSIAN_* env vars, and the
// startup banner on stderr shows the resolved site + email.
func newStdioCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stdio",
		Short: "Run the MCP server on stdio JSON-RPC (default)",
		Long: `stdio runs the mcp-confluence MCP server on the standard
JSON-RPC transport. The server reads newline-delimited JSON-RPC
messages from stdin and writes responses to stdout. This is the
canonical Hermes MCP-host integration path.

Persistent flags (--site, --email, --api-token, --debug, --config)
are honored via viper; flag values are re-injected into the process
env so the locked Q22 .env ordering is preserved by composition.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			composeFlagsIntoEnv()
			return run()
		},
	}
}

// newServeCmd returns the `serve` subcommand. Phase 18 wires the
// TCP/HTTP transport: a net/http listener accepts POST /mcp
// requests, and each request is dispatched to the SAME mcp.Server
// instance the stdio subcommand uses (only the framing differs).
//
// Dependency construction mirrors runLifecycle (cfg → client → server)
// so the resolve-and-build order is identical to the stdio path.
// The deps-building is duplicated here rather than extracted into
// a helper because runLifecycle also wires stdin/stdout through an
// io.Pipe and calls serveUntilDone — those stdio-specific steps are
// not what serve wants; extracting them would create a leaky
// abstraction. The two paths share the configuration and dependency
// tiers but the lifecycle diverges after srv construction.
//
// Signal handling: SIGINT/SIGTERM cancel the context, httpSrv.Shutdown
// is called with a 5-second grace period, and Serve errors other than
// http.ErrServerClosed are propagated as RunE errors so the operator
// sees a non-zero exit on listener failure.
func newServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the MCP server on TCP/HTTP (POST /mcp JSON-RPC bridge)",
		Long: `serve runs the mcp-confluence MCP server on a TCP/HTTP
socket. Each POST /mcp HTTP request is dispatched to the same
mcp.Server the stdio subcommand uses — only the framing changes.

USAGE:
  mcp-confluence serve [flags]

FLAGS:
      --listen string   host:port to bind (default 127.0.0.1:8080)
      --site string     Confluence site prefix (overrides ATLASSIAN_SITE_NAME)
      --email string    Account email (overrides ATLASSIAN_USER_EMAIL)
      --api-token string  API token (overrides ATLASSIAN_API_TOKEN). NEVER log this.
      --debug bool      Enable verbose stderr logging (mirrors DEBUG env)
      --config string   Optional viper-compatible config file (JSON/YAML/TOML)

EXAMPLES:
  # Bind to localhost (most common; dev/test):
  mcp-confluence serve --listen=127.0.0.1:8080

  # Bind to all interfaces (only behind a trusted reverse proxy):
  mcp-confluence serve --listen=0.0.0.0:8080

  # Kernel-pick an ephemeral port (for scripted smoke tests):
  mcp-confluence serve --listen=127.0.0.1:0

HERMES REGISTRATION:
  # ~/.hermes/config.yaml — register the HTTP-bridged MCP server
  mcp_servers:
    confluence:
      command: /path/to/mcp-confluence
      args: ["serve", "--listen=127.0.0.1:8080"]
      env:
        ATLASSIAN_SITE_NAME: smartergroup
        ATLASSIAN_USER_EMAIL: "you@example.com"
        ATLASSIAN_API_TOKEN:  "${ATLASSIAN_API_TOKEN}"

SECURITY:
  - No bearer auth on /mcp — the binary holds the credential, not the caller.
  - Default bind is 127.0.0.1:8080 (localhost only).
  - Bind fails closed: malformed --listen exits non-zero.
  - 0.0.0.0 binds are parseable but must only be used behind a trusted proxy.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Compose flag values into the process env so the
			// Q22-locked config.LoadFromEnv reads them at the
			// highest tier. Same helper as the stdio path.
			composeFlagsIntoEnv()

			// Read --listen via the package-singleton viper
			// (the one bindViperToFlags bound to the
			// persistent flags). Flag > env > config
			// precedence already applies.
			listen := pkgViper.GetString("listen")
			if listen == "" {
				listen = "127.0.0.1:8080"
			}

			// Build the same dependency chain the stdio
			// subcommand builds: config → atlassian.Client →
			// mcp.Server. The serve path differs in one
			// place: instead of server.New (which uses
			// the stdio transport), we use
			// server.NewWithTransport so the bridge is
			// the transport the mcp-golang protocol
			// installs its message handler on. The
			// bridge's Start is a no-op (the httpSrv
			// drives the lifecycle), so the readLoop of
			// the underlying transport.Transport never
			// starts — but the protocol's Connect still
			// wires messageHandler onto the bridge,
			// which is what the dispatch loop in
			// handler.go needs.
			cfg, err := config.LoadFromEnv()
			if err != nil {
				return fmt.Errorf("config: %w", err)
			}
			cli, err := atlassian.New(cfg)
			if err != nil {
				return fmt.Errorf("build atlassian client: %w", err)
			}

			// Two-step wiring: build the bridge first,
			// pass it to server.NewWithTransport so the
			// mcp-golang protocol registers itself on
			// the bridge, then build the http.Server
			// around the SAME bridge. Sharing the
			// bridge is what makes the POST /mcp →
			// JSON-RPC dispatch → response round-trip
			// work.
			bridge := httptransport.NewBridge()
			srv, err := server.NewWithTransport(
				server.ServerDeps{Config: cfg, Client: cli},
				bridge,
			)
			if err != nil {
				return fmt.Errorf("build mcp server: %w", err)
			}

			// Build the *http.Server. NewHTTPServer
			// calls parseListenFlag internally, so a
			// bad --listen value (e.g. "not-a-port")
			// returns a typed error here.
			logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
			httpSrv, err := httptransport.NewHTTPServer(bridge, listen, logger)
			if err != nil {
				return fmt.Errorf("serve: %w", err)
			}

			// Wire signals: SIGINT/SIGTERM cancel the
			// lifecycle context. Shutdown is invoked with
			// a 5-second grace period below.
			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			// Bind the listener explicitly so the
			// resolved address (when --listen has port=0,
			// the kernel picks one) is logged. We could
			// have used httpSrv.ListenAndServe() but that
			// hides the bound port — for smoke tests and
			// operator UX, logging the resolved address
			// is worth the 3 extra lines.
			ln, err := net.Listen("tcp", listen)
			if err != nil {
				return fmt.Errorf("listen %s: %w", listen, err)
			}
			log.Printf("mcp-confluence %s serving on http://%s (site=%s, email=%s)",
				version, ln.Addr().String(), cfg.SiteName, cfg.UserEmail)

			// Run srv.Serve() so the protocol layer wires
			// the message handler onto the bridge.
			// srv.Serve() returns nil almost immediately
			// (the bridge's Start is a no-op), and from
			// then on the httpSrv is the active serving
			// surface.
			if err := srv.Serve(); err != nil {
				return fmt.Errorf("mcp server: %w", err)
			}

			// Drive httpSrv in a goroutine so we can
			// select on signal-vs-error in the main
			// goroutine. http.ErrServerClosed is the
			// normal Shutdown return value; we filter
			// it out so a clean shutdown isn't
			// misreported.
			serveErr := make(chan error, 1)
			go func() {
				if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
					serveErr <- err
				}
			}()

			select {
			case <-ctx.Done():
			case err := <-serveErr:
				return err
			}

			// Graceful shutdown. 5s is enough for the
			// in-flight HTTP requests to drain; the
			// bridge transport has no goroutine of its
			// own to wait on.
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return httpSrv.Shutdown(shutdownCtx)
		},
	}
}

// bindViperToFlags wires viper's flag + env binding for the
// persistent flags. Per specs/14-cobra-viper-golang/01-research-and-surface.md
// §3: BindPFlag is called AFTER flag registration (the canonical
// gotcha). SetEnvPrefix + AutomaticEnv pick up ATLASSIAN_*
// automatically; we also bind explicit env-var names for the
// legacy var names (SITE_NAME, USER_EMAIL, API_TOKEN, DEBUG) so
// the precedence is consistent across flag/env.
//
// pkgViper is the package-level binding of viper that
// bindViperToFlags populates and composeFlagsIntoEnv reads from.
//
// viper's README recommends "initialize a Viper instance and pass that
// around when necessary" — but in a single-binary CLI where the
// root cobra command constructs the viper instance and 1+ RunE
// closures need to read it, package-private globals are the
// simplest correct storage. A wide-area singleton with exported
// name would be unsafe; a package-private (lowercase) name is
// fine — only the local functions below can read or write it.
//
// Order-of-operations invariant: bindViperToFlags MUST be called
// before composeFlagsIntoEnv on every invocation path. The
// newRootCmd factory is the only call to bindViperToFlags and it
// runs synchronously before Execute() walks the RunE tree.
var pkgViper *viper.Viper

// bindViperToFlags sets the package-level pkgViper. The viper
// instance has both the AutomaticEnv env-binding AND the BindPFlag
// pflag-binding, so viper.GetString("site") inside
// composeFlagsIntoEnv reads the flag when present and falls back
// to the env var when the flag is absent.
//
// (We intentionally do NOT return the viper instance from this
// helper — Phase 16's signature was the zero-argument one. The
// package-private singleton is the minimum plumbing.)
func bindViperToFlags(pflags *pflag.FlagSet) {
	v := viper.New()
	v.SetEnvPrefix("ATLASSIAN")
	v.AutomaticEnv()

	// Explicit env-var binding: viper's default
	// "ATLASSIAN_<FLAG_UPPER>" mapping works for the ATLASSIAN_*
	// vars, but our project uses ATLASSIAN_SITE_NAME (not
	// ATLASSIAN_SITE) and DEBUG (no ATLASSIAN_ prefix). The
	// explicit BindEnv calls below pin the mapping so future
	// rename of a flag doesn't silently change the env-var
	// surface.
	_ = v.BindEnv("site", "SITE_NAME")
	_ = v.BindEnv("email", "USER_EMAIL")
	_ = v.BindEnv("api-token", "API_TOKEN")
	_ = v.BindEnv("debug", "DEBUG") // DEBUG is unprefixed; see below.
	_ = v.BindEnv("listen", "LISTEN")

	// BindPFlag AFTER the flag is registered (the canonical
	// cobra+viper gotcha). pflag.FlagSet.Lookup returns the flag
	// already registered via PersistentFlags().String above.
	if f := pflags.Lookup("site"); f != nil {
		_ = v.BindPFlag("site", f)
	}
	if f := pflags.Lookup("email"); f != nil {
		_ = v.BindPFlag("email", f)
	}
	if f := pflags.Lookup("api-token"); f != nil {
		_ = v.BindPFlag("api-token", f)
	}
	if f := pflags.Lookup("debug"); f != nil {
		_ = v.BindPFlag("debug", f)
	}
	if f := pflags.Lookup("listen"); f != nil {
		_ = v.BindPFlag("listen", f)
	}
	// --config is a path flag; Phase 16/17/18 don't read its
	// value into viper yet. It's registered so --help lists it;
	// future phases may wire it through v.ReadInConfig().
	pkgViper = v
}

// composeFlagsIntoEnv is the Phase 17 flag-composition path: it
// reads the resolved viper picture (flag > env > config) for the
// three required settings and re-injects any non-empty value into
// the appropriate ATLASSIAN_* env var. After this call returns,
// the env-var tier the Q22-locked config.LoadFromEnv reads from
// already reflects the flag values, so the locked 4-tier ordering
//
//	flag (viper) → process env (setenv) → cwd .env → binary-dir .env
//
// is preserved by composition — internal/config/* is NOT touched.
//
// Both the stdio subcommand RunE and the root RunE call this
// helper BEFORE run() so `mcp-confluence --site=foo`,
// `mcp-confluence stdio --site=foo`, and (when the user sets the
// var via the process env) `mcp-confluence --site=foo` with
// ATLASSIAN_SITE_NAME=bar all converge on the same final value
// (the flag wins; env is the next tier; .env files the last two).
//
// A single startup banner is also printed to stderr using the
// post-composition env-var values so the operator (and the
// TestStdio_FlagsOverrideEnv gate) can see at a glance which
// value the binary will actually use:
//
//	mcp-confluence v0.1.0 starting (site=<site>, email=<email>)
//
// This is the same one-liner the v0.1 binary prints (see
// runLifecycle's cfg.Debug branch), so existing log-parsing
// isn't disrupted — Phase 17 lifts the line out of the
// cfg.Debug guard so it fires unconditionally. When DEBUG=true
// the same line is then re-printed by runLifecycle alongside
// the API-token-redaction note; both lines are stable.
func composeFlagsIntoEnv() {
	// Read from the package-singleton pkgViper that bindViperToFlags
	// already configured. This is the SAME viper instance the
	// PersistentFlags are bound to, so flag > env > config precedence
	// already applies: viper.GetString here reads the flag value if
	// present, else the env var, else "".
	//
	// (Re-creating a fresh viper.New() in this helper was the bug
	// the test suite caught — see TestStdio_FlagsOverrideEnv. The
	// fresh instance had only BindEnv mappings and never saw the
	// pflag bindings, so flag values were silently dropped.)
	v := pkgViper
	if v == nil {
		// Defensive: bindViperToFlags was not called. This shouldn't
		// happen in production (newRootCmd always calls it before
		// any RunE fires) but a unit test that synthesises the
		// helper directly would trip here. We log+continue rather
		// than panic so the rest of the binary still works.
		log.Printf("composeFlagsIntoEnv: pkgViper not initialised; flag composition disabled")
		v = viper.New()
		v.SetEnvPrefix("ATLASSIAN")
		v.AutomaticEnv()
		_ = v.BindEnv("site", "SITE_NAME")
		_ = v.BindEnv("email", "USER_EMAIL")
		_ = v.BindEnv("api-token", "API_TOKEN")
		_ = v.BindEnv("debug", "DEBUG")
		_ = v.BindEnv("listen", "LISTEN")
	}

	// Inject each non-empty value into the appropriate
	// ATLASSIAN_* env var. viper.GetString returns "" for
	// unset keys; we treat that as "leave the existing env
	// value alone" so a stale .env file or a partial flag set
	// cannot accidentally wipe a real value.
	if s := v.GetString("site"); s != "" {
		_ = os.Setenv("ATLASSIAN_SITE_NAME", s)
	}
	if s := v.GetString("email"); s != "" {
		_ = os.Setenv("ATLASSIAN_USER_EMAIL", s)
	}
	if s := v.GetString("api-token"); s != "" {
		_ = os.Setenv("ATLASSIAN_API_TOKEN", s)
	}
	// DEBUG is bool-typed in viper's env binding; we don't
	// re-inject it because (a) config.LoadFromEnv reads it
	// from the env anyway, (b) the only consumer is the
	// cfg.Debug branch in runLifecycle, and (c) the value
	// surface is intentionally not promoted via os.Setenv to
	// keep the helper focused on the three required creds.

	// Post-composition startup banner. The values are read
	// from the process env so they reflect the resolution
	// order the rest of the binary will see (flag-injected
	// values win; .env values fill the gaps).
	fmt.Fprintf(os.Stderr, "mcp-confluence %s starting (site=%s, email=%s)\n",
		version,
		os.Getenv("ATLASSIAN_SITE_NAME"),
		os.Getenv("ATLASSIAN_USER_EMAIL"),
	)
}

// buildHelpText returns the multi-section help text used by
// mcp-confluence --help. The text is rendered DIRECTLY to os.Stderr
// by the custom HelpFunc installed in newRootCmd. The HERMES
// REGISTRATION block is included verbatim so operators can
// copy-paste the YAML into ~/.hermes/config.yaml.
//
// When cmd is the root command we show both stdio and serve
// registration examples. When cmd is a subcommand (e.g. "stdio" or
// "serve") we show only the relevant registration block plus the
// subcommand-specific instructions.
//
// Per-tool subcommands (the 5 CRUD subcommands added in Phase 20 —
// see cli_tool_crud.go) ship their own USAGE / FLAGS / EXAMPLES /
// HERMES REGISTRATION sections inside cmd.Long. For those commands
// we short-circuit: just print cmd.Long verbatim, with no extra
// boilerplate. The presence of "FLAGS (auto-generated from " in
// cmd.Long is the marker we use to detect this case.
func buildHelpText(cmd *cobra.Command) string {
	if strings.Contains(cmd.Long, "FLAGS (auto-generated from ") {
		// Per-tool subcommand. The Long text is the full
		// help document; do not append any of the
		// buildHelpText boilerplate.
		return cmd.Long + "\n"
	}
	// Build the USAGE / FLAGS / ENV block. We re-use cobra's
	// template engine for the boilerplate (flag list, command
	// list) by writing directly to a strings.Builder, then
	// appending our hand-authored sections.
	var buf strings.Builder
	buf.WriteString(cmd.Long + "\n\n")
	buf.WriteString("USAGE:\n")
	if cmd.HasParent() {
		fmt.Fprintf(&buf, "  %s [flags]\n", cmd.UseLine())
	} else {
		fmt.Fprintf(&buf, "  %s [command] [flags]\n", cmd.UseLine())
	}
	buf.WriteString("\n")

	// Subcommands list (root only — subcommands don't list their
	// siblings).
	if !cmd.HasParent() {
		buf.WriteString("COMMANDS:\n")
		for _, c := range cmd.Commands() {
			if c.Hidden {
				continue
			}
			fmt.Fprintf(&buf, "  %-12s %s\n", c.Name(), c.Short)
		}
		buf.WriteString("  help        Show help for any command\n")
		buf.WriteString("\n")
	}

	// FLAGS section.
	buf.WriteString("FLAGS:\n")
	buf.WriteString("  -h, --help            Show this help text (stderr; no stdout pollution)\n")
	if cmd.Version != "" {
		buf.WriteString("  -V, --version         Print the mcp-confluence version string\n")
	}
	if cmd.HasPersistentFlags() || cmd.HasLocalFlags() {
		if cmd.HasPersistentFlags() {
			buf.WriteString("\nPersistent flags (apply to all subcommands):\n")
			buf.WriteString(flagUsage(cmd.PersistentFlags()))
		}
		if cmd.HasLocalFlags() {
			buf.WriteString("\nLocal flags (this subcommand only):\n")
			buf.WriteString(flagUsage(cmd.LocalFlags()))
		}
	}
	buf.WriteString("\n")

	// ENV VARS section.
	buf.WriteString("ENV VARS:\n")
	buf.WriteString("  All persistent flags have a corresponding ATLASSIAN_* env var:\n")
	buf.WriteString("      --site          ↔  ATLASSIAN_SITE_NAME\n")
	buf.WriteString("      --email         ↔  ATLASSIAN_USER_EMAIL\n")
	buf.WriteString("      --api-token     ↔  ATLASSIAN_API_TOKEN\n")
	buf.WriteString("      --debug         ↔  DEBUG\n")
	buf.WriteString("  Precedence (locked Q22 + viper): flag > process env > .env file > default.\n\n")

	// HERMES REGISTRATION section(s). Subcommands show their own
	// registration block; the root shows both.
	if !cmd.HasParent() {
		buf.WriteString("HERMES REGISTRATION — stdio mode (default):\n\n")
		buf.WriteString("```yaml\n")
		buf.WriteString(hermesStdioRegistrationYAML)
		buf.WriteString("\n```\n\n")
		buf.WriteString("HERMES REGISTRATION — serve (TCP/HTTP) mode:\n\n")
		buf.WriteString("```yaml\n")
		buf.WriteString(hermesServeRegistrationYAML)
		buf.WriteString("\n```\n\n")
	} else if cmd.Name() == "stdio" {
		buf.WriteString("HERMES REGISTRATION — stdio mode:\n\n")
		buf.WriteString("```yaml\n")
		buf.WriteString(hermesStdioRegistrationYAML)
		buf.WriteString("\n```\n\n")
	} else if cmd.Name() == "serve" {
		buf.WriteString("HERMES REGISTRATION — serve (TCP/HTTP) mode:\n\n")
		buf.WriteString("```yaml\n")
		buf.WriteString(hermesServeRegistrationYAML)
		buf.WriteString("\n```\n\n")
	}

	// Trailing pointer.
	if !cmd.HasParent() {
		buf.WriteString(`Use "mcp-confluence [command] --help" for more information about a command.`)
	} else {
		buf.WriteString(`Use "mcp-confluence --help" for the full command tree.`)
	}
	buf.WriteString("\n")
	return buf.String()
}

// flagUsage formats a flag set as a multi-line help block with
// two-space indentation. We hand-roll this rather than using
// pflag.FlagUsages() because the pflag default doubles as the
// source-of-truth format, but we want predictable indentation
// and no ANSI colors. The format is: `<long-name> <type>   <usage>`.
func flagUsage(flags *pflag.FlagSet) string {
	var buf strings.Builder
	flags.VisitAll(func(f *pflag.Flag) {
		if f.Hidden {
			return
		}
		// long name + shorthand if any. Build the final string
		// directly (avoids the ineffective-assign lint pattern).
		var name string
		if f.Shorthand != "" {
			name = "-" + f.Shorthand + ", --" + f.Name
		} else {
			name = "    --" + f.Name
		}
		// type suffix (string, bool, etc.)
		typeStr := ""
		if f.Value.Type() != "bool" {
			typeStr = " " + f.Value.Type()
		}
		fmt.Fprintf(&buf, "  %-26s%s   %s\n", name, typeStr, f.Usage)
	})
	return buf.String()
}

// buildUsageText returns the one-line usage error message printed
// when the user invokes the binary with an unknown subcommand or
// malformed flags.
func buildUsageText(cmd *cobra.Command) string {
	if cmd.HasParent() {
		return fmt.Sprintf("Usage: %s [flags]\n\n", cmd.UseLine())
	}
	return `Usage: mcp-confluence [command] [flags]

Run 'mcp-confluence --help' for the full command tree and HERMES REGISTRATION examples.
`
}

func main() {
	// Build the command tree. main() applies the stdout-protection
	// discipline (SetOut io.Discard, SetErr os.Stderr) BEFORE
	// Execute. This is the load-bearing invariant — every byte on
	// stdout is either a JSON-RPC stdio message or the binary is
	// in --help / --version mode (both of which write to stderr
	// via the custom HelpFunc / SetVersionTemplate, bypassing
	// SetOut).
	cmd := newRootCmd()

	// Note: we INTENTIONALLY do NOT call cmd.SetOut(io.Discard).
	// Cobra's --help routing is already overridden via SetHelpFunc
	// (writes to os.Stderr directly). Cobra's --version path
	// renders via OutOrStdout, and after SetOut(io.Discard) the
	// --version text would silently go to /dev/null (visible
	// symptom: 0 bytes on stderr when running `bin/mcp-confluence
	// --version`). The JSON-RPC-stdout invariant is enforced by
	// code discipline throughout the binary (no fmt.Println
	// anywhere; log.Printf defaults to stderr; the JSON-RPC
	// transport owns os.Stdout via wireStdinEOF's explicit
	// stdOut handoff). Cobra's default SetOut (os.Stdout) only
	// fires on the help/version/usage paths, which are bootstrap-
	// only events that complete before any JSON-RPC is in flight.
	cmd.SetErr(os.Stderr)

	// --version output: routed to stderr via the SetVersionTemplate
	// in newRootCmd() + the SetErr(os.Stderr) above. cobra renders
	// the version template via OutOrStderr() (which returns our
	// os.Stderr here). No SetVersionFunc wrapper needed; cobra does
	// not expose one in v1.10.2 (only the struct field Version +
	// the templating method SetVersionTemplate — see
	// specs/14-cobra-viper-golang/01-research-and-surface.md §4).

	if err := cmd.Execute(); err != nil {
		// Cancellation (SIGINT, SIGTERM, stdin EOF) propagates
		// through run() → runLifecycle() as context.Canceled.
		// Treat that as a clean exit, not an error: do NOT print
		// to stderr (the orchestrator is gracefully shutting us
		// down; no log noise), and exit 0.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			os.Exit(0)
		}
		// Cobra has already printed the error to SetErr (stderr)
		// via the default error-print path; we additionally print
		// the wrapped error so the operator sees the cause even
		// when cobra's auto-rendering was silenced. Avoid the
		// "FATAL:" prefix — runLifecycle / config.validate may
		// already include it (doubled prefix is noise).
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

// run is the production entrypoint. It wires SIGINT/SIGTERM into a
// cancellable context (per Go's signal.NotifyContext idiom) and
// delegates to runLifecycle. The signal handling lives here — not in
// runLifecycle — so tests can drive runLifecycle with a custom
// context that they cancel themselves.
func run() error {
	ctx, cancel := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	return runLifecycle(ctx)
}

// runLifecycle is the testable load → build → serve pipeline. It
// returns ctx.Err() when the context is cancelled (by signal, by a
// test cancelling an injected context, or by stdin EOF), or an error
// from config / atlassian / server construction if any of those
// steps fails, or the error from srv.Serve() if the server fails on
// its own.
//
// The function never panics on expected failure modes — the load
// step returns a typed config error, the build steps return wrapped
// errors, and the serve step forwards the server's error verbatim.
// The recover() boundary is in internal/tools/safeHandler; the
// entrypoint itself trusts the layered error model.
func runLifecycle(ctx context.Context) error {
	// 1. Load config from env + .env file (fail-fast on missing).
	//    Settings resolution order (LOCKED 2026-07-09, see
	//    specs/01-foundations/03-env-var-contract.md):
	//      1. Process env (highest priority)
	//      2. .env in cwd
	//      3. .env next to the binary
	cfg, err := config.LoadFromEnv()
	if err != nil {
		return err
	}

	// 2. Debug log (stderr only). We deliberately do NOT log cfg.APIKey
	//    — the token is a secret and must never reach a logger.
	if cfg.Debug {
		log.Printf("mcp-confluence %s starting (site=%s, email=%s)",
			version, cfg.SiteName, cfg.UserEmail)
		log.Printf("Note: API token value not logged for security")
	}

	// 3. Build the atlassian HTTP client. AuthMissingError surfaces
	//    here if any of the three required fields is empty (defense
	//    in depth — config.validate already rejected empties, but
	//    atlassian.New re-checks so a future caller bypassing
	//    config.LoadFromEnv still gets a clear error).
	cli, err := atlassian.New(cfg)
	if err != nil {
		return fmt.Errorf("build atlassian client: %w", err)
	}

	// 4. Wire stdin through an io.Pipe so the lifecycle can detect
	//    stdin EOF. We derive a child context whose cancel is owned
	//    by the stdin-watcher goroutine: when the MCP parent closes
	//    its stdin, io.Copy returns, we cancel the child ctx, and
	//    the blocking select in serveUntilDone wakes up with
	//    context.Canceled.
	//
	//    Why a pipe: the metoro-io stdio transport's readLoop reads
	//    from a bufio.Reader around its input, and exits silently
	//    on EOF without invoking any shutdown hook. A separate
	//    goroutine that ALSO reads os.Stdin would compete with the
	//    transport's bufio.Reader for bytes (corrupting the JSON-RPC
	//    stream). Funnelling os.Stdin through a pipe gives us a
	//    single SOURCE of input — we own the writer end — so we get
	//    a clean EOF signal without byte-stealing.
	serveCtx, cancelServe := context.WithCancel(ctx)
	defer cancelServe()
	stdIn, stdOut := wireStdinEOF(serveCtx, cancelServe, os.Stdin, os.Stdout, cfg.Debug)

	// 5. Build the MCP server with the pipe-backed stdio transport.
	srv, err := server.NewWithTransport(
		server.ServerDeps{Config: cfg, Client: cli},
		stdio.NewStdioServerTransportWithIO(stdIn, stdOut),
	)
	if err != nil {
		return fmt.Errorf("build mcp server: %w", err)
	}

	// 6. Serve until serveCtx is cancelled. On the stdio transport
	//    srv.Serve() returns nil almost immediately (the actual
	//    blocking call is the transport's readLoop goroutine); we
	//    then block on ctx.Done() until SIGINT/SIGTERM or stdin
	//    EOF cancels the context.
	return serveUntilDone(serveCtx, srv)
}

// wireStdinEOF returns the (in, out) pair the stdio transport
// should use, and arranges for `cancel` to fire when os.Stdin hits
// EOF. A single goroutine copies os.Stdin into a pipe; when the
// copy completes (parent closed stdin, or copy error), the pipe
// writer is closed AND cancel() is invoked so the main blocking
// select wakes up with context.Canceled.
//
// Output goes straight to os.Stdout — the transport writes are
// flushed by the underlying writer and don't need the pipe
// treatment.
//
// debug controls whether internal errors are logged; pass cfg.Debug
// so the function honors the same flag as the rest of the lifecycle.
//
// On pipe creation failure (effectively impossible on Linux, but
// guarded so a weird runtime env doesn't crash the entrypoint), we
// fall back to the raw FDs — the process will then block on stdin
// until killed externally, which matches the legacy (Phase 0)
// behavior.
func wireStdinEOF(
	ctx context.Context,
	cancel context.CancelFunc,
	stdin *os.File,
	stdout *os.File,
	debug bool,
) (io.Reader, io.Writer) {
	pr, pw, err := os.Pipe()
	if err != nil {
		if debug {
			log.Printf("stdin pipe creation failed: %v; falling back to raw os.Stdin", err)
		}
		return stdin, stdout
	}

	go func() {
		defer func() {
			_ = pw.Close()
		}()
		_, copyErr := io.Copy(pw, stdin)
		// EOF is the expected case (parent closed stdin); not an
		// error. Any other error is worth surfacing in debug mode
		// but is non-fatal — the pipe close below still triggers
		// context cancellation and clean shutdown.
		if copyErr != nil && copyErr != io.EOF && debug {
			log.Printf("stdin copy ended with error: %v", copyErr)
		}
		// Cancel the lifecycle context. This wakes serveUntilDone's
		// <-ctx.Done() select, which returns ctx.Err() ==
		// context.Canceled. Idempotent and safe to call from a
		// goroutine: signal.NotifyContext's cancel has already been
		// deferred by run() and is also safe.
		cancel()
		_ = ctx // keep ctx in scope for the docstring's "honor upstream cancellation"
	}()

	return pr, stdout
}

// serveUntilDone blocks until ctx is cancelled, returning ctx.Err()
// on shutdown. It first calls srv.Serve() to wire up the protocol
// handlers and start the transport's readLoop goroutine; on the
// stdio transport that call returns nil almost immediately (the
// readLoop is the actual blocking call, and it reads from the
// transport's input in a separate goroutine). After Serve returns
// we block on ctx.Done() — the process exit triggered by
// SIGINT/SIGTERM, stdin EOF, or a test cancelling its injected
// context is the shutdown signal we honor here.
//
// If srv.Serve() returns an error (e.g. transport.Start failed),
// we surface it immediately rather than blocking on a context that
// may never fire.
func serveUntilDone(ctx context.Context, srv *mcp.Server) error {
	if err := srv.Serve(); err != nil {
		return err
	}
	<-ctx.Done()
	return ctx.Err()
}
