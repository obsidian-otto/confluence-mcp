// Phase 9 — main lifecycle tests.
//
// The cmd/mcp-confluence binary has a run() entrypoint that does
// load → build → serve. The tests here exercise:
//
//  1. Missing required env vars short-circuit runLifecycle with a
//     config-layer error (never reaching atlassian.New or server.New).
//     This is the fail-fast path the user sees at startup when they
//     forget to set ATLASSIAN_*_NAME / _EMAIL / _API_TOKEN.
//
//  2. With valid env vars, runLifecycle builds deps, calls srv.Serve(),
//     and blocks on ctx.Done(). Cancelling the context must cause
//     runLifecycle to return ctx.Err() within a short timeout —
//     proving the lifecycle wires the context through instead of
//     blocking forever.
//
//  3. The debug log line (when DEBUG=true) never includes the
//     ATLASSIAN_API_TOKEN value. The token is read once by
//     config.LoadFromEnv and must not appear in any log output.
//
// We test runLifecycle (not run) directly: run() wraps
// signal.NotifyContext around the process, which is hard to inject.
// runLifecycle is the seam the production run() delegates to after
// building its context.
//
// All tests chdir into t.TempDir() so the .env in the repo root
// does NOT leak into LoadFromEnv — that file holds real credentials
// that the test must never accidentally read. Tests also clear the
// three required env vars via t.Setenv("...", "") so a developer's
// shell environment does not accidentally satisfy the contract.
package main

import (
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"testing"
	"time"
)

// withCleanEnv clears the three required Atlassian env vars and
// changes into a fresh empty temp directory so neither the developer's
// shell env nor the repo's .env can leak into the test. t.Cleanup
// restores both on test exit.
func withCleanEnv(t *testing.T) {
	t.Helper()
	t.Setenv("ATLASSIAN_SITE_NAME", "")
	t.Setenv("ATLASSIAN_USER_EMAIL", "")
	t.Setenv("ATLASSIAN_API_TOKEN", "")
	// Empty DEBUG too — the developer may have DEBUG=true in their shell.
	t.Setenv("DEBUG", "")
	t.Chdir(t.TempDir())
}

// TestRunLifecycle_MissingEnvReturnsError asserts the fail-fast path:
// when none of the three required env vars are set (and no .env file
// is reachable), runLifecycle returns a non-nil error before doing any
// network or registration work.
//
// The error must mention at least one of the required env-var names
// so the user knows what to fix. config.validate emits a "FATAL:
// <NAME> is not set. ..." message per missing var, and the FIRST
// missing one is reported.
func TestRunLifecycle_MissingEnvReturnsError(t *testing.T) {
	withCleanEnv(t)

	// cwd is now an empty temp dir; the only remaining sources for
	// LoadFromEnv are the process env (all empty via t.Setenv) and
	// the test-binary's directory (no .env there either).
	err := runLifecycle(context.Background())
	if err == nil {
		t.Fatal("runLifecycle with no env / .env: expected error, got nil")
	}
	msg := err.Error()
	// The error must name one of the three required vars so the user
	// knows what to set. We don't assert which one — config.validate
	// reports them in a fixed order (site, email, token) but pinning
	// to a specific one would couple the test to that ordering.
	switch {
	case strings.Contains(msg, "ATLASSIAN_SITE_NAME"):
	case strings.Contains(msg, "ATLASSIAN_USER_EMAIL"):
	case strings.Contains(msg, "ATLASSIAN_API_TOKEN"):
	default:
		t.Errorf("error %q does not mention any required ATLASSIAN_* env var", msg)
	}
}

// TestRunLifecycle_NoEnvNeverTouchesNetwork asserts the missing-env
// path short-circuits before reaching atlassian.New or server.New.
// We rely on a behavioral signal: the error comes back within a
// tight deadline (no network attempt, no slow atlassian
// initialization). config.LoadFromEnv is pure stdlib — fast.
func TestRunLifecycle_NoEnvNeverTouchesNetwork(t *testing.T) {
	withCleanEnv(t)

	start := time.Now()
	err := runLifecycle(context.Background())
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected error from missing env")
	}
	// 1 second is a generous ceiling for stdlib file I/O. A network
	// attempt against a non-existent .atlassian.net host would take
	// orders of magnitude longer (DNS + connect timeout).
	if elapsed > 1*time.Second {
		t.Errorf("runLifecycle took %v with missing env — likely touched network", elapsed)
	}
}

