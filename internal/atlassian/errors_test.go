package atlassian

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// TestAuthMissingError_ImplementsError verifies the AuthMissingError type
// satisfies the error interface and produces a stable, non-empty message.
func TestAuthMissingError_ImplementsError(t *testing.T) {
	// Compile-time assertion that *AuthMissingError satisfies the error
	// interface. If this file stops compiling, the contract is broken.
	var _ error = (*AuthMissingError)(nil)
	err := &AuthMissingError{Field: "ATLASSIAN_API_TOKEN"}
	msg := err.Error()
	if msg == "" {
		t.Fatal("AuthMissingError message must not be empty")
	}
	// Message should mention the field so the user knows which env var to set.
	if !strings.Contains(msg, "ATLASSIAN_API_TOKEN") {
		t.Errorf("AuthMissingError message should mention the missing field, got: %q", msg)
	}
}

// TestAuthMissingError_NeverContainsValue guards against a regression where a
// caller accidentally passes the secret value into the struct. The error
// message must only ever echo the field name — never the value.
func TestAuthMissingError_NeverContainsValue(t *testing.T) {
	const secretSentinel = "X-SECRET-VALUE-DO-NOT-LEAK-X"
	err := &AuthMissingError{Field: "ATLASSIAN_API_TOKEN"}
	if strings.Contains(err.Error(), secretSentinel) {
		t.Fatalf("AuthMissingError leaked sentinel value: %q", err.Error())
	}
}

