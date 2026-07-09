package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// Config holds the resolved settings for the mcp-confluence process.
//
// APIKey is intentionally named "APIKey" (not "token") and is typed as
// `string`. The field name is part of the project's secret-handling
// contract — never assign a secret to a variable literally named `token`
// anywhere in this package, to prevent accidental logging by debug
// printers that walk struct fields.
type Config struct {
	SiteName  string // <site>.atlassian.net — the part before ".atlassian.net"
	UserEmail string // Atlassian account email for basic auth
	APIKey    string // Atlassian API token (basic-auth password). Treat as secret.
	Debug     bool   // when true, log each tool call to stderr
}

// envKeys enumerates every setting this package resolves. Kept here (not
// scattered) so token-redaction and naming rules stay in one place.
var envKeys = struct {
	SiteName  string
	UserEmail string
	APIKey    string
	Debug     string
}{
	SiteName:  "ATLASSIAN_SITE_NAME",
	UserEmail: "ATLASSIAN_USER_EMAIL",
	APIKey:    "ATLASSIAN_API_TOKEN",
	Debug:     "DEBUG",
}

// LoadFromEnv resolves settings using the Q22-locked priority chain:
// process env > cwd .env > binary-dir .env. Missing .env files are not
// errors; missing required settings (after resolution) ARE errors and
// produce a clear, token-redacted message on stderr via the returned error.
func LoadFromEnv() (*Config, error) {
	return loadFromSources(
		os.Getenv,
		os.Getwd,
		func() string { return filepath.Dir(mustExecutable()) },
	)
}

// loadFromSources is the testable core: it accepts the three source
// accessors so tests can drive the priority chain without touching the
// developer's real env, cwd, or binary path. The returned Config only
// contains values that resolved to non-empty strings from one of the
// three tiers, in priority order.
func loadFromSources(
	getenv func(string) string,
	getwd func() (string, error),
	binaryDir func() string,
) (*Config, error) {
	merged := make(map[string]string)

	// Tier 3: binary-dir .env (lowest priority)
	if dir := binaryDir(); dir != "" {
		if err := mergeDotenv(filepath.Join(dir, ".env"), merged); err != nil {
			return nil, err
		}
	}

	// Tier 2: cwd .env
	if wd, err := getwd(); err == nil && wd != "" {
		if err := mergeDotenv(filepath.Join(wd, ".env"), merged); err != nil {
			return nil, err
		}
	}

	// Tier 1: process env (highest priority — overwrite lower tiers)
	for _, k := range []string{
		envKeys.SiteName, envKeys.UserEmail, envKeys.APIKey, envKeys.Debug,
	} {
		if v, ok := lookupNonEmpty(getenv, k); ok {
			merged[k] = v
		}
	}

	cfg := &Config{
		SiteName:  merged[envKeys.SiteName],
		UserEmail: merged[envKeys.UserEmail],
		APIKey:    merged[envKeys.APIKey],
	}

	if raw, ok := merged[envKeys.Debug]; ok {
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid %s=%q: must be a boolean (true/false)", envKeys.Debug, raw)
		}
		cfg.Debug = b
	}

	if err := validate(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// mergeDotenv reads a .env file (if it exists) and merges its entries into
// dst, OVERWRITING any existing value. The Q22 priority chain relies on
// this overwrite semantics: a higher tier must win over a lower tier even
// when the higher tier only specifies a subset of keys. Missing files are
// silent; parse errors are fatal and redacted.
func mergeDotenv(path string, dst map[string]string) error {
	entries, err := Load(path)
	if err != nil {
		return err
	}
	for k, v := range entries {
		dst[k] = v
	}
	return nil
}

// lookupNonEmpty returns (value, true) only when the env var is set AND
// non-empty. Empty strings are treated as "unset" so the priority chain
// can fall through.
func lookupNonEmpty(getenv func(string) string, key string) (string, bool) {
	v := getenv(key)
	if v == "" {
		return "", false
	}
	return v, true
}

// validate enforces the required-setting contract. The error messages
// mention the env-var name so the user knows what to set, and NEVER echo
// any value (in particular, the API token is never logged even when set).
func validate(c *Config) error {
	switch {
	case c.SiteName == "":
		return fmt.Errorf("FATAL: %s is not set. Set it to your site prefix (the part before \".atlassian.net\"). Example:\n  %s=your-company",
			envKeys.SiteName, envKeys.SiteName)
	case c.UserEmail == "":
		return fmt.Errorf("FATAL: %s is not set. Set it to the Atlassian account email that issued the API token. Example:\n  %s=you@example.com",
			envKeys.UserEmail, envKeys.UserEmail)
	case c.APIKey == "":
		return fmt.Errorf("FATAL: %s is not set. Generate one at https://id.atlassian.com/manage-profile/security/api-tokens then set it in the environment. Value: <value redacted>",
			envKeys.APIKey)
	}
	return nil
}

// mustExecutable returns the path of the running binary, or "" if it
// cannot be resolved (e.g. during `go test` on some platforms). Callers
// treat an empty result as "no binary-dir .env to consult".
func mustExecutable() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return exe
}

// ErrNotFound is returned by future callers that need to distinguish a
// missing .env from a parse error. Currently unused but reserved so
// downstream packages don't have to re-import os to check
// errors.Is(err, os.ErrNotExist) on Load's return.
var ErrNotFound = errors.New("dotenv: file not found")
