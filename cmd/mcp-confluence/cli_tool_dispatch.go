// cmd/mcp-confluence/cli_tool_dispatch.go
//
// Phase 20 — per-tool CLI subcommands. This file holds the
// reflection-driven flag-binding helper and the runToolInvocation
// adapter that bridges cobra's RunE closure into the existing
// internal/tools.Handle* handlers.
//
// Design contract (do not change without a spec):
//
//  1. The 18 MCP tool handlers in internal/tools all share the
//     same signature:
//
//     func HandleXxx(ctx context.Context, client *atlassian.Client,
//     args json.RawMessage) (string, error)
//
//  2. Their args structs (e.g. GetArgs, PostArgs) carry
//     `jsonschema:"description=...,required"` tags that the
//     metoro-io/mcp-golang framework reads at registration
//     time. We RE-USE the same tags here, via reflection, to
//     build cobra flag bindings — so a schema change in
//     internal/tools/args.go automatically flows into the CLI
//     help text with no manual duplication.
//
//  3. The CLI dispatch is the ONE legitimate stdout writer in
//     this binary. The stdio / HTTP transports reserve stdout
//     for the JSON-RPC wire; the CLI transport (i.e. the user
//     typing `mcp-confluence conf_get --path=...` in a shell)
//     is the legitimate use case for writing tool results to
//     stdout. See runToolInvocation below — that is the ONLY
//     place in the package that calls fmt.Println.
//
//  4. Persistent flags (--site, --email, --api-token) are
//     already inherited from the root command and pushed into
//     the process env by composeFlagsIntoEnv() (defined in
//     main.go). After that runs, config.LoadFromEnv() and
//     atlassian.New(cfg) produce the same client the stdio
//     and serve subcommands use, so the three transports share
//     identical credential resolution semantics.
//
//  5. The toolHandlers map below wires the 5 CRUD entries for
//     Phase 20. Phase 21 adds the remaining 13 entries
//     (list_spaces, list_pages, get_page_body, get_page_tree,
//     search, help, post_markdown, put_markdown,
//     get_page_markdown, upload_attachment, list_attachments,
//     delete_attachment, upload_drawio). The subcommand
//     factories in cli_tool_crud.go look up the handler from
//     this map by name — the dispatch is data-driven, not
//     hand-wired.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"reflect"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/bennie/mcp-confluence/internal/atlassian"
	"github.com/bennie/mcp-confluence/internal/config"
	internal "github.com/bennie/mcp-confluence/internal/tools"
)

// toolHandlers maps each MCP tool name to its Handle* function.
// Phase 20 wired the 5 CRUD entries; Phase 21 added the other 13
// (list_spaces, list_pages, get_page_body, get_page_tree, search,
// help, post_markdown, put_markdown, get_page_markdown,
// upload_attachment, list_attachments, delete_attachment,
// upload_drawio). Together the 18 entries cover the full MCP
// tool surface — every tool the stdio / HTTP transports expose
// also has a CLI dispatch entry. The subcommand factories in
// cli_tool_crud.go / cli_tool_convenience.go / cli_tool_markdown.go
// / cli_tool_attachments.go / cli_tool_drawio.go look up the
// handler from this map by name — the dispatch is data-driven,
// not hand-wired.
//
// Type identity note: every Handle* function shares the same
// shape `func(context.Context, *atlassian.Client, json.RawMessage)
// (string, error)`, so a single function type is sufficient and
// the map values are interchangeable.
var toolHandlers = map[string]func(context.Context, *atlassian.Client, json.RawMessage) (string, error){
	// Phase 20 — 5 CRUD (raw REST pass-through)
	"conf_get":    internal.HandleGet,
	"conf_post":   internal.HandlePost,
	"conf_put":    internal.HandlePut,
	"conf_patch":  internal.HandlePatch,
	"conf_delete": internal.HandleDelete,
	// Phase 21 — 6 convenience (typed wrappers over /wiki/api/v2/*)
	"conf_list_spaces":   internal.HandleListSpaces,
	"conf_list_pages":    internal.HandleListPages,
	"conf_get_page_body": internal.HandleGetPageBody,
	"conf_get_page_tree": internal.HandleGetPageTree,
	"conf_search":        internal.HandleSearch,
	"conf_help":          internal.HandleHelp,
	// Phase 21 — 3 markdown (round-trip conf_post/put via internal/markdown)
	"conf_post_markdown":     internal.HandlePostMarkdown,
	"conf_put_markdown":      internal.HandlePutMarkdown,
	"conf_get_page_markdown": internal.HandleGetPageMarkdown,
	// Phase 21 — 3 attachments (v1 upload + v2 list/delete)
	"conf_upload_attachment": internal.HandleUploadAttachment,
	"conf_list_attachments":  internal.HandleListAttachments,
	"conf_delete_attachment": internal.HandleDeleteAttachment,
	// Phase 21 — 1 drawio (upload + embed in one call)
	"conf_upload_drawio": internal.HandleUploadDrawio,
}

