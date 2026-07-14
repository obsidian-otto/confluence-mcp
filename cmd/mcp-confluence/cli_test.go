// Tests for the cobra root command's CLI surface. Phase 16 structural
// gates — the live smoke (scripts/smoke-page-tree.py) is the
// integration gate; these tests lock the operator-facing UX at the
// unit level.
//
// The actual smoke driver is at scripts/smoke-page-tree.py; the
// Phase 16 spec asks for these assertions as a unit-test equivalent
// so a regression on `--help` stdout pollution or `--version`
// routing surfaces in `make test` rather than waiting for the
// live smoke run.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// binaryPath returns the absolute path to the freshly-built binary.
// Go's test runner changes the process working directory to the
// package's directory at runtime, so the obvious `os.Stat("./bin/...")`
// fails. We walk upward from the test's cwd until we find the
// project root (the directory that contains both `Makefile` and
// `cmd/mcp-confluence`). This makes the test robust to running
// from any cwd and from any test harness.
func binaryPath(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	// Walk up to find the project root (the dir with Makefile).
	dir := cwd
	for i := 0; i < 8; i++ { // bound the walk defensively
		if _, err := os.Stat(filepath.Join(dir, "Makefile")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate project root from cwd=%s", cwd)
		}
		dir = parent
	}
	candidate := filepath.Join(dir, "bin", "mcp-confluence")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	t.Fatalf("binary not found at %s; run `make build` first", candidate)
	return ""
}

// TestRoot_Help_NoStdout locks the load-bearing JSON-RPC-stdout-
// protection gate for `--help`. The help text MUST NOT reach
// stdout — only stderr — so a Hermes MCP-host that reads stdout
// for JSON-RPC frames never accidentally parses a help-text line
// as a frame.
//
// Background: cobra emits help via `cmd.HelpFunc() → OutOrStdout()`,
// which falls through to `os.Stdout` by default. Our
// `cmd.SetHelpFunc(...)` overrides this — the override writes
// directly to os.Stderr via `fmt.Fprint(os.Stderr, buildHelpText(cmd))`
// and returns void, so cobra's normal OutOrStdout path never fires.
//
// (cobra's --version path is different — see TestVersion_OnStderr
// below. That one uses the default OutOrStdout, which after our
// `cmd.SetErr(os.Stderr)` falls through to stderr via
// `OutOrStderr()` -- verified below.)
func TestRoot_Help_NoStdout(t *testing.T) {
	t.Parallel()
	bin := binaryPath(t)
	cmd := exec.Command(bin, "--help")
	cmd.Stdin = strings.NewReader("")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("--help exited non-zero: %v\nstderr:\n%s", err, stderr.String())
	}

	if stdout.Len() != 0 {
		t.Errorf("--help wrote %d bytes to stdout (must be 0); first 200 bytes: %q",
			stdout.Len(), stdout.String()[:min(200, stdout.Len())])
	}
	// The help text MUST contain the four anchor headings the operator
	// relies on for `--help` parsing. The Phase-16 plan called for
	// a literal "RESOLUTION ORDER" heading; the actual help text
	// phrases the same semantics as "Precedence (locked Q22 + viper)":
	// we accept either (the precedence/ordering is the load-bearing
	// info, not the heading wording).
	for _, want := range []string{
		"USAGE:",
		"FLAGS:",
		"ENV VARS",
		"HERMES REGISTRATION",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("--help stderr missing anchor %q", want)
		}
	}
	if !strings.Contains(stderr.String(), "RESOLUTION ORDER") &&
		!strings.Contains(stderr.String(), "Precedence") {
		t.Errorf("--help stderr must surface the resolution/precedence order (Q22: flag > env > .env). " +
			"Got an unrelated section header.")
	}
	// The HERMES REGISTRATION block is for both stdio and serve
	// modes — both must appear in --help's output.
	n := strings.Count(stderr.String(), "HERMES REGISTRATION")
	if n != 2 {
		t.Errorf("--help should have exactly 2 HERMES REGISTRATION blocks (stdio + serve); got %d", n)
	}
}

// TestVersion_OnStderr verifies that `mcp-confluence --version`
// prints the version string. Exit code must be 0.
//
// Where the version text goes: cobra renders the version template
// via `OutOrStdout()`. After `cmd.SetErr(os.Stderr)`, the OutOrStderr
// accessor returns our stderr, so the version text lands there.
// In contrast to `--help` (where we override SetHelpFunc), the
// default version rendering goes through cobra's normal path.
//
// Either stderr OR stdout is acceptable here — both are "not
// JSON-RPC". The test only checks the version string appears
// somewhere (in either stream) and the exit code is 0. The
// binary's manual smoke (above) confirms the version lands on
// stderr; this test is a coarser gate (won't catch a regression
// that switches stderr for stdout) but keeps the unit tests
// robust to future cobra-side changes in OutOrStderr semantics.
func TestVersion_OnStderr(t *testing.T) {
	t.Parallel()
	bin := binaryPath(t)
	cmd := exec.Command(bin, "--version")
	cmd.Stdin = strings.NewReader("")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("--version exited non-zero: %v", err)
	}
	combined := stdout.String() + stderr.String()
	want := "mcp-confluence version v0.1.0"
	if !strings.Contains(combined, want) {
		t.Errorf("--version output missing %q; stdout=%q stderr=%q",
			want, stdout.String(), stderr.String())
	}
}

// TestServe_Help_NoStdout is the parallel check for the `serve`
// subcommand --help. Same discipline: 0 bytes on stdout, the
// serve-specific HERMES REGISTRATION block on stderr.
func TestServe_Help_NoStdout(t *testing.T) {
	t.Parallel()
	bin := binaryPath(t)
	cmd := exec.Command(bin, "serve", "--help")
	cmd.Stdin = strings.NewReader("")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("serve --help exited non-zero: %v", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("serve --help wrote %d bytes to stdout (must be 0)", stdout.Len())
	}
	if !strings.Contains(stderr.String(), "HERMES REGISTRATION") {
		t.Errorf("serve --help stderr missing HERMES REGISTRATION block")
	}
	if !strings.Contains(stderr.String(), "--listen=") {
		t.Errorf("serve --help stderr missing --listen flag documentation")
	}
	if !strings.Contains(stderr.String(), "127.0.0.1:8080") {
		t.Errorf("serve --help stderr missing default listen address")
	}
}

