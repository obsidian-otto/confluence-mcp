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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/bennie/mcp-confluence/internal/config"
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

	// Build the base URL. We assume the conventional Cloud shape; the
	// go-atlassian library does the same internally.
	baseURL := "https://" + cfg.SiteName

	auth := &Auth{Email: cfg.UserEmail, APIToken: cfg.APIKey}

	// Construct the underlying ctreminiom client. Passing nil for the
	// httpClient lets the library default to http.DefaultClient; we
	// then override it in Do() to our own HTTPClient field. The
	// constructor also wires up the basic-auth state so library calls
	// (e.g. future typed-service usage) work without re-applying auth.
	native, err := confluence.New(nil, cfg.SiteName)
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

// buildURL composes the full URL from base + path + query. The path must
// start with "/" (validated by the caller). The query map's empty-valued
// entries are dropped.
func buildURL(base, path string, query map[string]string) (string, error) {
	if !strings.HasPrefix(path, "/") {
		return "", fmt.Errorf("path must start with \"/\"; got %q", path)
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	// url.URL.JoinReference would be more elegant, but our paths are
	// always site-relative and we want a literal join.
	u.Path = strings.TrimRight(u.Path, "/") + path
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
