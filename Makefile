# =============================================================================
# mcp-confluence Makefile — Single Source of Truth
# =============================================================================
# Quick start:
#   make help           # show every command
#   make verify-tools   # confirm go, pack, docker are installed
#   make install        # go mod download
#   make build          # compile to ./bin/mcp-confluence
#   make test           # run all tests
#   make check          # lint + test (pre-commit gate)
#   make image          # build OCI image via pack + Paketo Go BuildPak
#
# Settings are loaded from (in priority order):
#   1. process environment
#   2. .env in the current working directory
#   3. .env next to the binary
# See ../confluence/specs/confluence-go-mcp/01-foundations/03-env-var-contract.md
# for the full contract.
# =============================================================================

# -----------------------------------------------------------------------
# Configurable variables — edit for your project
# -----------------------------------------------------------------------
PROJECT_NAME  := mcp-confluence
BINARY_NAME   := mcp-confluence
MODULE_PATH   := github.com/$(PROJECT_NAME)
CMD_DIR       := cmd/$(BINARY_NAME)
BUILD_DIR     := bin
IMAGE_NAME    := $(PROJECT_NAME)
IMAGE_TAG     ?= latest
BUILDER       := paketobuildpacks/builder-jammy-tiny
BUILD_PACK    := paketo-buildpacks/go
SBOM_DIR      := sbom
REGISTRY      ?= ghcr.io/bennie

GO            := $(shell command -v go 2>/dev/null)
PACK          := $(shell command -v pack 2>/dev/null)
DOCKER        := $(shell command -v docker 2>/dev/null)
GOLANGCI      := $(shell command -v golangci-lint 2>/dev/null)
GOVULNCHECK   := $(shell command -v govulncheck 2>/dev/null)

