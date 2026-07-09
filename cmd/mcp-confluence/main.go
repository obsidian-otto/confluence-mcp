// cmd/mcp-confluence/main.go
//
// Entrypoint for the mcp-confluence MCP server.
//
// Phase 0 (bootstrap): the binary only prints its version to stderr
// and exits 0. Subsequent phases (per IMPLEMENTATION_PLAN.md) replace
// main() with the full load-config / build-client / serve lifecycle.
//
// All logging goes to stderr — stdout is reserved for the JSON-RPC
// stream that the stdio MCP transport consumes (see
// specs/09-anti-patterns/01-stdout-pollution.md).
package main

import (
	"fmt"
	"os"
)

const version = "v0.1.0"

func main() {
	fmt.Fprintf(os.Stderr, "mcp-confluence %s\n", version)
	os.Exit(0)
}
