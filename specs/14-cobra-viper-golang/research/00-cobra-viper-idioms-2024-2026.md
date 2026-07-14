# Cobra + Viper Idioms (2024–2026)

Reference digest for the confluence-mcp project. Each section pulls a snippet verbatim or near-verbatim from a recent source and cites the URL. All snippets were captured in July 2026 from currently-live pages.

Sources used (in order of weight):
1. `golang.elitedev.in` — "Complete Guide to Integrating Cobra with Viper" (Sep 4, 2025) — https://golang.elitedev.in/golang/complete-guide-to-integrating-cobra-with-viper-for-powerful-go-cli-configuration-management-3b02c6a3/
2. `glukhov.org` — "Building CLI Apps in Go with Cobra & Viper" (2024–2025) — https://www.glukhov.org/developer-tools/cli-tools/go-cli-applications-with-cobra-and-viper/
3. `buanacoding.com` — "How to Build a CLI Tool in Go with Cobra and Viper" (Oct 4, 2025, updated Jul 1, 2026) — https://buanacoding.com/2025/10/how-to-build-a-cli-tool-in-go-with-cobra-and-viper.html
4. `opdev.github.io` — "A Primer on Viper / Integrating With Cobra" — https://opdev.github.io/viper-primer/cobra_integration.html
5. `spf13/viper` README + TROUBLESHOOTING.md — https://github.com/spf13/viper
6. `developers-heaven.net` — "Building CLI Tools with Go (Cobra, spf13/viper)" (Jul 30, 2025) — https://developers-heaven.net/blog/building-command-line-interface-cli-tools-with-go-cobra-spf13-viper/

---

## 1. The official precedence order (verbatim from the spf13/viper README)

> "Viper uses the following precedence for merging:
> 1. explicit call to `Set`
> 2. flags
> 3. environment variables
> 4. config files
> 5. external key/value stores
> 6. defaults"
>
> — https://github.com/spf13/viper#putting-values-in-viper

**Reading order in user-facing terms (highest → lowest):** flag → env var → config file → default.
This is the one invariant every source agrees on.

---

## 2. The "BindPFlag must come after the flag is registered" gotcha

From the opdev.io viper-primer — a working demo of the bug, then the fix. This is the cleanest "show the failure" write-up I found in 2024–2025.

### Before binding (broken — viper.GetBool returns false even with --toggle)

```go
var rootCmd = &cobra.Command{
    Use:   "snakes",
    Run: func(cmd *cobra.Command, args []string) {
        toggleValue, _ := cmd.Flags().GetBool("toggle")
        fmt.Println("The toggle flag is set to: ", toggleValue)
        fmt.Println("The toggle config in viper is set to:", viper.GetBool("toggle"))
    },
}

func init() {
    cobra.OnInitialize(initConfig)
    rootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
    // ← no viper.BindPFlag here
}
```

Run with `--toggle`: cobra sees `true`, viper still sees `false`.

### After binding (fixed)

```go
func init() {
    cobra.OnInitialize(initConfig)
    rootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
    viper.BindPFlag("toggle", rootCmd.Flags().Lookup("toggle")) // ← add this
}
```

> "Right now, flag values aren't being stored in our Viper configuration. Luckily, Viper provides a way to bind the flag's value to the configuration."
>
> — https://opdev.github.io/viper-primer/cobra_integration.html

**Key rule:** call `viper.BindPFlag(key, cmd.Flags().Lookup("name"))` *after* the flag is registered. In practice that means inside the same `init()` (or a `PersistentPreRunE` for late-bound flags), and *before* any `Run`/`RunE` fires.

---

## 3. `SetEnvPrefix` + `AutomaticEnv` (verbatim from the spf13/viper README)

```go
// Tells Viper to use this prefix when reading environment variables
viper.SetEnvPrefix("spf")

// Viper will look for "SPF_ID", automatically uppercasing the prefix and key
viper.BindEnv("id")

// Alternatively, we can search for any environment variable prefixed and load
// them in
viper.AutomaticEnv()

os.Setenv("SPF_ID", "13")

id := viper.Get("id") // 13
```

— https://github.com/spf13/viper#working-with-environment-variables

Caveats from the same section:
- env vars are **case sensitive** (unlike viper config keys, which are case-insensitive)
- empty env vars are considered unset unless `viper.AllowEmptyEnv(true)` is called
- viper re-reads env vars on every `Get` — no caching

---

## 4. Idiomatic full pattern (elitedev.in, Sep 2025) — "BindPFlag + AutomaticEnv + cfgFile flag"

This is the closest to a 2025 "blessed" scaffold. It shows the same thing the opdev primer shows, condensed into one block.

