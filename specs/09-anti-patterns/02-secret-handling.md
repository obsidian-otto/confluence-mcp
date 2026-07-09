# 09.2 — Secret Handling (API Tokens Never Echo)

## Overview

The Atlassian API token (`ATLASSIAN_API_TOKEN`) is the single
most sensitive credential the binary handles. It must never
appear in **any** of the following:

- stdout (would break JSON-RPC AND leak the secret)
- stderr (visible to Hermes `hermes mcp logs`, to debuggers,
  to anyone running `2>log.txt`)
- log files (the upstream's `~/.mcp/data/*.log` files; we
  don't write a log file at v1 but a future v1.1 must redact)
- error messages returned to the LLM (visible to the user)
- HTTP request bodies (we don't make outbound calls with the
  token in the body — it's in the `Authorization: Basic ...`
  header)
- environment dumps (any debug print that includes `os.Environ()`)

This file documents the patterns for safe secret handling
and the tests that prove no leak.

## Sources

- Hermes security model (env filtering, credential stripping):
  `~/.hermes/skills/mcp/native-mcp/SKILL.md` — "All other
  environment variables ... are excluded unless you explicitly
  add them via the `env` config key." and "If an MCP tool call
  fails, any credential-like patterns in the error message
  are automatically redacted before being shown to the LLM."
- Atlassian token storage guidance:
  https://id.atlassian.com/manage-profile/security/api-tokens
  (the canonical token-management page).
- OWASP secret handling:
  https://cheatsheetseries.owasp.org/cheatsheets/Secrets_Management_Cheat_Sheet.html

## Spec

### Token flow (safe path)

```
~/.hermes/.env                     (mode 0600, owned by user)
   ATLASSIAN_API_TOKEN=ATATT3x...
       │
       ▼ Hermes env-var resolution (server-connect time)
mcp_servers.confluence.env.ATLASSIAN_API_TOKEN="${ATLASSIAN_API_TOKEN}"
       │
       ▼ Hermes subprocess env (only env vars listed in `env:` block)
subprocess env: {ATLASSIAN_SITE_NAME, ATLASSIAN_USER_EMAIL, ATLASSIAN_API_TOKEN}
       │
       ▼ cmd/mcp-confluence/main.go
os.Getenv("ATLASSIAN_API_TOKEN")  ← raw string in memory
       │
       ▼ internal/atlassian/auth.go
client.Auth.SetBasicAuth(email, token)  ← still in memory
       │
       ▼ HTTP request
Authorization: Basic base64(email:token)  ← transmitted over TLS to atlassian.net
```

The token's **only** appearances are:

1. `~/.hermes/.env` (mode 0600).
2. Memory of the running Hermes process.
3. Memory of the running `mcp-confluence` subprocess.
4. The TLS-encrypted `Authorization` header to
   `*.atlassian.net`.

It must NOT appear in any other context.

### Code patterns

#### ✅ Read from env at startup, never log

```go
// internal/config/config.go
type Config struct {
    SiteName  string
    UserEmail string
    APIToken  string  // private field; never logged
    Debug     bool
}

func LoadFromEnv() (*Config, error) {
    c := &Config{
        SiteName:  os.Getenv("ATLASSIAN_SITE_NAME"),
        UserEmail: os.Getenv("ATLASSIAN_USER_EMAIL"),
        APIToken:  os.Getenv("ATLASSIAN_API_TOKEN"),
        Debug:     os.Getenv("DEBUG") == "true",
    }
    if c.SiteName == "" || c.UserEmail == "" || c.APIToken == "" {
        return nil, fmt.Errorf("missing required env var")
    }
    return c, nil
}
```

#### ✅ Pass to go-atlassian, never log

```go
// internal/atlassian/client.go
client, err := confluence.New(nil, cfg.SiteName+".atlassian.net")
if err != nil { return nil, err }
client.Auth.SetBasicAuth(cfg.UserEmail, cfg.APIToken)
return client, nil
```

#### ✅ Log without token

```go
// cmd/mcp-confluence/main.go
log.Printf("mcp-confluence v%s starting (site=%s, email=%s)",
    version, cfg.SiteName, cfg.UserEmail)
// NEVER: log.Printf("...token=%s", cfg.APIToken)
```

#### ✅ Sanitize errors that might contain the token

The Confluence API never echoes the `Authorization` header
in its error responses, so 4xx/5xx bodies don't contain the
token. But the **Atlassian DNS / TLS error messages**
sometimes include the URL (which contains the site name,
not the token — safe) or the IP (safe).

For belt-and-suspenders safety, the error formatter in
`internal/atlassian/errors.go` redacts any string matching
`ATATT[A-Za-z0-9]{30,}` from error messages:

```go
var tokenPattern = regexp.MustCompile(`ATATT[A-Za-z0-9]{30,}`)

func SanitizeError(s string) string {
    return tokenPattern.ReplaceAllString(s, "[REDACTED]")
}
```

### Anti-patterns

#### ❌ Logging the full config

```go
// BREAKS — leaks the token.
log.Printf("config: %+v", cfg)
```

#### ❌ Returning the env in an error

```go
// BREAKS — leaks the token via os.Environ().
return fmt.Errorf("config: %s", os.Environ())
```

#### ❌ Panic with config in the message

```go
// BREAKS — panic message is written to stderr.
panic(fmt.Sprintf("invalid config: %+v", cfg))
```

#### ❌ Test fixtures that hardcode a real token

```go
// BREAKS — committing a real token to git.
const testToken = "ATATT3xFfGF0..."
```

Test fixtures must use **synthetic** tokens
(`ATATTtesttoken...`) or read from a
`TEST_ATLASSIAN_API_TOKEN` env var that is explicitly
documented as "never set to a real token in CI".

### Tests for safe secret handling

```go
// internal/config/config_test.go
func TestLoadFromEnv_NoTokenLeak(t *testing.T) {
    os.Setenv("ATLASSIAN_SITE_NAME", "test")
    os.Setenv("ATLASSIAN_USER_EMAIL", "test@example.com")
    os.Setenv("ATLASSIAN_API_TOKEN", "ATATT3xFfGF0testsecret12345678901234567890")
    os.Setenv("DEBUG", "true")
    defer os.Clearenv()

    cfg, err := LoadFromEnv()
    if err != nil { t.Fatal(err) }

    // Capture stderr
    r, w, _ := os.Pipe()
    log.SetOutput(w)
    log.Printf("config loaded: %+v", cfg)
    w.Close()
    buf, _ := io.ReadAll(r)

    if strings.Contains(string(buf), "ATATT3xFfGF0testsecret") {
        t.Errorf("token leaked in log output: %s", buf)
    }
}
```

This test **must pass** for every release.

## Verification

A reader of this spec should be able to:

1. Run `go test ./internal/config/... -run TestLoadFromEnv_NoTokenLeak`
   and see it pass.
2. Run `grep -rn 'cfg\.APIToken\|os\.Getenv("ATLASSIAN_API_TOKEN")' --include='*.go'`
   and confirm every match is either in a function that
   passes the value to `SetBasicAuth` or stores it in a
   variable named `token` (no logger or formatter).
3. Set `DEBUG=true` and run the binary; pipe stderr to
   `grep ATATT` and confirm the only match is the
   `[REDACTED]` placeholder (if the regex is exercised) or
   no match at all (normal path).