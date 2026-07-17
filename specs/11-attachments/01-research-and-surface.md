# specs/11-attachments/01-research-and-surface.md

## Overview

The user requested (2026-07-10) raw-binary attachment tools for the
mcp-confluence server so that PNG, PDF, and drawio files can be
uploaded to a Confluence page directly. The goal of this spec is to
document (a) the actual Confluence REST surface for attachments and
(b) the proposed mcp-confluence tool surface that exposes it.

## Sources

1. Confluence Cloud REST API v2 — Attachment group
   `https://developer.atlassian.com/cloud/confluence/rest/v2/api-group-attachment/`
   (fetched 2026-07-10).
2. Atlassian Support KB — Using the Confluence REST API to upload an
   attachment to one or more pages
   `https://support.atlassian.com/confluence/kb/using-the-confluence-rest-api-to-upload-an-attachment-to-one-or-more-pages/`
   (fetched 2026-07-10; updated 2026-05-27 per the page footer).
3. go-atlassian v1.6.1 attachment service implementation
   `~/go/pkg/mod/github.com/ctreminiom/go-atlassian@v1.6.1/confluence/internal/attachment_content_impl.go`
   (read locally 2026-07-10).
4. Atlassian Community thread "Upload Attachment via API v2"
   `https://community.atlassian.com/forums/Confluence-questions/Upload-Attachment-via-API-v2/qaq-p/2352854`
   (fetched 2026-07-10; confirms no v2 upload endpoint exists).
5. drawio integration docs
   `https://www2.drawio.com/doc/faq/recover-moved-diagram-confluence-cloud`
   (fetched 2026-07-10; confirms the drawio macro is
   `<ac:structured-macro ac:name="drawio">` referencing the
   attachment filename).

## Spec

### Key research finding — v2 has no upload endpoint

The Confluence Cloud REST API v2 exposes 8 attachment operations,
all read-only or destructive:

  - `GET    /wiki/api/v2/attachments`
  - `GET    /wiki/api/v2/attachments/{id}`
  - `DELETE /wiki/api/v2/attachments/{id}`
  - `GET    /wiki/api/v2/blogposts/{id}/attachments`
  - `GET    /wiki/api/v2/custom-content/{id}/attachments`
  - `GET    /wiki/api/v2/labels/{id}/attachments`
  - `GET    /wiki/api/v2/pages/{id}/attachments`
  - `GET    /wiki/api/v2/attachments/{id}/thumbnail/download`

There is **no `POST` to `/wiki/api/v2/.../attachments`** for
uploads. The go-atlassian v2 `Attachment` service confirms this —
it exposes only `Get`, `Gets`, and `Delete` methods.

**All attachment uploads still go through the v1 endpoint:**

  ```
  POST /wiki/rest/api/content/{pageId}/child/attachment
  Content-Type: multipart/form-data
  X-Atlassian-Token: no-check      ← required to bypass CSRF check
  ```

The form body carries the file as `file=@<path>` plus optional
fields (`comment`, `minorEdit`). This was the case in 2024 and
remains the case as of the 2026-05-27 KB update.

### Library support

`go-atlassian@v1.6.1` exposes a typed `ContentAttachmentService`
on the **v1** confluence client (note: v2 wraps v1 — see
`confluence/v2/api_client_impl.go:44` — but the v2-typed
`Attachment` field on the v2 client only wraps the v2
read/delete methods, not the v1 upload).

The v1 `Create()` method:

  - signature: `Create(ctx, attachmentID, status, fileName, file io.Reader)`
  - sends `POST wiki/rest/api/content/{id}/child/attachment`
  - sets multipart/form-data correctly
  - **sets `X-Atlassian-Token: no-check` automatically** when
    `NewRequest` is called with a non-empty `type_` (Content-Type)
    parameter, which the upload path always does
  - the multipart body is built with `mime/multipart`, so binary
    streams (PDF, PNG, drawio XML) round-trip correctly without
    base64 encoding

The existing `internal/atlassian.Client` wrapper stores the
underlying `*confluence.Client` (both v1 and v2) as
`c.native` (private field). It does **not** expose
`native.Attachment` directly, so a new wrapper method on
`internal/atlassian.Client` is needed.

### Authentication / scope

The v1 attachment endpoint requires one of:

  - **Basic auth** (email + API token) — what this server already
    uses for every other call. `write:attachment:confluence` scope
    on OAuth.
  - The same `Authorization: Basic base64(email:token)` header
    that `Client.Do` and `Client.Call` already apply.

No change to the existing auth flow.

### File type support

The v1 endpoint accepts any binary blob — the file type is
recorded as `mediaType` on the returned `ContentPageScheme`.
PNG, PDF, drawio, JPEG, SVG, DOCX, XLSX, MP4, ZIP all work.

The drawio file viewer is a Confluence plugin (drawio for
Confluence / draw.io). After upload, the diagram is rendered
by inserting `<ac:structured-macro ac:name="drawio">` in the
page body referencing the attachment by filename. The upload
itself is format-agnostic — the viewer macro is a separate
concern, owned by the page body, not the attachment.

### Limitations of the v1 endpoint

  - **100 MB per-file cap** (Atlassian Cloud hard limit; site
    admins can lower it under _General Configuration → Attachment
    Maximum Size_). Calls over the cap return `413 Payload Too
    Large`.
  - **Single page at a time** — the endpoint is `/content/{id}/child/attachment`,
    not `/content/multi/...`. Bulk upload requires a loop.
  - **`X-Atlassian-Token: no-check` is mandatory** — without it
    the server returns `403 Forbidden` due to CSRF protection.
    go-atlassian sets this automatically when called via
    `Create()` / `CreateOrUpdate()`.