```go
var cfgFile string

func init() {
    cobra.OnInitialize(initConfig)
    rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "config file")
    viper.BindPFlag("config", rootCmd.PersistentFlags().Lookup("config"))
}

func initConfig() {
    if cfgFile != "" {
        viper.SetConfigFile(cfgFile)
    }
    viper.AutomaticEnv()
    if err := viper.ReadInConfig(); err == nil {
        fmt.Println("Using config file:", viper.ConfigFileUsed())
    }
}
```

> "Notice how the `--config` flag is bound to Viper? This allows other parts of your application to access the config file path through `viper.GetString("config")`, maintaining a single source of truth."
>
> — https://golang.elitedev.in/golang/complete-guide-to-integrating-cobra-with-viper-for-powerful-go-cli-configuration-management-3b02c6a3/

The author also notes: "Viper automatically handles the order of configuration sources: flags override environment variables, which override config file values, which override defaults."

---

## 5. The "flag → env → config file → default" practical pattern (glukhov.org, 2025)

This is the most complete 2025-era example. Shows the full root.go for a real CLI.

```go
func init() {
    cobra.OnInitialize(initConfig)

    rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "",
        "config file (default is $HOME/.mytasks.yaml)")
    rootCmd.PersistentFlags().String("db", "",
        "database file location")

    viper.BindPFlag("database", rootCmd.PersistentFlags().Lookup("db"))
}

func initConfig() {
    if cfgFile != "" {
        viper.SetConfigFile(cfgFile)
    } else {
        home, err := os.UserHomeDir()
        if err != nil {
            fmt.Fprintln(os.Stderr, err)
            os.Exit(1)
        }

        viper.AddConfigPath(home)
        viper.AddConfigPath(".")
        viper.SetConfigType("yaml")
        viper.SetConfigName(".mytasks")
    }

    viper.SetEnvPrefix("MYTASKS")
    viper.AutomaticEnv()

    if err := viper.ReadInConfig(); err == nil {
        fmt.Println("Using config file:", viper.ConfigFileUsed())
    }
}
```

— https://www.glukhov.org/developer-tools/cli-tools/go-cli-applications-with-cobra-and-viper/

Resulting env-var surface (auto-derived from viper keys, uppercased, prefixed):

```sh
export MYTASKS_DATABASE=/tmp/tasks.db
export MYTASKS_NOTIFICATIONS_ENABLED=false
mytasks list
```

Note the key-shape gotcha: nested viper keys (e.g. `notifications.enabled`) become `MYTASKS_NOTIFICATIONS_ENABLED` — the dots become underscores.

---

## 6. The buanacoding.com (Oct 2025) "kitchen-sink" version — shows the same pattern in a longer scaffold

This is the "production-quality" version with completion, version subcommand, and a separator of `cmd/`, `internal/`, and root config.

```go
func init() {
    cobra.OnInitialize(initConfig)

    rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.tasker.yaml)")
    rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")

    // Bind flags to viper
    viper.BindPFlag("verbose", rootCmd.PersistentFlags().Lookup("verbose"))
}

func initConfig() {
    if cfgFile != "" {
        viper.SetConfigFile(cfgFile)
    } else {
        home, err := os.UserHomeDir()
        if err != nil {
            fmt.Fprintln(os.Stderr, err)
            os.Exit(1)
        }
        viper.AddConfigPath(home)
        viper.SetConfigType("yaml")
        viper.SetConfigName(".tasker")
    }

    viper.AutomaticEnv() // ← key line: picks up MYTASKS_*, etc.
    if err := viper.ReadInConfig(); err == nil {
        if viper.GetBool("verbose") {
            fmt.Println("Using config file:", viper.ConfigFileUsed())
        }
    }
}
```

— https://buanacoding.com/2025/10/how-to-build-a-cli-tool-in-go-with-cobra-and-viper.html

Caveat from the same author: "Best part — it handles priority automatically. Flags override env vars, env vars override config files, config files override defaults."

---

## 7. The "cobra-cli init --viper" scaffold output (opdev.io)

`cobra-cli init --viper=true` generates this root.go — the canonical 2024+ starting point.

```go
package cmd

var cfgFile string

func init() {
    cobra.OnInitialize(initConfig)
    rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "",
        "config file (default is $HOME/.snakes.yaml)")
    rootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}

func initConfig() {
    if cfgFile != "" {
        viper.SetConfigFile(cfgFile)
    } else {
        home, err := os.UserHomeDir()
        cobra.CheckErr(err)
        viper.AddConfigPath(home)
        viper.SetConfigType("yaml")
        viper.SetConfigName(".snakes")
    }
    viper.AutomaticEnv()
    if err := viper.ReadInConfig(); err == nil {
        fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
    }
}
```

— https://opdev.github.io/viper-primer/cobra_integration.html

---

## 8. Practical "flag → env → .env" pattern (synthesized from all five sources)

The standard idiom is: cobra flag → viper.BindPFlag → viper.AutomaticEnv → viper.ReadInConfig (for a regular config file). To add a `.env` file fallback on top, two patterns appear in the wild:

### Pattern A — built-in (no extra dep)
Viper natively reads files in **envfile** format (one `KEY=value` per line). Just set the type:

```go
viper.SetConfigFile(".env")
viper.SetConfigType("env")
_ = viper.ReadInConfig() // ignore "not found" — env vars / flags still work
viper.AutomaticEnv()
```

— https://github.com/spf13/viper#reading-config-files (lists envfile as a natively supported type)

### Pattern B — godotenv + viper (when you need the familiar `.env` UX)
Drop in `github.com/joho/godotenv`, then in `initConfig`:

```go
if err := godotenv.Load(); err != nil {
    // .env is optional — silent fall-through is idiomatic
}
viper.AutomaticEnv() // env vars (including those just loaded) win over the config file
viper.SetEnvPrefix("MYAPP")
if err := viper.ReadInConfig(); err == nil { /* … */ }
```

The order of precedence stays: explicit `Set` → flag (via `BindPFlag`) → env var (which now includes the loaded `.env`) → config file → default. (No single 2024–2026 blog post I found in the top search results combines `godotenv` + `viper` + cobra in one snippet — this synthesis is the conventional Go pattern, drawing on the glukhov precedence order plus the widely-known `godotenv.Load()` call.)

---

## 9. Hyphenated flag → camelCase viper key (opdev primer, again)

> "Viper configurations are typically configured using camelCase or snake_case, but long flags are typically hyphenated. You'll likely want to bind your flag values to appropriate Viper configuration values in cases where you have hyphenated long flags by binding them to equivalent values in your preferred Viper-friendly case."
>
> — https://opdev.github.io/viper-primer/cobra_integration.html

```go
rootCmd.Flags().StringP("log-level", "l", "", "Help message for log level")
viper.BindPFlag("logLevel", rootCmd.Flags().Lookup("log-level"))
```

If the user has a config file, the key there is `logLevel`, not `log-level`. This is why `BindPFlag` takes a separate key argument rather than inferring it from the flag name.

---

## 10. "viper as source of truth" (opdev primer)

> "Once you've bound your Viper configuration to Cobra, you can technically access your user's configuration using either. With that said, it (subjectively) makes sense to leverage your Viper configuration as your source of truth once your cobra flags are bound. This is because you can store the values of your flags in your Viper configuration, but it's not exactly a bi-directional relationship, and your Viper configuration values don't get stored in your cobra flags."
>
> — https://opdev.github.io/viper-primer/cobra_integration.html

Practical implication: in your `Run`/`RunE`, read with `viper.GetString("port")`, not `cmd.Flags().GetString("port")`. The flag is just an *input*; viper is the *output* of the merged config.

---

## 11. What the 2024–2026 sources agree on (consensus)

Pulled from all five posts (no contradictions found):

1. Call `viper.BindPFlag(key, flag.Lookup(name))` **after** the flag is registered.
2. Set `viper.AutomaticEnv()` and optionally `viper.SetEnvPrefix("FOO")` to enable env-var fallback.
3. Call `viper.ReadInConfig()` inside the `cobra.OnInitialize` callback (so `--config` flags get honored).
4. Use `viper.Get*` inside `Run`/`RunE` — treat viper as the merged source of truth.
5. Precedence is **flag > env > config file > default**, every time. This is documented in the official viper README and is not disputed in any of the five posts.

## 12. What the 2024–2026 sources disagree on (or skip)

- **Viper as global singleton vs injected instance.** The official viper README now recommends "the best practice is to initialize a Viper instance and pass that around when necessary" (pkg.go.dev/github.com/spf13/viper), but every blog post above still uses the global `viper.X` form. Worth knowing for testability.
- **HCL support was dropped** (viper 2025-10-08 commit "docs: clenups, remove HCL support").
- **YAML v2 support was dropped** earlier; `viper_yaml3` build tag exists if you hit the unquoted-y/n boolean coercion issue (TROUBLESHOOTING.md).
- **No blog post in the top 10 search results for 2024–2026** does a clean "cobra-only vs cobra+viper" tradeoff. The general sentiment: "for a simple CLI you only need Cobra; add Viper when you need config files / multiple envs / env-var binding" (buanacoding FAQ).

---

## File-level takeaway for the confluence-mcp project

The MCP server we're building is **library code, not a CLI**, so most of the cobra patterns don't apply. The two viper primitives that *do* matter:

- `viper.SetEnvPrefix("CONFLUENCE")` + `viper.AutomaticEnv()` → env-var-driven config for `CONFLUENCE_URL`, `CONFLUENCE_TOKEN`, etc., with the same case-insensitive key semantics documented in §3.
- `viper.SetDefault("timeout", "30s")` → safe defaults that env vars can override, matching the §1 precedence order.

Cite this document in code comments where the env-var / default precedence is relied on.
