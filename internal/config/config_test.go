package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadFromSources covers the Q22 priority chain:
//
//	process env > cwd .env > binary-dir .env.
//
// Each scenario supplies a custom env/cwd/binaryDir so the test stays
// hermetic and never reads the developer's real .env or shell env.
func TestLoadFromSources(t *testing.T) {
	const (
		envSite  = "ATLASSIAN_SITE_NAME"
		envEmail = "ATLASSIAN_USER_EMAIL"
		envTok   = "ATLASSIAN_API_TOKEN"
		envDebug = "DEBUG"
	)

	type sources struct {
		env       map[string]string
		cwd       string // directory containing the cwd ".env" (or "" for none)
		binaryDir string // directory containing the binary-dir ".env" (or "" for none)
	}

	tests := []struct {
		name      string
		s         sources
		want      Config
		wantErr   bool
		wantErrIs string // substring expected in error message
	}{
		{
			name: "all set via process env",
			s: sources{
				env: map[string]string{
					envSite: "from-env", envEmail: "env@example.com",
					envTok: "env-token", envDebug: "true",
				},
			},
			want: Config{
				SiteName: "from-env", UserEmail: "env@example.com",
				APIKey: "env-token", Debug: true,
			},
		},
		{
			name: "missing site name -> error",
			s: sources{
				env: map[string]string{
					envEmail: "env@example.com", envTok: "tok",
				},
			},
			wantErr:   true,
			wantErrIs: envSite,
		},
		{
			name: "missing email -> error",
			s: sources{
				env: map[string]string{
					envSite: "site", envTok: "tok",
				},
			},
			wantErr:   true,
			wantErrIs: envEmail,
		},
		{
			name: "missing token -> error (message must NOT echo token)",
			s: sources{
				env: map[string]string{
					envSite: "site", envEmail: "env@example.com",
				},
			},
			wantErr:   true,
			wantErrIs: envTok,
		},
		{
			name: "empty values are treated as unset",
			s: sources{
				env: map[string]string{
					envSite: "", envEmail: "env@example.com", envTok: "tok",
				},
			},
			wantErr:   true,
			wantErrIs: envSite,
		},
		{
			name: ".env in cwd picked up when env is empty",
			s: sources{
				env: map[string]string{},
				cwd: writeEnvToTemp(t, "ATLASSIAN_SITE_NAME=acme\n"+
					"ATLASSIAN_USER_EMAIL=user@example.com\n"+
					"ATLASSIAN_API_TOKEN=cwd-token\n"),
			},
			want: Config{
				SiteName: "acme", UserEmail: "user@example.com",
				APIKey: "cwd-token",
			},
		},
		{
			name: ".env next to binary picked up when cwd and env are empty",
			s: sources{
				env: map[string]string{},
				binaryDir: writeEnvToTemp(t, "ATLASSIAN_SITE_NAME=bin\n"+
					"ATLASSIAN_USER_EMAIL=bin@example.com\n"+
					"ATLASSIAN_API_TOKEN=bin-token\n"),
			},
			want: Config{
				SiteName: "bin", UserEmail: "bin@example.com",
				APIKey: "bin-token",
			},
		},
		{
			name: "process env wins over .env in cwd",
			s: sources{
				env: map[string]string{
					envSite: "from-env", envEmail: "env@example.com", envTok: "env-token",
				},
				cwd: writeEnvToTemp(t, "ATLASSIAN_SITE_NAME=cwd-site\n"+
					"ATLASSIAN_USER_EMAIL=cwd@example.com\n"+
					"ATLASSIAN_API_TOKEN=cwd-token\n"),
			},
			want: Config{
				SiteName: "from-env", UserEmail: "env@example.com", APIKey: "env-token",
			},
		},
		{
			name: "cwd .env wins over binary-dir .env when env is empty",
			s: sources{
				env: map[string]string{},
				cwd: writeEnvToTemp(t, "ATLASSIAN_SITE_NAME=cwd-site\n"+
					"ATLASSIAN_USER_EMAIL=cwd@example.com\n"+
					"ATLASSIAN_API_TOKEN=cwd-token\n"),
				binaryDir: writeEnvToTemp(t, "ATLASSIAN_SITE_NAME=bin-site\n"+
					"ATLASSIAN_USER_EMAIL=bin@example.com\n"+
					"ATLASSIAN_API_TOKEN=bin-token\n"),
			},
			want: Config{
				SiteName: "cwd-site", UserEmail: "cwd@example.com", APIKey: "cwd-token",
			},
		},
		{
			name: "DEBUG parses as bool true/false",
			s: sources{
				env: map[string]string{
					envSite: "s", envEmail: "e", envTok: "t", envDebug: "TRUE",
				},
			},
			want: Config{SiteName: "s", UserEmail: "e", APIKey: "t", Debug: true},
		},
		{
			name: "DEBUG defaults to false when unset",
			s: sources{
				env: map[string]string{
					envSite: "s", envEmail: "e", envTok: "t",
				},
			},
			want: Config{SiteName: "s", UserEmail: "e", APIKey: "t"},
		},
		{
			name: "DEBUG invalid value -> error",
			s: sources{
				env: map[string]string{
					envSite: "s", envEmail: "e", envTok: "t", envDebug: "yes-please",
				},
			},
			wantErr:   true,
			wantErrIs: envDebug,
		},
		{
			// A valid token is set, but DEBUG is malformed → triggers a fatal
			// error. The token value must not appear anywhere in the message.
			name: "fatal error message never echoes the token value",
			s: sources{
				env: map[string]string{
					envSite: "site", envEmail: "env@example.com",
					envTok:   "ATATT3xSECRET-DO-NOT-LEAK",
					envDebug: "yes-please",
				},
			},
			wantErr:   true,
			wantErrIs: envDebug,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			envFn := func(k string) string { return tt.s.env[k] }
			cwdFn := func() (string, error) {
				if tt.s.cwd == "" {
					return "/nonexistent-test-cwd-please-do-not-exist", nil
				}
				return tt.s.cwd, nil
			}
			binFn := func() string {
				if tt.s.binaryDir == "" {
					return "/nonexistent-test-bindir-please-do-not-exist"
				}
				return tt.s.binaryDir
			}

			got, err := loadFromSources(envFn, cwdFn, binFn)
			if (err != nil) != tt.wantErr {
				t.Fatalf("loadFromSources() error = %v, wantErr=%v", err, tt.wantErr)
			}
			if tt.wantErr {
				if tt.wantErrIs != "" && !strings.Contains(err.Error(), tt.wantErrIs) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErrIs)
				}
				if strings.Contains(err.Error(), "ATATT3xSECRET-DO-NOT-LEAK") {
					t.Fatalf("error leaked token value: %v", err)
				}
				return
			}
			if got.SiteName != tt.want.SiteName ||
				got.UserEmail != tt.want.UserEmail ||
				got.APIKey != tt.want.APIKey ||
				got.Debug != tt.want.Debug {
				t.Errorf("loadFromSources() = %+v, want %+v", *got, tt.want)
			}
		})
	}
}

// TestLoadFromEnv_ReadsRealProcessEnv is a smoke test that LoadFromEnv
// actually calls os.Getenv / os.Executable (sanity check that the wrapper
// wires through to the real sources).
func TestLoadFromEnv_ReadsRealProcessEnv(t *testing.T) {
	t.Setenv("ATLASSIAN_SITE_NAME", "smoke-site")
	t.Setenv("ATLASSIAN_USER_EMAIL", "smoke@example.com")
	t.Setenv("ATLASSIAN_API_TOKEN", "smoke-token")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv: %v", err)
	}
	if cfg.SiteName != "smoke-site" || cfg.UserEmail != "smoke@example.com" ||
		cfg.APIKey != "smoke-token" {
		t.Fatalf("LoadFromEnv = %+v", *cfg)
	}
}

// writeEnvToTemp creates a fresh temp directory containing a .env file
// with the given content and returns the directory path.
func writeEnvToTemp(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(content), 0o600); err != nil {
		t.Fatalf("writeEnvToTemp: %v", err)
	}
	return dir
}
