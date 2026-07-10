// Package atlassian — Client wrapper. The Client type is a thin, JSON-aware
// HTTP wrapper used by Phase 6's executeRequest helper. The 5 MCP tools
// never see the go-atlassian library directly: they call Client.Do (raw
// bytes + status) or Client.Call (raw bytes + status, JSON-decoded).
//
// Architectural notes:
//
//  1. We use the raw HTTP path (Client.HTTP.Do) rather than ctreminiom's
//     typed services, because the v2 REST API endpoints are not typed in
//     the library (see specs/03-go-atlassian/01-package-layout.md). Doing
//     raw HTTP also gives us byte-perfect bodies for the downstream
//     JMESPath/TOON pipeline.
//
//  2. The base URL is constructed as "https://<site>" assuming the
//     conventional <site>.atlassian.net Atlassian Cloud shape. The Client
//     does not validate site beyond emptiness; that's a config-layer
//     concern.
//
//  3. The API token is held in the Auth.APIToken field. The package's
//     secret-handling contract forbids logging this value, and the
//     auth.applyAuthHeader method (auth.go) only places it in the
//     Authorization header — never in a return value, log line, or
//     error message.
package atlassian

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"strings"

	"github.com/bennie/mcp-confluence/internal/config"
	"github.com/bennie/mcp-confluence/internal/templates"
	"github.com/ctreminiom/go-atlassian/v2/confluence"
)

// HTTPClient is the minimal interface Client.Do needs. It matches
// http.Client's Do method. Tests substitute a custom client (via
// httptest.Server.Client() or a custom RoundTripper) to capture
// outgoing requests without hitting a real Atlassian endpoint.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client is the wrapper over ctreminiom/go-atlassian's Confluence client
// that the MCP tool handlers use. It exposes a small surface (Do, Call)
// that returns the raw response body, which the downstream JMESPath/TOON
// pipeline in Phase 6 consumes.
//
// The underlying ctreminiom Client is held in `native` so future phases
// can call typed v1 services (e.g. for SearchService.CQL) without
// re-wiring. For now, Phase 2 only uses the HTTP/auth fields.
type Client struct {
	HTTPClient HTTPClient
	Auth       *Auth
	BaseURL    string
	native     *confluence.Client
}

// New constructs a Client from a resolved config.Config. It returns
// *AuthMissingError if any of SiteName, UserEmail, or APIKey is empty —
// the calling layer (Phase 9 main) propagates that error so the user sees
// a field-named "set this env var" message on stderr.
//
// New() also pre-flights the underlying confluence.Client construction so
// the user gets the go-atlassian initialization error (if any) at startup
// rather than on first MCP tool call.
func New(cfg *config.Config) (*Client, error) {
	if cfg == nil {
		return nil, &AuthMissingError{Field: "config"}
	}
	if cfg.SiteName == "" {
		return nil, &AuthMissingError{Field: "ATLASSIAN_SITE_NAME"}
	}
	if cfg.UserEmail == "" {
		return nil, &AuthMissingError{Field: "ATLASSIAN_USER_EMAIL"}
	}
	if cfg.APIKey == "" {
		return nil, &AuthMissingError{Field: "ATLASSIAN_API_TOKEN"}
	}

	// Build the base URL. The locked settings contract (specs/01-foundations/
	// 03-env-var-contract.md) defines ATLASSIAN_SITE_NAME as the *bare site
	// prefix* — e.g. "acme" — and the server builds the URL as
	// "https://acme.atlassian.net". The upstream aashari MCP tool documents
	// the same convention. (The ATLASSIAN_API_BASE_URL opt-out for Data
	// Center is gap Q4 — not implemented in v1.)
	baseURL := templates.AtlassianBaseURL(cfg.SiteName)

	auth := &Auth{Email: cfg.UserEmail, APIToken: cfg.APIKey}

	// Construct the underlying ctreminiom client. The library expects the
	// full hostname, NOT the bare site prefix — pass the resolved hostname
	// (e.g. "acme.atlassian.net"). Passing nil for the httpClient lets the
	// library default to http.DefaultClient; we then override it in Do()
	// to our own HTTPClient field. The constructor also wires up the
	// basic-auth state so library calls (e.g. future typed-service usage)
	// work without re-applying auth.
	native, err := confluence.New(nil, baseURL)
	if err != nil {
		return nil, fmt.Errorf("atlassian: build client: %w", err)
	}
	if err := auth.applyBasicAuth(native); err != nil {
		return nil, err
	}

	return &Client{
		HTTPClient: http.DefaultClient,
		Auth:       auth,
		BaseURL:    baseURL,
		native:     native,
	}, nil
}

