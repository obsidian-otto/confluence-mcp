# specs/12-drawio-attachments/01-research-and-surface.md

## Overview

The user requested (2026-07-10) the ability to upload a drawio file
as a Confluence attachment AND have it render inline on the page as
an embedded diagram. The goal of this spec is to document (a) the
exact wire shape Confluence expects for an embedded drawio diagram,
(b) the two-mode design choice (editable vs. static PNG), and (c)
the proposed mcp-confluence tool that does the upload + page-body
edit in one call.

## Sources

1. draw.io — Recover a diagram that was moved to another page
   in Confluence Cloud
   `https://www2.drawio.com/doc/faq/recover-moved-diagram-confluence-cloud`
   (fetched 2026-07-10). Provides the canonical macro XHTML shape:
   `<ac:structured-macro ac:name="drawio" ac:schema-version="1"
   ac:macro-id="...">` with `<ac:parameter ac:name="diagramName">`
   binding to the attachment by filename.

2. draw.io — Embed and reuse diagrams
   `https://www.drawio.com/docs/integrations/atlassian/confluence/confluence-cloud-embed-diagram/`
   (fetched 2026-07-10). Documents the editor's interaction with
   the same macro and how the `diagramName` parameter resolves.

3. hunyadi/md2conf — Draw.io Diagrams
   `https://deepwiki.com/hunyadi/md2conf/8.3-draw.io-diagrams`
   (fetched 2026-07-10). The reference Python implementation's
   approach: upload the .drawio file as an attachment, then
   reference it from the page body. Two render modes — editable
   (needs marketplace app) vs. static PNG (needs drawio CLI).
   Full rationale for the `--render-drawio` / `--no-render-drawio`
   split.

4. hunyadi/md2conf source — render.py
   `https://github.com/hunyadi/md2conf/blob/a7649b17/md2conf/drawio/render.py`
   (fetched 2026-07-10). Reference PNG-with-embedded-XML
   extraction algorithm (decompress_diagram +
   extract_xml_from_png). Not used directly by our server but
   documents the file format conventions we need to validate
   against.