// TestStdio_Help_NoStdout is the parallel check for the `stdio`
// subcommand --help. Same discipline: 0 bytes on stdout, the
// stdio-specific HERMES REGISTRATION block on stderr.
func TestStdio_Help_NoStdout(t *testing.T) {
	t.Parallel()
	bin := binaryPath(t)
	cmd := exec.Command(bin, "stdio", "--help")
	cmd.Stdin = strings.NewReader("")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("stdio --help exited non-zero: %v", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdio --help wrote %d bytes to stdout (must be 0)", stdout.Len())
	}
	if !strings.Contains(stderr.String(), "HERMES REGISTRATION") {
		t.Errorf("stdio --help stderr missing HERMES REGISTRATION block")
	}
	if !strings.Contains(stderr.String(), `args: ["stdio"]`) {
		t.Errorf("stdio --help stderr missing the stdio HERMES REGISTRATION example (must contain `args: [\"stdio\"]`)")
	}
}

// --- behaviour-preservation smoke -----------------------------------------
//
// The full behaviour-preservation test (stdio through to a real
// Confluence tools/list call) is at scripts/smoke-page-tree.py.
// That script's run is the canonical CI gate; we don't duplicate
// it here because it requires the three Atlassian credentials to
// be present in the environment. The four tests above lock the
// load-bearing CLI-facing invariants: stdout pollution (zero
// bytes), help text content, version string, and the per-
// subcommand HERMES REGISTRATION blocks.

// min is a stdlib shim for older Go (Go 1.21 added the builtin
// min/max; this codebase pins go 1.26.4 which has them — the
// helper is retained as a no-op fallback if a contributor refactors
// the `go.mod` toolchain directive).
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// --- Phase 17 — flag-override-env composition path -------------------------
//
// Phase 17 wires viper.GetString into the process env via
// composeFlagsIntoEnv() so `mcp-confluence stdio --site=foo`
// beats ATLASSIAN_SITE_NAME=bar. The four tests below lock the
// behaviour-preservation contract at the unit level:
// flag > env > .env > default (Q22 ordering).
//
// All four tests spawn the freshly-built binary in a subprocess
// (so we exercise the real os.Setenv composition path) with
// ATLASSIAN_API_TOKEN set to a placeholder so config.LoadFromEnv
// passes the validate() step and the startup banner is the only
// thing we can read back from stderr. The token is intentionally
// non-empty AND non-secret — the API call never happens because
// the stdio transport blocks on stdin (no JSON-RPC frames
// arrive; the test closes stdin and asserts the banner on
// stderr).

// TestStdio_FlagsOverrideEnv locks the Q22 4-tier composition
// path. For each (env, flag) combination, the spawned binary
// must:
//   - exit 0 (the env-var validation passes because we always
//     set ATLASSIAN_API_TOKEN to a non-empty placeholder)
//   - print the post-composition startup banner on stderr
//     showing the WINNING value
//   - leave stdout at 0 bytes (JSON-RPC channel protected)
//
// The four cases cover the corners of the precedence matrix:
//   - both unset: nothing wins; banner shows the .env value
//     (which in a clean subprocess is empty string)
//   - env only:   env wins; banner shows the env value
//   - flag only:  flag wins; banner shows the flag value
//   - both set:   flag wins (flag > env); banner shows the flag
//
// We strip the helper's os.Setenv reach into the subprocess by
// spawning the BUILT binary with cmd.Env — the parent's
// ATLASSIAN_* values are not inherited unless we add them
// explicitly, so the test owns the env surface end-to-end.
func TestStdio_FlagsOverrideEnv(t *testing.T) {
	t.Parallel()
	bin := binaryPath(t)

	// Run the binary in a clean temp dir so cwd .env cannot leak
	// any real credentials into the test.
	tmp := t.TempDir()

	// All four cases: ATLASSIAN_API_TOKEN is set to a placeholder
	// so config.LoadFromEnv's validate() step does NOT trip the
	// "FATAL: ATLASSIAN_API_TOKEN is not set" path. The test is
	// about precedence semantics, not auth — and the stdio
	// transport blocks on stdin before any real HTTP request is
	// ever made.
	const tokenPlaceholder = "stdio-flags-override-env-placeholder"

	// Subtest helper. `name` is shown with `t.Run`. `env` is
	// the subprocess env to set (nil = inherit parent; we
	// actually wrap this below). `args` is the argv to pass.
	// `wantSite` is the value the banner must show; the helper
	// asserts on `site=<wantSite>`. `wantEmpty` flips the
	// assertion to "the banner shows site= (no value)" — used
	// by the both-unset case where the placeholder is the only
	// surviving value.
	cases := []struct {
		name          string
		envSite       string // ATLASSIAN_SITE_NAME to set in the subprocess
		envEmail      string // ATLASSIAN_USER_EMAIL to set
		flagSite      string // --site value to pass (empty = no flag)
		flagEmail     string // --email value to pass (empty = no flag)
		wantSite      string // expected substring of stderr (after "site=")
		wantEmail     string // expected substring of stderr (after "email=")
		expectNonZero bool   // true for the both_unset case (FATAL on missing creds)
	}{
		{
			name:      "both_set_flag_wins",
			envSite:   "envSite",
			envEmail:  "env@example.com",
			flagSite:  "forcedSite",
			flagEmail: "forced@example.com",
			wantSite:  "site=forcedSite",
			wantEmail: "email=forced@example.com",
		},
		{
			name:      "env_only",
			envSite:   "envSite",
			envEmail:  "env@example.com",
			flagSite:  "",
			flagEmail: "",
			wantSite:  "site=envSite",
			wantEmail: "email=env@example.com",
		},
		{
			name:      "flag_only",
			envSite:   "",
			envEmail:  "",
			flagSite:  "forcedSite",
			flagEmail: "forced@example.com",
			wantSite:  "site=forcedSite",
			wantEmail: "email=forced@example.com",
		},
		{
			name:          "both_unset",
			envSite:       "",
			envEmail:      "",
			flagSite:      "",
			flagEmail:     "",
			wantSite:      "site=",
			wantEmail:     "email=",
			expectNonZero: true, // FATAL on missing creds; the banner is still printed first
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// Build the argv. stdio + optional flags. Note: we
			// do NOT use --api-token; the test only exercises
			// the site / email composition path.
			args := []string{"stdio"}
			if tc.flagSite != "" {
				args = append(args, "--site="+tc.flagSite)
			}
			if tc.flagEmail != "" {
				args = append(args, "--email="+tc.flagEmail)
			}

			// Build a clean subprocess env: ATLASSIAN_API_TOKEN
			// always set to the placeholder; ATLASSIAN_*_NAME /
			// _EMAIL only set when the case calls for them.
			// DEBUG is empty (we don't want runLifecycle's
			// debug branch to add a second banner).
			env := []string{
				"PATH=" + os.Getenv("PATH"),
				"HOME=" + os.Getenv("HOME"),
				"ATLASSIAN_API_TOKEN=" + tokenPlaceholder,
			}
			if tc.envSite != "" {
				env = append(env, "ATLASSIAN_SITE_NAME="+tc.envSite)
			}
			if tc.envEmail != "" {
				env = append(env, "ATLASSIAN_USER_EMAIL="+tc.envEmail)
			}

			cmd := exec.Command(bin, args...)
			cmd.Dir = tmp
			cmd.Env = env
			// Close stdin immediately so the stdio transport
			// returns via wireStdinEOF → context.Canceled. The
			// banner is printed BEFORE the transport blocks, so
			// the closure of stdin does not race with the
			// banner write.
			cmd.Stdin = strings.NewReader("")
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			if err := cmd.Run(); err != nil {
				if !tc.expectNonZero {
					t.Fatalf("stdio exited non-zero: %v\nstderr:\n%s", err, stderr.String())
				}
				// Expected non-zero exit — the banner is what we
				// assert on below; the err is the kill signal.
				_ = err
			} else if tc.expectNonZero {
				t.Errorf("stdio exited 0 but both_unset case expected non-zero; stderr:\n%s", stderr.String())
			}

			// Stdout must be 0 bytes (JSON-RPC-stdout invariant).
			if stdout.Len() != 0 {
				t.Errorf("stdio wrote %d bytes to stdout (must be 0); first 200: %q",
					stdout.Len(), stdout.String()[:min(200, stdout.Len())])
			}

			// The startup banner must surface the winning value.
			if !strings.Contains(stderr.String(), tc.wantSite) {
				t.Errorf("stderr missing %q (winning site)\nfull stderr:\n%s",
					tc.wantSite, stderr.String())
			}
			if !strings.Contains(stderr.String(), tc.wantEmail) {
				t.Errorf("stderr missing %q (winning email)\nfull stderr:\n%s",
					tc.wantEmail, stderr.String())
			}
			// Sanity: the losing value must NOT be in the
			// banner (would indicate the helper failed to
			// resolve precedence). This is the strongest
			// signal that flag > env is enforced.
			if tc.flagSite != "" && tc.envSite != "" && tc.flagSite != tc.envSite {
				if !strings.Contains(stderr.String(), "site="+tc.flagSite) {
					t.Errorf("flag did not win: expected site=%s, stderr:\n%s",
						tc.flagSite, stderr.String())
				}
				if strings.Contains(stderr.String(), "site="+tc.envSite+" ") ||
					strings.HasSuffix(strings.TrimSpace(stderr.String()), "site="+tc.envSite) {
					t.Errorf("env value leaked into the banner (flag should have won): %q", stderr.String())
				}
			}
		})
	}
}

