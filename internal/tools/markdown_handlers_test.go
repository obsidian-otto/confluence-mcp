// markdown_handlers_test.go — Phase 14: per-handler unit tests for
// the v2 markdown tools (HandlePostMarkdown, HandlePutMarkdown,
// HandleGetPageMarkdown).
//
// Each test exercises the handler's three responsibilities:
//
//	(a) CONVERSION: the handler must call
//	    internal/markdown.MarkdownToStorageXHTML (or
//	    StorageXHTMLToMarkdown) and use the result on the wire.
//	    We assert the body sent to the test server is the conversion
//	    output, byte-for-byte (the storage XHTML appears inside the
//	    envelope's `body.value` field).
//
//	(b) ENVELOPE: the wire shape is the same as HandlePost /
//	    HandlePut / HandleGetPageBody. The body is
//	    {representation: "storage", value: <XHTML>} for post/put;
//	    the GET response is the storage envelope the existing
//	    HandleGetPageBody returns.
//
//	(c) DELEGATION: the handler does NOT re-implement the wire
//	    call. It converts and then calls the existing
//	    HandlePost / HandlePut / HandleGetPageBody helper. We
//	    verify this by:
//
//	      - for post/put: asserting the request method (POST/PUT)
//	        and path (/wiki/api/v2/pages[/{id}]) match the CRUD
//	        handler contract.
//	      - for get: asserting the request path matches the
//	        templates.PageBodyPath output (the same path
//	        HandleGetPageBody builds).
//
// The tests use the existing newTestClient / requestRecorder
// harness from execute_test.go (same package). The test server
// responds with a Confluence-shaped JSON envelope so the response
// decoding path in atlassian.Client.Call works the same way it
// would in production.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bennie/mcp-confluence/internal/markdown"
)

// TestHandlePostMarkdown_ConversionAndEnvelope is the canonical
// test for HandlePostMarkdown. It asserts:
//
//   - The handler accepts a PostMarkdownArgs with an inline
//     `markdown` body.
//   - It calls markdown.MarkdownToStorageXHTML and embeds the
//     result inside a `{representation: "storage", value: <XHTML>}`
//     envelope.
//   - It delegates to HandlePost (method=POST, path=
//     /wiki/api/v2/pages).
//   - The other PostArgs fields (spaceId, title, status) are
//     forwarded verbatim.
func TestHandlePostMarkdown_ConversionAndEnvelope(t *testing.T) {
	h := newHandlerHarness(t)
	h.rec.setResponse(http.StatusOK, `{"id":"page-99","title":"My new page","version":{"number":1}}`)

	md := "# Hello\n\nThis is **bold**.\n"
	raw := json.RawMessage(fmt.Sprintf(`{
		"spaceId":"780763211",
		"title":"My new page",
		"markdown":%q
	}`, md))

	got, err := HandlePostMarkdown(context.Background(), h.client, raw)
	if err != nil {
		t.Fatalf("HandlePostMarkdown: %v", err)
	}
	if got == "" {
		t.Fatal("HandlePostMarkdown returned empty string")
	}

	// (c) DELEGATION — assert method and path match the upstream
	// HandlePost contract.
	if h.rec.method != "POST" {
		t.Errorf("method = %q, want POST", h.rec.method)
	}
	if h.rec.path != "/wiki/api/v2/pages" {
		t.Errorf("path = %q, want /wiki/api/v2/pages", h.rec.path)
	}

	// (b) ENVELOPE — the body must be a JSON object containing the
	// Confluence v2 page-create shape.
	var sent map[string]any
	if err := json.Unmarshal(h.rec.body, &sent); err != nil {
		t.Fatalf("body is not valid JSON: %v\nbody=%q", err, h.rec.body)
	}
	if sent["spaceId"] != "780763211" {
		t.Errorf("body.spaceId = %v, want 780763211", sent["spaceId"])
	}
	if sent["title"] != "My new page" {
		t.Errorf("body.title = %v", sent["title"])
	}
	if sent["status"] != "current" {
		// status is defaulting to "current" inside the handler.
		// (We didn't set status in the input, so the handler
		// may either omit it or fill in the default. We accept
		// both: the assertion is on the value when set.)
		if _, hasStatus := sent["status"]; hasStatus {
			t.Errorf("body.status = %v, want 'current' or absent", sent["status"])
		}
	}

	// (a) CONVERSION — the body.value must be the exact output of
	// markdown.MarkdownToStorageXHTML(md).
	expected, err := markdown.MarkdownToStorageXHTML(md)
	if err != nil {
		t.Fatalf("markdown.MarkdownToStorageXHTML: %v", err)
	}
	gotBody, ok := sent["body"].(map[string]any)
	if !ok {
		t.Fatalf("body.body is not an object: %T (%v)", sent["body"], sent["body"])
	}
	if gotBody["representation"] != "storage" {
		t.Errorf("body.body.representation = %v, want 'storage'", gotBody["representation"])
	}
	gotValue, ok := gotBody["value"].(string)
	if !ok {
		t.Fatalf("body.body.value is not a string: %T (%v)", gotBody["value"], gotBody["value"])
	}
	if gotValue != expected {
		t.Errorf("body.body.value does not match MarkdownToStorageXHTML output\n  got:  %q\n  want: %q", gotValue, expected)
	}
}

