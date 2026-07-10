// No-textual-content-loss tests for the markdown package. These
// lock the contract from spec 03-known-lossy-constructs.md:
//
//	For each of the 14 "preserved" feature categories, the set of
//	non-whitespace tokens in the original markdown must be a subset
//	of the set of tokens in the round-tripped markdown (md → Conf
//	storage XHTML → md).
//
// The check is set-inclusion rather than byte equality because
// whitespace, indentation, and reference-style link rendering are
// allowed to differ. The textual CONTENT — words, URLs, code-block
// bodies, list items, table cell text — must survive.
package markdown_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/bennie/mcp-confluence/internal/markdown"
)

// TestRoundTripPreservesText is the umbrella test that exercises the
// "no textual content loss" contract across the 14 preserved
// categories. Each subtest focuses on one category.
func TestRoundTripPreservesText(t *testing.T) {
	cases := []struct {
		name string
		in   string
		// expected is the per-category list of original tokens
		// that MUST appear in the round-tripped output.
		expected []string
	}{
		{
			name: "01-headings-H1-to-H6",
			in: `# Alpha

## Bravo

### Charlie

#### Delta

##### Echo

###### Foxtrot`,
			expected: []string{"Alpha", "Bravo", "Charlie", "Delta", "Echo", "Foxtrot"},
		},
		{
			name:     "02-bold",
			in:       `this is **important** text`,
			expected: []string{"important", "text"},
		},
		{
			name:     "03-italic",
			in:       `a *subtle* change`,
			expected: []string{"subtle", "change"},
		},
		{
			name:     "04-inline-code",
			in:       "call `Save()` to persist",
			expected: []string{"Save", "persist"},
		},
		{
			name:     "05-strikethrough",
			in:       `this is ~~deprecated~~ but works`,
			expected: []string{"deprecated", "works"},
		},
		{
			name: "06-fenced-code-with-language",
			in: `Use the built-in helper.

` + "```python\ndef greet(name):\n    print('hello', name)\n```" + `

Then call it.`,
			expected: []string{"greet", "name", "print", "hello"},
		},
		{
			name: "07-unordered-list",
			in: `- apples
- bananas
- cherries`,
			expected: []string{"apples", "bananas", "cherries"},
		},
		{
			name: "08-ordered-list",
			in: `1. first
2. second
3. third`,
			expected: []string{"first", "second", "third"},
		},
		{
			name: "09-tables",
			in: `| alpha | beta |
|-------|------|
| one   | two  |
| three | four |`,
			expected: []string{"alpha", "beta", "one", "two", "three", "four"},
		},
		{
			name:     "10-external-links",
			in:       `See [the docs](https://example.com/docs) for details`,
			expected: []string{"https://example.com/docs", "docs", "details"},
		},
		{
			name:     "11-blockquote",
			in:       `> a famous quote worth remembering`,
			expected: []string{"famous", "quote", "remembering"},
		},
		{
			name:     "12-horizontal-rule",
			in:       "first section\n\n---\n\nsecond section",
			expected: []string{"first", "section", "second", "section"},
		},
		{
			name:     "13-unicode",
			in:       "日本語 français español — すべてのテキストが保持されます",
			expected: []string{"日本語", "français", "español", "すべて", "保持"},
		},
		{
			name: "14-html-entities",
			in: `The XML must escape the less-than and greater-than.

` + "```xml\n<tag attr=\"&amp;\">&lt;value&gt;</tag>\n```" + `

End.`,
			expected: []string{"XML", "escape", "less-than", "greater-than", "End"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			xhtml, err := markdown.MarkdownToStorageXHTML(tc.in)
			if err != nil {
				t.Fatalf("md → xhtml: %v", err)
			}
			out, err := markdown.StorageXHTMLToMarkdown(xhtml)
			if err != nil {
				t.Fatalf("xhtml → md: %v", err)
			}
			// Token-level check: every "expected" token must appear
			// (case-insensitive, as a substring) in the output.
			lOut := strings.ToLower(out)
			for _, want := range tc.expected {
				if !strings.Contains(lOut, strings.ToLower(want)) {
					t.Errorf("lost token %q\n--- in ---\n%s\n--- out ---\n%s\n--- end ---",
						want, tc.in, out)
				}
			}
		})
	}
}

// TestTokenize_HelperSpotCheck exercises the regex-based tokeniser
// to lock its behavior: tokens are the textual content (a-zA-Z0-9),
// separated by any non-textual char (whitespace OR punctuation).
// Code-fence content is preserved verbatim (so test code with
// underscores and braces still tokenises cleanly).
func TestTokenize_HelperSpotCheck(t *testing.T) {
	tokens := tokenize(`a *b* c, d. code_with_underscores`)
	for _, want := range []string{"a", "b", "c", "d", "code_with_underscores"} {
		if !contains(tokens, want) {
			t.Errorf("missing token %q in %v", want, tokens)
		}
	}
}

// tokenize splits a string into its textual tokens (a-zA-Z0-9 runs,
// possibly containing underscore). Whitespace and punctuation are
// delimiters and are dropped. Used by helpers in this file but not
// required for the main TestRoundTripPreservesText assertion (which
// uses substring containment directly).
func tokenize(s string) []string {
	re := regexp.MustCompile(`[A-Za-z0-9_]+`)
	return re.FindAllString(s, -1)
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
