# 07.2 — The `pack build` Command

## Overview

This file documents the **canonical `pack build` invocation**
for the Go MCP server, including the builder, buildpack,
output image name, and SBOM extraction. The command is
reproducible: same input → same image digest.

## Sources

- Paketo Go how-to: https://paketo.io/docs/howto/go/
- `pack` CLI reference:
  https://buildpacks.io/docs/tools/pack/cli/pack_build/
- Paketo Go buildpack:
  https://github.com/paketo-buildpacks/go
- SBOM extraction: `--sbom-output-dir` flag in `pack build`.

## Spec

### The canonical build command

The Makefile wraps this invocation under `make image`:

```bash
pack build mcp-confluence:latest \
  --path . \
  --builder paketobuildpacks/builder-jammy-tiny \
  --buildpack paketo-buildpacks/go \
  --sbom-output-dir ./sbom \
  --pull-policy if-not-present
```

| Flag | Value | Why |
| ---- | ----- | --- |
| `--path` | `.` | Build from current directory (where `project.toml` lives) |
| `--builder` | `paketobuildpacks/builder-jammy-tiny` | Distroless run image (no glibc) |
| `--buildpack` | `paketo-buildpacks/go` | Explicitly name the Go buildpack (the tiny builder ships minimal buildpacks by default) |
| `--sbom-output-dir` | `./sbom` | Extract SBOMs to disk for audit / vulnerability scanning |
| `--pull-policy` | `if-not-present` | Reuse cached builder image; don't re-pull every build |

### What `pack build` does

1. **Detect phase** — runs the Go buildpack's `detect`
   binary. The Go buildpack detects `go.mod` and contributes
   the Go distribution + Go build buildpacks to the plan.
2. **Analyze phase** — reads metadata from any previous
   build of the same image name (for layer caching).
3. **Build phase** — runs the Go buildpack's `build`
   binary, which:
   - Installs the Go toolchain (from `BP_GO_VERSION`).
   - Runs `go build -o /workspace/cmd/mcp-confluence
     ./cmd/mcp-confluence` (with `BP_GO_TARGETS` and
     `BP_GO_BUILD_LDFLAGS` env vars applied).
   - Generates SBOM files (CycloneDX JSON + SPDX) under
     `/layers/paketo-buildpacks_go-mod-vendor/sbom.*`.
4. **Export phase** — assembles the OCI image with the run
   image (`paketobuildpacks/run-jammy-tiny`) as the base,
   adds the built binary at `/workspace/cmd/mcp-confluence`,
   sets the entrypoint to the binary, and copies the SBOMs
   to image metadata.
5. **SBOM extraction** — if `--sbom-output-dir` is set,
   copies the SBOM files from `/layers/.../sbom.*` to the
   output dir.

### Build time expectations

On a 2024-era x86_64 laptop with cached builder image:

- First build (cold): 90-180 seconds (Go toolchain download
  + `go mod download` + `go build`).
- Subsequent builds (warm, same module): 5-15 seconds
  (layer cache hit on Go module download + build cache).

### Builder pinning (reproducible builds)

For production CI, pin the builder SHA:

```bash
BUILDER="paketobuildpacks/builder-jammy-tiny@sha256:<digest>"
pack build mcp-confluence:latest \
  --builder "$BUILDER" \
  ...
```

The current pinned digest is checked into
`infra/pinned-digests.txt` (out of scope for v1; gap Q17).

### Multi-arch builds

Paketo supports `linux/amd64` and `linux/arm64`. For v1 the
default platform (`linux/amd64`) is sufficient. Multi-arch
is gap Q18.

### Local vs CI invocation

Local (developer, via `make image`):

```bash
make image    # wraps: pack build mcp-confluence:dev ...
```

CI (GitHub Actions):

```yaml
- name: Build with pack
  run: |
    pack build ghcr.io/${{ github.repository }}/mcp-confluence:${{ github.sha }} \
      --path . \
      --builder paketobuildpacks/builder-jammy-tiny \
      --buildpack paketo-buildpacks/go \
      --sbom-output-dir ./sbom \
      --pull-policy always
```

The CI invocation differs only in:
- Image tag includes the commit SHA.
- `--pull-policy always` (always pull the latest builder
  for security patches).

## Verification

A reader of this spec should be able to:

1. Run `make image` from a clone of the `confluence-mcp/`
   repo and confirm a successful build (exit 0, no errors).
2. Confirm the produced image is small (~10-15 MB for a
   static Go binary + distroless base).
3. Confirm `docker run --rm mcp-confluence:latest` exits
   with the FATAL error for missing env vars.
4. Confirm `make image-inspect` (or `pack inspect
   mcp-confluence:latest`) shows the builder, run image, and
   labels.