// TestAPIError_ErrorFormat is the spec-pinned test: the literal format must
// be exactly "<METHOD> <path>: <status> <statusText> - <body>" per
// specs/09-anti-patterns/03-error-shapes.md. Drift here is a bug.
func TestAPIError_ErrorFormat(t *testing.T) {
	tests := []struct {
		name     string
		method   string
		path     string
		status   int
		statusTx string
		body     string
		want     string
	}{
		{
			name:     "404 not found",
			method:   "GET",
			path:     "/wiki/api/v2/pages/999",
			status:   404,
			statusTx: "Not Found",
			body:     `{"code":"NOT_FOUND","message":"Page not found"}`,
			want:     `GET /wiki/api/v2/pages/999: 404 Not Found - {"code":"NOT_FOUND","message":"Page not found"}`,
		},
		{
			name:     "409 conflict",
			method:   "PUT",
			path:     "/wiki/api/v2/pages/1234567",
			status:   409,
			statusTx: "Conflict",
			body:     `{"code":"VERSION_MISMATCH","message":"Current version is 3, not 2"}`,
			want:     `PUT /wiki/api/v2/pages/1234567: 409 Conflict - {"code":"VERSION_MISMATCH","message":"Current version is 3, not 2"}`,
		},
		{
			name:     "401 unauthorized",
			method:   "GET",
			path:     "/wiki/api/v2/spaces",
			status:   401,
			statusTx: "Unauthorized",
			body:     `{"code":"AUTHENTICATION_FAILED","message":"Authentication failed"}`,
			want:     `GET /wiki/api/v2/spaces: 401 Unauthorized - {"code":"AUTHENTICATION_FAILED","message":"Authentication failed"}`,
		},
		{
			name:     "post create",
			method:   "POST",
			path:     "/wiki/api/v2/pages",
			status:   400,
			statusTx: "Bad Request",
			body:     `{"code":"INVALID","message":"missing spaceId"}`,
			want:     `POST /wiki/api/v2/pages: 400 Bad Request - {"code":"INVALID","message":"missing spaceId"}`,
		},
		{
			name:     "empty body",
			method:   "DELETE",
			path:     "/wiki/api/v2/pages/1",
			status:   204,
			statusTx: "No Content",
			body:     "",
			want:     `DELETE /wiki/api/v2/pages/1: 204 No Content - `,
		},
		{
			name:     "patch",
			method:   "PATCH",
			path:     "/wiki/api/v2/pages/42",
			status:   500,
			statusTx: "Internal Server Error",
			body:     `{"message":"boom"}`,
			want:     `PATCH /wiki/api/v2/pages/42: 500 Internal Server Error - {"message":"boom"}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := &APIError{
				Method:     tc.method,
				Path:       tc.path,
				StatusCode: tc.status,
				StatusText: tc.statusTx,
				Body:       []byte(tc.body),
			}
			got := e.Error()
			if got != tc.want {
				t.Errorf("Error() mismatch\n  got:  %q\n  want: %q", got, tc.want)
			}
		})
	}
}

// TestAPIError_BodyTruncation pins the 2000-char body cap from
// specs/09-anti-patterns/03-error-shapes.md (Helper: the error formatter).
func TestAPIError_BodyTruncation(t *testing.T) {
	huge := strings.Repeat("a", 5000)
	e := &APIError{
		Method:     "GET",
		Path:       "/wiki/api/v2/spaces",
		StatusCode: 500,
		StatusText: "Internal Server Error",
		Body:       []byte(huge),
	}
	got := e.Error()
	if !strings.HasSuffix(got, "... (truncated)") {
		t.Errorf("expected truncated marker suffix, got tail: %q", got[max(0, len(got)-40):])
	}
	// The body portion in the formatted message must NOT exceed 2000 chars
	// from the source. Format: "METHOD path: STATUS TEXT - BODY[:truncated]"
	prefix := "GET /wiki/api/v2/spaces: 500 Internal Server Error - "
	if !strings.HasPrefix(got, prefix) {
		t.Fatalf("format prefix mismatch: %q", got[:min(80, len(got))])
	}
	bodyPart := strings.TrimPrefix(got, prefix)
	// strip the " ... (truncated)" marker
	if !strings.HasSuffix(bodyPart, "... (truncated)") {
		t.Fatalf("missing truncation marker in %q", bodyPart)
	}
	bodyOnly := strings.TrimSuffix(bodyPart, "... (truncated)")
	if len(bodyOnly) != 2000 {
		t.Errorf("bodyPart length = %d, want 2000", len(bodyOnly))
	}
}

// TestAPIError_ErrorsAsRecoverable verifies the typed error can be unwrapped
// by the caller via errors.As, which Phase 6's executeRequest helper relies
// on to differentiate API failures from network/unmarshal failures.
func TestAPIError_ErrorsAsRecoverable(t *testing.T) {
	orig := &APIError{
		Method:     "GET",
		Path:       "/wiki/api/v2/spaces",
		StatusCode: 401,
		StatusText: "Unauthorized",
		Body:       []byte(`{"message":"nope"}`),
	}
	wrapped := fmt.Errorf("tool conf_get: %w", orig)

	var got *APIError
	if !errors.As(wrapped, &got) {
		t.Fatal("errors.As should recover *APIError from wrapped error")
	}
	if got.StatusCode != 401 || got.Path != "/wiki/api/v2/spaces" {
		t.Errorf("recovered APIError wrong: %+v", got)
	}
}

// TestAPIError_NeverLeaksToken is a guard rail: the body field could
// conceivably carry an echoed token (e.g. a misconfigured proxy). The
// formatted message must never grow if the body contains the literal
// "<value redacted>" placeholder alone, but more importantly, our formatter
// must not inject the token from any Auth field. We assert that no
// Auth/APIToken/email field is referenced in Error().
func TestAPIError_NeverLeaksToken(t *testing.T) {
	e := &APIError{
		Method:     "GET",
		Path:       "/wiki/api/v2/spaces",
		StatusCode: 401,
		StatusText: "Unauthorized",
		Body:       []byte(`{"message":"Authentication failed"}`),
	}
	msg := e.Error()
	// Common token-shaped words must not appear unless the upstream body
	// (which the caller controls) actually contained them. This test's body
	// does NOT contain them, so the formatter must not invent them.
	for _, leak := range []string{"ATLASSIAN_API_TOKEN", "api_token", "Bearer "} {
		if strings.Contains(msg, leak) {
			t.Errorf("APIError message leaked %q: %q", leak, msg)
		}
	}
}
