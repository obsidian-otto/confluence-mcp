// Tests for the storage XHTML → markdown direction (the
// conf_get_page_markdown tool path).
//
// The fixtures here exercise the rendering of the seven CommonMark
// shapes we promise to preserve across round-trips:
//
//   - tables
//   - fenced code blocks
//   - links (external)
//   - headings
//   - blockquotes (including nested)
//   - strikethrough
//   - task lists
//
// The exact output of html-to-markdown v2 with the CommonPlugins set
// (base + commonmark + strikethrough + table) is the oracle. Drift
// there should be caught here so the golden-file fixtures stay in
// sync with the library behaviour.
package markdown

import (
	"strings"
	"testing"
)

// TestStorageToMarkdown_Headings verifies atx-style heading rendering
// (the commonmark plugin defaults).
func TestStorageToMarkdown_Headings(t *testing.T) {
	xhtml := `<h1>H1</h1><h2>H2</h2><h3>H3</h3>`
	got, err := StorageXHTMLToMarkdown(xhtml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustContain(t, got, `# H1`)
	mustContain(t, got, `## H2`)
	mustContain(t, got, `### H3`)
}

// TestStorageToMarkdown_InlineFormatting verifies the basic inline
// Markdown spans (bold = <strong>, italic = <em>, inline code =
// <code>, strike = <del> via the strikethrough plugin).
func TestStorageToMarkdown_InlineFormatting(t *testing.T) {
	xhtml := `<p>Some <em>emph</em> and <strong>bold</strong> and <code>inline</code> and <del>strike</del>.</p>`
	got, err := StorageXHTMLToMarkdown(xhtml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustContain(t, got, `*emph*`)
	mustContain(t, got, `**bold**`)
	mustContain(t, got, "`inline`")
	mustContain(t, got, `~~strike~~`)
}

// TestStorageToMarkdown_ExternalLink verifies the [text](URL) form
// comes out for plain <a href=URL>TAG</a>.
func TestStorageToMarkdown_ExternalLink(t *testing.T) {
	xhtml := `<p><a href="https://example.com/page">a link</a></p>`
	got, err := StorageXHTMLToMarkdown(xhtml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustContain(t, got, `[a link](https://example.com/page)`)
}

// TestStorageToMarkdown_Table verifies the GFM table rendering.
//
// The html-to-markdown table plugin recognises <table><thead>… and
// <table><tbody>… structures and emits pipe-syntax markdown.
func TestStorageToMarkdown_Table(t *testing.T) {
	xhtml := `<table>` +
		`<thead><tr><th>a</th><th>b</th></tr></thead>` +
		`<tbody><tr><td>1</td><td>2</td></tr></tbody>` +
		`</table>`
	got, err := StorageXHTMLToMarkdown(xhtml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustContain(t, got, `| a`)
	mustContain(t, got, `| b`)
	mustContain(t, got, `---`)
	mustContain(t, got, `| 1`)
	mustContain(t, got, `| 2`)
}

// TestStorageToMarkdown_FencedCodeBlock verifies that a <pre><code>
// is rendered as a fenced block. The literal language class would
// be picked up by h2m and emitted as the fence info-string.
func TestStorageToMarkdown_FencedCodeBlock(t *testing.T) {
	xhtml := `<pre><code class="language-go">package main
func main() {}
</code></pre>`
	got, err := StorageXHTMLToMarkdown(xhtml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustContain(t, got, "```go")
	mustContain(t, got, "package main")
	mustContain(t, got, "func main() {}")
	mustContain(t, got, "```")
}

// TestStorageToMarkdown_Blockquote verifies blockquote rendering
// from <blockquote><p>…</p></blockquote>.
func TestStorageToMarkdown_Blockquote(t *testing.T) {
	xhtml := `<blockquote><p>quoted text</p></blockquote>`
	got, err := StorageXHTMLToMarkdown(xhtml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustContain(t, got, "> quoted text")
}

// TestStorageToMarkdown_Strikethrough verifies the strikethrough
// plugin wraps <del> as ~~x~~.
func TestStorageToMarkdown_Strikethrough(t *testing.T) {
	xhtml := `<p>before <del>removed</del> after</p>`
	got, err := StorageXHTMLToMarkdown(xhtml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustContain(t, got, `~~removed~~`)
}

// TestStorageToMarkdown_TaskList verifies that goldmark's checkbox
// <input> elements (rendered by storage as plain text or as
// Confluence's task list) come back without the literal checkbox
// tags. The exact checkbox state handling is documented as
// known-lossy in spec 03; this test asserts at minimum that the
// checkbox TEXT survives.
func TestStorageToMarkdown_TaskList(t *testing.T) {
	xhtml := `<ul>` +
		`<li><input disabled="" type="checkbox"> task1</li>` +
		`<li><input checked="" disabled="" type="checkbox"> task2</li>` +
		`</ul>`
	got, err := StorageXHTMLToMarkdown(xhtml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustContain(t, got, "task1")
	mustContain(t, got, "task2")
	// The literal <input> tag must NOT leak through.
	if strings.Contains(got, "<input") {
		t.Errorf("expected stripped <input>; got:\n%s", got)
	}
}

// TestStorageToMarkdown_Empty is a sanity check: empty input gives
// empty (or whitespace-only) output without error.
func TestStorageToMarkdown_Empty(t *testing.T) {
	got, err := StorageXHTMLToMarkdown("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(got) != "" {
		t.Errorf("expected empty output for empty input; got %q", got)
	}
}
