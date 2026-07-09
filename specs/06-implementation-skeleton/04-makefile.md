# 06.4 — `Makefile` (Single Source of Truth)

> **LOCKED 2026-07-09:** User confirmed that the project must
> use a Makefile as the single source of truth for all
> commands, per the `project` skill rules
> (`~/.hermes/skills/project/project/`). See
> `99-gap-questions/02-partial-answers.md` Q14.

## Overview

The `Makefile` at the Go module root wraps every command a
developer or CI runner needs into a single, discoverable,
self-documenting interface. Per the `project` skill:

- **All commands in one file.** No scattered shell scripts.
- **Self-documenting.** `make help` lists every target with
  its `## description`.
- **Standard names.** `build`, `test`, `lint`, `format`,
  `check`, `clean`, `install`, `run`, `image`, `push`,
  `dev` — the names the skill mandates.
- **Cross-environment.** Same Makefile works on developer
  laptops and in CI containers.
- **Idempotent.** `make build` twice in a row produces the
  same result; `make clean` then `make build` is the
  canonical reset.

This file documents the Makefile's target set, the
`.PHONY` declaration, the configurable variables at the top,
and the expected invocation for each command.

## Sources

- Project skill: `~/.hermes/skills/project/project/SKILL.md`
  (the rules for build automation).
- Project skill rules reference:
  `~/.hermes/skills/project/project/references/rules.md`
  (the "Required Standard Commands" and "Makefile
  Templates" sections).
- Template:
  `~/.hermes/skills/project/project/templates/makefile-ada-alire.mk`
  (the Ada template; adapted to Go, not directly copied).
- Go tooling docs: https://go.dev/doc/cmd (the `go build`,
  `go test`, `go vet`, `gofmt` commands).

## Spec

### Configurable variables (top of file)

```makefile
# =============================================================================
# mcp-confluence Makefile — Single Source of Truth
# =============================================================================
# Quick start:
#   make install   # go mod download + verify pack/docker
#   make build     # compile the binary
#   make test      # run all tests
#   make image     # build the OCI image with pack
#   make run       # run locally (uses .env if present)
# =============================================================================

PROJECT_NAME  := mcp-confluence
BINARY_NAME   := mcp-confluence
MODULE_PATH   := github.com/<owner>/mcp-confluence
CMD_DIR       := cmd/$(BINARY_NAME)
BUILD_DIR     := bin
IMAGE_NAME    := $(PROJECT_NAME)
IMAGE_TAG     := latest
BUILDER       := paketobuildpacks/builder-jammy-tiny
BUILD_PACK    := paketo-buildpacks/go
SBOM_DIR      := sbom
GOLANGCI      := $(shell command -v golangci-lint 2>/dev/null)
GO            := $(shell command -v go)
PACK          := $(shell command -v pack)
DOCKER        := $(shell command -v docker)
```

### `.PHONY` declaration (full list)

```makefile
.PHONY: help install clean build test lint format check type-check security \
        run dev image image-push image-inspect sbom \
        verify-env verify-tools \
        info locate-bin \
        all
```

### Standard targets (per the project skill)

| Target | Purpose | Equivalent command |
| ------ | ------- | ------------------ |
| `help` | List all targets with descriptions | self-documenting |
| `install` | `go mod download` + verify `pack`, `docker`, `go` are on PATH | `go mod download` |
| `build` | Compile the binary to `./bin/mcp-confluence` | `go build -o bin/mcp-confluence ./cmd/mcp-confluence` |
| `test` | Run all unit + integration tests | `go test ./...` |
| `lint` | Run `go vet` + `gofmt -l` (optionally `golangci-lint`) | `go vet ./...` |
| `format` | Run `gofmt -w` on all Go source files | `gofmt -w .` |
| `check` | Run lint + test (the pre-commit gate) | `make lint test` |
| `type-check` | `go build` with no emit (catches type errors without producing a binary) | `go build -o /dev/null ./...` |
| `security` | `govulncheck ./...` (skipped with warning if not installed) | `govulncheck ./...` |
| `clean` | Remove `./bin/`, `./sbom/`, Go test cache | `rm -rf bin sbom && go clean -testcache` |
| `run` | Build + run locally (loads `.env` if present) | `./bin/mcp-confluence` |
| `dev` | Build + run with `DEBUG=true` | `DEBUG=true ./bin/mcp-confluence` |
| `image` | Build the OCI image via `pack` | `pack build mcp-confluence:latest --builder ...` |
| `image-push` | Tag + push to `${REGISTRY}/${IMAGE_NAME}:${IMAGE_TAG}` | `docker push ...` |
| `image-inspect` | Show the built image's metadata | `pack inspect mcp-confluence:latest` |
| `sbom` | Extract the SBOM from the most recent `image` build | extracted from `./sbom/` |
| `verify-env` | Print the three required env vars (with token redacted) | `env \| grep ATLASSIAN_` |
| `verify-tools` | Confirm `go`, `pack`, `docker` are installed and meet version minimums | one-line `command -v` checks |
| `info` | Show Go version, pack version, docker version, project paths | versions + paths |
| `locate-bin` | Show the built binary path | `ls -la bin/` |
| `all` | Alias for `build test lint image` | the canonical CI sequence |

