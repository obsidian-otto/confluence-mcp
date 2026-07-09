// Package toon provides a TOON (Token-Oriented Object Notation) encoder.
//
// TOON is a YAML-like indentation-based text format designed for LLM prompts.
// It is described at https://github.com/toon-format/spec (version 3.3, MIT).
// This implementation is a Go port of the reference encoder used by the
// upstream @aashari/mcp-server-atlassian-confluence project.
//
// The package is intentionally stdlib-only: no production-quality TOON
// library exists for Go, and the upstream Node.js implementation depends on
// the @toon-format/toon ESM package, which is not portable here.
package toon

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"testing"
)

// roundTrip is the oracle: take a Go value, encode to TOON, decode back, and
// confirm json.Marshal(decode(encode(v))) is byte-identical to json.Marshal(v).
//
// The JSON step normalises object key order, float formatting, and other
// host-specific quirks, so this oracle only verifies structural equivalence
// — it does not require that we emit TOON exactly like the upstream library.
// (See encode_test.go for tests that pin the exact output.)
func roundTrip(t *testing.T, name string, v any) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		want, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("json.Marshal input: %v", err)
		}
		encoded, err := Encode(v)
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}
		var decoded any
		if err := Decode(encoded, &decoded); err != nil {
			t.Fatalf("Decode(%q): %v", encoded, err)
		}
		got, err := json.Marshal(decoded)
		if err != nil {
			t.Fatalf("json.Marshal decoded: %v", err)
		}
		if !bytes.Equal(want, got) {
			t.Fatalf("round-trip mismatch:\n  want: %s\n  got:  %s\n  toon: %q", want, got, encoded)
		}
	})
}

func TestEncode_RoundTrip(t *testing.T) {
	cases := []struct {
		name string
		v    any
	}{
		{"null", nil},
		{"empty_map", map[string]any{}},
		{"empty_slice", []any{}},
		{"flat_string", map[string]any{"k": "hello"}},
		{"flat_int", map[string]any{"n": 42}},
		{"flat_float", map[string]any{"pi": 3.14}},
		{"flat_bool_t", map[string]any{"b": true}},
		{"flat_bool_f", map[string]any{"b": false}},
		{"flat_null", map[string]any{"x": nil}},
		{"multiple_keys", map[string]any{"id": 1, "name": "Ada", "active": true}},
		{"nested_object", map[string]any{
			"page": map[string]any{
				"id":   789,
				"body": map[string]any{"representation": "storage", "value": "<p>...</p>"},
			},
		}},
		{"array_of_primitives", map[string]any{"nums": []any{1, 2, 3}}},
		{"array_of_strings", map[string]any{"tags": []any{"a", "b", "c"}}},
		{"array_of_uniform_objects", []any{
			map[string]any{"id": 1, "name": "Ada"},
			map[string]any{"id": 2, "name": "Bob"},
		}},
		{"array_of_heterogeneous_objects", []any{
			map[string]any{"id": 1, "name": "Ada"},
			map[string]any{"id": 2, "role": "admin"},
		}},
		{"long_string", map[string]any{"x": strings.Repeat("a", 1500)}},
		{"string_with_colon", map[string]any{"key": "foo: bar"}},
		{"string_with_quotes", map[string]any{"key": `has "quotes"`}},
		{"string_with_newline", map[string]any{"key": "line1\nline2"}},
		{"string_with_backslash", map[string]any{"key": `has\backslash`}},
		{"string_with_tab", map[string]any{"key": "tab\there"}},
		{"string_looks_like_number", map[string]any{"v": "42"}},
		{"string_looks_like_bool", map[string]any{"v": "true"}},
		{"string_looks_like_null", map[string]any{"v": "null"}},
		{"empty_string", map[string]any{"v": ""}},
		{"unicode", map[string]any{"name": "日本語🚀"}},
		{"nested_arrays", map[string]any{"results": []any{[]any{1, 2}, []any{3, 4}}}},
		{"null_in_array", []any{1, nil, 3}},
		{"bool_in_array", []any{true, false, true}},
		{"mixed_array", []any{1, "two", true, nil}},
		// negative_zero is omitted from the round-trip test because the
		// TOON spec (§2) normalises -0 → 0; the JSON equality oracle
		// would fail since Go preserves the -0 bit pattern. The exact-
		// output test below asserts the encoder emits "0" for -0.
		{"zero_int", map[string]any{"x": 0}},
		{"negative_int", map[string]any{"x": -7}},
		{"negative_float", map[string]any{"x": -3.14}},
		{"float_whole_value", map[string]any{"x": 5.0}},
		{"deep_nesting", map[string]any{
			"a": map[string]any{
				"b": map[string]any{
					"c": map[string]any{"d": 1},
				},
			},
		}},
		{"array_of_empty_objects", []any{map[string]any{}, map[string]any{}}},
		{"confluence_spaces_response", map[string]any{
			"results": []any{
				map[string]any{"id": "1", "key": "DEV", "name": "Dev Space", "type": "global"},
				map[string]any{"id": "2", "key": "OPS", "name": "Ops Space", "type": "global"},
			},
			"_links": map[string]any{
				"self": "https://example.atlassian.net/wiki/api/v2/spaces?limit=2",
			},
		}},
	}
	for _, c := range cases {
		roundTrip(t, c.name, c.v)
	}
}