// Do executes a single HTTP request against the Atlassian API and
// returns the raw response body and status code. On a 4xx/5xx response
// it returns a *APIError matching the spec's literal error shape
// (specs/09-anti-patterns/03-error-shapes.md). The caller is responsible
// for any further processing (JSON decoding, JMESPath, TOON).
//
// Parameters:
//
//   - ctx: request-scoped context (cancelled ctx propagates to the HTTP client).
//   - method: HTTP method (GET, POST, PUT, PATCH, DELETE).
//   - path: API path relative to BaseURL, with a leading "/" (e.g.
//     "/wiki/api/v2/spaces").
//   - query: optional query-string parameters. nil is allowed; keys
//     with empty values are skipped.
//   - body: optional request body. nil is allowed; the body is sent
//     verbatim (Do does NOT re-marshal).
func (c *Client) Do(ctx context.Context, method, path string, query map[string]string, body []byte) ([]byte, int, error) {
	if c == nil {
		return nil, 0, &AuthMissingError{Field: "Client"}
	}

	fullURL, err := buildURL(c.BaseURL, path, query)
	if err != nil {
		return nil, 0, fmt.Errorf("atlassian: build URL: %w", err)
	}

	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = strings.NewReader(string(body))
	}
	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return nil, 0, fmt.Errorf("atlassian: build request: %w", err)
	}

	// Apply the basic-auth header. The token lives only in the
	// Authorization header value; it never appears in any other field
	// of the request, log line, or error message.
	if err := c.Auth.applyAuthHeader(req); err != nil {
		return nil, 0, err
	}

	// Default Content-Type when a body is present. The Atlassian v2 REST
	// API expects application/json for write methods.
	if len(body) > 0 && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	// Accept header is implicit for stdlib http, but we set it
	// explicitly so a proxy or test double that logs headers knows we
	// want JSON.
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json")
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		// Network / DNS / TLS error — Class 2 in the spec. We surface
		// the upstream err verbatim (sans token) by wrapping it in a
		// generic error; the executeRequest helper in Phase 6 decides
		// whether to re-wrap with the "<METHOD> <path>: network error: ..."
		// prefix.
		return nil, 0, fmt.Errorf("%s %s: network error: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("%s %s: read body: %w", method, path, err)
	}

	if resp.StatusCode >= 400 {
		return respBody, resp.StatusCode, &APIError{
			Method:     method,
			Path:       path,
			StatusCode: resp.StatusCode,
			StatusText: http.StatusText(resp.StatusCode),
			Body:       respBody,
		}
	}
	return respBody, resp.StatusCode, nil
}

// Call is a convenience wrapper over Do that JSON-decodes the response
// body into a map[string]any for the Phase 6 JMESPath/TOON pipeline. On
// success it returns the decoded map. On 4xx/5xx it returns the same
// *APIError Do would (passing through errors.As). On invalid JSON it
// returns a decode error.
func (c *Client) Call(ctx context.Context, method, path string, query map[string]string, body []byte) (map[string]any, error) {
	respBody, status, err := c.Do(ctx, method, path, query, body)
	if err != nil {
		// Pass APIError and network errors through unchanged so the
		// handler can recover the typed error via errors.As.
		return nil, err
	}
	_ = status // status is part of the non-error path's success signal via the body shape

	// Handle empty bodies (e.g. 204 No Content from DELETE). The Phase 6
	// pipeline encodes an empty map as a valid TOON output.
	if len(respBody) == 0 {
		return map[string]any{}, nil
	}

	// Decode into a generic map. The Confluence v2 API returns objects
	// at the top level (e.g. {"results": [...]}); a top-level array
	// would also be possible but the spec's tool mapping assumes the
	// object shape.
	var out map[string]any
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("%s %s: %d %s: invalid JSON response: %s",
			method, path, status, http.StatusText(status), firstN(respBody, 200))
	}
	return out, nil
}

