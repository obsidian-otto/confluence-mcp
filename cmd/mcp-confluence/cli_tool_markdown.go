// cmd/mcp-confluence/cli_tool_markdown.go
//
// Phase 21 — per-tool CLI subcommands for the 3 markdown
// handlers (conf_post_markdown, conf_put_markdown,
// conf_get_page_markdown). These wrap the v2 REST page endpoints
// with a markdown source — the handler runs the markdown through
// internal/markdown.MarkdownToStorageXHTML (or its reverse) and
// delegates to HandlePost / HandlePut / HandleGetPageBody, so the
// 9-step TOON/JMESPath/truncation pipeline is shared.
//
// Markdown source can be supplied in two ways:
//   - inline  (--markdown='## Hello, world.')
//   - on disk (--markdownFile=/path/to/page.md)
//
// Inline wins when both are set. At least one must be set;
// otherwise the handler returns an error.
//
// Pattern matches cli_tool_crud.go exactly. Same RunE body
// (runToolInvocation); per-command variation is the args struct
// type and the Handle* function. The args structs are larger
// than the CRUD ones (markdown / markdownFile / parentId /
// spaceId / title / status / jq / outputFormat) so the Long
// help text below lists the key flags explicitly.
package main

import (
	"github.com/spf13/cobra"

	internal "github.com/bennie/mcp-confluence/internal/tools"
)

// newConfPostMarkdownCmd returns the `conf_post_markdown`
// subcommand. It maps 1:1 to internal.HandlePostMarkdown — a
// wrapper over HandlePost that builds the storage XHTML from
// the supplied markdown source.
func newConfPostMarkdownCmd() *cobra.Command {
	args := &internal.PostMarkdownArgs{}
	cmd := &cobra.Command{
		Use:   "conf_post_markdown",
		Short: "Create a Confluence page from a markdown source (CommonMark + GFM)",
		Long: `conf_post_markdown creates a new Confluence page from a markdown
source (TOON-encoded response, by default). The handler runs
the markdown through internal/markdown.MarkdownToStorageXHTML
(goldmark → storage-format XHTML) and POSTs the result to
/wiki/api/v2/pages. CommonMark + GFM (tables, task lists, fenced
code blocks, strikethrough) is supported.

USAGE:
  mcp-confluence conf_post_markdown [flags]

FLAGS (auto-generated from internal/tools.PostMarkdownArgs):
      --spaceId string      Numeric space id where the new page will live (required, e.g. '780763211').
      --title string        Title for the new page (required).
      --markdown string     Markdown source for the new page body (alternative to --markdownFile).
      --markdownFile string Absolute path to a markdown file on disk (alternative to --markdown; capped at 1 MB).
      --parentId string     Optional parent page id; omit for a top-level page.
      --status string       'current' (default) | 'archived'.
      --jq string           Optional JMESPath filter applied to the created-page response.
      --outputFormat string  '' or 'toon' (default) | 'json'.

EXAMPLES:
  # Create a page from inline markdown:
  mcp-confluence conf_post_markdown --spaceId=780763211 --title='Hello' \
      --markdown=$'# Hello, world.\\n\\nA **bold** paragraph.'

  # Create a page from a file on disk:
  mcp-confluence conf_post_markdown --spaceId=780763211 --title='Oncall' \
      --markdownFile=/home/bennie/pages/oncall.md

  # Create a child page under an existing parent:
  mcp-confluence conf_post_markdown --spaceId=780763211 --title='Child' \
      --parentId=163935 --markdown=$'## Child\\n\\nNested content.'

  # Return just the new page id + version via jq:
  mcp-confluence conf_post_markdown --spaceId=780763211 --title='X' \
      --markdown='x' --jq='{id: id, version: version.number}'

HERMES REGISTRATION:
  # Not an MCP-host registration — per-tool subcommands are
  # the shell-script dispatch surface, not themselves MCP
  # tools. Use from a Makefile for a "publish doc" target:
  #
  #   publish-oncall-doc:
  #       mcp-confluence conf_post_markdown --spaceId=$$SPACE_ID \
  #           --title=$$TITLE --markdownFile=$$SOURCE \
  #           --jq='{id: id, title: title}'
  #
  # The markdown round-trip is lossy for some Confluence-
  # specific constructs (macros, info panels, layout sections,
  # mentions, attachments, status lozenges) — for those, use
  # the raw conf_post with hand-built storage XHTML.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runToolInvocation(cmd, nil, internal.HandlePostMarkdown, args)
		},
	}
	if err := registerFlagsFromArgsStruct(cmd, args); err != nil {
		panic("conf_post_markdown: registerFlagsFromArgsStruct: " + err.Error())
	}
	return cmd
}

// newConfPutMarkdownCmd returns the `conf_put_markdown`
// subcommand. It maps 1:1 to internal.HandlePutMarkdown — a
// wrapper over HandlePut that updates the page body from a
// markdown source.
func newConfPutMarkdownCmd() *cobra.Command {
	args := &internal.PutMarkdownArgs{}
	cmd := &cobra.Command{
		Use:   "conf_put_markdown",
		Short: "Update an existing Confluence page's body from a markdown source",
		Long: `conf_put_markdown updates the body of an existing Confluence page
from a markdown source (TOON-encoded response, by default). The
handler runs the markdown through internal/markdown and PUTs
the result to /wiki/api/v2/pages/{pageId}; the version.number
is auto-incremented by the underlying HandlePut handler.

USAGE:
  mcp-confluence conf_put_markdown [flags]

FLAGS (auto-generated from internal/tools.PutMarkdownArgs):
      --pageId string       Numeric page id of the page to update (required).
      --title string        New page title. Omit to keep the existing title.
      --markdown string     Markdown source for the new page body (alternative to --markdownFile).
      --markdownFile string Absolute path to a markdown file on disk (alternative to --markdown; capped at 1 MB).
      --jq string           Optional JMESPath filter applied to the updated-page response.
      --outputFormat string  '' or 'toon' (default) | 'json'.

EXAMPLES:
  # Update a page's body from inline markdown (also keeps existing title):
  mcp-confluence conf_put_markdown --pageId=163935 \
      --markdown=$'## New section\\n\\nUpdated body.'

  # Update from a file on disk AND change the title:
  mcp-confluence conf_put_markdown --pageId=163935 --title='Oncall v2' \
      --markdownFile=/home/bennie/pages/oncall.md

  # Return just the new version number (jq):
  mcp-confluence conf_put_markdown --pageId=163935 --markdown='x' \
      --jq='{id: id, version: version.number}'

HERMES REGISTRATION:
  # Use from a Makefile for a "rewrite doc" target:
  #
  #   rewrite-oncall-doc:
  #       mcp-confluence conf_put_markdown --pageId=$$PAGE_ID \
  #           --title=$$TITLE --markdownFile=$$SOURCE \
  #           --jq='{id: id, version: version.number}'
  #
  # PUT is a full-replacement operation, so the wire request
  # includes the page id + spaceId + version (incremented) +
  # the new body. If you only want to change the title (or any
  # single field), use conf_patch instead.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runToolInvocation(cmd, nil, internal.HandlePutMarkdown, args)
		},
	}
	if err := registerFlagsFromArgsStruct(cmd, args); err != nil {
		panic("conf_put_markdown: registerFlagsFromArgsStruct: " + err.Error())
	}
	return cmd
}

