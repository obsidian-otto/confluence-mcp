// Package tools — drawio_handler.go: handler for the v3
// `conf_upload_drawio` tool. Orchestrates the draw.io app
// upload-AND-embed flow on Confluence Cloud, mirroring what
// happens when a user drag-drops a `.drawio` file onto a
// page in the web UI:
//
//  1. Upload the drawio source file (raw .drawio XML, or
//     a pre-prepared .drawio.png / .drawio.svg) as a v1
//     multipart attachment with the extension-derived
//     Content-Type (application/vnd.jgraph.mxfile for
//     .drawio).
//  2. Create the `ac:com.mxgraph.confluence.plugins.diagramly:
//     drawio-diagram` custom-content entity on the page
//     whose body carries the drawio metadata JSON. The
//     draw.io app finds the diagram via this entity.
//  3. PUT the page body with the
//     <ac:structured-macro ac:name="inc-drawio"> macro
//     that references the custom-content entity by
//     custContentId. The draw.io app's read-view renderer
//     hooks into this macro: it loads the diagram from
//     the custom-content entity and renders the
//     "Edit this diagram" affordance. Without the macro,
//     the diagram doesn't render inline even though the
//     entity exists (verified live 2026-07-13 on the
//     user's smartergroup.atlassian.net site: the
//     entity appears in the drawio Diagrams list, but
//     the page stays blank until the macro is added).
//
// The inc-drawio macro envelope mirrors what the draw.io
// app writes when the user drag-drops a .drawio file (or
// embeds a new drawio diagram). The draw.io app's "Edit
// this diagram" affordance triggers on this macro's
// presence.
//
// All variable slots are HTML-escaped so a malicious
// filename cannot break the page body.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bennie/mcp-confluence/internal/atlassian"
	"github.com/bennie/mcp-confluence/internal/jmespath"
)

