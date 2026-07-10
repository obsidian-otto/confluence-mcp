// drawio_handler_test.go — handler tests for the v3
// `conf_upload_drawio` tool. The orchestrator's flow is harder
// to test in isolation than the lower-level handlers because it
// makes 3 separate HTTP calls (create page if needed, upload
// attachment, update page body). We assert:
//
//   - args validation: exactly-one of pageId/spaceId, exactly-one
//     of drawioFile/drawioPngFile/drawioSvgFile, non-empty file
//     path, etc.
//   - happy path: a drawio file uploaded to an existing page
//     produces a 200 response envelope with attachmentId,
//     pageId, and diagramName.
//   - new-page path: spaceId + title triggers a page creation
//     before the upload step (we check the recorder captured
//     the POST + PUT in the right order).
//   - XHTML shape: the PUT body contains a
//     <ac:structured-macro ac:name="drawio"> with the
//     diagramName parameter.
package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bennie/mcp-confluence/internal/atlassian"
)

// TestHandleUploadDrawio_RequiresExactlyOneInput asserts the
// args mutex: exactly one of drawioFile/drawioPngFile/drawioSvgFile
// must be set.
func TestHandleUploadDrawio_RequiresExactlyOneInput(t *testing.T) {
	srv, _ := newTestClient(t)
	c := testClient(t, srv)

	tmpDir := t.TempDir()
	drawioPath := filepath.Join(tmpDir, "x.drawio")
	if err := os.WriteFile(drawioPath, []byte(`<mxfile/>`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cases := []struct {
		name string
		raw  string
	}{
		{"no input file", `{"pageId":"1"}`},
		{"two input files", `{"pageId":"1","drawioFile":"` + drawioPath + `","drawioPngFile":"` + drawioPath + `"}`},
		{"all three", `{"pageId":"1","drawioFile":"` + drawioPath + `","drawioPngFile":"` + drawioPath + `","drawioSvgFile":"` + drawioPath + `"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := HandleUploadDrawio(context.Background(), c, json.RawMessage(tc.raw))
			if err == nil {
				t.Errorf("expected error for %s, got nil", tc.name)
			}
			if !strings.Contains(err.Error(), "conf_upload_drawio") {
				t.Errorf("error %q does not name the tool", err.Error())
			}
		})
	}
}

// TestHandleUploadDrawio_RequiresExactlyOneTarget asserts the
// args mutex: exactly one of pageId OR (spaceId+title) must be set.
func TestHandleUploadDrawio_RequiresExactlyOneTarget(t *testing.T) {
	srv, _ := newTestClient(t)
	c := testClient(t, srv)

	tmpDir := t.TempDir()
	drawioPath := filepath.Join(tmpDir, "x.drawio")
	if err := os.WriteFile(drawioPath, []byte(`<mxfile/>`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cases := []struct {
		name string
		raw  string
	}{
		{"neither", `{"drawioFile":"` + drawioPath + `"}`},
		{"both", `{"pageId":"1","spaceId":"2","title":"x","drawioFile":"` + drawioPath + `"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := HandleUploadDrawio(context.Background(), c, json.RawMessage(tc.raw))
			if err == nil {
				t.Errorf("expected error for %s, got nil", tc.name)
			}
		})
	}
}

// TestHandleUploadDrawio_MissingFile asserts the file-read error
// path: a drawioFile pointing at a non-existent path fails
// cleanly.
func TestHandleUploadDrawio_MissingFile(t *testing.T) {
	srv, _ := newTestClient(t)
	c := testClient(t, srv)

	raw := json.RawMessage(`{"pageId":"1","drawioFile":"/nonexistent/path.drawio"}`)
	_, err := HandleUploadDrawio(context.Background(), c, raw)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "read") {
		t.Errorf("error %q does not mention file read failure", err.Error())
	}
}

// TestHandleUploadDrawio_ExistingPage_FlowOrder asserts the
// happy-path: an existing page gets a 2-call flow (v1 upload,
// then v2 PUT body). The recorder captures both calls and we
// check the order.
func TestHandleUploadDrawio_ExistingPage_FlowOrder(t *testing.T) {
	// Multi-call recorder: each request appends to a slice.
	type call struct{ method, path string }
	var calls []call
	srv, _ := newTestClient(t)
	// Replace the default recorder with a multi-call one.
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, call{r.Method, r.URL.Path})
		w.Header().Set("Content-Type", "application/json")
		// First call (upload v1) returns a ContentPageScheme
		// envelope with one attachment. Subsequent calls
		// (page GET + PUT) return a page envelope.
		switch {
		case r.Method == "POST" && r.URL.Path == "/wiki/rest/api/content/163935/child/attachment":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"results":[{"id":"att999","title":"arch.drawio.png","version":1}]}`))
		case r.Method == "GET" && r.URL.Path == "/wiki/api/v2/pages/163935":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"163935","title":"Smoke test","version":3}`))
		case r.Method == "PUT" && r.URL.Path == "/wiki/api/v2/pages/163935":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"163935","title":"Smoke test","version":4}`))
		default:
			t.Errorf("unexpected call: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	})
	c := testClient(t, srv)

	tmpDir := t.TempDir()
	drawioPath := filepath.Join(tmpDir, "arch.drawio")
	if err := os.WriteFile(drawioPath, []byte(`<mxfile><diagram name="arch"/></mxfile>`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	raw := json.RawMessage(`{"pageId":"163935","drawioFile":"` + drawioPath + `","outputFormat":"json"}`)
	out, err := HandleUploadDrawio(context.Background(), c, raw)
	if err != nil {
		t.Fatalf("HandleUploadDrawio: %v", err)
	}

	// Decode the JSON envelope.
	var env map[string]any
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output not valid JSON: %v\nout=%s", err, out)
	}
	if env["attachmentId"] != "att999" {
		t.Errorf("attachmentId = %v, want att999", env["attachmentId"])
	}
	if env["diagramName"] != "arch" {
		t.Errorf("diagramName = %v, want arch (derived from filename)", env["diagramName"])
	}
	page, ok := env["page"].(map[string]any)
	if !ok {
		t.Fatalf("page not an object: %T", env["page"])
	}
	if page["id"] != "163935" {
		t.Errorf("page.id = %v, want 163935", page["id"])
	}

	// Assert the call order: upload must happen before PUT
	// (because we need the attachment on the page before we
	// can reference it in the macro). GET on /pages/{id} may
	// happen between them to fetch the version.
	if len(calls) < 2 {
		t.Fatalf("expected >=2 calls, got %d: %+v", len(calls), calls)
	}
	if calls[0].method != "POST" || calls[0].path != "/wiki/rest/api/content/163935/child/attachment" {
		t.Errorf("call 0 = %+v, want POST /wiki/rest/api/content/163935/child/attachment", calls[0])
	}
	if calls[len(calls)-1].method != "PUT" || calls[len(calls)-1].path != "/wiki/api/v2/pages/163935" {
		t.Errorf("last call = %+v, want PUT /wiki/api/v2/pages/163935", calls[len(calls)-1])
	}
}

// TestHandleUploadDrawio_NewPage_CreatesFirst asserts the
// spaceId+title path triggers a POST to /wiki/api/v2/pages
// BEFORE the upload.
func TestHandleUploadDrawio_NewPage_CreatesFirst(t *testing.T) {
	type call struct{ method, path string }
	var calls []call
	srv, _ := newTestClient(t)
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, call{r.Method, r.URL.Path})
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "POST" && r.URL.Path == "/wiki/api/v2/pages":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"555","title":"New diagram","version":1}`))
		case r.Method == "POST" && r.URL.Path == "/wiki/rest/api/content/555/child/attachment":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"results":[{"id":"att777","title":"d.drawio.png","version":1}]}`))
		case r.Method == "PUT" && r.URL.Path == "/wiki/api/v2/pages/555":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"555","title":"New diagram","version":2}`))
		default:
			t.Errorf("unexpected call: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	})
	c := testClient(t, srv)

	tmpDir := t.TempDir()
	drawioPath := filepath.Join(tmpDir, "d.drawio")
	if err := os.WriteFile(drawioPath, []byte(`<mxfile/>`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	raw := json.RawMessage(`{"spaceId":"123","title":"New diagram","drawioFile":"` + drawioPath + `","outputFormat":"json"}`)
	if _, err := HandleUploadDrawio(context.Background(), c, raw); err != nil {
		t.Fatalf("HandleUploadDrawio: %v", err)
	}

	// Expected order: POST /pages (create), POST .../attachment (upload),
	// PUT /pages/{id} (body).
	if len(calls) != 3 {
		t.Fatalf("expected 3 calls, got %d: %+v", len(calls), calls)
	}
	if calls[0].path != "/wiki/api/v2/pages" {
		t.Errorf("call 0 = %+v, want POST /wiki/api/v2/pages", calls[0])
	}
	if calls[1].path != "/wiki/rest/api/content/555/child/attachment" {
		t.Errorf("call 1 = %+v, want POST .../attachment", calls[1])
	}
	if calls[2].path != "/wiki/api/v2/pages/555" {
		t.Errorf("call 2 = %+v, want PUT /wiki/api/v2/pages/555", calls[2])
	}
}

// TestHandleUploadDrawio_MacroEnvelope asserts the PUT body
// contains the correct macro XHTML with the diagramName.
func TestHandleUploadDrawio_MacroEnvelope(t *testing.T) {
	srv, rec := newTestClient(t)
	rec.setResponse(http.StatusOK, `{"results":[{"id":"att1","title":"d.drawio.png","version":1}]}`)

	// Override to also respond to GET + PUT calls the handler
	// makes after the upload. Use a wrapping handler that
	// captures the PUT body.
	var putBody []byte
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "POST" && r.URL.Path == "/wiki/rest/api/content/1/child/attachment":
			_, _ = w.Write([]byte(`{"results":[{"id":"att1","title":"d.drawio.png","version":1}]}`))
		case r.Method == "GET":
			_, _ = w.Write([]byte(`{"id":"1","title":"P","version":1}`))
		case r.Method == "PUT":
			buf := make([]byte, 0, 1024)
			tmp := make([]byte, 1024)
			for {
				n, err := r.Body.Read(tmp)
				if n > 0 {
					buf = append(buf, tmp[:n]...)
				}
				if err != nil {
					break
				}
			}
			putBody = buf
			_, _ = w.Write([]byte(`{"id":"1","title":"P","version":2}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	})
	c := testClient(t, srv)

	tmpDir := t.TempDir()
	drawioPath := filepath.Join(tmpDir, "d.drawio")
	if err := os.WriteFile(drawioPath, []byte(`<mxfile/>`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	raw := json.RawMessage(`{"pageId":"1","drawioFile":"` + drawioPath + `","diagramDisplayName":"myDiagram","width":800,"height":600,"outputFormat":"json"}`)
	if _, err := HandleUploadDrawio(context.Background(), c, raw); err != nil {
		t.Fatalf("HandleUploadDrawio: %v", err)
	}

	var body map[string]any
	if err := json.Unmarshal(putBody, &body); err != nil {
		t.Fatalf("PUT body not JSON: %v\nbody=%s", err, putBody)
	}
	bodyObj, ok := body["body"].(map[string]any)
	if !ok {
		t.Fatalf("body.body not an object: %T", body["body"])
	}
	if bodyObj["representation"] != "storage" {
		t.Errorf("body.representation = %v, want 'storage'", bodyObj["representation"])
	}
	storageValue, ok := bodyObj["value"].(string)
	if !ok {
		t.Fatalf("body.value not a string: %T", bodyObj["value"])
	}
	if !strings.Contains(storageValue, `ac:name="drawio"`) {
		t.Errorf("storage value missing drawio macro: %s", storageValue)
	}
	if !strings.Contains(storageValue, `ac:name="diagramName">myDiagram`) {
		t.Errorf("storage value missing diagramName param: %s", storageValue)
	}
	if !strings.Contains(storageValue, `ac:name="width">800`) {
		t.Errorf("storage value missing width=800: %s", storageValue)
	}
	if !strings.Contains(storageValue, `ac:name="height">600`) {
		t.Errorf("storage value missing height=600: %s", storageValue)
	}
	if !strings.Contains(storageValue, `ac:macro-id="drawio-mcp-confluence"`) {
		t.Errorf("storage value missing stable macro-id: %s", storageValue)
	}
}

// TestHandleUploadDrawio_SvgPassthrough asserts that when the
// caller provides drawioSvgFile, the bytes are uploaded as-is
// (no PNG wrapping).
func TestHandleUploadDrawio_SvgPassthrough(t *testing.T) {
	type call struct {
		method, path string
		// capture the multipart filename so we can confirm
		// the wire attachment is named .drawio.svg
		filename string
	}
	var calls []call
	srv, _ := newTestClient(t)
	srv.Config.Handler = func() http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// multipart filename extraction
			fname := ""
			if r.Method == "POST" && strings.Contains(r.URL.Path, "attachment") {
				// We don't have access to the multipart
				// parser easily here — but we can read
				// the "file" form field's filename
				// header via the Content-Disposition
				// header in the multipart body.
				// Reading the whole body to find it.
				buf := make([]byte, 0, 16*1024)
				tmp := make([]byte, 4096)
				for {
					n, err := r.Body.Read(tmp)
					if n > 0 {
						buf = append(buf, tmp[:n]...)
					}
					if err != nil {
						break
					}
				}
				// Find filename="<name>" in the body.
				idx := strings.Index(string(buf), `filename="`)
				if idx >= 0 {
					start := idx + len(`filename="`)
					end := strings.Index(string(buf[start:]), `"`)
					if end >= 0 {
						fname = string(buf[start : start+end])
					}
				}
			}
			calls = append(calls, call{r.Method, r.URL.Path, fname})
			w.Header().Set("Content-Type", "application/json")
			switch {
			case r.Method == "POST" && r.URL.Path == "/wiki/rest/api/content/9/child/attachment":
				_, _ = w.Write([]byte(`{"results":[{"id":"att888","title":"d.drawio.svg","version":1}]}`))
			case r.Method == "GET":
				_, _ = w.Write([]byte(`{"id":"9","title":"P","version":1}`))
			case r.Method == "PUT":
				_, _ = w.Write([]byte(`{"id":"9","title":"P","version":2}`))
			default:
				_, _ = w.Write([]byte(`{}`))
			}
		})
	}()

	c := testClient(t, srv)

	tmpDir := t.TempDir()
	svgPath := filepath.Join(tmpDir, "diagram.drawio.svg")
	svgBody := []byte(`<svg xmlns="http://www.w3.org/2000/svg" content="https%3A%2F%2Fexample.com%2F"><rect/></svg>`)
	if err := os.WriteFile(svgPath, svgBody, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	raw := json.RawMessage(`{"pageId":"9","drawioSvgFile":"` + svgPath + `","outputFormat":"json"}`)
	out, err := HandleUploadDrawio(context.Background(), c, raw)
	if err != nil {
		t.Fatalf("HandleUploadDrawio: %v", err)
	}
	_ = out

	// Find the upload call.
	var upload *call
	for i := range calls {
		if calls[i].method == "POST" && strings.Contains(calls[i].path, "attachment") {
			upload = &calls[i]
			break
		}
	}
	if upload == nil {
		t.Fatalf("no upload call recorded: %+v", calls)
	}
	if !strings.HasSuffix(upload.filename, ".drawio.svg") {
		t.Errorf("upload filename = %q, want suffix .drawio.svg", upload.filename)
	}
}

// errAsAPIErrorDrawio is a tiny helper, like errAsAPIError in
// attachments_handlers_test.go. Kept private to this file.
func errAsAPIErrorDrawio(err error, target **atlassian.APIError) bool {
	if err == nil || target == nil {
		return false
	}
	ae, ok := err.(*atlassian.APIError)
	if !ok {
		return false
	}
	*target = ae
	return true
}

// compile-time sanity: the helper satisfies its signature.
var _ = errAsAPIErrorDrawio
