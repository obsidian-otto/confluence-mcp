# Dockerfile — fallback for users without `pack` (Paketo) installed.
#
# This produces the SAME artifact as `make image`:
#   - Static binary, CGO_ENABLED=0, ldflags "-s -w -X main.version=v0.1.0"
#   - Distroless base (no glibc, no shell, CA certs only)
#   - OCI labels: org.opencontainers.image.title/description/source/licenses
#
# Build:  docker build -t mcp-confluence:dev .
# Run:    docker run --rm -i -e ATLASSIAN_SITE_NAME=... \
#           -e ATLASSIAN_USER_EMAIL=... -e ATLASSIAN_API_TOKEN=... \
#           mcp-confluence:dev
#
# Use `make image` (pack + Paketo) for the canonical build path —
# it produces a fully reproducible build with SBOM extraction. The
# Dockerfile exists for environments where pack cannot be installed
# (e.g. minimal CI runners, air-gapped setups).

# ---- build stage ----------------------------------------------------------
# Must match the toolchain version declared in go.mod (>= 1.26) so cgo
# semantics and stdlib signatures line up.
FROM golang:1.26.4-alpine AS build

WORKDIR /src

# Cache deps separately — go.mod / go.sum rarely change, source often does.
COPY go.mod go.sum ./
RUN go mod download

COPY cmd/   cmd/
COPY internal/ internal/

ENV CGO_ENABLED=0
ARG VERSION=v0.1.0
RUN go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
        -o /out/mcp-confluence ./cmd/mcp-confluence

# ---- runtime stage --------------------------------------------------------
# Distroless: no shell, no apt, no libc. Just the binary + CA certs.
# Compatible with the same `paketobuildpacks/run-jammy-tiny` semantics.
FROM gcr.io/distroless/static-debian12:nonroot

# OCI labels (mirrors the project.toml [_.metadata] block).
LABEL org.opencontainers.image.title="mcp-confluence"
LABEL org.opencontainers.image.description="Confluence Cloud MCP server in Go (stdio transport) — exposes conf_get / conf_post / conf_put / conf_patch / conf_delete over JSON-RPC."
LABEL org.opencontainers.image.source="https://github.com/bennie/mcp-confluence"
LABEL org.opencontainers.image.licenses="MIT"
LABEL org.opencontainers.image.version="v0.1.0"
LABEL io.bennie.mcp-confluence.tools="conf_get,conf_post,conf_put,conf_patch,conf_delete"

COPY --from=build /out/mcp-confluence /mcp-confluence

# distroless static ships the `nonroot` uid (65532). MCP stdio needs RW
# in /tmp for the truncation dump at /tmp/mcp/<id>.json.
USER 65532:65532
WORKDIR /workspace
ENTRYPOINT ["/mcp-confluence"]