// HandleUploadDrawio is the `conf_upload_drawio` handler. The
// flow:
//
//  1. Validate args: exactly one of {pageId} or {spaceId+title};
//     exactly one of {drawioFile}, {drawioPngFile}, or
//     {drawioSvgFile}; non-empty file path; non-empty diagramName
//     (or one derivable from the file basename).
//  2. Stage the source attachment under its final drawio
//     filename. Standalone .drawio input uploads byte-for-byte
//     under a .drawio filename; pre-prepared .drawio.png /
//     .drawio.svg input uploads verbatim under its existing
//     filename. atlassian.Client.UploadAttachment sets the
//     file part's Content-Type from the filename extension, so
//     .drawio uploads land with application/vnd.jgraph.mxfile.
//  3. Create the page (new-page path only) with an empty
//     body so the draw.io app can insert its own macros on
//     first edit.
//  4. Upload the source attachment via the v1 multipart
//     endpoint with X-Atlassian-Token: no-check.
//  5. Create the `ac:com.mxgraph.confluence.plugins.diagramly:
//     drawio-diagram` custom-content entity on the page
//     whose body is the drawio metadata JSON. The draw.io
//     app finds the diagram via this entity. CRITICAL
//     encoding: the body.value must be URL-encoded JSON
//     (the draw.io app's body parser does
//     decodeURIComponent(value) first, then JSON.parse).
//  6. PUT the page body with the
//     <ac:structured-macro ac:name="inc-drawio"> macro
//     that references the custom-content entity by
//     custContentId. The macro is the trigger that causes
//     the draw.io app to render the diagram inline and
//     offer the "Edit this diagram" affordance. Without
//     the macro, the page stays blank even though the
//     entity and attachment exist (verified live 2026-07-13).
//  7. Return a small envelope {attachmentId, attachmentTitle,
//     attachmentVersion, customContentId, customContentVersion,
//     diagramName, page: {id, title, version}} to the caller.
//
// On any failure after a successful upload (step 4) but
// before a successful macro PUT (step 6), the attachment is
// orphaned on the page. The caller can recover with
// conf_delete_attachment.
func HandleUploadDrawio(ctx context.Context, client *atlassian.Client, args json.RawMessage) (string, error) {
	var a UploadDrawioArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("conf_upload_drawio: decode args: %w", err)
	}

	// --- Step 1: validate args ---
	hasPageID := a.PageId != ""
	hasSpaceAndTitle := a.SpaceId != "" && a.Title != ""
	if hasPageID == hasSpaceAndTitle {
		return "", fmt.Errorf("conf_upload_drawio: provide exactly one of pageId (existing page) or spaceId+title (new page)")
	}
	hasDrawioFile := a.DrawioFile != ""
	hasDrawioPngFile := a.DrawioPngFile != ""
	hasDrawioSvgFile := a.DrawioSvgFile != ""
	srcCount := 0
	if hasDrawioFile {
		srcCount++
	}
	if hasDrawioPngFile {
		srcCount++
	}
	if hasDrawioSvgFile {
		srcCount++
	}
	if srcCount != 1 {
		return "", fmt.Errorf("conf_upload_drawio: provide exactly one of drawioFile, drawioPngFile, or drawioSvgFile (got %d)", srcCount)
	}

	inputPath := a.DrawioFile
	if inputPath == "" {
		inputPath = a.DrawioPngFile
	}
	if inputPath == "" {
		inputPath = a.DrawioSvgFile
	}

	// Derive the diagramDisplayName from the file basename if
	// not supplied.
	diagramName := a.DiagramDisplayName
	if diagramName == "" {
		base := filepath.Base(inputPath)
		for _, ext := range []string{".drawio.svg", ".drawio.png", ".drawio", ".svg", ".png"} {
			if strings.HasSuffix(base, ext) {
				diagramName = base[:len(base)-len(ext)]
				break
			}
		}
		if diagramName == "" {
			diagramName = base
		}
	}

	// --- Step 2: resolve the exact source attachment filename ---
	uploadFilename := filepath.Base(inputPath)
	if hasDrawioFile {
		uploadFilename = diagramName + ".drawio"
	} else if hasDrawioPngFile {
		uploadFilename = diagramName + ".drawio.png"
	} else if hasDrawioSvgFile {
		uploadFilename = diagramName + ".drawio.svg"
	}

	// --- Step 3: create the page (new-page path only) ---
	var pageID string
	var initialPage *pageEnvelope
	if hasSpaceAndTitle {
		env, err := createPageWithEmptyBody(ctx, client, a.SpaceId, a.Title)
		if err != nil {
			return "", fmt.Errorf("conf_upload_drawio: create page: %w", err)
		}
		initialPage = env
		pageID = env.ID
	} else {
		pageID = a.PageId
	}

	// --- Step 4: stage + upload the source attachment ---
	tmpDir, err := os.MkdirTemp("", "mcp-confluence-drawio-*")
	if err != nil {
		return "", fmt.Errorf("conf_upload_drawio: create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	stagedPath := filepath.Join(tmpDir, uploadFilename)
	if err := os.Link(inputPath, stagedPath); err != nil {
		buf, rerr := os.ReadFile(inputPath)
		if rerr != nil {
			return "", fmt.Errorf("conf_upload_drawio: read %q: %w", inputPath, rerr)
		}
		if werr := os.WriteFile(stagedPath, buf, 0o644); werr != nil {
			return "", fmt.Errorf("conf_upload_drawio: stage %q: %w", stagedPath, werr)
		}
	}

	respBody, _, err := client.UploadAttachment(ctx, pageID, stagedPath, a.Comment, false)
	if err != nil {
		if initialPage != nil {
			_ = deletePage(ctx, client, pageID)
		}
		return "", fmt.Errorf("conf_upload_drawio: upload attachment: %w", err)
	}

	attachment, err := parseAttachmentFromUploadResponse(respBody)
	if err != nil {
		return "", fmt.Errorf("conf_upload_drawio: parse upload response: %w; raw body: %s", err, string(respBody))
	}

	// --- Step 5: create the draw.io custom-content entity ---
	customContent, cerr := createDrawioCustomContent(ctx, client, pageID, uploadFilename, attachment.Version.Number)
	if cerr != nil {
		return "", fmt.Errorf("conf_upload_drawio: create custom-content entity: %w (attachment %q is orphaned on page %s)", cerr, attachment.ID, pageID)
	}

	// --- Step 6: PUT the page body with the inc-drawio macro ---
	//
	// The draw.io app's read-view renderer hooks into the
	// <ac:structured-macro ac:name="inc-drawio"> macro. The
	// macro references the custom-content entity by
	// custContentId; the draw.io app loads the diagram from
	// the entity body and renders inline. This is the
	// load-bearing step that makes the diagram visible
	// (verified live 2026-07-13 on the user's site: an
	// entity + attachment without a body macro = blank
	// page, even though the entity is in the drawio Diagrams
	// list).
	storageValue := buildIncDrawioMacroXHTML(customContent, attachment, pageID, client.BaseURL)

	var finalPage *pageEnvelope
	if initialPage != nil {
		// Newly-created page: PUT the body with the
		// version number from the create response.
		updated, perr := updatePageBody(ctx, client, pageID, storageValue, a.Title, "", initialPage.Version.Number+1)
		if perr != nil {
			return "", fmt.Errorf("conf_upload_drawio: set page body: %w (attachment %q is orphaned on page %s)", perr, attachment.ID, pageID)
		}
		finalPage = updated
	} else {
		// Existing page: fetch the current version+title
		// so we can PUT with the correct version+1 (v2 PUT
		// requires a non-empty title).
		currentTitle, currentVersion, ferr := fetchPage(ctx, client, pageID)
		if ferr != nil {
			return "", fmt.Errorf("conf_upload_drawio: fetch current page: %w (attachment %q is on page %s but body was not updated)", ferr, attachment.ID, pageID)
		}
		updated, uerr := updatePageBody(ctx, client, pageID, storageValue, "", currentTitle, currentVersion+1)
		if uerr != nil {
			return "", fmt.Errorf("conf_upload_drawio: update page body: %w (attachment %q is orphaned on page %s)", uerr, attachment.ID, pageID)
		}
		finalPage = updated
	}

	// --- Step 7: build the response envelope ---
	envelope := map[string]any{
		"attachmentId":         attachment.ID,
		"attachmentTitle":      attachment.Title,
		"attachmentVersion":    attachment.Version.Number,
		"customContentId":      customContent.ID,
		"customContentVersion": customContent.Version.Number,
		"diagramName":          uploadFilename,
		"page": map[string]any{
			"id":      finalPage.ID,
			"title":   finalPage.Title,
			"version": finalPage.Version.Number,
		},
	}

	data := any(envelope)
	if a.JQ != "" {
		filtered, ferr := jmespath.Apply(a.JQ, data)
		if ferr != nil {
			return "", fmt.Errorf("conf_upload_drawio: jq filter error: %v", ferr)
		}
		data = filtered
	}

	encoded, eerr := encodeOutput(data, a.OutputFormat)
	if eerr != nil {
		return "", fmt.Errorf("conf_upload_drawio: encode error: %v", eerr)
	}

	final, terr := truncateForAI(encoded, "PUT", "/wiki/api/v2/pages/"+pageID)
	if terr != nil {
		_, _ = fmt.Fprintf(stderrForDrawio(),
			"tools: failed to persist full response: %v\n", terr)
	}
	return final, nil
}

// stderrForDrawio returns os.Stderr for the truncation-error log
// path. Kept private + inline so the truncation path mirrors
// the existing handlers without a new helper exposed at
// package scope.
func stderrForDrawio() interface {
	Write(p []byte) (n int, err error)
} {
	return os.Stderr
}

// pageEnvelope is the minimal subset of the v2 page envelope
// we care about for the drawio flow: id, title, version.
//
// The v2 page envelope has a nested version object
// ({"number": 1, "message": "", "minorEdit": false, ...}) rather
// than a flat int. Live smoke test on 2026-07-10 caught this —
// the original handler assumed version was an int.
type pageEnvelope struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Version struct {
		Number int `json:"number"`
	} `json:"version"`
}

