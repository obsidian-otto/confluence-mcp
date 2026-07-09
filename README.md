# mcp-confluence

A Go re-implementation of `@aashari/mcp-server-atlassian-confluence` v3.3.0:
a single static binary that exposes five MCP tools (`conf_get`,
`conf_post`, `conf_put`, `conf_patch`, `conf_delete`) over stdio so
Hermes Agent can drive a Confluence Cloud instance through the v2
REST API. Built on `ctreminiom/go-atlassian/v2` for HTTP,
`metoro-io/mcp-golang` for MCP framing, with a custom TOON encoder
and a stdlib `.env` parser. Settings resolution (per the Q22 lock):
process env > cwd `.env` > binary-dir `.env`.

Run `make help` to list every available command — the Makefile is the
single source of truth for builds, tests, lint, and the container
image. The full design lives in [`specs/`](specs/) (32 files across
12 topic folders, all complete); the phased delivery plan is in
[`IMPLEMENTATION_PLAN.md`](IMPLEMENTATION_PLAN.md).
