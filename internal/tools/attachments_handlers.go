// Package tools — attachments_handlers.go: handlers for the 3
// attachment tools (conf_upload_attachment, conf_list_attachments,
// conf_delete_attachment). Together they bring the server from 13
// to 16 tools.
//
// Wire shape per tool:
//
//   - conf_upload_attachment → v1 POST
//     /wiki/rest/api/content/{pageId}/child/attachment
//     (multipart/form-data + X-Atlassian-Token: no-check). Custom
//     path — does NOT go through the 9-step executeRequest pipeline
//     because the response shape is v1's ContentPageScheme (mediaType
//     lives in .extensions.mediaType, not .mediaType) and the body
//     is multipart, not JSON. The handler uses
//     atlassian.Client.UploadAttachment directly.
//
//   - conf_list_attachments → v2 GET
//     /wiki/api/v2/pages/{id}/attachments. Goes through the
//     executeRequest pipeline (matches ListPages semantics).
//
//   - conf_delete_attachment → v2 DELETE
//     /wiki/api/v2/attachments/{id} (or ?purge=true). Goes through
//     the executeRequest pipeline.
//
// Full rationale + verified research in
// specs/11-attachments/01-research-and-surface.md.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/bennie/mcp-confluence/internal/atlassian"
	"github.com/bennie/mcp-confluence/internal/jmespath"
)

// HandleUploadAttachment is the `conf_upload_attachment` handler.
// It decodes args into UploadAttachmentArgs, delegates the multipart
// upload to atlassian.Client.UploadAttachment (which builds the
// multipart body + sets the X-Atlassian-Token: no-check header),
// then runs the response through the standard JMESPath / TOON /
// truncation pipeline shared with the other tools.
//
// The v1 endpoint returns a ContentPageScheme envelope:
//
//	{ "results": [{ "id": "...", "title": "...", "type":
//	"attachment", "version": { "number": 1 }, ... }], ... }
//
// We re-decode the raw bytes into a map so JMESPath / TOON can work
// uniformly. The 4xx/5xx path is handled inside UploadAttachment
// (returns *APIError, matching the rest of the server's contract).
func HandleUploadAttachment(ctx context.Context, client *atlassian.Client, args json.RawMessage) (string, error) {
	var a UploadAttachmentArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("conf_upload_attachment: decode args: %w", err)
	}
	if a.PageId == "" {
		return "", fmt.Errorf("conf_upload_attachment: pageId is required")
	}
	if a.FilePath == "" {
		return "", fmt.Errorf("conf_upload_attachment: filePath is required")
	}

	respBody, _, err := client.UploadAttachment(ctx, a.PageId, a.FilePath, a.Comment, a.MinorEdit)
	if err != nil {
		// Pass *APIError through unchanged so callers can
		// errors.As(err, &atlassian.APIError{}).
		return "", err
	}

	// Decode the v1 ContentPageScheme into map[string]any so the
	// downstream pipeline (JMESPath / TOON / truncation) can work
	// uniformly with the other tools. An empty body (rare for v1
	// upload — would indicate a server bug) becomes an empty map.
	data := any(map[string]any{})
	if len(respBody) > 0 {
		var decoded map[string]any
		if jerr := json.Unmarshal(respBody, &decoded); jerr != nil {
			return "", fmt.Errorf("conf_upload_attachment: decode response: %w", jerr)
		}
		data = decoded
	}

	// JMESPath filter.
	if a.JQ != "" {
		filtered, ferr := jmespath.Apply(a.JQ, data)
		if ferr != nil {
			return "", fmt.Errorf("POST /wiki/rest/api/content/%s/child/attachment: jq filter error: %v", a.PageId, ferr)
		}
		data = filtered
	}

	// Encode (TOON default; JSON when requested).
	encoded, eerr := encodeOutput(data, a.OutputFormat)
	if eerr != nil {
		return "", fmt.Errorf("POST /wiki/rest/api/content/%s/child/attachment: encode error: %v", a.PageId, eerr)
	}

	final, terr := truncateForAI(encoded, "POST", fmt.Sprintf("/wiki/rest/api/content/%s/child/attachment", a.PageId))
	if terr != nil {
		fmt.Fprintf(os.Stderr,
			"tools: failed to persist full response: %v\n", terr)
	}
	return final, nil
}

