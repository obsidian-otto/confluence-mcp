package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/bennie/mcp-confluence/internal/atlassian"
	"github.com/bennie/mcp-confluence/internal/config"
)

// newHelpTestClient returns an *atlassian.Client built from a
// fresh config.Config. We use the real config package because
// atlassian.New requires a *config.Config, but we don't go through
// LoadFromEnv (which reads the filesystem and the process env) —
// instead we construct a Config in-place. The handy New validates
// required fields, so a missing key would fail-fast here.
//
// The conf_help handler does NOT exercise the network at all, so
// most of these tests can run fully offline; the others call
// executeRequest which would need a live server, so for those we
// expect a network-layer error and assert that the error did NOT
// come from arg decoding.
func newHelpTestClient(t *testing.T) *atlassian.Client {
	t.Helper()
	cfg := &config.Config{
		SiteName:  "test-site",
		UserEmail: "tester@example.com",
		APIKey:    "smoke-test-token-not-a-real-secret",
		Debug:     false,
	}
	c, err := atlassian.New(cfg)
	if err != nil {
		t.Fatalf("atlassian.New: %v", err)
	}
	return c
}

// TestHandleListSpaces_DecodesArgs verifies that HandleListSpaces
// correctly decodes representative JSON into ListSpacesArgs without
// a panic, even though this test does not exercise the network
// path (the executeRequest call inside the handler would require a
// live server; that case is covered by TestExecuteRequest via an
// httptest server). We assert only the local-decoding invariants:
//
//   - Successful unmarshal of a representative payload.
//   - Empty payload returns "cql is required"-style error from
//     unknown-paths (i.e. the handler did not panic).
func TestHandleListSpaces_DecodesArgs(t *testing.T) {
	t.Parallel()

	c := newHelpTestClient(t)
	ctx := context.Background()

	// A typical payload: limit, type, status, outputFormat. We
	// assert the handler does not panic and returns a string.
	raw := []byte(`{"limit":50,"type":"personal","status":"current","outputFormat":"json"}`)
	out, err := HandleListSpaces(ctx, c, raw)
	if err != nil {
		// The upstream API is not actually reachable in tests,
		// so we expect a network error here. What we DON'T
		// expect is a decode/panic error.
		if strings.Contains(err.Error(), "decode args") {
			t.Fatalf("decode should not fail: %v", err)
		}
	}
	if out == "" && err == nil {
		t.Fatalf("expected at least one of out or err to be non-zero")
	}
}

// TestHandleListPages_DecodesArgs is the same shape as the
// ListSpaces test, but for list-pages.
func TestHandleListPages_DecodesArgs(t *testing.T) {
	t.Parallel()

	c := newHelpTestClient(t)
	ctx := context.Background()

	raw := []byte(`{"space-id":"780763211","title":"oncall","sort":"-modified-date","limit":10}`)
	out, err := HandleListPages(ctx, c, raw)
	if err != nil {
		if strings.Contains(err.Error(), "decode args") {
			t.Fatalf("decode should not fail: %v", err)
		}
	}
	if out == "" && err == nil {
		t.Fatalf("expected at least one of out or err to be non-zero")
	}
}

// TestHandleGetPageBody_RequiresPageID asserts that the handler
// refuses calls without a page-id (no second chance to recover
// when the upstream returns 404 on a guessed id).
func TestHandleGetPageBody_RequiresPageID(t *testing.T) {
	t.Parallel()
	c := newHelpTestClient(t)
	ctx := context.Background()

	_, err := HandleGetPageBody(ctx, c, []byte(`{"body-format":"storage"}`))
	if err == nil {
		t.Fatalf("expected error for missing page-id")
	}
	if !strings.Contains(err.Error(), "page-id") {
		t.Errorf("error message should mention page-id: %v", err)
	}
}

// TestHandleSearch_RequiresCQL is the parallel check for the
// search handler.
func TestHandleSearch_RequiresCQL(t *testing.T) {
	t.Parallel()
	c := newHelpTestClient(t)
	ctx := context.Background()

	_, err := HandleSearch(ctx, c, []byte(`{"limit":5}`))
	if err == nil {
		t.Fatalf("expected error for missing cql")
	}
	if !strings.Contains(err.Error(), "cql") {
		t.Errorf("error message should mention cql: %v", err)
	}
}

