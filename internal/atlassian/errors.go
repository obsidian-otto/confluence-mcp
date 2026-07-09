// Package atlassian is a thin wrapper over ctreminiom/go-atlassian/v2 that
// exposes a small, JSON-aware HTTP surface (Do/Call) for the five mcp-confluence
// tools. This file defines the project's typed error vocabulary.
//
// Anti-pattern reference: specs/09-anti-patterns/03-error-shapes.md. The
// literal error message shape for an upstream 4xx/5xx is:
//
//	<METHOD> <path>: <statusCode> <statusText> - <body>
//
// Example: `GET /wiki/api/v2/spaces: 401 Unauthorized - {"message":"..."}`.
//
// The token field of the Auth struct is NEVER to be echoed into any error
// message. The body truncation is set at 2000 characters to keep the LLM
// context manageable.
package atlassian

import "fmt"

// maxBodyInError is the byte cap on the response body echoed into an
// APIError message. Past this, the body is truncated and a
// "... (truncated)" marker is appended.
const maxBodyInError = 2000

// AuthMissingError signals that a required authentication field was empty
// when a Client was constructed. The Field is the env-var name (e.g.
// "ATLASSIAN_API_TOKEN") so the user knows what to set; the actual value
// (when set) is never referenced by this error.
type AuthMissingError struct {
	Field string
}

// Error implements the error interface.
func (e *AuthMissingError) Error() string {
	return fmt.Sprintf("missing required auth field: %s (check the env var is set in process env, cwd .env, or binary-dir .env)", e.Field)
}

// APIError represents a 4xx or 5xx response from the Atlassian REST API.
// The Method, Path, StatusCode, StatusText, and Body fields are populated by
// the client's Do/Call helpers; the formatted message follows the literal
// shape from specs/09-anti-patterns/03-error-shapes.md.
type APIError struct {
	Method     string // HTTP method (GET, POST, PUT, PATCH, DELETE)
	Path       string // API path (e.g. "/wiki/api/v2/spaces")
	StatusCode int    // Numeric HTTP status (e.g. 404)
	StatusText string // HTTP status text (e.g. "Not Found")
	Body       []byte // Raw response body; truncated to maxBodyInError chars in Error()
}

// Error formats the APIError in the literal shape required by the spec:
//
//	<METHOD> <path>: <statusCode> <statusText> - <body>
//
// The body is truncated at maxBodyInError characters; a "... (truncated)"
// marker is appended if the truncation occurs. The token (held separately
// in the Client's Auth field) is NOT included in this output under any
// circumstances.
func (e *APIError) Error() string {
	body := e.Body
	if len(body) > maxBodyInError {
		body = append([]byte(nil), body[:maxBodyInError]...)
		body = append(body, []byte("... (truncated)")...)
	}
	return fmt.Sprintf("%s %s: %d %s - %s", e.Method, e.Path, e.StatusCode, e.StatusText, body)
}
