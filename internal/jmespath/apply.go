// Package jmespath is a thin wrapper over github.com/jmespath/go-jmespath.
//
// It exists so callers can:
//
//  1. Short-circuit on an empty expression (the upstream's Search function
//     always parses; we want zero parse cost when no filter is requested).
//  2. Get a typed error for syntax errors instead of the upstream's plain
//     error string, so callers can distinguish a parse failure from a
//     successful-but-empty query.
//
// The parser binding is package-level so tests can swap it for a counter and
// prove the empty-expr case never reaches the upstream.
package jmespath

import (
	"fmt"

	gj "github.com/jmespath/go-jmespath"
)

// SearchFn is the shape of the upstream search call. The package-level
// `parser` variable holds one of these; tests swap it to instrument calls.
type SearchFn func(expression string, data any) (any, error)

// parser is the upstream search seam. Bound to gj.Search by default.
// Production code must never reassign this; tests are allowed to.
var parser SearchFn = func(expression string, data any) (any, error) {
	return gj.Search(expression, data)
}

// SwapParser replaces the package-level parser binding and returns the prior
// binding. Intended for tests that need to count or stub upstream calls.
//
// Returns the previous binding so callers can restore it via a second call:
//
//	prev := SwapParser(myStub)
//	defer SwapParser(prev)
//
// Not safe for concurrent use; tests are single-goroutine.
func SwapParser(fn SearchFn) SearchFn {
	prev := parser
	parser = fn
	return prev
}

// ExpressionError signals that a JMESPath expression failed to parse.
// Callers can errors.As(err, &ExpressionError{}) to distinguish a parse
// failure from any other error (e.g., a network error raised later in a
// pipeline).
type ExpressionError struct {
	Expression string
	Err        error
}

func (e *ExpressionError) Error() string {
	return fmt.Sprintf("invalid jmespath expression %q: %v", e.Expression, e.Err)
}

func (e *ExpressionError) Unwrap() error { return e.Err }

// Apply evaluates a JMESPath expression against data.
//
// When expression == "", Apply returns data unchanged with a nil error and
// never invokes the upstream parser — this is a hot path for callers that
// conditionally apply jq filters. Any other expression is delegated to
// the parser binding (production: gj.Search). Parse failures are wrapped in
// *ExpressionError so callers can detect them via errors.As.
func Apply(expression string, data any) (any, error) {
	if expression == "" {
		return data, nil
	}
	got, err := parser(expression, data)
	if err != nil {
		return nil, &ExpressionError{Expression: expression, Err: err}
	}
	return got, nil
}
