// cmd/mcp-confluence/cli_tool_crud.go
//
// Phase 20 — per-tool CLI subcommands for the 5 CRUD handlers
// (conf_get, conf_post, conf_put, conf_patch, conf_delete).
//
// Each subcommand is a thin cobra adapter that:
//
//  1. Allocates a fresh instance of the matching *Args struct
//     (e.g. internal.GetArgs).
//
//  2. Calls registerFlagsFromArgsStruct (from cli_tool_dispatch.go)
//     to populate cmd.Flags() with a --flag for every struct
//     field, with descriptions lifted from the jsonschema
//     tags. Cobra's --help output shows the flags; cobra's
//     parser accepts them on the command line.
//
//  3. Wires RunE to runToolInvocation (from
//     cli_tool_dispatch.go), which:
//     - composes the persistent flags (--site/--email/--api-token)
//     into the process env (Q22 ordering);
//     - loads config + builds an atlassian.Client;
//     - reads the bound flags back into the args struct and
//     marshals to json.RawMessage;
//     - invokes the locked internal.Handle* function;
//     - prints the returned string to stdout (the ONE
//     legitimate stdout write in the binary).
//
// The 5 subcommands share the same RunE body; the per-command
// variation is just the args struct type and the Handle*
// function. The Long help text below is hand-authored because
// (a) it surfaces the EXAMPLES the operator will copy-paste
// and (b) the HERMES REGISTRATION block documents how the
// subcommand fits into a Makefile / shell-script automation
// flow (NOT an MCP-host registration, because the
// per-tool subcommands are not themselves MCP servers — they
// are the dispatch surface for a single tool call).
package main

import (
	"github.com/spf13/cobra"

	internal "github.com/bennie/mcp-confluence/internal/tools"
)

// newConfGetCmd returns the `conf_get` subcommand. It maps
// 1:1 to internal.HandleGet — the only state it carries is the
// request args struct (Path, Query, JQ, OutputFormat).
func newConfGetCmd() *cobra.Command {
	args := &internal.GetArgs{}
	cmd := &cobra.Command{
		Use:   "conf_get",
		Short: "Read any Confluence data (HTTP GET; 30-60% token savings via TOON)",
		Long: `conf_get issues an HTTP GET to the Confluence REST API and
prints the decoded response (TOON-encoded by default) on stdout.
This is the raw REST pass-through; the convenience wrappers
(conf_list_spaces, conf_list_pages, conf_get_page_body, etc.)
build on top of this in later phases.

USAGE:
  mcp-confluence conf_get [flags]

FLAGS (auto-generated from internal/tools.GetArgs):
      --path string         The Confluence REST API path. Must start with /wiki/. (required)
      --query string        Optional URL query parameters as k1=v1,k2=v2.
      --jq string           Optional JMESPath filter evaluated against the decoded response.
      --outputFormat string Output format: '' or 'toon' (default; 30-60% fewer tokens) | 'json'.

EXAMPLES:
  # List up to 2 spaces (TOON-encoded by default):
  mcp-confluence conf_get --path=/wiki/api/v2/spaces?limit=2

  # Get a single page's storage body, extract just the markdown via jq:
  mcp-confluence conf_get --path=/wiki/api/v2/pages/163935/body --jq=results.value

  # Force raw JSON output (skip TOON encoding):
  mcp-confluence conf_get --path=/wiki/api/v2/spaces?limit=1 --outputFormat=json

HERMES REGISTRATION:
  # Not an MCP-host registration. The 5 CRUD subcommands are
  # not exposed as MCP tools — they are the shell-script /
  # Makefile dispatch surface for a single tool call.
  #
  # Use them from a Makefile target:
  #
  #   list-spaces:
  #       mcp-confluence conf_get --path=/wiki/api/v2/spaces?limit=$$LIMIT
  #
  # Or from a shell script that pipes to jq:
  #
  #   mcp-confluence conf_get --path=/wiki/api/v2/spaces \
  #       --jq='results[*].{id: id, key: key, name: name}'`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runToolInvocation(cmd, nil, internal.HandleGet, args)
		},
	}
	// Register the per-field cobra flags. The subcommand's
	// --help text (rendered by the custom HelpFunc) shows the
	// auto-generated FLAGS section; the field tags are the
	// single source of truth.
	if err := registerFlagsFromArgsStruct(cmd, args); err != nil {
		// Programmer error: the args struct has a field
		// type we don't support. Fail loud at startup so
		// the regression is caught before any user invocation.
		panic("conf_get: registerFlagsFromArgsStruct: " + err.Error())
	}
	return cmd
}

