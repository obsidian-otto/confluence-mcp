---
title: Cobra + viper design decision for mcp-confluence
product: n/a (Go tooling)
variance: n/a
status: design recommendation (Option B); implementation gated on user OK
companion_to: 01-research-and-surface.md
---

## Purpose

Companion to `01-research-and-surface.md`. That document is the
research deliverable; **this document is the recommendation + the
rationale + the risks**. Implementation (the actual code change to
`cmd/mcp-confluence/`) is a follow-up commit gated on user
approval of the recommendation.

## Recommendation in one paragraph

Adopt **Option B**: add `github.com/spf13/cobra v1.10.2` and
`github.com/spf13/viper v1.21.0` to `go.mod`, write a new
`cmd/mcp-confluence/cli.go` (~120 LOC) that defines a single
flag-only root command, and **keep the existing
`internal/config/dotenv.go` stdlib parser verbatim** — the CLI
flags re-inject into the process environment so the locked
Q22 `.env` pre-order is preserved by composition, not
modification. The minimal observable change is the appearance of
`--help` / `--version` / `--site` / `--email` / `--api-token` /
`--debug` / `--config`; every other behavior (JSON-RPC stream,
tool surface, error envelopes, secret handling) is bit-for-bit
unchanged from the current `v0.1.0` binary.

## Why Option B

| Criterion | Option A (cobra only) | **Option B (cobra+viper, keep stdlib)** | Option C (cobra+viper+godotenv) |
| --------- | -------------------- | --------------------------------------- | ------------------------------ |
| New Go deps | 1 (cobra) | **2 (cobra + viper)** | 3 (+godotenv) |
| Lines added to `cmd/mcp-confluence/` | ~60 | **~120** | ~140 |
| Touches `internal/config/dotenv.go`? | NO | **NO** | YES (violates Q22) |
| Touches `internal/config/dotenv_test.go`? | NO | **NO** | YES (violates Q22) |
| Supports `--config <file>` story? | NO | **YES** | YES |
| Idiom matches 2024-2026 Go conventions | Partial (cobra root + flags only; no config layering) | **Full (matches every blog post surveyed)** | Full |
| Risk | Low | **Low-to-medium (new abstractions, but no invariants broken)** | High — re-opens locked Q22 |

The decisive factor is **Q22**, which locks the 30-LOC stdlib
`.env` parser and its 171-line test module. Options A and B
both keep the parser; Option C does not. Between A and B, B
matches the canonical cobra+viper idiom every 2024-2026 source
[16][17][18][19][20] uses, which makes the generated code
Google-able for the next contributor. The marginal cost of
adopting viper is `+~60 LOC` and `+1 dep` — both within budget
for this project.

## What is NOT changing

- `internal/config/dotenv.go` — Q22-locked, untouched.
- `internal/config/dotenv_test.go` — Q22-locked, untouched.
- `internal/config/config.go` — the `*Config` type and its
  `LoadFromEnv()` entry point stay; CLI flags feed into
  `LoadFromEnv` via the process environment.
- `internal/tools/descriptions.go` — verbatim upstream
  descriptions stay verbatim; no CLI wording change.
- `internal/tools/register.go` — 17→18 tool count stays
  unchanged (CLI is orthogonal to tool registration).
- `cmd/mcp-confluence/main.go`'s `runLifecycle`, `run`,
  `runUntilDone`, `wireStdinEOF` — all unchanged. The CLI
  handler calls `run()` exactly as `main()` does today;
  the only difference is that CLI flags have already been
  resolved into the process env by then.
- `Makefile` — no new target; `make build` / `make test`
  continue to work because cobra + viper compile under
  `CGO_ENABLED=0` (both are pure Go with no cgo deps).
- `scripts/smoke-page-tree.py` — the live-smoke driver
  continues to work unchanged; the binary still
  accepts the same stdio JSON-RPC stream.

## What IS changing

- `go.mod` — two new entries: `github.com/spf13/cobra v1.10.2`
  + `github.com/spf13/viper v1.21.0`. Both are pure Go (no cgo),
  so `CGO_ENABLED=0` is preserved.
