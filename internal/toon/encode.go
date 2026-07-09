// Package toon provides a TOON (Token-Oriented Object Notation) encoder.
//
// TOON is a YAML-like indentation-based text format designed for LLM prompts.
// The format is described at https://github.com/toon-format/spec (v3.3, MIT).
// This implementation is a Go port of the reference encoder used by the
// upstream @aashari/mcp-server-atlassian-confluence project. The package is
// intentionally stdlib-only: no production-quality TOON library exists for
// Go, and the upstream Node.js implementation depends on the ESM
// @toon-format/toon package, which is not portable here.
//
// Public API:
//
//   - Encode(v any) ([]byte, error) — encode a Go value to TOON bytes
//   - Marshal(v any) ([]byte, error) — alias for Encode (encoding/json parity)
//   - MarshalIndent(v any, prefix, indent string) ([]byte, error) — like
//     encoding/json's MarshalIndent; accepts a custom indent width.
//   - Decode(data []byte, dst *any) error — minimal decoder used for
//     round-trip tests. Not part of the production tool surface; tool
//     consumers receive the raw TOON bytes directly.
package toon

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Encode marshals v to TOON format.
//
// The input is typically the result of json.Unmarshal (map[string]any,
// []any, string, float64, bool, nil), but Go-native types are accepted and
// normalised per the TOON data model (§3 of the spec):
//
//   - bool, string, float64/int/int64, nil, map[string]any, []any, json.Number
//   - encoding/json Marshaler / encoding.TextMarshaler are honoured
//   - NaN, +Inf, -Inf → null (per §2 number rules)
//   - unsupported types (chan, func, complex, *struct without MarshalJSON)
//     return an error
//
// Encode does not write a trailing newline.
func Encode(v any) ([]byte, error) {
	var buf bytes.Buffer
	e := newEncoder(&buf, 2)
	if err := e.encodeRoot(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Marshal is an alias for Encode, providing API parity with encoding/json.
func Marshal(v any) ([]byte, error) { return Encode(v) }

// MarshalIndent is like Encode but uses the given indent string (e.g.,
// "  ", "    ", "\t") for nested lines. The prefix argument is accepted for
// signature parity with encoding/json's MarshalIndent but is largely a no-op
// in TOON since the root form begins at column 0 and indentation is
// structural.
func MarshalIndent(v any, prefix, indent string) ([]byte, error) {
	_ = prefix // no-op: TOON indentation is structural, not decorative
	if indent == "" {
		indent = "  "
	}
	var buf bytes.Buffer
	e := newEncoder(&buf, len(indent))
	if err := e.encodeRoot(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ---------------------------------------------------------------------------
// Encoder
// ---------------------------------------------------------------------------

type encoder struct {
	buf    *bytes.Buffer
	indent string
	// delimiter is the active document delimiter. The reference encoder
	// uses comma; tab/pipe variants exist per §11 of the spec but are not
	// surfaced in the public API (the upstream also defaults to comma).
	delim byte
}

func newEncoder(buf *bytes.Buffer, indentSize int) *encoder {
	return &encoder{
		buf:    buf,
		indent: strings.Repeat(" ", indentSize),
		delim:  ',',
	}
}

func (e *encoder) writeIndent(depth int) {
	for i := 0; i < depth; i++ {
		e.buf.WriteString(e.indent)
	}
}

// encodeRoot writes the root form of v. Per §5 of the spec:
//   - root object → key: value lines
//   - empty root object → "" (no output)
//   - root array → [N]: or [N]{f1,f2,...}: header + rows
//   - root primitive → just the value (no trailing newline)
func (e *encoder) encodeRoot(v any) error {
	norm, err := normalize(v)
	if err != nil {
		return err
	}
	switch n := norm.(type) {
	case nil:
		e.buf.WriteString("null")
	case map[string]any:
		if len(n) == 0 {
			// empty object → empty document
			return nil
		}
		if err := e.encodeObjectBody(n, 0); err != nil {
			return err
		}
	case []any:
		return e.encodeArray(n, "", 0)
	default:
		// scalar root
		e.writeScalar(n)
	}
	return nil
}

// encodeObjectBody writes the body of an object at the given depth, with
// no header (the caller is responsible for the parent key prefix when
// nesting). Used for the root object and the body of nested objects.
func (e *encoder) encodeObjectBody(m map[string]any, depth int) error {
	// Stable key order: Go map iteration is random; the TOON spec requires
	// preserving "key order as encountered by the encoder" (§2), which we
	// interpret as sorted-alphabetical for deterministic output. The
	// reference @toon-format/toon encoder also emits sorted keys for the
	// root object (insertion order is preserved when the source is a JS
	// object literal, but Go maps have no insertion order).
	keys := sortedKeys(m)
	for _, k := range keys {
		v := m[k]
		if err := e.encodeKeyValue(k, v, depth); err != nil {
			return err
		}
	}
	return nil
}

func (e *encoder) encodeKeyValue(key string, val any, depth int) error {
	norm, err := normalize(val)
	if err != nil {
		return fmt.Errorf("toon: key %q: %w", key, err)
	}
	e.writeIndent(depth)
	switch n := norm.(type) {
	case nil:
		// Per §8: primitive fields use "key: value" with single space.
		e.writeKey(key)
		e.buf.WriteString(": null\n")
	case map[string]any:
		// Per §8: nested or empty objects use bare "key:" (no space after).
		e.writeKey(key)
		e.buf.WriteByte(':')
		if len(n) == 0 {
			e.buf.WriteString("\n")
			return nil
		}
		e.buf.WriteString("\n")
		return e.encodeObjectBody(n, depth+1)
	case []any:
		// Per §9: array headers include the key inline, e.g.,
		// "key[N]: v1,v2" (primitive) or "key[N]{f1,f2}:\n  ..." (block).
		if err := e.writeArrayInlineOrBlock(n, key, depth, true); err != nil {
			return err
		}
	default:
		// Per §8: primitive fields use "key: value" with single space.
		e.writeKey(key)
		e.buf.WriteString(": ")
		e.writeScalar(n)
		e.buf.WriteByte('\n')
	}
	return nil
}

// writeArrayInlineOrBlock emits an array header at the current indent
// level and chooses between inline / tabular / expanded-list forms.
// `key` may be "" when called from the root encoder for a bare array.
// includeKey controls whether the key is prefixed to the header (true for
// a normal "key:" call site; false for a nested-array-on-hyphen-line call
// site where the caller has already written "- ").
func (e *encoder) writeArrayInlineOrBlock(arr []any, key string, depth int, includeKey bool) error {
	// Empty array → "[]"
	if len(arr) == 0 {
		e.buf.WriteString("[]\n")
		return nil
	}
	header := e.arrayHeader(arr, key, includeKey)
	e.buf.WriteString(header)

	// Choose the rendering mode.
	if isPrimitiveArray(arr) {
		// Inline form: key[N]: v1,v2,v3
		e.buf.WriteByte(' ')
		for i, item := range arr {
			if i > 0 {
				e.buf.WriteByte(e.delim)
			}
			e.writeScalar(item)
		}
		e.buf.WriteByte('\n')
		return nil
	}
	if rows, ok := tabularRows(arr); ok {
		// Tabular form: header already written; emit one row per object.
		e.buf.WriteByte('\n')
		for _, row := range rows {
			e.writeIndent(depth + 1)
			for i, cell := range row {
				if i > 0 {
					e.buf.WriteByte(e.delim)
				}
				e.writeScalar(cell)
			}
			e.buf.WriteByte('\n')
		}
		return nil
	}
	// Expanded list form: each item starts with "- " at depth+1.
	e.buf.WriteByte('\n')
	return e.encodeListItems(arr, depth+1)
}

// encodeListItems emits an expanded list of objects. Object items are
// rendered with "- key: value" lines (first field on the hyphen line,
// remaining fields at depth+1). Primitive items are rendered with "- value".
func (e *encoder) encodeListItems(arr []any, depth int) error {
	for _, item := range arr {
		norm, err := normalize(item)
		if err != nil {
			return fmt.Errorf("toon: array item: %w", err)
		}
		e.writeIndent(depth)
		switch n := norm.(type) {
		case map[string]any:
			if len(n) == 0 {
				// Empty object → bare "-" per §10
				e.buf.WriteString("-\n")
				continue
			}
			keys := sortedKeys(n)
			// First key on the hyphen line; rest at depth+1.
			e.buf.WriteString("- ")
			if err := e.writeFirstAndRest(n, keys, depth); err != nil {
				return err
			}
		case []any:
			// Nested array item: header on the hyphen line.
			e.buf.WriteString("- ")
			if err := e.writeArrayInlineOrBlock(n, "", depth, false); err != nil {
				return err
			}
		default:
			e.buf.WriteString("- ")
			e.writeScalar(n)
			e.buf.WriteByte('\n')
		}
	}
	return nil
}

// writeFirstAndRest writes the first key:value of an object on the current
// (hyphen) line and remaining keys at depth+1 with continuation indent.
func (e *encoder) writeFirstAndRest(m map[string]any, keys []string, depth int) error {
	first := keys[0]
	v := m[first]
	norm, err := normalize(v)
	if err != nil {
		return err
	}
	switch n := norm.(type) {
	case nil:
		e.writeKey(first)
		e.buf.WriteString(": null\n")
	case map[string]any:
		e.writeKey(first)
		e.buf.WriteByte(':')
		if len(n) == 0 {
			e.buf.WriteString("\n")
		} else {
			e.buf.WriteString("\n")
			if err := e.encodeObjectBody(n, depth+2); err != nil {
				return err
			}
		}
	case []any:
		if err := e.writeArrayInlineOrBlock(n, first, depth, true); err != nil {
			return err
		}
	default:
		e.writeKey(first)
		e.buf.WriteString(": ")
		e.writeScalar(n)
		e.buf.WriteByte('\n')
	}
	for _, k := range keys[1:] {
		if err := e.encodeKeyValue(k, m[k], depth+1); err != nil {
			return err
		}
	}
	return nil
}

// encodeArray is the entry for a root-level array.
func (e *encoder) encodeArray(arr []any, key string, depth int) error {
	if len(arr) == 0 {
		e.buf.WriteString("[]")
		return nil
	}
	if isPrimitiveArray(arr) {
		header := arrayHeaderLenOnly(key, len(arr), e.delim)
		e.buf.WriteString(header)
		e.buf.WriteByte(' ')
		for i, item := range arr {
			if i > 0 {
				e.buf.WriteByte(e.delim)
			}
			e.writeScalar(item)
		}
		e.buf.WriteByte('\n')
		return nil
	}
	if rows, ok := tabularRows(arr); ok {
		header := e.arrayHeader(arr, key, true)
		e.buf.WriteString(header)
		e.buf.WriteByte('\n')
		for _, row := range rows {
			e.writeIndent(depth + 1)
			for i, cell := range row {
				if i > 0 {
					e.buf.WriteByte(e.delim)
				}
				e.writeScalar(cell)
			}
			e.buf.WriteByte('\n')
		}
		return nil
	}
	header := arrayHeaderLenOnly(key, len(arr), e.delim)
	e.buf.WriteString(header)
	e.buf.WriteByte('\n')
	return e.encodeListItems(arr, depth+1)
}

// arrayHeader returns the full header line including field list for
// tabular arrays, or just [N]: for non-tabular. The caller writes the
// trailing space + content (or \n).
//
// includeKey controls whether the leading key segment is emitted. When
// called from encodeKeyValue the caller has already written "key:", so
// the key is omitted here (only the bracketed length + optional fields).
// When called from encodeArray (root) the key is included.
func (e *encoder) arrayHeader(arr []any, key string, includeKey bool) string {
	if rows, ok := tabularRows(arr); ok {
		fields := tabularFields(arr)
		_ = rows
		var sb strings.Builder
		if includeKey && key != "" {
			sb.WriteString(key)
		}
		sb.WriteByte('[')
		sb.WriteString(strconv.Itoa(len(arr)))
		if e.delim != ',' {
			sb.WriteByte(e.delim)
		}
		sb.WriteByte(']')
		sb.WriteByte('{')
		for i, f := range fields {
			if i > 0 {
				sb.WriteByte(e.delim)
			}
			sb.WriteString(quoteKeyIfNeeded(f))
		}
		sb.WriteByte('}')
		sb.WriteByte(':')
		return sb.String()
	}
	if includeKey {
		return arrayHeaderLenOnly(key, len(arr), e.delim)
	}
	return arrayHeaderLenOnly("", len(arr), e.delim)
}

func arrayHeaderLenOnly(key string, n int, delim byte) string {
	var sb strings.Builder
	if key != "" {
		sb.WriteString(key)
	}
	sb.WriteByte('[')
	sb.WriteString(strconv.Itoa(n))
	if delim != ',' {
		sb.WriteByte(delim)
	}
	sb.WriteByte(']')
	sb.WriteByte(':')
	return sb.String()
}

// ---------------------------------------------------------------------------
// Scalar emission
// ---------------------------------------------------------------------------

func (e *encoder) writeScalar(v any) {
	switch n := v.(type) {
	case nil:
		e.buf.WriteString("null")
	case bool:
		if n {
			e.buf.WriteString("true")
		} else {
			e.buf.WriteString("false")
		}
	case string:
		e.writeString(n, false)
	case json.Number:
		e.writeNumber(string(n))
	case float64:
		e.writeFloat(n)
	case float32:
		e.writeFloat(float64(n))
	case int:
		e.writeFloat(float64(n))
	case int64:
		e.writeFloat(float64(n))
	case int32:
		e.writeFloat(float64(n))
	default:
		// Should be unreachable after normalize() but guard anyway.
		e.buf.WriteString("null")
	}
}

// writeString emits v as a TOON string, quoting if required.
func (e *encoder) writeString(s string, isKey bool) {
	if needsQuoting(s, isKey) {
		e.buf.WriteByte('"')
		e.writeEscaped(s)
		e.buf.WriteByte('"')
		return
	}
	e.buf.WriteString(s)
}

// writeEscaped writes s with TOON escape sequences inside a quoted string.
// Per §7.1: backslash, double-quote, newline, carriage return, tab, and
// control characters U+0000..U+0008, U+000B, U+000C, U+000E..U+001F must
// be escaped. Other characters pass through verbatim.
func (e *encoder) writeEscaped(s string) {
	for _, r := range s {
		switch r {
		case '\\':
			e.buf.WriteString(`\\`)
		case '"':
			e.buf.WriteString(`\"`)
		case '\n':
			e.buf.WriteString(`\n`)
		case '\r':
			e.buf.WriteString(`\r`)
		case '\t':
			e.buf.WriteString(`\t`)
		case '\b':
			e.buf.WriteString(`\b`)
		case '\f':
			e.buf.WriteString(`\f`)
		default:
			if r < 0x20 {
				fmt.Fprintf(e.buf, `\u%04x`, r)
			} else {
				e.buf.WriteRune(r)
			}
		}
	}
}

// needsQuoting reports whether a string must be wrapped in double quotes.
// Rules derived from §7.2 (string values) and §7.3 (keys) of the TOON
// spec and cross-checked against the @toon-format/toon reference encoder.
//
// Strings that look like numbers / booleans / null are also quoted (the
// spec lists this as a quoting requirement to disambiguate from typed
// literals).
func needsQuoting(s string, isKey bool) bool {
	if s == "" {
		return true
	}
	// Single hyphen is always quoted (looks like a list marker).
	if s == "-" {
		return true
	}
	// Any character requiring escape triggers quoting.
	if strings.ContainsAny(s, ":\"\\\n\r\t") {
		return true
	}
	if strings.ContainsRune(s, '\b') || strings.ContainsRune(s, '\f') {
		return true
	}
	// Control characters (U+0000..U+001F excluding the handled ones above).
	for _, r := range s {
		if r < 0x20 {
			return true
		}
	}
	if isKey {
		// Key-specific rules (§7.3):
		//   - contains ':', '[', ']', '{', '}', ',', '"', or whitespace
		//   - starts with '-'
		//   - contains only digits (would be parsed as a number)
		if strings.ContainsAny(s, ":[]{}, \t\"") || strings.HasPrefix(s, "-") {
			return true
		}
		// Numeric-only key: must be quoted.
		if isAllDigits(s) {
			return true
		}
		return false
	}
	// Value-specific rules (§7.2):
	//   - contains ':' or ',' (TOON delimiters / structural)
	//   - starts with '-' followed by space or end-of-string (list marker)
	//   - starts with '[', '{' (array/object syntax lookalikes)
	//   - matches the bare keywords true / false / null
	//   - parses as a number, OR has a leading-zero form (e.g., "05"),
	//     OR matches scientific notation "1e-6"
	//   - has leading or trailing whitespace
	if strings.ContainsRune(s, ',') {
		return true
	}
	if strings.HasPrefix(s, "- ") || s == "-" {
		return true
	}
	if strings.HasPrefix(s, "[") || strings.HasPrefix(s, "{") {
		return true
	}
	if s == "true" || s == "false" || s == "null" {
		return true
	}
	if looksLikeNumber(s) {
		return true
	}
	if strings.HasPrefix(s, " ") || strings.HasSuffix(s, " ") {
		return true
	}
	return false
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// looksLikeNumber reports whether s would parse as a Go float64 literal
// (decimal or scientific), AND is in canonical form (no leading zeros,
// no trailing-zero fractional part issues). It is intentionally permissive
// — the spec is conservative and quotes any string that the decoder
// would parse as a number.
func looksLikeNumber(s string) bool {
	if s == "" {
		return false
	}
	// Try strconv.ParseFloat first.
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return false
	}
	// Reject leading-zero forms (e.g., "05") — these are non-numeric per
	// §4 decoder rules.
	if strings.Contains(s, ".") || strings.ContainsAny(s, "eE") {
		// Allow normal forms.
		_ = f
		return true
	}
	// Integer form: must not have leading zeros (unless the string is "0").
	if len(s) > 1 && s[0] == '0' {
		return false
	}
	if len(s) > 2 && s[0] == '-' && s[1] == '0' {
		return false
	}
	return true
}

// quoteKeyIfNeeded is a top-level wrapper for key emission that respects
// the §7.3 quoting rules. Used by arrayHeader.
func quoteKeyIfNeeded(k string) string {
	if needsQuoting(k, true) {
		var sb strings.Builder
		sb.WriteByte('"')
		for _, r := range k {
			switch r {
			case '\\':
				sb.WriteString(`\\`)
			case '"':
				sb.WriteString(`\"`)
			case '\n':
				sb.WriteString(`\n`)
			case '\r':
				sb.WriteString(`\r`)
			case '\t':
				sb.WriteString(`\t`)
			default:
				if r < 0x20 {
					fmt.Fprintf(&sb, `\u%04x`, r)
				} else {
					sb.WriteRune(r)
				}
			}
		}
		sb.WriteByte('"')
		return sb.String()
	}
	return k
}

// writeKey writes a key. The trailing colon is added by the caller
// (encodeKeyValue, writeFirstAndRest, or writeArrayInlineOrBlock).
func (e *encoder) writeKey(k string) {
	e.writeString(k, true)
}

// ---------------------------------------------------------------------------
// Number formatting
// ---------------------------------------------------------------------------

// writeFloat formats n per §2 canonical rules:
//   - NaN, +Inf, -Inf → null
//   - 0 / -0 → "0"
//   - integer-valued float → as integer ("5" not "5.0")
//   - |n| < 1e-6 or |n| >= 1e21 → lowercase exponent form
//   - otherwise → minimal decimal
func (e *encoder) writeFloat(n float64) {
	if math.IsNaN(n) || math.IsInf(n, 0) {
		e.buf.WriteString("null")
		return
	}
	if n == 0 {
		e.buf.WriteString("0")
		return
	}
	abs := math.Abs(n)
	if abs < 1e-6 || abs >= 1e21 {
		// Exponential form. strconv.FormatFloat uses 'e' with explicit sign.
		e.buf.WriteString(strconv.FormatFloat(n, 'e', -1, 64))
		return
	}
	// Integer-valued floats render as integers.
	if n == math.Trunc(n) {
		e.buf.WriteString(strconv.FormatInt(int64(n), 10))
		return
	}
	// Otherwise, minimal decimal. strconv.FormatFloat with -1 precision
	// produces the shortest representation that round-trips.
	e.buf.WriteString(strconv.FormatFloat(n, 'f', -1, 64))
}

// writeNumber handles json.Number (which is just a string).
func (e *encoder) writeNumber(s string) {
	// Best-effort canonical form: try to parse and re-format.
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		e.writeFloat(f)
		return
	}
	// Not parseable as float (shouldn't happen with valid JSON numbers)
	// — emit as a quoted string to avoid losing data.
	e.buf.WriteByte('"')
	e.buf.WriteString(s)
	e.buf.WriteByte('"')
}

// ---------------------------------------------------------------------------
// Tabular / array-shape detection
// ---------------------------------------------------------------------------

// isPrimitiveArray reports whether every element is a JSON primitive
// (string, number, bool, nil).
func isPrimitiveArray(arr []any) bool {
	for _, v := range arr {
		switch v.(type) {
		case nil, bool, string, float64, float32, int, int64, int32,
			uint, uint64, uint32, json.Number:
			// primitive
		default:
			return false
		}
	}
	return true
}

// tabularRows returns the rendered rows for a uniform array of objects if
// the array is in tabular form. The second return value is false if the
// array is not tabular.
//
// Per §9.3: tabular form is used for arrays of objects where every object
// has the same set of keys (key set equality, ignoring order) and every
// value in each object is a primitive. The field order is taken from the
// first object (per the §9.3 fixture: "uses field order from first object
// for tabular headers").
func tabularRows(arr []any) ([][]any, bool) {
	if len(arr) == 0 {
		return nil, false
	}
	first, ok := arr[0].(map[string]any)
	if !ok {
		return nil, false
	}
	if len(first) == 0 {
		// Empty objects are NOT tabular (§9.3: "Empty objects {} MUST NOT
		// use tabular form"). Use expanded-list form.
		return nil, false
	}
	fields := sortedKeys(first)
	fieldSet := stringSet(fields)

	rows := make([][]any, 0, len(arr))
	for _, item := range arr {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, false
		}
		if !sameKeySet(obj, fieldSet) {
			return nil, false
		}
		row := make([]any, len(fields))
		for i, f := range fields {
			v := obj[f]
			norm, err := normalize(v)
			if err != nil {
				return nil, false
			}
			// Tabular cells must be primitives (§9.3). Nested objects /
			// arrays force expanded-list form.
			switch norm.(type) {
			case map[string]any, []any:
				return nil, false
			}
			row[i] = norm
		}
		rows = append(rows, row)
	}
	return rows, true
}

// tabularFields returns the field names for the tabular header of arr.
// Caller must have verified the array is tabular (via tabularRows) first.
func tabularFields(arr []any) []string {
	first, _ := arr[0].(map[string]any)
	return sortedKeys(first)
}

// ---------------------------------------------------------------------------
// Type normalisation
// ---------------------------------------------------------------------------

// normalize converts any Go value to a JSON-model value:
//
//	nil, bool, string, float64, map[string]any, []any, json.Number
//
// It honours json.Marshaler and encoding.TextMarshaler (matching the
// reference encoder's documented hook behaviour per §3).
func normalize(v any) (any, error) {
	switch n := v.(type) {
	case nil, bool, string, float64, map[string]any, []any, json.Number:
		return n, nil
	case float32:
		return float64(n), nil
	case int:
		return float64(n), nil
	case int8:
		return float64(n), nil
	case int16:
		return float64(n), nil
	case int32:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case uint:
		return float64(n), nil
	case uint8:
		return float64(n), nil
	case uint16:
		return float64(n), nil
	case uint32:
		return float64(n), nil
	case uint64:
		return float64(n), nil
	case json.Marshaler:
		// json.Marshaler is honoured (§3). Re-decode the JSON output so
		// the encoder only ever sees JSON-model types.
		b, err := n.MarshalJSON()
		if err != nil {
			return nil, fmt.Errorf("json.Marshaler: %w", err)
		}
		var decoded any
		if err := json.Unmarshal(b, &decoded); err != nil {
			return nil, fmt.Errorf("json.Marshaler output: %w", err)
		}
		return decoded, nil
	case encodingTextMarshaler:
		b, err := n.MarshalText()
		if err != nil {
			return nil, fmt.Errorf("TextMarshaler: %w", err)
		}
		return string(b), nil
	default:
		// Fallback: round-trip through json.Marshal so any type with
		// reflection-friendly fields is still encodable.
		b, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("toon: unsupported type %T: %w", v, err)
		}
		var decoded any
		if err := json.Unmarshal(b, &decoded); err != nil {
			return nil, fmt.Errorf("toon: unsupported type %T: %w", v, err)
		}
		return decoded, nil
	}
}

// encodingTextMarshaler is the subset of encoding.TextMarshaler we use.
// Defined as a local interface to avoid importing encoding (and to make
// the intent explicit).
type encodingTextMarshaler interface {
	MarshalText() ([]byte, error)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func stringSet(ss []string) map[string]struct{} {
	out := make(map[string]struct{}, len(ss))
	for _, s := range ss {
		out[s] = struct{}{}
	}
	return out
}

func sameKeySet(m map[string]any, set map[string]struct{}) bool {
	if len(m) != len(set) {
		return false
	}
	for k := range m {
		if _, ok := set[k]; !ok {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Minimal decoder (round-trip oracle only)
// ---------------------------------------------------------------------------

// Decode parses TOON data into dst, which must be a pointer to any.
//
// The decoder is intentionally minimal — it exists to support round-trip
// tests in encode_test.go and is not part of the production tool surface
// (LLM consumers receive TOON bytes directly; if they need JSON they pass
// outputFormat="json").
//
// Implementation strategy: convert the TOON document into equivalent JSON
// text via a recursive line-based parser, then json.Unmarshal into the
// destination. This keeps the decoder small and reliable at the cost of
// supporting a strict subset of TOON — exactly the subset the encoder
// produces. Features NOT supported:
//
//   - Tab/pipe delimiter variants (only comma)
//   - Key folding with dots
//   - Block scalars / literal strings spanning lines
//   - Comments (TOON forbids them anyway)
func Decode(data []byte, dst *any) error {
	if dst == nil {
		return errors.New("toon: Decode: dst is nil")
	}
	jsonBytes, err := toonToJSON(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(jsonBytes, dst)
}

// toonToJSON converts a TOON document to a JSON document with the same
// data shape. The output is suitable for json.Unmarshal into any.
func toonToJSON(data []byte) ([]byte, error) {
	p := &toonParser{src: string(data), indentSize: 2}
	doc, err := p.parseRoot()
	if err != nil {
		return nil, err
	}
	if !p.atEnd() {
		return nil, fmt.Errorf("toon: trailing content at offset %d: %q", p.pos, p.src[p.pos:])
	}
	return []byte(doc), nil
}

// toonParser walks the TOON source by line. Depth is measured in
// (indent-unit) levels; each level corresponds to indentSize spaces of
// leading whitespace.
type toonParser struct {
	src        string
	pos        int
	indentSize int
}

func (p *toonParser) atEnd() bool {
	p.skipBlankLines()
	return p.pos >= len(p.src)
}

// skipBlankLines advances past any blank lines.
func (p *toonParser) skipBlankLines() {
	for p.pos < len(p.src) && p.src[p.pos] == '\n' {
		p.pos++
	}
}

// nextLine returns the next non-blank line (trimmed of leading spaces),
// along with its indent width (count of leading spaces). Updates p.pos to
// point past the newline.
func (p *toonParser) nextLine() (indent int, line string, ok bool) {
	p.skipBlankLines()
	if p.pos >= len(p.src) {
		return 0, "", false
	}
	start := p.pos
	indent = 0
	for p.pos < len(p.src) && p.src[p.pos] == ' ' {
		indent++
		p.pos++
	}
	nl := strings.IndexByte(p.src[p.pos:], '\n')
	var body string
	if nl < 0 {
		body = p.src[p.pos:]
		p.pos = len(p.src)
	} else {
		body = p.src[p.pos : p.pos+nl]
		p.pos += nl + 1
	}
	_ = start
	return indent, body, true
}

// peekLine returns the next non-blank line's indent and body without
// advancing the cursor.
func (p *toonParser) peekLine() (indent int, line string, ok bool) {
	saved := p.pos
	ind, body, ok := p.nextLine()
	p.pos = saved
	return ind, body, ok
}

// parseRoot dispatches on the first non-blank line.
func (p *toonParser) parseRoot() (string, error) {
	_, first, ok := p.peekLine()
	if !ok {
		return "{}", nil
	}
	trimmed := strings.TrimSpace(first)
	if trimmed == "" {
		return "{}", nil
	}
	if trimmed == "[]" {
		p.nextLine()
		return "[]", nil
	}
	if trimmed[0] == '[' {
		_, line, _ := p.nextLine()
		// Root array: items live at depth 1 (2-space indent).
		return p.parseArrayFromHeaderLine(line, 1)
	}
	// Single-line primitive root (no colon, no newline): "hello", "42",
	// "true", etc. A colon-bearing first line belongs to an object.
	if !strings.Contains(trimmed, ":") {
		p.nextLine()
		return toonScalarToJSON(trimmed), nil
	}
	// Root object (depth 0 in indent-units).
	return p.parseObjectBody(0)
}

// parseObjectBody reads key:value lines at the given depth (in
// indent-unit levels, where each level is indentSize spaces). Lines at
// the correct depth are consumed; shallower lines terminate the block.
func (p *toonParser) parseObjectBody(depth int) (string, error) {
	wantIndent := depth * p.indentSize
	var sb strings.Builder
	sb.WriteByte('{')
	first := true
	for {
		ind, line, ok := p.peekLine()
		if !ok {
			break
		}
		if ind < wantIndent {
			break
		}
		if ind > wantIndent {
			return "", fmt.Errorf("toon: unexpected indent %d (want %d) at offset %d", ind, wantIndent, p.pos)
		}
		p.nextLine()
		kind, key, val := classifyObjectField(line)
		if !first {
			sb.WriteByte(',')
		}
		first = false
		switch kind {
		case fieldPrimitive:
			sb.WriteString(jsonString(key))
			sb.WriteByte(':')
			sb.WriteString(toonScalarToJSON(val))
		case fieldNestedObject:
			sb.WriteString(jsonString(key))
			sb.WriteByte(':')
			nested, err := p.parseObjectBody(depth + 1)
			if err != nil {
				return "", err
			}
			sb.WriteString(nested)
		case fieldArrayInline, fieldArrayBlock:
			sb.WriteString(jsonString(key))
			sb.WriteByte(':')
			arr, err := p.parseArrayFromHeaderLine(val, depth+1)
			if err != nil {
				return "", err
			}
			sb.WriteString(arr)
		default:
			return "", fmt.Errorf("toon: unrecognised object field line %q", line)
		}
	}
	sb.WriteByte('}')
	return sb.String(), nil
}

// fieldKind classifies a single object-body line.
type fieldKind int

const (
	fieldUnknown      fieldKind = iota
	fieldPrimitive              // "key: value" — value is a JSON primitive
	fieldNestedObject           // "key:" — value is a nested object on subsequent lines
	fieldArrayInline            // "key[N]: v1,v2,..." — inline primitive array
	fieldArrayBlock             // "key[N]{f1,f2}:" — block array (tabular or list)
)

// classifyObjectField identifies how to interpret an object-body line.
// It returns (kind, key, val). For fieldArrayInline / fieldArrayBlock,
// val is the array-header line MINUS the key prefix (no key, just
// "[N]: ..." or "[N]{f1,f2}:" ready for parseArrayFromHeaderLine). For
// fieldNestedObject, val is "". For fieldPrimitive, val is the trimmed
// primitive text.
func classifyObjectField(line string) (fieldKind, string, string) {
	// Find the first unquoted ':'.
	inQ := false
	escape := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		if escape {
			escape = false
			continue
		}
		if c == '\\' && inQ {
			escape = true
			continue
		}
		if c == '"' {
			inQ = !inQ
			continue
		}
		if c == ':' && !inQ {
			k := line[:i]
			rest := strings.TrimSpace(line[i+1:])
			// Array header: "key[N]: ..." or "key[N]{f1,f2}: ..." —
			// the key ends with ']' (no field list) or '}' (field list).
			if isArrayHeaderKey(k) {
				openIdx := strings.LastIndex(k, "[")
				if openIdx < 0 {
					return fieldUnknown, line, ""
				}
				headerKey := k[:openIdx]
				arrHeader := k[openIdx:]
				if rest != "" {
					arrHeader += " " + rest
				}
				headerKey, _ = unquoteToonKey(headerKey)
				if hasInlineArrayValues(arrHeader) {
					return fieldArrayInline, headerKey, arrHeader
				}
				return fieldArrayBlock, headerKey, arrHeader
			}
			k, _ = unquoteToonKey(k)
			if rest == "" {
				return fieldNestedObject, k, ""
			}
			if rest[0] == '[' {
				return fieldArrayInline, k, rest
			}
			return fieldPrimitive, k, rest
		}
	}
	return fieldUnknown, line, ""
}

// isArrayHeaderKey reports whether k looks like "name[N]" or
// "name[N]{f1,f2}" — i.e., it ends with ']' (no field list) or '}' (with
// field list) and contains a matching '['.
func isArrayHeaderKey(k string) bool {
	if len(k) == 0 {
		return false
	}
	last := k[len(k)-1]
	if last != ']' && last != '}' {
		return false
	}
	return strings.Contains(k, "[")
}

// hasInlineArrayValues reports whether an array header (the
// "[N]{f1,f2,...}:" portion) has values on the same line (inline form)
// vs a colon only (block form). The header is given without the trailing
// key prefix — e.g., "[3]" or "[2]{id,name}".
func hasInlineArrayValues(header string) bool {
	// Find the closing ']' (or '}' if there are fields).
	closeIdx := strings.IndexAny(header, "]}")
	if closeIdx < 0 {
		return false
	}
	after := strings.TrimSpace(header[closeIdx+1:])
	return after != ""
}

// parseArrayFromHeaderLine parses an array whose header line is `line`.
// `line` may be "[N]: ...", "[N]{f1,f2}: ...", or just "[N]" / "[N]{...}".
// The caller has already removed the leading key (if any).
func (p *toonParser) parseArrayFromHeaderLine(line string, depth int) (string, error) {
	// Strip a trailing colon (the header terminator).
	line = strings.TrimSpace(strings.TrimSuffix(line, ":"))
	if line == "" {
		return "", fmt.Errorf("toon: empty array header")
	}
	if line[0] != '[' {
		return "", fmt.Errorf("toon: array header must start with '[': %q", line)
	}
	closeIdx := strings.IndexByte(line, ']')
	if closeIdx < 0 {
		return "", fmt.Errorf("toon: missing ']' in array header %q", line)
	}
	body := line[1:closeIdx]
	delim := byte(',')
	if body != "" {
		switch body[len(body)-1] {
		case '	':
			delim = '	'
			body = body[:len(body)-1]
		case '|':
			delim = '|'
			body = body[:len(body)-1]
		}
	}
	n, err := strconv.Atoi(strings.TrimSpace(body))
	if err != nil {
		return "", fmt.Errorf("toon: bad array length in header %q: %w", line, err)
	}
	rest := strings.TrimSpace(line[closeIdx+1:])
	// If rest starts with ':', strip it (the colon that follows the
	// closing bracket for inline arrays — e.g., "key[N]: v1,v2").
	if strings.HasPrefix(rest, ":") {
		rest = strings.TrimSpace(rest[1:])
	}
	var fields []string
	if strings.HasPrefix(rest, "{") {
		end := strings.IndexByte(rest, '}')
		if end < 0 {
			return "", fmt.Errorf("toon: missing '}' in array field list %q", line)
		}
		fstr := rest[1:end]
		parts := strings.Split(fstr, string(delim))
		fields = make([]string, len(parts))
		for i, p := range parts {
			f := strings.TrimSpace(p)
			if len(f) >= 2 && f[0] == '"' && f[len(f)-1] == '"' {
				uq, _ := unquoteToonString(f[1 : len(f)-1])
				fields[i] = uq
			} else {
				fields[i] = f
			}
		}
		rest = strings.TrimSpace(rest[end+1:])
	}
	if rest != "" {
		// Inline form.
		parts := strings.Split(rest, string(delim))
		if len(parts) != n {
			return "", fmt.Errorf("toon: inline array length mismatch: header says %d, got %d", n, len(parts))
		}
		var sb strings.Builder
		sb.WriteByte('[')
		for i, part := range parts {
			if i > 0 {
				sb.WriteByte(',')
			}
			sb.WriteString(toonScalarToJSON(strings.TrimSpace(part)))
		}
		sb.WriteByte(']')
		return sb.String(), nil
	}
	// Block form: rows/list at `depth` (the caller has already advanced
	// the indent-unit depth for us).
	if len(fields) > 0 {
		return p.parseTabularArray(n, fields, delim, depth)
	}
	return p.parseListArray(n, depth)
}

func (p *toonParser) parseTabularArray(n int, fields []string, delim byte, depth int) (string, error) {
	wantIndent := depth * p.indentSize
	var sb strings.Builder
	sb.WriteByte('[')
	for i := 0; i < n; i++ {
		ind, line, ok := p.nextLine()
		if !ok {
			return "", fmt.Errorf("toon: expected %d tabular rows, got %d", n, i)
		}
		if ind != wantIndent {
			return "", fmt.Errorf("toon: tabular row at indent %d, want %d", ind, wantIndent)
		}
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteByte('{')
		cells := splitTopLevel(line, delim)
		for j, f := range fields {
			if j > 0 {
				sb.WriteByte(',')
			}
			sb.WriteString(jsonString(f))
			sb.WriteByte(':')
			var cell string
			if j < len(cells) {
				cell = strings.TrimSpace(cells[j])
			}
			sb.WriteString(toonScalarToJSON(cell))
		}
		sb.WriteByte('}')
	}
	sb.WriteByte(']')
	return sb.String(), nil
}

func (p *toonParser) parseListArray(n int, depth int) (string, error) {
	// depth is in indent-units. List items start at depth*indentSize
	// spaces; sibling keys at (depth+1)*indentSize spaces.
	wantItemIndent := depth * p.indentSize
	wantSiblingIndent := (depth + 1) * p.indentSize
	var sb strings.Builder
	sb.WriteByte('[')
	for i := 0; i < n; i++ {
		ind, line, ok := p.nextLine()
		if !ok {
			return "", fmt.Errorf("toon: expected %d list items, got %d", n, i)
		}
		if ind != wantItemIndent {
			return "", fmt.Errorf("toon: list item at indent %d, want %d", ind, wantItemIndent)
		}
		if i > 0 {
			sb.WriteByte(',')
		}
		if !strings.HasPrefix(line, "-") {
			return "", fmt.Errorf("toon: expected list item at line %q", line)
		}
		rest := strings.TrimPrefix(line, "-")
		rest = strings.TrimPrefix(rest, " ")
		if rest == "" {
			sb.WriteString("{}")
			continue
		}
		if rest[0] == '[' {
			arr, err := p.parseArrayFromHeaderLine(rest, depth+1)
			if err != nil {
				return "", err
			}
			sb.WriteString(arr)
			continue
		}
		sb.WriteByte('{')
		kind, key, val := classifyObjectField(rest)
		switch kind {
		case fieldPrimitive:
			sb.WriteString(jsonString(key))
			sb.WriteByte(':')
			sb.WriteString(toonScalarToJSON(val))
		case fieldNestedObject:
			sb.WriteString(jsonString(key))
			sb.WriteByte(':')
			nested, err := p.parseObjectBody(depth + 1)
			if err != nil {
				return "", err
			}
			sb.WriteString(nested)
		case fieldArrayInline, fieldArrayBlock:
			sb.WriteString(jsonString(key))
			sb.WriteByte(':')
			arr, err := p.parseArrayFromHeaderLine(val, depth+1)
			if err != nil {
				return "", err
			}
			sb.WriteString(arr)
		default:
			return "", fmt.Errorf("toon: unrecognised list-object field %q", rest)
		}
		for {
			ind, line, ok := p.peekLine()
			if !ok || ind < wantSiblingIndent {
				break
			}
			if ind != wantSiblingIndent {
				break
			}
			p.nextLine()
			kind2, k2, v2 := classifyObjectField(line)
			sb.WriteByte(',')
			switch kind2 {
			case fieldPrimitive:
				sb.WriteString(jsonString(k2))
				sb.WriteByte(':')
				sb.WriteString(toonScalarToJSON(v2))
			case fieldNestedObject:
				sb.WriteString(jsonString(k2))
				sb.WriteByte(':')
				nested2, err := p.parseObjectBody(depth + 2)
				if err != nil {
					return "", err
				}
				sb.WriteString(nested2)
			case fieldArrayInline, fieldArrayBlock:
				sb.WriteString(jsonString(k2))
				sb.WriteByte(':')
				arr2, err := p.parseArrayFromHeaderLine(v2, depth+2)
				if err != nil {
					return "", err
				}
				sb.WriteString(arr2)
			default:
				return "", fmt.Errorf("toon: unrecognised list-object sibling %q", line)
			}
		}
		sb.WriteByte('}')
	}
	sb.WriteByte(']')
	return sb.String(), nil
}

// unquoteToonKey unquotes a quoted key.
func unquoteToonKey(k string) (string, error) {
	if len(k) >= 2 && k[0] == '"' && k[len(k)-1] == '"' {
		return unquoteToonString(k[1 : len(k)-1])
	}
	return k, nil
}

// unquoteToonString decodes TOON-style escapes in s.
func unquoteToonString(s string) (string, error) {
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' {
			sb.WriteByte(c)
			continue
		}
		if i+1 >= len(s) {
			return "", fmt.Errorf("trailing backslash in %q", s)
		}
		i++
		switch s[i] {
		case '\\':
			sb.WriteByte('\\')
		case '"':
			sb.WriteByte('"')
		case 'n':
			sb.WriteByte('\n')
		case 'r':
			sb.WriteByte('\r')
		case 't':
			sb.WriteByte('	')
		case 'b':
			sb.WriteByte('\b')
		case 'f':
			sb.WriteByte('\f')
		case 'u':
			if i+4 >= len(s) {
				return "", fmt.Errorf("short \\u escape in %q", s)
			}
			var r rune
			_, err := fmt.Sscanf(s[i+1:i+5], "%04x", &r)
			if err != nil {
				return "", fmt.Errorf("bad \\u escape in %q: %v", s, err)
			}
			sb.WriteRune(r)
			i += 4
		default:
			return "", fmt.Errorf("unknown escape \\%c in %q", s[i], s)
		}
	}
	return sb.String(), nil
}

// toonScalarToJSON converts an unquoted TOON scalar token to its JSON
// representation.
func toonScalarToJSON(tok string) string {
	switch tok {
	case "null":
		return "null"
	case "true":
		return "true"
	case "false":
		return "false"
	case "":
		return "null"
	}
	if len(tok) >= 2 && tok[0] == '"' && tok[len(tok)-1] == '"' {
		// Already quoted; return as-is (the quotes are JSON-compatible).
		s, err := unquoteToonString(tok[1 : len(tok)-1])
		if err == nil {
			return jsonString(s)
		}
		return tok
	}
	// Try parsing as a float.
	if _, err := strconv.ParseFloat(tok, 64); err == nil {
		return tok
	}
	// Treat as bare string.
	return jsonString(tok)
}

// jsonString returns s as a JSON string literal (including surrounding quotes).
func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// splitTopLevel splits s on delim, respecting double-quoted regions and
// backslash escapes inside quotes.
func splitTopLevel(s string, delim byte) []string {
	var out []string
	var cur strings.Builder
	inQ := false
	escape := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if escape {
			cur.WriteByte(c)
			escape = false
			continue
		}
		if c == '\\' && inQ {
			cur.WriteByte(c)
			escape = true
			continue
		}
		if c == '"' {
			inQ = !inQ
			cur.WriteByte(c)
			continue
		}
		if c == delim && !inQ {
			out = append(out, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(c)
	}
	out = append(out, cur.String())
	return out
}
