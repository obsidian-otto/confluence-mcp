// Package markdown_test covers the markdown → Confluence storage XHTML
// direction. The tests are organised per post-processor rule:
//
//   - Rule 1: <pre><code class="language-X">...</code></pre>           → ac:structured-macro code
//   - Rule 2: <img src="URL" alt="ALT">                                → ac:image ri:url
//   - Rule 3: <a href="URL">TEXT</a>                                   → kept as-is (the ac:link wrap
//     is only used for internal-Confluence targets; external pass-through)
//   - Rule 4: html root                                                → inject xmlns:ac, xmlns:ri
//   - Rule 5: <br>, <hr>, void elements                                → self-closing form
//
// The tests intentionally exercise the public MarkdownToStorageXHTML
// surface; internals (htmlPostProcess) are validated end-to-end.
package markdown

import (
	"strings"
	"testing"
)

// TestMarkdownToStorage_CodeBlock_WithLanguage checks Rule 1.
// Goldmark emits <pre><code class="language-X">CODE</code></pre> for
// fenced blocks; the post-processor must wrap as an ac:structured-macro
// with an ac:parameter for the language and the body inside CDATA.
func TestMarkdownToStorage_CodeBlock_WithLanguage(t *testing.T) {
	in := "```go\npackage main\nfunc main() {}\n```\n"
	got, err := MarkdownToStorageXHTML(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mustContain(t, got, `<ac:structured-macro ac:name="code"`)
	mustContain(t, got, `ac:schema-version="1"`)
	mustContain(t, got, `<ac:parameter ac:name="language">go</ac:parameter>`)
	mustContain(t, got, `<ac:plain-text-body><![CDATA[`)
	mustContain(t, got, `package main`)
	mustContain(t, got, `]]></ac:plain-text-body>`)
	mustContain(t, got, `</ac:structured-macro>`)
	mustNotContain(t, got, `<pre>`)
	mustNotContain(t, got, `<code class="language-go">`)
}

// TestMarkdownToStorage_CodeBlock_NoLanguage checks Rule 1 again, but
// without a language tag. Goldmark produces <pre><code>...</code></pre>
// in that case; the post-processor must skip the ac:parameter
// language element.
func TestMarkdownToStorage_CodeBlock_NoLanguage(t *testing.T) {
	in := "```\nplain text\nno lang\n```\n"
	got, err := MarkdownToStorageXHTML(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mustContain(t, got, `<ac:structured-macro ac:name="code"`)
	mustContain(t, got, `<ac:plain-text-body><![CDATA[`)
	mustContain(t, got, `plain text`)
	mustNotContain(t, got, `ac:name="language"`)
	mustNotContain(t, got, `<pre>`)
}

// TestMarkdownToStorage_Image_ToACImage checks Rule 2. A markdown image
// becomes <ac:image><ri:url ri:value="URL"/></ac:image>. Alt is dropped
// (the spec's known-lossy register documents this).
func TestMarkdownToStorage_Image_ToACImage(t *testing.T) {
	in := "![alt text](https://example.com/x.png)\n"
	got, err := MarkdownToStorageXHTML(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustContain(t, got, `<ac:image>`)
	mustContain(t, got, `<ri:url ri:value="https://example.com/x.png"/>`)
	mustContain(t, got, `</ac:image>`)
	mustNotContain(t, got, `<img `)
}

// TestMarkdownToStorage_ExternalLink_PassesThrough checks Rule 3. The
// spec calls for external <a href> tags to be left as plain
// <a href="URL">TEXT</a>; Confluence accepts this and renders it
// identically to the wrapped form.
func TestMarkdownToStorage_ExternalLink_PassesThrough(t *testing.T) {
	in := "[a link](https://example.com/page)\n"
	got, err := MarkdownToStorageXHTML(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustContain(t, got, `<a href="https://example.com/page">`)
	mustContain(t, got, `a link`)
	mustContain(t, got, `</a>`)
	// We should NOT wrap a plain external <a> in <ac:link>; that is
	// only used for internal Confluence targets.
	mustNotContain(t, got, `<ac:link>`)
	mustNotContain(t, got, `<ri:url`)
}

// TestMarkdownToStorage_NamespacesOnRoot checks Rule 4. The xmlns:ac
// and xmlns:ri declarations must end up on the root <html> element.
func TestMarkdownToStorage_NamespacesOnRoot(t *testing.T) {
	in := "# hello\n"
	got, err := MarkdownToStorageXHTML(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Looking for the root <html ...> tag carrying both xmlns:ac and xmlns:ri.
	if !strings.Contains(got, `xmlns:ac="http://atlassian.com/content"`) {
		t.Errorf("missing xmlns:ac on root; got:\n%s", got)
	}
	if !strings.Contains(got, `xmlns:ri="http://atlassian.com/resource/identifier"`) {
		t.Errorf("missing xmlns:ri on root; got:\n%s", got)
	}
}

// TestMarkdownToStorage_SelfClosingTags checks Rule 5. <br>, <hr>, <img>
// (in code blocks) must use the self-closing XHTML form. Goquery's HTML
// serialisation uses <br><hr> by default; the post-processor rewrites
// them as <br/><hr/>.
func TestMarkdownToStorage_SelfClosingTags(t *testing.T) {
	// A thematic break in markdown becomes <hr>.
	in := "first paragraph\n\n---\n\nsecond paragraph\n"
	got, err := MarkdownToStorageXHTML(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustContain(t, got, `<hr/>`)
	mustNotContain(t, got, `<hr>`)
}

// TestMarkdownToStorage_HeadingsAndLists is a sanity check that common
// HTML elements (h1, ul, strong, em) survive the pipeline unchanged.
func TestMarkdownToStorage_HeadingsAndLists(t *testing.T) {
	in := "# H1\n\ntext with **bold** and *italic* and `code`.\n\n- a\n- b\n- c\n"
	got, err := MarkdownToStorageXHTML(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustContain(t, got, `<h1>H1</h1>`)
	mustContain(t, got, `<strong>bold</strong>`)
	mustContain(t, got, `<em>italic</em>`)
	mustContain(t, got, `<code>code</code>`)
	mustContain(t, got, `<ul>`)
	mustContain(t, got, `<li>a</li>`)
}

// TestMarkdownToStorage_CDATAEscapes is a regression test: a code block
// containing XML-significant characters (<, >, &) must survive the
// conversion inside CDATA so that Confluence's XML parser does not
// choke on it.
func TestMarkdownToStorage_CDATAEscapes(t *testing.T) {
	in := "```html\n<div class=\"x\">&amp;</div>\n```\n"
	got, err := MarkdownToStorageXHTML(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustContain(t, got, `<ac:plain-text-body><![CDATA[`)
	mustContain(t, got, `<div class="x">&amp;</div>`)
	mustContain(t, got, `]]></ac:plain-text-body>`)
}

// TestMarkdownToStorage_Empty is a sanity check: empty input produces
// an empty (but well-formed) XHTML document.
func TestMarkdownToStorage_Empty(t *testing.T) {
	got, err := MarkdownToStorageXHTML("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, `<html`) {
		t.Errorf("expected <html> root even for empty input; got: %q", got)
	}
}

// --- helpers ------------------------------------------------------------

func mustContain(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("expected output to contain %q\n--- got ---\n%s\n--- end ---",
			needle, haystack)
	}
}

func mustNotContain(t *testing.T, haystack, needle string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Errorf("expected output NOT to contain %q\n--- got ---\n%s\n--- end ---",
			needle, haystack)
	}
}