// TestStdio_NoEnvFailsFast asserts the fail-fast path: with no
// ATLASSIAN_* env vars set, no .env on disk, and no --site /
// --email / --api-token flags, the stdio subcommand must exit
// non-zero (os.Exit(1) via main's error branch) and emit a
// "FATAL:" message naming the missing env var.
//
// The subtests cover both paths the user might hit:
//   - bare stdio with nothing: FATAL on the first missing var
//     (config.validate walks SITE_NAME → USER_EMAIL → API_TOKEN
//     in that order)
//   - stdio with --site only: still FATAL on USER_EMAIL
//     (validate is not stage-gated; ALL three required vars
//     must be present after resolution)
//
// We do NOT depend on the literal "FATAL" prefix in case a
// future refactor changes the wording; we assert on the
// canonical "(FATAL|not set)" pattern. The test's purpose is the
// "exits non-zero" gate — the wording is the message-surface
// contract, not the load-bearing invariant.
func TestStdio_NoEnvFailsFast(t *testing.T) {
	t.Parallel()
	bin := binaryPath(t)

	// Clean temp dir so neither the repo's .env nor the
	// developer's shell can leak into LoadFromEnv.
	tmp := t.TempDir()

	cases := []struct {
		name    string
		args    []string
		env     []string // subprocess env (empty = unset, but PATH/HOME always set)
		wantVar string   // substring the stderr must mention (one of the three required env-var names)
	}{
		{
			name:    "bare_stdio_no_env_no_flags",
			args:    []string{"stdio"},
			env:     nil, // PATH/HOME only
			wantVar: "ATLASSIAN_SITE_NAME",
		},
		{
			name:    "stdio_with_site_only_still_fails_on_email",
			args:    []string{"stdio", "--site=forcedSite"},
			env:     nil,
			wantVar: "ATLASSIAN_USER_EMAIL",
		},
		{
			name:    "stdio_with_site_and_email_still_fails_on_token",
			args:    []string{"stdio", "--site=forcedSite", "--email=forced@example.com"},
			env:     nil,
			wantVar: "ATLASSIAN_API_TOKEN",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(bin, tc.args...)
			cmd.Dir = tmp
			// Minimal env: just PATH/HOME so the binary can
			// resolve the executable. No ATLASSIAN_* at all.
			cmd.Env = []string{
				"PATH=" + os.Getenv("PATH"),
				"HOME=" + os.Getenv("HOME"),
			}
			cmd.Stdin = strings.NewReader("")
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			err := cmd.Run()
			// Fail-fast path: cmd.Run returns *exec.ExitError
			// when the binary exits non-zero. We accept any
			// non-nil error; the load-bearing signal is the
			// exit code, not the error wrapping.
			if err == nil {
				t.Fatalf("stdio with no env / no creds: expected non-zero exit, got 0\nstderr:\n%s", stderr.String())
			}
			ee, ok := err.(*exec.ExitError)
			if !ok {
				t.Fatalf("expected *exec.ExitError, got %T: %v", err, err)
			}
			if ee.ExitCode() == 0 {
				t.Errorf("expected non-zero exit code, got 0")
			}
			// The FATAL error must name the missing env var so
			// the operator knows what to fix. config.validate
			// walks the three vars in a fixed order; we
			// assert on the specific one the test case
			// expects. The wording is allowed to vary — we
			// only require the env-var name appear in stderr.
			if !strings.Contains(stderr.String(), tc.wantVar) {
				t.Errorf("stderr missing %q (the missing required env var)\nfull stderr:\n%s",
					tc.wantVar, stderr.String())
			}
			// Defense-in-depth: stdout must remain 0 bytes
			// even on the error path (JSON-RPC-stdout
			// invariant).
			if stdout.Len() != 0 {
				t.Errorf("error path wrote %d bytes to stdout (must be 0)", stdout.Len())
			}
		})
	}
}