// toolHandler returns the Handle* function for the given tool
// name, or nil if no such tool is registered. Callers MUST check
// the result — a subcommand factory for an unknown tool would be
// a programmer error and should fail loudly at startup.
func toolHandler(name string) func(context.Context, *atlassian.Client, json.RawMessage) (string, error) {
	return toolHandlers[name]
}

// descriptionRe extracts the `description=...` value from a
// jsonschema struct tag. The value is a comma-separated list of
// `key=value` tokens, so we capture everything up to the next
// comma. Backslash-escaped commas inside the description
// (uncommon in our schemas) would need a more careful parse —
// current usage in internal/tools/args.go never embeds commas
// inside descriptions, so the simple regex is sufficient.
var descriptionRe = regexp.MustCompile(`description=([^,]+)`)

// parseJsonschemaTag pulls the human-readable description and the
// `required` boolean flag out of a `jsonschema:"description=...,required"`
// struct tag. The function is lenient: a missing or malformed tag
// returns an empty description and required=false. Callers (the
// flag-binding code below) add a "(required)" suffix to the
// description itself when required is true, so cobra's flag help
// text surfaces the constraint without inventing a new flag.
func parseJsonschemaTag(schemaTag string) (description string, required bool) {
	if schemaTag == "" {
		return "", false
	}
	if m := descriptionRe.FindStringSubmatch(schemaTag); len(m) == 2 {
		description = strings.TrimSpace(m[1])
	}
	// The `required` token is bare (no `=` value). Tokenize the
	// tag on commas and look for an exact match — this avoids
	// the false-positive of `description=...required...` (a
	// description that happens to contain the substring
	// "required"), which would slip past a naive contains()
	// check.
	for _, tok := range strings.Split(schemaTag, ",") {
		if strings.TrimSpace(tok) == "required" {
			required = true
			break
		}
	}
	return description, required
}

// queryPairSplitToken is the separator between successive k=v
// pairs in a single --query flag value. We use "," because
// every URL query parameter key/value pair uses "&" on the
// wire — using "," here keeps the CLI expression one separator
// away from the URL form, so `&` in --query would not collide
// with the shell's background operator. See the comment above
// bindQueryMap below for the full rationale.
const queryPairSplitToken = ","

// bindQueryMap parses a `--query=k1=v1,k2=v2` flag value into a
// map[string]string. The value comes in as a single string from
// cobra (one --query=... occurrence); we split on "," to recover
// individual pairs, then on the FIRST "=" to recover key and
// value (values may legitimately contain "=" characters, e.g.
// base64 padding or CQL operators).
//
// Format choice: comma-separated k=v is preferred over:
//   - multiple --query=k=v flags: cobra's StringSlice would work
//     but doubles the typing for typical use, and produces
//     harder-to-read Make targets.
//   - JSON object (--query='{"k":"v"}'): too noisy for a CLI
//     knob used 95% of the time with a single key (e.g.
//     --query=limit=5).
//   - URL query string (--query=k=v&k2=v2): collides with the
//     shell's background operator; would require quoting in
//     every call site.
//
// The split-on-first-"=" rule means a value containing "=" is
// preserved verbatim: --query=cql=type%3Dpage parses as
// {"cql":"type%3Dpage"} (URL-decoded by the upstream handler
// later). Empty pairs (e.g. trailing ",") are dropped.
func bindQueryMap(raw string) map[string]string {
	out := map[string]string{}
	if raw == "" {
		return out
	}
	for _, pair := range strings.Split(raw, queryPairSplitToken) {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		eq := strings.IndexByte(pair, '=')
		if eq < 0 {
			// No "=" — treat the whole token as a key with
			// empty value. This matches how many HTTP query
			// parsers behave (e.g. `?flag` → key="flag",
			// value=""). Callers who need strict key=value
			// semantics should validate upstream.
			out[pair] = ""
			continue
		}
		key := strings.TrimSpace(pair[:eq])
		val := pair[eq+1:]
		if key == "" {
			continue
		}
		out[key] = val
	}
	return out
}

