// Package markdown — storage XHTML → markdown direction.
//
// StorageXHTMLToMarkdown converts a Confluence storage-format XHTML
// payload back into a Markdown string. It is the entry point for
// conf_get_page_markdown.
//
// The conversion uses github.com/JohannesKaufmann/html-to-markdown/v2
// (the project-wide Q24 lock) with the CommonPlugins set:
//
//   - base          — common HTML element renders
//   - commonmark    — CommonMark syntax (headings, lists, code blocks,
//     links, blockquotes, em/strong, line breaks)
//   - strikethrough — <del> → ~~x~~
//   - table         — <table>/<thead>/<tbody>/<tr>/<th>/<td> → GFM pipe
//
// These four together cover the 14 "preserved-on-round-trip"
// categories from spec 03-known-lossy-constructs.md. The 10 known
// lossy constructs (mentions, layouts, panels, etc.) are documented
// in spec 03 as expected drops.
//
// Pre-processing:
//   - ac:structured-macro code blocks (the upload direction emits
//     these) are normalised to plain <pre><code class="language-X">
//     BEFORE h2m sees them. Without this step h2m extracts only the
//     language parameter and drops the body code, which would lose
//     every code-block fixture's content in the round-trip test
//     corpus.
package markdown

import (
	"fmt"
	"regexp"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/strikethrough"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/table"
)

// defaultConverter is the shared h2m converter. It is built once
// (NewConverter is fairly expensive due to plugin registration) and
// reused across all calls.
var defaultConverter = converter.NewConverter(
	converter.WithPlugins(
		base.NewBasePlugin(),
		commonmark.NewCommonmarkPlugin(),
		strikethrough.NewStrikethroughPlugin(),
		table.NewTablePlugin(),
	),
)

// codeMacroRe matches an <ac:structured-macro ac:name="code" …>
// with optional <ac:parameter ac:name="language">…</ac:parameter>
// and a <ac:plain-text-body><![CDATA[BODY]]></ac:plain-text-body>
// inside. The captured groups are:
//
//  1. the language (or empty)
//  2. the code body
var codeMacroRe = regexp.MustCompile(
	`(?s)<ac:structured-macro[^>]*ac:name="code"[^>]*>` +
		`(?:<ac:parameter[^>]*ac:name="language"[^>]*>([^<]*)</ac:parameter>)?` +
		`<ac:plain-text-body><!\[CDATA\[(.*?)\]\]></ac:plain-text-body>` +
		`</ac:structured-macro>`,
)

// StorageXHTMLToMarkdown converts a Confluence storage-format XHTML
// payload to a Markdown string.
//
// Errors are returned only for the (rare) html.Parse failure inside
// the h2m library.
func StorageXHTMLToMarkdown(xhtml string) (string, error) {
	// Pre-process: collapse code-macro blocks to <pre><code
	// class="language-X">CODE</code></pre> so h2m sees a familiar
	// shape and emits a proper fenced block. Without this, the
	// upload → download round-trip loses every code block's body.
	normalised := codeMacroRe.ReplaceAllStringFunc(xhtml, func(m string) string {
		groups := codeMacroRe.FindStringSubmatch(m)
		if len(groups) < 3 {
			return m
		}
		lang := groups[1]
		body := groups[2]
		if lang != "" {
			return fmt.Sprintf(`<pre><code class="language-%s">%s</code></pre>`,
				lang, body)
		}
		return fmt.Sprintf(`<pre><code>%s</code></pre>`, body)
	})

	md, err := defaultConverter.ConvertString(normalised)
	if err != nil {
		return "", fmt.Errorf("markdown/h2m: %w", err)
	}
	return md, nil
}
