// cmd/mcp-confluence/cli_tool_attachments.go
//
// Phase 21 — per-tool CLI subcommands for the 3 attachment
// handlers (upload_attachment, list_attachments,
// delete_attachment). The wire path is asymmetric:
//
//   - upload  → v1 REST (POST /wiki/rest/api/content/{pageId}/
//     child/attachment with multipart/form-data +
//     X-Atlassian-Token: no-check). Confluence Cloud
//     has no v2 upload endpoint as of 2026-07-10.
//   - list    → v2 REST (GET /wiki/api/v2/pages/{id}/attachments)
//   - delete  → v2 REST (DELETE /wiki/api/v2/attachments/{id})
//
// The upload flag is the special case: --filePath takes an
// absolute path on disk; the handler opens it with os.Open and
// streams via io.Copy — files do NOT load into memory beyond
// the multipart body buffer. 100 MB is the Atlassian Cloud
// hard cap; calls over the cap return 413 Payload Too Large.
//
// Pattern matches cli_tool_crud.go exactly. Same RunE body
// (runToolInvocation); per-command variation is the args struct
// type and the Handle* function.
package main

import (
	"github.com/spf13/cobra"

	internal "github.com/bennie/mcp-confluence/internal/tools"
)

// newUploadAttachmentCmd returns the `upload_attachment`
// subcommand. It maps 1:1 to internal.HandleUploadAttachment —
// uploads a single binary file from disk as an attachment to a
// Confluence page.
func newUploadAttachmentCmd() *cobra.Command {
	args := &internal.UploadAttachmentArgs{}
	cmd := &cobra.Command{
		Use:   "upload_attachment",
		Short: "Upload a binary file as an attachment to a Confluence page (v1 REST)",
		Long: `upload_attachment uploads a single binary file from disk as
an attachment to a Confluence page (TOON-encoded response, by
default). The wire path is the v1 REST API — Confluence Cloud has
no v2 upload endpoint. PNG, PDF, drawio XML, JPEG, SVG, DOCX,
XLSX, MP4, ZIP all work; the file is uploaded as-is, no base64
inflation.

The handler streams the file via io.Copy — files do NOT load into
memory beyond the multipart body buffer. 100 MB is the Atlassian
Cloud hard cap; calls over the cap return 413 Payload Too Large.

USAGE:
  mcp-confluence upload_attachment [flags]

FLAGS (auto-generated from internal/tools.UploadAttachmentArgs):
      --pageId string       Numeric page id where the attachment will live (required, e.g. '163935').
      --filePath string     Absolute path to the file on disk (required, e.g. '/home/user/diagram.drawio').
      --comment string      Optional changelog / version comment.
      --minorEdit bool      Mark the new attachment version as a minor edit (default false).
      --jq string           Optional JMESPath filter against the v1 ContentPageScheme envelope.
      --outputFormat string  '' or 'toon' (default) | 'json'.

EXAMPLES:
  # Upload a single file to a page:
  mcp-confluence upload_attachment --pageId=163935 --filePath=/tmp/diagram.drawio

  # Upload with a changelog comment:
  mcp-confluence upload_attachment --pageId=163935 --filePath=/tmp/diagram.png \
      --comment='Updated 2026-07-14 — refresh stale screenshot'

  # Get just the new attachment id via jq:
  mcp-confluence upload_attachment --pageId=163935 --filePath=/tmp/diagram.png \
      --jq='results[0].{id: id, title: title, mediaType: mediaType}'

  # Bulk upload from a Makefile (per-file invocation — parallel
  # uploads are safe; the server does not rate-limit per-page):
  #
  #   upload-attachments:
  #       for f in $$(ls $$SOURCE_DIR); do \
  #           mcp-confluence upload_attachment --pageId=$$PAGE_ID \
  #               --filePath=$$SOURCE_DIR/$$f --comment='bulk upload'; \
  #       done

HERMES REGISTRATION:
  # Not an MCP-host registration — per-tool subcommands are
  # the shell-script dispatch surface, not themselves MCP
  # tools. The drawio-specific flow (upload + embed) is in
  # upload_drawio (cli_tool_drawio.go) — this subcommand
  # is the generic binary-upload path.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runToolInvocation(cmd, nil, internal.HandleUploadAttachment, args)
		},
	}
	if err := registerFlagsFromArgsStruct(cmd, args); err != nil {
		panic("upload_attachment: registerFlagsFromArgsStruct: " + err.Error())
	}
	return cmd
}

// newListAttachmentsCmd returns the `list_attachments`
// subcommand. It maps 1:1 to internal.HandleListAttachments —
// lists the attachments on a page via the v2 REST endpoint.
func newListAttachmentsCmd() *cobra.Command {
	args := &internal.ListAttachmentsArgs{}
	cmd := &cobra.Command{
		Use:   "list_attachments",
		Short: "List attachments on a Confluence page (v2 REST)",
		Long: `list_attachments lists attachments on a single page
(TOON-encoded, by default). The wire path is the v2 REST API
(GET /wiki/api/v2/pages/{id}/attachments), which is cursor-
paginated and capped at 100 per page.

USAGE:
  mcp-confluence list_attachments [flags]

FLAGS (auto-generated from internal/tools.ListAttachmentsArgs):
      --pageId string       Numeric page id whose attachments should be listed (required, e.g. '163935').
      --cursor string       Opaque pagination cursor from a previous call.
      --limit int           Maximum attachments to return (default 25; max 100 — the v2 endpoint cap).
      --mediaType string    Substring filter on the attachment's mediaType (e.g. 'image' to match image/png and image/jpeg).
      --filename string     Exact filename filter (case-sensitive).
      --jq string           Optional JMESPath filter against the v2 MultiEntityResult<Attachment> envelope.
      --outputFormat string  '' or 'toon' (default) | 'json'.

EXAMPLES:
  # List all attachments on a page:
  mcp-confluence list_attachments --pageId=163935

  # Just the image attachments (mediaType substring):
  mcp-confluence list_attachments --pageId=163935 --mediaType=image

  # Find a specific filename:
  mcp-confluence list_attachments --pageId=163935 --filename=diagram.png

  # Flatten to {id, title, mediaType, fileSize} via jq:
  mcp-confluence list_attachments --pageId=163935 \
      --jq='results[*].{id: id, title: title, mediaType: mediaType, fileSize: fileSize}'

HERMES REGISTRATION:
  # Use from a Makefile for "audit all images on page X":
  #
  #   audit-page-images:
  #       mcp-confluence list_attachments --pageId=$$PAGE_ID --mediaType=image \
  #           --jq='results[*].{title: title, fileSize: fileSize}'
  #
  # The id from this listing is the input to delete_attachment.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runToolInvocation(cmd, nil, internal.HandleListAttachments, args)
		},
	}
	if err := registerFlagsFromArgsStruct(cmd, args); err != nil {
		panic("list_attachments: registerFlagsFromArgsStruct: " + err.Error())
	}
	return cmd
}

