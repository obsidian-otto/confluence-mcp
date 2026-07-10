// Package markdown implements the bidirectional markdown ↔
// Confluence storage format conversions used by the
// conf_post_markdown / conf_put_markdown / conf_get_page_markdown
// MCP tools.
//
// The upload direction (MarkdownToStorageXHTML) is a 3-stage pipeline:
//
//	md string
//	  │  (1) goldmark.New + GFM + WithXHTML  → CommonMark XHTML
//	  ▼
//	html string                  (goldmark output)
//	  │  (2) htmlPostProcess: structural walk via goquery identifies
//	  │      rule-1 / rule-2 elements; structural string substitutions
//	  │      apply the 5 rewrite rules
//	  ▼
//	storage XHTML string         (Confluence storage wire format)
//
// Stage 3 (the {representation: storage, value: ...} envelope) is
// the caller's job — it lives in the CRUD handler.
package markdown

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
)

// xml namespaces used by Confluence storage format.
const (
	xmlnsAC = "http://atlassian.com/content"
	xmlnsRI = "http://atlassian.com/resource/identifier"
)

// goldmarkConverter is the shared Markdown → XHTML renderer.
// WithXHTML() makes goldmark emit self-closing void elements
// (<br/>, <hr/>) which simplifies Rule 5.
var goldmarkConverter = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithRendererOptions(
		html.WithXHTML(),
	),
)

// MarkdownToStorageXHTML converts a Markdown source string into the
// Confluence storage-format XHTML wire shape. It always emits an
// explicit <html xmlns:ac=… xmlns:ri=…> root so callers that
// pass the result to /wiki/api/v2/pages get a Confluence-valid
// document even when the markdown source is empty.
func MarkdownToStorageXHTML(md string) (string, error) {
	var buf bytes.Buffer
	if err := goldmarkConverter.Convert([]byte(md), &buf); err != nil {
		return "", fmt.Errorf("markdown/goldmark: %w", err)
	}
	xhtml := buf.String()

	xhtml, err := htmlPostProcess(xhtml)
	if err != nil {
		return "", fmt.Errorf("markdown/postprocess: %w", err)
	}
	return xhtml, nil
}

// htmlPostProcess applies the 5 transformation rules from
// specs/10-markdown-roundtrip/02-post-processing.md to the goldmark
// output.
//
//  1. <pre><code class="language-X">CODE</code></pre>
//     → ac:structured-macro code (CDATA body)
//  2. <img src="URL" alt="ALT">
//     → <ac:image><ri:url ri:value="URL"/></ac:image>
//  3. <a href="URL">TEXT</a>  (external)
//     → pass-through (Confluence accepts plain <a href>)
//  4. <html> root
//     → inject xmlns:ac, xmlns:ri if absent
//  5. self-closing void elements
//     → WithXHTML() on goldmark already emits them correctly
type splice struct {
	start, end int
	repl       string
}

func htmlPostProcess(htmlIn string) (string, error) {
	// Use goquery for AST identification only — we apply rewrites
	// by structural string substitution against the original
	// goldmark bytes (the alternative, doc.Html() round-trip, would
	// re-encode CDATA into HTML comment form and reformat
	// self-closing tags).
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlIn))
	if err != nil {
		return "", err
	}

	var rewrites []splice

	// --- Rule 1: <pre><code class="language-X">CODE</code></pre> ---
	doc.Find("pre > code").Each(func(_ int, s *goquery.Selection) {
		cls, _ := s.Attr("class")
		lang := strings.TrimSpace(strings.TrimPrefix(cls, "language-"))
		// Text content (literal bytes after entity decoding). The
		// CDATA wrapper in the output preserves these chars
		// verbatim.
		code := s.Text()

		// Match the opening tag and closing tag positions in the
		// original string. We use a regex anchored on the exact
		// goldmark-emitted form: "<pre><code class="language-X">"
		// at the start, "</code></pre>" at the end.
		start, end := findCodeBlockBounds(htmlIn, lang)
		if start < 0 || end <= start {
			return
		}

		var macro string
		if lang != "" {
			macro = fmt.Sprintf(
				`<ac:structured-macro ac:name="code" ac:schema-version="1">`+
					`<ac:parameter ac:name="language">%s</ac:parameter>`+
					`<ac:plain-text-body><![CDATA[%s]]></ac:plain-text-body>`+
					`</ac:structured-macro>`,
				lang, code,
			)
		} else {
			macro = fmt.Sprintf(
				`<ac:structured-macro ac:name="code" ac:schema-version="1">`+
					`<ac:plain-text-body><![CDATA[%s]]></ac:plain-text-body>`+
					`</ac:structured-macro>`,
				code,
			)
		}
		rewrites = append(rewrites, splice{start, end, macro})
	})

	// --- Rule 2: <img src="URL" alt="ALT"> → ac:image/ri:url ---
	doc.Find("img").Each(func(_ int, s *goquery.Selection) {
		src, ok := s.Attr("src")
		if !ok || src == "" {
			return
		}
		// goldmark with WithXHTML emits <img src="..." alt="..."/>
		// (note the space before />). We match the exact source via
		// attribute read so we can locate it in the original.
		var needle string
		if alt, has := s.Attr("alt"); has && alt != "" {
			needle = fmt.Sprintf(`<img src="%s" alt="%s"`, src, alt)
		} else {
			needle = fmt.Sprintf(`<img src="%s"`, src)
		}
		// Find the tag and capture up through the closing `>` or `/>`.
		idx := strings.Index(htmlIn, needle)
		if idx < 0 {
			return
		}
		// Find end of the tag.
		tagEnd := strings.IndexAny(htmlIn[idx:], `>`)
		if tagEnd < 0 {
			return
		}
		end := idx + tagEnd + 1
		rewrites = append(rewrites, splice{
			start: idx, end: end,
			repl: fmt.Sprintf(`<ac:image><ri:url ri:value="%s"/></ac:image>`,
				attrEscape(src)),
		})
	})

	// --- Rule 3: <a href> pass-through is a no-op ---

	// Apply rewrites in reverse-offset order so earlier offsets
	// remain valid as we splice.
	out := applyRewrites(htmlIn, rewrites)

	// --- Rule 4: namespace injection ---
	out = injectRootNamespaces(out)

	// --- Rule 5: ensure self-closing form for any stray <br>, <hr>
	// (Goldmark with WithXHTML already does this, but we run a
	// safety pass for the rare case where a post-processor
	// substitute leaves an unclosed void element behind.)
	out = enforceSelfClosing(out)

	return out, nil
}

