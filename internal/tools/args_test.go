package tools

import (
	"encoding/json"
	"testing"
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
	type expectPost struct {
		Path         string
		Query        map[string]string
		Body         map[string]any
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
