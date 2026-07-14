// Package tools — page_tree_test.go: tests for the
// `conf_get_page_tree` handler, the multi-call orchestrator that
// fans out three v2 REST GETs (ancestors / children / descendants)
// and merges their envelopes into one response.
//
// These tests use the same testClient + httptest pattern as
// execute_test.go. The recorder is sharded by the path segment the
// handler appends to `/wiki/api/v2/pages/{id}/` so each sub-call
// gets its own canned response.
package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/bennie/mcp-confluence/internal/atlassian"
)

// multiRecorder holds three canned responses (ancestors / children /
// descendants) keyed by URL-path tail. A single httptest.Server
// serves all three endpoints; the handler's three sequential
// sub-calls are matched by path suffix. Each captured sub-call
// records the full URL so tests can assert on limit/depth queries.
type multiRecorder struct {
	mu       sync.Mutex
	calls    []string // URL paths seen, in order (with full query string)
	ancestor string   // JSON body for /ancestors
	children string   // JSON body for /children
	descend  string   // JSON body for /descendants
	status   int      // 200 unless set otherwise
	failOn   string   // if a sub-call hits this path tail, return 404
}

func (m *multiRecorder) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.calls = append(m.calls, r.URL.RequestURI())
		body := "{}"
		status := http.StatusOK
		if m.status != 0 {
			status = m.status
		}
		// Strip /wiki/api/v2/pages/ to get the page-id + tail.
		tail := strings.TrimPrefix(r.URL.Path, "/wiki/api/v2/pages/")
		switch {
		case m.failOn != "" && strings.HasSuffix(tail, m.failOn):
			// Force a fail on a specific sub-call.
			body = `{"code":"NOT_FOUND","message":"page not found"}`
			status = http.StatusNotFound
		case strings.HasSuffix(tail, "/ancestors"):
			body = m.ancestor
		case strings.HasSuffix(tail, "/children"):
			body = m.children
		case strings.HasSuffix(tail, "/descendants"):
			body = m.descend
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
}

// newPageTreeTestClient wraps multiRecorder in an httptest server
// and a *atlassian.Client pointed at it. The server is closed via
// t.Cleanup.
func newPageTreeTestClient(t *testing.T, m *multiRecorder) (*atlassian.Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(m.handler())
	t.Cleanup(srv.Close)
	c := testClient(t, srv)
	return c, srv
}

// TestHandleGetPageTree_RequiresPageID asserts the handler refuses
// calls without a page-id — same pattern as
// TestHandleGetPageBody_RequiresPageID and
// TestHandleSearch_RequiresCQL.
func TestHandleGetPageTree_RequiresPageID(t *testing.T) {
	t.Parallel()
	c, _ := newPageTreeTestClient(t, &multiRecorder{})

	_, err := HandleGetPageTree(context.Background(), c, []byte(`{"limit":10}`))
	if err == nil {
		t.Fatalf("expected error for missing page-id")
	}
	if !strings.Contains(err.Error(), "page-id") {
		t.Errorf("error message should mention page-id: %v", err)
	}
}

// TestHandleGetPageTree_BuildsAllThreePaths asserts the handler
// issues exactly three GETs to the v2 endpoints, one per
// /ancestors, /children, /descendants tail, and stops there.
func TestHandleGetPageTree_BuildsAllThreePaths(t *testing.T) {
	t.Parallel()
	rec := &multiRecorder{
		ancestor: `{"results":[],"_links":{}}`,
		children: `{"results":[],"_links":{}}`,
		descend:  `{"results":[],"_links":{}}`,
	}
	c, _ := newPageTreeTestClient(t, rec)

	_, err := HandleGetPageTree(context.Background(), c,
		[]byte(`{"page-id":"163935","limit":5,"depth":2}`))
	if err != nil {
		t.Fatalf("HandleGetPageTree: %v", err)
	}

	if len(rec.calls) != 3 {
		t.Fatalf("expected 3 sub-calls, got %d: %v", len(rec.calls), rec.calls)
	}
	// Assert each sub-call has the expected PAGE + tail without
	// requiring a specific limit query (the recorder captures the
	// full RequestURI including query).
	for i, wantTail := range []string{
		"163935/ancestors",
		"163935/children",
		"163935/descendants",
	} {
		if !strings.HasPrefix(rec.calls[i], "/wiki/api/v2/pages/"+wantTail) {
			t.Errorf("sub-call %d: want path /wiki/api/v2/pages/%s, got %q",
				i, wantTail, rec.calls[i])
		}
	}
}