// findCodeBlockBounds locates a <pre><code[ class="language-X"]> ... </code></pre>
// block in htmlIn and returns the byte range covering the FULL block
// (opening tag through closing tag, inclusive).
//
// Returns -1, -1 if not found.
func findCodeBlockBounds(htmlIn, lang string) (int, int) {
	var openTag string
	if lang != "" {
		openTag = fmt.Sprintf(`<pre><code class="language-%s">`, lang)
	} else {
		openTag = `<pre><code>`
	}
	start := strings.Index(htmlIn, openTag)
	if start < 0 {
		return -1, -1
	}
	endTag := `</code></pre>`
	end := strings.Index(htmlIn[start:], endTag)
	if end < 0 {
		return -1, -1
	}
	return start, start + end + len(endTag)
}

// applyRewrites splices rewrites into htmlIn in REVERSE offset order
// so earlier offsets remain valid.
func applyRewrites(htmlIn string, rewrites []splice) string {
	if len(rewrites) == 0 {
		return htmlIn
	}
	// Sort by start descending (simple bubble — small N).
	sorted := make([]splice, len(rewrites))
	copy(sorted, rewrites)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j].start > sorted[j-1].start; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	out := htmlIn
	for _, r := range sorted {
		if r.start < 0 || r.end > len(out) || r.end < r.start {
			continue
		}
		out = out[:r.start] + r.repl + out[r.end:]
	}
	return out
}

// injectRootNamespaces ensures the <html> root has both xmlns:ac and
// xmlns:ri declarations.
//
// If goldmark's output doesn't contain an <html> tag (e.g. for empty
// input it emits nothing), we wrap the entire body in our own
// <html xmlns:ac="…" xmlns:ri="…">…</html>.
func injectRootNamespaces(htmlIn string) string {
	// Match any <html> tag with zero or more leading attribute sections.
	openTag := regexp.MustCompile(`(?is)<html\b[^>]*>`)
	hasHtml := openTag.MatchString(htmlIn)
	if hasHtml {
		return openTag.ReplaceAllStringFunc(htmlIn, func(match string) string {
			lower := strings.ToLower(match)
			if strings.Contains(lower, `xmlns:ac=`) {
				return match
			}
			// If the tag is just "<html>", replace wholesale.
			if strings.EqualFold(match, "<html>") {
				return `<html xmlns:ac="` + xmlnsAC + `" xmlns:ri="` + xmlnsRI + `">`
			}
			// <html attr="…"> → inject namespaces just after "html".
			return match[:len("<html")] + ` xmlns:ac="` + xmlnsAC + `" xmlns:ri="` + xmlnsRI + `"` +
				match[len("<html"):]
		})
	}
	// No <html> root at all — wrap the entire body.
	if strings.TrimSpace(htmlIn) == "" {
		return `<html xmlns:ac="` + xmlnsAC + `" xmlns:ri="` + xmlnsRI + `"></html>`
	}
	return `<html xmlns:ac="` + xmlnsAC + `" xmlns:ri="` + xmlnsRI + `">` + htmlIn + `</html>`
}

// enforceSelfClosing rewrites <br>, <hr>, <img …>, <col>, <area>,
// <base>, <embed>, <input>, <link>, <meta>, <param>, <source>,
// <track>, <wbr> to self-closing form if not already.
// In practice goldmark WithXHTML() emits them as "<hr />" (with a
// space before the "/"), which is valid XHTML but stylistically
// inconsistent; this pass normalises them. Self-closed bare tags
// without attributes ("<br>", "<hr>") are also normalised.
func enforceSelfClosing(htmlIn string) string {
	void := []string{"br", "hr", "img", "col", "area", "base", "embed",
		"input", "link", "meta", "param", "source", "track", "wbr"}
	out := htmlIn
	for _, tag := range void {
		// Match "<TAG" + optional space+attrs + optional " /" + ">".
		// Always rewrite to "<TAG/>" or "<TAG ATTRS/>".
		pat := regexp.MustCompile(
			`(?i)<` + tag + `(?:\s+[^>]*?)?\s*/?>`,
		)
		out = pat.ReplaceAllStringFunc(out, func(m string) string {
			// Strip trailing whitespace, optional '/', and '>'.
			stripped := strings.TrimRight(m, " 	/>")
			return stripped + "/>"
		})
	}
	return out
}

// attrEscape escapes XML-significant characters inside a
// double-quoted attribute value.
func attrEscape(s string) string {
	s = strings.ReplaceAll(s, `&`, `&amp;`)
	s = strings.ReplaceAll(s, `"`, `&quot;`)
	s = strings.ReplaceAll(s, `<`, `&lt;`)
	s = strings.ReplaceAll(s, `>`, `&gt;`)
	return s
}
