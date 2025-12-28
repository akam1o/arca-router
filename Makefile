.PHONY: help build build-cli clean rpm deb version test fmt vet check install-nfpm integration-test generate-binapi

# Binary names
BINARY_NAME=arca-routerd
CLI_BINARY_NAME=arca-cli
NETCONFD_BINARY_NAME=arca-netconfd
BUILD_DIR=build/bin
DIST_DIR=dist

# Version information
VERSION ?= $(shell git describe --tags --abbrev=0 2>/dev/null || echo "0.1.0")
GIT_COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

# Reproducible builds
SOURCE_DATE_EPOCH ?= $(shell git log -1 --format=%ct 2>/dev/null || date +%s)
# Derive BUILD_DATE from SOURCE_DATE_EPOCH for reproducibility
BUILD_DATE=$(shell date -u -d "@$(SOURCE_DATE_EPOCH)" +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || date -u -r $(SOURCE_DATE_EPOCH) +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || echo "unknown")

# Build flags
LDFLAGS=-X main.Version=$(VERSION) -X main.Commit=$(GIT_COMMIT) -X main.BuildDate=$(BUILD_DATE)
BUILD_FLAGS=-ldflags "$(LDFLAGS)" -trimpath

# NFPM settings
NFPM_CONFIG=build/package/nfpm.yaml
NFPM_VERSION=v2.35.0

help: ## Display this help message
	@echo "ARCA Router - Makefile targets:"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'
	@echo ""
	@echo "Environment variables:"
	@echo "  VERSION            Override version (default: from git tag)"
	@echo "  SOURCE_DATE_EPOCH  Set for reproducible builds (default: from git)"

version: ## Display version information
	@echo "Version:    $(VERSION)"
	@echo "Commit:     $(GIT_COMMIT)"
	@echo "Build Date: $(BUILD_DATE)"
	@echo "EPOCH:      $(SOURCE_DATE_EPOCH)"

build: ## Build all binaries (arca-routerd, arca-cli, arca-netconfd)
	@echo "Building $(BINARY_NAME), $(CLI_BINARY_NAME), and $(NETCONFD_BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=1 SOURCE_DATE_EPOCH=$(SOURCE_DATE_EPOCH) go build $(BUILD_FLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/arca-routerd
	CGO_ENABLED=0 SOURCE_DATE_EPOCH=$(SOURCE_DATE_EPOCH) go build $(BUILD_FLAGS) -o $(BUILD_DIR)/$(CLI_BINARY_NAME) ./cmd/arca-cli
	CGO_ENABLED=1 SOURCE_DATE_EPOCH=$(SOURCE_DATE_EPOCH) go build $(BUILD_FLAGS) -o $(BUILD_DIR)/$(NETCONFD_BINARY_NAME) ./cmd/arca-netconfd
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME), $(BUILD_DIR)/$(CLI_BINARY_NAME), $(BUILD_DIR)/$(NETCONFD_BINARY_NAME)"

build-cli: ## Build only arca-cli binary
	@echo "Building $(CLI_BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 SOURCE_DATE_EPOCH=$(SOURCE_DATE_EPOCH) go build $(BUILD_FLAGS) -o $(BUILD_DIR)/$(CLI_BINARY_NAME) ./cmd/arca-cli
	@echo "Build complete: $(BUILD_DIR)/$(CLI_BINARY_NAME)"

test: ## Run tests
	@echo "Running tests..."
	go test -v ./...

fmt: ## Format code
	@echo "Formatting code..."
	go fmt ./...
	gofmt -w .

vet: ## Run go vet
	@echo "Running go vet..."
	go vet ./...

check: fmt vet test ## Run all checks (fmt, vet, test)
	@echo "All checks passed"

clean: ## Clean build artifacts
	@echo "Cleaning..."
	rm -rf $(BUILD_DIR)
	rm -rf $(DIST_DIR)
	rm -f $(BINARY_NAME)
	rm -f $(CLI_BINARY_NAME)

install-nfpm: ## Install NFPM tool
	@echo "Installing NFPM $(NFPM_VERSION)..."
	go install github.com/goreleaser/nfpm/v2/cmd/nfpm@$(NFPM_VERSION)
	@echo "NFPM installed. Verify with: nfpm --version"

