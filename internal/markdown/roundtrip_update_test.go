// Regenerate golden files. Build with -tags update to enable:
//
//	go test -tags update ./internal/markdown/...
//
// This rewrites every fixture's want.xhtml and want.xhtml.md from
// the current pipeline output. Use this when an intentional change
// to the post-processor or to html-to-markdown shifts the expected
// output. The TestUpdateGoldens test is a no-op when the tag is NOT
// set — the production tests are in roundtrip_test.go.

//go:build update
// +build update

package markdown_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bennie/mcp-confluence/internal/markdown"
)

func TestUpdateGoldens(t *testing.T) {
	fixtures, err := filepath.Glob(filepath.Join(goldenDir, "*"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	updated := 0
	for _, dir := range fixtures {
		if !isFixtureDir(dir) {
			continue
		}
		inBytes, err := os.ReadFile(filepath.Join(dir, "in.md"))
		if err != nil {
			continue
		}
		xhtml, err := markdown.MarkdownToStorageXHTML(string(inBytes))
		if err != nil {
			t.Errorf("[%s] XHTML pipeline: %v", filepath.Base(dir), err)
			continue
		}
		md, err := markdown.StorageXHTMLToMarkdown(xhtml)
		if err != nil {
			t.Errorf("[%s] reverse pipeline: %v", filepath.Base(dir), err)
			continue
		}
		_ = os.WriteFile(filepath.Join(dir, "want.xhtml"), []byte(xhtml), 0o644)
		_ = os.WriteFile(filepath.Join(dir, "want.xhtml.md"), []byte(md), 0o644)
		updated++
	}
	t.Logf("regenerated %d fixture pairs", updated)
}