// newConfPostCmd returns the `conf_post` subcommand. POST takes
// a JSON body, so the --body flag carries the raw object string
// (the Handle* layer parses it via json.Unmarshal).
func newConfPostCmd() *cobra.Command {
	args := &internal.PostArgs{}
	cmd := &cobra.Command{
		Use:   "conf_post",
		Short: "Create Confluence resources (HTTP POST)",
		Long: `conf_post issues an HTTP POST to the Confluence REST API and
prints the decoded response (TOON-encoded by default) on stdout.
The request body is supplied via the --body flag as a JSON
object string; HandlePost marshals it before sending.

USAGE:
  mcp-confluence conf_post [flags]

FLAGS (auto-generated from internal/tools.PostArgs):
      --path string         The Confluence REST API path (POST target). (required)
      --query string        Optional URL query parameters as k1=v1,k2=v2.
      --body string         JSON object to send as the request body.
      --jq string           Optional JMESPath filter applied to the response.
      --outputFormat string Output format: '' or 'toon' (default) | 'json'.

EXAMPLES:
  # Create a new page in a space (storage-format body):
  mcp-confluence conf_post --path=/wiki/api/v2/pages \
      --body='{"spaceId":"780763211","status":"current","title":"CLI test","body":{"representation":"storage","value":"<p>hi</p>"}}'

  # Add a label to a page:
  mcp-confluence conf_post --path=/wiki/api/v2/pages/163935/labels \
      --body='{"name":"needs-review"}'

HERMES REGISTRATION:
  # Shell-script automation example — gate a doc publish step:
  #
  #   publish-doc:
  #       mcp-confluence conf_post --path=/wiki/api/v2/pages \
  #           --body="$$(jq -n --arg title "$$TITLE" --arg body "$$BODY" \
  #               '{spaceId: "780763211", status: "current", title: $$title, body: {representation: "storage", value: $$body}}')"`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runToolInvocation(cmd, nil, internal.HandlePost, args)
		},
	}
	if err := registerFlagsFromArgsStruct(cmd, args); err != nil {
		panic("conf_post: registerFlagsFromArgsStruct: " + err.Error())
	}
	return cmd
}

// newConfPutCmd returns the `conf_put` subcommand. PUT is a
// full-replacement operation, so the --body must contain the
// complete resource (including version.number incremented by 1
// for pages).
func newConfPutCmd() *cobra.Command {
	args := &internal.PutArgs{}
	cmd := &cobra.Command{
		Use:   "conf_put",
		Short: "Replace Confluence resources (HTTP PUT — full update)",
		Long: `conf_put issues an HTTP PUT to the Confluence REST API and
prints the decoded response (TOON-encoded by default) on stdout.
PUT is a full-replacement operation: the request body must
contain the complete resource (e.g. for pages, version.number
incremented by 1). For partial updates use conf_patch.

USAGE:
  mcp-confluence conf_put [flags]

FLAGS (auto-generated from internal/tools.PutArgs):
      --path string         The Confluence REST API path (PUT target). (required)
      --query string        Optional URL query parameters as k1=v1,k2=v2.
      --body string         JSON object representing the full replacement resource.
      --jq string           Optional JMESPath filter applied to the response.
      --outputFormat string Output format: '' or 'toon' (default) | 'json'.

EXAMPLES:
  # Update a page's title (full replacement — body must include version):
  mcp-confluence conf_put --path=/wiki/api/v2/pages/163935 \
      --body='{"id":"163935","status":"current","title":"New title","spaceId":"780763211","body":{"representation":"storage","value":"<p>Updated</p>"},"version":{"number":2}}'

  # Return just the new version number (jq filter):
  mcp-confluence conf_put --path=/wiki/api/v2/pages/163935 \
      --body='{"id":"163935","status":"current","title":"X","spaceId":"780763211","version":{"number":2}}' \
      --jq='{id: id, version: version.number}'

HERMES REGISTRATION:
  # Use conf_put from a Makefile for in-place doc rewrites:
  #
  #   rewrite-doc:
  #       mcp-confluence conf_put --path=/wiki/api/v2/pages/$$PAGE_ID \
  #           --body="$$(cat /tmp/page-payload.json)"`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runToolInvocation(cmd, nil, internal.HandlePut, args)
		},
	}
	if err := registerFlagsFromArgsStruct(cmd, args); err != nil {
		panic("conf_put: registerFlagsFromArgsStruct: " + err.Error())
	}
	return cmd
}