5. Atlassian Developer Community — Embed a draw.io diagram via
   API call
   `https://community.developer.atlassian.com/t/how-to-embed-a-draw-io-diagram-into-a-confluence-page-through-an-api-call/91839`
   (fetched 2026-07-10). Confirms the upload-attachment-first
   then edit-page-body flow (no single API call for "attach +
   embed" — it's a 2-step process).

## Spec

### How drawio embedding actually works in Confluence Cloud

A drawio diagram appears on a Confluence page via the
`<ac:structured-macro ac:name="drawio">` macro. The macro binds
to an attachment by its filename via the
`<ac:parameter ac:name="diagramName">` parameter. Confluence's
rendering depends on which marketplace app is installed:

  - **draw.io Diagrams for Confluence** (marketplace app, free)
    — full interactive editor, the macro accepts a rich parameter
    set (`contentId`, `pageId`, `revision`, `baseUrl`, `width`,
    `height`, `zoom`, `lbox`, `simple`, `inComment`, etc.). The
    macro envelope in this mode looks like:

    ```xml
    <ac:structured-macro ac:name="drawio" ac:schema-version="1"
                          ac:macro-id="41ddd3f1-9613-4f13-b0f1-9b24b68db2eb">
      <ac:parameter ac:name="contentId">678821920</ac:parameter>
      <ac:parameter ac:name="pageId">678854685</ac:parameter>
      <ac:parameter ac:name="diagramDisplayName">rulers-measurements.drawio</ac:parameter>
      <ac:parameter ac:name="revision">1</ac:parameter>
      <ac:parameter ac:name="baseUrl">https://drawio.atlassian.net/wiki</ac:parameter>
      <ac:parameter ac:name="diagramName">rulers-measurements.drawio</ac:parameter>
      <ac:parameter ac:name="width">1151</ac:parameter>
      <ac:parameter ac:name="height">911</ac:parameter>
    </ac:structured-macro>
    ```

  - **Static PNG (no marketplace app)** — the macro is the same
    `ac:name="drawio"` but the attachment must be a PNG with the
    drawio XML embedded in the `tEXt` chunk (keyword `mxfile`,
    URL-encoded, DEFLATE-compressed, base64-encoded inner XML).
    Confluence's stock renderer doesn't know how to display a
    raw `.drawio` file, so the file must be the PNG variant for
    non-marketplace installs.

Since this server cannot know whether the user's Confluence
instance has the drawio marketplace app, the tool MUST support
both modes — but the default is the static PNG (because it works
on any Confluence Cloud instance).

### The wire flow (2 steps, not 1)

Atlassian has no "attach + embed" single endpoint. The flow is:

  1. **Upload the attachment** — same v1
     `POST /wiki/rest/api/content/{pageId}/child/attachment`
     multipart endpoint that `conf_upload_attachment` already
     uses. The drawio file or PNG-with-embedded-XML goes up
     unchanged; binary round-trip is byte-perfect because
     Confluence stores it as a blob.

  2. **Edit the page body** — `PUT /wiki/api/v2/pages/{id}` with
     a new body containing the `<ac:structured-macro ac:name="drawio">`
     block. The block references the attachment by filename via
     the `diagramName` parameter. The `conf_put` tool can do this
     in principle, but the macro envelope is verbose hand-built
     XHTML that no one wants to type — hence the dedicated tool.

Both steps succeed independently. If step 1 succeeds and step 2
fails (e.g. version conflict), the attachment is orphaned on the
page; the caller can recover with `conf_delete_attachment`.

### Static-PNG mode (default) — file format details

A "drawio PNG" is a normal PNG with one extra `tEXt` chunk whose
keyword is `mxfile` and whose text is:

  1. URL-encode the drawio XML
  2. DEFLATE-compress (raw, no zlib headers — `-zlib.MAX_WBITS`)
  3. base64-encode

The drawio XML envelope is:

  ```xml
  <mxfile>
    <diagram name="...">
      <!-- either the expanded tree here, OR base64+deflate+urlencoded text -->
    </diagram>
  </mxfile>
  ```

This means the user can:
  - Provide a standalone `.drawio` file → we wrap it into a PNG
  - Provide an already-prepared `.drawio.png` → we upload as-is
  - Provide a plain PNG (no embedded XML) → rejected with a clear
    error message ("not a drawio PNG; missing mxfile tEXt chunk")

The mcp-confluence server can do the wrapping inline in Go (no
external drawio CLI needed) using stdlib + an embedded copy of
md2conf's algorithm. The reverse direction (extract XML from a
`.drawio.png`) is NOT required for upload — we only need the
forward direction (wrap XML → PNG-with-embedded-XML).

### Proposed tool surface

Add **1 new tool** to bring the total from 16 to 17.

#### `conf_upload_drawio`

Uploads a drawio file AND embeds it on the page in one call.

  - **args:**
      - `pageId` (string, required) — numeric page id
      - `title` (string, optional) — new page title (only used when
        creating a NEW page; if pageId is omitted, the tool creates
        a new page in `spaceId` instead)
      - `spaceId` (string, optional) — space id for new page
        creation (mutually exclusive with pageId; if both set,
        pageId wins)
      - `drawioFile` (string, optional) — path to a standalone
        `.drawio` file. The tool wraps it into a PNG-with-embedded-XML
        and uploads as `.drawio.png`. The wrapping logic follows
        md2conf's render.py: URL-encode → DEFLATE → base64 → embed
        in `tEXt` chunk → wrap in PNG container.
      - `drawioPngFile` (string, optional) — path to an
        already-prepared `.drawio.png`. Uploaded as-is. Mutually
        exclusive with `drawioFile`; if both set, `drawioFile` wins.
      - `diagramDisplayName` (string, optional) — display name shown
        in the editor. Defaults to the basename of the input file
        without the extension.
      - `width` (int, optional) — macro width in pixels (default 1151)
      - `height` (int, optional) — macro height in pixels (default 911)
      - `outputFormat` ("toon" | "json", default "toon")
      - `jq` (string, optional)
  - **wire shape:** 2-step flow — POST v1 attachment endpoint +
    PUT v2 page body. The macro envelope is the **minimal** form
    (just `diagramName` + dimensions) so the tool works with or
    without the drawio marketplace app installed.
  - **returns:** TOON-encoded envelope:
    ```
    {
      attachmentId: "<from upload step>",
      attachmentTitle: "<filename>.drawio.png",
      attachmentVersion: 1,
      diagramName: "<display name>",
      page: { id, title, version }
    }
    ```