### Target bodies (the most important ones)

```makefile
help: ## Display this help message
	@printf "\n\033[36m=== $(PROJECT_NAME) — Available Commands ===\033[0m\n"
	@printf "\033[33m%-18s %s\033[0m\n" "Command" "Description"
	@printf "\033[33m%-18s %s\033[0m\n" "-------" "-----------"
	@grep -E '^[a-zA-Z_][a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
	  sort | \
	  awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[32m%-16s\033[0m %s\n", $$1, $$2}'
	@printf "\n"

verify-tools: ## Verify required tools (go, pack, docker) are installed
	@command -v $(GO) >/dev/null 2>&1 || { \
	  echo "\033[31mError: go not found. Install Go 1.23+.\033[0m"; exit 1; }
	@command -v $(PACK) >/dev/null 2>&1 || { \
	  echo "\033[31mError: pack not found. Install: https://buildpacks.io/docs/tools/pack/\033[0m"; exit 1; }
	@command -v $(DOCKER) >/dev/null 2>&1 || { \
	  echo "\033[31mError: docker not found.\033[0m"; \
	  exit 1; }
	@echo "\033[32m✓ go / pack / docker all present\033[0m"

install: verify-tools ## Install Go module dependencies
	@echo "\033[34mDownloading Go modules...\033[0m"
	$(GO) mod download
	@echo "\033[32m✓ Modules downloaded\033[0m"

build: ## Build the binary to ./bin/$(BINARY_NAME)
	@echo "\033[34mBuilding $(BINARY_NAME)...\033[0m"
	@mkdir -p $(BUILD_DIR)
	$(GO) build -ldflags "-s -w" -o $(BUILD_DIR)/$(BINARY_NAME) ./$(CMD_DIR)
	@echo "\033[32m✓ Built $(BUILD_DIR)/$(BINARY_NAME)\033[0m"

test: ## Run all tests
	@echo "\033[34mRunning tests...\033[0m"
	$(GO) test ./...

lint: ## Lint with go vet + gofmt
	@echo "\033[34mLinting...\033[0m"
	$(GO) vet ./...
	@bad=$$($(GO)fmt -l .); \
	  if [ -n "$$bad" ]; then \
	    echo "\033[31mFiles needing gofmt:\033[0m"; echo "$$bad"; exit 1; \
	  fi
	@if [ -n "$(GOLANGCI)" ]; then \
	  echo "\033[34mRunning golangci-lint...\033[0m"; \
	  golangci-lint run; \
	else \
	  echo "\033[33mNote: golangci-lint not installed; skipping. Install: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest\033[0m"; \
	fi
	@echo "\033[32m✓ Lint passed\033[0m"

format: ## Format Go source files with gofmt
	@echo "\033[34mFormatting...\033[0m"
	$(GO)fmt -w .
	@echo "\033[32m✓ Formatted\033[0m"

type-check: ## Run go build with no output (type-check only)
	@echo "\033[34mType-checking...\033[0m"
	$(GO) build -o /dev/null ./...
	@echo "\033[32m✓ Type check passed\033[0m"

security: ## Run govulncheck (skipped with warning if not installed)
	@echo "\033[34mChecking for known vulnerabilities...\033[0m"
	@command -v govulncheck >/dev/null 2>&1 && \
	  govulncheck ./... || \
	  echo "\033[33mNote: govulncheck not installed. Install: go install golang.org/x/vuln/cmd/govulncheck@latest\033[0m"

check: lint test ## Run all quality checks (lint + test)
	@echo "\033[32m✓ All checks passed\033[0m"

clean: ## Remove build artifacts and test cache
	@echo "\033[33mCleaning...\033[0m"
	rm -rf $(BUILD_DIR) $(SBOM_DIR)
	$(GO) clean -testcache
	@echo "\033[32m✓ Cleaned\033[0m"

run: build ## Build and run locally (uses .env if present)
	@echo "\033[34mRunning $(BINARY_NAME)...\033[0m"
	@if [ ! -f .env ]; then \
	  echo "\033[33mWarning: no .env found. The binary will exit with FATAL unless env vars are set.\033[0m"; \
	  echo "Copy .env.example to .env and fill in your credentials, or export the env vars.\033[0m"; \
	fi
	./$(BUILD_DIR)/$(BINARY_NAME)

dev: build ## Build and run with DEBUG=true (verbose stderr logging)
	@echo "\033[34mRunning $(BINARY_NAME) in dev mode...\033[0m"
	@if [ ! -f .env ]; then \
	  echo "\033[33mWarning: no .env found.\033[0m"; \
	fi
	DEBUG=true ./$(BUILD_DIR)/$(BINARY_NAME)

image: verify-tools build ## Build OCI image with $(BUILDER)
	@echo "\033[34mBuilding OCI image with $(BUILDER)...\033[0m"
	$(PACK) build $(IMAGE_NAME):$(IMAGE_TAG) \
	  --path . \
	  --builder $(BUILDER) \
	  --buildpack $(BUILD_PACK) \
	  --sbom-output-dir $(SBOM_DIR) \
	  --pull-policy if-not-present
	@echo "\033[32m✓ Image built: $(IMAGE_NAME):$(IMAGE_TAG)\033[0m"

image-push: image ## Tag and push image to ${REGISTRY}/${IMAGE_NAME}:${IMAGE_TAG}
	@echo "\033[34mPushing image to $(REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)...\033[0m"
	$(DOCKER) tag $(IMAGE_NAME):$(IMAGE_TAG) $(REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)
	$(DOCKER) push $(REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)
	@echo "\033[32m✓ Pushed\033[0m"

image-inspect: ## Show the built OCI image metadata
	$(PACK) inspect $(IMAGE_NAME):$(IMAGE_TAG)

sbom: image ## Show the SBOM (CycloneDX JSON) for the built image
	@echo "\033[34mSBOM files:\033[0m"
	@find $(SBOM_DIR) -name 'sbom.cdx.json' -exec echo {} \;

verify-env: ## Verify required env vars are set (token redacted)
	@echo "ATLASSIAN_SITE_NAME=$${ATLASSIAN_SITE_NAME:-<unset>}"
	@echo "ATLASSIAN_USER_EMAIL=$${ATLASSIAN_USER_EMAIL:-<unset>}"
	@if [ -n "$$ATLASSIAN_API_TOKEN" ]; then \
	  echo "ATLASSIAN_API_TOKEN=<set (length=$${#ATLASSIAN_API_TOKEN})>"; \
	else \
	  echo "ATLASSIAN_API_TOKEN=<unset>"; \
	fi

info: ## Show project and tool versions
	@printf "\n\033[36m=== $(PROJECT_NAME) ===\033[0m\n"
	@printf "  Module:    $(MODULE_PATH)\n"
	@printf "  Binary:    $(BUILD_DIR)/$(BINARY_NAME)\n"
	@printf "  Image:     $(IMAGE_NAME):$(IMAGE_TAG)\n"
	@printf "  Go:        "; $(GO) version 2>/dev/null || echo "not installed"
	@printf "  Pack:      "; $(PACK) --version 2>/dev/null || echo "not installed"
	@printf "  Docker:    "; $(DOCKER) --version 2>/dev/null || echo "not installed"

locate-bin: ## Show the built binary path
	@ls -la $(BUILD_DIR)/$(BINARY_NAME) 2>/dev/null || echo "Binary not built yet — run \`make build\`"

all: build test lint image ## Build, test, lint, image (canonical CI sequence)

.DEFAULT_GOAL := help
```

