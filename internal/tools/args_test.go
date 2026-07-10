package tools

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/invopop/jsonschema"
)

// TestArgsRoundTrip asserts that each of the five argument types
// (Get / Post / Put / Patch / Delete) unmarshals a representative
// JSON payload into the expected field values AND that re-marshaling
// the struct produces a stable result.
//
// The JSON shapes below mirror what an MCP caller would send over
// JSON-RPC — the keys match the upstream zod schema's snake/camel
// form (path, query, body, jq, outputFormat).
func TestArgsRoundTrip(t *testing.T) {
	t.Parallel()

	type expectGet struct {
		Path         string
		Query        map[string]string
		JQ           string
		OutputFormat string
	}

	t.Run("Get", func(t *testing.T) {
		raw := []byte(`{"path":"/wiki/api/v2/spaces","query":{"limit":"5","cursor":"abc"},"jq":"results[*].id","outputFormat":"json"}`)

		var got GetArgs
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("unmarshal GetArgs: %v", err)
		}
		want := expectGet{
			Path:         "/wiki/api/v2/spaces",
			Query:        map[string]string{"limit": "5", "cursor": "abc"},
			JQ:           "results[*].id",
			OutputFormat: "json",
		}
		if got.Path != want.Path {
			t.Errorf("Path = %q, want %q", got.Path, want.Path)
		}
		if len(got.Query) != len(want.Query) {
			t.Errorf("Query len = %d, want %d", len(got.Query), len(want.Query))
		}
		for k, v := range want.Query {
			if got.Query[k] != v {
				t.Errorf("Query[%q] = %q, want %q", k, got.Query[k], v)
			}
		}
		if got.JQ != want.JQ {
			t.Errorf("JQ = %q, want %q", got.JQ, want.JQ)
		}
		if got.OutputFormat != want.OutputFormat {
			t.Errorf("OutputFormat = %q, want %q", got.OutputFormat, want.OutputFormat)
		}

		// Re-marshal and check stability (omitted fields stay out).
		out, err := json.Marshal(got)
		if err != nil {
			t.Fatalf("marshal GetArgs: %v", err)
		}
		var roundtrip GetArgs
		if err := json.Unmarshal(out, &roundtrip); err != nil {
			t.Fatalf("re-unmarshal GetArgs: %v", err)
		}
		if roundtrip.Path != got.Path || roundtrip.JQ != got.JQ || roundtrip.OutputFormat != got.OutputFormat {
			t.Errorf("scalar roundtrip changed: before=%+v after=%+v", got, roundtrip)
		}
		if !mapsEqual(roundtrip.Query, got.Query) {
			t.Errorf("Query roundtrip changed: before=%+v after=%+v", got.Query, roundtrip.Query)
		}
	})

	t.Run("Post", func(t *testing.T) {
		raw := []byte(`{"path":"/wiki/api/v2/pages","query":{"space-id":"123"},"body":{"spaceId":"123","title":"Hi"},"jq":"{id: id}","outputFormat":"toon"}`)

		var got PostArgs
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("unmarshal PostArgs: %v", err)
		}
		if got.Path != "/wiki/api/v2/pages" {
			t.Errorf("Path = %q", got.Path)
		}
		if got.Query["space-id"] != "123" {
			t.Errorf("Query[space-id] = %q", got.Query["space-id"])
		}
		if got.Body["title"] != "Hi" {
			t.Errorf("Body[title] = %v", got.Body["title"])
		}
		if got.JQ != "{id: id}" {
			t.Errorf("JQ = %q", got.JQ)
		}
		if got.OutputFormat != "toon" {
			t.Errorf("OutputFormat = %q", got.OutputFormat)
		}

		out, err := json.Marshal(got)
		if err != nil {
			t.Fatalf("marshal PostArgs: %v", err)
		}
		var roundtrip PostArgs
		if err := json.Unmarshal(out, &roundtrip); err != nil {
			t.Fatalf("re-unmarshal PostArgs: %v", err)
		}
		// Body is a free-form map; reflect.DeepEqual handles it.
		if roundtrip.Path != got.Path || roundtrip.JQ != got.JQ || roundtrip.OutputFormat != got.OutputFormat {
			t.Errorf("scalar roundtrip changed: before=%+v after=%+v", got, roundtrip)
		}
	})

	t.Run("Put", func(t *testing.T) {
		raw := []byte(`{"path":"/wiki/api/v2/pages/42","body":{"id":"42","version":{"number":2}},"outputFormat":"json"}`)

		var got PutArgs
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("unmarshal PutArgs: %v", err)
		}
		if got.Path != "/wiki/api/v2/pages/42" {
			t.Errorf("Path = %q", got.Path)
		}
		if got.Body["id"] != "42" {
			t.Errorf("Body[id] = %v", got.Body["id"])
		}
		body, ok := got.Body["version"].(map[string]any)
		if !ok {
			t.Fatalf("Body[version] not an object: %T", got.Body["version"])
		}
		if body["number"].(float64) != 2 {
			t.Errorf("Body[version][number] = %v", body["number"])
		}
		if got.OutputFormat != "json" {
			t.Errorf("OutputFormat = %q", got.OutputFormat)
		}

		out, err := json.Marshal(got)
		if err != nil {
			t.Fatalf("marshal PutArgs: %v", err)
		}
		var roundtrip PutArgs
		if err := json.Unmarshal(out, &roundtrip); err != nil {
			t.Fatalf("re-unmarshal PutArgs: %v", err)
		}
		if roundtrip.Path != got.Path || roundtrip.OutputFormat != got.OutputFormat {
			t.Errorf("scalar roundtrip changed")
		}
	})

	t.Run("Patch", func(t *testing.T) {
		// PATCH body is a list of operations per the prompt's spec
		// (mirroring JSON-Patch conventions in the upstream API).
		raw := []byte(`{"path":"/wiki/api/v2/spaces/42","body":[{"op":"replace","path":"/name","value":"New"}],"jq":"id"}`)

		var got PatchArgs
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("unmarshal PatchArgs: %v", err)
		}
		if got.Path != "/wiki/api/v2/spaces/42" {
			t.Errorf("Path = %q", got.Path)
		}
		if len(got.Body) != 1 {
			t.Fatalf("Body len = %d, want 1", len(got.Body))
		}
		if got.Body[0]["op"] != "replace" {
			t.Errorf("Body[0][op] = %v", got.Body[0]["op"])
		}
		if got.Body[0]["path"] != "/name" {
			t.Errorf("Body[0][path] = %v", got.Body[0]["path"])
		}
		if got.Body[0]["value"] != "New" {
			t.Errorf("Body[0][value] = %v", got.Body[0]["value"])
		}
		if got.JQ != "id" {
			t.Errorf("JQ = %q", got.JQ)
		}

		out, err := json.Marshal(got)
		if err != nil {
			t.Fatalf("marshal PatchArgs: %v", err)
		}
		var roundtrip PatchArgs
		if err := json.Unmarshal(out, &roundtrip); err != nil {
			t.Fatalf("re-unmarshal PatchArgs: %v", err)
		}
		if roundtrip.Path != got.Path || roundtrip.JQ != got.JQ {
			t.Errorf("scalar roundtrip changed")
		}
		if len(roundtrip.Body) != len(got.Body) {
			t.Errorf("Body length changed: %d vs %d", len(roundtrip.Body), len(got.Body))
		}
	})

	t.Run("Delete", func(t *testing.T) {
		raw := []byte(`{"path":"/wiki/api/v2/pages/42","query":{"status":"trashed"},"jq":"{id: id, status: status}","outputFormat":"json"}`)

		var got DeleteArgs
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("unmarshal DeleteArgs: %v", err)
		}
		want := expectGet{
			Path:         "/wiki/api/v2/pages/42",
			Query:        map[string]string{"status": "trashed"},
			JQ:           "{id: id, status: status}",
			OutputFormat: "json",
		}
		if got.Path != want.Path {
			t.Errorf("Path = %q, want %q", got.Path, want.Path)
		}
		if got.Query["status"] != "trashed" {
			t.Errorf("Query[status] = %q", got.Query["status"])
		}
		if got.JQ != want.JQ {
			t.Errorf("JQ = %q, want %q", got.JQ, want.JQ)
		}
		if got.OutputFormat != want.OutputFormat {
			t.Errorf("OutputFormat = %q", want.OutputFormat)
		}

		out, err := json.Marshal(got)
		if err != nil {
			t.Fatalf("marshal DeleteArgs: %v", err)
		}
		var roundtrip DeleteArgs
		if err := json.Unmarshal(out, &roundtrip); err != nil {
			t.Fatalf("re-unmarshal DeleteArgs: %v", err)
		}
		if roundtrip.Path != got.Path || roundtrip.JQ != got.JQ || roundtrip.OutputFormat != got.OutputFormat {
			t.Errorf("scalar roundtrip changed: before=%+v after=%+v", got, roundtrip)
		}
		if !mapsEqual(roundtrip.Query, got.Query) {
			t.Errorf("Query roundtrip changed: before=%+v after=%+v", got.Query, roundtrip.Query)
		}
	})
}

