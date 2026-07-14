// listen.go — `--listen` flag validator for the `serve` subcommand.
//
// The default listen address is 127.0.0.1:8080 (locked Q28, see
// IMPLEMENTATION_PLAN.md Phase 18 and AGENTS.md "Hard rule #9").
// The validator parses a `host:port` string and returns the host
// and port as separate values. The bind-call later in main.go
// uses those values with net.Listen; a bind error fails the
// process with a clear stderr message — the binary does NOT
// silently flip to a different address on bind failure (no
// security-by-obscurity default flip).
//
// Accepted host values:
//
//   - IPv4 address (e.g. "127.0.0.1", "0.0.0.0", "192.168.1.50")
//   - IPv6 address (e.g. "[::1]", "[fe80::1]") — wrapped in
//     brackets in the listen string; parseListenFlag strips the
//     brackets so the host field returned to the caller is the
//     raw IPv6 literal
//   - DNS hostname (e.g. "localhost", "mcp.internal.example.com")
//   - The wildcard "*" — translated to "0.0.0.0" by the net
//     package itself; we accept it as parseable
//
// Rejected values:
//
//   - Missing port: "127.0.0.1" (no `:` separator) — the
//     "0.0.0.0" string itself has no port and is rejected for
//     the same reason
//   - Non-numeric port: "127.0.0.1:abc"
//   - Out-of-range port: "127.0.0.1:99999" (must be 1..65535)
//   - Port 0 is allowed (the kernel picks a free port; the
//     integration test in cli_test.go uses this so the test
//     doesn't have to hard-code 8080)
//   - Empty string: ""
//
// Parseable but warned at the help-text layer: "0.0.0.0:8080"
// parses cleanly and the bind succeeds — but the SECURITY
// block in `serve --help` and AGENTS.md warns against binding
// to 0.0.0.0 on a shared network. We do NOT block it at the
// parser; that's the operator's call to make.
package httptransport

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// ErrEmptyListen is returned by parseListenFlag when the input
// string is empty (or only whitespace). The serve subcommand
// treats this as a usage error and exits non-zero.
var ErrEmptyListen = errors.New("--listen: address is empty")

// ErrMissingPort is returned when the input has no `:` separator
// or the part after the last `:` is empty. The serve subcommand
// treats this as a usage error.
var ErrMissingPort = errors.New("--listen: address must be host:port (no port found)")

// ErrInvalidPort is returned when the port portion cannot be
// parsed as a uint16 in the 0..65535 range.
var ErrInvalidPort = errors.New("--listen: port must be a number in 0..65535")

// parseListenFlag parses a `host:port` listen address into its
// constituent parts. It returns the host (with IPv6 brackets
// stripped) and the integer port. On failure it returns a
// descriptive error; the caller is expected to print it to
// stderr and exit non-zero.
//
// This function does NOT validate that the address is bindable —
// that's net.Listen's job. The parser's job is to fail loud on
// malformed input (so the operator doesn't accidentally bind to
// a default-fallback address) and to keep the parse pure (no
// I/O, no DNS lookup, no permission checks).
//
// The function is the load-bearing piece of the "fails-closed"
// bind guarantee: a malformed listen string exits before any
// bind attempt, so the binary never reaches net.Listen with a
// silently-corrected address.
func parseListenFlag(s string) (host string, port int, err error) {
	// Trim surrounding whitespace — operators sometimes paste
	// "127.0.0.1:8080\n" from a config file.
	s = strings.TrimSpace(s)
	if s == "" {
		return "", 0, ErrEmptyListen
	}

	// IPv6 literal: "[::1]:8080" or "[::1]". The brackets are
	// part of the textual address; we need to strip them for
	// the host return value so net.Listen gets a raw literal.
	if strings.HasPrefix(s, "[") {
		end := strings.LastIndex(s, "]")
		if end < 0 {
			return "", 0, fmt.Errorf("--listen: malformed IPv6 address %q (missing closing bracket)", s)
		}
		host = s[1:end] // strip the [ and ]
		rest := s[end+1:]
		if rest == "" {
			// "[::1]" with no port — reject.
			return "", 0, ErrMissingPort
		}
		if !strings.HasPrefix(rest, ":") {
			return "", 0, fmt.Errorf("--listen: malformed IPv6 address %q (expected ':' after ']')", s)
		}
		portStr := rest[1:]
		port, err = parsePort(portStr)
		if err != nil {
			return "", 0, fmt.Errorf("--listen: %w", err)
		}
		return host, port, nil
	}

	// IPv4 / hostname: split on the LAST `:` so the host
	// portion can contain colons in pathological cases (it
	// shouldn't, but LastIndex is the conservative choice and
	// matches what net.SplitHostPort does internally).
	idx := strings.LastIndex(s, ":")
	if idx < 0 {
		return "", 0, ErrMissingPort
	}
	host = s[:idx]
	portStr := s[idx+1:]
	if host == "" {
		return "", 0, fmt.Errorf("--listen: host portion is empty in %q", s)
	}
	if portStr == "" {
		return "", 0, ErrMissingPort
	}
	port, err = parsePort(portStr)
	if err != nil {
		return "", 0, fmt.Errorf("--listen: %w", err)
	}
	return host, port, nil
}

// parsePort parses the port string and returns it as an int.
// Returns ErrInvalidPort on a non-numeric or out-of-range value.
// Port 0 IS allowed — the kernel picks a free ephemeral port;
// the test suite uses it to avoid hard-coding 8080 in concurrent
// CI runs.
func parsePort(s string) (int, error) {
	if s == "" {
		return 0, ErrMissingPort
	}
	// strconv.Atoi is the most permissive parse; we constrain
	// the range to uint16 ourselves so the error message
	// stays specific to the listen context.
	p, err := strconv.Atoi(s)
	if err != nil {
		return 0, ErrInvalidPort
	}
	if p < 0 || p > 65535 {
		return 0, ErrInvalidPort
	}
	return p, nil
}

// joinHostPort is the inverse of parseListenFlag — it takes
// the (host, port) pair and returns the canonical
// `host:port` string suitable for net.Listen. We use
// net.JoinHostPort so IPv6 literals get the bracket treatment
// automatically (e.g. "[::1]:8080" rather than "::1:8080" which
// would be ambiguous).
func joinHostPort(host string, port int) string {
	return net.JoinHostPort(host, strconv.Itoa(port))
}
