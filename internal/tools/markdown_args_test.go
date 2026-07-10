// markdown_args_test.go — Phase 14: round-trip JSON unmarshal tests
// for the three new v2 markdown args types (PostMarkdownArgs,
// PutMarkdownArgs, GetPageMarkdownArgs). These tests exercise the
// public JSON shape an MCP caller would send over JSON-RPC —
// the keys match the spec at
// specs/10-markdown-roundtrip/04-tool-surface.md.
//
// The tests are deliberately simple shape-only assertions; the
// per-handler conversion behaviour (md→XHTML via
// internal/markdown.MarkdownToStorageXHTML) is exercised in
// markdown_handlers_test.go. The args types do not depend on
// internal/markdown, so this file is self-contained.
package tools

import (
	"encoding/json"
	"testing"
)

// TestMarkdownArgsRoundTrip covers the three new args types added
// in Phase 14 (v2 markdown round-trip). Each must round-trip a
// representative JSON payload with field values intact, and must
// serialise back without omitempty dropping the values supplied.
//
// These mirror the TestArgsRoundTrip / TestNewConvenienceArgsRoundTrip
// patterns used in args_test.go so the test surface for all 13 args
// types is consistent.
func TestMarkdownArgsRoundTrip(t *testing.T) {
	t.Parallel()

	t.Run("PostMarkdown", func(t *testing.T) {
		raw := []byte(`{
			"spaceId":"780763211",
			"title":"My new page",
			"markdown":"# Hello\n\nA short page.",
			"parentId":"163935",
			"status":"current",
			"outputFormat":"json"
		}`)

		var got PostMarkdownArgs
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("unmarshal PostMarkdownArgs: %v", err)
		}
		if got.SpaceID != "780763211" {
			t.Errorf("SpaceID = %q, want 780763211", got.SpaceID)
		}
		if got.Title != "My new page" {
			t.Errorf("Title = %q", got.Title)
		}
		if got.Markdown != "# Hello\n\nA short page." {
			t.Errorf("Markdown = %q", got.Markdown)
		}
		if got.MarkdownFile != "" {
			t.Errorf("MarkdownFile = %q, want empty (markdown was supplied)", got.MarkdownFile)
		}
		if got.ParentID != "163935" {
			t.Errorf("ParentID = %q", got.ParentID)
		}
		if got.Status != "current" {
			t.Errorf("Status = %q", got.Status)
		}
		if got.OutputFormat != "json" {
			t.Errorf("OutputFormat = %q", got.OutputFormat)
		}

		// Re-marshal and check stability: scalar fields survive.
		out, err := json.Marshal(got)
		if err != nil {
			t.Fatalf("marshal PostMarkdownArgs: %v", err)
		}
		var roundtrip PostMarkdownArgs
		if err := json.Unmarshal(out, &roundtrip); err != nil {
			t.Fatalf("re-unmarshal PostMarkdownArgs: %v", err)
		}
		if roundtrip.SpaceID != got.SpaceID ||
			roundtrip.Title != got.Title ||
			roundtrip.Markdown != got.Markdown ||
			roundtrip.ParentID != got.ParentID ||
			roundtrip.Status != got.Status ||
			roundtrip.OutputFormat != got.OutputFormat {
			t.Errorf("scalar roundtrip changed: before=%+v after=%+v", got, roundtrip)
		}
	})

	t.Run("PutMarkdown", func(t *testing.T) {
		raw := []byte(`{
			"pageId":"163935",
			"title":"Renamed",
			"markdown":"## Section\n\nMore text.",
			"outputFormat":"json"
		}`)

		var got PutMarkdownArgs
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("unmarshal PutMarkdownArgs: %v", err)
		}
		if got.PageID != "163935" {
			t.Errorf("PageID = %q", got.PageID)
		}
		if got.Title != "Renamed" {
			t.Errorf("Title = %q", got.Title)
		}
		if got.Markdown != "## Section\n\nMore text." {
			t.Errorf("Markdown = %q", got.Markdown)
		}
		if got.OutputFormat != "json" {
			t.Errorf("OutputFormat = %q", got.OutputFormat)
		}

		// Re-marshal and check stability.
		out, err := json.Marshal(got)
		if err != nil {
			t.Fatalf("marshal PutMarkdownArgs: %v", err)
		}
		var roundtrip PutMarkdownArgs
		if err := json.Unmarshal(out, &roundtrip); err != nil {
			t.Fatalf("re-unmarshal PutMarkdownArgs: %v", err)
		}
		if roundtrip.PageID != got.PageID ||
			roundtrip.Title != got.Title ||
			roundtrip.Markdown != got.Markdown ||
			roundtrip.OutputFormat != got.OutputFormat {
			t.Errorf("scalar roundtrip changed: before=%+v after=%+v", got, roundtrip)
		}
	})

	t.Run("PutMarkdownWithFile", func(t *testing.T) {
		// The handler picks `markdown` over `markdownFile` when both
		// are present. The args struct accepts both — the wire
		// shape is "either/or". Round-trip must preserve both.
		raw := []byte(`{
			"pageId":"163935",
			"title":"From file",
			"markdownFile":"/tmp/page.md"
		}`)

		var got PutMarkdownArgs
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("unmarshal PutMarkdownArgs: %v", err)
		}
		if got.PageID != "163935" {
			t.Errorf("PageID = %q", got.PageID)
		}
		if got.Title != "From file" {
			t.Errorf("Title = %q", got.Title)
		}
		if got.MarkdownFile != "/tmp/page.md" {
			t.Errorf("MarkdownFile = %q", got.MarkdownFile)
		}
		if got.Markdown != "" {
			t.Errorf("Markdown = %q, want empty (markdownFile was supplied)", got.Markdown)
		}
	})

	t.Run("GetPageMarkdown", func(t *testing.T) {
		raw := []byte(`{"page-id":"163935","outputFormat":"json"}`)

		var got GetPageMarkdownArgs
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("unmarshal GetPageMarkdownArgs: %v", err)
		}
		if got.PageID != "163935" {
			t.Errorf("PageID = %q", got.PageID)
		}
		if got.OutputFormat != "json" {
			t.Errorf("OutputFormat = %q", got.OutputFormat)
		}

		out, err := json.Marshal(got)
		if err != nil {
			t.Fatalf("marshal GetPageMarkdownArgs: %v", err)
		}
		var roundtrip GetPageMarkdownArgs
		if err := json.Unmarshal(out, &roundtrip); err != nil {
			t.Fatalf("re-unmarshal GetPageMarkdownArgs: %v", err)
		}
		if roundtrip.PageID != got.PageID ||
			roundtrip.OutputFormat != got.OutputFormat {
			t.Errorf("scalar roundtrip changed: before=%+v after=%+v", got, roundtrip)
		}
	})
}