// TestRunLifecycle_ValidEnvBlocksUntilCanceled is the positive-path
// test: with all three required env vars set, runLifecycle builds
// the atlassian client, builds the mcp.Server, and calls srv.Serve().
// On the stdio transport srv.Serve() returns nil quickly (it just
// wires up handlers and spawns the transport's readLoop goroutine);
// runLifecycle then blocks on ctx.Done(). We cancel the context and
// assert that runLifecycle returns ctx.Err() within a short timeout.
//
// Why we don't just assert "runLifecycle returns something":
// the test needs to distinguish "ctx-driven shutdown" from
// "srv.Serve failed early". The strongest signal is cancelling the
// context and verifying the returned error wraps context.Canceled —
// that proves the lifecycle honors the injected context end-to-end.
//
// NOTE: The transport's readLoop goroutine started by srv.Serve
// outlives the test (it reads from os.Stdin which is shared with
// go test). That's a known acceptable leak; the test binary exits
// at the end of `go test` and the goroutine dies with it.
func TestRunLifecycle_ValidEnvBlocksUntilCanceled(t *testing.T) {
	// We chdir into an empty temp dir so the repo's .env (which has
	// real-looking values for the user's actual Atlassian site) does
	// NOT contribute to LoadFromEnv. The token value set here is a
	// smoke-test placeholder — no real Atlassian host is contacted
	// in this test because the stdio transport blocks on stdin.
	t.Chdir(t.TempDir())
	t.Setenv("ATLASSIAN_SITE_NAME", "smoke-site")
	t.Setenv("ATLASSIAN_USER_EMAIL", "smoke@example.com")
	t.Setenv("ATLASSIAN_API_TOKEN", "smoke-token-not-a-real-secret")
	t.Setenv("DEBUG", "")

	ctx, cancel := context.WithCancel(context.Background())

	type result struct {
		err     error
		elapsed time.Duration
	}
	done := make(chan result, 1)
	go func() {
		start := time.Now()
		err := runLifecycle(ctx)
		done <- result{err: err, elapsed: time.Since(start)}
	}()

	// Give runLifecycle a brief moment to wire up deps + start srv.Serve.
	// 100 ms is generous; config + atlassian.New + server.New are
	// all in-process construction with no network.
	time.Sleep(100 * time.Millisecond)

	cancel()

	select {
	case r := <-done:
		if !errors.Is(r.err, context.Canceled) {
			t.Errorf("runLifecycle err = %v, want context.Canceled", r.err)
		}
		// Sanity bound: the lifecycle returned promptly after cancel.
		// This catches a regression where the ctx is no longer wired
		// through and runLifecycle only exits when srv.Serve() does.
		if r.elapsed > 2*time.Second {
			t.Errorf("runLifecycle took %v to return after ctx cancel — context not honored?", r.elapsed)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("runLifecycle did not return within 3s of ctx cancel")
	}
}

// TestRunLifecycle_ValidEnvNeverLogsToken is a defense-in-depth check
// that the debug log line (which DOES fire because we set DEBUG=true
// here) never includes the ATLASSIAN_API_TOKEN value. The token is
// only ever read by config.LoadFromEnv and held in memory; it must
// not appear in any log output.
//
// We capture log output by redirecting the standard logger via
// log.SetOutput. Reassigning os.Stderr directly does NOT work for
// log.Printf — the log package captured os.Stderr at init() time
// and ignores later reassignment. We also reassign os.Stderr as
// belt-and-braces so any future fmt.Fprintf(os.Stderr, ...) call is
// also captured.
func TestRunLifecycle_ValidEnvNeverLogsToken(t *testing.T) {
	t.Chdir(t.TempDir())
	const smokeToken = "TOK-CANARY-MUST-NOT-LEAK-7F2A"
	t.Setenv("ATLASSIAN_SITE_NAME", "smoke-site")
	t.Setenv("ATLASSIAN_USER_EMAIL", "smoke@example.com")
	t.Setenv("ATLASSIAN_API_TOKEN", smokeToken)
	t.Setenv("DEBUG", "true")

	logReader, logWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	origLogOutput := log.Writer()
	origStderr := os.Stderr
	log.SetOutput(logWriter)
	os.Stderr = logWriter
	t.Cleanup(func() {
		log.SetOutput(origLogOutput)
		os.Stderr = origStderr
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runLifecycle(ctx) }()

	// Let the debug log land.
	time.Sleep(100 * time.Millisecond)

	cancel()
	<-done

	// Close the writer so the reader sees EOF.
	_ = logWriter.Close()

	buf := make([]byte, 4096)
	n, _ := logReader.Read(buf)
	output := string(buf[:n])

	if strings.Contains(output, smokeToken) {
		t.Fatalf("stderr leaked the API token! Output:\n%s", output)
	}
	// The site and email are non-secret and may appear; the token must
	// not. We assert the rest of the debug line is present so we know
	// the log path actually ran (not that the test silently no-oped).
	if !strings.Contains(output, "smoke-site") {
		t.Errorf("expected debug log to mention site name; stderr was:\n%s", output)
	}
}