# -----------------------------------------------------------------------
# Terminal colors
# -----------------------------------------------------------------------
BLUE   := \033[34m
GREEN  := \033[32m
YELLOW := \033[33m
RED    := \033[31m
CYAN   := \033[36m
RESET  := \033[0m

# -----------------------------------------------------------------------
# .PHONY — every target declared below is phony
# -----------------------------------------------------------------------
.PHONY: help install clean build test lint format check type-check security \
        run dev image image-push image-inspect docker-build sbom \
        verify-env verify-tools \
        info locate-bin \
        all

.DEFAULT_GOAL := help

# -----------------------------------------------------------------------
# Help — self-documenting from `## description` comments
# -----------------------------------------------------------------------
help: ## Display this help message
	@printf "\n$(CYAN)=== $(PROJECT_NAME) — Available Commands ===$(RESET)\n"
	@printf "$(YELLOW)%-18s %s$(RESET)\n" "Command" "Description"
	@printf "$(YELLOW)%-18s %s$(RESET)\n" "-------" "-----------"
	@grep -E '^[a-zA-Z_][a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
	  sort | \
	  awk 'BEGIN {FS = ":.*?## "}; {printf "  $(GREEN)%-16s$(RESET) %s\n", $$1, $$2}'
	@printf "\n"

# -----------------------------------------------------------------------
# Tool verification
# -----------------------------------------------------------------------
verify-tools: ## Verify required tools (go, pack, docker) are installed
	@command -v $(GO) >/dev/null 2>&1 || { \
	  printf "$(RED)Error: go not found. Install Go 1.23+ from https://go.dev/dl/$(RESET)\n"; \
	  exit 1; }
	@command -v $(PACK) >/dev/null 2>&1 || { \
	  printf "$(RED)Error: pack not found. Install from https://buildpacks.io/docs/tools/pack/$(RESET)\n"; \
	  exit 1; }
	@command -v $(DOCKER) >/dev/null 2>&1 || { \
	  printf "$(RED)Error: docker not found.$(RESET)\n"; \
	  exit 1; }
	@printf "$(GREEN)✓ go / pack / docker all present$(RESET)\n"

# -----------------------------------------------------------------------
# Settings verification (for .env / env-var debugging — token redacted)
# -----------------------------------------------------------------------
verify-env: ## Verify required env vars are set (token redacted)
	@printf "$(BLUE)Required settings (resolution order: env > .env):$(RESET)\n"
	@printf "  ATLASSIAN_SITE_NAME=$${ATLASSIAN_SITE_NAME:-$(YELLOW)<unset>$(RESET)}\n"
	@printf "  ATLASSIAN_USER_EMAIL=$${ATLASSIAN_USER_EMAIL:-$(YELLOW)<unset>$(RESET)}\n"
	@if [ -n "$$ATLASSIAN_API_TOKEN" ]; then \
	  printf "  ATLASSIAN_API_TOKEN=$(GREEN)<set (length=$${#ATLASSIAN_API_TOKEN})>$(RESET)\n"; \
	else \
	  printf "  ATLASSIAN_API_TOKEN=$(YELLOW)<unset>$(RESET)\n"; \
	fi
	@printf "  DEBUG=$${DEBUG:-$(YELLOW)<unset>$(RESET)}\n"
	@if [ -f .env ]; then \
	  printf "$(GREEN)✓ .env found in cwd$(RESET)\n"; \
	else \
	  printf "$(YELLOW)Note: no .env in cwd. Falling back to binary-dir .env or process env.$(RESET)\n"; \
	fi

# -----------------------------------------------------------------------
# Setup & Installation
# -----------------------------------------------------------------------
install: verify-tools ## Install Go module dependencies
	@printf "$(BLUE)Downloading Go modules...$(RESET)\n"
	$(GO) mod download
	@printf "$(GREEN)✓ Modules downloaded$(RESET)\n"

# -----------------------------------------------------------------------
# Build
# -----------------------------------------------------------------------
build: ## Build the binary to ./bin/$(BINARY_NAME) (statically linked for distroless)
	@printf "$(BLUE)Building $(BINARY_NAME)...$(RESET)\n"
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 $(GO) build -ldflags "-s -w" -o $(BUILD_DIR)/$(BINARY_NAME) ./$(CMD_DIR)
	@printf "$(GREEN)✓ Built $(BUILD_DIR)/$(BINARY_NAME)$(RESET)\n"

# -----------------------------------------------------------------------
# Test
# -----------------------------------------------------------------------
test: ## Run all tests
	@printf "$(BLUE)Running tests...$(RESET)\n"
	$(GO) test ./...

# -----------------------------------------------------------------------
# Code Quality
# -----------------------------------------------------------------------
lint: ## Lint with go vet + gofmt
	@printf "$(BLUE)Linting...$(RESET)\n"
	$(GO) vet ./...
	@bad=$$($(GO)fmt -l .); \
	  if [ -n "$$bad" ]; then \
	    printf "$(RED)Files needing gofmt:$(RESET)\n"; echo "$$bad"; exit 1; \
	  fi
	@if [ -n "$(GOLANGCI)" ]; then \
	  printf "$(BLUE)Running golangci-lint...$(RESET)\n"; \
	  $(GOLANGCI) run; \
	else \
	  printf "$(YELLOW)Note: golangci-lint not installed; skipping. Install:$(RESET)\n"; \
	  printf "$(YELLOW)  go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest$(RESET)\n"; \
	fi
	@printf "$(GREEN)✓ Lint passed$(RESET)\n"

format: ## Format Go source files with gofmt
	@printf "$(BLUE)Formatting...$(RESET)\n"
	$(GO)fmt -w .
	@printf "$(GREEN)✓ Formatted$(RESET)\n"

type-check: ## Run go build with no output (type-check only)
	@printf "$(BLUE)Type-checking...$(RESET)\n"
	$(GO) build -o /dev/null ./...
	@printf "$(GREEN)✓ Type check passed$(RESET)\n"

security: ## Run govulncheck (skipped with warning if not installed)
	@printf "$(BLUE)Checking for known vulnerabilities...$(RESET)\n"
	@if [ -n "$(GOVULNCHECK)" ]; then \
	  $(GOVULNCHECK) ./...; \
	else \
	  printf "$(YELLOW)Note: govulncheck not installed. Install:$(RESET)\n"; \
	  printf "$(YELLOW)  go install golang.org/x/vuln/cmd/govulncheck@latest$(RESET)\n"; \
	fi

check: lint test ## Run all quality checks (lint + test)
	@printf "$(GREEN)✓ All checks passed$(RESET)\n"

# -----------------------------------------------------------------------
# Clean
# -----------------------------------------------------------------------
clean: ## Remove build artifacts and test cache
	@printf "$(YELLOW)Cleaning...$(RESET)\n"
	rm -rf $(BUILD_DIR) $(SBOM_DIR)
	$(GO) clean -testcache
	@printf "$(GREEN)✓ Cleaned$(RESET)\n"

# -----------------------------------------------------------------------
# Development
# -----------------------------------------------------------------------
run: build ## Build and run locally (uses .env if present)
	@printf "$(BLUE)Running $(BINARY_NAME)...$(RESET)\n"
	@if [ ! -f .env ]; then \
	  printf "$(YELLOW)Warning: no .env found. Falling back to process env.$(RESET)\n"; \
	  printf "$(YELLOW)Copy .env.example to .env and fill in your credentials.$(RESET)\n"; \
	fi
	./$(BUILD_DIR)/$(BINARY_NAME)

dev: build ## Build and run with DEBUG=true (verbose stderr logging)
	@printf "$(BLUE)Running $(BINARY_NAME) in dev mode...$(RESET)\n"
	@if [ ! -f .env ]; then \
	  printf "$(YELLOW)Warning: no .env found. Falling back to process env.$(RESET)\n"; \
	fi
	DEBUG=true ./$(BUILD_DIR)/$(BINARY_NAME)

# -----------------------------------------------------------------------
# Container image (pack + Paketo Go BuildPak)
# -----------------------------------------------------------------------
image: verify-tools build ## Build OCI image with $(BUILDER) (pack + Paketo Go BuildPak)
	@printf "$(BLUE)Building OCI image with $(BUILDER)...$(RESET)\n"
	$(PACK) build $(IMAGE_NAME):$(IMAGE_TAG) \
	  --path . \
	  --builder $(BUILDER) \
	  --buildpack $(BUILD_PACK) \
	  --sbom-output-dir $(SBOM_DIR) \
	  --pull-policy if-not-present
	@printf "$(GREEN)✓ Image built: $(IMAGE_NAME):$(IMAGE_TAG)$(RESET)\n"

image-push: image ## Tag and push image to ${REGISTRY}/${IMAGE_NAME}:${IMAGE_TAG}
	@printf "$(BLUE)Pushing image to $(REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)...$(RESET)\n"
	$(DOCKER) tag $(IMAGE_NAME):$(IMAGE_TAG) $(REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)
	$(DOCKER) push $(REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)
	@printf "$(GREEN)✓ Pushed$(RESET)\n"

image-inspect: ## Show the built OCI image metadata
	$(PACK) inspect $(IMAGE_NAME):$(IMAGE_TAG)

docker-build: verify-tools ## Build OCI image with plain docker (fallback when pack unavailable)
	@printf "$(BLUE)Building OCI image with docker (Dockerfile fallback)...$(RESET)\n"
	$(DOCKER) build -t $(IMAGE_NAME):$(IMAGE_TAG) --build-arg VERSION=$(IMAGE_TAG) .
	@printf "$(GREEN)✓ Image built: $(IMAGE_NAME):$(IMAGE_TAG)$(RESET)\n"

sbom: image ## Show the SBOM (CycloneDX JSON) for the built image
	@printf "$(BLUE)SBOM files:$(RESET)\n"
	@find $(SBOM_DIR) -name 'sbom.cdx.json' -exec echo {} \;

# -----------------------------------------------------------------------
# Info
# -----------------------------------------------------------------------
info: ## Show project and tool versions
	@printf "\n$(CYAN)=== $(PROJECT_NAME) ===$(RESET)\n"
	@printf "  Module:    $(MODULE_PATH)\n"
	@printf "  Binary:    $(BUILD_DIR)/$(BINARY_NAME)\n"
	@printf "  Image:     $(IMAGE_NAME):$(IMAGE_TAG)\n"
	@printf "  Builder:   $(BUILDER)\n"
	@printf "  Go:        "; $(GO) version 2>/dev/null || echo "not installed"
	@printf "  Pack:      "; $(PACK) --version 2>/dev/null || echo "not installed"
	@printf "  Docker:    "; $(DOCKER) --version 2>/dev/null || echo "not installed"
	@printf "  golangci:  "; $(GOLANGCI) version 2>/dev/null || echo "not installed (optional)"
	@printf "  govulnchk: "; $(GOVULNCHECK) -version 2>/dev/null || echo "not installed (optional)"

locate-bin: ## Show the built binary path
	@ls -la $(BUILD_DIR)/$(BINARY_NAME) 2>/dev/null || \
	  printf "$(YELLOW)Binary not built yet — run \`make build\`$(RESET)\n"

# -----------------------------------------------------------------------
# Canonical CI sequence
# -----------------------------------------------------------------------
all: build test lint image ## Build, test, lint, image (canonical CI sequence)