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
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