### `.env.example`

A template committed to the repo for new users:

```bash
# .env.example — copy to .env and fill in your real values.
# NEVER commit .env itself (it's in .gitignore).
#
# Generate your API token at:
# https://id.atlassian.com/manage-profile/security/api-tokens

ATLASSIAN_SITE_NAME=your-company
ATLASSIAN_USER_EMAIL=you@example.com
ATLASSIAN_API_TOKEN=ATATT3xFfGF0...

# Optional — enable debug logging (writes to stderr)
DEBUG=false
```

### `.gitignore` (LOCKED 2026-07-09)

```gitignore
# Secrets — never commit
.env
.env.local
.env.*.local

# Build artifacts
/bin/
/dist/

# SBOM extraction
/sbom/

# Go test cache and coverage
*.test
*.out
coverage.txt

# IDE
.idea/
.vscode/

# OS
.DS_Store
Thumbs.db
```

### How `make help` looks

```
=== mcp-confluence — Available Commands ===
Command          Description
-------          -----------
all              Build, test, lint, image (canonical CI sequence)
build            Build the binary to ./bin/mcp-confluence
check            Run all quality checks (lint + test)
clean            Remove build artifacts and test cache
dev              Build and run with DEBUG=true
format           Format Go source files with gofmt
help             Display this help message
image            Build OCI image with Paketo Go BuildPak
image-inspect    Show the built OCI image metadata
image-push       Tag and push image to ${REGISTRY}/${IMAGE_NAME}:${IMAGE_TAG}
info             Show project and tool versions
install          Install Go module dependencies
lint             Lint with go vet + gofmt
locate-bin       Show the built binary path
run              Build and run locally (uses .env if present)
sbom             Show the SBOM (CycloneDX JSON)
security         Run govulncheck (skipped with warning if not installed)
test             Run all tests
type-check       Run go build with no output (type-check only)
verify-env       Verify required env vars are set (token redacted)
verify-tools     Verify required tools (go, pack, docker) are installed
```

