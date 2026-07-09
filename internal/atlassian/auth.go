package atlassian

import (
	"net/http"

	"github.com/ctreminiom/go-atlassian/v2/confluence"
)

// Auth holds basic-auth credentials for the Atlassian Cloud REST API. The
// APIToken field is the secret; the package's secret-handling contract
// (per specs/09-anti-patterns/02-secret-handling.md) forbids logging the
// value of this field. The SetBasicAuth method on the underlying
// go-atlassian client only stores the values; it does not log them.
//
// The struct is deliberately small and purpose-built: any future auth mode
// (OAuth, PAT) gets its own type rather than accumulating optional fields
// here.
type Auth struct {
	Email    string // Atlassian account email (the basic-auth "user")
	APIToken string // API token (the basic-auth "password"). SECRET.
}

// applyBasicAuth configures the supplied ctreminiom Confluence Client to
// send `Authorization: Basic base64(email:APIToken)` on every request.
// The APIToken is never logged; the go-atlassian library stores it in
// its in-memory auth struct only.
//
// Returns *AuthMissingError if either the email or token is empty so the
// caller can surface a clear, field-named error to the user.
func (a *Auth) applyBasicAuth(c *confluence.Client) error {
	if a == nil {
		return &AuthMissingError{Field: "Auth"}
	}
	if a.Email == "" {
		return &AuthMissingError{Field: "ATLASSIAN_USER_EMAIL"}
	}
	if a.APIToken == "" {
		return &AuthMissingError{Field: "ATLASSIAN_API_TOKEN"}
	}
	// SetBasicAuth stores the values in the library's internal auth struct
	// for inclusion in the Authorization header. It returns no error in the
	// current library version; we discard the (bool, error) return values.
	c.Auth.SetBasicAuth(a.Email, a.APIToken)
	return nil
}

// applyAuthHeader sets the Authorization header directly on a *http.Request
// for callers using the raw HTTP path (Client.HTTP.Do). The basic-auth
// scheme encodes the email:APIToken pair as base64. The APIToken is never
// logged; the encoded form is also not echoed by this package's logging.
//
// Used by the Do() helper, which builds requests without going through
// the go-atlassian NewRequest pipeline (because Phase 6's executeRequest
// needs full control over the URL, query string, and raw body).
func (a *Auth) applyAuthHeader(req *http.Request) error {
	if a == nil {
		return &AuthMissingError{Field: "Auth"}
	}
	if a.Email == "" {
		return &AuthMissingError{Field: "ATLASSIAN_USER_EMAIL"}
	}
	if a.APIToken == "" {
		return &AuthMissingError{Field: "ATLASSIAN_API_TOKEN"}
	}
	// We delegate the base64 encoding to the standard library's basic-auth
	// helper, which produces the same "Basic <base64>" form the
	// go-atlassian library uses internally.
	req.SetBasicAuth(a.Email, a.APIToken)
	return nil
}