// TestHandlePutMarkdown_ConversionAndEnvelope — the PUT path
// mirrors POST but targets /wiki/api/v2/pages/{id} and includes
// the page id in the body.
func TestHandlePutMarkdown_ConversionAndEnvelope(t *testing.T) {
	h := newHandlerHarness(t)
	h.rec.setResponse(http.StatusOK, `{"id":"42","version":{"number":3}}`)

	md := "## Updated section\n\nMore text."
	raw := json.RawMessage(fmt.Sprintf(`{
		"pageId":"42",
		"title":"Renamed",
		"markdown":%q
	}`, md))

	got, err := HandlePutMarkdown(context.Background(), h.client, raw)
	if err != nil {
		t.Fatalf("HandlePutMarkdown: %v", err)
	}
	if got == "" {
		t.Fatal("HandlePutMarkdown returned empty string")
	}

	if h.rec.method != "PUT" {
		t.Errorf("method = %q, want PUT", h.rec.method)
	}
	if h.rec.path != "/wiki/api/v2/pages/42" {
		t.Errorf("path = %q, want /wiki/api/v2/pages/42", h.rec.path)
	}

	var sent map[string]any
	if err := json.Unmarshal(h.rec.body, &sent); err != nil {
		t.Fatalf("body is not valid JSON: %v\nbody=%q", err, h.rec.body)
	}
	if sent["id"] != "42" {
		t.Errorf("body.id = %v, want 42", sent["id"])
	}
	if sent["title"] != "Renamed" {
		t.Errorf("body.title = %v", sent["title"])
	}

	expected, err := markdown.MarkdownToStorageXHTML(md)
	if err != nil {
		t.Fatalf("markdown.MarkdownToStorageXHTML: %v", err)
	}
	gotBody, ok := sent["body"].(map[string]any)
	if !ok {
		t.Fatalf("body.body is not an object: %T (%v)", sent["body"], sent["body"])
	}
	if gotBody["representation"] != "storage" {
		t.Errorf("body.body.representation = %v, want 'storage'", gotBody["representation"])
	}
	gotValue, ok := gotBody["value"].(string)
	if !ok {
		t.Fatalf("body.body.value is not a string: %T (%v)", gotBody["value"], gotBody["value"])
	}
	if gotValue != expected {
		t.Errorf("body.body.value does not match MarkdownToStorageXHTML output\n  got:  %q\n  want: %q", gotValue, expected)
	}
}

// TestHandlePostMarkdown_MarkdownFile — the handler picks
// `markdownFile` over `markdown` when only the file is supplied.
// The test writes a small markdown file to a t.TempDir(), sends
// the file path as the args, and asserts the file's contents
// appear on the wire after conversion.
func TestHandlePostMarkdown_MarkdownFile(t *testing.T) {
	h := newHandlerHarness(t)
	h.rec.setResponse(http.StatusOK, `{"id":"page-99","version":{"number":1}}`)

	dir := t.TempDir()
	path := filepath.Join(dir, "page.md")
	md := "# From file\n\nfile-based body."
	if err := os.WriteFile(path, []byte(md), 0o600); err != nil {
		t.Fatalf("write temp md: %v", err)
	}

	raw := json.RawMessage(fmt.Sprintf(`{
		"spaceId":"780763211",
		"title":"From file",
		"markdownFile":%q
	}`, path))

	if _, err := HandlePostMarkdown(context.Background(), h.client, raw); err != nil {
		t.Fatalf("HandlePostMarkdown: %v", err)
	}

	// The conversion output for the file contents is what must
	// appear on the wire.
	expected, err := markdown.MarkdownToStorageXHTML(md)
	if err != nil {
		t.Fatalf("markdown.MarkdownToStorageXHTML: %v", err)
	}
	var sent map[string]any
	if err := json.Unmarshal(h.rec.body, &sent); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}
	gotBody, _ := sent["body"].(map[string]any)
	gotValue, _ := gotBody["value"].(string)
	if gotValue != expected {
		t.Errorf("body.body.value does not match MarkdownToStorageXHTML output for file contents\n  got:  %q\n  want: %q", gotValue, expected)
	}
}

