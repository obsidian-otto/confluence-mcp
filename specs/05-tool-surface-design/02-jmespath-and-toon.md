# 05.2 — JMESPath Library and TOON Encoder

## Overview

Two non-trivial libraries back the Go MCP server's response
shaping: **JMESPath** for the `jq` filter parameter, and
**TOON** for the token-efficient default output format.
This file documents the library choices, the alternatives
considered, and the implementation notes for each.

## Sources

- JMESPath spec: https://jmespath.org/
- `jmespath/go-jmespath`:
  https://github.com/jmespath/go-jmespath (the canonical Go
  JMESPath implementation; used by AWS SDK for Go and many
  others).
- TOON format:
  - Spec: https://github.com/toon-format/toon (the TOON
    specification repository, MIT).
  - Upstream usage: described in the upstream README
    "TOON Output Format" section (lines 184-210).

## Spec

### JMESPath library: `github.com/jmespath/go-jmespath`

**Choice:** `github.com/jmespath/go-jmespath` (latest stable;
`v0.4.0` at survey time).

**Alternatives considered:**

| Library | Pros | Cons | Verdict |
| ------- | ---- | ---- | ------- |
| `jmespath/go-jmespath` | Canonical, used by AWS SDK, well-tested, pure Go | None material | **Chosen** |
| `jmespath-community/go-jmespath` | Active fork | Less adoption | Rejected |
| Custom parser | Full control | High maintenance burden | Rejected |

**API:**

```go
import "github.com/jmespath/go-jmespath"

result, err := jmespath.Search("results[*].id", data)
// result is `any` (interface{}); marshal to JSON for downstream encoding
```

**Wrapping pattern** (in `internal/jmespath/`):

```go
package jmespath

import (
    "encoding/json"
    "github.com/jmespath/go-jmespath"
)

func Apply(expression string, data any) (any, error) {
    if expression == "" {
        return data, nil
    }
    parsed, err := jmespath.New(expression)
    if err != nil {
        return nil, fmt.Errorf("invalid jmespath expression: %w", err)
    }
    return parsed.Search(data)
}
```

The wrapper handles expression parsing once (at registration
time? — no, per-call; the expressions may be dynamic) and
returns a uniform error type. The library supports the full
JMESPath spec including `[*]`, `[?condition]`,
`{key: value}` projections, and function calls.

### TOON encoder: custom implementation

**Choice:** implement a **TOON-compatible encoder** in
`internal/toon/`, following the TOON spec at
https://github.com/toon-format/toon.

**Why custom?** At survey time (2026-07-09), there is no
production-quality TOON library for Go. The TOON spec is
small (≈200 lines) and the format is straightforward YAML-like
indented text. A custom encoder is ~150 lines of Go.

**TOON spec essentials:**

- Tabular arrays (objects with the same keys) use a header
  row + indented rows:

  ```
  results:
    - id: 123
      title: My Page
    - id: 456
      title: Other Page
  ```

- Nested objects use indentation:

  ```
  page:
    id: 789
    body:
      representation: storage
      value: <p>...</p>
  ```

- Strings are unquoted unless they contain special characters
  (`:`, `#`, leading/trailing whitespace, etc.). Strings that
  contain `:` must be quoted.

- Numbers and booleans are unquoted. `null` is rendered as
  `~` (TOON) or `null` (compatible mode).

**Encoder interface:**

```go
package toon

import (
    "bytes"
    "encoding/json"
    "io"
)

// Encode marshals v (typically a map[string]any or []any
// from JSON unmarshal) to TOON format.
func Encode(v any) (string, error)

// EncodeJSON marshals to JSON. (Trivial wrapper; exported for
// the outputFormat=json path.)
func EncodeJSON(v any) (string, error) {
    b, err := json.MarshalIndent(v, "", "  ")
    return string(b), err
}
```

**Tests** (`internal/toon/encode_test.go`):

- Empty object → `{}`
- Empty array → `[]`
- Flat object → `key: value\nkey2: value2\n`
- Nested object → indentation
- Array of homogeneous objects → tabular form
- Array of heterogeneous objects → list form
- Special characters in strings → quoted
- Round-trip: JSON → TOON → JSON must produce equal Go values

### What we deliberately do not support

| Feature | Why not |
| ------- | ------- |
| TOON streaming for huge responses | The 40k truncation kicks in first; we never encode >40k to TOON. |
| TOON round-trip parser | We only encode (output). The LLM gets TOON; if it needs JSON it passes `outputFormat: "json"`. |
| Custom JMESPath functions | Only the standard JMESPath function library (`length`, `keys`, `values`, `sort`, `map`, `filter`, `reduce`, etc.) is supported by `go-jmespath`. We don't extend it. |

## Verification

A reader of this spec should be able to:

1. Run the upstream against a test instance with
   `--jq "results[*].{id: id, name: name}"` and capture the
   TOON output. Then run the Go port with the same args and
   confirm the byte-for-byte equivalent output.
2. Run `go test ./internal/toon/...` and confirm the
   round-trip tests pass for 10+ JSON fixtures.
3. Run `go test ./internal/jmespath/...` and confirm
   JMESPath expressions like
   `results[?status=='current'].id` produce the expected
   slice.