// newConfPatchCmd returns the `conf_patch` subcommand. PATCH
// takes an RFC 6902-style array of operations as its body (not
// a single object), so --body is a JSON array string.
func newConfPatchCmd() *cobra.Command {
	args := &internal.PatchArgs{}
	cmd := &cobra.Command{
		Use:   "conf_patch",
		Short: "Partially update Confluence resources (HTTP PATCH — JSON Patch array)",
		Long: `conf_patch issues an HTTP PATCH to the Confluence REST API and
prints the decoded response (TOON-encoded by default) on stdout.
The request body is a JSON ARRAY of patch operations
(RFC 6902 style) — not a single object.

USAGE:
  mcp-confluence conf_patch [flags]

FLAGS (auto-generated from internal/tools.PatchArgs):
      --path string         The Confluence REST API path (PATCH target). (required)
      --query string        Optional URL query parameters as k1=v1,k2=v2.
      --body string         JSON ARRAY of patch operations (RFC 6902 style).
      --jq string           Optional JMESPath filter applied to the response.
      --outputFormat string Output format: '' or 'toon' (default) | 'json'.

EXAMPLES:
  # Update only the page title (and version bump) via two PATCH ops:
  mcp-confluence conf_patch --path=/wiki/api/v2/pages/163935 \
      --body='[{"op":"replace","path":"/title","value":"New title"},{"op":"replace","path":"/version/number","value":"2"}]'

HERMES REGISTRATION:
  # PATCH from a Makefile for atomic field-level updates:
  #
  #   bump-title:
  #       mcp-confluence conf_patch --path=/wiki/api/v2/pages/$$PAGE_ID \
  #           --body="[{\"op\":\"replace\",\"path\":\"/title\",\"value\":\"$$NEW_TITLE\"}]"`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runToolInvocation(cmd, nil, internal.HandlePatch, args)
		},
	}
	if err := registerFlagsFromArgsStruct(cmd, args); err != nil {
		panic("conf_patch: registerFlagsFromArgsStruct: " + err.Error())
	}
	return cmd
}

// newConfDeleteCmd returns the `conf_delete` subcommand.
// DELETE takes no body, so the only args are Path, Query, JQ,
// OutputFormat — same shape as conf_get.
func newConfDeleteCmd() *cobra.Command {
	args := &internal.DeleteArgs{}
	cmd := &cobra.Command{
		Use:   "conf_delete",
		Short: "Delete Confluence resources (HTTP DELETE — most return 204 No Content)",
		Long: `conf_delete issues an HTTP DELETE to the Confluence REST API.
Most successful deletes return 204 No Content with an empty
body, so the TOON-encoded stdout is typically empty; pass
--outputFormat=json to surface the envelope verbatim.

USAGE:
  mcp-confluence conf_delete [flags]

FLAGS (auto-generated from internal/tools.DeleteArgs):
      --path string         The Confluence REST API path (DELETE target). (required)
      --query string        Optional URL query parameters as k1=v1,k2=v2.
      --jq string           Optional JMESPath filter applied to the (likely empty) response.
      --outputFormat string Output format: '' or 'toon' (default) | 'json'.

EXAMPLES:
  # Delete a page by id:
  mcp-confluence conf_delete --path=/wiki/api/v2/pages/163935

  # Delete a label from a page (path includes the label id):
  mcp-confluence conf_delete --path=/wiki/api/v2/pages/163935/labels/789

  # Force JSON output so the (empty) envelope is visible:
  mcp-confluence conf_delete --path=/wiki/api/v2/pages/163935 --outputFormat=json

HERMES REGISTRATION:
  # Shell-script bulk delete (run with care — destructive):
  #
  #   cleanup-stale-pages:
  #       for id in $$(cat /tmp/stale-ids.txt); do \
  #           mcp-confluence conf_delete --path=/wiki/api/v2/pages/$$id; \
  #       done`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runToolInvocation(cmd, nil, internal.HandleDelete, args)
		},
	}
	if err := registerFlagsFromArgsStruct(cmd, args); err != nil {
		panic("conf_delete: registerFlagsFromArgsStruct: " + err.Error())
	}
	return cmd
}
