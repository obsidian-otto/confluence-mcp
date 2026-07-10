// Package templates provides compiled, reusable text/template helpers
// for the assembly patterns that show up across the MCP server:
//
//   - Atlassian base URLs: "https://<site>.atlassian.net"
//   - Confluence v2 page-body paths: "/wiki/api/v2/pages/<id>?body-format=<fmt>"
//   - Single-quote / backtick interpolation when a string already lives
//     in a backtick-delimited raw-string literal and needs an inner
//     backtick that the raw-string form cannot express.
//
// Every entry point in this package is a thin wrapper around an
// already-compiled *template.Template. The templates are parsed once
// at package init via a compile-time check (see below); runtime paths
// never see a parse failure because the templates are fixed and tested.
//
// Why a dedicated package? The same handful of small-format templates
// would otherwise be re-declared in three or four sibling packages
// (atlassian, tools, server, ...) — each would silently drift in
// whitespace, casing, or trailing slash. Centralising the templates
// means the path literal "/wiki/api/v2/pages/" lives in exactly one
// place and is covered by exactly one set of tests.
//
// ## When NOT to use this package
//
//   - For per-call user input that must be escaped, use
//     net/url.Values and url.URL{}.String() instead — escaping is
//     not the job of these templates.
//   - For one-off, single-use format strings that nobody else shares,
//     inline fmt.Sprintf is still the right tool. Don't reach for
//     templates just to avoid one Sprintf call.
//
// ## Anti-pattern this replaces
//
// The codebase previously assembled these strings with chains of `+`
// that ran across many lines:
//
//	baseURL := "https://" + cfg.SiteName + ".atlassian.net"
//	path    := fmt.Sprintf("/wiki/api/v2/pages/%s?body-format=%s",
//	                        a.PageID, bodyFormat)
//
// Both forms are unreadable past ~3 segments, hide the structural
// delimiters (the protocol, the query separator, the parameter
// name) inside the `%s` placeholders, and make it trivial to write
// `/wiki/api/v2/pages%s` (missing slash) without compiler help. The
// text/template form keeps the literal skeleton — including every
// slash and query separator — visible at a glance.
package templates

import (
	"bytes"
	"fmt"
	"text/template"
)

// Backtick is the single ASCII backtick character (0x60). It exists
// as a named constant so raw-string literals can interpolate a
// literal backtick without falling back to the awkward
// “ ` + "`" + ` “ pattern. Use it as a regular const in compile-time
// string concatenation; the Go compiler folds the result.
const Backtick = "`"

// ErrTemplatePanic is the sentinel error returned if any of the
// package-level templates fail to compile at init. In practice this
// can never happen — the templates are literal strings under our
// control and are covered by TestAllTemplatesCompile — but the
// recovery is here so a typo in a template cannot crash the binary
// with a bare panic at startup.
var ErrTemplatePanic = fmt.Errorf("templates: internal template failed to compile")

// atlassianBaseURLTmpl is the Cloud-only URL template. The locked
// settings contract (specs/01-foundations/03-env-var-contract.md)
// defines ATLASSIAN_SITE_NAME as the bare site prefix — e.g.
// "acme" — and the server joins it as "https://acme.atlassian.net".
// The ATLASSIAN_API_BASE_URL opt-out for Data Center is gap Q4 and
// is not part of v1; when implemented it will live as a sibling
// template (atlassianBaseURLOverrideTmpl) rather than as an
// optional branch inside this one.
var atlassianBaseURLTmpl = template.Must(template.New("atlassianBaseURL").Parse(
	"https://{{.Site}}.atlassian.net",
))

// pageBodyPathTmpl is the path used by HandleGetPageBody. The
// Confluence Cloud v2 API does NOT support a separate
// `/wiki/api/v2/pages/{id}/body` sub-endpoint — the body is inlined
// into the GET-page response when the caller supplies
// `body-format=<storage|view|atlas_doc_format>` as a query-string
// parameter. We therefore emit the GET-page path with the
// body-format baked into the URL.
//
// Layout is deliberately fixed: the prefix, the page-id slot, the
// query separator, the parameter name, and the format slot are all
// literal so a malformed page id can never accidentally escape the
// path and forge a different endpoint.
var pageBodyPathTmpl = template.Must(template.New("pageBodyPath").Parse(
	"/wiki/api/v2/pages/{{.PageID}}?body-format={{.Format}}",
))

// AtlassianBaseURL renders the canonical Cloud base URL for a given
// site prefix. It panics if the underlying template fails to
// execute — which cannot happen for a value-free template, so the
// panic indicates a programmer error (e.g. a future edit that
// references an undefined action).
//
// Parameters:
//
//	site — the bare site prefix (e.g. "acme"); must not contain
//	       a "/" or ":" (caller's responsibility).
//
// Returns: e.g. "https://acme.atlassian.net".
func AtlassianBaseURL(site string) string {
	var buf bytes.Buffer
	if err := atlassianBaseURLTmpl.Execute(&buf, struct{ Site string }{Site: site}); err != nil {
		// Cannot happen for a template that only references .Site.
		// Defensive: re-panic with a recognisable message.
		panic(fmt.Errorf("templates: AtlassianBaseURL execute: %w", err))
	}
	return buf.String()
}

// PageBodyPath renders the GET-page path with a body-format query
// parameter baked in. Used by HandleGetPageBody.
//
// Parameters:
//
//	pageID — the numeric page id (e.g. "163935").
//	format — one of "storage" (default), "view", "atlas_doc_format";
//	         caller is responsible for defaulting to "storage".
//
// Returns: e.g. "/wiki/api/v2/pages/163935?body-format=storage".
func PageBodyPath(pageID, format string) string {
	var buf bytes.Buffer
	if err := pageBodyPathTmpl.Execute(&buf, struct {
		PageID string
		Format string
	}{PageID: pageID, Format: format}); err != nil {
		panic(fmt.Errorf("templates: PageBodyPath execute: %w", err))
	}
	return buf.String()
}

// Backticked returns s wrapped in a pair of ASCII backticks. The
// canonical use is composing description strings that need to render
// to "code spans" in the LLM-facing tool descriptions:
//
//	"Run " + Backticked("conf_get") + " once per session."
//
// The function itself does no allocation beyond the two bytes it
// adds; the compiler can inline it. For compile-time concatenation
// (where Backticked is not available because it's a function, not
// a const) use the Backtick const directly:
//
//	const greet = "press " + Backtick + "enter" + Backtick + " to continue"
func Backticked(s string) string { return Backtick + s + Backtick }

// newTemplate parses a template literal. It exists so the test
// suite can re-parse the production patterns and assert success
// without depending on the package-level Must-bound values (which
// would already have panicked if they failed to parse at init).
// Returning the parsed *template.Template through any lets the
// test caller ignore the concrete type.
func newTemplate(p string) (any, error) {
	return template.New("t").Parse(p)
}
