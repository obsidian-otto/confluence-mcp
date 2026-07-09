package tools

import (
	"bytes"
	_ "embed"
	"regexp"
	"testing"
)

//go:embed upstream.atlassian.api.tool.ts
var upstreamToolTSSource []byte

// TestDescriptionConstantsMatchUpstream asserts that the five
// CONF_*_DESCRIPTION constants in descriptions.go are byte-identical
// to the runtime-evaluated value of the corresponding
// `CONF_*_DESCRIPTION` constant in the upstream
// `@aashari/mcp-server-atlassian-confluence` v3.3.0 source
// (src/tools/atlassian.api.tool.ts, lines 127-223).
//
// The upstream source file is vendored (unmodified) at
// internal/tools/upstream.atlassian.api.tool.ts and embedded at
// compile time via `go:embed`. The test parses each upstream
// template-literal constant, JS-evaluates its escape sequences (the
// only ones used in this file are \\`, \\\\, \\n, and line-continuation
// backslash-newline), and compares the result against our const.
//
// Drift between the constants and the upstream wording would mean a
// stale description or a copy-paste mistake; this test fails fast.
func TestDescriptionConstantsMatchUpstream(t *testing.T) {
	t.Parallel()

	expected := parseUpstreamDescriptionConstants(t, upstreamToolTSSource)

	cases := []struct {
		name string
		got  string
	}{
		{"GET", CONF_GET_DESCRIPTION},
		{"POST", CONF_POST_DESCRIPTION},
		{"PUT", CONF_PUT_DESCRIPTION},
		{"PATCH", CONF_PATCH_DESCRIPTION},
		{"DELETE", CONF_DELETE_DESCRIPTION},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			exp, ok := expected[c.name]
			if !ok {
				t.Fatalf("upstream source contains no CONF_%s_DESCRIPTION", c.name)
			}
			if c.got != exp {
				// Use byte-level diff to localise the drift.
				for i := 0; i < len(exp) && i < len(c.got); i++ {
					if exp[i] != c.got[i] {
						t.Fatalf(
							"CONF_%s_DESCRIPTION drift at byte %d:\n  expected (upstream): %q...\n  got:                 %q...\n  expected len=%d, got len=%d",
							c.name, i, snippet(exp, i), snippet(c.got, i), len(exp), len(c.got),
						)
					}
				}
				t.Fatalf(
					"CONF_%s_DESCRIPTION length mismatch: expected %d bytes, got %d bytes; trailing diff: expected suffix=%q got suffix=%q",
					c.name, len(exp), len(c.got), tail(exp), tail(c.got),
				)
			}
		})
	}
}

// TestAllUpstreamDescriptionsCovered is a guardrail: if the upstream
// adds a sixth tool with its own CONF_*_DESCRIPTION, this test fails
// so we know to add a matching constant + Go const.
func TestAllUpstreamDescriptionsCovered(t *testing.T) {
	t.Parallel()

	expected := parseUpstreamDescriptionConstants(t, upstreamToolTSSource)
	got := map[string]bool{
		"GET":    true,
		"POST":   true,
		"PUT":    true,
		"PATCH":  true,
		"DELETE": true,
	}

	for name := range expected {
		if !got[name] {
			t.Errorf("upstream has CONF_%s_DESCRIPTION but the Go port has no matching constant", name)
		}
	}
	for name := range got {
		if _, ok := expected[name]; !ok {
			t.Errorf("Go const CONF_%s_DESCRIPTION has no upstream counterpart", name)
		}
	}
}

// snippet returns a ±20 character window around `i` for diagnostics.
func snippet(s string, i int) string {
	lo := i - 20
	if lo < 0 {
		lo = 0
	}
	hi := i + 20
	if hi > len(s) {
		hi = len(s)
	}
	return s[lo:hi]
}

// tail returns the last 40 bytes (or the whole string if shorter).
func tail(s string) string {
	if len(s) <= 40 {
		return s
	}
	return s[len(s)-40:]
}

// upstreamConstRE captures a single `const CONF_X_DESCRIPTION = ` ... `;` block.
var upstreamConstRE = regexp.MustCompile(
	`(?s)const CONF_(GET|POST|PUT|PATCH|DELETE)_DESCRIPTION = ` + "`" + `(.*?)` + "`" + `;`,
)

// parseUpstreamDescriptionConstants extracts the runtime string value
// of every CONF_*_DESCRIPTION constant in the upstream source.
// JavaScript template literals interpret a small set of escape
// sequences: \` -> `, \\ -> \, \n -> newline, \t -> tab, and a
// backslash at end-of-line acts as a line continuation (it elides both
// characters). This function implements that subset — it is the only
// set the upstream uses in these particular literals.
func parseUpstreamDescriptionConstants(t *testing.T, src []byte) map[string]string {
	t.Helper()

	matches := upstreamConstRE.FindAllSubmatch(src, -1)
	if len(matches) == 0 {
		t.Fatalf("no CONF_*_DESCRIPTION constants found in upstream source (%d bytes)", len(src))
	}

	out := make(map[string]string, len(matches))
	for _, m := range matches {
		name := string(m[1])
		body := m[2]
		out[name] = jsTemplateLiteralValue(body)
	}
	return out
}

// jsTemplateLiteralValue evaluates the body of a JS template literal
// (without the surrounding backticks) into its runtime string.
// This is intentionally minimal — it only implements the escape
// sequences the upstream actually uses in CONF_*_DESCRIPTION.
func jsTemplateLiteralValue(body []byte) string {
	var buf bytes.Buffer
	buf.Grow(len(body))
	for i := 0; i < len(body); i++ {
		c := body[i]
		if c != '\\' || i+1 >= len(body) {
			buf.WriteByte(c)
			continue
		}
		nxt := body[i+1]
		switch nxt {
		case '`':
			buf.WriteByte('`')
			i++
		case '\\':
			buf.WriteByte('\\')
			i++
		case 'n':
			buf.WriteByte('\n')
			i++
		case 't':
			buf.WriteByte('\t')
			i++
		case '\n':
			// Line continuation: drop the backslash and the newline.
			i++
		default:
			// Unknown escape — keep both bytes verbatim (do not silently
			// drop data; the test will catch the discrepancy).
			buf.WriteByte(c)
		}
	}
	return buf.String()
}
