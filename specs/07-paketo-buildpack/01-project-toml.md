# 07.1 — `project.toml` (Paketo Project Descriptor)

## Overview

`project.toml` is the **Packeto Buildpacks project
descriptor** that ships at the Go module root. `pack build`
reads it automatically; no flags needed for the env-var
overrides. This file documents the canonical `project.toml`
for the Go MCP server.

## Sources

- Paketo project descriptor docs:
  https://buildpacks.io/docs/app-developer-guide/using-project-descriptor/
- Paketo Go buildpack reference:
  https://paketo.io/docs/reference/go-reference
- Paketo Go how-to: https://paketo.io/docs/howto/go/

## Spec

### The canonical `project.toml`

```toml
# project.toml — Paketo Buildpacks project descriptor
# https://buildpacks.io/docs/app-developer-guide/using-project-descriptor/
[_]
schema-version = "0.2"

[[ io.buildpacks.build.env ]]
name = "BP_GO_VERSION"
value = "1.23.4"

[[ io.buildpacks.build.env ]]
name = "BP_GO_TARGETS"
value = "./cmd/mcp-confluence"

[[ io.buildpacks.build.env ]]
name = "BP_GO_BUILD_LDFLAGS"
value = "-s -w -X main.version=1.0.0"

# CGO_ENABLED=0 — produces a static binary (no glibc dependency)
[[ io.buildpacks.build.env ]]
name = "CGO_ENABLED"
value = "0"

# Set BP_LAUNCH_POINT to the produced binary name
[[ io.buildpacks.build.env ]]
name = "BP_LAUNCH_POINT"
value = "./cmd/mcp-confluence"

# Custom labels for SBOM traceability
[[ io.buildpacks.label ]]
key = "org.opencontainers.image.title"
value = "mcp-confluence"

[[ io.buildpacks.label ]]
key = "org.opencontainers.image.description"
value = "Confluence Cloud MCP server in Go (stdio transport)"

[[ io.buildpacks.label ]]
key = "org.opencontainers.image.source"
value = "https://github.com/<owner>/mcp-confluence"

[[ io.buildpacks.label ]]
key = "org.opencontainers.image.licenses"
value = "MIT"
```

### What each setting does

| Setting | Value | Why |
| ------- | ----- | --- |
| `BP_GO_VERSION` | `1.23.4` | Pins Go to a specific version matching `go.mod` (currently requires 1.23 for `ctreminiom/go-atlassian/v2`) |
| `BP_GO_TARGETS` | `./cmd/mcp-confluence` | The package to compile (instead of the default which is the root `.`) |
| `BP_GO_BUILD_LDFLAGS` | `-s -w -X main.version=1.0.0` | Strip debug symbols; set the version constant |
| `CGO_ENABLED` | `0` | **Required** — produces a static binary, no glibc. The `paketobuildpacks/builder-jammy-tiny` run image is `distroless` and has no libc. |
| `BP_LAUNCH_POINT` | `./cmd/mcp-confluence` | The path (relative to `/workspace`) of the produced binary that becomes the image entrypoint |
| `io.buildpacks.label.*` | various | OCI image labels for provenance / SBOM traceability |

### Why CGO_ENABLED=0

The `paketobuildpacks/builder-jammy-tiny` builder's run image
is a distroless base (`paketobuildpacks/run-jammy-tiny`) —
it contains the binary and CA certs but **no glibc**, **no
libpthread**, **no libdl**. A CGO-enabled Go binary links
against glibc and would fail to start with `exec format
error` or missing-library errors at runtime.

Setting `CGO_ENABLED=0` forces pure-Go compilation, producing
a fully static binary. `ldd ./mcp-confluence` on the built
binary should print `not a dynamic executable`.

### Why `-X main.version=1.0.0`

`cmd/mcp-confluence/main.go` declares `const version =
"1.0.0"`. The `-ldflags "-X main.version=..."` overrides
this at link time. The CI pipeline can substitute the git
commit SHA: `-X main.version=$(git rev-parse --short HEAD)`.

### Why `builder-jammy-tiny` (not `-base` or `-full`)

Three Paketo builders work with the Go buildpack:

| Builder | Run image | Use case |
| ------- | --------- | -------- |
| `paketobuildpacks/builder-jammy-full` | Ubuntu Jammy + libs | Java, .NET, Python, etc. — overkill for a single Go binary |
| `paketobuildpacks/builder-jammy-base` | Ubuntu Jammy minimal | General Go apps |
| `paketobuildpacks/builder-jammy-tiny` | **distroless** | **Go apps (recommended)** — smallest, most secure |

The Go buildpack is included in all three (when the
buildpack is explicitly named; otherwise the builder's
default buildpacks apply). For a single Go MCP binary,
`-tiny` is the right choice: tiny image, no shell, no
package manager, CVE surface limited to the binary +
libc-free Go runtime + CA certs.

### What we deliberately do NOT add

| Setting | Why not |
| ------- | ------- |
| `[[ io.buildpacks.build.env ]]` for `GOFLAGS` | Not needed; `-ldflags` and `-tags` go in `BP_GO_BUILD_*` instead |
| `[io.buildpacks]` `pre-package` / `post-package` hooks | No custom packaging needed |
| `[io.buildpacks.group]` buildpack override | Use the default Go buildpack chain (Go Distribution + Go Build) |
| `[io.buildpacks.sbom]` | Default SBOM format (CycloneDX) is fine |

## Verification

A reader of this spec should be able to:

1. Place the above `project.toml` at the Go module root.
2. Run `make image` (which invokes `pack build
   mcp-confluence:test --builder
   paketobuildpacks/builder-jammy-tiny`) and confirm a
   successful build.
3. Run `pack inspect mcp-confluence:test` and confirm:
   - `Stack: io.buildpacks.stacks.jammy`
   - `Run Image: paketobuildpacks/run-jammy-tiny`
   - `Labels: org.opencontainers.image.title = mcp-confluence`
   - `Processes: web = ./cmd/mcp-confluence`
4. Run `docker run --rm mcp-confluence:test` and confirm it
   exits with the FATAL error for missing env vars (proves
   the entrypoint runs in the distroless image).