// attachmentEnvelope is the minimal subset of the v1 attachment
// envelope (inside ContentPageScheme.Results[i]) that we care
// about for the drawio flow: id, title, version number.
//
// The v1 ContentPageScheme envelope has a nested version object
// ({"by": ..., "when": ..., "number": 1, "minorEdit": false, ...})
// rather than a flat int. Live smoke test on 2026-07-10 caught
// this — the original handler assumed version was an int.
type attachmentEnvelope struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Version struct {
		Number int `json:"number"`
	} `json:"version"`
}

// customContentEnvelope is the minimal subset of the v2
// custom-content envelope we care about for the drawio flow:
// id, version.
type customContentEnvelope struct {
	ID      string `json:"id"`
	Version struct {
		Number int `json:"number"`
	} `json:"version"`
}

// parseAttachmentFromUploadResponse pulls the first attachment
// out of a v1 ContentPageScheme JSON envelope. Returns an
// error if the body is not valid JSON, has no results array,
// or the first result has no id.
func parseAttachmentFromUploadResponse(body []byte) (*attachmentEnvelope, error) {
	var page struct {
		Results []attachmentEnvelope `json:"results"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if len(page.Results) == 0 {
		return nil, fmt.Errorf("no results in response")
	}
	first := page.Results[0]
	if first.ID == "" {
		return nil, fmt.Errorf("first result has no id")
	}
	return &first, nil
}

// createPageWithEmptyBody creates a new page with an empty
// storage body. The draw.io app inserts its own macros on
// first edit, so a placeholder paragraph would just be noise.
// Returns the new page's envelope. Caller is expected to add
// the diagram (upload + custom-content entity) shortly after.
func createPageWithEmptyBody(ctx context.Context, client *atlassian.Client, spaceID, title string) (*pageEnvelope, error) {
	body := map[string]any{
		"spaceId": spaceID,
		"status":  "current",
		"title":   title,
		"body": map[string]any{
			"representation": "storage",
			"value":          "",
		},
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode create-page body: %w", err)
	}

	respBody, err := client.Call(ctx, "POST", "/wiki/api/v2/pages", nil, bodyBytes)
	if err != nil {
		return nil, err
	}

	var env pageEnvelope
	if err := jsonFromMap(respBody, &env); err != nil {
		return nil, fmt.Errorf("decode create-page response: %w", err)
	}
	if env.ID == "" {
		return nil, fmt.Errorf("create-page response missing id")
	}
	return &env, nil
}

// deletePage removes a page by id (used to roll back a failed
// upload that left an orphan empty page on the user's space).
// v2 DELETE is idempotent — no body or version required.
func deletePage(ctx context.Context, client *atlassian.Client, pageID string) error {
	path := "/wiki/api/v2/pages/" + pageID
	_, err := client.Call(ctx, "DELETE", path, nil, nil)
	return err
}

// updatePageBody PUTs a new body to /wiki/api/v2/pages/{id}
// with the supplied storage XHTML. The caller must pass the
// next version number (current+1 for existing pages, or the
// create response's version+1 for newly-created pages). If
// titleOverride is empty, the caller MUST also pass
// currentTitle — the v2 API rejects a PUT body with no
// title unless status=DRAFT.
func updatePageBody(ctx context.Context, client *atlassian.Client, pageID, storageValue, titleOverride, currentTitle string, version int) (*pageEnvelope, error) {
	body := map[string]any{
		"id":     pageID,
		"status": "current",
		"body": map[string]any{
			"representation": "storage",
			"value":          storageValue,
		},
		"version": map[string]any{
			"number": version,
		},
	}
	title := titleOverride
	if title == "" {
		title = currentTitle
	}
	if title == "" {
		return nil, fmt.Errorf("update page body: title is required for PUT (caller must supply titleOverride or currentTitle)")
	}
	body["title"] = title
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode update-page body: %w", err)
	}

	path := "/wiki/api/v2/pages/" + pageID
	respBody, err := client.Call(ctx, "PUT", path, nil, bodyBytes)
	if err != nil {
		return nil, err
	}

	var env pageEnvelope
	if err := jsonFromMap(respBody, &env); err != nil {
		return nil, fmt.Errorf("decode update-page response: %w", err)
	}
	return &env, nil
}

// fetchPage returns the current title and version number of an
// existing page so the caller can compute version+1 and supply
// the title for the PUT body.
func fetchPage(ctx context.Context, client *atlassian.Client, pageID string) (title string, version int, err error) {
	path := "/wiki/api/v2/pages/" + pageID
	respBody, err := client.Call(ctx, "GET", path, nil, nil)
	if err != nil {
		return "", 0, err
	}

	var env pageEnvelope
	if err := jsonFromMap(respBody, &env); err != nil {
		return "", 0, fmt.Errorf("decode fetch-page response: %w", err)
	}
	if env.Version.Number == 0 {
		return "", 0, fmt.Errorf("fetch-page response has version=0")
	}
	return env.Title, env.Version.Number, nil
}

// createDrawioCustomContent creates the
// `ac:com.mxgraph.confluence.plugins.diagramly:drawio-diagram`
// custom-content entity on the page. The draw.io app finds
// the diagram via this entity.
//
// The entity body is the drawio metadata JSON the app
// expects:
//
//	{"pageId": "<pageId>", "type": "page", "diagramName":
//	 "<filename>.drawio", "version": 1, "inComment": false,
//	 "comments": [], "isSketch": 0}
//
// CRITICAL encoding detail: the body.value field must be
// URL-encoded JSON, not raw JSON. The draw.io app's body
// parser calls `decodeURIComponent(value)` first, then
// `JSON.parse(decoded)`. Verified live 2026-07-13 on the
// user's site: the working arch entity has body.value =
// "%7B%22pageId%22%3A%221829666817%22%2C..." (URL-encoded);
// a body of raw JSON doesn't appear in the user's drawio
// diagram list.
func createDrawioCustomContent(ctx context.Context, client *atlassian.Client, pageID, diagramName string, attachmentVersion int) (*customContentEnvelope, error) {
	rawJSON := fmt.Sprintf(
		`{"pageId":%q,"type":"page","diagramName":%q,"version":%d,"inComment":false,"comments":[],"isSketch":0}`,
		pageID, diagramName, attachmentVersion,
	)
	bodyValue := url.QueryEscape(rawJSON)
	body := map[string]any{
		"type":   "ac:com.mxgraph.confluence.plugins.diagramly:drawio-diagram",
		"title":  diagramName,
		"pageId": pageID,
		"body": map[string]any{
			"representation": "storage",
			"value":          bodyValue,
		},
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode custom-content body: %w", err)
	}
	respBody, err := client.Call(ctx, "POST", "/wiki/api/v2/custom-content", nil, bodyBytes)
	if err != nil {
		return nil, err
	}
	var env customContentEnvelope
	if err := jsonFromMap(respBody, &env); err != nil {
		return nil, fmt.Errorf("decode custom-content response: %w", err)
	}
	if env.ID == "" {
		return nil, fmt.Errorf("custom-content response missing id")
	}
	return &env, nil
}

// buildIncDrawioMacroXHTML composes the Confluence storage
// XHTML for the draw.io app's "inc-drawio" macro. The macro
// is the read-view renderer hook — the draw.io app's renderer
// fires on this macro and loads the diagram from the
// custom-content entity. The macro's custContentId parameter
// points at the entity.
//
// All variable slots are HTML-escaped so a malicious filename
// cannot break the page body.
func buildIncDrawioMacroXHTML(customContent *customContentEnvelope, attachment *attachmentEnvelope, pageID, baseURL string) string {
	// Use stable IDs so re-running this tool against the
	// same page doesn't accumulate duplicate macros — PUT
	// overwrites the entire body, so the IDs stabilise the
	// editor's diff view across runs.
	const macroID = "drawio-mcp-confluence"
	escapedPageID := html.EscapeString(pageID)
	escapedCustomContentID := html.EscapeString(customContent.ID)
	escapedAttachmentTitle := html.EscapeString(attachment.Title)
	escapedBase := html.EscapeString(baseURL)
	return fmt.Sprintf(
		`<ac:structured-macro ac:name="inc-drawio" ac:schema-version="1" data-layout="default" ac:macro-id="%s">`+
			`<ac:parameter ac:name="pageId">%s</ac:parameter>`+
			`<ac:parameter ac:name="custContentId">%s</ac:parameter>`+
			`<ac:parameter ac:name="diagramDisplayName">%s</ac:parameter>`+
			`<ac:parameter ac:name="revision">%s</ac:parameter>`+
			`<ac:parameter ac:name="baseUrl">%s</ac:parameter>`+
			`<ac:parameter ac:name="diagramName">%s</ac:parameter>`+
			`<ac:parameter ac:name="imgPageId">%s</ac:parameter>`+
			`<ac:parameter ac:name="width">1500</ac:parameter>`+
			`<ac:parameter ac:name="height">990</ac:parameter>`+
			`<ac:parameter ac:name="simple">0</ac:parameter>`+
			`<ac:parameter ac:name="zoom">1</ac:parameter>`+
			`<ac:parameter ac:name="lbox">1</ac:parameter>`+
			`<ac:parameter ac:name="hiResPreview">0</ac:parameter>`+
			`<ac:parameter ac:name="pCenter">0</ac:parameter>`+
			`<ac:parameter ac:name="links">auto</ac:parameter>`+
			`<ac:parameter ac:name="tbstyle">top</ac:parameter>`+
			`<ac:parameter ac:name="includedDiagram">1</ac:parameter>`+
			`</ac:structured-macro>`,
		macroID,
		escapedPageID,
		escapedCustomContentID,
		escapedAttachmentTitle,
		strconv.Itoa(customContent.Version.Number),
		escapedBase,
		escapedAttachmentTitle,
		escapedPageID,
	)
}

// jsonFromMap marshals a map[string]any and unmarshals it
// into the target struct. Used when atlassian.Client.Call
// has already decoded the response into map[string]any —
// we need to convert it into the typed struct for field
// access. The marshal+unmarshal round-trip is wasteful but
// tiny (the v2 page envelope is <1 KB).
func jsonFromMap(m map[string]any, target any) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, target)
}