// parseBoolFlagValue is a defensive helper for the Bool field
// case. pflag's flag.Value for a bool flag returns "true" or
// "false" from String(); we re-parse to ensure we set the
// reflect.Value correctly. A blank string (e.g. unset) is
// treated as false (the Go zero value).
func parseBoolFlagValue(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "false", "0", "no":
		return false, nil
	case "true", "1", "yes":
		return true, nil
	default:
		return false, fmt.Errorf("invalid bool value %q", s)
	}
}

// registerFlagsFromArgsStruct walks the fields of argsStruct (a
// pointer to a struct with `json` and `jsonschema` tags) and
// registers a cobra flag on cmd's local FlagSet for each field.
// Callers invoke this from the subcommand factory BEFORE cobra
// parses the command line, so the registered flags appear in
// --help and are accepted on the command line.
//
// Supported field types: string, bool, int, int64, and
// `map[string]string` (the Query field — bound as
// --query=k1=v1,k2=v2). Other field types return a descriptive
// error so a future struct addition is caught loudly.
func registerFlagsFromArgsStruct(cmd *cobra.Command, argsStruct any) error {
	v := reflect.ValueOf(argsStruct).Elem()
	if v.Kind() != reflect.Struct {
		return fmt.Errorf("registerFlagsFromArgsStruct: expected pointer to struct, got %T", argsStruct)
	}
	t := v.Type()
	pf := cmd.Flags()
	return registerFlagsForType(pf, t)
}

func registerFlagsForType(pf *pflag.FlagSet, t reflect.Type) error {
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		jsonTag := field.Tag.Get("json")
		if jsonTag == "" || jsonTag == "-" {
			continue
		}
		// The json tag may carry an `,omitempty` suffix; we
		// only need the name part for cobra's flag name.
		name := strings.Split(jsonTag, ",")[0]
		schemaTag := field.Tag.Get("jsonschema")
		description, required := parseJsonschemaTag(schemaTag)
		if required {
			description = description + " (required)"
		}

		switch field.Type.Kind() {
		case reflect.String:
			pf.String(name, "", description)
		case reflect.Bool:
			pf.Bool(name, false, description)
		case reflect.Int, reflect.Int64:
			pf.Int(name, 0, description)
		case reflect.Map:
			// The CRUD args structs have two map-shaped
			// fields: Query (map[string]string) and Body
			// (map[string]any for POST/PUT, or
			// []map[string]any for PATCH). We bind all of
			// them as --query / --body string flags and
			// post-process in readFlags below — see
			// bindQueryMap (k1=v1,k2=v2) and bindBody*
			// (raw JSON object / array).
			if field.Type.Key().Kind() == reflect.String &&
				field.Type.Elem().Kind() == reflect.String {
				pf.String(name, "", description+" (k1=v1,k2=v2)")
			} else if field.Type.Key().Kind() == reflect.String &&
				field.Type.Elem().Kind() == reflect.Interface {
				// Body: map[string]any → JSON object string
				pf.String(name, "", description+" (JSON object string)")
			} else {
				return fmt.Errorf("--%s: unsupported map type %s", name, field.Type)
			}
		case reflect.Slice:
			// PATCH's Body is []map[string]any. Bind as a
			// JSON-array string (the user passes the literal
			// array; HandlePatch unmarshals into the slice).
			if field.Type.Elem().Kind() == reflect.Map &&
				field.Type.Elem().Key().Kind() == reflect.String &&
				field.Type.Elem().Elem().Kind() == reflect.Interface {
				pf.String(name, "", description+" (JSON array string)")
			} else {
				return fmt.Errorf("--%s: unsupported slice type %s", name, field.Type)
			}
		default:
			return fmt.Errorf("--%s: unsupported field type %s", name, field.Type.Kind())
		}
	}
	return nil
}

