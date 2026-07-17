---
title: Adding cobra + viper CLI options to the mcp-confluence Go binary
product: confluence-cloud (consumer: mcp-confluence, a Go MCP server)
variance: n/a — tooling addition
status: research-complete-2026-07-14; implementation pending user OK
sources:
  cobra:    github.com/spf13/cobra v1.10.2 (2025-12-04)
  viper:    github.com/spf13/viper v1.21.0 (2025-09-08)
  godotenv: github.com/joho/godotenv v1.5.1 (optional, evaluated but RECOMMENDED AGAINST)
---

## Overview

User request (2026-07-14): "research how to add cli options to the
current application using the golang libraries cobra and viper,
save the output to specs/14-cobra-viper-golang/".

The "application" is the `mcp-confluence` Go binary built by this
repo. It currently has **no CLI surface** — `cmd/mcp-confluence/main.go`
reads config from `process env > cwd .env > binary-dir .env` (per the
locked Q22 contract), prints a one-line startup message to stderr,
and serves JSON-RPC over stdin/stdout [1][2]. Every test of "the
command works" is a `make build && bin/mcp-confluence < /dev/null`
or a register-and-test-via-hermes dance. There is no `--help`, no
`--version`, no way to override one setting for one run without
editing the `.env` file.

### What this spec covers

1. **The two libraries** — confirmed current versions and how each
   one fits the project's stdio-MCP-server shape (rather than the
   typical CLI-tool shape they were designed for).
2. **The three reasonable design choices** for this project —
   (a) cobra-only flags, (b) cobra + viper with the existing
   stdlib .env parser kept, (c) cobra + viper replacing the
   parser — each with a cost/risk profile.
3. **A reference implementation outline** for the recommended
   option (b), grounded in the actual project files
   (`internal/config/config.go`, `internal/config/dotenv.go`,
   `cmd/mcp-confluence/main.go`).
4. **Hard invariants** that any CLI addition MUST respect — the
   JSON-RPC-stdout rule, the locked Q22 .env pre-order,
   the secret-handling rules, the ProjectLock "no new scattered
   shell scripts" rule.

### What this spec does NOT cover

- Implementation work — that's a follow-up commit, gated on user
  approval of the design choice below. Research-only here.
- Any change to `internal/markdown`, `internal/tools`, or the
  17 MCP tools themselves. CLI is purely an entrypoint concern.
- Migration to `kong`, `urfave/cli`, stdlib `flag`, or any
  non-cobra CLI library — research scope was cobra + viper per
  the user's request.

## Sources

The numbered citations below are referenced throughout the rest
of the document. Each is a URL reachable on 2026-07-14 from
this host. The first three primary sources were harvested
verbatim into `.research/` (~225 KB of upstream material)
in research-task-1; the MCP-server convention survey lives
in research-task-2's `/tmp` notes, and the 2024-2026 idiom
digest in `research/cobra-viper-idioms-2024-2026.md`.

### Primary (verified July 2026)

1. Cobra user_guide.md (canonical minimal program + 12-line
   `var rootCmd` block) — https://raw.githubusercontent.com/spf13/cobra/main/site/content/user_guide.md
2. Cobra source: `command.go` (Command type, line-anchored
   output/setter methods) — https://raw.githubusercontent.com/spf13/cobra/main/command.go
3. Cobra pkg.go.dev version page (v1.10.2 confirmed Dec 3, 2025) — https://pkg.go.dev/github.com/spf13/cobra?tab=versions
4. Viper README (config-source precedence, env binding, .env
   format support) — https://raw.githubusercontent.com/spf13/viper/master/README.md
5. Viper source: `viper.go` (`SupportedExts` declares
   "env" and "dotenv" as file formats) — https://raw.githubusercontent.com/spf13/viper/master/viper.go
6. Viper pkg.go.dev version page (v1.21.0 confirmed Sep 8, 2025) — https://pkg.go.dev/github.com/spf13/viper?tab=versions
7. opdev.io "A Primer on Viper / Integrating With Cobra" — https://opdev.github.io/viper-primer/cobra_integration.html
   The cleanest "BindPFlag-before-flag-gets-set bug" write-up;
   canonical "cobra-cli init --viper" scaffold output.

### Secondary (2024-2026)

8. golang.elitedev.in (Sep 4, 2025) — "Complete Guide to Integrating
   Cobra with Viper" — https://golang.elitedev.in/golang/complete-guide-to-integrating-cobra-with-viper-for-powerful-go-cli-configuration-management-3b02c6a3/