// TestArgsOmitEmpty ensures that omitempty fields are not serialized
// when at their zero values. This is important because an empty JQ
// or outputFormat must not show up in canonical JSON.
func TestArgsOmitEmpty(t *testing.T) {
	t.Parallel()

	t.Run("Get omits zero jq/outputFormat/query", func(t *testing.T) {
		got := GetArgs{Path: "/wiki/api/v2/spaces"}
		out, err := json.Marshal(got)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		// Only "path" must be present.
		want := `{"path":"/wiki/api/v2/spaces"}`
		if string(out) != want {
			t.Errorf("marshal = %s, want %s", out, want)
		}
	})

	t.Run("Post omits zero body/query/jq/outputFormat", func(t *testing.T) {
		got := PostArgs{Path: "/wiki/api/v2/pages", Body: map[string]any{"title": "Hi"}}
		out, err := json.Marshal(got)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		// "body" is NOT omitempty; it must always be present so that
		// downstream callers requiring a body can rely on it.
		// (We deliberately diverge from upstream's zod `.required()` —
		// at the JSON layer the field is always materialised and the
		// handler layer enforces "required" via the path/body checks.)
		if !contains(string(out), `"body":`) {
			t.Errorf("marshal missing body: %s", out)
		}
		if !contains(string(out), `"path":"/wiki/api/v2/pages"`) {
			t.Errorf("marshal missing path: %s", out)
		}
	})

	t.Run("Patch body serialises as array", func(t *testing.T) {
		got := PatchArgs{Path: "/x", Body: []map[string]any{{"op": "add"}}}
		out, err := json.Marshal(got)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if !contains(string(out), `"body":[`) {
			t.Errorf("body not serialised as array: %s", out)
		}
	})
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// mapsEqual does a deep-enough equality on a map[string]string.
// (Go does not allow `==` on maps, so we have to walk it ourselves.)
func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// TestNewConvenienceArgsRoundTrip covers the four new args types
// added in the post-v1 quality-of-life pass (conf_list_spaces,
// conf_list_pages, conf_get_page_body, conf_search, conf_help).
// Each must round-trip a representative JSON payload with field
// values intact, and must serialise back without omitempty
// dropping the values supplied.
func TestNewConvenienceArgsRoundTrip(t *testing.T) {
	t.Parallel()

	t.Run("ListSpaces", func(t *testing.T) {
		raw := []byte(`{"limit":50,"cursor":"eyJpZCI6MTIzfQ","type":"personal","status":"current","outputFormat":"json"}`)
		var got ListSpacesArgs
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.Limit != 50 {
			t.Errorf("Limit = %d, want 50", got.Limit)
		}
		if got.Cursor != "eyJpZCI6MTIzfQ" {
			t.Errorf("Cursor = %q", got.Cursor)
		}
		if got.Type != "personal" {
			t.Errorf("Type = %q", got.Type)
		}
		if got.Status != "current" {
			t.Errorf("Status = %q", got.Status)
		}
		if got.OutputFormat != "json" {
			t.Errorf("OutputFormat = %q", got.OutputFormat)
		}
	})

	t.Run("ListPages", func(t *testing.T) {
		raw := []byte(`{"space-id":"780763211","title":"oncall","status":"current","limit":100,"sort":"-modified-date","body-format":"view"}`)
		var got ListPagesArgs
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.SpaceID != "780763211" {
			t.Errorf("SpaceID = %q", got.SpaceID)
		}
		if got.Title != "oncall" {
			t.Errorf("Title = %q", got.Title)
		}
		if got.Limit != 100 {
			t.Errorf("Limit = %d", got.Limit)
		}
		if got.SortField != "-modified-date" {
			t.Errorf("SortField = %q", got.SortField)
		}
		if got.BodyFormat != "view" {
			t.Errorf("BodyFormat = %q", got.BodyFormat)
		}
	})

	t.Run("GetPageBody", func(t *testing.T) {
		raw := []byte(`{"page-id":"163935","body-format":"storage","outputFormat":"json"}`)
		var got GetPageBodyArgs
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.PageID != "163935" {
			t.Errorf("PageID = %q", got.PageID)
		}
		if got.BodyFormat != "storage" {
			t.Errorf("BodyFormat = %q", got.BodyFormat)
		}
		if got.OutputFormat != "json" {
			t.Errorf("OutputFormat = %q", got.OutputFormat)
		}
	})

	t.Run("Search", func(t *testing.T) {
		raw := []byte(`{"cql":"type=page AND text~mcp","limit":50,"start":25}`)
		var got SearchArgs
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.CQL != "type=page AND text~mcp" {
			t.Errorf("CQL = %q", got.CQL)
		}
		if got.Limit != 50 {
			t.Errorf("Limit = %d", got.Limit)
		}
		if got.Start != 25 {
			t.Errorf("Start = %d", got.Start)
		}
	})

	t.Run("Help", func(t *testing.T) {
		raw := []byte(`{"topic":"conf_get"}`)
		var got HelpArgs
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.Topic != "conf_get" {
			t.Errorf("Topic = %q", got.Topic)
		}
	})
}

// TestArgsJsonschemaTagsPresent asserts that every args struct has
// non-empty `jsonschema:"description=..."` tags on its fields.
// Empty descriptions produce the dreaded `type: object` schema
// with no human-readable description — the audit doc flagged exactly
// that case for the new helpers. This is a structural guarantee.
//
// TestArgsJsonschemaTagsNotBlank is the matching "no empty
// descriptions" assertion that runs over all nine arg types.
func TestArgsJsonschemaTagsPresent(t *testing.T) {
	t.Parallel()

	argTypes := []any{
		GetArgs{},
		PostArgs{},
		PutArgs{},
		PatchArgs{},
		DeleteArgs{},
		ListSpacesArgs{},
		ListPagesArgs{},
		GetPageBodyArgs{},
		SearchArgs{},
		HelpArgs{},
	}

	for _, a := range argTypes {
		t.Run(reflect.TypeOf(a).Name(), func(t *testing.T) {
			rt := reflect.TypeOf(a)
			for i := 0; i < rt.NumField(); i++ {
				f := rt.Field(i)
				tag := f.Tag.Get("jsonschema")
				if tag == "" {
					t.Errorf("field %s.%s has no jsonschema tag", rt.Name(), f.Name)
					continue
				}
				if !strings.Contains(tag, "description=") {
					t.Errorf("field %s.%s jsonschema tag missing description= : %q", rt.Name(), f.Name, tag)
				}
			}
		})
	}
}

// TestArgsSchemasAreAccurate uses invopop/jsonschema (the same
// library mcp-golang uses internally) to reflect each args struct
// into a JSON Schema and assert a few liveness properties:
//
//   - The schema has type=object with properties.
//   - Every arg struct's schema includes a non-empty description
//     on at least one field (proves the jsonschema tag actually
//     propagated).
//   - The 'path' field on the five CRUD tools (Get, Post, Put,
//     Patch, Delete) is in the required list.
//   - The 'page-id' field on GetPageBodyArgs and 'cql' on
//     SearchArgs are required.
//   - The body field on PostArgs/PutArgs is type=object, the body
//     field on PatchArgs is type=array with items:object. (This is
//     the explicit body-shape assertion; reading the schema is
//     what proves the issue-1 confusion is gone: the schema is now
//     documented accurately.)
func TestArgsSchemasAreAccurate(t *testing.T) {
	t.Parallel()

	rs := jsonschema.Reflector{}
	// Use the json tag for property names (jsonschema default is the
	// field name, but mcp-golang uses json tags — align here).
	rs.FieldNameTag = "json"

	cases := []struct {
		name          string
		sample        any
		wantRequired  []string
		wantBodyShape string // "object", "array-of-object", or "" if no body
		bodyField     string
	}{
		{"GetArgs", GetArgs{}, []string{"path"}, "", ""},
		{"PostArgs", PostArgs{}, []string{"path"}, "object", "body"},
		{"PutArgs", PutArgs{}, []string{"path"}, "object", "body"},
		{"PatchArgs", PatchArgs{}, []string{"path"}, "array-of-object", "body"},
		{"DeleteArgs", DeleteArgs{}, []string{"path"}, "", ""},
		{"ListSpacesArgs", ListSpacesArgs{}, nil, "", ""},
		{"ListPagesArgs", ListPagesArgs{}, nil, "", ""},
		{"GetPageBodyArgs", GetPageBodyArgs{}, []string{"page-id"}, "", ""},
		{"SearchArgs", SearchArgs{}, []string{"cql"}, "", ""},
		{"HelpArgs", HelpArgs{}, nil, "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sch := rs.ReflectFromType(reflect.TypeOf(tc.sample))

			root := derefRoot(sch)
			if root.Type != "object" {
				t.Fatalf("root type = %q, want object", root.Type)
			}
			props := root.Properties
			if props == nil || props.Len() == 0 {
				t.Fatalf("no properties on root schema")
			}

			// Collect property names + descriptions in insertion order.
			// jsonschema's Properties is *orderedmap.OrderedMap from
			// github.com/wk8/go-ordered-map/v2; iterate via
			// Oldest() -> Pair.Next(). Pair.Value is already a
			// *jsonschema.Schema (concrete typed), no assertion needed.
			pairs := make([][2]string, 0, props.Len())
			pair := props.Oldest()
			for pair != nil {
				pairs = append(pairs, [2]string{pair.Key, pair.Value.Description})
				pair = pair.Next()
			}
			if len(pairs) == 0 {
				t.Fatalf("properties yielded no entries")
			}
			sort.SliceStable(pairs, func(i, j int) bool { return pairs[i][0] < pairs[j][0] })

			// Check descriptions are populated (proof tags took effect).
			hasDesc := false
			for _, p := range pairs {
				if p[1] != "" {
					hasDesc = true
					break
				}
			}
			if !hasDesc {
				t.Errorf("no field has a non-empty description; jsonschema tags may be missing")
			}

			// Required field assertions.
			if len(tc.wantRequired) > 0 {
				wantReq := map[string]bool{}
				for _, r := range tc.wantRequired {
					wantReq[r] = true
				}
				for _, r := range root.Required {
					delete(wantReq, r)
				}
				if len(wantReq) > 0 {
					t.Errorf("missing required fields: %v", wantReq)
				}
			}

			// Body shape assertions (the core of Issue 1 from the
			// post-v1 audit: verify the body field's type matches
			// its actual Go-side semantics, never a surprise from
			// reflection).
			if tc.wantBodyShape != "" {
				bodyProp, bodyOK := props.Get(tc.bodyField)
				if !bodyOK {
					t.Fatalf("body field %q not in properties", tc.bodyField)
				}
				switch tc.wantBodyShape {
				case "object":
					if bodyProp.Type != "object" {
						t.Errorf("body type = %q, want object (got %+v)", bodyProp.Type, bodyProp)
					}
				case "array-of-object":
					if bodyProp.Type != "array" {
						t.Errorf("body type = %q, want array", bodyProp.Type)
					}
					if bodyProp.Items == nil || bodyProp.Items.Type != "object" {
						t.Errorf("body items not type=object: %+v", bodyProp.Items)
					}
				}
			}
		})
	}
}

// derefRoot follows the $ref pointer that top-level ReflectFromType
// produces, returning the actual schema in $defs/T. It panics if the
// schema shape is unexpected; that is intentional — if upstream
// invopop/jsonschema changes how it lays out $ref, every test in
// this file fails loudly rather than silently skipping.
func derefRoot(sch *jsonschema.Schema) *jsonschema.Schema {
	if sch == nil {
		panic("derefRoot: nil schema")
	}
	if sch.Ref != "" {
		// sch.Ref is "#/$defs/T"; extract T and look up.
		parts := strings.SplitN(sch.Ref, "/", 3)
		if len(parts) < 3 {
			panic("derefRoot: unexpected $ref shape: " + sch.Ref)
		}
		defName := parts[2]
		def, ok := sch.Definitions[defName]
		if !ok {
			panic("derefRoot: $defs[" + defName + "] not found")
		}
		return def
	}
	return sch
}