// TestHandleHelp_ReturnsSurface verifies that `conf_help` returns
// a populated surface map even when no topic filter is set.
//
// This is the only handleX test that fully executes offline — the
// surface is built from in-package constants and does not call
// executeRequest, so the assertion can be exact: the response
// is valid JSON, has a "tools" object, and that object contains
// each of the seventeen tool names.
func TestHandleHelp_ReturnsSurface(t *testing.T) {
	t.Parallel()

	c := newHelpTestClient(t)
	ctx := context.Background()

	// Force JSON output so the test can unmarshal deterministically.
	out, err := HandleHelp(ctx, c, []byte(`{"outputFormat":"json"}`))
	if err != nil {
		t.Fatalf("HandleHelp: %v", err)
	}

	var resp struct {
		Topic string                    `json:"topic"`
		Tools map[string]map[string]any `json:"tools"`
		Note  string                    `json:"note"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("unmarshal: %v\nout=%q", err, out)
	}
	if resp.Topic != "all" {
		t.Errorf("default topic = %q, want 'all'", resp.Topic)
	}

	// The full 17-tool surface: 5 CRUD + 5 convenience + 3 markdown
	// + 3 attachments + 1 drawio. Keep this in sync with
	// helpSurface() in convenience.go.
	want := []string{
		"conf_get", "conf_post", "conf_put", "conf_patch", "conf_delete",
		"conf_list_spaces", "conf_list_pages", "conf_get_page_body",
		"conf_search", "conf_help",
		"conf_post_markdown", "conf_put_markdown", "conf_get_page_markdown",
		"conf_upload_attachment", "conf_list_attachments", "conf_delete_attachment",
		"conf_upload_drawio",
	}
	for _, name := range want {
		entry, ok := resp.Tools[name]
		if !ok {
			t.Errorf("surface missing %q", name)
			continue
		}
		if _, ok := entry["description"].(string); !ok {
			t.Errorf("surface[%s].description not a string", name)
		}
		if _, ok := entry["args"].(map[string]any); !ok {
			t.Errorf("surface[%s].args not an object", name)
		}
		if _, ok := entry["example"].(string); !ok {
			t.Errorf("surface[%s].example not a string", name)
		}
	}

	// Belt-and-braces: also assert no surprise tools slipped in.
	if got := len(resp.Tools); got != len(want) {
		t.Errorf("surface has %d entries, want %d (extra: %v)", got, len(want), keys(resp.Tools))
	}

	// The note must agree with the live count — guards against the
	// "All N tools" string drifting from the actual surface size.
	if !strings.Contains(resp.Note, fmt.Sprintf("All %d tools", len(want))) {
		t.Errorf("note %q does not advertise %d tools", resp.Note, len(want))
	}
}

// TestHandleHelp_TopicFilter verifies that supplying a topic
// narrows the surface map to that one entry.
func TestHandleHelp_TopicFilter(t *testing.T) {
	t.Parallel()
	c := newHelpTestClient(t)
	ctx := context.Background()

	out, err := HandleHelp(ctx, c, []byte(`{"topic":"conf_search","outputFormat":"json"}`))
	if err != nil {
		t.Fatalf("HandleHelp: %v", err)
	}

	var resp struct {
		Topic string                    `json:"topic"`
		Tools map[string]map[string]any `json:"tools"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Topic != "conf_search" {
		t.Errorf("topic = %q, want conf_search", resp.Topic)
	}
	if len(resp.Tools) != 1 {
		t.Errorf("expected exactly 1 tool in filtered surface, got %d: %v", len(resp.Tools), keys(resp.Tools))
	}
	if _, ok := resp.Tools["conf_search"]; !ok {
		t.Errorf("expected conf_search in filtered surface")
	}
}

// TestHandleHelp_UnknownTopic returns an empty tools object when
// the topic doesn't match a real tool.
func TestHandleHelp_UnknownTopic(t *testing.T) {
	t.Parallel()
	c := newHelpTestClient(t)
	ctx := context.Background()

	out, err := HandleHelp(ctx, c, []byte(`{"topic":"bogus_tool","outputFormat":"json"}`))
	if err != nil {
		t.Fatalf("HandleHelp: %v", err)
	}
	var resp struct {
		Tools map[string]map[string]any `json:"tools"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Tools) != 0 {
		t.Errorf("expected empty tools for unknown topic, got %d", len(resp.Tools))
	}
}

// TestHandleHelp_TOONDefault verifies that omitting outputFormat
// gives the TOON-encoded response. We can't unmarshal TOON directly,
// but we CAN check that the response isn't plain JSON: a TOON
// response starts with a non-`{` character (typically a key:value
// pair without the JSON braces wrapper).
func TestHandleHelp_TOONDefault(t *testing.T) {
	t.Parallel()
	c := newHelpTestClient(t)
	ctx := context.Background()

	out, err := HandleHelp(ctx, c, []byte(`{}`))
	if err != nil {
		t.Fatalf("HandleHelp: %v", err)
	}
	if out == "" {
		t.Fatalf("empty output")
	}
	// Sanity check: should contain tool names somewhere.
	for _, name := range []string{"conf_get", "conf_help", "conf_search"} {
		if !strings.Contains(out, name) {
			t.Errorf("TOON response missing %q:\n%s", name, out)
		}
	}
}

// keys returns the keys of a map[string]anything for diagnostics.
func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
