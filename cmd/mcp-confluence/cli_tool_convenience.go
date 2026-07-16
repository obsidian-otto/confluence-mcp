// cmd/mcp-confluence/cli_tool_convenience.go
//
// Phase 21 — per-tool CLI subcommands for the 6 convenience
// handlers (list_spaces, list_pages, get_page_body,
// get_page_tree, search, help). Each is a typed
// wrapper over the raw CRUD pass-through; the subcommand's
// args struct is the handler's well-typed schema (no JMESPath
// required, no path memorization).
//
// Pattern matches cli_tool_crud.go exactly:
//
//  1. Allocate a fresh *Args struct.
//  2. Call registerFlagsFromArgsStruct to populate cmd.Flags()
//     with a --flag for every struct field.
//  3. Wire RunE to runToolInvocation (the ONE legitimate stdout
//     writer in the binary) which:
//     - composes the persistent flags (--site/--email/--api-token)
//     into the process env (Q22 ordering);
//     - loads config + builds an atlassian.Client;
//     - reads the bound flags back into the args struct and
//     marshals to json.RawMessage;
//     - invokes the locked internal.Handle* function;
//     - prints the returned string to stdout.
//
// The Long help text below is hand-authored for two reasons:
// (a) the EXAMPLES block is what the operator copy-pastes, and
// (b) the AUTOMATION block documents how the subcommand
// fits into a Makefile / shell-script automation flow (NOT an
// MCP-host registration, because the per-tool subcommands are
// not themselves MCP servers — they are the dispatch surface
// for a single tool call).
package main

import (
	"github.com/spf13/cobra"

	internal "github.com/bennie/mcp-confluence/internal/tools"
)

// newListSpacesCmd returns the `list_spaces` subcommand.
// It maps 1:1 to internal.HandleListSpaces — a wrapper over
// /wiki/api/v2/spaces with sensible-by-default field selection.
func newListSpacesCmd() *cobra.Command {
	args := &internal.ListSpacesArgs{}
	cmd := &cobra.Command{
		Use:   "list_spaces",
		Short: "List Confluence spaces with sensible defaults",
		Long: `list_spaces lists all Confluence spaces the caller has
access to (TOON-encoded by default). It is a typed wrapper over
get /wiki/api/v2/spaces — no path or JMESPath required.

USAGE:
  mcp-confluence list_spaces [flags]

FLAGS (auto-generated from internal/tools.ListSpacesArgs):
      --limit int          Maximum number of spaces to return (default 25; max 250).
      --cursor string      Opaque pagination cursor from a previous call.
      --type string        Filter by space type. '' for all. Common: 'personal', 'global'.
      --status string      Filter by space status. '' for all. Common: 'current', 'archived'.
      --outputFormat string  '' or 'toon' (default; 30-60% fewer tokens) | 'json'.

EXAMPLES:
  # List all current spaces, 5 per page:
  mcp-confluence list_spaces --limit=5 --status=current

  # List your personal spaces only, with a jq filter for the essentials:
  mcp-confluence list_spaces --type=personal --jq='results[*].{id: id, key: key, name: name}'

  # Force raw JSON output:
  mcp-confluence list_spaces --limit=1 --outputFormat=json

AUTOMATION:
  # Not an MCP-host registration. The per-tool subcommands are
  # not exposed as MCP tools — they are the shell-script /
  # Makefile dispatch surface for a single tool call.
  #
  # Use from a Makefile target:
  #
  #   list-spaces:
  #       mcp-confluence list_spaces --limit=$$LIMIT --jq='results[*].{id: id, key: key}'
  #
  # Or pipe to grep:
  #
  #   mcp-confluence list_spaces --type=personal | grep -i 'bennie'`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runToolInvocation(cmd, nil, internal.HandleListSpaces, args)
		},
	}
	if err := registerFlagsFromArgsStruct(cmd, args); err != nil {
		panic("list_spaces: registerFlagsFromArgsStruct: " + err.Error())
	}
	return cmd
}