// HandleListAttachments is the `conf_list_attachments` handler.
// It decodes args into ListAttachmentsArgs, builds the v2 path
// /wiki/api/v2/pages/{pageId}/attachments with optional query
// params (cursor, limit, mediaType, filename), then forwards
// through the standard 9-step pipeline.
//
// ListAttachmentsArgs satisfies the reqArgs interface so it can go
// through executeRequest unchanged.
func HandleListAttachments(ctx context.Context, client *atlassian.Client, args json.RawMessage) (string, error) {
	var a ListAttachmentsArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("conf_list_attachments: decode args: %w", err)
	}
	if a.PageId == "" {
		return "", fmt.Errorf("conf_list_attachments: pageId is required")
	}
	return executeRequest(ctx, client, a, "GET", nil)
}

// HandleDeleteAttachment is the `conf_delete_attachment` handler.
// It decodes args into DeleteAttachmentArgs, builds the v2 path
// /wiki/api/v2/attachments/{attachmentId} with an optional
// ?purge=true query, then forwards through the standard 9-step
// pipeline.
//
// DeleteAttachmentArgs satisfies the reqArgs interface.
func HandleDeleteAttachment(ctx context.Context, client *atlassian.Client, args json.RawMessage) (string, error) {
	var a DeleteAttachmentArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("conf_delete_attachment: decode args: %w", err)
	}
	if a.AttachmentId == "" {
		return "", fmt.Errorf("conf_delete_attachment: attachmentId is required")
	}
	return executeRequest(ctx, client, a, "DELETE", nil)
}

// GetPath / GetQuery / GetJQ / GetOutputFormat — interface adapters
// for the 3 new args types. Matches the existing pattern in
// execute.go.
//
// Note: HandleUploadAttachment bypasses executeRequest, so
// UploadAttachmentArgs does NOT need to satisfy reqArgs. Only the
// two v2-based tools do.

// ListAttachmentsArgs satisfies reqArgs.
func (a ListAttachmentsArgs) GetPath() string {
	return fmt.Sprintf("/wiki/api/v2/pages/%s/attachments", a.PageId)
}
func (a ListAttachmentsArgs) GetQuery() map[string]string {
	q := map[string]string{}
	if a.Cursor != "" {
		q["cursor"] = a.Cursor
	}
	if a.Limit > 0 {
		q["limit"] = fmt.Sprintf("%d", a.Limit)
	}
	if a.MediaType != "" {
		q["mediaType"] = a.MediaType
	}
	if a.Filename != "" {
		q["filename"] = a.Filename
	}
	if len(q) == 0 {
		return nil
	}
	return q
}
func (a ListAttachmentsArgs) GetJQ() string           { return a.JQ }
func (a ListAttachmentsArgs) GetOutputFormat() string { return a.OutputFormat }

// DeleteAttachmentArgs satisfies reqArgs. When Purge is true, the
// query map gets ?purge=true — Confluence uses this to permanently
// delete instead of moving to trash.
func (a DeleteAttachmentArgs) GetPath() string {
	return fmt.Sprintf("/wiki/api/v2/attachments/%s", a.AttachmentId)
}
func (a DeleteAttachmentArgs) GetQuery() map[string]string {
	if a.Purge {
		return map[string]string{"purge": "true"}
	}
	return nil
}
func (a DeleteAttachmentArgs) GetJQ() string           { return a.JQ }
func (a DeleteAttachmentArgs) GetOutputFormat() string { return a.OutputFormat }

// Compile-time assertions: each new args type that should go through
// the 9-step pipeline must satisfy reqArgs. Matches the assertion
// block at the top of execute.go.
var (
	_ reqArgs = ListAttachmentsArgs{}
	_ reqArgs = DeleteAttachmentArgs{}
)
