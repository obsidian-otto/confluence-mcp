package jmespath_test

import (
	"errors"
	"strings"
	"testing"

	jmp "github.com/jmespath/go-jmespath"

	mjp "github.com/bennie/mcp-confluence/internal/jmespath"
)

// These tests pin the contract for internal/jmespath.Apply:
//
//   - Empty expression short-circuits; the upstream parser must NOT be called.
//   - Non-empty expressions delegate to the upstream library and round-trip
//     common JMESPath shapes (object access, filter, projection).
//   - Syntax errors surface as a typed error.
//   - Missing field paths return nil, not an error (JMESPath spec behavior).
//   - Large-array dot-projections return a slice.
//
// The "parser" package-level var in apply.go is the seam that lets us prove
// short-circuit behavior without network/mocking infra: we swap it for a
// counter inside the tests, then restore the production binding.

// withCountingParser swaps package's parser fn with a counter, runs fn, then
// restores the original. The counter increments on every Search call,
// independent of success/failure. We use it to assert that empty-expr never
// reaches the upstream.
func withCountingParser(t *testing.T) (counter func() int, restore func()) {
	t.Helper()
	orig := mjp.SwapParser(func(expr string, data any) (any, error) {
		return jmp.Search(expr, data)
	})
	var n int
	c := func() int { return n }
	mjp.SwapParser(func(expr string, data any) (any, error) {
		n++
		return jmp.Search(expr, data)
	})
	return c, func() { mjp.SwapParser(orig) }
}

func TestApply_EmptyExpression_ShortCircuits(t *testing.T) {
	counter, restore := withCountingParser(t)
	defer restore()

	data := map[string]any{"foo": "bar", "n": float64(42)}
	got, err := mjp.Apply("", data)
	if err != nil {
		t.Fatalf("Apply(\"\", data) error = %v, want nil", err)
	}
	gotMap, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("Apply(\"\", data) returned %T, want map[string]any (identity passthrough)", got)
	}
	if gotMap["foo"] != "bar" || gotMap["n"] != float64(42) {
		t.Fatalf("Apply(\"\", data) = %v, want identity %v", gotMap, data)
	}
	if n := counter(); n != 0 {
		t.Fatalf("parser.Search was called %d times for empty expr; want 0 (short-circuit)", n)
	}
}

func TestApply_EmptyExpression_WithNilData(t *testing.T) {
	counter, restore := withCountingParser(t)
	defer restore()

	// No panic, no parser hit, returns the nil identity.
	got, err := mjp.Apply("", nil)
	if err != nil {
		t.Fatalf("Apply(\"\", nil) error = %v, want nil", err)
	}
	if got != nil {
		t.Fatalf("Apply(\"\", nil) = %v, want nil", got)
	}
	if n := counter(); n != 0 {
		t.Fatalf("parser.Search was called %d times for empty expr+nil data; want 0", n)
	}
}

func TestApply_ValidExpr_ObjectAccess(t *testing.T) {
	data := map[string]any{
		"id":   "SP-1",
		"name": "Engineering",
		"key":  "ENG",
	}
	got, err := mjp.Apply("name", data)
	if err != nil {
		t.Fatalf("Apply(\"name\", data) error = %v, want nil", err)
	}
	if got != "Engineering" {
		t.Fatalf("Apply(\"name\", data) = %v, want %q", got, "Engineering")
	}
}

func TestApply_ValidExpr_ArrayFilter(t *testing.T) {
	data := map[string]any{
		"results": []any{
			map[string]any{"id": "1", "status": "current"},
			map[string]any{"id": "2", "status": "archived"},
			map[string]any{"id": "3", "status": "current"},
		},
	}
	got, err := mjp.Apply("results[?status=='current'].id", data)
	if err != nil {
		t.Fatalf("Apply(filter, data) error = %v, want nil", err)
	}
	want := []any{"1", "3"}
	gotSlice, ok := got.([]any)
	if !ok {
		t.Fatalf("Apply(filter, data) = %T, want []any", got)
	}
	if len(gotSlice) != len(want) {
		t.Fatalf("Apply(filter, data) len = %d, want %d", len(gotSlice), len(want))
	}
	for i := range want {
		if gotSlice[i] != want[i] {
			t.Fatalf("Apply(filter, data)[%d] = %v, want %v", i, gotSlice[i], want[i])
		}
	}
}