// newListPagesCmd returns the `list_pages` subcommand.
// It maps 1:1 to internal.HandleListPages — a wrapper over
// /wiki/api/v2/pages with space-id, status, and title filters.
func newListPagesCmd() *cobra.Command {
	args := &internal.ListPagesArgs{}
	cmd := &cobra.Command{
		Use:   "list_pages",
		Short: "List Confluence pages with filters by space, title, status, sort",
		Long: `list_pages lists pages in a Confluence space (TOON-encoded
by default). It is a typed wrapper over get
/wiki/api/v2/pages — pass --space-id to scope the listing (without
it the result set is the entire site).

USAGE:
  mcp-confluence list_pages [flags]

FLAGS (auto-generated from internal/tools.ListPagesArgs):
      --space-id string    Numeric space id (e.g. '780763211'). Strongly recommended.
      --space-key string   Space key (e.g. '~712020...'). Mutually exclusive with --space-id.
      --title string       Substring filter on page titles (case-sensitive).
      --status string      Page status filter. 'current' for non-archived pages.
      --limit int          Maximum pages to return (default 25; max 250).
      --cursor string      Opaque pagination cursor.
      --sort string        Server-side sort field ('id', 'title', '-modified-date', etc).
      --body-format string '' | 'storage' | 'view' | 'atlas_doc_format'. Omit for no body.
      --outputFormat string  '' or 'toon' (default) | 'json'.

EXAMPLES:
  # List the 10 most recently modified pages in a space:
  mcp-confluence list_pages --space-id=780763211 --limit=10 --sort=-modified-date

  # Find all current pages whose title contains "oncall":
  mcp-confluence list_pages --space-id=780763211 --title=oncall --status=current

  # Drill into one page's id + title via jq:
  mcp-confluence list_pages --space-id=780763211 --limit=5 \
      --jq='results[*].{id: id, title: title}'

AUTOMATION:
  # Use from a Makefile for "list all pages in space X" targets:
  #
  #   list-pages:
  #       mcp-confluence list_pages --space-id=$$SPACE_ID --limit=$$LIMIT
  #
  # Or for a nightly audit:
  #
  #   nightly-audit:
  #       mcp-confluence list_pages --space-id=$$SPACE_ID --sort=-modified-date \
  #           --jq='results[*].{id: id, title: title, modified: version}'`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runToolInvocation(cmd, nil, internal.HandleListPages, args)
		},
	}
	if err := registerFlagsFromArgsStruct(cmd, args); err != nil {
		panic("list_pages: registerFlagsFromArgsStruct: " + err.Error())
	}
	return cmd
}

// newGetPageBodyCmd returns the `get_page_body` subcommand.
// It maps 1:1 to internal.HandleGetPageBody — fetches a single
// page's body in storage / view / atlas_doc_format representation.
func newGetPageBodyCmd() *cobra.Command {
	args := &internal.GetPageBodyArgs{}
	cmd := &cobra.Command{
		Use:   "get_page_body",
		Short: "Read a single page's body in a chosen representation",
		Long: `get_page_body reads the body of a single Confluence page
(TOON-encoded by default). The --body-format flag picks the
representation: 'storage' (default, raw Confluence XHTML), 'view'
(rendered HTML), or 'atlas_doc_format' (Atlassian Document Format).

USAGE:
  mcp-confluence get_page_body [flags]

FLAGS (auto-generated from internal/tools.GetPageBodyArgs):
      --page-id string       Numeric page id (required). Example: '163935'.
      --body-format string   'storage' (default) | 'view' | 'atlas_doc_format'.
      --outputFormat string  '' or 'toon' (default) | 'json'.

EXAMPLES:
  # Get a page's storage body (the form that PUT/PATCH accept back):
  mcp-confluence get_page_body --page-id=163935 --body-format=storage

  # Get the rendered HTML view of a page:
  mcp-confluence get_page_body --page-id=163935 --body-format=view

  # Get just the body value (jq against the {value, representation} envelope):
  mcp-confluence get_page_body --page-id=163935 --jq='value'

AUTOMATION:
  # Use from a Makefile for "fetch this page and write it to disk":
  #
  #   fetch-page-body:
  #       mcp-confluence get_page_body --page-id=$$PAGE_ID --body-format=storage \
  #           --outputFormat=json > /tmp/page-body.json
  #
  # The TOON default is fine when piping into a Go / Python tool
  # that knows the format; use --outputFormat=json when piping
  # into a generic shell pipeline (jq without a TOON frontend).`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runToolInvocation(cmd, nil, internal.HandleGetPageBody, args)
		},
	}
	if err := registerFlagsFromArgsStruct(cmd, args); err != nil {
		panic("get_page_body: registerFlagsFromArgsStruct: " + err.Error())
	}
	return cmd
}

// newGetPageTreeCmd returns the `get_page_tree` subcommand.
// It maps 1:1 to internal.HandleGetPageTree — fans out three v2
// REST calls (ancestors, children, descendants) and merges the
// envelopes into one response.
func newGetPageTreeCmd() *cobra.Command {
	args := &internal.GetPageTreeArgs{}
	cmd := &cobra.Command{
		Use:   "get_page_tree",
		Short: "Get a page's ancestors, children, and descendants in one call",
		Long: `get_page_tree returns a page's position in its space tree:
the ancestor chain (root → immediate parent), direct children,
and the descendants subtree — three v2 endpoints merged into one
response (TOON-encoded by default).

USAGE:
  mcp-confluence get_page_tree [flags]

FLAGS (auto-generated from internal/tools.GetPageTreeArgs):
      --page-id string     Numeric page id whose tree position to fetch (required).
      --limit int          Per-subcall cap on results in ancestors/children/descendants (default 25; max 250).
      --depth int          For descendants only: how many levels deep to recurse (default 1; max 10).
      --outputFormat string  '' or 'toon' (default) | 'json'.

EXAMPLES:
  # Get the immediate tree position (ancestors + direct children):
  mcp-confluence get_page_tree --page-id=163935

  # Recurse 3 levels into the descendants subtree:
  mcp-confluence get_page_tree --page-id=163935 --depth=3 --limit=50

  # Flatten ancestors to {id, title} pairs for breadcrumb UI:
  mcp-confluence get_page_tree --page-id=163935 \
      --jq='{ancestors: ancestors.results[*].{id: id, title: title}}'

AUTOMATION:
  # Local addition — Confluence Cloud v2 has no single
  # "get page tree position" endpoint; the ancestors/children/
  # descendants split is an API design choice. The subcommand
  # fans out three GETs and merges the envelopes; callers see
  # one combined response keyed by ancestors/children/descendants.
  #
  # Use from a Makefile for a breadcrumb generator:
  #
  #   breadcrumbs:
  #       mcp-confluence get_page_tree --page-id=$$PAGE_ID \
  #           --jq='ancestors.results[*].title' | paste -sd '/'`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runToolInvocation(cmd, nil, internal.HandleGetPageTree, args)
		},
	}
	if err := registerFlagsFromArgsStruct(cmd, args); err != nil {
		panic("get_page_tree: registerFlagsFromArgsStruct: " + err.Error())
	}
	return cmd
}