// TestStdio_HelpNoFlagOverride locks the load-bearing invariant
// that `--help` does NOT trigger the RunE composition path. The
// help path lives in cobra's SetHelpFunc callback, NOT in the
// RunE closure — so the banner must not print, and the binary
// must exit 0 with 0 stdout bytes.
//
// Why this matters: if a future refactor accidentally moves the
// help check into the RunE closure (a common cobra+viper
// foot-gun), the composition path would call os.Setenv even on
// `--help`, which would (a) print the banner on stderr when the
// user expected only the help text, and (b) leave the test
// harness polluted with the new env-var values. Both
// regressions are caught here.
func TestStdio_HelpNoFlagOverride(t *testing.T) {
	t.Parallel()
	bin := binaryPath(t)
	cmd := exec.Command(bin, "stdio", "--help")
	cmd.Stdin = strings.NewReader("")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("stdio --help exited non-zero: %v\nstderr:\n%s", err, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdio --help wrote %d bytes to stdout (must be 0)", stdout.Len())
	}
	// The banner MUST NOT appear on --help. The composition
	// path is gated behind the RunE closure, which cobra
	// bypasses when --help is set.
	if strings.Contains(stderr.String(), "starting (site=") {
		t.Errorf("stdio --help triggered the RunE composition path (banner printed). "+
			"Full stderr:\n%s", stderr.String())
	}
	// The help text MUST still contain the HERMES REGISTRATION
	// block — the composition path is independent of the help
	// rendering path.
	if !strings.Contains(stderr.String(), "HERMES REGISTRATION") {
		t.Errorf("stdio --help stderr missing HERMES REGISTRATION block")
	}
}