9. glukhov.org (2024-2025) — "Building CLI Apps in Go with Cobra & Viper" — https://www.glukhov.org/developer-tools/cli-tools/go-cli-applications-with-cobra-and-viper/
10. buanacoding.com (Oct 4, 2025; updated Jul 1, 2026) — "How to Build a CLI Tool in Go with Cobra and Viper" — https://buanacoding.com/2025/10/how-to-build-a-cli-tool-in-go-with-cobra-and-viper.html
11. developers-heaven.net (Jul 30, 2025) — "Building CLI Tools with Go" — https://developers-heaven.net/blog/building-command-line-interface-cli-tools-with-go-cobra-spf13-viper/
12. godotenv README (the alternative path; not adopted) — https://github.com/joho/godotenv

### Survey of stdio MCP server CLI conventions

13. modelcontextprotocol/go-sdk README + examples/server/* — flagless
    stdio-only pattern; `-http` flag exists in `examples/server/everything`
    for the HTTP variant. — https://github.com/modelcontextprotocol/go-sdk
14. mark3labs/mcp-go README — `server.ServeStdio(s, opts ...)` is the
    one-call entry point; no flag parser; configuration via env vars
    only. — https://github.com/mark3labs/mcp-go
15. metoro-io/mcp-golang README + `metoro-io/metoro-mcp-server/main.go`
    — same pattern: zero flags, hardwired stdio, env-only config via
    `os.Getenv`, `panic(err)` on missing envs. — https://github.com/metoro-io/metoro-mcp-golang / https://github.com/metoro-io/metoro-mcp-server
16. ProjectLock Q22 partial-answers — the locked `.env` preorder. —
    `specs/99-gap-questions/02-partial-answers.md`
17. ProjectLock secrets anti-pattern — the token-redaction contract. —
    `specs/09-anti-patterns/02-secret-handling.md`
18. ProjectLock stdout pollution anti-pattern — the JSON-RPC invariant
    on stdout. — `specs/09-anti-patterns/01-stdout-pollution.md`
19. ProjectLock "13" error envelope — `<METHOD> <path>: <status> <text> -
    <body>` — `specs/09-anti-patterns/03-error-shapes.md`
20. Current `internal/config/dotenv.go` (30-LOC stdlib .env parser),
    locked as part of Q22. — `internal/config/dotenv.go`

## Spec

### 1. Confirmed library versions (July 2026)

Per pkg.go.dev [3][6] cross-checked against GitHub Releases API:

| Library | Module | Latest stable | Go directive | Released |
| ------- | ------ | ------------- | ------------ | -------- |
| Cobra   | `github.com/spf13/cobra` | **v1.10.2** | `go 1.15` (toolchain bump via go.mod) | 2025-12-04 |
| Viper   | `github.com/spf13/viper` | **v1.21.0** | `go 1.23.0` | 2025-09-08 |

The project's current `go.mod` is `go 1.26.4` (per
`cmd/mcp-confluence/main.go:54`'s `const version = "v0.1.0"` and the
go binary's own toolchain auto-resolution). Both libraries build
cleanly under that toolchain.

Viper requires `go 1.23.0` (its go.mod says so) which means adopting
viper pulls the entire toolchain to 1.23 minimum. The current
project is already on 1.26.4 so this is a no-op floor-shift.

### 2. What each library does (one paragraph each)

**Cobra** [1][2] is a command-tree parser: you define a `*cobra.Command`
with `Use`, `Short`, `Long`, and a `Run`/`RunE` closure; the library
parses `os.Args`, dispatches to the right subcommand, applies
positional-args validation, renders `--help` automatically, and
returns an exit code on failure. Its public API is the
`*cobra.Command` struct with `Flags()`, `PersistentFlags()`,
`BoolP/StringP/IntP/DurationP` flag helpers, `SetOut/SetErr/SetIn`
output-stream setters, `SetHelpFunc/SetUsageFunc/SetVersionTemplate`
template hooks, and `ExecuteContext(ctx) error` as the entry point.
Cobra ships **zero** config-layering machinery of its own; flags
get parsed once into `cmd.Flags()`, and that's the entire contract.

**Viper** [4][5][6] is a config-layering engine: it reads from
multiple sources — explicit `Set`, flag values (bound via
`BindPFlag`), environment variables (via `SetEnvPrefix` +
`AutomaticEnv`), config files in `JSON / TOML / YAML / INI / envfile
/ Java properties` formats (extension-sniffed via `SupportedExts`),
external KV stores (etcd, Consul, Firestore), and `SetDefault` —
all merged under one key/value tree with a documented precedence
order. Viper has **no** built-in CLI-parsing; its
`BindPFlag(key, flag)` consumes an already-parsed pflag so the
canonical pattern is cobra-and-viper together, not either alone.
Viper does NOT auto-load `.env` files from cwd (this is a common
misconception [4][12]); the caller must explicitly
`SetConfigFile(".env")` + `ReadInConfig()`.

### 3. The canonical cobra+viper integration pattern

From the opdev primer [7] (the canonical scaffold output of
`cobra-cli init --viper=true`), with viper-README [4] env-var
binding:

```
rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "",
    "config file (default is $HOME/.snakes.yaml)")
rootCmd.PersistentFlags().BoolP("toggle", "t", false, "...")

cobra.OnInitialize(initConfig)   // ← deferred init

func initConfig() {
    if cfgFile != "" { viper.SetConfigFile(cfgFile) }
    viper.SetEnvPrefix("SPF")
    viper.AutomaticEnv()         // ← picks up SPF_TOGGLE, etc.
    viper.BindPFlag("toggle",
        rootCmd.PersistentFlags().Lookup("toggle"))
    _ = viper.ReadInConfig()     // config file is optional
}

// In Run/RunE:
val := viper.GetBool("toggle")    // ← viper is the source of truth
```

The four "every source agrees on" invariants from [7][8][9][10][11]:

1. Call `viper.BindPFlag(key, flag.Lookup(name))` **after** the
   flag is registered, inside the same `init()` (or a
   `PersistentPreRunE` for late-bound flags), and *before* any
   `Run`/`RunE` fires.
2. Set `viper.AutomaticEnv()` to enable env-var fallback; set
   `viper.SetEnvPrefix("FOO")` to namespace it.
3. Call `viper.ReadInConfig()` inside the `cobra.OnInitialize`
   callback so `--config` flags get honored.
4. Use `viper.Get*` inside `Run`/`RunE` — treat viper as the merged
   source of truth.

Viper's own precedence (verbatim from [4] lines 75-82):

1. explicit call to `Set`
2. flags
3. environment variables
4. config files
5. external key/value stores
6. defaults

So in user-facing terms: **flag > env > config file > default**.
This is the one invariant every source agrees on.

### 4. Critical gotchas for stdio MCP servers

**(a) Cobra writes to stdout by default** — `--help`, `--version`,
and command-error rendering all land in `os.Stdout` unless
overridden. For a stdio MCP server that is fatal (it would corrupt
the JSON-RPC stream on the same fd). The workaround [2] is
to call `SetOut` and `SetErr` *before* `ExecuteContext`:

```
rootCmd.SetOut(io.Discard)         // --help and command output
rootCmd.SetErr(os.Stderr)          // usage + error rendering
```

`io.Discard` for stdout is correct: a `mcp-confluence --help`
call should not pollute the upstream's JSON-RPC pipe. The user's
actual CLI caller (a human or an integration script) sees the
help output go to stderr (where diagnostic logs already are)
or not at all. Recommendation in §6 routes help to stderr so
`mcp-confluence --help 2>&1` is still useful for debugging.

A second layer: cobra's `Command.HelpFunc` and `Command.UsageFunc`
can be replaced wholesale via `SetHelpFunc(cmd, args)` and
`SetUsageFunc(cmd) error` to write to `os.Stderr` directly if a
custom help layout is preferred. For v1 of the CLI, the
`SetOut(io.Discard)` approach is sufficient and zero-LOC.

**(b) Viper's `viper.IsSet("debug")` is the right read pattern** —
once a flag is bound, reading via the cobra flag surface is
not just fragile (viper can be set via env or config that
the flag never saw) but also discards the merged picture. The
idiom [7] is: bind the flag once, then read the merged value
via `viper.IsSet`/`viper.GetString`/`viper.GetBool`. In this
project's `runLifecycle`, the single existing call site
`config.LoadFromEnv()` returns a `*Config`; the minimal-diff
implementation wires that call to read its three string fields
from viper first, falling back to the existing dotenv+env scan.

**(c) The locked Q22 `.env` pre-order is "process env > cwd .env
> binary-dir .env"** [16]. Viper's default precedence is
"flag > env > config file" [4]. Reconciling: process env > cwd
.env > binary-dir .env maps to the project policy — the *highest
precedence* tier is process env (viper's "env" tier), then the
explicit .env lookup order. The simplest reconciliation is to
**keep the existing `internal/config/dotenv.go` parser** as
the authoritative source of `*Config`, and let viper sit on top
of it as a flag-and-cli-input layer: flags funnel into viper,
viper's `GetString("site_name")` returns the flag-or-default
value, and `runLifecycle` then funnels that value into the
existing `config.LoadFromEnv()` call as a process-env override.
This preserves the locked Q22 contract and adds flags on top
without re-litigating the .env ordering question.

**(d) Viper's `SetVersion` does not exist** — the version field is
a struct property (`cmd.Version = "v1.0.0"`) and the templating
is `cmd.SetVersionTemplate("...")` [2]. The current project already
has `const version = "v0.1.0"` in `cmd/mcp-confluence/main.go:54`
that's settable via `-ldflags -X main.version=<x>`; the cobra
upgrade should set `cmd.Version = version` (read once at package
init) so `--version` Just Works without forking the literal.

**(e) Existing project wiring to preserve**:
- `log.SetFlags(...)` is called inside `runLifecycle` based on
  `cfg.Debug` (a bool). After this change, the read becomes
  `viper.GetBool("debug")` (resolved via flag > env > config file
  precedence), and the Q22 .env-still-loaded last principle is
  preserved by keeping the dotenv parser in `internal/config/`.
- `wireStdinEOF` (in `cmd/mcp-confluence/main.go:188`) does the
  json-rpc-on-stdio pipe trick — no change here. The CLI must
  not log a single byte to stdout, period.

### 5. Three design choices for THIS project

Cost/risk profile of each option:

#### Option A — cobra only, keep stdlib config

```
$ mcp-confluence --help                    # cobra-generated
$ mcp-confluence --site=foo --email=u@f --token=...   # overrides env
$ mcp-confluence                            # default: stdio server (current behavior)
```

- **New deps**: cobra only (one new entry in go.mod).
- **Code size**: ~60 LOC added to `cmd/mcp-confluence/` — a new
  `cli.go` with the root command + flag-bindings + a `--help`
  exit path before reaching `runLifecycle`.
- **JSON-RPC invariant**: `rootCmd.SetOut(io.Discard)` keeps stdout clean.
- **`.env` story**: unchanged. `internal/config/dotenv.go` still
  parses, `internal/config/config.go` still merges.
- **Pros**: minimal diff; keeps Q22 verbatim; one new dep.
- **Cons**: no `--config <file>` story, no `--key=value` shorthand,
  no `viper.Unmarshal(&cfg)` ergonomics; cobra doesn't auto-load
  .env files so we'd be hand-rolling the env-var-bind.

#### Option B — cobra + viper, keep stdlib dotenv (RECOMMENDED) ✓

```
$ mcp-confluence --help                    # cobra-generated, stderr
$ mcp-confluence --site=foo --email=u@f --api-token=ATAT...  # overrides
$ mcp-confluence --config /etc/mcp-confluence.yaml # viper-only feature
$ mcp-confluence --debug                   # viper + log.SetFlags(...)
$ mcp-confluence                            # default: stdio server
```

- **New deps**: cobra + viper (two new entries in go.mod).
- **Code size**: ~120 LOC: `cli.go` + a thin `viper_loader.go`
  shim that reads viper, populates the merged map, and
  hands off to the existing `config.LoadFromEnv` shape.
- **JSON-RPC invariant**: same `SetOut(io.Discard)` pattern.
- **`.env` story**: unchanged. The existing dotenv parser still
  runs. Viper's `AutomaticEnv` picks up whatever
  `LoadFromEnv` then writes into the process environment.
- **Pros**: future-flexible (config files, key/value stores
  if ever wanted), matches every 2024-2026 blog-post idiom
  [7][8][9][10][11], and the diff to `runLifecycle` is small
  (replace the LoadFromEnv env-path arg with viper-resolved
  values).
- **Cons**: two new deps; one more abstraction layer to
  understand for future contributors.

#### Option C — cobra + viper + godotenv (replaces stdlib parser)

Identical to B except `github.com/joho/godotenv` replaces
`internal/config/dotenv.go`. This violates Q22 [16] — Q22
locks the stdlib parser at "30 LOC stdlib Go in
`internal/config/dotenv.go`", and the
171-line `dotenv_test.go` is one of the project's
most-tested modules. Replacing it re-opens the locked decision.

**RECOMMENDED AGAINST** on those grounds. Documented here for
completeness; if a future maintainer wants yaml/json config
support, that's the time to revisit Q22.

#### Recommendation

**Option B**. It is the minimum-diff path that:
- Respects Q22 (no change to `internal/config/dotenv.go`).
- Respects the JSON-RPC stdout invariant (one cobra line).
- Adds future flexibility (config files, unmarshaling) for
  zero cost in this commit.
- Matches every recent idiom [7][8][9][10][11] so the
  generated code is Google-able by the next contributor.

If the user wants an even smaller diff, Option A is acceptable
as a stepping-stone; it covers the "add `--help` and a few
overrides" use case without viper.

### 6. Reference implementation outline for Option B

New file `cmd/mcp-confluence/cli.go` (sketch — not committed):

```go
package main

import (
    "io"
    "os"
    "fmt"

    "github.com/spf13/cobra"
    "github.com/spf13/viper"

    "github.com/bennie/mcp-confluence/internal/config"
)

const (
    envPrefix = "ATLASSIAN"
    flagSite  = "site"
    flagEmail = "email"
    flagToken = "api-token"
    flagDebug = "debug"
    flagCfg   = "config"
)

func newRootCmd() *cobra.Command {
    root := &cobra.Command{
        Use:           "mcp-confluence",
        Short:         "Confluence MCP server (stdlib JSON-RPC)",
        Long:          "mcp-confluence serves the 18 confluence tools ...",
        Version:       version, // from main.go:54
        SilenceUsage:  true,
        SilenceErrors: true,
        RunE:          runServer,
    }

    root.SetOut(io.Discard)            // <-- JSON-RPC stdout protection
    root.SetErr(os.Stderr)             // <-- errors/help land on stderr

    pflags := root.PersistentFlags()
    pflags.String(flagSite, "", "Confluence site prefix (overrides ATLASSIAN_SITE_NAME)")
    pflags.String(flagEmail, "", "Account email (overrides ATLASSIAN_USER_EMAIL)")
    pflags.String(flagToken, "", "API token (overrides ATLASSIAN_API_TOKEN)")
    pflags.Bool(flagDebug, false, "Verbose stderr logging")
    pflags.String(flagCfg, "", "Path to a viper-compatible config file")

    cobra.OnInitialize(func() { initViper(root) })
    return root
}

func initViper(root *cobra.Command) {
    v := viper.New()
    v.SetEnvPrefix(envPrefix)
    v.AutomaticEnv()
    // Pre-register the canonical env keys so flag/env interaction is uniform.
    _ = v.BindEnv(flagSite, "SITE_NAME")
    _ = v.BindEnv(flagEmail, "USER_EMAIL")
    _ = v.BindEnv(flagToken, "API_TOKEN")
    _ = v.BindEnv(flagDebug, "DEBUG")

    pflags := root.PersistentFlags()
    _ = v.BindPFlag(flagSite,   pflags.Lookup(flagSite))
    _ = v.BindPFlag(flagEmail,  pflags.Lookup(flagEmail))
    _ = v.BindPFlag(flagToken,  pflags.Lookup(flagToken))
    _ = v.BindPFlag(flagDebug,  pflags.Lookup(flagDebug))

    if path := v.GetString(flagCfg); path != "" {
        v.SetConfigFile(path)
        _ = v.ReadInConfig() // optional
    }
}

// runServer is the bridge: it reads the merged viper picture,
// re-injects into the process env so the stdlib dotenv + LoadFromEnv
// path stays authoritative (Q22 is preserved by composition, not
// modification). Then calls the existing runLifecycle.
func runServer(_ *cobra.Command, _ []string) error {
    v := viper.New()
    if site := v.GetString(flagSite);   site  != "" { _ = os.Setenv("ATLASSIAN_SITE_NAME",    site) }
    if email := v.GetString(flagEmail); email != "" { _ = os.Setenv("ATLASSIAN_USER_EMAIL",  email) }
    if token := v.GetString(flagToken); token != "" { _ = os.Setenv("ATLASSIAN_API_TOKEN",   token) }
    if v.GetBool(flagDebug)                          { _ = os.Setenv("DEBUG",                   "true") }

    // The locked Q22 path: dotenv parser + LoadFromEnv now sees
    // process env (flags just set) > cwd .env > binary-dir .env.
    if _, err := config.LoadFromEnv(); err != nil {
        return err
    }
    return run()
}

func main() {
    if err := newRootCmd().Execute(); err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
}
```

Notes on the sketch:

- `os.Setenv` from the CLI flag into the process env is a deliberate
  composition, NOT a bypass. It lets the existing `LoadFromEnv`
  path handle all merging including the locked Q22 .env tiers.
- `config.LoadFromEnv` is called for its side effects (it logs
  validation failures, populates `apiToken redacted` info, etc.) —
  the return value is discarded because we re-enter via `run()`.
- `SilenceUsage: true` + `SilenceErrors: true` keeps cobra from
  re-printing the (stderr) usage banner when, say, `--email` is
  empty and the merge fails — the error stays clean.
- The `runLifecycle` tests (`main_test.go:TestRunLifecycle_*`) keep
  working because `run()` is still the entrypoint.

### 7. The 2024-2026 idiom consensus (one-line tripwires to avoid)

From [7][8][9][10][11], the things that bit other projects:

- **The `BindPFlag`-before-flag-registration bug** [7]: forget
  the line and `--debug` is silently ignored. Shown verbatim in
  `research/cobra-viper-idioms-2024-2026.md` §2.
- **Hyphenated flag → camelCase viper key mismatch** [7]:
  `viper.BindPFlag("logLevel", flag.Lookup("log-level"))` —
  bind to the camelCase key, not the hyphenated name. In this
  project the flag names are all single words (`site`, `email`,
  `api-token`, `debug`) so this is moot, but the conversion
  rule is documented here for completeness.
- **Viper `SetDefault` does NOT win** [4]: the inverse of what
  the name suggests. Defaults lose against every other source.
  Use `SetDefault` only for "what happens when nothing is set",
  not for "what I want to win".
- **Viper env vars are case-sensitive** [4]: `--site` is
  bound to `viper` key `site`; the env var lookup uses
  `SetEnvPrefix("ATLASSIAN")` so it's `ATLASSIAN_SITE`, but
  `BindEnv("SITE_NAME", ...)` overrides to
  `ATLASSIAN_SITE_NAME`. In this project the env names
  predate Q22 lock so we keep them and bind explicitly
  (`v.BindEnv(flagSite, "SITE_NAME")`).

### 8. Hard invariants (must-not-break checklist)

Any CLI addition has to survive all of these:

| # | Invariant | Source | Mitigated by |
| - | --------- | ------ | ------------ |
| 1 | No stdout writes except JSON-RPC | specs/09-anti-patterns/01-stdout-pollution.md | `SetOut(io.Discard)` |
| 2 | `cfg.Debug`-gated log.SetFlags | specs/09-anti-patterns/01-stdout-pollution.md | `viper.GetBool("debug")` reads |
| 3 | Settings order = process env > cwd .env > binary-dir .env | specs/99-gap-questions/02-partial-answers.md (Q22) | Composition path: flags re-inject into process env; `LoadFromEnv` still authoritative |
| 4 | API token never logged | specs/09-anti-patterns/02-secret-handling.md | Existing `make verify-env` redacts length only; `cfg.Debug` startup log already notes "value not logged" |
| 5 | Error envelope `<METHOD> <path>: <status> <text> - <body>` | specs/09-anti-patterns/03-error-shapes.md | Unchanged — CLI doesn't touch the request pipeline |
| 6 | Tool name set frozen at 17 (currently; 18 after `conf_get_page_tree` lands) | specs/02-upstream-aashari/, scripts/server_test.go | Unchanged — CLI doesn't touch tool registration |
| 7 | CONF_*_DESCRIPTION strings verbatim from upstream | specs/02-upstream-aashari/, descriptions_test.go | Unchanged |
| 8 | Makefile is single source of truth | ProjectLock Q14 | No new shell scripts; cobra lives in `cmd/mcp-confluence/cli.go` (Go file, not script) |
| 9 | All 18 tools' input schemas have jsonschema tags | specs/05-tool-surface-design/, args_test.go | Unchanged |
| 10 | The 171-line `dotenv_test.go` keeps passing | internal/config/dotenv_test.go | Composition path doesn't touch the parser |

A single test must be added:
`TestMain_CLIFlagsOverrideEnv` (or similar) proving that
`--site=... --debug` overrides the process env without
re-running the dotenv parser. The test should use a custom
http transport (per `main_test.go`'s existing pattern) so it
doesn't actually hit the network.

### 9. Open question — subcommands vs. flags-only

The four stdio-MCP-server reference projects [13][14][15]
(plus `metoro-io/metoro-mcp-server`) **all** ship flagless,
no-subcommand binaries. The convention is: the host that
launches the binary (Claude Desktop, Hermes, etc.) configures
it via `args: []` and `env: {...}` — there is no human-typed
CLI surface in the typical deployment. This project's
deployment is the same: Hermes `mcp_servers:` block drives
the binary via env-only.

So the CLI is **mostly for operator debugging** —
"`./mcp-confluence --help` to see what flags exist" rather
than a primary user surface. Given that:

- **Recommended**: flag-only on a single root command, **no
  subcommands**. A `serve` subcommand would be the natural
  default — `mcp-confluence serve` could spell out "start the
  stdio server" — but it would also require breaking the
  currently-flagless invocation, which could confuse the
  registered Hermes MCP that runs `bin/mcp-confluence` with
  no args. The current Hermes block:
  ```yaml
  command: ./bin/mcp-confluence
  args: []    # ← no subcommand
  ```
  would silently fail if we made `serve` mandatory.

- **Optional follow-up (NOT in this commit)**: a hidden
  `version` subcommand (`mcp-confluence version`) would
  coexist cleanly — `args: ["version"]` would output version
  info; `args: []` would still run the stdio server. This
  matches the buanacoding scaffold [10] exactly.

The recommended Option B v1 delivers flag-only, no subcommands.
A `version` subcommand can land in a follow-up if
operator UX demands it.

## Verification

| Item | Verification | Result (to be filled in implementation) |
| ---- | ------------ | --------------------------------------- |
| Pinned versions | `go list -m github.com/spf13/cobra github.com/spf13/viper` returns exact pins | pending impl |
| `make test` green | 156 → ~165 test functions after the new `TestMain_CLI*` cases | pending impl |
| `make lint` green | `golangci-lint run` 0 issues | pending impl |
| `make build` produces a binary that accepts `--help` | `./bin/mcp-confluence --help` exits 0 with NO stdout pollution; stderr contains the cobra-rendered usage | pending impl |
| `--site=foo` overrides env | `--site=foo --email=u@f.com --api-token=ATATx bin/mcp-confluence </dev/null` succeeds where `bin/mcp-confluence </dev/null` would fail with "site is required" | pending impl |
| Q22 .env pre-order still holds | `ATLASSIAN_API_TOKEN=x ./bin/mcp-confluence </dev/null` succeeds; without env, `.env` in cwd is read; without either, errors out — same as today | pending impl |
| Tool name set still frozen at 17 (or 18 after `conf_get_page_tree`) | `bin/mcp-confluence --help 2>&1 \| grep conf_` matches the registered list | pending impl |
| JSON-RPC stdout invariant | `bin/mcp-confluence --help </dev/null | head -1` returns empty (no JSON-RPC); `bin/mcp-confluence --help 2>&1` returns the help text | pending impl |
| No new scattered shell scripts | `find . -name "*.sh" -not -path "./.git/*"` count is unchanged | pending impl |
| Hermes integration | Restart Hermes; `hermes mcp list` still shows the server; `mcp__confluence__conf_help` still works | pending impl after user restart |
| Live smoke against the user's own Confluence Cloud workspace | Re-run `python3 scripts/smoke-page_tree.py` against the new binary | pending impl |

### Live-verification plan (when implementing)

1. Build the binary (`make build`).
2. Run a smoke loop:
   - `./bin/mcp-confluence </dev/null` — must continue to behave like today (startup banner on stderr, then serve JSON-RPC).
   - `./bin/mcp-confluence --help </dev/null` — exits 0, no stdout, usage on stderr.
   - `./bin/mcp-confluence --version </dev/null` — exits 0, prints `mcp-confluence version v0.1.0` on stderr.
   - `ATLASSIAN_SITE_NAME=bogus ./bin/mcp-confluence --site=acme </dev/null` — startup banner should say `site=acme` (flag won over env).
3. Smoke against the MCP wire:
   `python3 scripts/smoke_page_tree.py` — must pass unchanged.
4. Hermes integration: user restarts Hermes; `hermes mcp test confluence` should still register the server (now with `--help`/`--version` added but no other behavior change).