// newSearchCmd returns the `search` subcommand. It maps
// 1:1 to internal.HandleSearch — wraps the v1 search endpoint
// (/wiki/rest/api/search) with a CQL expression.
func newSearchCmd() *cobra.Command {
	args := &internal.SearchArgs{}
	cmd := &cobra.Command{
		Use:   "search",
		Short: "Search Confluence via Confluence Query Language (CQL)",
		Long: `search searches Confluence content (TOON-encoded by default).
The v1 search endpoint is the only Confluence API that accepts
CQL; the v2 endpoints do not understand CQL or a portable search
expression.

USAGE:
  mcp-confluence search [flags]

FLAGS (auto-generated from internal/tools.SearchArgs):
      --cql string         Confluence Query Language expression (required). Caller URL-encodes special characters.
      --limit int          Maximum search results to return (default 25; max 100).
      --start int          Pagination start offset for v1 search (default 0).
      --excludedContent string  Optional v1 search 'excerpt' inclusion ('excerpt' | 'content' | 'highlight').
      --outputFormat string  '' or 'toon' (default) | 'json'.

EXAMPLES:
  # Find pages mentioning mcp-confluence:
  mcp-confluence search --cql='type=page AND text~mcp-confluence'

  # Find a personal space by name:
  mcp-confluence search --cql='type=page AND space.type=personal AND space.title~bennie'

  # Pages you created:
  mcp-confluence search --cql='creator=currentUser() AND type=page' --limit=10

  # Get just titles + urls (jq against the v1 results envelope):
  mcp-confluence search --cql='type=page AND text~oncall' \
      --jq='results[*].{title: title, url: url}'

AUTOMATION:
  # URL-encode CQL expressions with --cql=... — the handler
  # does NOT auto-encode. Use $'...' (bash) or jq -rnR
  # for the awkward operators.
  #
  # Use from a Makefile for ad-hoc audit queries:
  #
  #   audit-stale-docs:
  #       mcp-confluence search \
  #           --cql='lastModified < "$$CUTOFF" AND type=page' \
  #           --jq='results[*].{title: title, lastModified: lastModified}'`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runToolInvocation(cmd, nil, internal.HandleSearch, args)
		},
	}
	if err := registerFlagsFromArgsStruct(cmd, args); err != nil {
		panic("search: registerFlagsFromArgsStruct: " + err.Error())
	}
	return cmd
}

// newHelpCmd returns the `help` subcommand. It maps 1:1
// to internal.HandleHelp — returns a human-readable tour of the
// 18 MCP tool surface, optionally filtered to a single topic.
func newHelpCmd() *cobra.Command {
	args := &internal.HelpArgs{}
	cmd := &cobra.Command{
		Use:   "help",
		Short: "Show how to use the confluence MCP server — the tool surface in one call",
		Long: `help returns a human-readable tour of the 18 MCP tool surface
(TOON-encoded by default). Pass --topic=<tool-name> to get just
one tool's entry (e.g. --topic=conf_get).

USAGE:
  mcp-confluence help [flags]

FLAGS (auto-generated from internal/tools.HelpArgs):
      --topic string       Optional tool name to filter the help response (e.g. 'conf_get'). '' or 'all' = full surface.
      --outputFormat string  '' or 'toon' (default, preferred for human reading) | 'json'.

EXAMPLES:
  # Full tool surface:
  mcp-confluence help

  # Just one tool's entry:
  mcp-confluence help --topic=conf_post_markdown

  # Force raw JSON for downstream tooling:
  mcp-confluence help --outputFormat=json | jq 'keys'

AUTOMATION:
  # Use from a Makefile for a "what tools does this server
  # expose?" smoke check:
  #
  #   list-tools:
  #       mcp-confluence help --outputFormat=json | jq 'keys'
  #
  # The TOON default is preferred for human reading; use
  # --outputFormat=json when piping into jq / a script.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runToolInvocation(cmd, nil, internal.HandleHelp, args)
		},
	}
	if err := registerFlagsFromArgsStruct(cmd, args); err != nil {
		panic("help: registerFlagsFromArgsStruct: " + err.Error())
	}
	return cmd
}