rpm: build ## Build RPM package
	@echo "Building RPM package..."
	@mkdir -p $(DIST_DIR)
	@if ! command -v nfpm >/dev/null 2>&1; then \
		echo "Error: nfpm not found. Install with: make install-nfpm"; \
		exit 1; \
	fi
	@# Normalize file mtimes for reproducibility
	@find $(BUILD_DIR) -type f -exec touch -d "@$(SOURCE_DATE_EPOCH)" {} + 2>/dev/null || true
	@find build/systemd -type f -exec touch -d "@$(SOURCE_DATE_EPOCH)" {} + 2>/dev/null || true
	@find build/package -type f -exec touch -d "@$(SOURCE_DATE_EPOCH)" {} + 2>/dev/null || true
	@find examples -type f -exec touch -d "@$(SOURCE_DATE_EPOCH)" {} + 2>/dev/null || true
	VERSION=$(VERSION) SOURCE_DATE_EPOCH=$(SOURCE_DATE_EPOCH) nfpm package -p rpm -f $(NFPM_CONFIG) -t $(DIST_DIR)/
	@echo ""
	@echo "RPM package created:"
	@ls -lh $(DIST_DIR)/*.rpm

deb: build ## Build DEB package (for Debian Bookworm)
	@echo "Building DEB package..."
	@mkdir -p $(DIST_DIR)
	@if ! command -v nfpm >/dev/null 2>&1; then \
		echo "Error: nfpm not found. Install with: make install-nfpm"; \
		exit 1; \
	fi
	@# Normalize file mtimes for reproducibility
	@find $(BUILD_DIR) -type f -exec touch -d "@$(SOURCE_DATE_EPOCH)" {} + 2>/dev/null || true
	@find build/systemd -type f -exec touch -d "@$(SOURCE_DATE_EPOCH)" {} + 2>/dev/null || true
	@find build/package -type f -exec touch -d "@$(SOURCE_DATE_EPOCH)" {} + 2>/dev/null || true
	@find examples -type f -exec touch -d "@$(SOURCE_DATE_EPOCH)" {} + 2>/dev/null || true
	VERSION=$(VERSION) SOURCE_DATE_EPOCH=$(SOURCE_DATE_EPOCH) nfpm package -p deb -f $(NFPM_CONFIG) -t $(DIST_DIR)/
	@echo ""
	@echo "DEB package created:"
	@ls -lh $(DIST_DIR)/*.deb

rpm-test: rpm ## Build and test RPM package metadata
	@echo "Testing RPM package..."
	@rpm -qpi $(DIST_DIR)/arca-router-*.rpm
	@echo ""
	@echo "RPM contents:"
	@rpm -qpl $(DIST_DIR)/arca-router-*.rpm

rpm-verify: ## Verify RPM package reproducibility (requires clean dist/)
	@echo "Verifying reproducible build..."
	@if [ -n "$$(ls -A $(DIST_DIR) 2>/dev/null)" ]; then \
		echo "Error: $(DIST_DIR) is not empty. Run 'make clean' first."; \
		exit 1; \
	fi
	@$(MAKE) rpm
	@mv $(DIST_DIR)/arca-router-$(VERSION)-1.x86_64.rpm $(DIST_DIR)/arca-router-$(VERSION)-1.x86_64.rpm.1
	@$(MAKE) rpm
	@echo "Comparing checksums..."
	@sha256sum $(DIST_DIR)/arca-router-$(VERSION)-1.x86_64.rpm $(DIST_DIR)/arca-router-$(VERSION)-1.x86_64.rpm.1
	@if cmp -s $(DIST_DIR)/arca-router-$(VERSION)-1.x86_64.rpm $(DIST_DIR)/arca-router-$(VERSION)-1.x86_64.rpm.1; then \
		echo "✓ Reproducible build verified"; \
		rm -f $(DIST_DIR)/arca-router-$(VERSION)-1.x86_64.rpm.1; \
	else \
		echo "✗ Build is not reproducible"; \
		exit 1; \
	fi

deb-test: deb ## Build and test DEB package metadata
	@echo "Testing DEB package..."
	@dpkg-deb -I $(DIST_DIR)/arca-router_*.deb
	@echo ""
	@echo "DEB contents:"
	@dpkg-deb -c $(DIST_DIR)/arca-router_*.deb

deb-verify: ## Verify DEB package reproducibility (requires clean dist/)
	@echo "Verifying reproducible build..."
	@if [ -n "$$(ls -A $(DIST_DIR) 2>/dev/null)" ]; then \
		echo "Error: $(DIST_DIR) is not empty. Run 'make clean' first."; \
		exit 1; \
	fi
	@$(MAKE) deb
	@mv $(DIST_DIR)/arca-router_$(VERSION)-1_amd64.deb $(DIST_DIR)/arca-router_$(VERSION)-1_amd64.deb.1
	@$(MAKE) deb
	@echo "Comparing checksums..."
	@sha256sum $(DIST_DIR)/arca-router_$(VERSION)-1_amd64.deb $(DIST_DIR)/arca-router_$(VERSION)-1_amd64.deb.1
	@if cmp -s $(DIST_DIR)/arca-router_$(VERSION)-1_amd64.deb $(DIST_DIR)/arca-router_$(VERSION)-1_amd64.deb.1; then \
		echo "✓ Reproducible build verified"; \
		rm -f $(DIST_DIR)/arca-router_$(VERSION)-1_amd64.deb.1; \
	else \
		echo "✗ Build is not reproducible"; \
		exit 1; \
	fi

integration-test: build ## Run integration tests
	@echo "Running integration tests..."
	@bash test/integration_test.sh

generate-binapi: ## Generate VPP binapi (Go bindings for VPP API)
	@echo "Generating VPP binapi..."
	@bash scripts/generate-binapi.sh

all: check build rpm deb ## Run all checks, build binaries, RPM and DEB packages

packages: rpm deb ## Build both RPM and DEB packages

.DEFAULT_GOAL := help