## Proposed tool surface

Add **3 new tools** to bring the total from 13 to 16.

### 1. `conf_upload_attachment`

Uploads a single binary file as an attachment to a page.

  - **args:**
      - `pageId` (string, required) — numeric page id
      - `filePath` (string, required) — absolute path to the
        file on disk. The handler opens the file with `os.Open`,
        streams it via `io.Reader`, and does NOT base64-encode
        (this preserves the binary round-trip — base64-encoding
        would inflate size ~33% and is only required for JSON
        payloads, not for multipart/form-data)
      - `comment` (string, optional) — attachment comment /
        changelog message (default: empty)
      - `minorEdit` (bool, optional) — mark as minor edit
        (default: `true` to match go-atlassian's default)
  - **handler:** builds the multipart body via
    `mime/multipart.Writer`, calls `internal/atlassian.Client.UploadAttachment`
      which delegates to `client.native.Attachment.Create()` (v1)
  - **returns:** the standard v1 attachment envelope:
    `{ "results": [{ "id": "...", "title": "...", "mediaType": "...",
      "fileSize": ..., "downloadLink": "...", "webuiLink": "...",
      "version": { "number": ... } }] }`
    TOON-encoded by default.

### 2. `conf_list_attachments`

Lists the attachments on a page.

  - **args:**
      - `pageId` (string, required)
      - `cursor` (string, optional)
      - `limit` (int, optional, default 25, max 100 — the v2
        endpoint caps at 100)
      - `mediaType` (string, optional) — substring filter
        (e.g. `"image"` to find PNG/JPEG)
      - `filename` (string, optional) — exact filename match
      - `jq` (string, optional)
      - `outputFormat` ("toon" | "json", default "toon")
  - **handler:** GET
    `/wiki/api/v2/pages/{id}/attachments?limit=...&cursor=...`
    via the v2 raw path (this endpoint IS in v2 — only the
    upload is v1-only). Same 9-step pipeline as `conf_get`.
  - **returns:** `MultiEntityResult<Attachment>` envelope.

### 3. `conf_delete_attachment`

Deletes an attachment by id.

  - **args:**
      - `attachmentId` (string, required)
      - `purge` (bool, optional, default `false`) — set to
        `true` to permanently delete (skip trash)
      - `jq` (string, optional)
      - `outputFormat` (string, optional)
  - **handler:** DELETE `/wiki/api/v2/attachments/{id}` (or
    `?purge=true` when purging) via the v2 raw path.
  - **returns:** empty body on 204; the standard error envelope
    on failure.

### Why upload is v1 and the others are v2

This is the only way the surface is consistent — there's no v2
upload, so we hit v1 for the write path and v2 for the read/delete
paths. The mix is documented in the tool descriptions (each tool's
"Wire shape:" section will name the actual endpoint and the API
version).

### Total tool count after this change

  - 5 CRUD — unchanged
  - 5 convenience — unchanged
  - 3 markdown — unchanged
  - 3 attachments (NEW) — upload, list, delete
  - **= 16 tools**

### Drawio-specific concerns

drawio upload uses `conf_upload_attachment` unchanged. After
upload, the page body needs a `<ac:structured-macro ac:name="drawio">`
block referencing the attachment by filename. That body edit
goes through `conf_put_markdown` with markdown like:

  ```markdown
  {{drawio(my-diagram.drawio)}}
  ```

or, for raw XHTML, `conf_put` with hand-built storage XHTML:

  ```xml
  <ac:structured-macro ac:name="drawio">
    <ac:parameter ac:name="filename">my-diagram.drawio</ac:parameter>
  </ac:structured-macro>
  ```

The markdown renderer (`internal/markdown.MarkdownToStorageXHTML`)
already converts raw `<ac:structured-macro>` blocks — it does
not strip unknown macro names. So a markdown "drawio" macro
template will work as a follow-up edit after `conf_upload_attachment`.

If the user wants this seamless, a 4th tool `conf_upload_drawio`
that uploads AND inserts the macro in one call is a candidate
for v3. Out of scope for this spec.

## Verification

### Library surface (already verified locally)

```
$ grep -nE "^func " .../confluence/internal/attachment_content_impl.go
18: NewContentAttachmentService
36: (a *ContentAttachmentService) Gets
49: (a *ContentAttachmentService) CreateOrUpdate
62: (a *ContentAttachmentService) Create
```

```
$ grep -nE "X-Atlassian-Token" .../confluence/api_client_impl.go
96: req.Header.Set("X-Atlassian-Token", "no-check")
```

### End-to-end test plan (post-implementation)

1. `make check` — green (vet + lint + tests).
2. `make build` — binary produced.
3. Restart Hermes so the new binary loads.
4. `mcp__confluence__conf_help topic=all` reports "All 16 tools".
5. Live test against the user's own Confluence Cloud workspace:
    a. Create a sandbox page via `conf_post_markdown`.
    b. Call `conf_upload_attachment` with a small test PNG.
    c. Call `conf_list_attachments` to verify the upload.
    d. Call `conf_delete_attachment` to clean up.
6. Commit + auto-push per the gitter skill.

### C1 caveat — V2 API surface drift

The Confluence Cloud v2 API is GA and the 8 attachment endpoints
listed above are documented in the current developer.atlassian.com
page. Atlassian has not announced a v2 upload endpoint (the
community thread was last active in 2024). Treat this spec's
v1-upload assumption as accurate until Atlassian publishes a
v2 upload equivalent.