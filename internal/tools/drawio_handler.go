// Package tools — drawio_handler.go: handler for the v3
// `conf_upload_drawio` tool. Orchestrates the two-step flow:
// (1) upload the drawio file via the v1 multipart endpoint,
// (2) edit the page body to add the
//
//	<ac:structured-macro ac:name="drawio"> macro.
//
// See specs/12-drawio-attachments/01-research-and-surface.md for
// the full design rationale + the wire shape details. The
// helper that wraps a .drawio XML file into a .drawio.png lives
// in internal/drawio (not in this package) so it can be reused
// by other call sites (e.g. a future tool that uploads multiple
// drawio files in batch).
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bennie/mcp-confluence/internal/atlassian"
	"github.com/bennie/mcp-confluence/internal/drawio"
	"github.com/bennie/mcp-confluence/internal/jmespath"
)

// HandleUploadDrawio is the `conf_upload_drawio` handler. The
// flow:
//
//  1. Validate args: exactly one of {pageId} or {spaceId+title};
//     exactly one of {drawioFile} or {drawioPngFile}; non-empty
//     file path; non-empty diagramName (or one derivable from
//     the file basename).
//  2. Read the source file from disk. If `drawioFile` was
//     supplied, wrap the .drawio XML into a .drawio.png via
//     internal/drawio.WrapToPng. If `drawioPngFile` was
//     supplied, read the bytes verbatim.
//  3. Upload the PNG bytes to the v1 attachment endpoint via
//     atlassian.Client.UploadAttachment. This is the same path
//     conf_upload_attachment uses; we just call the lower-level
//     client method directly because we already have the bytes
//     in memory (no on-disk file to hand to the existing
//     handler).
//  4. Build the page body XHTML containing the drawio macro
//     envelope and either:
//     - PUT to /wiki/api/v2/pages/{pageId} if pageId was set,
//     OR
//     - POST to /wiki/api/v2/pages if spaceId+title was set
//     (create a new page with the diagram).
//  5. Return a small envelope {attachmentId, attachmentTitle,
//     attachmentVersion, diagramName, page: {id, title, version}}
//     to the caller for follow-up operations.
//
// On any failure after a successful upload (step 3) but before
// a successful page edit (step 4), the attachment is orphaned
// on the page. The caller can recover with
// conf_delete_attachment. We surface a clear error message that
// names the attachment id so the cleanup path is unambiguous.
func HandleUploadDrawio(ctx context.Context, client *atlassian.Client, args json.RawMessage) (string, error) {
	var a UploadDrawioArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("conf_upload_drawio: decode args: %w", err)
	}

	// --- Step 1: validate args ---
	hasPageID := a.PageId != ""
	hasSpaceAndTitle := a.SpaceId != "" && a.Title != ""
	if hasPageID == hasSpaceAndTitle {
		// both true (both set) or both false (neither set) —
		// both are errors.
		return "", fmt.Errorf("conf_upload_drawio: provide exactly one of pageId (existing page) or spaceId+title (new page)")
	}
	hasDrawioFile := a.DrawioFile != ""
	hasDrawioPngFile := a.DrawioPngFile != ""
	hasDrawioSvgFile := a.DrawioSvgFile != ""
	// Count how many are set — exactly one must be.
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

	// Resolve the input file path. Precedence:
	// drawioFile > drawioPngFile > drawioSvgFile (the mutex
	// check above already guarantees exactly one is set, so
	// this is just the canonical order).
	inputPath := a.DrawioFile
	uploadExtension := "drawio.png"
	if inputPath == "" {
		inputPath = a.DrawioPngFile
	}
	if inputPath == "" {
		inputPath = a.DrawioSvgFile
		uploadExtension = "drawio.svg"
	}

	// Derive the diagramDisplayName from the file basename if
	// not supplied. This becomes the `diagramName` parameter
	// in the macro, which is what Confluence uses to find the
	// attachment on the page.
	diagramName := a.DiagramDisplayName
	if diagramName == "" {
		base := filepath.Base(inputPath)
		// Strip any of the drawio file extensions. The
		// upload-filename suffix (.drawio.png or .drawio.svg)
		// is added back below as the wire attachment name.
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

	// Resolve width / height to the macro defaults.
	width := a.Width
	if width == 0 {
		width = 1151
	}
	height := a.Height
	if height == 0 {
		height = 911
	}

	// --- Step 2: read source file + build PNG bytes ---
	//
	// Three paths:
	//   - drawioFile: standalone .drawio XML. Wrap into .drawio.png.
	//   - drawioPngFile: already-prepared .drawio.png. Read verbatim.
	//   - drawioSvgFile: already-prepared .drawio.svg. Read verbatim.
	var pngBytes []byte
	switch {
	case hasDrawioFile:
		// Standalone .drawio XML — wrap into .drawio.png.
		// The wrapped file's on-the-wire filename is
		// "<diagramName>.drawio.png" so the drawio macro
		// finds it under a recognizable name.
		var err error
		pngBytes, err = drawio.WrapToPng(inputPath)
		if err != nil {
			return "", fmt.Errorf("conf_upload_drawio: wrap drawio XML to PNG: %w", err)
		}
	default:
		// Either .drawio.png or .drawio.svg — read verbatim.
		// The upload-filename suffix (chosen above based on
		// which input was set) makes the wire attachment
		// recognizable as a drawio file regardless of
		// extension. drawio's renderer picks it up by file
		// content (the embedded XML), not by extension.
		var err error
		pngBytes, err = os.ReadFile(inputPath)
		if err != nil {
			return "", fmt.Errorf("conf_upload_drawio: read %q: %w", inputPath, err)
		}
	}

	// --- Step 3: upload the PNG to the target page ---
	//
	// We need a target pageId for the upload step. If the
	// caller asked to create a new page (spaceId+title),
	// we need to create the page FIRST (with a placeholder
	// body), then upload to it. The clean order is:
	//   a) create empty page (if new)
	//   b) upload attachment to pageId
	//   c) PUT page body with the drawio macro
	//
	// This ordering avoids the chicken-and-egg of "the page
	// doesn't exist yet but the attachment has to live on
	// it". The placeholder body is overwritten in step (c).
	var pageID string
	var initialPage *pageEnvelope
	if hasSpaceAndTitle {
		env, err := createEmptyPage(ctx, client, a.SpaceId, a.Title)
		if err != nil {
			return "", fmt.Errorf("conf_upload_drawio: create page: %w", err)
		}
		initialPage = env
		pageID = env.ID
	} else {
		pageID = a.PageId
	}

	// Write the PNG to a temp file so we can hand a path to
	// atlassian.Client.UploadAttachment (which opens it via
	// os.Open + streams via io.Copy). The temp file is
	// removed in defer. We use the diagramName as the
	// filename so the attachment's basename on Confluence is
	// "<diagramName>.drawio.png" — the drawio macro's
	// diagramName parameter then resolves to it directly.
	tmpDir, err := os.MkdirTemp("", "mcp-confluence-drawio-*")
	if err != nil {
		return "", fmt.Errorf("conf_upload_drawio: create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	tmpFilePath := filepath.Join(tmpDir, diagramName+"."+uploadExtension)
	if err := os.WriteFile(tmpFilePath, pngBytes, 0o644); err != nil {
		return "", fmt.Errorf("conf_upload_drawio: write temp file: %w", err)
	}

	respBody, _, err := client.UploadAttachment(ctx, pageID, tmpFilePath, a.Comment, false)
	if err != nil {
		// If we just created the page and the upload
		// failed, clean up the orphan page so the caller
		// isn't left with a useless empty page on their
		// space.
		if initialPage != nil {
			_ = deletePage(ctx, client, pageID, initialPage.Version.Number)
		}
		return "", fmt.Errorf("conf_upload_drawio: upload attachment: %w", err)
	}

	// Parse the v1 ContentPageScheme envelope to extract
	// the created attachment's id + title + version.
	attachment, err := parseAttachmentFromUploadResponse(respBody)
	if err != nil {
		// Attachment uploaded but we couldn't parse the
		// response. The attachment IS on the page — surface
		// a clear error with the raw body so the operator
		// can investigate.
		return "", fmt.Errorf("conf_upload_drawio: parse upload response: %w; raw body: %s", err, string(respBody))
	}

	// --- Step 4: edit the page body to add the macro ---
	//
	// For an existing page: PUT with the macro body. We
	// overwrite the body entirely (PUT is full-replacement).
	// The caller is opting into this by invoking the tool —
	// their original body is gone. That's the same trade-off
	// as conf_put on a page.
	//
	// For a freshly-created page: PUT to set the body (the
	// placeholder is replaced). We need the version number
	// we got back from the create step.
	storageValue := buildDrawioMacroXHTML(diagramName, width, height, client.BaseURL)

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
		// Existing page: we need the current version
		// number. Fetch it, then PUT with version+1.
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

	// --- Step 5: build the response envelope ---
	envelope := map[string]any{
		"attachmentId":      attachment.ID,
		"attachmentTitle":   attachment.Title,
		"attachmentVersion": attachment.Version.Number,
		"diagramName":       diagramName,
		"page": map[string]any{
			"id":      finalPage.ID,
			"title":   finalPage.Title,
			"version": finalPage.Version.Number,
		},
	}

	// JMESPath filter.
	data := any(envelope)
	if a.JQ != "" {
		filtered, ferr := jmespath.Apply(a.JQ, data)
		if ferr != nil {
			return "", fmt.Errorf("conf_upload_drawio: jq filter error: %v", ferr)
		}
		data = filtered
	}

	// Encode (TOON default; JSON when requested).
	encoded, eerr := encodeOutput(data, a.OutputFormat)
	if eerr != nil {
		return "", fmt.Errorf("conf_upload_drawio: encode error: %v", eerr)
	}

	final, terr := truncateForAI(encoded, "POST", "/wiki/rest/api/content/"+pageID+"/child/attachment")
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

// createEmptyPage creates a page with a placeholder body
// ("<p>(placeholder)</p>") so the upload step has a valid
// pageId to attach to. Returns the new page's envelope. The
// caller is expected to overwrite the body via updatePageBody
// shortly after.
func createEmptyPage(ctx context.Context, client *atlassian.Client, spaceID, title string) (*pageEnvelope, error) {
	body := map[string]any{
		"spaceId": spaceID,
		"status":  "current",
		"title":   title,
		"body": map[string]any{
			"representation": "storage",
			"value":          "<p>(placeholder — body will be replaced)</p>",
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

// updatePageBody PUTs a new body to /wiki/api/v2/pages/{id}
// with the supplied storage XHTML. The caller must pass the
// next version number (current+1 for existing pages, or the
// create response's version+1 for newly-created pages). If
// titleOverride is empty, the caller MUST also pass
// currentTitle (the page's existing title) — the v2 API
// rejects a PUT body with no title unless status=DRAFT. We
// always emit a title in the body to keep the request simple.
//
// The reason for the title round-trip on existing pages:
// the v2 PUT endpoint requires either a non-empty title or
// a status of "draft". For our purposes we're always
// updating a "current" page, so we have to include the
// title. Fetching it once during the existing-page branch
// is the cleanest way to avoid the round-trip on every call.
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
	// titleOverride wins when the caller explicitly sets it
	// (new-page path); otherwise fall back to currentTitle
	// (existing-page path). If BOTH are empty, fail loudly
	// rather than send a malformed PUT.
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
// the title for the PUT body. The GET is via the v2 endpoint
// with no body-format (just metadata).
//
// Title is needed because the v2 PUT endpoint rejects a body
// with an empty title unless status=DRAFT — see updatePageBody
// below for the full rationale.
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

// deletePage removes a page by id (used to roll back a
// failed upload that left an orphan empty page on the user's
// space). The version argument is the version number we got
// back from the create step — Confluence requires it on PUT
// for the version-incrementing delete semantics.
//
// Wait — DELETE in v2 does not require a body or version.
// It's idempotent. So we just call it.
func deletePage(ctx context.Context, client *atlassian.Client, pageID string, _ int) error {
	path := "/wiki/api/v2/pages/" + pageID
	_, err := client.Call(ctx, "DELETE", path, nil, nil)
	return err
}

// buildDrawioMacroXHTML composes the Confluence storage XHTML
// for a drawio diagram macro. The minimal envelope (just
// diagramName + width + height) is enough for the macro to
// render the diagram; if the drawio marketplace app is
// installed, Confluence's editor will auto-fill the rich
// parameter set (contentId, pageId, revision, baseUrl) on
// next edit.
//
// The baseUrl parameter is included because drawio's renderer
// uses it to resolve cross-page references; the Confluence
// Cloud stock URL is https://<site>.atlassian.net/wiki.
func buildDrawioMacroXHTML(diagramName string, width, height int, baseURL string) string {
	// Use a stable macro-id so re-running this tool against
	// the same page doesn't accumulate duplicate macros —
	// PUT overwrites the entire body, so the macro-id
	// stabilises the editor's diff view across runs.
	const macroID = "drawio-mcp-confluence"
	return fmt.Sprintf(
		`<ac:structured-macro ac:name="drawio" ac:schema-version="1" `+
			`ac:macro-id="%s">`+
			`<ac:parameter ac:name="diagramName">%s</ac:parameter>`+
			`<ac:parameter ac:name="width">%s</ac:parameter>`+
			`<ac:parameter ac:name="height">%s</ac:parameter>`+
			`<ac:parameter ac:name="baseUrl">%s</ac:parameter>`+
			`</ac:structured-macro>`,
		macroID,
		diagramName,
		strconv.Itoa(width),
		strconv.Itoa(height),
		baseURL,
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