// UploadAttachment uploads a file from disk as an attachment to a
// Confluence page. This is the ONE endpoint in this server that hits
// the v1 REST API (POST /wiki/rest/api/content/{pageId}/child/attachment)
// because the v2 attachments API exposes only read/delete — there is no
// v2 upload endpoint (verified against
// developer.atlassian.com/cloud/confluence/rest/v2/api-group-attachment/
// on 2026-07-10; full rationale in
// specs/11-attachments/01-research-and-surface.md).
//
// The implementation builds the multipart/form-data body with stdlib
// mime/multipart (so binary streams round-trip without base64 inflation)
// and sends the request via Client.HTTPClient. The X-Atlassian-Token
// header MUST be set to "no-check" — without it, Confluence returns 403
// due to CSRF protection. This header is the only thing that differs
// from a regular v2 write call, hence why this method lives on the
// atlassian.Client wrapper rather than going through Client.Do.
//
// Parameters:
//
//   - ctx: request-scoped context.
//   - pageId: numeric page id (Confluence v2 string-shaped — e.g.
//     "163935").
//   - filePath: absolute path to the file on disk. The file is opened
//     with os.Open and streamed; not loaded into memory.
//   - comment: optional changelog message (empty = no comment).
//   - minorEdit: whether the new attachment version is a minor edit
//     (true matches go-atlassian's default; pass-through to Confluence).
//
// Returns the raw response body (a v1 ContentPageScheme JSON envelope
// with the created/updated attachment metadata) and the HTTP status
// code. Errors are typed *APIError on 4xx/5xx (matching Client.Do's
// contract) so the executeRequest pipeline in Phase 6 can render them
// uniformly.
//
// The 100 MB cap is enforced by Atlassian Cloud, not by this method —
// calls over the cap return 413 from the server. Callers that need a
// pre-flight size check should stat() the file first.
func (c *Client) UploadAttachment(ctx context.Context, pageId, filePath, comment string, minorEdit bool) ([]byte, int, error) {
	if c == nil {
		return nil, 0, &AuthMissingError{Field: "Client"}
	}
	if pageId == "" {
		return nil, 0, &AuthMissingError{Field: "pageId"}
	}
	if filePath == "" {
		return nil, 0, &AuthMissingError{Field: "filePath"}
	}

	// Open the file. os.Open returns *AuthMissingError-shaped errors
	// via fmt.Errorf wrap — the handler layer will surface a clear
	// "open file" message. We do NOT read the file into memory: the
	// multipart.Writer streams directly from the *os.File via io.Copy
	// so 100 MB PDFs round-trip without spiking the heap.
	file, err := os.Open(filePath)
	if err != nil {
		return nil, 0, fmt.Errorf("upload_attachment: open %q: %w", filePath, err)
	}
	defer func() { _ = file.Close() }()

	// Stat for the multipart filename + a sanity-check that the path
	// is to a regular file (not a directory or symlink-to-nowhere).
	// We don't pre-flight the size cap here — the server enforces it
	// with a 413 — but we do refuse empty files because the multipart
	// upload with an empty payload is ambiguous.
	info, err := file.Stat()
	if err != nil {
		return nil, 0, fmt.Errorf("upload_attachment: stat %q: %w", filePath, err)
	}
	if info.IsDir() {
		return nil, 0, fmt.Errorf("upload_attachment: %q is a directory, not a file", filePath)
	}
	if info.Size() == 0 {
		return nil, 0, fmt.Errorf("upload_attachment: %q is an empty file", filePath)
	}

	// Build the multipart body in-memory. We could stream the body
	// directly to the request, but using a *bytes.Buffer keeps the
	// Content-Length calculable (multipart bodies need it set
	// explicitly OR the request must use chunked transfer encoding).
	// A 100 MB file in a *bytes.Buffer is ~100 MB heap — acceptable
	// for the spec's "small to medium files" intent.
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Filename in the multipart form is the base name — Confluence
	// stores attachments by their on-page filename, not the full path.
	// Use filepath.Base to extract just the file's basename.
	filename := info.Name()
	// Detect the file's MIME type from its extension so
	// Confluence indexes the attachment correctly. Without
	// this, the file part's Content-Type defaults to
	// application/octet-stream, which causes the drawio
	// marketplace app to fail with "Invalid descriptor" when
	// trying to read a .drawio file (it expects
	// application/vnd.jgraph.mxfile), and other apps to
	// mis-detect file types.
	contentType := detectContentType(filename)
	var part io.Writer
	if contentType != "" {
		// CreateFormFile with the explicit Content-Type via
		// CreatePart (CreateFormFile doesn't accept a type).
		// We build the form file header manually so the
		// part's Content-Type reflects the actual file type.
		fheader := textproto.MIMEHeader{}
		fheader.Set("Content-Disposition",
			fmt.Sprintf(`form-data; name="file"; filename=%q`, filename))
		fheader.Set("Content-Type", contentType)
		part, err = writer.CreatePart(fheader)
	} else {
		part, err = writer.CreateFormFile("file", filename)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("upload_attachment: create form file: %w", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return nil, 0, fmt.Errorf("upload_attachment: copy file data: %w", err)
	}

	// Optional changelog comment. Empty string means "no comment";
	// we skip the field in that case so the wire envelope is cleaner.
	if comment != "" {
		if err := writer.WriteField("comment", comment); err != nil {
			return nil, 0, fmt.Errorf("upload_attachment: write comment field: %w", err)
		}
	}

	// minorEdit: Confluence wants "true"/"false" as strings in
	// multipart fields. Default to true to match go-atlassian's
	// v1 ContentAttachmentService.Create default.
	if err := writer.WriteField("minorEdit", boolToString(minorEdit)); err != nil {
		return nil, 0, fmt.Errorf("upload_attachment: write minorEdit field: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, 0, fmt.Errorf("upload_attachment: finalize multipart body: %w", err)
	}

	// Compose the full URL. Path is /wiki/rest/api/content/{id}/child/attachment
	// (the v1 endpoint — see package doc above).
	fullURL, err := buildURL(c.BaseURL,
		fmt.Sprintf("/wiki/rest/api/content/%s/child/attachment", pageId), nil)
	if err != nil {
		return nil, 0, fmt.Errorf("upload_attachment: build URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, body)
	if err != nil {
		return nil, 0, fmt.Errorf("upload_attachment: build request: %w", err)
	}

	// Auth header (basic auth via email:APIToken).
	if err := c.Auth.applyAuthHeader(req); err != nil {
		return nil, 0, err
	}

	// Multipart Content-Type includes the boundary parameter; the
	// writer produces it via FormDataContentType().
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "application/json")

	// CSRF bypass — WITHOUT THIS HEADER CONFLUENCE RETURNS 403.
	// This is the one and only header that distinguishes the
	// attachment upload from a regular v2 write. Documented inline
	// because if it's removed, uploads silently break in production.
	req.Header.Set("X-Atlassian-Token", "no-check")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("POST /wiki/rest/api/content/%s/child/attachment: network error: %w", pageId, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("upload_attachment: read body: %w", err)
	}

	if resp.StatusCode >= 400 {
		return respBody, resp.StatusCode, &APIError{
			Method:     http.MethodPost,
			Path:       fmt.Sprintf("/wiki/rest/api/content/%s/child/attachment", pageId),
			StatusCode: resp.StatusCode,
			StatusText: http.StatusText(resp.StatusCode),
			Body:       respBody,
		}
	}
	return respBody, resp.StatusCode, nil
}

// boolToString returns "true" or "false" — Confluence's multipart
// field convention. Kept private to avoid the stdlib strconv import
// for a single use site.
//
// Note: Go's zero value for bool is false, so callers that omit
// minorEdit get a "false" on the wire. The Confluence API default
// for new attachments is to treat an unset field as a minor edit;
// the go-atlassian v1 Create method explicitly sets "true" by
// default. We do not replicate that behaviour here because (a) the
// Go zero value idiom is "the user did not opt in", and (b)
// callers who care can pass minorEdit: true explicitly. The
// wire field is always present so Confluence has a deterministic
// value to act on regardless of which choice the user makes.
func boolToString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// buildURL composes the full URL from base + path + query. The path must
// start with "/" (validated by the caller).
//
// The path may contain a trailing query string (e.g.
// "/wiki/api/v2/spaces?limit=2") — matching the upstream aashari's
// `fetchAtlassian(creds, "${path}${queryString}", ...)` shape documented
// in specs/02-upstream-aashari/01-architecture.md. Any query parameters
// embedded in the path are merged into the query map (caller-provided
// entries take precedence on key collision), then the resulting
// url.Values is encoded as the URL's RawQuery.
//
// The query map's empty-valued entries are dropped.
func buildURL(base, path string, query map[string]string) (string, error) {
	if !strings.HasPrefix(path, "/") {
		return "", fmt.Errorf("path must start with \"/\"; got %q", path)
	}
	// Split a trailing "?..." off the path and merge it into query so
	// callers can pass either:
	//   path: "/wiki/api/v2/spaces"      query: {"limit":"2"}
	// or:
	//   path: "/wiki/api/v2/spaces?limit=2"  query: nil
	// — both produce the same final URL. This matches the upstream
	// service's flexible input handling.
	cleanPath := path
	if i := strings.Index(path, "?"); i >= 0 {
		cleanPath = path[:i]
		raw := path[i+1:]
		if raw != "" {
			if embedded, err := url.ParseQuery(raw); err == nil {
				for k, vs := range embedded {
					if _, present := query[k]; !present && len(vs) > 0 {
						if query == nil {
							query = map[string]string{}
						}
						query[k] = vs[0]
					}
				}
			}
		}
	}

	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	// url.URL.JoinReference would be more elegant, but our paths are
	// always site-relative and we want a literal join.
	u.Path = strings.TrimRight(u.Path, "/") + cleanPath
	if len(query) > 0 {
		q := u.Query()
		for k, v := range query {
			if v == "" {
				continue
			}
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
	}
	return u.String(), nil
}

// firstN returns up to n leading bytes of b as a string. Used for the
// "invalid JSON response" error message which truncates the body to
// 200 chars per specs/09-anti-patterns/03-error-shapes.md (Class 3).
func firstN(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n])
}

// detectContentType returns the MIME type for an attachment
// filename, or "" if no mapping is known. The mapping covers
// the file types the mcp-confluence tools are most likely to
// upload: PNG/JPEG for images, PDF for documents, the special
// "drawio" MIME type for .drawio XML (the drawio marketplace
// app refuses to open .drawio files uploaded as
// application/octet-stream), and a handful of common archive
// / office formats.
//
// The mapping is intentionally hand-curated rather than using
// Go's mime.TypeByExtension (which on Linux is empty because
// the OS /etc/mime.types lookup is gated behind a cgo build
// tag). Hand-curating also lets us pin the drawio MIME type
// — the most important entry — to the value the drawio
// marketplace app requires.
func detectContentType(filename string) string {
	// Find the extension (case-insensitive). For multi-dot
	// names like "architecture.drawio.png" we want the LAST
	// extension, not just the last segment.
	ext := strings.ToLower(filename)
	if i := strings.LastIndex(ext, "."); i >= 0 {
		ext = ext[i+1:]
	}
	switch ext {
	case "png":
		return "image/png"
	case "jpg", "jpeg":
		return "image/jpeg"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	case "svg":
		return "image/svg+xml"
	case "pdf":
		return "application/pdf"
	case "zip":
		return "application/zip"
	case "doc", "docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case "xls", "xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case "ppt", "pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case "json":
		return "application/json"
	case "xml":
		return "application/xml"
	case "txt", "log", "md":
		return "text/plain"
	case "html", "htm":
		return "text/html"
	case "css":
		return "text/css"
	case "js":
		return "application/javascript"
	// The drawio file format. The marketplace app reads
	// this MIME type to identify the attachment as a
	// diagram; if it sees application/octet-stream it
	// fails with "Invalid descriptor".
	case "drawio":
		return "application/vnd.jgraph.mxfile"
	default:
		return ""
	}
}