// TestHandleGetPageTree_SuccessEnvelope asserts the merged envelope
// shape: {pageId, ancestors, children, descendants} where the three
// sub-call envelopes are preserved verbatim under those keys.
func TestHandleGetPageTree_SuccessEnvelope(t *testing.T) {
	t.Parallel()
	ancestorBody := `{"results":[{"id":"100","title":"root"}],"_links":{}}`
	childrenBody := `{"results":[{"id":"200","title":"childA"}],"_links":{}}`
	descendBody := `{"results":[{"id":"300","title":"grandchild"}],"_links":{}}`
	rec := &multiRecorder{
		ancestor: ancestorBody,
		children: childrenBody,
		descend:  descendBody,
	}
	c, _ := newPageTreeTestClient(t, rec)

	out, err := HandleGetPageTree(context.Background(), c,
		[]byte(`{"page-id":"163935","outputFormat":"json"}`))
	if err != nil {
		t.Fatalf("HandleGetPageTree: %v", err)
	}
	if out == "" {
		t.Fatal("empty output")
	}

	var got struct {
		PageID      string         `json:"pageId"`
		Ancestors   map[string]any `json:"ancestors"`
		Children    map[string]any `json:"children"`
		Descendants map[string]any `json:"descendants"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode response: %v\nresponse was: %s", err, out)
	}
	if got.PageID != "163935" {
		t.Errorf("pageId: want 163935, got %s", got.PageID)
	}
	if got.Ancestors["results"] == nil {
		t.Errorf("ancestors.results: missing in %s", out)
	}
	if got.Children["results"] == nil {
		t.Errorf("children.results: missing in %s", out)
	}
	if got.Descendants["results"] == nil {
		t.Errorf("descendants.results: missing in %s", out)
	}
	// And verify the actual results flowed through unmutated.
	aRes, _ := json.Marshal(got.Ancestors["results"])
	if !strings.Contains(string(aRes), `"id":"100"`) {
		t.Errorf("ancestors.results lost the upstream id: %s", aRes)
	}
	dRes, _ := json.Marshal(got.Descendants["results"])
	if !strings.Contains(string(dRes), `"id":"300"`) {
		t.Errorf("descendants.results lost the upstream id: %s", dRes)
	}
}

// TestHandleGetPageTree_TOONDefault asserts the default output
// format is TOON (NOT json). The ProjectLock says every handler's
// default output is TOON — execRequest tests assert that, and so
// does this.
func TestHandleGetPageTree_TOONDefault(t *testing.T) {
	t.Parallel()
	rec := &multiRecorder{
		ancestor: `{"results":[],"_links":{}}`,
		children: `{"results":[],"_links":{}}`,
		descend:  `{"results":[],"_links":{}}`,
	}
	c, _ := newPageTreeTestClient(t, rec)

	// No outputFormat — TOON default.
	out, err := HandleGetPageTree(context.Background(), c,
		[]byte(`{"page-id":"163935"}`))
	if err != nil {
		t.Fatalf("HandleGetPageTree: %v", err)
	}
	if out == "" {
		t.Fatal("empty output")
	}
	if strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("default output should be TOON, not raw JSON; got: %s", out)
	}
	if !strings.Contains(out, "pageId:") {
		t.Errorf("TOON output should contain 'pageId:' field; got: %s", out)
	}
	if !strings.Contains(out, "ancestors:") {
		t.Errorf("TOON output should contain 'ancestors:' field; got: %s", out)
	}
}

// TestHandleGetPageTree_PropagatesAncestors404 asserts that the
// first sub-call's failure (here: 404 on /ancestors) is propagated
// and the other two sub-calls are NOT issued. Fail-fast semantics.
func TestHandleGetPageTree_PropagatesAncestors404(t *testing.T) {
	t.Parallel()
	rec := &multiRecorder{
		// Subsequent sub-calls would return 200 if reached, but
		// we want to confirm they are NOT reached.
		ancestor: `{"results":[],"_links":{}}`,
		children: `{"results":[],"_links":{}}`,
		descend:  `{"results":[],"_links":{}}`,
		failOn:   "/ancestors",
	}
	c, _ := newPageTreeTestClient(t, rec)

	_, err := HandleGetPageTree(context.Background(), c,
		[]byte(`{"page-id":"163935","outputFormat":"json"}`))
	if err == nil {
		t.Fatal("expected error for 404 sub-call")
	}
	if !strings.Contains(err.Error(), "ancestors sub-call") {
		t.Errorf("error should mention the failing sub-call; got: %v", err)
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error should contain status 404; got: %v", err)
	}
	// Only the first sub-call should have hit the wire.
	if len(rec.calls) != 1 {
		t.Errorf("fail-fast: only 1 sub-call should have run, got %d: %v",
			len(rec.calls), rec.calls)
	}
}

// TestHandleGetPageTree_ClampLimitAndDepth asserts out-of-range
// limit/depth values are silently dropped rather than crashing (the
// endpoint defaults take over). The handler substitutes "" for
// non-positive or out-of-range values; the v2 client then omits
// empty query params from the URL (see internal/atlassian/client.go
// buildURL), so the captured RequestURI should NOT contain
// "limit=" or "depth=".
func TestHandleGetPageTree_ClampLimitAndDepth(t *testing.T) {
	t.Parallel()
	rec := &multiRecorder{
		ancestor: `{"results":[],"_links":{}}`,
		children: `{"results":[],"_links":{}}`,
		descend:  `{"results":[],"_links":{}}`,
	}
	c, _ := newPageTreeTestClient(t, rec)

	// limit=999 (above 250) and depth=999 (above 10) — should NOT
	// propagate as query params.
	_, err := HandleGetPageTree(context.Background(), c,
		[]byte(`{"page-id":"163935","limit":999,"depth":999}`))
	if err != nil {
		t.Fatalf("HandleGetPageTree: %v", err)
	}
	if len(rec.calls) != 3 {
		t.Fatalf("want 3 sub-calls, got %d", len(rec.calls))
	}
	for _, uri := range rec.calls {
		if strings.Contains(uri, "limit=") {
			t.Errorf("out-of-range limit should be stripped, but %s contains limit=", uri)
		}
	}
	// Depth should also be stripped (only on /descendants, but
	// we check all since /children + /ancestors ignore it).
	for _, uri := range rec.calls {
		if strings.Contains(uri, "depth=") {
			t.Errorf("out-of-range depth should be stripped, but %s contains depth=", uri)
		}
	}
}

// TestHandleGetPageTree_InRangeLimitAndDepth asserts in-range
// limit/depth values DO propagate as query params.
func TestHandleGetPageTree_InRangeLimitAndDepth(t *testing.T) {
	t.Parallel()
	rec := &multiRecorder{
		ancestor: `{"results":[],"_links":{}}`,
		children: `{"results":[],"_links":{}}`,
		descend:  `{"results":[],"_links":{}}`,
	}
	c, _ := newPageTreeTestClient(t, rec)

	_, err := HandleGetPageTree(context.Background(), c,
		[]byte(`{"page-id":"163935","limit":7,"depth":3}`))
	if err != nil {
		t.Fatalf("HandleGetPageTree: %v", err)
	}
	if len(rec.calls) != 3 {
		t.Fatalf("want 3 sub-calls, got %d", len(rec.calls))
	}
	for i, uri := range rec.calls {
		if !strings.Contains(uri, "limit=7") {
			t.Errorf("sub-call %d (%s) should contain limit=7", i, uri)
		}
	}
	// Only the descendants sub-call should have depth.
	if !strings.Contains(rec.calls[2], "depth=3") {
		t.Errorf("descendants sub-call (%s) should contain depth=3", rec.calls[2])
	}
	if strings.Contains(rec.calls[0], "depth=") || strings.Contains(rec.calls[1], "depth=") {
		t.Errorf("ancestors/children should ignore depth; got: %s, %s",
			rec.calls[0], rec.calls[1])
	}
}