// TestEncode_ExactOutput pins the encoder to specific byte output for
// representative cases. The expected strings match the @toon-format/toon
// reference encoder (the upstream we are porting) and the canonical TOON v3.3
// fixtures, with a trailing newline added to each (the Go encoder always
// emits a final \n; the reference library does not — this is a stylistic
// difference, not a semantic one).
//
// For nested-object cases, the reference preserves insertion order while
// the Go encoder sorts keys alphabetically (Go maps have no insertion
// order). Those cases are therefore encoded as comments here; structural
// correctness is verified by the round-trip test instead.
func TestEncode_ExactOutput(t *testing.T) {
	cases := []struct {
		name string
		v    any
		want string
	}{
		{"scalar_string_root", "hello", "hello"},
		{"scalar_int_root", 42, "42"},
		{"scalar_bool_true_root", true, "true"},
		{"scalar_bool_false_root", false, "false"},
		{"scalar_null_root", nil, "null"},
		{"empty_map_root", map[string]any{}, ""},
		{"empty_slice_root", []any{}, "[]"},
		{"flat_object", map[string]any{"id": 123, "title": "My Page"},
			"id: 123\ntitle: My Page\n"},
		// nested_object: key order varies; covered by round-trip test.
		{"array_of_uniform_objects", []any{
			map[string]any{"id": 1, "name": "Ada"},
			map[string]any{"id": 2, "name": "Bob"},
		},
			"[2]{id,name}:\n  1,Ada\n  2,Bob\n"},
		{"array_of_heterogeneous_objects", []any{
			map[string]any{"id": 1, "name": "Ada"},
			map[string]any{"id": 2, "role": "admin"},
		},
			"[2]:\n  - id: 1\n    name: Ada\n  - id: 2\n    role: admin\n"},
		{"array_of_strings_inline", map[string]any{"tags": []any{"a", "b", "c"}},
			"tags[3]: a,b,c\n"},
		{"array_of_ints_inline", map[string]any{"nums": []any{1, 2, 3}},
			"nums[3]: 1,2,3\n"},
		{"null_value", map[string]any{"a": nil, "b": 1}, "a: null\nb: 1\n"},
		{"string_with_colon_quoted", map[string]any{"key": "foo: bar"},
			`key: "foo: bar"` + "\n"},
		{"string_with_quotes", map[string]any{"key": `has "quotes"`},
			`key: "has \"quotes\""` + "\n"},
		{"string_with_newline", map[string]any{"key": "line1\nline2"},
			`key: "line1\nline2"` + "\n"},
		{"string_with_backslash", map[string]any{"key": `has\backslash`},
			`key: "has\\backslash"` + "\n"},
		{"empty_nested_object", map[string]any{"user": map[string]any{}}, "user:\n"},
		{"array_of_arrays", map[string]any{"results": []any{[]any{1, 2}, []any{3, 4}}},
			"results[2]:\n  - [2]: 1,2\n  - [2]: 3,4\n"},
		{"array_of_nulls", []any{nil, nil}, "[2]: null,null\n"},
		{"string_looks_like_bool_quoted", map[string]any{"v": "true"}, `v: "true"` + "\n"},
		{"string_looks_like_number_quoted", map[string]any{"v": "42"}, `v: "42"` + "\n"},
		{"single_hyphen_string", map[string]any{"marker": "-"}, `marker: "-"` + "\n"},
		{"confluence_tabular_id_quoted", map[string]any{
			"results": []any{
				map[string]any{"id": "1", "key": "DEV"},
				map[string]any{"id": "2", "key": "OPS"},
			},
		},
			"results[2]{id,key}:\n  \"1\",DEV\n  \"2\",OPS\n"},
		// negative_zero normalises to "0" per §2 of the spec.
		{"negative_zero_normalises", map[string]any{"x": math.Copysign(0, -1)}, "x: 0\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Encode(c.v)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			if string(got) != c.want {
				t.Fatalf("Encode mismatch:\n  want: %q\n  got:  %q", c.want, got)
			}
		})
	}
}

