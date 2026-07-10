package tools

import (
	"bytes"
	_ "embed"
	"regexp"
	"strings"
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

// TestNewToolDescriptionsAreSubstantial asserts that the five
// post-v1 convenience tool descriptions are populated — they are
// local additions, not vendored from upstream, so the upstream-
// drift guardrail above doesn't apply. The audit doc (2026-07-10)
// recommended making tool descriptors "very accurate"; this test
// verifies that each new description contains at least one full
// sentence (not just a header) and explicitly mentions the tool
// name in prose form so an MCP client can read it as English
// rather than as a fragment.
func TestNewToolDescriptionsAreSubstantial(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		desc        string
		mentions    string // a substring expected in the description (case-sensitive)
		minLen      int
		mustContain string // a phrase like "returns TOON" the user expects
	}{
		{"CONF_LIST_SPACES", CONF_LIST_SPACES_DESCRIPTION, "List Confluence spaces", 200, "Returns TOON"},
		{"CONF_LIST_PAGES", CONF_LIST_PAGES_DESCRIPTION, "List Confluence pages", 200, "Returns TOON"},
		{"CONF_GET_PAGE_BODY", CONF_GET_PAGE_BODY_DESCRIPTION, "Read a single page", 200, "Returns TOON"},
		{"CONF_SEARCH", CONF_SEARCH_DESCRIPTION, "CQL", 200, "Returns TOON"},
		// conf_help is a self-describing tool: it doesn't return TOON
		// (it documents the rest of the surface that does), so its
		// first sentence must mention TOON-as-the-default elsewhere
		// rather than as its own return format.
		{"CONF_HELP", CONF_HELP_DESCRIPTION, "confluence", 200, "TOON"},
		// The three v2 markdown tool descriptions are local additions
		// (the upstream has no markdown tools). Each must satisfy
		// the same quality bar as the other new descriptions: ≥200
		// chars, mention the tool name in prose, contain a "Returns"
		// or "Converts" hint. The test name is the const identifier;
		// the `mentions` field is the tool name as it appears in
		// prose; the `mustContain` field picks up the format hint
		// (every tool returns TOON by default, except where the
		// tool's primary purpose is the conversion itself).
		{"CONF_POST_MARKDOWN", CONF_POST_MARKDOWN_DESCRIPTION, "conf_post_markdown", 200, "Returns TOON"},
		{"CONF_PUT_MARKDOWN", CONF_PUT_MARKDOWN_DESCRIPTION, "conf_put_markdown", 200, "Returns TOON"},
		{"CONF_GET_PAGE_MARKDOWN", CONF_GET_PAGE_MARKDOWN_DESCRIPTION, "conf_get_page_markdown", 200, "Returns TOON"},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			if len(c.desc) < c.minLen {
				t.Errorf("description length = %d, want >= %d", len(c.desc), c.minLen)
			}
			if !strings.Contains(c.desc, c.mentions) {
				t.Errorf("description should mention %q", c.mentions)
			}
			if !strings.Contains(c.desc, c.mustContain) {
				t.Errorf("description should contain %q", c.mustContain)
			}
		})
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