// newDeleteAttachmentCmd returns the `delete_attachment`
// subcommand. It maps 1:1 to internal.HandleDeleteAttachment —
// deletes an attachment by id (or purges it permanently).
func newDeleteAttachmentCmd() *cobra.Command {
	args := &internal.DeleteAttachmentArgs{}
	cmd := &cobra.Command{
		Use:   "delete_attachment",
		Short: "Delete an attachment by id (v2 REST; default moves to trash)",
		Long: `delete_attachment deletes an attachment by its numeric id
(TOON-encoded, by default). The wire path is the v2 REST API
(DELETE /wiki/api/v2/attachments/{id}). By default the
attachment is moved to trash; pass --purge=true to permanently
delete (irreversible).

Most successful deletes return 204 No Content with an empty
body, so the TOON-encoded stdout is typically empty. Pass
--outputFormat=json to surface the envelope verbatim.

USAGE:
  mcp-confluence delete_attachment [flags]

FLAGS (auto-generated from internal/tools.DeleteAttachmentArgs):
      --attachmentId string  Numeric attachment id (required). Get from list_attachments.
      --purge bool            Set true to permanently delete (purge) instead of moving to trash (default false; irreversible).
      --jq string             Optional JMESPath filter — most DELETE responses are 204 No Content.
      --outputFormat string   '' or 'toon' (default) | 'json'.

EXAMPLES:
  # Move an attachment to trash (default; recoverable):
  mcp-confluence delete_attachment --attachmentId=456

  # Permanently delete (irreversible):
  mcp-confluence delete_attachment --attachmentId=456 --purge=true

  # Find-then-delete from a Makefile (one-liner; the Make
  # caller must guard against the empty-attachment-id case):
  #
  #   cleanup-attachment:
  #       ID=$$(mcp-confluence list_attachments --pageId=$$PAGE_ID \
  #               --filename=$$NAME --jq='results[0].id')
  #       [ -n "$$ID" ] && mcp-confluence delete_attachment --attachmentId=$$ID

HERMES REGISTRATION:
  # To re-upload the same filename (new version, not separate
  # attachment), use upload_attachment with the same
  # --pageId — Confluence treats re-uploading the same filename
  # as a new version, not a separate attachment.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runToolInvocation(cmd, nil, internal.HandleDeleteAttachment, args)
		},
	}
	if err := registerFlagsFromArgsStruct(cmd, args); err != nil {
		panic("delete_attachment: registerFlagsFromArgsStruct: " + err.Error())
	}
	return cmd
}
