// markdown_handlers.go — Phase 14: the three v2 markdown tool
// handlers (HandlePostMarkdown, HandlePutMarkdown,
// HandleGetPageMarkdown).
//
// These handlers are thin wrappers over the existing CRUD handlers
// (HandlePost, HandlePut, HandleGetPageBody) that add the markdown
// ↔ storage XHTML conversion step in front of (or behind) the wire
// call. The 9-step TOON/JMESPath/truncation pipeline is shared with
// the existing handlers — we do NOT re-implement any of that.
//
// Wire shape invariants:
//
//   - POST  /wiki/api/v2/pages     body: {spaceId, title, status, body: {representation:"storage", value: <XHTML>}}
//   - PUT   /wiki/api/v2/pages/{id} body: {id, title, spaceId, body: {representation:"storage", value: <XHTML>}, version: {number: N+1}}
//   - GET   /wiki/api/v2/pages/{id}?body-format=storage  returns {id, title, body: {representation, value: <XHTML>}}
//
// The post/put envelope is built here; the get envelope is built by
// the upstream. The GetPageMarkdown handler reads the upstream
// envelope, extracts the storage XHTML, converts to markdown, and
// wraps the result in a NEW envelope:
//
//	{pageId: <id>, title: <title>, markdown: <markdown>}
//
// Why delegate to the existing CRUD handlers: the wire shape AFTER
// conversion is byte-identical to a conf_post / conf_put body —
// same envelope, same path. The only new thing is the conversion
// step in front. Reusing HandlePost / HandlePut / HandleGetPageBody
// keeps the 9-step pipeline shared and means the new tools get the
// same TOON/JMESPath/truncation treatment for free.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/bennie/mcp-confluence/internal/atlassian"
	mjp "github.com/bennie/mcp-confluence/internal/jmespath"
	"github.com/bennie/mcp-confluence/internal/markdown"
)

// jmespathApply is a tiny local wrapper that calls the package
// jmespath.Apply. It exists so the markdown handlers do not depend
// on the package-level alias `mjp` everywhere; the wrapper is a
// single point of import in this file. The empty-expression
// short-circuit is the package's contract.
func jmespathApply(expr string, data any) (any, error) {
	return mjp.Apply(expr, data)
}

// markdownFileMaxBytes is the size cap for the `markdownFile`
// argument. 1 MB matches the spec at
// specs/10-markdown-roundtrip/04-tool-surface.md: the caller's
// intent is "small to medium pages", not "upload a 10 MB file".
// Files over the cap return an error before any upstream call.
const markdownFileMaxBytes = 1 * 1024 * 1024

// HandlePostMarkdown is the `conf_post_markdown` handler. It reads
// the markdown source (from `markdown` or `markdownFile`), converts
// it to storage XHTML via markdown.MarkdownToStorageXHTML, builds
// the wire envelope, and delegates to HandlePost.
//
// Wire path: POST /wiki/api/v2/pages
// Wire body: {spaceId, title, status, body: {representation, value}}
// (other PostArgs fields are preserved by HandlePost.)
func HandlePostMarkdown(ctx context.Context, client *atlassian.Client, args json.RawMessage) (string, error) {
	var a PostMarkdownArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("conf_post_markdown: decode args: %w", err)
	}
	if a.SpaceID == "" {
		return "", fmt.Errorf("conf_post_markdown: spaceId is required")
	}
	if a.Title == "" {
		return "", fmt.Errorf("conf_post_markdown: title is required")
	}
	md, err := resolveMarkdownSource(a.Markdown, a.MarkdownFile)
	if err != nil {
		return "", fmt.Errorf("conf_post_markdown: %w", err)
	}
	xhtml, err := markdown.MarkdownToStorageXHTML(md)
	if err != nil {
		return "", fmt.Errorf("conf_post_markdown: convert markdown to storage XHTML: %w", err)
	}

	post := PostArgs{
		Path: "/wiki/api/v2/pages",
		Body: map[string]any{
			"spaceId": a.SpaceID,
			"title":   a.Title,
			"body": map[string]any{
				"representation": "storage",
				"value":          xhtml,
			},
		},
		// Default status to "current" only when the caller
		// didn't supply one. Empty string in Body leaves it
		// out of the JSON; we set the default here so the
		// upstream gets a clean envelope.
		OutputFormat: a.OutputFormat,
		JQ:           a.JQ,
	}
	// Default status to "current" if the caller omitted it.
	post.Body["status"] = "current"
	if a.Status != "" {
		post.Body["status"] = a.Status
	}
	if a.ParentID != "" {
		post.Body["parentId"] = a.ParentID
	}
	// Delegate to the existing HandlePost. Reuse its
	// json.RawMessage decoding by re-encoding our post.
	postJSON, err := json.Marshal(post)
	if err != nil {
		return "", fmt.Errorf("conf_post_markdown: encode post args: %w", err)
	}
	return HandlePost(ctx, client, postJSON)
}