// TestHandlePostMarkdown_RequiresMarkdown — at least one of
// `markdown` or `markdownFile` must be set; otherwise the handler
// returns a clear error (no upstream call, no body sent).
func TestHandlePostMarkdown_RequiresMarkdown(t *testing.T) {
	h := newHandlerHarness(t)

	raw := json.RawMessage(`{"spaceId":"1","title":"x"}`)
	out, err := HandlePostMarkdown(context.Background(), h.client, raw)
	if err == nil {
		t.Fatalf("expected error for missing markdown source; got result %q", out)
	}
	if !strings.Contains(err.Error(), "markdown") {
		t.Errorf("error message should mention 'markdown': %v", err)
	}
	if h.rec.method != "" {
		t.Errorf("no upstream call should have been made; got method %q", h.rec.method)
	}
}

// TestHandlePostMarkdown_MarkdownFileTooLarge — the 1 MB cap is
// enforced. The test writes a 2 MB file and asserts the handler
// returns a clear error before any upstream call.
func TestHandlePostMarkdown_MarkdownFileTooLarge(t *testing.T) {
	h := newHandlerHarness(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "huge.md")
	// 2 MB of "a" — well over the 1 MB cap.
	big := strings.Repeat("a", 2*1024*1024)
	if err := os.WriteFile(path, []byte(big), 0o600); err != nil {
		t.Fatalf("write temp md: %v", err)
	}

	raw := json.RawMessage(fmt.Sprintf(`{"spaceId":"1","title":"x","markdownFile":%q}`, path))
	out, err := HandlePostMarkdown(context.Background(), h.client, raw)
	if err == nil {
		t.Fatalf("expected error for oversized markdown file; got result %q", out)
	}
	if !strings.Contains(err.Error(), "size") && !strings.Contains(err.Error(), "1 MB") && !strings.Contains(err.Error(), "limit") {
		t.Errorf("error message should mention the size limit: %v", err)
	}
}

// TestHandlePostMarkdown_BadJSON — malformed JSON in args must
// return a clear error without crashing.
func TestHandlePostMarkdown_BadJSON(t *testing.T) {
	h := newHandlerHarness(t)
	_, err := HandlePostMarkdown(context.Background(), h.client, json.RawMessage(`{not json`))
	if err == nil {
		t.Fatal("expected error for bad JSON")
	}
}

// TestHandleGetPageMarkdown_ConversionAndEnvelope — the GET path
// calls HandleGetPageBody (which builds the
// /wiki/api/v2/pages/{id}?body-format=storage path), then
// converts the response's body.value to markdown. The handler
// returns a JSON envelope {pageId, title, markdown}.
func TestHandleGetPageMarkdown_ConversionAndEnvelope(t *testing.T) {
	// Build a Confluence v2 page response whose body.value is the
	// exact XHTML that markdown.MarkdownToStorageXHTML would
	// produce for some markdown. The handler must call
	// StorageXHTMLToMarkdown on this value to get the markdown
	// text back, then wrap it in the response envelope.
	md := "# Round trip\n\nThis should come back as markdown."
	xhtml, err := markdown.MarkdownToStorageXHTML(md)
	if err != nil {
		t.Fatalf("markdown.MarkdownToStorageXHTML: %v", err)
	}
	pageRespJSON := fmt.Sprintf(`{
		"id":"163935",
		"title":"Round trip",
		"body":{"representation":"storage","value":%q}
	}`, xhtml)

	recH := newHandlerHarness(t)
	recH.rec.setResponse(http.StatusOK, pageRespJSON)

	raw := json.RawMessage(`{"page-id":"163935","outputFormat":"json"}`)
	got, err := HandleGetPageMarkdown(context.Background(), recH.client, raw)
	if err != nil {
		t.Fatalf("HandleGetPageMarkdown: %v", err)
	}
	if got == "" {
		t.Fatal("HandleGetPageMarkdown returned empty string")
	}

	// (c) DELEGATION — the path must match the
	// HandleGetPageBody contract: /wiki/api/v2/pages/{id}?body-format=storage
	if !strings.Contains(recH.rec.path, "/wiki/api/v2/pages/163935") {
		t.Errorf("path = %q, want it to contain /wiki/api/v2/pages/163935", recH.rec.path)
	}
	if !strings.Contains(recH.rec.rawQuery, "body-format=storage") {
		t.Errorf("rawQuery = %q, want it to contain body-format=storage", recH.rec.rawQuery)
	}
}

