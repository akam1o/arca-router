.PHONY: help build build-cli build-v2 build-v2-cli clean rpm rpm-package deb deb-package version test fmt vet check install-nfpm integration-test frr-mgmtd-smoke package-lint generate-binapi generate-proto

# Binary names
BINARY_NAME=arca-routerd
CLI_BINARY_NAME=arca
V2_BINARY_NAME=arca-routerd-v2
V2_CLI_BINARY_NAME=arca-v2
BUILD_DIR=build/bin
DIST_DIR=dist

# Version information
VERSION ?= $(shell git describe --tags --abbrev=0 2>/dev/null || echo "0.1.0")
DEB_RELEASE ?= 1
RPM_RELEASE ?= 1.el9
GIT_COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

# Reproducible builds
SOURCE_DATE_EPOCH ?= $(shell git log -1 --format=%ct 2>/dev/null || date +%s)
# Derive BUILD_DATE from SOURCE_DATE_EPOCH for reproducibility
BUILD_DATE=$(shell date -u -d "@$(SOURCE_DATE_EPOCH)" +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || date -u -r $(SOURCE_DATE_EPOCH) +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || echo "unknown")

# Build flags
LDFLAGS=-X main.Version=$(VERSION) -X main.Commit=$(GIT_COMMIT) -X main.BuildDate=$(BUILD_DATE)
BUILD_FLAGS=-ldflags "$(LDFLAGS)" -trimpath -buildvcs=false

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

build: ## Build current v0.5 binaries (unified arca-routerd and arca CLI)
	@echo "Building $(BINARY_NAME) and $(CLI_BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=1 SOURCE_DATE_EPOCH=$(SOURCE_DATE_EPOCH) go build $(BUILD_FLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/arca-routerd-v2
	CGO_ENABLED=0 SOURCE_DATE_EPOCH=$(SOURCE_DATE_EPOCH) go build $(BUILD_FLAGS) -o $(BUILD_DIR)/$(CLI_BINARY_NAME) ./cmd/arca
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME), $(BUILD_DIR)/$(CLI_BINARY_NAME)"

build-cli: ## Build only current arca CLI binary
	@echo "Building $(CLI_BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 SOURCE_DATE_EPOCH=$(SOURCE_DATE_EPOCH) go build $(BUILD_FLAGS) -o $(BUILD_DIR)/$(CLI_BINARY_NAME) ./cmd/arca
	@echo "Build complete: $(BUILD_DIR)/$(CLI_BINARY_NAME)"

build-v2: ## Build v2 unified daemon and CLI with explicit v2 names
	@echo "Building $(V2_BINARY_NAME) and $(V2_CLI_BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=1 SOURCE_DATE_EPOCH=$(SOURCE_DATE_EPOCH) go build $(BUILD_FLAGS) -o $(BUILD_DIR)/$(V2_BINARY_NAME) ./cmd/arca-routerd-v2
	CGO_ENABLED=0 SOURCE_DATE_EPOCH=$(SOURCE_DATE_EPOCH) go build $(BUILD_FLAGS) -o $(BUILD_DIR)/$(V2_CLI_BINARY_NAME) ./cmd/arca
	@echo "Build complete: $(BUILD_DIR)/$(V2_BINARY_NAME), $(BUILD_DIR)/$(V2_CLI_BINARY_NAME)"

build-v2-cli: ## Build only arca-v2 binary
	@echo "Building $(V2_CLI_BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 SOURCE_DATE_EPOCH=$(SOURCE_DATE_EPOCH) go build $(BUILD_FLAGS) -o $(BUILD_DIR)/$(V2_CLI_BINARY_NAME) ./cmd/arca
	@echo "Build complete: $(BUILD_DIR)/$(V2_CLI_BINARY_NAME)"

generate-proto: ## Generate Go gRPC bindings from api/v1/router.proto
	PATH="$$(go env GOPATH)/bin:$$PATH" protoc \
		--go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		api/v1/router.proto

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

rpm: build rpm-package ## Build RPM package

rpm-package: package-lint ## Build RPM package (assumes binaries already built)
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
	VERSION=$(VERSION) RELEASE=$(RPM_RELEASE) SOURCE_DATE_EPOCH=$(SOURCE_DATE_EPOCH) nfpm package -p rpm -f $(NFPM_CONFIG) -t $(DIST_DIR)/
	@echo ""
	@echo "RPM package created:"
	@ls -lh $(DIST_DIR)/*.rpm

deb: build deb-package ## Build DEB package (for Debian Bookworm)

deb-package: package-lint ## Build DEB package (assumes binaries already built)
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
	VERSION=$(VERSION) RELEASE=$(DEB_RELEASE) SOURCE_DATE_EPOCH=$(SOURCE_DATE_EPOCH) nfpm package -p deb -f $(NFPM_CONFIG) -t $(DIST_DIR)/
	@echo ""
	@echo "DEB package created:"
	@ls -lh $(DIST_DIR)/*.deb

rpm-test: rpm ## Build and test RPM package metadata
	@echo "Testing RPM package..."
	@rpm -qpi $(DIST_DIR)/arca-router-*.rpm
	@echo ""
	@echo "RPM contents:"
	@rpm -qpl $(DIST_DIR)/arca-router-*.rpm
	@rpm -qpl $(DIST_DIR)/arca-router-*.rpm | grep -q '^/usr/share/arca-router/grafana/arca-routerd-dashboard.json$$'

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
	@dpkg-deb -c $(DIST_DIR)/arca-router_*.deb | awk '{p=$$NF; sub(/^\.\//, "", p); if (p !~ /^\//) p="/" p; print p}' | grep -q '^/usr/share/arca-router/grafana/arca-routerd-dashboard.json$$'

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

frr-mgmtd-smoke: ## Run live FRR mgmtd transactional apply smoke test
	@echo "Running live FRR mgmtd smoke test..."
	ARCA_FRR_MGMTD_SMOKE=1 go test -v ./pkg/frr -run TestFRRMgmtdSmokeApplyAndCleanup -count=1

package-lint: ## Validate package metadata and v0.5 service expectations
	@echo "Linting package metadata..."
	@for script in build/package/scripts/*.sh; do sh -n "$$script"; done
	@grep -q 'SupplementaryGroups=vpp frrvty' build/systemd/arca-routerd.service
	@if grep -Eq '^[[:space:]]*ReadWritePaths=.*\/etc\/frr' build/systemd/arca-routerd.service; then \
		echo "Error: default service must not grant direct /etc/frr writes"; \
		exit 1; \
	fi
	@grep -q 'observability/grafana/arca-routerd-dashboard.json' $(NFPM_CONFIG)
	@grep -q '/usr/share/arca-router/grafana/arca-routerd-dashboard.json' $(NFPM_CONFIG)
	@grep -q 'dst: /usr/bin/arca' $(NFPM_CONFIG)
	@if grep -q 'dst: /usr/bin/arca-cli' $(NFPM_CONFIG); then \
		echo "Error: package must not ship legacy arca-cli command"; \
		exit 1; \
	fi
	@grep -q 'mgmtd=yes' build/package/scripts/postinstall.sh
	@echo "Package metadata OK"

generate-binapi: ## Generate VPP binapi (Go bindings for VPP API)
	@echo "Generating VPP binapi..."
	@bash scripts/generate-binapi.sh

all: check build rpm deb ## Run all checks, build binaries, RPM and DEB packages

packages: rpm deb ## Build both RPM and DEB packages

.DEFAULT_GOAL := help
