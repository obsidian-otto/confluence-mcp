package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_Dotenv(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    map[string]string
		wantErr bool
	}{
		{
			name:    "empty file",
			content: "",
			want:    map[string]string{},
		},
		{
			name:    "single key value",
			content: "FOO=bar",
			want:    map[string]string{"FOO": "bar"},
		},
		{
			name:    "multiple keys",
			content: "FOO=bar\nBAZ=qux\nHELLO=world",
			want:    map[string]string{"FOO": "bar", "BAZ": "qux", "HELLO": "world"},
		},
		{
			name:    "double quoted value",
			content: `FOO="bar baz"`,
			want:    map[string]string{"FOO": "bar baz"},
		},
		{
			name:    "single quoted value",
			content: `FOO='bar baz'`,
			want:    map[string]string{"FOO": "bar baz"},
		},
		{
			name:    "comments ignored",
			content: "# this is a comment\nFOO=bar\n# another comment",
			want:    map[string]string{"FOO": "bar"},
		},
		{
			name:    "blank lines ignored",
			content: "\n\nFOO=bar\n\n\nBAZ=qux\n\n",
			want:    map[string]string{"FOO": "bar", "BAZ": "qux"},
		},
		{
			name:    "inline comment after value (no hash handling)",
			content: "FOO=bar # inline comment",
			// Per spec: we don't implement inline comments beyond leading `#`.
			// Whole-line comments (leading `#`) are stripped; inline `#` is part of the value.
			want: map[string]string{"FOO": "bar # inline comment"},
		},
		{
			name:    "value with equals sign",
			content: "FOO=bar=baz",
			want:    map[string]string{"FOO": "bar=baz"},
		},
		{
			name:    "empty value",
			content: "FOO=",
			want:    map[string]string{"FOO": ""},
		},
		{
			name:    "key with underscore and digits",
			content: "ATLASSIAN_SITE_NAME=acme\nATLASSIAN_USER_EMAIL=user@example.com",
			want: map[string]string{
				"ATLASSIAN_SITE_NAME":  "acme",
				"ATLASSIAN_USER_EMAIL": "user@example.com",
			},
		},
		{
			name:    "trailing whitespace stripped",
			content: "FOO=bar   ",
			want:    map[string]string{"FOO": "bar"},
		},
		{
			name:    "malformed: missing equals",
			content: "FOObar",
			wantErr: true,
		},
		{
			name:    "malformed: empty key",
			content: "=value",
			wantErr: true,
		},
		{
			name:    "malformed: unterminated double quote",
			content: `FOO="unterminated`,
			wantErr: true,
		},
		{
			name:    "malformed: unterminated single quote",
			content: `FOO='unterminated`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, ".env")
			if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
				t.Fatalf("setup: %v", err)
			}
			got, err := Load(path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Load(%q) error = %v, wantErr=%v", tt.content, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("Load(%q) = %v (len=%d), want %v (len=%d)",
					tt.content, got, len(got), tt.want, len(tt.want))
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("Load(%q)[%q] = %q, want %q",
						tt.content, k, got[k], v)
				}
			}
		})
	}
}

// TestLoad_MalformedRedactsValue ensures the error message for a malformed
// KEY=VALUE line whose value contains the API token NEVER echoes the token.
// Spec: "A line like ATLASSIAN_API_TOKEN=ATATT3x... that fails to parse
// produces the error 'invalid .env line N (ATLASSIAN_API_TOKEN=<value redacted>)'."
func TestLoad_MalformedRedactsValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	secret := "ATATT3xFfGF0secret-value-DO-NOT-LEAK"
	// Unterminated double-quote on the token value triggers a parse error
	// on the API_TOKEN line itself, exercising the redaction path.
	bad := `ATLASSIAN_API_TOKEN="` + secret + "\n"
	if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatalf("expected error for malformed line, got nil")
	}
	msg := err.Error()
	if contains(msg, secret) {
		t.Fatalf("error message leaked token value:\n%s", msg)
	}
	if !contains(msg, "<value redacted>") {
		t.Fatalf("error message should include <value redacted>:\n%s", msg)
	}
	if !contains(msg, "ATLASSIAN_API_TOKEN") {
		t.Fatalf("error message should include the key name:\n%s", msg)
	}
}

func contains(haystack, needle string) bool {
	if needle == "" {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