### What the project skill mandates vs what we add

| Project-skill standard command | Included? | Notes |
| ------------------------------ | --------- | ----- |
| `help` | ✓ | Self-documenting |
| `install` | ✓ | `go mod download` + `verify-tools` |
| `clean` | ✓ | Removes `bin/`, `sbom/`, test cache |
| `build` | ✓ | `go build -o bin/...` |
| `test` | ✓ | `go test ./...` |
| `lint` | ✓ | `go vet` + `gofmt -l` + optional `golangci-lint` |
| `format` | ✓ | `gofmt -w .` |
| `check` | ✓ | `make lint test` |
| `type-check` | ✓ | `go build -o /dev/null` |
| `coverage` | ❌ (gap Q21) | Deferred; not in v1 |
| `security` | ✓ | Optional `govulncheck` |
| `dev` | ✓ | `DEBUG=true ./bin/...` |
| `run` | ✓ | `./bin/...` |
| `package` (renamed `image`) | ✓ | `pack build ...` |
| `info` | ✓ | Tool versions + paths |

## Verification

A reader of this spec should be able to:

1. Run `make help` from the project root and confirm all 20
   targets listed with descriptions.
2. Run `make verify-tools` and confirm the script reports
   `✓ go / pack / docker all present` (or fails with a
   clear error pointing at the missing tool).
3. Run `make install && make build && make test && make lint
   && make check` in sequence and confirm each succeeds.
4. Run `make image && make image-inspect` and confirm the
   OCI image is built and inspectable.
5. Run `make verify-env` with no env vars set and confirm
   the output shows `<unset>` for all three (token is never
   echoed even when set; only its length is reported).
6. Confirm `.gitignore` excludes `.env`, `bin/`, `sbom/`.
7. Run `make help 2>&1 | head -25` and confirm the
   alphabetical sorted output matches the documented
   target set in the "How `make help` looks" section above.
8. Confirm the Makefile is the **single source of truth**:
   `find . -name '*.sh' -not -path './sbom/*' -not -path
   './bin/*'` returns no project-level shell scripts (every
   command lives in the Makefile).
9. Confirm `04-makefile.md` has exactly four H2 sections in
   Variant-B order: Overview, Sources, Spec, Verification.