// readFlagsFromArgsStruct reads the bound values from cmd's
// FlagSet back into a fresh instance of argsStruct, then
// json.Marshals the populated struct into a json.RawMessage
// suitable for passing to internal/tools.HandleXxx.
//
// The function is the second half of the bindFlags pair: the
// subcommand factory calls registerFlagsFromArgsStruct BEFORE
// cobra parses the args, and readFlagsFromArgsStruct AFTER
// (i.e. inside RunE). The two helpers are kept separate so
// cobra's --help sees the flags at parse time, and so the
// RunE closure has access to the parsed values.
//
// Return value: the JSON bytes that HandlerXxx expects. Field
// tags with `json:"...,omitempty"` (e.g. Query, JQ,
// OutputFormat) are honoured by encoding/json automatically —
// an empty string / nil map / false bool / 0 int is omitted
// from the payload unless the field is required.
func readFlagsFromArgsStruct(cmd *cobra.Command, argsStruct any) (json.RawMessage, error) {
	t := reflect.TypeOf(argsStruct).Elem()
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("readFlagsFromArgsStruct: expected pointer to struct, got %T", argsStruct)
	}

	// Allocate a fresh instance so we don't pollute the
	// caller's pointer with partly-populated state on error
	// paths. The caller passes us the type via a zero-valued
	// pointer; we re-allocate from the reflect.Type.
	target := reflect.New(t).Elem()
	pf := cmd.Flags()

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		jsonTag := field.Tag.Get("json")
		if jsonTag == "" || jsonTag == "-" {
			continue
		}
		name := strings.Split(jsonTag, ",")[0]
		f := pf.Lookup(name)
		if f == nil {
			// The subcommand factory should have
			// registered every field. If a field is missing
			// here, the factory and the read routine got
			// out of sync — fail loudly so the regression
			// is caught.
			return nil, fmt.Errorf("readFlagsFromArgsStruct: --%s: flag not registered (factory bug?)", name)
		}
		dest := target.Field(i)

		switch field.Type.Kind() {
		case reflect.String:
			dest.SetString(f.Value.String())
		case reflect.Bool:
			b, perr := parseBoolFlagValue(f.Value.String())
			if perr != nil {
				return nil, fmt.Errorf("readFlagsFromArgsStruct: --%s: %w", name, perr)
			}
			dest.SetBool(b)
		case reflect.Int, reflect.Int64:
			// pflag's IntValue stores its own int
			// representation in canonical decimal form; the
			// Value.String() path is sufficient.
			raw := f.Value.String()
			if raw == "" || raw == "0" {
				dest.SetInt(0)
				continue
			}
			var n int64
			if _, err := fmt.Sscanf(raw, "%d", &n); err != nil {
				return nil, fmt.Errorf("readFlagsFromArgsStruct: --%s: parse int %q: %w", name, raw, err)
			}
			dest.SetInt(n)
		case reflect.Map:
			raw := f.Value.String()
			switch {
			case field.Type.Elem().Kind() == reflect.String:
				// Query (map[string]string)
				m := bindQueryMap(raw)
				dest.Set(reflect.ValueOf(m))
			case field.Type.Elem().Kind() == reflect.Interface:
				// Body (map[string]any) — parse the
				// --body string as a JSON object and store
				// the parsed value. If raw is empty (no
				// --body flag), leave the zero value (nil
				// map) so encoding/json drops the field.
				if raw == "" {
					continue
				}
				var body map[string]any
				if err := json.Unmarshal([]byte(raw), &body); err != nil {
					return nil, fmt.Errorf("readFlagsFromArgsStruct: --%s: parse JSON object: %w", name, err)
				}
				dest.Set(reflect.ValueOf(body))
			default:
				return nil, fmt.Errorf("readFlagsFromArgsStruct: --%s: unsupported map value type %s", name, field.Type.Elem())
			}
		case reflect.Slice:
			// PATCH's Body is []map[string]any. Parse the
			// --body string as a JSON array.
			raw := f.Value.String()
			if raw == "" {
				continue
			}
			sliceType := reflect.TypeOf([]map[string]any{})
			ptr := reflect.New(sliceType)
			if err := json.Unmarshal([]byte(raw), ptr.Interface()); err != nil {
				return nil, fmt.Errorf("readFlagsFromArgsStruct: --%s: parse JSON array: %w", name, err)
			}
			dest.Set(ptr.Elem())
		default:
			return nil, fmt.Errorf("readFlagsFromArgsStruct: --%s: unsupported field type %s", name, field.Type.Kind())
		}
	}

	// Marshal the populated struct into a json.RawMessage. The
	// `json:",omitempty"` tags on optional fields (Query, JQ,
	// OutputFormat) cause empty values to be dropped — HandlerXxx
	// sees the same shape it would see over the JSON-RPC wire
	// from a well-formed client.
	raw, err := json.Marshal(target.Addr().Interface())
	if err != nil {
		return nil, fmt.Errorf("readFlagsFromArgsStruct: marshal: %w", err)
	}
	return raw, nil
}