// newConfGetPageMarkdownCmd returns the `conf_get_page_markdown`
// subcommand. It maps 1:1 to internal.HandleGetPageMarkdown —
// reads a page and converts its storage XHTML body back to
// markdown.
func newConfGetPageMarkdownCmd() *cobra.Command {
	args := &internal.GetPageMarkdownArgs{}
	cmd := &cobra.Command{
		Use:   "conf_get_page_markdown",
		Short: "Read a Confluence page's body as markdown (CommonMark + GFM)",
		Long: `conf_get_page_markdown reads a page's storage body and converts
it to markdown (TOON-encoded envelope {pageId, title, markdown},
by default). The handler delegates to HandleGetPageBody (the
storage XHTML fetch) and then runs the body through
internal/markdown.StorageXHTMLToMarkdown (html-to-markdown v2).

The round-trip is lossy for some Confluence-specific constructs
(image alt text, layout sections, info panels, mentions,
attachments, status lozenges) — see the known-lossy register in
the spec. 14 of the 24+ feature categories are preserved.

USAGE:
  mcp-confluence conf_get_page_markdown [flags]

FLAGS (auto-generated from internal/tools.GetPageMarkdownArgs):
      --page-id string      Numeric page id (required, e.g. '163935').
      --jq string           Optional JMESPath filter against the {pageId, title, markdown} envelope.
      --outputFormat string  '' or 'toon' (default) | 'json'.

EXAMPLES:
  # Read a page as markdown (full envelope):
  mcp-confluence conf_get_page_markdown --page-id=163935

  # Get just the markdown text (jq against the envelope):
  mcp-confluence conf_get_page_markdown --page-id=163935 --jq=markdown

  # Pipe to a file for offline editing:
  mcp-confluence conf_get_page_markdown --page-id=163935 --jq=markdown > /tmp/page.md

HERMES REGISTRATION:
  # Use from a Makefile for a "download doc for editing" target:
  #
  #   download-page:
  #       mcp-confluence conf_get_page_markdown --page-id=$$PAGE_ID \
  #           --jq=markdown > $$SOURCE.md
  #
  # Round-trip (download → edit → re-upload):
  #
  #   round-trip-page:
  #       mcp-confluence conf_get_page_markdown --page-id=$$PAGE_ID --jq=markdown > /tmp/page.md
  #       # ... edit /tmp/page.md ...
  #       mcp-confluence conf_put_markdown --pageId=$$PAGE_ID --markdownFile=/tmp/page.md`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runToolInvocation(cmd, nil, internal.HandleGetPageMarkdown, args)
		},
	}
	if err := registerFlagsFromArgsStruct(cmd, args); err != nil {
		panic("conf_get_page_markdown: registerFlagsFromArgsStruct: " + err.Error())
	}
	return cmd
}