// TestHandleGetPageMarkdown_ReturnsEnvelopeShape — the response
// shape is {pageId, title, markdown} (TOON/JSON encoded). The
// markdown text must include the user-visible text of the page
// (the heading, the paragraph) so a caller can read it.
func TestHandleGetPageMarkdown_ReturnsEnvelopeShape(t *testing.T) {
	md := "# My title\n\nbody."
	xhtml, err := markdown.MarkdownToStorageXHTML(md)
	if err != nil {
		t.Fatalf("markdown.MarkdownToStorageXHTML: %v", err)
	}
	pageRespJSON := fmt.Sprintf(`{
		"id":"7",
		"title":"My title",
		"body":{"representation":"storage","value":%q}
	}`, xhtml)

	recH := newHandlerHarness(t)
	recH.rec.setResponse(http.StatusOK, pageRespJSON)

	raw := json.RawMessage(`{"page-id":"7","outputFormat":"json"}`)
	got, err := HandleGetPageMarkdown(context.Background(), recH.client, raw)
	if err != nil {
		t.Fatalf("HandleGetPageMarkdown: %v", err)
	}

	// Unmarshal as JSON (we asked for outputFormat=json).
	var env struct {
		PageID   string `json:"pageId"`
		Title    string `json:"title"`
		Markdown string `json:"markdown"`
	}
	if err := json.Unmarshal([]byte(got), &env); err != nil {
		t.Fatalf("response is not valid JSON: %v\nresponse: %q", err, got)
	}
	if env.PageID != "7" {
		t.Errorf("envelope.pageId = %q, want 7", env.PageID)
	}
	if env.Title != "My title" {
		t.Errorf("envelope.title = %q, want My title", env.Title)
	}
	if env.Markdown == "" {
		t.Errorf("envelope.markdown is empty; want non-empty")
	}
	// The markdown should contain the user-visible text of the
	// original page (the heading and the body word). The
	// StorageXHTMLToMarkdown conversion may add some markdown
	// syntax around it, so we check the tokens are present.
	for _, want := range []string{"My title", "body"} {
		if !strings.Contains(env.Markdown, want) {
			t.Errorf("envelope.markdown missing %q: %q", want, env.Markdown)
		}
	}
}

// TestHandleGetPageMarkdown_RequiresPageID — the page-id field
// is required.
func TestHandleGetPageMarkdown_RequiresPageID(t *testing.T) {
	h := newHandlerHarness(t)
	_, err := HandleGetPageMarkdown(context.Background(), h.client, json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error for missing page-id")
	}
	if !strings.Contains(err.Error(), "page-id") {
		t.Errorf("error should mention page-id: %v", err)
	}
}

// TestHandlePutMarkdown_RequiresPageID — the page-id field is
// required for the PUT path too.
func TestHandlePutMarkdown_RequiresPageID(t *testing.T) {
	h := newHandlerHarness(t)
	raw := json.RawMessage(`{"title":"x","markdown":"y"}`)
	_, err := HandlePutMarkdown(context.Background(), h.client, raw)
	if err == nil {
		t.Fatal("expected error for missing pageId")
	}
	if !strings.Contains(err.Error(), "pageId") && !strings.Contains(err.Error(), "page-id") {
		t.Errorf("error should mention pageId/page-id: %v", err)
	}
}

// TestHandlePostMarkdown_RequiresSpaceID — the spaceId field is
// required for the POST path.
func TestHandlePostMarkdown_RequiresSpaceID(t *testing.T) {
	h := newHandlerHarness(t)
	raw := json.RawMessage(`{"title":"x","markdown":"y"}`)
	_, err := HandlePostMarkdown(context.Background(), h.client, raw)
	if err == nil {
		t.Fatal("expected error for missing spaceId")
	}
	if !strings.Contains(err.Error(), "spaceId") {
		t.Errorf("error should mention spaceId: %v", err)
	}
}
