// attachments_handlers_test.go — handler tests for the v3
// attachment tools (HandleUploadAttachment, HandleListAttachments,
// HandleDeleteAttachment).
//
// The tests use the existing newTestClient / requestRecorder harness
// from execute_test.go. The key behaviors we assert:
//
//   - HandleUploadAttachment sends POST to the v1
//     /wiki/rest/api/content/{pageId}/child/attachment endpoint
//     with multipart/form-data body AND the X-Atlassian-Token:
//     no-check CSRF-bypass header. The file's bytes are in the
//     multipart body (we parse the multipart to confirm).
//   - HandleListAttachments sends GET to the v2
//     /wiki/api/v2/pages/{pageId}/attachments endpoint with
//     optional query params (cursor, limit, mediaType, filename).
//   - HandleDeleteAttachment sends DELETE to the v2
//     /wiki/api/v2/attachments/{id} endpoint, adding
//     ?purge=true when the args request it.
//
// We also assert the error path: HandleUploadAttachment returns
// *atlassian.APIError on 4xx/5xx (matching the rest of the
// server's contract), and HandleList/HandleDelete reject empty
// IDs at the args-decode step.
package tools

import (
	"context"
	"encoding/json"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bennie/mcp-confluence/internal/atlassian"
)

// TestHandleUploadAttachment_SendsMultipartWithNoCheckHeader is the
// critical-path test: a successful upload must send the
// X-Atlassian-Token: no-check header AND a valid multipart body
// containing the file's bytes. Without the header Confluence
// returns 403; without a valid multipart body Confluence returns
// 400.
func TestHandleUploadAttachment_SendsMultipartWithNoCheckHeader(t *testing.T) {
	srv, rec := newTestClient(t)
	rec.setResponse(http.StatusOK, `{"results":[{"id":"att99","title":"hello.txt","extensions":{"fileSize":12}}]}`)

	c := testClient(t, srv)

	// Write a small file to a temp dir so the handler can open it.
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "hello.txt")
	if err := os.WriteFile(filePath, []byte("hello world\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	raw := json.RawMessage(`{"pageId":"163935","filePath":"` + filePath + `","comment":"smoke","outputFormat":"json"}`)
	got, err := HandleUploadAttachment(context.Background(), c, raw)
	if err != nil {
		t.Fatalf("HandleUploadAttachment: %v", err)
	}
	if got == "" {
		t.Fatal("HandleUploadAttachment returned empty string")
	}

	// (1) METHOD + PATH — the v1 endpoint.
	if rec.method != "POST" {
		t.Errorf("method = %q, want POST", rec.method)
	}
	if rec.path != "/wiki/rest/api/content/163935/child/attachment" {
		t.Errorf("path = %q, want /wiki/rest/api/content/163935/child/attachment", rec.path)
	}

	// (2) CSRF-BYPASS HEADER — without this, Confluence returns 403.
	if got := rec.headers.Get("X-Atlassian-Token"); got != "no-check" {
		t.Errorf("X-Atlassian-Token = %q, want 'no-check'", got)
	}

	// (3) MULTIPART BODY — the Content-Type must be multipart/form-data
	// with a boundary parameter. Parse it to confirm at least one
	// part is "file" and contains our file's bytes.
	ct := rec.headers.Get("Content-Type")
	if !strings.HasPrefix(ct, "multipart/form-data") {
		t.Fatalf("Content-Type = %q, want multipart/form-data...", ct)
	}
	mediaType, params, err := mime.ParseMediaType(ct)
	if err != nil {
		t.Fatalf("ParseMediaType: %v", err)
	}
	if mediaType != "multipart/form-data" {
		t.Errorf("media type = %q, want multipart/form-data", mediaType)
	}
	boundary, ok := params["boundary"]
	if !ok || boundary == "" {
		t.Fatalf("Content-Type missing boundary parameter: %q", ct)
	}
	mr := multipart.NewReader(strings.NewReader(string(rec.body)), boundary)
	var foundFile bool
	for {
		part, perr := mr.NextPart()
		if perr != nil {
			break
		}
		if part.FormName() == "file" {
			foundFile = true
			buf := make([]byte, 1024)
			n, _ := part.Read(buf)
			if string(buf[:n]) != "hello world\n" {
				t.Errorf("file part content = %q, want %q", string(buf[:n]), "hello world\n")
			}
		}
		if part.FormName() == "comment" {
			buf := make([]byte, 256)
			n, _ := part.Read(buf)
			if string(buf[:n]) != "smoke" {
				t.Errorf("comment part = %q, want 'smoke'", string(buf[:n]))
			}
		}
		if part.FormName() == "minorEdit" {
			buf := make([]byte, 16)
			n, _ := part.Read(buf)
			// Go zero value is false. The user can opt in by
			// passing "minorEdit": true in the args. The wire
			// field is always present so Confluence has a
			// deterministic value to act on.
			if string(buf[:n]) != "false" {
				t.Errorf("minorEdit = %q, want 'false' (Go zero value)", string(buf[:n]))
			}
		}
	}
	if !foundFile {
		t.Error("multipart body missing 'file' part")
	}
}

// TestHandleUploadAttachment_RequiresFilePath asserts the args
// validation. Empty pageId / filePath must fail with a clear
// error before any HTTP call.
func TestHandleUploadAttachment_RequiresFilePath(t *testing.T) {
	srv, _ := newTestClient(t)
	c := testClient(t, srv)

	cases := []struct {
		name string
		raw  string
	}{
		{"empty pageId", `{"pageId":"","filePath":"/tmp/x"}`},
		{"empty filePath", `{"pageId":"163935","filePath":""}`},
		{"missing filePath", `{"pageId":"163935"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := HandleUploadAttachment(context.Background(), c, json.RawMessage(tc.raw))
			if err == nil {
				t.Errorf("expected error for %s, got nil", tc.name)
			}
			if !strings.Contains(err.Error(), "conf_upload_attachment") {
				t.Errorf("error %q does not name the tool", err.Error())
			}
		})
	}
}

// TestHandleUploadAttachment_MissingFile asserts the file-not-found
// path: the handler must error cleanly, not panic or hang.
func TestHandleUploadAttachment_MissingFile(t *testing.T) {
	srv, _ := newTestClient(t)
	c := testClient(t, srv)

	raw := json.RawMessage(`{"pageId":"163935","filePath":"/nonexistent/path/to/file.png"}`)
	_, err := HandleUploadAttachment(context.Background(), c, raw)
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	if !strings.Contains(err.Error(), "open") {
		t.Errorf("error %q does not mention file open failure", err.Error())
	}
}

// TestHandleUploadAttachment_EmptyFile rejects zero-byte files
// (the multipart upload with an empty payload is ambiguous).
func TestHandleUploadAttachment_EmptyFile(t *testing.T) {
	srv, _ := newTestClient(t)
	c := testClient(t, srv)

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "empty.txt")
	if err := os.WriteFile(filePath, []byte{}, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	raw := json.RawMessage(`{"pageId":"163935","filePath":"` + filePath + `"}`)
	_, err := HandleUploadAttachment(context.Background(), c, raw)
	if err == nil {
		t.Fatal("expected error for empty file, got nil")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error %q does not mention empty file", err.Error())
	}
}

// TestHandleUploadAttachment_APIError verifies the 4xx/5xx path
// returns *atlassian.APIError (so callers can errors.As to read
// the structured fields).
func TestHandleUploadAttachment_APIError(t *testing.T) {
	srv, rec := newTestClient(t)
	rec.setResponse(http.StatusForbidden, `{"code":"NO_PERMISSION","message":"forbidden"}`)

	c := testClient(t, srv)
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "x.txt")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	raw := json.RawMessage(`{"pageId":"163935","filePath":"` + filePath + `"}`)
	_, err := HandleUploadAttachment(context.Background(), c, raw)
	if err == nil {
		t.Fatal("expected error for 403, got nil")
	}
	var apiErr *atlassian.APIError
	if !errAsAPIError(err, &apiErr) {
		t.Errorf("error is not *atlassian.APIError: %T (%v)", err, err)
	}
}

// errAsAPIError wraps errors.As for the test. Pulled into a helper
// so the test file does not need to import "errors" directly.
func errAsAPIError(err error, target **atlassian.APIError) bool {
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

// TestHandleListAttachments_BuildsPath asserts the v2 GET endpoint
// is built correctly with the pageId.
func TestHandleListAttachments_BuildsPath(t *testing.T) {
	srv, rec := newTestClient(t)
	rec.setResponse(http.StatusOK, `{"results":[],"_links":{}}`)

	c := testClient(t, srv)
	raw := json.RawMessage(`{"pageId":"163935"}`)

	if _, err := HandleListAttachments(context.Background(), c, raw); err != nil {
		t.Fatalf("HandleListAttachments: %v", err)
	}
	if rec.method != "GET" {
		t.Errorf("method = %q, want GET", rec.method)
	}
	if rec.path != "/wiki/api/v2/pages/163935/attachments" {
		t.Errorf("path = %q, want /wiki/api/v2/pages/163935/attachments", rec.path)
	}
	if rec.rawQuery != "" {
		t.Errorf("query = %q, want empty", rec.rawQuery)
	}
}

// TestHandleListAttachments_QueryParams asserts the optional
// query params (cursor, limit, mediaType, filename) are passed
// through correctly.
func TestHandleListAttachments_QueryParams(t *testing.T) {
	srv, rec := newTestClient(t)
	rec.setResponse(http.StatusOK, `{"results":[]}`)

	c := testClient(t, srv)
	raw := json.RawMessage(`{
		"pageId":"163935",
		"limit":50,
		"mediaType":"image",
		"filename":"diagram.drawio"
	}`)
	if _, err := HandleListAttachments(context.Background(), c, raw); err != nil {
		t.Fatalf("HandleListAttachments: %v", err)
	}
	for _, want := range []string{"limit=50", "mediaType=image", "filename=diagram.drawio"} {
		if !strings.Contains(rec.rawQuery, want) {
			t.Errorf("query missing %q; got: %q", want, rec.rawQuery)
		}
	}
	// cursor was unset, so it should NOT be in the query.
	if strings.Contains(rec.rawQuery, "cursor=") {
		t.Errorf("query unexpectedly contains cursor=: %q", rec.rawQuery)
	}
}

// TestHandleListAttachments_RequiresPageId asserts the args
// validation: empty pageId must error before any HTTP call.
func TestHandleListAttachments_RequiresPageId(t *testing.T) {
	srv, _ := newTestClient(t)
	c := testClient(t, srv)

	_, err := HandleListAttachments(context.Background(), c, json.RawMessage(`{"pageId":""}`))
	if err == nil {
		t.Fatal("expected error for empty pageId, got nil")
	}
	if !strings.Contains(err.Error(), "pageId is required") {
		t.Errorf("error %q does not name the missing field", err.Error())
	}
}

// TestHandleDeleteAttachment_BuildsPath asserts the v2 DELETE
// endpoint is built correctly.
func TestHandleDeleteAttachment_BuildsPath(t *testing.T) {
	srv, rec := newTestClient(t)
	rec.setResponse(http.StatusNoContent, ``)

	c := testClient(t, srv)
	raw := json.RawMessage(`{"attachmentId":"att219152391"}`)
	if _, err := HandleDeleteAttachment(context.Background(), c, raw); err != nil {
		t.Fatalf("HandleDeleteAttachment: %v", err)
	}
	if rec.method != "DELETE" {
		t.Errorf("method = %q, want DELETE", rec.method)
	}
	if rec.path != "/wiki/api/v2/attachments/att219152391" {
		t.Errorf("path = %q, want /wiki/api/v2/attachments/att219152391", rec.path)
	}
	if rec.rawQuery != "" {
		t.Errorf("query = %q, want empty (purge not set)", rec.rawQuery)
	}
}

// TestHandleDeleteAttachment_Purge asserts that purge=true adds
// ?purge=true to the query string.
func TestHandleDeleteAttachment_Purge(t *testing.T) {
	srv, rec := newTestClient(t)
	rec.setResponse(http.StatusNoContent, ``)

	c := testClient(t, srv)
	raw := json.RawMessage(`{"attachmentId":"att99","purge":true}`)
	if _, err := HandleDeleteAttachment(context.Background(), c, raw); err != nil {
		t.Fatalf("HandleDeleteAttachment: %v", err)
	}
	if rec.rawQuery != "purge=true" {
		t.Errorf("query = %q, want 'purge=true'", rec.rawQuery)
	}
}

// TestHandleDeleteAttachment_RequiresID asserts the args
// validation: empty attachmentId must error before any HTTP call.
func TestHandleDeleteAttachment_RequiresID(t *testing.T) {
	srv, _ := newTestClient(t)
	c := testClient(t, srv)

	_, err := HandleDeleteAttachment(context.Background(), c, json.RawMessage(`{"attachmentId":""}`))
	if err == nil {
		t.Fatal("expected error for empty attachmentId, got nil")
	}
	if !strings.Contains(err.Error(), "attachmentId is required") {
		t.Errorf("error %q does not name the missing field", err.Error())
	}
}

// TestAttachmentArgs_SatisfyReqArgs is the type-system test:
// the two v2-based args types must satisfy the reqArgs
// discriminator interface (HandleUploadAttachment bypasses
// executeRequest, so UploadAttachmentArgs intentionally does NOT
// satisfy reqArgs — it's the upload-only path).
func TestAttachmentArgs_SatisfyReqArgs(t *testing.T) {
	var _ reqArgs = ListAttachmentsArgs{}
	var _ reqArgs = DeleteAttachmentArgs{}
}