The tool is a high-level orchestrator: it reuses `HandleUploadAttachment`
for step 1, then calls `HandlePut` (or `HandlePost` for new pages) for
step 2 with hand-built XHTML. No new atlassian.Client methods needed.

### Total tool count after this change

  - 5 CRUD — unchanged
  - 5 convenience — unchanged
  - 3 markdown — unchanged
  - 3 attachments — unchanged
  - 1 drawio (NEW) — upload + embed
  - **= 17 tools**

### What is explicitly NOT in scope

  - **Updating an existing embedded diagram.** The tool always
    uploads a NEW attachment. Updating an existing drawio requires
    re-uploading the file (different filename per upload — Confluence
    enforces unique filenames per page), then either deleting the
    old attachment or leaving both on the page. The v1 endpoint
    supports PUT-as-update (CreateOrUpdate) but the macro then
    needs its `revision` parameter bumped — that's a v2 follow-up.
  - **Extracting the embedded XML back out of a drawio PNG.** Not
    needed for the upload path. md2conf's reverse algorithm is
    ~80 LOC but no caller in this server needs it.
  - **Editing the diagram in-place.** The marketplace app does
    that via a separate UI. The MCP server treats the diagram
    as opaque bytes.
  - **The "embed existing diagram" macro** (vs. "insert diagram
    from attachment"). That's a different macro name and is
    useful for linking to diagrams on OTHER pages — not needed
    here.

## Verification

### Library / spec verification

The macro XHTML shape in section "drawio embedding actually works"
above is the literal shape from the draw.io docs (Source 1) and
matches what md2conf generates (Source 3). The PNG-with-embedded-XML
format is the literal chunk format md2conf parses (Source 4
render.py).

### End-to-end test plan (post-implementation)

1. `make check` — green (vet + lint + tests).
2. `make build` — binary produced.
3. Restart Hermes so the new binary loads.
4. Live test against `smartergroup.atlassian.net`:
    a. Create a sandbox page via `conf_post_markdown`.
    b. Build a minimal `.drawio` file (xml envelope with one
       cell).
    c. Call `conf_upload_drawio` with that file.
    d. Verify via `conf_list_attachments` that the
       `.drawio.png` attachment is on the page.
    e. Open the page in the browser (manual) to confirm the
       diagram renders. If the marketplace app isn't installed,
       the diagram should still appear as a static PNG.
    f. Call `conf_delete_attachment` to clean up.
    g. Delete the page via `conf_delete`.
5. Commit + auto-push per the gitter skill.

### C1 caveat — macro parameter set

The drawio macro accepts many optional parameters (`zoom`,
`lbox`, `simple`, `inComment`, `tbstyle`, `links`). The proposed
tool exposes only `width` + `height` because those are the two
the drawio docs cite as user-facing controls in the embed flow.
Future revisions can add more — the wire shape is a list of
`<ac:parameter>` elements, easy to extend.

### C2 caveat — marketplace-app interaction

If the drawio marketplace app IS installed, Confluence may
auto-fill the `contentId`/`pageId`/`revision`/`baseUrl` parameters
when the page is opened in the editor. The MCP tool emits the
minimal envelope (just `diagramName` + dimensions) which both
modes tolerate. If a user needs the full rich envelope (e.g. for
strict version pinning), they can hand-build it via `conf_put`
with the storage XHTML.

### C3 caveat — static PNG correctness

The drawio PNG we generate is a valid PNG with one `tEXt` chunk.
For non-marketplace installs, Confluence's stock renderer just
shows the PNG image — the embedded XML is unused but harmless.
For marketplace installs, drawio's renderer picks up the XML and
renders it as the editable diagram. Both paths work.