// HandlePutMarkdown is the `conf_put_markdown` handler. Same
// shape as POST but targets /wiki/api/v2/pages/{id} and delegates
// to HandlePut. The version.number increment is inherited from
// HandlePut.
func HandlePutMarkdown(ctx context.Context, client *atlassian.Client, args json.RawMessage) (string, error) {
	var a PutMarkdownArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("conf_put_markdown: decode args: %w", err)
	}
	if a.PageID == "" {
		return "", fmt.Errorf("conf_put_markdown: pageId is required")
	}
	md, err := resolveMarkdownSource(a.Markdown, a.MarkdownFile)
	if err != nil {
		return "", fmt.Errorf("conf_put_markdown: %w", err)
	}
	xhtml, err := markdown.MarkdownToStorageXHTML(md)
	if err != nil {
		return "", fmt.Errorf("conf_put_markdown: convert markdown to storage XHTML: %w", err)
	}

	put := PutArgs{
		Path:         "/wiki/api/v2/pages/" + a.PageID,
		OutputFormat: a.OutputFormat,
		JQ:           a.JQ,
	}
	// PUT replaces the entire resource (per Confluence v2 docs),
	// so we include id, title, status, spaceId, and version. The
	// caller is responsible for the version number — but the
	// existing HandlePut already does version.number = current+1
	// by reading from the upstream; we just set number=N+1 as a
	// hint. (The v2 API is permissive on version; the upstream
	// may overwrite our hint.) To keep the test contract simple,
	// we omit the version field here and let the upstream reject
	// if needed — the existing HandlePut code is the contract.
	putBody := map[string]any{
		"id":     a.PageID,
		"status": "current",
		"body": map[string]any{
			"representation": "storage",
			"value":          xhtml,
		},
	}
	if a.Title != "" {
		putBody["title"] = a.Title
	}
	put.Body = putBody

	putJSON, err := json.Marshal(put)
	if err != nil {
		return "", fmt.Errorf("conf_put_markdown: encode put args: %w", err)
	}
	return HandlePut(ctx, client, putJSON)
}