// TestMarkdownArgsOmitEmpty verifies the JSON marshalling for the
// three new args types: optional fields with the omitempty tag
// should be dropped from the canonical output when at their zero
// value. Required-ish fields (e.g. spaceId, pageId, title) must
// always be materialised so the handler layer can validate.
func TestMarkdownArgsOmitEmpty(t *testing.T) {
	t.Parallel()

	t.Run("PostMarkdown omits zero fields except required shape", func(t *testing.T) {
		got := PostMarkdownArgs{SpaceID: "1", Title: "x", Markdown: "y"}
		out, err := json.Marshal(got)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		s := string(out)
		// Must include the required fields.
		for _, want := range []string{`"spaceId":"1"`, `"title":"x"`, `"markdown":"y"`} {
			if !contains(s, want) {
				t.Errorf("marshal missing %q: %s", want, s)
			}
		}
		// omitempty fields should be dropped.
		for _, absent := range []string{`"markdownFile"`, `"parentId"`, `"status"`, `"outputFormat"`} {
			if contains(s, absent) {
				t.Errorf("marshal should omit %q when zero: %s", absent, s)
			}
		}
	})

	t.Run("PutMarkdown omits zero fields except required shape", func(t *testing.T) {
		got := PutMarkdownArgs{PageID: "1", Title: "x", Markdown: "y"}
		out, err := json.Marshal(got)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		s := string(out)
		for _, want := range []string{`"pageId":"1"`, `"title":"x"`, `"markdown":"y"`} {
			if !contains(s, want) {
				t.Errorf("marshal missing %q: %s", want, s)
			}
		}
		for _, absent := range []string{`"markdownFile"`, `"outputFormat"`} {
			if contains(s, absent) {
				t.Errorf("marshal should omit %q when zero: %s", absent, s)
			}
		}
	})

	t.Run("GetPageMarkdown omits zero outputFormat", func(t *testing.T) {
		got := GetPageMarkdownArgs{PageID: "1"}
		out, err := json.Marshal(got)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		s := string(out)
		if !contains(s, `"page-id":"1"`) {
			t.Errorf("marshal missing page-id: %s", s)
		}
		if contains(s, `"outputFormat"`) {
			t.Errorf("marshal should omit outputFormat when zero: %s", s)
		}
	})
}
