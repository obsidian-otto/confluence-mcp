// drawio_handler_test.go — handler tests for the v3
// `conf_upload_drawio` tool. The orchestrator's flow is harder
// to test in isolation than the lower-level handlers because it
// makes 2-3 separate HTTP calls (create page if new, upload
// the source attachment, create the drawio custom-content
// entity, then PUT the page body with the inc-drawio macro).
// The page body is NEVER empty on success — the macro is the
// load-bearing trigger that causes the draw.io app to render
// the diagram inline.
package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
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

// TestHandleUploadDrawio_ExistingPage_FullFlow asserts the
// happy-path: an existing page gets a 3-call flow (v1 upload,
// v2 custom-content entity, v2 PUT body with the inc-drawio
// macro). The PUT body must contain the inc-drawio macro
// referencing the source attachment AND the custom-content
// entity's custContentId — this is what triggers the draw.io
// app's diagram detection and rendering.
func TestHandleUploadDrawio_ExistingPage_FullFlow(t *testing.T) {
	type upload struct {
		filename    string
		contentType string
		body        []byte
	}
	var uploads []upload
	var entityBody []byte
	var putBody []byte
	srv, _ := newTestClient(t)
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "POST" && r.URL.Path == "/wiki/rest/api/content/163935/child/attachment":
			reader, err := r.MultipartReader()
			if err != nil {
				t.Fatalf("MultipartReader: %v", err)
			}
			for {
				part, perr := reader.NextPart()
				if perr == io.EOF {
					break
				}
				if perr != nil {
					t.Fatalf("NextPart: %v", perr)
				}
				if part.FormName() != "file" {
					continue
				}
				contents, rerr := io.ReadAll(part)
				if rerr != nil {
					t.Fatalf("read multipart file: %v", rerr)
				}
				uploads = append(uploads, upload{
					filename:    part.FileName(),
					contentType: part.Header.Get("Content-Type"),
					body:        contents,
				})
			}
			_, _ = w.Write([]byte(`{"results":[{"id":"att999","title":"architecture.drawio","version":{"number":1}}]}`))
		case r.Method == "GET" && r.URL.Path == "/wiki/api/v2/pages/163935":
			_, _ = w.Write([]byte(`{"id":"163935","title":"DrawIO Test","version":{"number":3}}`))
		case r.Method == "POST" && r.URL.Path == "/wiki/api/v2/custom-content":
			var err error
			entityBody, err = io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read custom-content body: %v", err)
			}
			_, _ = w.Write([]byte(`{"id":"cc123","title":"architecture.drawio","version":{"number":1}}`))
		case r.Method == "PUT" && r.URL.Path == "/wiki/api/v2/pages/163935":
			var err error
			putBody, err = io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read PUT body: %v", err)
			}
			_, _ = w.Write([]byte(`{"id":"163935","title":"DrawIO Test","version":{"number":3}}`))
		default:
			t.Errorf("unexpected call: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	})
	c := testClient(t, srv)

	xml := []byte(`<mxfile host="test"><diagram id="d1" name="Architecture"><mxGraphModel><root><mxCell id="0"/></root></mxGraphModel></diagram></mxfile>`)
	drawioPath := filepath.Join(t.TempDir(), "architecture.drawio")
	if err := os.WriteFile(drawioPath, xml, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	raw := json.RawMessage(`{"pageId":"163935","drawioFile":"` + drawioPath + `","outputFormat":"json"}`)
	if _, err := HandleUploadDrawio(context.Background(), c, raw); err != nil {
		t.Fatalf("HandleUploadDrawio: %v", err)
	}

	if len(uploads) != 1 {
		t.Fatalf("upload count = %d, want 1", len(uploads))
	}
	got := uploads[0]
	if got.filename != "architecture.drawio" {
		t.Errorf("attachment filename = %q, want architecture.drawio", got.filename)
	}
	if got.contentType != "application/vnd.jgraph.mxfile" {
		t.Errorf("attachment Content-Type = %q, want application/vnd.jgraph.mxfile", got.contentType)
	}
	if string(got.body) != string(xml) {
		t.Error("uploaded drawio source differs from input")
	}

	// Custom-content entity body must match the draw.io app's
	// contract: pageId + type + diagramName + version +
	// inComment=false + comments=[] + isSketch=0, with
	// body.value URL-encoded.
	var request map[string]any
	if err := json.Unmarshal(entityBody, &request); err != nil {
		t.Fatalf("custom-content body not JSON: %v\nbody=%s", err, entityBody)
	}
	if request["type"] != "ac:com.mxgraph.confluence.plugins.diagramly:drawio-diagram" {
		t.Errorf("entity type = %v, want ac:com.mxgraph.confluence.plugins.diagramly:drawio-diagram", request["type"])
	}
	if request["pageId"] != "163935" {
		t.Errorf("entity pageId = %v, want 163935", request["pageId"])
	}
	if request["title"] != "architecture.drawio" {
		t.Errorf("entity title = %v, want architecture.drawio", request["title"])
	}
	body, ok := request["body"].(map[string]any)
	if !ok {
		t.Fatalf("entity body.body not an object: %T", request["body"])
	}
	if body["representation"] != "storage" {
		t.Errorf("entity body.representation = %v, want 'storage'", body["representation"])
	}
	storageValue, ok := body["value"].(string)
	if !ok {
		t.Fatalf("entity body.value not a string: %T", body["value"])
	}
	if !strings.HasPrefix(storageValue, "%7B") {
		t.Errorf("entity body.value is not URL-encoded: %q", storageValue)
	}
	decoded, derr := url.QueryUnescape(storageValue)
	if derr != nil {
		t.Fatalf("URL-decode body.value: %v", derr)
	}
	var inner map[string]any
	if err := json.Unmarshal([]byte(decoded), &inner); err != nil {
		t.Fatalf("URL-decoded body.value not valid JSON: %v\ndecoded=%s", err, decoded)
	}
	if inner["pageId"] != "163935" {
		t.Errorf("drawio body.pageId = %v, want 163935", inner["pageId"])
	}
	if inner["diagramName"] != "architecture.drawio" {
		t.Errorf("drawio body.diagramName = %v, want architecture.drawio", inner["diagramName"])
	}

	// PUT body must contain the inc-drawio macro referencing
	// both the source attachment (by diagramName) and the
	// custom-content entity (by custContentId). This is the
	// trigger that causes the draw.io app to render the
	// diagram inline (verified live 2026-07-13).
	var putRequest map[string]any
	if err := json.Unmarshal(putBody, &putRequest); err != nil {
		t.Fatalf("PUT body not JSON: %v\nbody=%s", err, putBody)
	}
	putBodyObj, ok := putRequest["body"].(map[string]any)
	if !ok {
		t.Fatalf("PUT body.body not an object: %T", putRequest["body"])
	}
	putStorageValue, ok := putBodyObj["value"].(string)
	if !ok {
		t.Fatalf("PUT body.body.value not a string: %T", putBodyObj["value"])
	}
	for _, expected := range []string{
		`ac:name="inc-drawio"`,
		`ac:name="pageId">163935`,
		`ac:name="custContentId">cc123`,
		`ac:name="diagramDisplayName">architecture.drawio`,
		`ac:name="diagramName">architecture.drawio`,
		`ac:name="imgPageId">163935`,
		`ac:name="width">1500`,
		`ac:name="height">990`,
		`ac:name="simple">0`,
		`ac:name="zoom">1`,
		`ac:name="lbox">1`,
		`ac:name="links">auto`,
		`ac:name="tbstyle">top`,
		`ac:name="includedDiagram">1`,
		`ac:macro-id="drawio-mcp-confluence"`,
	} {
		if !strings.Contains(putStorageValue, expected) {
			t.Errorf("PUT body storage value missing %q: %s", expected, putStorageValue)
		}
	}
	// Title round-trip: existing page title must be preserved.
	if putRequest["title"] != "DrawIO Test" {
		t.Errorf("PUT body.title = %v, want 'DrawIO Test' (existing title round-trip)", putRequest["title"])
	}
}

// TestHandleUploadDrawio_NewPage_FullFlow asserts the new-page
// path: create empty page (version=1) → upload → entity
// creation → PUT body with the inc-drawio macro. Three calls
// to /wiki/api/v2 (POST pages, POST custom-content, PUT pages)
// plus the v1 upload.
func TestHandleUploadDrawio_NewPage_FullFlow(t *testing.T) {
	type call struct{ method, path string }
	var calls []call
	srv, _ := newTestClient(t)
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, call{r.Method, r.URL.Path})
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "POST" && r.URL.Path == "/wiki/api/v2/pages":
			_, _ = w.Write([]byte(`{"id":"555","title":"New diagram","version":{"number":1}}`))
		case r.Method == "POST" && r.URL.Path == "/wiki/rest/api/content/555/child/attachment":
			_, _ = w.Write([]byte(`{"results":[{"id":"att777","title":"d.drawio","version":{"number":1}}]}`))
		case r.Method == "POST" && r.URL.Path == "/wiki/api/v2/custom-content":
			_, _ = w.Write([]byte(`{"id":"cc777","title":"d.drawio","version":{"number":1}}`))
		case r.Method == "PUT" && r.URL.Path == "/wiki/api/v2/pages/555":
			_, _ = w.Write([]byte(`{"id":"555","title":"New diagram","version":{"number":2}}`))
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

	if len(calls) != 4 {
		t.Fatalf("expected 4 calls, got %d: %+v", len(calls), calls)
	}
	if calls[0].path != "/wiki/api/v2/pages" {
		t.Errorf("call 0 = %+v, want POST /wiki/api/v2/pages", calls[0])
	}
	if calls[1].path != "/wiki/rest/api/content/555/child/attachment" {
		t.Errorf("call 1 = %+v, want POST .../attachment", calls[1])
	}
	if calls[2].path != "/wiki/api/v2/custom-content" {
		t.Errorf("call 2 = %+v, want POST /wiki/api/v2/custom-content", calls[2])
	}
	if calls[3].path != "/wiki/api/v2/pages/555" {
		t.Errorf("call 3 = %+v, want PUT /wiki/api/v2/pages/555", calls[3])
	}
}

// TestHandleUploadDrawio_SvgPassthrough asserts that when the
// caller provides drawioSvgFile, the bytes are uploaded as-is
// (no PNG wrapping).
func TestHandleUploadDrawio_SvgPassthrough(t *testing.T) {
	type call struct {
		method, path string
		filename     string
	}
	var calls []call
	srv, _ := newTestClient(t)
	srv.Config.Handler = func() http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fname := ""
			if r.Method == "POST" && strings.Contains(r.URL.Path, "attachment") {
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
				_, _ = w.Write([]byte(`{"results":[{"id":"att888","title":"d.drawio.svg","version":{"number":1}}]}`))
			case r.Method == "GET" && r.URL.Path == "/wiki/api/v2/pages/9":
				_, _ = w.Write([]byte(`{"id":"9","title":"P","version":{"number":1}}`))
			case r.Method == "POST" && r.URL.Path == "/wiki/api/v2/custom-content":
				_, _ = w.Write([]byte(`{"id":"cc888","title":"d.drawio.svg","version":{"number":1}}`))
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
	if _, err := HandleUploadDrawio(context.Background(), c, raw); err != nil {
		t.Fatalf("HandleUploadDrawio: %v", err)
	}

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
