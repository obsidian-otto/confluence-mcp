// Golden-file round-trip tests for the markdown package.
//
// Each fixture directory under ./golden/ contains:
//
//   - in.md           the markdown input
//   - want.xhtml      expected output of MarkdownToStorageXHTML(in.md)
//   - want.xhtml.md   expected output of StorageXHTMLToMarkdown(want.xhtml)
//
// The test walks every fixture and asserts the pipeline output
// matches the golden files. To regenerate after a deliberate
// pipeline change, see roundtrip_update_test.go (gated by the
// `update` build tag):
//
//	go test -tags update ./internal/markdown/...
package markdown_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bennie/mcp-confluence/internal/markdown"
)

const goldenDir = "testdata/golden"

func TestRoundtripGoldenFiles(t *testing.T) {
	fixtures, err := filepath.Glob(filepath.Join(goldenDir, "*"))
	if err != nil {
		t.Fatalf("glob fixtures: %v", err)
	}
	if len(fixtures) == 0 {
		t.Fatalf("no fixtures found under %s", goldenDir)
	}
	for _, dir := range fixtures {
		if !isFixtureDir(dir) {
			continue
		}
		name := filepath.Base(dir)
		t.Run(name, func(t *testing.T) {
			runFixture(t, dir)
		})
	}
}

// isFixtureDir returns true only for directories under goldenDir
// that contain an in.md file. (Skips dotfiles, README.md if present.)
func isFixtureDir(path string) bool {
	if path == "" {
		return false
	}
	base := filepath.Base(path)
	if strings.HasPrefix(base, ".") {
		return false
	}
	if base == "README.md" {
		return false
	}
	if strings.HasSuffix(base, ".md") && !strings.Contains(base, "-") {
		// single-file artifacts, not fixtures
		return false
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return false
	}
	if _, err := os.Stat(filepath.Join(path, "in.md")); err != nil {
		return false
	}
	return true
}

// runFixture executes the round-trip check on one fixture directory.
func runFixture(t *testing.T, dir string) {
	t.Helper()
	inBytes, err := os.ReadFile(filepath.Join(dir, "in.md"))
	if err != nil {
		t.Fatalf("read in.md: %v", err)
	}
	inMD := string(inBytes)

	gotXHTML, err := markdown.MarkdownToStorageXHTML(inMD)
	if err != nil {
		t.Fatalf("MarkdownToStorageXHTML: %v", err)
	}

	wantXHTMLBytes, err := os.ReadFile(filepath.Join(dir, "want.xhtml"))
	if err != nil {
		t.Fatalf("read want.xhtml: %v", err)
	}
	wantXHTML := string(wantXHTMLBytes)
	if gotXHTML != wantXHTML {
		t.Errorf("XHTML mismatch.\n--- got ---\n%s\n--- want ---\n%s\n--- end ---",
			gotXHTML, wantXHTML)
	}

	// Now exercise the reverse pipeline: storage → markdown.
	gotMD, err := markdown.StorageXHTMLToMarkdown(wantXHTML)
	if err != nil {
		t.Fatalf("StorageXHTMLToMarkdown: %v", err)
	}
	wantMDB, err := os.ReadFile(filepath.Join(dir, "want.xhtml.md"))
	if err != nil {
		t.Fatalf("read want.xhtml.md: %v", err)
	}
	wantMD := string(wantMDB)
	if gotMD != wantMD {
		t.Errorf("markdown mismatch.\n--- got ---\n%s\n--- want ---\n%s\n--- end ---",
			gotMD, wantMD)
	}
}