// runToolInvocation is the RunE body for every per-tool CLI
// subcommand. It is the ONE legitimate stdout writer in the
// binary — the CLI dispatch surface emits tool results to
// stdout so they can be piped to jq / pbcopy / a file.
//
// Pipeline (mirrors runLifecycle's cfg → client shape, but
// skips the stdio/HTTP server wiring):
//
//  1. composeFlagsIntoEnv() — pushes --site / --email /
//     --api-token into ATLASSIAN_* env vars so config.LoadFromEnv
//     sees them at the process-env tier (Q22 ordering).
//  2. config.LoadFromEnv() — produces *config.Config with all
//     three creds validated.
//  3. atlassian.New(cfg) — produces *atlassian.Client (same
//     instance the stdio / serve subcommands use).
//  4. readFlagsFromArgsStruct(cmd, argsStruct) — pulls the
//     bound flag values back into a fresh args struct and
//     marshals it to json.RawMessage.
//  5. handleFn(ctx, client, raw) — invokes the locked Handler*.
//  6. fmt.Println(result) — the tool result, on stdout. This
//     is the load-bearing line: cobra's SetOut is io.Discard,
//     so the only way to land the tool result on stdout is to
//     write here directly.
//
// Errors at any step are returned to the caller; cobra's
// main() loop prints them to stderr and exits non-zero (the
// conventional CLI failure path). We do NOT swallow errors —
// a missing API token must surface as a non-zero exit so a
// `make` target or shell script that calls the subcommand
// can detect failure.
//
// Secret handling: the API token flows from cmd.Flags() into
// ATLASSIAN_API_TOKEN via composeFlagsIntoEnv, then through
// config.LoadFromEnv, then through atlassian.New. None of
// those log the value. The startup banner printed by
// composeFlagsIntoEnv includes only the site and email — the
// token is never named in the log output.
func runToolInvocation(
	cmd *cobra.Command,
	_ []string,
	handleFn func(ctx context.Context, client *atlassian.Client, args json.RawMessage) (string, error),
	argsStruct any,
) error {
	// Resolve the handler. The per-tool subcommand factories pass
	// it explicitly via the closure; this fallback exists so the
	// map at the top of this file (and the toolHandler helper)
	// stay referenced — they're the canonical registry of all 18
	// tools, and a future change to add a "tools list" root
	// subcommand would read from toolHandlers directly.
	if handleFn == nil {
		handleFn = toolHandler(cmd.Name())
		if handleFn == nil {
			return fmt.Errorf("runToolInvocation: no handler for tool %q (programmer error in subcommand factory)", cmd.Name())
		}
	}
	if argsStruct == nil {
		return fmt.Errorf("runToolInvocation: argsStruct is nil (programmer error in subcommand factory)")
	}
	ctx := cmd.Context()
	if ctx == nil {
		// RunE can be reached outside a request context
		// (e.g. when cobra is invoked from a test that
		// bypasses Execute). Use Background so the handler
		// still has a working context for cancellation /
		// deadlines.
		ctx = context.Background()
	}

	// Step 1: push --site/--email/--api-token into the process
	// env so the Q22 .env ordering is preserved by composition.
	composeFlagsIntoEnv()

	// Step 2: load config from env + .env file. This is the
	// same call runLifecycle() makes; we share the validator
	// and the error envelope verbatim.
	cfg, err := config.LoadFromEnv()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Step 3: build the atlassian HTTP client. Same
	// construction as runLifecycle() / serve.
	client, err := atlassian.New(cfg)
	if err != nil {
		return fmt.Errorf("build atlassian client: %w", err)
	}

	// Step 4: read the bound flags into a fresh args struct
	// (the caller's pointer is owned by the cobra command
	// tree; we don't reuse it because we may need to
	// re-invoke this helper from a test harness).
	raw, err := readFlagsFromArgsStruct(cmd, argsStruct)
	if err != nil {
		return err
	}

	// Step 5: invoke the locked Handler*. The returned
	// string is the tool result — TOON-encoded by default.
	result, err := handleFn(ctx, client, raw)
	if err != nil {
		// We log to stderr AND return the error so cobra's
		// main() can choose its own exit semantics. The
		// handler's error envelope (`<METHOD> <path>: <status>
		// <text> - <body>` per internal/tools/execute.go) is
		// useful for the operator; we print it verbatim.
		log.Printf("%s: %v", cmd.Name(), err)
		return fmt.Errorf("%s: %w", cmd.Name(), err)
	}

	// THE ONE STDOUT WRITE IN THE BINARY. The CLI dispatch
	// surface IS supposed to print tool results to stdout —
	// that is the only legitimate exception to the JSON-RPC-
	// stdout invariant (which applies to the stdio / HTTP
	// transports, not the CLI). DO NOT add fmt.Println /
	// os.Stdout.Write calls anywhere else in the package.
	fmt.Println(result)
	return nil
}