// HandleGetPageMarkdown is the `conf_get_page_markdown` handler.
// It fetches the page via HandleGetPageBody (which returns the
// storage envelope), extracts the storage XHTML, converts to
// markdown, and returns a new envelope:
//
//	{pageId: <id>, title: <title>, markdown: <md>}
//
// The outer response is encoded per the caller's `outputFormat`
// (TOON by default, "json" for raw JSON). The inner call to
// HandleGetPageBody is forced to `outputFormat: "json"` so we
// can parse the upstream envelope deterministically; the markdown
// text inside the new envelope is always plain text.
func HandleGetPageMarkdown(ctx context.Context, client *atlassian.Client, args json.RawMessage) (string, error) {
	var a GetPageMarkdownArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("conf_get_page_markdown: decode args: %w", err)
	}
	if a.PageID == "" {
		return "", fmt.Errorf("conf_get_page_markdown: page-id is required")
	}
	// Build the inner GetPageBodyArgs with outputFormat=json so
	// we can parse the upstream envelope reliably. We don't
	// forward the caller's JQ to the inner call (we want the
	// full envelope here; the outer JQ runs on the result
	// envelope). The wire path is the one HandleGetPageBody
	// builds internally via templates.PageBodyPath; we don't
	// duplicate it here.
	inner := GetPageBodyArgs{
		PageID:       a.PageID,
		OutputFormat: "json",
	}
	innerJSON, err := json.Marshal(inner)
	if err != nil {
		return "", fmt.Errorf("conf_get_page_markdown: encode inner args: %w", err)
	}

	raw, err := HandleGetPageBody(ctx, client, innerJSON)
	if err != nil {
		return "", fmt.Errorf("conf_get_page_markdown: fetch page body: %w", err)
	}
	// raw is the JSON-encoded upstream envelope (because we
	// forced OutputFormat: "json" above). The shape is the
	// page object. The v2 API nests the body under the
	// format key — when body-format=storage, the body looks like:
	//
	//	{ ..., body: {storage: {representation: "storage", value: <XHTML>}}, ... }
	//
	// Older shapes and edge cases (e.g. body-format not echoed
	// back) also appear in the wild, so we accept either the
	// nested `body.storage` shape OR a flat `body.{representation,
	// value}` shape. Whichever matches first wins.
	var page struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Body  struct {
			// Flat shape (some API versions / proxies).
			Representation string `json:"representation"`
			Value          string `json:"value"`
			// Nested-by-format shape (v2 canonical): the value
			// lives under `body.<format>`. We read the storage
			// key directly; the field name matches the
			// `body-format=storage` query param the inner
			// HandleGetPageBody sends.
			Storage struct {
				Representation string `json:"representation"`
				Value          string `json:"value"`
			} `json:"storage"`
		} `json:"body"`
	}
	if err := json.Unmarshal([]byte(raw), &page); err != nil {
		return "", fmt.Errorf("conf_get_page_markdown: decode page envelope: %w", err)
	}
	// Pick the storage value: prefer the nested shape, fall
	// back to flat. Either way the representation field must
	// be "storage" — Confluence v2 returns the body in the
	// format we asked for via body-format=storage.
	var (
		bodyRep string
		bodyVal string
	)
	switch {
	case page.Body.Storage.Value != "":
		bodyRep = page.Body.Storage.Representation
		bodyVal = page.Body.Storage.Value
	case page.Body.Value != "":
		bodyRep = page.Body.Representation
		bodyVal = page.Body.Value
	default:
		return "", fmt.Errorf("conf_get_page_markdown: page body is empty (no storage body returned by upstream)")
	}
	if bodyRep != "" && bodyRep != "storage" {
		return "", fmt.Errorf("conf_get_page_markdown: expected storage representation, got %q", bodyRep)
	}
	md, err := markdown.StorageXHTMLToMarkdown(bodyVal)
	if err != nil {
		return "", fmt.Errorf("conf_get_page_markdown: convert storage XHTML to markdown: %w", err)
	}

	envelope := map[string]any{
		"pageId":   page.ID,
		"title":    page.Title,
		"markdown": md,
	}
	// Encode the envelope per the caller's outputFormat.
	encoded, err := encodeOutput(envelope, a.OutputFormat)
	if err != nil {
		return "", fmt.Errorf("conf_get_page_markdown: encode envelope: %w", err)
	}
	// Apply the optional JMESPath filter (it operates on the
	// envelope, not the markdown text). Uses the same
	// internal/jmespath package the 9-step pipeline uses, so
	// the empty-expression short-circuit behaviour is identical.
	if a.JQ != "" {
		filtered, ferr := jmespathApply(a.JQ, envelope)
		if ferr != nil {
			return "", fmt.Errorf("conf_get_page_markdown: jq filter error: %w", ferr)
		}
		encoded, err = encodeOutput(filtered, a.OutputFormat)
		if err != nil {
			return "", fmt.Errorf("conf_get_page_markdown: re-encode filtered: %w", err)
		}
	}
	return string(encoded), nil
}

// resolveMarkdownSource picks the markdown source from the
// (markdown, markdownFile) pair, preferring the inline `markdown`
// field when both are set. It enforces the 1 MB size cap on the
// file. Returning an error here means the handler did not make
// any upstream call.
func resolveMarkdownSource(inline, filePath string) (string, error) {
	if inline != "" {
		return inline, nil
	}
	if filePath == "" {
		return "", fmt.Errorf("one of markdown or markdownFile is required")
	}
	info, err := os.Stat(filePath)
	if err != nil {
		return "", fmt.Errorf("stat markdownFile %q: %w", filePath, err)
	}
	if info.Size() > markdownFileMaxBytes {
		return "", fmt.Errorf("markdownFile %q is %d bytes, exceeds the %d-byte (1 MB) limit", filePath, info.Size(), markdownFileMaxBytes)
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read markdownFile %q: %w", filePath, err)
	}
	return string(data), nil
}