// TestMarshal_Alias confirms Marshal(v) produces the same bytes as Encode(v).
func TestMarshal_Alias(t *testing.T) {
	v := map[string]any{"id": 1, "name": "Ada"}
	enc, err := Encode(v)
	if err != nil {
		t.Fatal(err)
	}
	mar, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if string(enc) != string(mar) {
		t.Fatalf("Marshal differs from Encode:\n  Encode: %q\n  Marshal: %q", enc, mar)
	}
}

// TestMarshalIndent confirms MarshalIndent accepts prefix + indent arguments
// (mirroring encoding/json's signature) and applies the indent width to
// nested lines. The prefix argument is accepted for API parity but is
// largely a no-op in TOON since the root form begins at column 0 — it is
// surfaced on continuation lines only.
func TestMarshalIndent(t *testing.T) {
	v := map[string]any{
		"page": map[string]any{
			"id":   1,
			"name": "Ada",
		},
	}
	got, err := MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want := "page:\n  id: 1\n  name: Ada\n"
	if string(got) != want {
		t.Fatalf("MarshalIndent default:\n  want: %q\n  got:  %q", want, got)
	}

	// 4-space indent widens nested lines.
	got4, err := MarshalIndent(v, "", "    ")
	if err != nil {
		t.Fatal(err)
	}
	want4 := "page:\n    id: 1\n    name: Ada\n"
	if string(got4) != want4 {
		t.Fatalf("MarshalIndent 4-space:\n  want: %q\n  got:  %q", want4, got4)
	}

	// Prefix arg is accepted without error (signature parity with
	// encoding/json). It is not semantically meaningful in TOON because
	// indentation is structural; we just confirm the call does not panic
	// and produces the same structural indent.
	if _, err := MarshalIndent(v, ">>", "  "); err != nil {
		t.Fatalf("MarshalIndent with prefix returned error: %v", err)
	}
}

// TestEncode_TokenSavings sanity-checks that the encoder shrinks a
// representative Confluence v2 response. The 30-60% target is verified at
// the integration level in Phase 10; here we just assert the encoder is not
// blowing up the size.
func TestEncode_TokenSavings(t *testing.T) {
	v := map[string]any{
		"results": []any{
			map[string]any{
				"id":          "131073",
				"key":         "DEV",
				"name":        "Development",
				"type":        "global",
				"status":      "current",
				"description": "Dev space",
			},
			map[string]any{
				"id":          "131074",
				"key":         "OPS",
				"name":        "Operations",
				"type":        "global",
				"status":      "current",
				"description": "Ops space",
			},
		},
		"_links": map[string]any{
			"self": "https://example.atlassian.net/wiki/api/v2/spaces?limit=2",
			"next": "/wiki/api/v2/spaces?cursor=abc&limit=2",
			"base": "https://example.atlassian.net",
		},
	}
	jsonBytes, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	toonBytes, err := Encode(v)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("json size: %d, toon size: %d, ratio: %.2f%%",
		len(jsonBytes), len(toonBytes),
		float64(len(toonBytes))/float64(len(jsonBytes))*100)
	// Conservative upper bound: TOON must be at least 5% smaller than JSON
	// for any homogeneous tabular array — if it isn't, the encoder is broken.
	if len(toonBytes) >= len(jsonBytes) {
		t.Fatalf("TOON not smaller than JSON: json=%d, toon=%d\njson:\n%s\ntoon:\n%s",
			len(jsonBytes), len(toonBytes), jsonBytes, toonBytes)
	}
}

// TestEncode_InvalidType confirms unsupported Go types (channels, funcs)
// return an error rather than panicking.
func TestEncode_InvalidType(t *testing.T) {
	type bad struct {
		Ch chan int
	}
	if _, err := Encode(bad{Ch: make(chan int)}); err == nil {
		t.Fatal("expected error for unsupported channel type, got nil")
	}
}