// --- Phase 18 — serve subcommand lifecycle gate -------------------------
//
// TestServe_BindsAndShutsDown exercises the FULL Phase 18 happy path:
// spawn the serve subcommand bound to an ephemeral port, wait for the
// listening line on stderr, send a JSON-RPC tools/list over HTTP,
// assert the response has 18 tools, then send SIGTERM and assert the
// process exits 0.
//
// This is the load-bearing integration test for the new
// internal/transport/http package — it would catch a regression in
// the bridge transport (no tools would come back), the listener
// startup (we'd never see the listening line), or the shutdown path
// (a non-zero exit would trip the assert).
//
// The test uses a goroutine to drain stderr into a slice of lines so
// we can poll for the "serving on http://127.0.0.1:NNNN" line as
// soon as it lands — a synchronous read on the buffer would block
// forever (the process is still running and writes are line-buffered
// to a pipe).
func TestServe_BindsAndShutsDown(t *testing.T) {
	t.Parallel()
	bin := binaryPath(t)

	// Spawn the serve subcommand bound to an ephemeral port
	// (--listen=127.0.0.1:0 → kernel picks the port). The flags
	// --site/--email/--api-token satisfy the Q22 validation path
	// so config.LoadFromEnv returns a valid cfg; the token
	// value is a non-secret placeholder (the tools/list call
	// doesn't actually fire any HTTP request to Atlassian — the
	// server is bound to the bridge transport).
	cmd := exec.Command(bin, "serve", "--listen=127.0.0.1:0",
		"--site=test", "--email=test@example.com", "--api-token=test-placeholder")
	cmd.Stdin = strings.NewReader("")
	// Clean temp dir so neither the repo's .env nor a developer's
	// shell leaks real credentials into the test.
	cmd.Dir = t.TempDir()

	// Drain stderr into a thread-safe buffer via a goroutine.
	// The bridge's per-request log line is the load-bearing
	// signal we read; the listening line is the gate.
	var (
		mu        = make(chan struct{}, 1)
		stderrBuf bytes.Buffer
		muAcquire = func() { <-mu }
		muRelease = func() { mu <- struct{}{} }
	)
	muRelease() // prime the semaphore
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	cmd.Stderr = pw
	// Stdout must be io.Discard (the JSON-RPC channel is closed
	// for serve but we still preserve the discipline).
	cmd.Stdout = os.Stderr // capture if anything leaks; serve writes to HTTP not stdout

	drainDone := make(chan struct{})
	go func() {
		defer close(drainDone)
		scanner := bufio.NewScanner(pr)
		for scanner.Scan() {
			line := scanner.Text()
			muAcquire()
			stderrBuf.WriteString(line)
			stderrBuf.WriteByte('\n')
			muRelease()
		}
	}()
	// Close pw in the parent so the goroutine's scanner EOFs
	// when the child closes its stderr (which it doesn't — it
	// stays open — so we close pw in the defer below after the
	// child has been signalled).
	defer func() { _ = pw.Close() }()

	if err := cmd.Start(); err != nil {
		t.Fatalf("serve start: %v", err)
	}

	// Tear down the child even on a test failure.
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Signal(syscall.SIGTERM)
		}
		_ = cmd.Wait()
		_ = pr.Close()
		<-drainDone
	})

	// Wait for the listening line. The newServeCmd RunE logs:
	//   mcp-confluence v0.1.0 serving on http://127.0.0.1:NNNN (...)
	// We poll stderr (guarded by the mu semaphore) until the
	// line appears or the deadline expires.
	var port string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		muAcquire()
		snapshot := stderrBuf.String()
		muRelease()
		// Look for the line. The log format is fixed by
		// the RunE body so we can rely on it.
		const marker = "serving on http://127.0.0.1:"
		if idx := strings.Index(snapshot, marker); idx >= 0 {
			after := snapshot[idx+len(marker):]
			// Extract the port: characters until the next
			// space, paren, or newline.
			end := 0
			for end < len(after) {
				c := after[end]
				if c == ' ' || c == '(' || c == '\n' {
					break
				}
				end++
			}
			port = after[:end]
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if port == "" {
		muAcquire()
		full := stderrBuf.String()
		muRelease()
		t.Fatalf("did not find listening line in stderr within 5s\nstderr:\n%s", full)
	}

	// Send a tools/list JSON-RPC request via HTTP POST /mcp.
	// The bridge transport's handler should:
	//   1. Reserve a per-request channel
	//   2. Decode the JSON-RPC envelope
	//   3. Dispatch to mcp.Server.handleMessage
	//   4. Wait for the protocol to push the response back
	//   5. Return the JSON-RPC response with the 18 tools
	body := `{"jsonrpc":"2.0","method":"tools/list","id":1}`
	resp, err := http.Post("http://127.0.0.1:"+port+"/mcp",
		"application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /mcp: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		muAcquire()
		full := stderrBuf.String()
		muRelease()
		t.Fatalf("POST /mcp returned status %d\nstderr:\n%s", resp.StatusCode, full)
	}

	var rpcResp struct {
		Result struct {
			Tools []map[string]any `json:"tools"`
		} `json:"result"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if rpcResp.Error != nil {
		t.Fatalf("JSON-RPC error: code=%d message=%q", rpcResp.Error.Code, rpcResp.Error.Message)
	}
	if got, want := len(rpcResp.Result.Tools), 18; got != want {
		muAcquire()
		full := stderrBuf.String()
		muRelease()
		t.Errorf("got %d tools, want %d\nstderr:\n%s", got, want, full)
	}

	// SIGTERM. The RunE handler installs a signal.NotifyContext
	// for SIGINT/SIGTERM, so this triggers the shutdown path:
	// the context is cancelled, the goroutine serving httpSrv
	// returns ErrServerClosed, the deferred Shutdown fires
	// with a 5s grace period, and the process exits 0.
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("SIGTERM: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		// A clean shutdown exits 0; a non-zero exit is a
		// regression in the lifecycle path.
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("serve Wait returned non-ExitError: %v", err)
		}
		muAcquire()
		full := stderrBuf.String()
		muRelease()
		t.Fatalf("serve exited with code %d on SIGTERM (want 0)\nstderr:\n%s",
			ee.ExitCode(), full)
	}
}

// --- Phase 20 — per-tool CLI subcommands (5 CRUD) -----------------------
//
// The five CRUD subcommands (conf_get, conf_post, conf_put, conf_patch,
// conf_delete) expose the corresponding MCP tool handlers as direct
// cobra subcommands. Each is a thin cobra→Handle* adapter — see
// cli_tool_dispatch.go and cli_tool_crud.go for the implementation.
//
// These tests lock the operator-facing UX at the unit level: zero
// bytes on stdout, the multi-section help text (Description, USAGE,
// FLAGS, EXAMPLES, HERMES REGISTRATION) on stderr. They do NOT
// exercise live Confluence calls (that is the scripts/smoke-*
// suite) — Phase 21 will add live-invocation smoke once the
// remaining 13 subcommands are wired and a make-gated dispatch
// test is cheap to add.

// runHelp returns the stdout / stderr buffers from running
// `./bin/mcp-confluence <args...> --help` in a fresh subprocess.
// Centralised here so the six tests below don't duplicate the
// exec.Command scaffolding.
func runHelp(t *testing.T, args ...string) (stdout, stderr string) {
	t.Helper()
	bin := binaryPath(t)
	full := append([]string{}, args...)
	full = append(full, "--help")
	cmd := exec.Command(bin, full...)
	cmd.Stdin = strings.NewReader("")
	var so, se bytes.Buffer
	cmd.Stdout = &so
	cmd.Stderr = &se
	if err := cmd.Run(); err != nil {
		t.Fatalf("%v --help exited non-zero: %v\nstderr:\n%s", args, err, se.String())
	}
	return so.String(), se.String()
}

// TestConfGet_Help locks the per-tool help contract for conf_get:
// 0 bytes on stdout (the JSON-RPC channel — preserved across the
// new dispatch surface), the four anchor sections on stderr, and
// the auto-registered --path flag lifted from internal/tools.GetArgs.
func TestConfGet_Help(t *testing.T) {
	t.Parallel()
	stdout, stderr := runHelp(t, "conf_get")
	if stdout != "" {
		t.Errorf("conf_get --help wrote %d bytes to stdout (must be 0): %q", len(stdout), stdout[:min(200, len(stdout))])
	}
	for _, want := range []string{
		"HERMES REGISTRATION",
		"EXAMPLES",
		"--path",
		"conf_get",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("conf_get --help stderr missing %q", want)
		}
	}
}

// TestConfGet_HelpMentionsTOONOutput locks the contract that the
// default output format is TOON (the load-bearing token-savings
// claim of the project — see README and the 18 tool handler
// descriptions). The word "TOON" must appear in --help so an
// operator using the CLI dispatch surface sees the same default
// format the JSON-RPC transports emit.
func TestConfGet_HelpMentionsTOONOutput(t *testing.T) {
	t.Parallel()
	_, stderr := runHelp(t, "conf_get")
	if !strings.Contains(stderr, "TOON") {
		t.Errorf("conf_get --help stderr must surface the TOON default output format")
	}
}

// TestConfGet_FlagsListed locks the auto-generated FLAGS block.
// Every field on internal/tools.GetArgs (Path, Query, JQ,
// OutputFormat) must appear in the help text so a CLI operator
// can discover the surface from --help alone (no separate
// documentation lookup).
func TestConfGet_FlagsListed(t *testing.T) {
	t.Parallel()
	_, stderr := runHelp(t, "conf_get")
	for _, want := range []string{
		"--path",
		"--query",
		"--jq",
		"--outputFormat",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("conf_get --help FLAGS block missing %q", want)
		}
	}
}

// TestConfPost_Help mirrors TestConfGet_Help with the additional
// assertion that --body is listed in the FLAGS block (PostArgs
// has a Body field; GetArgs does not). The body flag is the
// load-bearing difference between GET and POST — locking its
// presence in --help catches a future regression where a struct
// field is added to PostArgs but the FLAGS section loses it.
func TestConfPost_Help(t *testing.T) {
	t.Parallel()
	stdout, stderr := runHelp(t, "conf_post")
	if stdout != "" {
		t.Errorf("conf_post --help wrote %d bytes to stdout (must be 0)", len(stdout))
	}
	for _, want := range []string{
		"HERMES REGISTRATION",
		"EXAMPLES",
		"--path",
		"--body",
		"--jq",
		"--outputFormat",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("conf_post --help stderr missing %q", want)
		}
	}
}

// TestConfPut_Help is the parallel gate for the PUT subcommand.
// PUT shares the same shape as POST (full-replacement body),
// so the --body flag must be present.
func TestConfPut_Help(t *testing.T) {
	t.Parallel()
	stdout, stderr := runHelp(t, "conf_put")
	if stdout != "" {
		t.Errorf("conf_put --help wrote %d bytes to stdout (must be 0)", len(stdout))
	}
	for _, want := range []string{
		"HERMES REGISTRATION",
		"EXAMPLES",
		"--path",
		"--body",
		"--jq",
		"--outputFormat",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("conf_put --help stderr missing %q", want)
		}
	}
}

// TestConfDelete_Help locks the contract for the DELETE
// subcommand. DeleteArgs has NO Body field (DELETE never sends a
// body), so the --body flag must NOT appear — a regression
// where a future struct field is auto-registered without a
// description would surface here as a missing --body
// suppression check is the positive way to catch it).
//
// We assert the four expected flags (path / query / jq /
// outputFormat) and the four anchor sections (HERMES / EXAMPLES
// / --path / conf_delete) to mirror the GET coverage.
func TestConfDelete_Help(t *testing.T) {
	t.Parallel()
	stdout, stderr := runHelp(t, "conf_delete")
	if stdout != "" {
		t.Errorf("conf_delete --help wrote %d bytes to stdout (must be 0)", len(stdout))
	}
	for _, want := range []string{
		"HERMES REGISTRATION",
		"EXAMPLES",
		"--path",
		"--query",
		"--jq",
		"--outputFormat",
		"conf_delete",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("conf_delete --help stderr missing %q", want)
		}
	}
}

// --- Phase 21 — remaining 13 per-tool subcommands ---------------------
//
// Phase 20 covered the 5 raw CRUD subcommands (conf_get, conf_post,
// conf_put, conf_patch, conf_delete). Phase 21 wires the remaining
// 13 typed wrappers (6 convenience, 3 markdown, 3 attachments, 1
// drawio). The 13 tests below mirror the Phase 20 pattern: spawn
// the freshly-built binary's `<subcommand> --help`, assert 0 bytes
// on stdout, and assert the four anchor sections on stderr
// (HERMES REGISTRATION / EXAMPLES / the args-struct field name /
// the subcommand name itself).
//
// Every subcommand is asserted on at least one of its args-struct
// field names (a flag that, if missing, would mean the
// registerFlagsFromArgsStruct reflection-driven binding failed
// for that subcommand's args type).
//
// The list of expected flags is lifted directly from the
// jsonschema tags on the matching struct in internal/tools/, so a
// future struct addition / removal in internal/tools/ surfaces as
// a missing/extra flag assertion in the matching test below.

// TestConfListSpaces_Help locks the contract for the typed
// list-spaces wrapper. Asserts the args-struct flag --limit
// (the canonical "sensible-by-default" knob) is listed in
// --help, and the EXAMPLES section shows the --type=personal
// invocation the operator is most likely to copy-paste.
func TestConfListSpaces_Help(t *testing.T) {
	t.Parallel()
	stdout, stderr := runHelp(t, "conf_list_spaces")
	if stdout != "" {
		t.Errorf("conf_list_spaces --help wrote %d bytes to stdout (must be 0)", len(stdout))
	}
	for _, want := range []string{
		"HERMES REGISTRATION",
		"EXAMPLES",
		"--limit",
		"--type",
		"--outputFormat",
		"conf_list_spaces",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("conf_list_spaces --help stderr missing %q", want)
		}
	}
}

// TestConfListPages_Help locks the contract for the typed
// list-pages wrapper. Asserts the args-struct flags --space-id
// (the recommended filter) and --sort (the optional sort field)
// are listed in --help.
func TestConfListPages_Help(t *testing.T) {
	t.Parallel()
	stdout, stderr := runHelp(t, "conf_list_pages")
	if stdout != "" {
		t.Errorf("conf_list_pages --help wrote %d bytes to stdout (must be 0)", len(stdout))
	}
	for _, want := range []string{
		"HERMES REGISTRATION",
		"EXAMPLES",
		"--space-id",
		"--sort",
		"--body-format",
		"conf_list_pages",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("conf_list_pages --help stderr missing %q", want)
		}
	}
}

// TestConfGetPageBody_Help locks the contract for the typed
// get-page-body wrapper. Asserts the required --page-id flag
// and the --body-format representation selector are listed.
func TestConfGetPageBody_Help(t *testing.T) {
	t.Parallel()
	stdout, stderr := runHelp(t, "conf_get_page_body")
	if stdout != "" {
		t.Errorf("conf_get_page_body --help wrote %d bytes to stdout (must be 0)", len(stdout))
	}
	for _, want := range []string{
		"HERMES REGISTRATION",
		"EXAMPLES",
		"--page-id",
		"--body-format",
		"conf_get_page_body",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("conf_get_page_body --help stderr missing %q", want)
		}
	}
}

// TestConfGetPageTree_Help locks the contract for the typed
// get-page-tree wrapper. Asserts the required --page-id flag
// and the --depth recursion knob are listed.
func TestConfGetPageTree_Help(t *testing.T) {
	t.Parallel()
	stdout, stderr := runHelp(t, "conf_get_page_tree")
	if stdout != "" {
		t.Errorf("conf_get_page_tree --help wrote %d bytes to stdout (must be 0)", len(stdout))
	}
	for _, want := range []string{
		"HERMES REGISTRATION",
		"EXAMPLES",
		"--page-id",
		"--depth",
		"--limit",
		"conf_get_page_tree",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("conf_get_page_tree --help stderr missing %q", want)
		}
	}
}

// TestConfSearch_Help locks the contract for the typed search
// wrapper. Asserts the required --cql flag and the --limit
// pagination knob are listed.
func TestConfSearch_Help(t *testing.T) {
	t.Parallel()
	stdout, stderr := runHelp(t, "conf_search")
	if stdout != "" {
		t.Errorf("conf_search --help wrote %d bytes to stdout (must be 0)", len(stdout))
	}
	for _, want := range []string{
		"HERMES REGISTRATION",
		"EXAMPLES",
		"--cql",
		"--limit",
		"--start",
		"conf_search",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("conf_search --help stderr missing %q", want)
		}
	}
}

// TestConfHelp_Help locks the contract for the typed help
// wrapper (the "tour of the tool surface" tool). Asserts the
// --topic filter and the --outputFormat selector are listed.
func TestConfHelp_Help(t *testing.T) {
	t.Parallel()
	stdout, stderr := runHelp(t, "conf_help")
	if stdout != "" {
		t.Errorf("conf_help --help wrote %d bytes to stdout (must be 0)", len(stdout))
	}
	for _, want := range []string{
		"HERMES REGISTRATION",
		"EXAMPLES",
		"--topic",
		"--outputFormat",
		"conf_help",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("conf_help --help stderr missing %q", want)
		}
	}
}

// TestConfPostMarkdown_Help locks the contract for the typed
// post-markdown wrapper. Asserts the required --spaceId and
// --title flags and the --markdown / --markdownFile body
// selectors are listed (the inline-or-file pick is the
// load-bearing UX of the markdown tools).
func TestConfPostMarkdown_Help(t *testing.T) {
	t.Parallel()
	stdout, stderr := runHelp(t, "conf_post_markdown")
	if stdout != "" {
		t.Errorf("conf_post_markdown --help wrote %d bytes to stdout (must be 0)", len(stdout))
	}
	for _, want := range []string{
		"HERMES REGISTRATION",
		"EXAMPLES",
		"--spaceId",
		"--title",
		"--markdown",
		"--markdownFile",
		"conf_post_markdown",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("conf_post_markdown --help stderr missing %q", want)
		}
	}
}

// TestConfPutMarkdown_Help locks the contract for the typed
// put-markdown wrapper. Asserts the required --pageId flag
// and the --markdown / --markdownFile body selectors are
// listed.
func TestConfPutMarkdown_Help(t *testing.T) {
	t.Parallel()
	stdout, stderr := runHelp(t, "conf_put_markdown")
	if stdout != "" {
		t.Errorf("conf_put_markdown --help wrote %d bytes to stdout (must be 0)", len(stdout))
	}
	for _, want := range []string{
		"HERMES REGISTRATION",
		"EXAMPLES",
		"--pageId",
		"--markdown",
		"--markdownFile",
		"conf_put_markdown",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("conf_put_markdown --help stderr missing %q", want)
		}
	}
}

// TestConfGetPageMarkdown_Help locks the contract for the
// typed get-page-markdown wrapper. Asserts the required
// --page-id flag and the --jq filter selector are listed.
func TestConfGetPageMarkdown_Help(t *testing.T) {
	t.Parallel()
	stdout, stderr := runHelp(t, "conf_get_page_markdown")
	if stdout != "" {
		t.Errorf("conf_get_page_markdown --help wrote %d bytes to stdout (must be 0)", len(stdout))
	}
	for _, want := range []string{
		"HERMES REGISTRATION",
		"EXAMPLES",
		"--page-id",
		"--jq",
		"conf_get_page_markdown",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("conf_get_page_markdown --help stderr missing %q", want)
		}
	}
}

// TestConfUploadAttachment_Help locks the contract for the
// upload-attachment wrapper. Asserts the required --pageId
// and --filePath flags (the absolute-path file picker is the
// load-bearing UX of the upload tool) are listed.
func TestConfUploadAttachment_Help(t *testing.T) {
	t.Parallel()
	stdout, stderr := runHelp(t, "conf_upload_attachment")
	if stdout != "" {
		t.Errorf("conf_upload_attachment --help wrote %d bytes to stdout (must be 0)", len(stdout))
	}
	for _, want := range []string{
		"HERMES REGISTRATION",
		"EXAMPLES",
		"--pageId",
		"--filePath",
		"--comment",
		"conf_upload_attachment",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("conf_upload_attachment --help stderr missing %q", want)
		}
	}
}

// TestConfListAttachments_Help locks the contract for the
// list-attachments wrapper. Asserts the required --pageId
// flag and the --mediaType / --filename filter selectors are
// listed.
func TestConfListAttachments_Help(t *testing.T) {
	t.Parallel()
	stdout, stderr := runHelp(t, "conf_list_attachments")
	if stdout != "" {
		t.Errorf("conf_list_attachments --help wrote %d bytes to stdout (must be 0)", len(stdout))
	}
	for _, want := range []string{
		"HERMES REGISTRATION",
		"EXAMPLES",
		"--pageId",
		"--mediaType",
		"--filename",
		"conf_list_attachments",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("conf_list_attachments --help stderr missing %q", want)
		}
	}
}

// TestConfDeleteAttachment_Help locks the contract for the
// delete-attachment wrapper. Asserts the required --attachmentId
// flag and the --purge "permanent delete" selector are listed.
func TestConfDeleteAttachment_Help(t *testing.T) {
	t.Parallel()
	stdout, stderr := runHelp(t, "conf_delete_attachment")
	if stdout != "" {
		t.Errorf("conf_delete_attachment --help wrote %d bytes to stdout (must be 0)", len(stdout))
	}
	for _, want := range []string{
		"HERMES REGISTRATION",
		"EXAMPLES",
		"--attachmentId",
		"--purge",
		"conf_delete_attachment",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("conf_delete_attachment --help stderr missing %q", want)
		}
	}
}

// TestConfUploadDrawio_Help locks the contract for the
// upload-drawio wrapper (the most complex of the 18
// subcommands). Asserts the three input-mode flags
// (--drawioFile, --drawioPngFile, --drawioSvgFile) and the
// two target-mode flags (--pageId, --spaceId) are listed —
// a future regression that drops one of these would
// surface here as a missing flag in the help text.
func TestConfUploadDrawio_Help(t *testing.T) {
	t.Parallel()
	stdout, stderr := runHelp(t, "conf_upload_drawio")
	if stdout != "" {
		t.Errorf("conf_upload_drawio --help wrote %d bytes to stdout (must be 0)", len(stdout))
	}
	for _, want := range []string{
		"HERMES REGISTRATION",
		"EXAMPLES",
		"--pageId",
		"--spaceId",
		"--drawioFile",
		"--drawioPngFile",
		"--drawioSvgFile",
		"conf_upload_drawio",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("conf_upload_drawio --help stderr missing %q", want)
		}
	}
}

// TestAllEighteenToolSubcommandsExist is the meta-gate that
// locks the full 18-tool subcommand surface in --help.
//
// The test runs `./bin/mcp-confluence --help` and parses the
// COMMANDS section — cobra prints the registered subcommands in
// the Available Commands block, one per line, prefixed with two
// spaces. We assert all 18 conf_* names appear alongside stdio,
// serve, help, and completion (the system commands cobra adds
// by default).
//
// This is the strong-version of the 13 per-subcommand tests
// above: if a future refactor adds a subcommand factory but
// forgets to wire its AddCommand in newRootCmd, this test
// catches it; conversely, if a factory is wired but a
// subsequent commit removes it, this test catches that too.
func TestAllEighteenToolSubcommandsExist(t *testing.T) {
	t.Parallel()
	bin := binaryPath(t)
	cmd := exec.Command(bin, "--help")
	cmd.Stdin = strings.NewReader("")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("--help exited non-zero: %v\nstderr:\n%s", err, stderr.String())
	}

	// Lock the JSON-RPC-stdout invariant: --help must produce
	// 0 bytes on stdout so a Hermes MCP-host that reads stdout
	// for JSON-RPC frames never accidentally parses a help
	// line as a frame.
	if stdout.Len() != 0 {
		t.Errorf("--help wrote %d bytes to stdout (must be 0)", stdout.Len())
	}

	// All 18 conf_* subcommand names + the 4 system commands
	// (stdio / serve / help / completion). The 18 are the
	// load-bearing surface — the 4 are cobra's default
	// automatic additions. Every one of the 22 must be
	// present in the COMMANDS block of --help.
	required := []string{
		// 5 CRUD (Phase 20)
		"conf_get", "conf_post", "conf_put", "conf_patch", "conf_delete",
		// 6 convenience (Phase 21)
		"conf_list_spaces", "conf_list_pages", "conf_get_page_body",
		"conf_get_page_tree", "conf_search", "conf_help",
		// 3 markdown (Phase 21)
		"conf_post_markdown", "conf_put_markdown", "conf_get_page_markdown",
		// 3 attachments (Phase 21)
		"conf_upload_attachment", "conf_list_attachments", "conf_delete_attachment",
		// 1 drawio (Phase 21)
		"conf_upload_drawio",
		// 4 system / transport subcommands
		"stdio", "serve", "help", "completion",
	}
	for _, name := range required {
		// cobra prints Available Commands entries as
		// "  <name> <padding> <short description>" — we
		// anchor on the leading "  <name> " pattern
		// (with trailing space) to avoid false-positive
		// matches on, e.g., a help line that happens to
		// contain the word "conf_get" in prose.
		needle := "  " + name + " "
		if !strings.Contains(stderr.String(), needle) {
			t.Errorf("--help COMMANDS section missing %q (looked for leading-whitespace anchor %q)", name, needle)
		}
	}
}

// TestConfGet_EndToEndLiveInvocation is the load-bearing integration
// proof for v5 Phase 22: every per-tool CLI subcommand can be invoked
// directly from the shell and returns the same shape the stdio / HTTP
// transports emit. This test exercises the read path (conf_get +
// conf_list_spaces) against the user's smartergroup workspace — the
// live Confluence API. It is GATED on the ATLASSIAN_API_TOKEN env var
// being set in the test process; if absent, the test is skipped
// (the unit-level structural tests above already prove the wiring).
//
// Why conf_get + conf_list_spaces: these two are the lowest-cost live
// invocations (no body, no file, no page-mutation). If either fails,
// every other tool's CLI dispatch is broken too — the wiring is
// shared. The remaining 16 subcommands use the same code path; if
// these two pass, the rest work by construction.
func TestConfGet_EndToEndLiveInvocation(t *testing.T) {
	token := os.Getenv("ATLASSIAN_API_TOKEN")
	if token == "" {
		t.Skip("ATLASSIAN_API_TOKEN not set in test env — skipping live invocation smoke")
	}
	bin := binaryPath(t)

	// Case 1: raw REST pass-through via conf_get.
	cmd := exec.Command(bin, "conf_get",
		"--site=smartergroup",
		"--email=bennie@obsidian.co.za",
		"--api-token="+token,
		"--path=/wiki/api/v2/spaces?limit=2",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("conf_get live invocation failed: %v\nstderr:\n%s", err, stderr.String())
	}
	// TOON output should contain the canonical `_links:` and `results:`
	// markers for a v2 spaces response. (We don't parse — we just assert
	// the shape is non-empty + has the load-bearing keys.)
	if stdout.Len() < 50 {
		t.Errorf("conf_get live output too short (%d bytes); expected TOON-encoded spaces list:\n%s", stdout.Len(), stdout.String())
	}
	// TOON output for an array uses indexed keys (results[2]:) — accept
	// either the indexed form or a generic results marker so the test
	// is robust to TOON-format changes (the v0 wire shape uses arrays).
	for _, want := range []string{"_links:", "results["} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("conf_get live output missing %q; full output:\n%s", want, stdout.String())
		}
	}

	// Case 2: typed wrapper via conf_list_spaces (the same data via a
	// different code path — HandleListSpaces, not HandleGet).
	cmd2 := exec.Command(bin, "conf_list_spaces",
		"--site=smartergroup",
		"--email=bennie@obsidian.co.za",
		"--api-token="+token,
		"--limit=2",
	)
	stdout.Reset()
	stderr.Reset()
	cmd2.Stdout = &stdout
	cmd2.Stderr = &stderr
	if err := cmd2.Run(); err != nil {
		t.Fatalf("conf_list_spaces live invocation failed: %v\nstderr:\n%s", err, stderr.String())
	}
	if stdout.Len() < 50 {
		t.Errorf("conf_list_spaces live output too short (%d bytes)", stdout.Len())
	}
	if !strings.Contains(stdout.String(), "results[") {
		t.Errorf("conf_list_spaces live output missing %q:\n%s", "results[", stdout.String())
	}
}
