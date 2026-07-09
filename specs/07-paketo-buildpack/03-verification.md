# 07.3 — Verification (Five Commands That Prove It Works)

## Overview

This file documents the **five commands** a developer runs
after `make image` to confirm the image is correct, runnable,
and SBOM-billable. These are the same commands the CI pipeline
runs after each build (per
`08-deployment-hermes/03-sample-invocation.md`).

## Sources

- `docker run` exit-code semantics: docs.docker.com.
- `pack inspect`: buildpacks.io/docs/tools/pack/cli/pack_inspect.
- SBOM format: CycloneDX 1.5
  (https://cyclonedx.org/specification/overview/).

## Spec

### The five commands

```bash
# 1. Build the image (via the Makefile)
make image
# Under the hood: pack build mcp-confluence:latest --path . \
#   --builder paketobuildpacks/builder-jammy-tiny \
#   --buildpack paketo-buildpacks/go --sbom-output-dir ./sbom
# Expected: exit 0, "Successfully built image 'mcp-confluence:latest'"
# Expected time: 90-180s cold, 5-15s warm

# 2. Confirm the image runs (entrypoint present + exec works)
docker run --rm mcp-confluence:latest
# Expected: stderr line "FATAL: ATLASSIAN_SITE_NAME is not set..."
# Expected exit code: 1
# This proves: distroless run image works, static binary starts, env validation fires

# 3. Confirm the image metadata (builder, run image, labels, processes)
pack inspect mcp-confluence:latest
# Expected output includes:
#   Stack: io.buildpacks.stacks.jammy
#   Run Image: paketobuildpacks/run-jammy-tiny
#   Processes:
#     web: ./cmd/mcp-confluence
#   Labels:
#     org.opencontainers.image.title=mcp-confluence
#     org.opencontainers.image.licenses=MIT

# 4. Confirm the binary is statically linked (no glibc dependency)
# (The distroless image has no shell — extract the binary to inspect.)
docker create --name extract mcp-confluence:latest
docker cp extract:/workspace/cmd/mcp-confluence ./mcp-confluence-extracted
docker rm extract
ldd ./mcp-confluence-extracted
# Expected: "not a dynamic executable"
# Or: no output (a fully static binary reports nothing).

# 5. Confirm the SBOM is present and parseable
ls -la sbom/build/paketo-buildpacks_go-build/
# Expected: sbom.cdx.json (CycloneDX) and sbom.spdx.json (SPDX)
jq '.components | length' sbom/build/paketo-buildpacks_go-build/sbom.cdx.json
# Expected: a positive integer (the number of Go modules in the SBOM)
```

### Alternative verification: Hermes integration

The fifth "command" is actually a Hermes-side test:

```bash
# Register the binary with Hermes (per 08-deployment-hermes/01-config-yaml.md)
# Edit ~/.hermes/config.yaml to add the mcp_servers.confluence block
hermes mcp test confluence
# Expected: list of tools including mcp_confluence_conf_get, etc.
# Expected: no errors about connection, no stderr noise

# In a Hermes chat:
# > List my Confluence spaces.
# Expected: tool call to mcp_confluence_conf_get with
#   path=/wiki/api/v2/spaces, jq="results[*].{id:id,key:key,name:name}"
# Expected: tool returns TOON-encoded list of spaces
```

### What each verification catches

| Command | Bug class caught |
| ------- | ---------------- |
| `make image` exit 0 | Build script error, missing `go.mod`, wrong target path, dependency resolution failure |
| `docker run` exit 1 with FATAL | Distroless base missing something (libc, CA certs), binary entrypoint wrong, env validation broken |
| `pack inspect` | Wrong builder used (e.g. `builder-jammy-base` instead of `-tiny`), missing labels, wrong entrypoint |
| `ldd` static check | Accidental CGO dependency, missing `CGO_ENABLED=0` |
| SBOM parse | SBOM extraction failed, wrong buildpack (e.g. no Go buildpack), `--sbom-output-dir` flag missing |
| `hermes mcp test` | MCP framing broken (stdout pollution), tool registration failed, JSON-RPC parsing error |

### When a verification fails

| Symptom | Likely cause | Fix |
| ------- | ------------ | --- |
| `make image` fails with "no buildpacks participating" | `project.toml` missing or `--buildpack paketo-buildpacks/go` flag missing | Add both |
| `docker run` exits immediately with no output | Image entrypoint wrong | Check `BP_LAUNCH_POINT` and `pack inspect` |
| `docker run` errors `exec format error` | Built for wrong arch (e.g. ARM64 on x86) | Add `--platform linux/amd64` to `pack build` |
| `docker run` errors `no such file` | `BP_LAUNCH_POINT` path wrong | Check path is relative to `/workspace` |
| `ldd` shows `libc.so.6 => /lib/...` | CGO enabled | Set `CGO_ENABLED=0` in `project.toml` |
| `hermes mcp test` shows "no tools" | MCP server fails to start | Check stderr captured by Hermes; usually env vars missing or stdout pollution |
| `hermes mcp test` shows tools but tools/call hangs | Handler panics, no safeHandler wrapper | Add `safeHandler` per `04-mcp-golang-framework/02-stdio-transport.md` |

## Verification

A reader of this spec should be able to:

1. Run all five commands in sequence on a fresh clone.
2. Interpret the output of each command (know what success
   looks like).
3. Diagnose the most common failures from the "When a
   verification fails" table.