- New file `cmd/mcp-confluence/cli.go` (~120 LOC).
- New test file `cmd/mcp-confluence/cli_test.go` (~80 LOC)
  — at minimum:
  - `TestRoot_Help` — `--help` exits 0, no stdout, usage on stderr.
  - `TestRoot_Version` — `--version` exits 0, prints version on stderr.
  - `TestRoot_FlagsOverrideEnv` — `--site=foo` beats `ATLASSIAN_SITE_NAME=bar`.
  - `TestRoot_Q22DotenvOrderUnchanged` — the stdlib .env parser still fires after flag resolution.
- `cmd/mcp-confluence/main.go` — the current 246-line main.go
  is reorganized so `main()` calls `Execute()` instead of
  `run()` directly; the existing `run()` / `runLifecycle()` /
  `serveUntilDone()` / `wireStdinEOF()` functions stay where
  they are, called unchanged from the new CLI handler.
  Net line delta: ~0 LOC for `main.go` (a `main()` rewrite).

## Risk register

| Risk | Likelihood | Mitigation |
| ---- | ---------- | ---------- |
| Cobra's stdout writes break the JSON-RPC channel | HIGH if not mitigated | `rootCmd.SetOut(io.Discard)` AND `rootCmd.SetErr(os.Stderr)` before `Execute()` |
| Viper flag/env precedence flips the Q22 `.env` order | LOW | Composition path: flags re-inject into `os.Setenv` so `LoadFromEnv`'s process-env tier wins first, then dotenv's cwd tier, then binary-dir tier — same as today |
| Cobra's `--help` calls `os.Exit(0)` from inside flag parsing | LOW | `RunE: runServer` returns an error; `SilenceUsage: true` keeps the usage banner from re-printing on validation failure |
| Hermes re-launches binary with `args: []` and chokes on the new CLI argument parser | LOW | Root command's `RunE` is reached when no subcommand/flag is given; `args: []` produces the same behavior as today |
| Cobra's `flag.Args()` positional args break (no positionals today) | NONE — no positionals in mcp-confluence | n/a |
| Viper brings in a cgo dependency | NONE — viper is pure Go | n/a |
| New `cobra` `init()` ordering affects `main_test.go`'s lifecycle tests | MEDIUM | Tests call `runLifecycle` directly, bypassing `Execute()`; the new `cli.go` does not touch `runLifecycle`'s signature. A new `TestMain_CLI*` test covers the CLI path separately. |

## Implementation sequencing (when user approves Option B)

Three commits (per the per-step git-commit rule):

1. **Commit 1**: add cobra + viper to `go.mod` and `go.sum`;
   create `cmd/mcp-confluence/cli.go` scaffolding + flag wiring;
   `make build` green; `make test` green (no behavioral change yet,
   the new binary is functionally identical to today because
   `runServer` is unreachable from any flags a human would
   commonly pass).

2. **Commit 2**: wire the flag-handler to `run()` via the
   process-env composition path; add `cmd/mcp-confluence/cli_test.go`;
   `make test` and `make check` green; verify `--help` exits
   cleanly with no stdout pollution (this is the load-bearing
   invariant check).

3. **Commit 3**: live smoke against `smartergroup.atlassian.net`
   using `scripts/smoke-page_tree.py`; document the CLI surface
   in `AGENTS.md` and `make help` (a one-line addition to the
   header comment).

Each commit is independently buildable and independently green
on `make check`.

## Open questions for the user

1. **Option B (recommended) — proceed, or would you prefer A
   (smaller diff, no viper)?** If A, the spec shrinks to
   "cobra + flags only" and there's no config-file or
   viper-unmarshal story.
2. **`--api-token` vs `--token`** — recommendation: `--api-token`
   (matches the `ATLASSIAN_API_TOKEN` env name; matches the
   upstream Node tool). Shorter `--token` is fine if you
   prefer brevity.
3. **Should the binary expose a `version` subcommand too?**
   Recommendation: not in this commit. The root command's
   `--version` flag is enough for operator use; a subcommand
   adds asymmetry that could confuse the Hermes
   `args: ["version"]` distinction.
4. **Edit the existing `cmd/mcp-confluence/main.go` for the
   CLI plumbing, or add a sibling `cmd/mcp-confluence/cli.go`?**
   Recommendation: sibling. Keeps the diff to `main.go` near
   zero and lets `cli_test.go` exercise the CLI without
   touching the existing `main_test.go` lifecycle tests.