func TestApply_SyntaxError_ReturnsTypedError(t *testing.T) {
	data := map[string]any{"a": float64(1)}
	_, err := mjp.Apply("@@@bad expression@@@", data)
	if err == nil {
		t.Fatalf("Apply(bad expr, data) error = nil, want non-nil")
	}
	// The wrapper must produce a recognizable error type so callers can
	// distinguish a parse failure from a successful query.
	var jerr *mjp.ExpressionError
	if !errors.As(err, &jerr) {
		t.Fatalf("Apply(bad expr, data) error type = %T, want *ExpressionError", err)
	}
	if jerr.Expression == "" {
		t.Fatalf("ExpressionError.Expression is empty")
	}
	if !strings.Contains(err.Error(), "jmespath") {
		t.Fatalf("error message %q does not mention jmespath", err.Error())
	}
}

func TestApply_DotProjection_LargeArray(t *testing.T) {
	const N = 500
	items := make([]any, N)
	for i := 0; i < N; i++ {
		items[i] = map[string]any{
			"id":     float64(i),
			"title":  "item-" + string(rune('a'+i%26)),
			"status": "current",
		}
	}
	data := map[string]any{"results": items}

	got, err := mjp.Apply("results[*].id", data)
	if err != nil {
		t.Fatalf("Apply(dot-proj, data) error = %v, want nil", err)
	}
	slice, ok := got.([]any)
	if !ok {
		t.Fatalf("Apply(dot-proj, data) = %T, want []any", got)
	}
	if len(slice) != N {
		t.Fatalf("Apply(dot-proj, data) len = %d, want %d", len(slice), N)
	}
	// Spot-check the first and last — proves projection order is preserved.
	if slice[0] != float64(0) {
		t.Fatalf("slice[0] = %v, want 0", slice[0])
	}
	if slice[N-1] != float64(N-1) {
		t.Fatalf("slice[%d] = %v, want %d", N-1, slice[N-1], N-1)
	}
}

func TestApply_MissingField_ReturnsNil_NoError(t *testing.T) {
	data := map[string]any{"a": "x"}
	got, err := mjp.Apply("does.not.exist", data)
	if err != nil {
		t.Fatalf("Apply(missing, data) error = %v, want nil (JMESPath returns nil for missing path)", err)
	}
	if got != nil {
		t.Fatalf("Apply(missing, data) = %v, want nil", got)
	}
}

func TestApply_NestedObject_DeepAccess(t *testing.T) {
	data := map[string]any{
		"page": map[string]any{
			"id": "p1",
			"body": map[string]any{
				"representation": "storage",
				"value":          "<p>hi</p>",
			},
		},
	}
	got, err := mjp.Apply("page.body.value", data)
	if err != nil {
		t.Fatalf("Apply(nested, data) error = %v", err)
	}
	if got != "<p>hi</p>" {
		t.Fatalf("Apply(nested, data) = %v, want %q", got, "<p>hi</p>")
	}
}

// TestSwapParser_RestoresBinding guards against the test seam leaking into
// production — after a swap and a restore, calling Apply must use the real
// parser again. Catches regressions in SwapParser semantics.
func TestSwapParser_RestoresBinding(t *testing.T) {
	// Snap the current production binding.
	prodApply := func() (any, error) {
		return mjp.Apply("a", map[string]any{"a": "from-prod"})
	}
	got, err := prodApply()
	if err != nil {
		t.Fatalf("production Apply error = %v", err)
	}
	if got != "from-prod" {
		t.Fatalf("production Apply = %v, want from-prod", got)
	}

	// Swap, verify counter trips, restore, verify it stops tripping.
	orig := mjp.SwapParser(func(expr string, data any) (any, error) {
		return "swapped", nil
	})
	got, err = mjp.Apply("anything", nil)
	if err != nil {
		t.Fatalf("swapped Apply error = %v", err)
	}
	if got != "swapped" {
		t.Fatalf("swapped Apply = %v, want swapped", got)
	}
	mjp.SwapParser(orig)

	got, err = mjp.Apply("a", map[string]any{"a": "restored"})
	if err != nil {
		t.Fatalf("restored Apply error = %v", err)
	}
	if got != "restored" {
		t.Fatalf("restored Apply = %v, want restored", got)
	}
}
