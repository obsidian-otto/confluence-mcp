# 02 — Automation scripts

> Goal: drive `mcp-confluence` from Bash, Python, Make, or
> CI without an MCP host attached. The binary is just a
> JSON-RPC server; treat it like one.

## Modes

The binary has two ways to be driven programmatically:

| Mode | Wire | When to use |
| ---- | ---- | ----------- |
| `mcp-confluence stdio` | newline-delimited JSON-RPC over `stdin`/`stdout` | one-shot scripts, `make` recipes, GitHub Actions, anywhere the process has a stable pipe |
| `mcp-confluence serve --listen=…` | HTTP `POST /mcp` body = JSON-RPC | long-lived daemon, `curl` smoke tests, multi-tool invocations from one client process, anything where you want request-response semantics instead of stream framing |

Both modes produce **byte-identical JSON-RPC responses**. Pick
by topology, not by wire compatibility.

## Layer 1 — Bash over stdio

The simplest possible Bash pattern. Open the binary, write one
request, read one response, close.

```bash
#!/usr/bin/env bash
# script.sh — list all Confluence spaces via mcp-confluence
set -euo pipefail

# Spawn the binary, write one JSON-RPC request to its stdin,
# close stdin (sends EOF), capture stdout, kill the process.
REQUEST='{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
RESPONSE="$(printf '%s\n' "$REQUEST" | ./bin/mcp-confluence stdio 2>/dev/null)"

# The startup banner ('mcp-confluence v0.1.0 starting…') went to
# stderr and was discarded. The JSON-RPC response is the only
# stdout payload.

echo "$RESPONSE" | jq '.result.tools | length' # 18
```

Three-line Bash. Works without jq if you don't have it (use
`grep -c '"name":'` instead).

## Layer 2 — Bash over `serve`

For repeated calls in one shell session, the TCP/HTTP mode is
faster (no process-spawn overhead per call) and easier to debug
(network tab in your editor, `tail -f` on access logs, etc.).

```bash
#!/usr/bin/env bash
# script.sh — multi-tool Confluence call over HTTP
set -euo pipefail

./bin/mcp-confluence serve --listen=127.0.0.1:8080 &
PID=$!
trap 'kill $PID 2>/dev/null' EXIT

# Wait for the port to open (the binary takes ~50 ms to bind).
for _ in {1..50}; do
  ss -tln | grep -q ':8080' && break
  sleep 0.1
done

# List tools
curl -sf -X POST http://127.0.0.1:8080/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' | jq '.result.tools | length'
# 18

# Call a real tool
curl -sf -X POST http://127.0.0.1:8080/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"conf_get","arguments":{"path":"/wiki/api/v2/spaces?limit=5","outputFormat":"json"}}}'
```

## Layer 3 — JSON-RPC framing (one-line wire format)

Both modes use the same JSON-RPC 2.0 message shape. Every
request is:

```json
{"jsonrpc":"2.0","id":<int>,"method":"<method>","params":<obj>}
```

Every response is:

```json
{"id":<int>,"jsonrpc":"2.0","result":<obj>}
# or, on error:
{"id":<int>,"jsonrpc":"2.0","error":{"code":<int>,"message":"<str>"}}
```

`id` can be any JSON-serialisable scalar (you'll usually see
1, 2, 3, …). It's echoed back so async clients can correlate.
`method` is one of `"initialize"`, `"notifications/initialized"`,
`"tools/list"`, `"tools/call"`, or `"ping"`.

For `tools/call` you MUST provide `params.name` (the tool name)
and `params.arguments` (the tool's args struct, per the schema
that `tools/list` reports). Add `params.arguments.outputFormat` =
`"json"` if you want machine-parsable output instead of the
default TOON encoding.

## Layer 4 — Python

The cleanest Python example uses the `subprocess` module's
`stdin=subprocess.PIPE` plumbing:

```python
#!/usr/bin/env python3
"""client.py — list the 18 tools and call one real tool."""
import json
import subprocess
import sys

BINARY = "./bin/mcp-confluence"

def call(binary_args: list[str], request: dict) -> dict:
    """Send one JSON-RPC request, return the response dict."""
    proc = subprocess.Popen(
        [BINARY, *binary_args],
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.DEVNULL,  # noisy banner — discard
        text=True,
    )
    out, _ = proc.communicate(input=json.dumps(request) + "\n", timeout=15)
    return json.loads(out.strip().splitlines()[-1])

# Init handshake (technically optional for metoro-io/mcp-golang,
# but recommended for portability across MCP hosts).
print(call(["stdio"], {"jsonrpc": "2.0", "id": 1, "method": "initialize",
                       "params": {"protocolVersion": "2024-11-05",
                                 "capabilities": {},
                                 "clientInfo": {"name": "client.py", "version": "0.1"}}}))

# tools/list
tools = call(["stdio"], {"jsonrpc": "2.0", "id": 2, "method": "tools/list"})
print("tool_count:", len(tools["result"]["tools"]))

# tools/call (real Confluence API call)
resp = call(
    ["stdio"],
    {"jsonrpc": "2.0", "id": 3, "method": "tools/call",
     "params": {"name": "conf_get",
                "arguments": {"path": "/wiki/api/v2/spaces?limit=2",
                               "outputFormat": "json"}}}
)
print("space_count:", len(resp["result"]["results"]))
```

The HTTP variant just swaps `Popen` for `requests`:

```python
import json
import requests

URL = "http://127.0.0.1:8080/mcp"

def post(request: dict) -> dict:
    r = requests.post(URL, json=request, timeout=15)
    r.raise_for_status()
    return r.json()

resp = post({"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
print("tool_count:", len(resp["result"]["tools"]))
```

## Layer 5 — Makefile recipes

If you have integration tests driven by `make`, this is the
shortest end-to-end smoke:

```makefile
# In your project's Makefile
MCP_BIN := /path/to/mcp-confluence
MCP_ENV := ATLASSIAN_SITE_NAME=...  ATLASSIAN_USER_EMAIL=...  ATLASSIAN_API_TOKEN=...

.PHONY: tools-list
tools-list:
	$(MCP_ENV) printf '%s\n' \
	  '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' \
	  | $(MCP_BIN) stdio 2>/dev/null \
	  | jq '.result.tools | length'
# Output:
# 18
```

The pattern works because the binary reads newline-delimited
JSON-RPC on stdin and emits one response per line on stdout.

## Layer 6 — CI / GitHub Actions

```yaml
# .github/workflows/smoke.yml
- name: Run mcp-confluence smoke
  run: |
    set -euo pipefail
    make build
    RESP="$(printf '%s\n' \
      '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' \
      | ./bin/mcp-confluence stdio 2>/dev/null)"
    echo "got: $RESP"
    test "$(echo "$RESP" | jq '.result.tools | length')" = "18"
  env:
    ATLASSIAN_SITE_NAME: ${{ secrets.ATLASSIAN_SITE_NAME }}
    ATLASSIAN_USER_EMAIL: ${{ secrets.ATLASSIAN_USER_EMAIL }}
    ATLASSIAN_API_TOKEN:  ${{ secrets.ATLASSIAN_API_TOKEN }}
```

The two failure modes this catches:

1. The binary doesn't build (e.g. broken Go refactor).
2. The binary doesn't load all 18 tools (e.g. a registration bug).

Both are now caught in CI before you discover them in Hermes.

## Common pitfalls

| Pitfall | Symptom | Fix |
| ------- | ------- | --- |
| Reading stderr and stdout together | JSON parse error on the response | redirect stderr: `2>/dev/null` or `2>err.log` |
| Forgetting the `params.arguments` wrapper on `tools/call` | `-32602 Invalid params` from the server | include `params.arguments` as a dict, not just the args |
| Re-spawning per call when many calls in a row | slow | use `serve` mode + `curl` (or the Python `requests.post` form) |
| Using `printf` in C/POSIX locales | `{"id":1,...}` parses fine but TOON bytes get mangled | stick to JSON output: `"outputFormat": "json"` in `params.arguments` |
| Running the binary with a real interactive TTY | EOF-on-stdin never fires; the binary blocks forever | pipe stdin from `< file` or from `<< EOF` |

## Patterns NOT to use

- **Don't spawn a new process per call from a tight loop.** The
  binary's startup cost is ~30-80 ms. Use `serve` mode + HTTP
  if you're making >10 calls.
- **Don't hand-parse TOON.** Use `outputFormat: "json"` to get a
  machine-parseable envelope. The TOON encoder saves tokens
  when a human is reading the output, but it's a token format
  first; it expects a JSON-aware consumer.
- **Don't pass the API token via the HTTP request body.** The
  `serve` mode reads the token from the binary's process env
  at startup; there is no `/auth` endpoint. The HTTP transport
  is intentionally token-less because the binary already holds
  the credential.
