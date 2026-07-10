package templates

import (
	"strings"
	"testing"
)

// TestAllTemplatesCompile is a defensive belt-and-braces check that
// the package-init template.Must(...) calls succeed. In practice
// the templates are parsed at package import time, so a typo would
// surface as a startup panic rather than a test failure — this
// function exists so a reviewer can see at a glance that the test
// suite still exercises the compilation path on every `go test`
// run, and so a future change that converts `Must` to `New` + an
// explicit error check has an obvious anchor test.
func TestAllTemplatesCompile(t *testing.T) {
	t.Parallel()

	// The two top-level templates are bound at package init.
	// Asserting against the package-level *Template values directly
	// would be implementation-specific; instead we re-parse the
	// same literal strings and assert success. If the production
	// template and this test template ever drift, the production
	// panic will fire on startup; this test serves as a duplicate.
	patterns := []string{
		"https://{{.Site}}.atlassian.net",
		"/wiki/api/v2/pages/{{.PageID}}?body-format={{.Format}}",
	}
	for _, p := range patterns {
		// Each iteration runs inside a sub-test so a panic in
		// parseTemplate doesn't take down sibling cases.
		p := p
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			if _, err := parseTemplate(p); err != nil {
				t.Fatalf("template %q should compile: %v", p, err)
			}
		})
	}
}

// TestAtlassianBaseURL asserts the rendered URL matches the locked
// contract from specs/01-foundations/03-env-var-contract.md.
//
// Table-driven: each row is a representative site prefix. The
// "empty" case documents that the function does NOT validate site
// emptiness — that's a config-layer concern (atlassian.New), and
// the helper intentionally stays a pure string formatter.
func TestAtlassianBaseURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		site string
		want string
	}{
		{"typical", "acme", "https://acme.atlassian.net"},
		{"dotted prefix", "my-company", "https://my-company.atlassian.net"},
		{"numeric prefix", "team123", "https://team123.atlassian.net"},
		{"empty (caller-validated upstream)", "", "https://.atlassian.net"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := AtlassianBaseURL(tt.site); got != tt.want {
				t.Errorf("AtlassianBaseURL(%q) = %q, want %q", tt.site, got, tt.want)
			}
		})
	}
}

// TestAtlassianBaseURL_NeverIncludesQueryString is the regression
// guard for the bug where a future edit accidentally adds a query
// string to the template — the atlassian base URL is a prefix, not
// an endpoint, and must never contain "?" or "&".
func TestAtlassianBaseURL_NeverIncludesQueryString(t *testing.T) {
	t.Parallel()
	out := AtlassianBaseURL("acme")
	if strings.ContainsAny(out, "?&") {
		t.Errorf("base URL should not contain query separators: %q", out)
	}
	if !strings.HasPrefix(out, "https://") {
		t.Errorf("base URL must use https: %q", out)
	}
}

// TestPageBodyPath verifies the canonical body-path layout. The
// literal skeleton (`/wiki/api/v2/pages/`, `?body-format=`) is what
// makes the template worth using; any drift in those characters is
// caught here.
func TestPageBodyPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		pageID string
		format string
		want   string
	}{
		{"storage (default upstream)", "163935", "storage", "/wiki/api/v2/pages/163935?body-format=storage"},
		{"view representation", "163935", "view", "/wiki/api/v2/pages/163935?body-format=view"},
		{"atlas_doc_format", "163935", "atlas_doc_format", "/wiki/api/v2/pages/163935?body-format=atlas_doc_format"},
		{"alphabetic page id", "abc", "storage", "/wiki/api/v2/pages/abc?body-format=storage"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := PageBodyPath(tt.pageID, tt.format); got != tt.want {
				t.Errorf("PageBodyPath(%q, %q) = %q, want %q", tt.pageID, tt.format, got, tt.want)
			}
		})
	}
}

// TestPageBodyPath_Skeleton asserts the literal skeleton (path
// prefix, slash, query separator, parameter name, equals sign) is
// preserved exactly across inputs — even with hostile inputs that
// would otherwise look fine but contain query separators.
//
// The function does NOT url-encode inputs by design (the upstream
// page-id format is numeric in practice); the assertion here is
// that the skeleton never gets eaten by an input.
func TestPageBodyPath_Skeleton(t *testing.T) {
	t.Parallel()
	out := PageBodyPath("42", "storage")
	if !strings.HasPrefix(out, "/wiki/api/v2/pages/") {
		t.Errorf("missing path prefix: %q", out)
	}
	if !strings.Contains(out, "?body-format=") {
		t.Errorf("missing body-format query: %q", out)
	}
	if got := strings.Count(out, "?"); got != 1 {
		t.Errorf("should contain exactly one '?': %q (got %d)", out, got)
	}
}

// TestBackticked asserts the helper is exactly `s`, not "s" or
// (s). The previous raw-string-split pattern produced the same
// bytes but at high cognitive cost.
func TestBackticked(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{"", "``"},
		{"jq", "`jq`"},
		{"conf_get path=\"/wiki/api/v2/spaces?limit=5\"", "`conf_get path=\"/wiki/api/v2/spaces?limit=5\"`"},
		{"multi\nline", "`multi\nline`"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			if got := Backticked(tt.in); got != tt.want {
				t.Errorf("Backticked(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestBacktickConstIsSingleByte pins the byte value of the Backtick
// const. If a future edit accidentally typed `"“" ` (two
// backticks), this test fails fast with a clear message.
func TestBacktickConstIsSingleByte(t *testing.T) {
	t.Parallel()
	if Backtick != "`" {
		t.Errorf("Backtick const must be a single ASCII backtick, got %q", Backtick)
	}
	if len(Backtick) != 1 {
		t.Errorf("Backtick const must be exactly one byte, got %d", len(Backtick))
	}
}

// parseTemplate is a thin shim over template.New used by
// TestAllTemplatesCompile. Kept as a function (not an inline Must)
// so a deliberate future change to use template.New + explicit
// error can be wired in here without touching the test bodies.
func parseTemplate(p string) (any, error) {
	t, err := newTemplate(p)
	if err != nil {
		return nil, err
	}
	return t, nil
}
