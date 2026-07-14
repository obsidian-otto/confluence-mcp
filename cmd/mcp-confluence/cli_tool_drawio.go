// cmd/mcp-confluence/cli_tool_drawio.go
//
// Phase 21 — per-tool CLI subcommand for the drawio handler
// (upload_drawio). The most complex of the 18 subcommands
// because the args struct carries two mutually-exclusive axes:
//
//   - 3 INPUT modes (exactly one of):
//     --drawioFile     standalone .drawio XML
//     --drawioPngFile  .drawio.png with embedded XML (tEXt chunk, "mxfile")
//     --drawioSvgFile  .drawio.svg with embedded XML (root <svg content="...">)
//     Precedence when multiple are set: drawioFile > drawioPngFile > drawioSvgFile.
//
//   - 2 TARGET modes (exactly one of):
//     --pageId         existing page; tool adds the drawio macro to its body
//     --spaceId + --title   create a new page; tool creates it with the macro
//
// The wire flow (up to 4 REST calls, mirrors a native owning
// diagram in the draw.io Confluence app):
//
//  1. Create the page when --spaceId + --title are supplied.
//  2. Upload the source through the v1 attachment endpoint
//     with an extension-derived Content-Type
//     (.drawio → application/vnd.jgraph.mxfile).
//  3. Create the ac:com.mxgraph.confluence.plugins.diagramly:
//     drawio-diagram custom-content entity with URL-encoded
//     metadata JSON.
//  4. PUT an owning <ac:structured-macro ac:name="drawio"> on
//     the same page (with fresh UUIDs and the app metadata:
//     mVer, inComment, pageId, custContentId, diagramDisplayName,
//     diagramName, contentVer, revision, baseUrl).
//
// Pattern matches cli_tool_crud.go exactly. Same RunE body
// (runToolInvocation). The Long help below documents the modes
// in detail because they are load-bearing — passing the wrong
// combination (e.g. only --pageId on a brand-new page) returns
// a server error, not a "you forgot a flag" hint.
package main

import (
	"github.com/spf13/cobra"

	internal "github.com/bennie/mcp-confluence/internal/tools"
)

// newUploadDrawioCmd returns the `upload_drawio`
// subcommand. It maps 1:1 to internal.HandleUploadDrawio —
// uploads a drawio file (XML, or PNG/SVG with embedded XML)
// AND embeds it on the page in one call.
func newUploadDrawioCmd() *cobra.Command {
	args := &internal.UploadDrawioArgs{}
	cmd := &cobra.Command{
		Use:   "upload_drawio",
		Short: "Upload a drawio diagram and embed it on a Confluence page in one call",
		Long: `upload_drawio uploads a drawio diagram (XML, or PNG/SVG with
embedded drawio XML) and embeds it on a Confluence page in one
call (TOON-encoded response envelope, by default). The wire flow
fans out up to 4 REST calls — create page (if needed), upload
attachment, create custom-content entity, PUT the owning drawio
macro on the page body. This mirrors what a native draw.io
Confluence app "create new diagram" does.

INPUT MODES (exactly one of):
      --drawioFile     Path to a standalone .drawio XML file on disk.
                       Uploaded verbatim as application/vnd.jgraph.mxfile.
      --drawioPngFile  Path to an already-prepared .drawio.png on disk
                       (PNG with drawio XML embedded in a tEXt chunk,
                       keyword 'mxfile'). Uploaded verbatim.
      --drawioSvgFile  Path to a .drawio.svg on disk (SVG with drawio
                       XML embedded in the root <svg> 'content' attr).
                       Uploaded verbatim.
  Precedence when multiple are set: drawioFile > drawioPngFile > drawioSvgFile.

TARGET MODES (exactly one of):
      --pageId         Numeric page id of an EXISTING page. Tool adds
                       the <ac:structured-macro ac:name="drawio">
                       block to the existing body.
      --spaceId + --title   Numeric space id + new-page title. Tool
                            creates the page AND embeds the macro on it.

USAGE:
  mcp-confluence upload_drawio [flags]

FLAGS (auto-generated from internal/tools.UploadDrawioArgs):
      --pageId string          Numeric page id of an EXISTING page (mutually exclusive with --spaceId).
      --spaceId string         Numeric space id for creating a new page (mutually exclusive with --pageId).
      --title string           Title for the new page (required when --spaceId is set; ignored when --pageId is set).
      --drawioFile string      Path to a .drawio XML file (mutually exclusive with --drawioPngFile and --drawioSvgFile).
      --drawioPngFile string   Path to a .drawio.png (mutually exclusive with --drawioFile and --drawioSvgFile).
      --drawioSvgFile string   Path to a .drawio.svg (mutually exclusive with --drawioFile and --drawioPngFile).
      --diagramDisplayName string  Display name (defaults to the input filename without extension).
      --width int              Macro width in pixels (default 1151).
      --height int             Macro height in pixels (default 911).
      --comment string         Optional changelog message stored on the attachment.
      --jq string              Optional JMESPath filter against the response envelope.
      --outputFormat string    '' or 'toon' (default) | 'json'.

EXAMPLES:
  # Create a NEW page with an embedded drawio diagram:
  mcp-confluence upload_drawio \
      --spaceId=780763211 --title='Architecture v2' \
      --drawioFile=/home/bennie/diagrams/arch.drawio

  # Embed a diagram on an EXISTING page:
  mcp-confluence upload_drawio --pageId=163935 \
      --drawioPngFile=/home/bennie/diagrams/arch.drawio.png

  # Custom display name and macro size:
  mcp-confluence upload_drawio --pageId=163935 \
      --drawioFile=/home/bennie/diagrams/seq.drawio \
      --diagramDisplayName='Sequence diagram' --width=900 --height=600

  # Return just the two ids (attachment + custom-content) for follow-up:
  mcp-confluence upload_drawio --pageId=163935 --drawioFile=/tmp/x.drawio \
      --jq='{attachmentId: attachmentId, customContentId: customContentId, pageId: page.id}'

  # SVG variant:
  mcp-confluence upload_drawio --pageId=163935 \
      --drawioSvgFile=/home/bennie/diagrams/seq.drawio.svg

HERMES REGISTRATION:
  # The drawio macro emitted is the native owning-page variant
  # (ac:name="drawio" with fresh ac:local-id / ac:macro-id UUIDs
  # and the app metadata: mVer, inComment, pageId, custContentId,
  # diagramDisplayName, diagramName, contentVer, revision, baseUrl).
  # This distinction is load-bearing: the "drawio" macro OPENS the
  # editor; "inc-drawio" is for embedding a diagram owned by
  # another page and OPENS the viewer.
  #
  # Use from a Makefile for a "publish diagram" target:
  #
  #   publish-diagram:
  #       mcp-confluence upload_drawio \
  #           --spaceId=$$SPACE_ID --title=$$TITLE \
  #           --drawioFile=$$SOURCE \
  #           --jq='{attachmentId: attachmentId, customContentId: customContentId}'
  #
  # For general binary attachments (PNG, PDF, DOCX, etc. that
  # are NOT drawio diagrams) use upload_attachment instead
  # — it accepts any binary format and does not wrap with the
  # drawio-specific macro insertion.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runToolInvocation(cmd, nil, internal.HandleUploadDrawio, args)
		},
	}
	if err := registerFlagsFromArgsStruct(cmd, args); err != nil {
		panic("upload_drawio: registerFlagsFromArgsStruct: " + err.Error())
	}
	return cmd
}
