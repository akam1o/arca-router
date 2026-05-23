.PHONY: help build build-cli clean rpm rpm-package rpm-test rpm-verify deb deb-package deb-test deb-verify version test fmt vet check release-check release-evidence-check install-nfpm integration-test script-lint netconf-client-lint netconf-client-evidence netconf-ncclient-evidence netconf-libnetconf2-evidence netconf-evidence-verify netconf-standard-xpath-evidence netconf-standard-xpath-evidence-verify netconf-pyez-evidence frr-mgmtd-smoke security-audit package-lint package-binary-arch-check generate-binapi generate-proto all packages

# Binary names
BINARY_NAME=arca-routerd
CLI_BINARY_NAME=arca
BUILD_DIR=build/bin
DIST_DIR=dist
NETCONF_EVIDENCE_DIR ?= artifacts/netconf-clients
PYTHON ?= python3

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
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'
	@echo ""
	@echo "Environment variables:"
	@echo "  VERSION            Override version (default: from git tag)"
	@echo "  SOURCE_DATE_EPOCH  Set for reproducible builds (default: from git)"

version: ## Display version information
	@echo "Version:    $(VERSION)"
	@echo "Commit:     $(GIT_COMMIT)"
	@echo "Build Date: $(BUILD_DATE)"
	@echo "EPOCH:      $(SOURCE_DATE_EPOCH)"

build: ## Build current binaries (unified arca-routerd and arca CLI)
	@echo "Building $(BINARY_NAME) and $(CLI_BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=1 SOURCE_DATE_EPOCH=$(SOURCE_DATE_EPOCH) go build $(BUILD_FLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/arca-routerd
	CGO_ENABLED=0 SOURCE_DATE_EPOCH=$(SOURCE_DATE_EPOCH) go build $(BUILD_FLAGS) -o $(BUILD_DIR)/$(CLI_BINARY_NAME) ./cmd/arca
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME), $(BUILD_DIR)/$(CLI_BINARY_NAME)"

build-cli: ## Build only current arca CLI binary
	@echo "Building $(CLI_BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 SOURCE_DATE_EPOCH=$(SOURCE_DATE_EPOCH) go build $(BUILD_FLAGS) -o $(BUILD_DIR)/$(CLI_BINARY_NAME) ./cmd/arca
	@echo "Build complete: $(BUILD_DIR)/$(CLI_BINARY_NAME)"

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
	gofmt -s -w $$(git ls-files '*.go')

vet: ## Run go vet
	@echo "Running go vet..."
	go vet ./...

check: fmt vet test ## Run all checks (fmt, vet, test)
	@echo "All checks passed"

release-check: package-lint script-lint netconf-client-lint ## Run local v0.10 release readiness checks
	@echo "Running v0.10 release readiness checks..."
	go test ./...
	go vet ./...
	git diff --check
	@echo "v0.10 release readiness checks passed"

release-evidence-check: release-check netconf-evidence-verify netconf-standard-xpath-evidence-verify ## Verify local v0.10 release evidence before sign-off
	@echo "Local v0.10 release evidence checks passed"
	@echo "Attach artifacts from $(NETCONF_EVIDENCE_DIR) to sign-off; v0.10 PR release does not require RC packages, and host/lab-only evidence can be recorded as accepted warnings or deferred gates."

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

package-binary-arch-check: ## Verify package binaries match amd64/x86_64 package metadata
	@for bin in "$(BUILD_DIR)/$(BINARY_NAME)" "$(BUILD_DIR)/$(CLI_BINARY_NAME)"; do \
		if [ ! -f "$$bin" ]; then \
			echo "Error: $$bin not found. Run make build in a Linux amd64 environment before packaging."; \
			exit 1; \
		fi; \
		if ! file "$$bin" | grep -Eq 'ELF 64-bit.*(x86-64|x86_64|AMD x86-64)'; then \
			echo "Error: $$bin must be a Linux amd64 ELF binary for package arch amd64/x86_64."; \
			file "$$bin"; \
			exit 1; \
		fi; \
	done
	@echo "Package binary architecture OK"

rpm-package: package-lint package-binary-arch-check ## Build RPM package (assumes binaries already built)
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

deb-package: package-lint package-binary-arch-check ## Build DEB package (assumes binaries already built)
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

script-lint: ## Syntax-check shell helper scripts
	@echo "Linting shell helper scripts..."
	@for script in \
		build/package/scripts/*.sh \
		scripts/*.sh \
		test/integration_test.sh \
		tests/netconf_clients/*.sh; do \
		bash -n "$$script"; \
	done

netconf-client-lint: ## Syntax-check NETCONF client helper scripts
	@echo "Linting NETCONF client helper scripts..."
	@$(PYTHON) -c 'import pathlib; [compile(path.read_text(encoding="utf-8"), str(path), "exec") for path in pathlib.Path("tests/netconf_clients").glob("*.py")]'

netconf-client-evidence: netconf-ncclient-evidence netconf-libnetconf2-evidence ## Run required NETCONF client interop checks and write sign-off evidence
	@echo "NETCONF client evidence complete: $(NETCONF_EVIDENCE_DIR)"

netconf-ncclient-evidence: ## Run ncclient NETCONF interop and write sign-off evidence
	@echo "Running ncclient NETCONF interop evidence..."
	NETCONF_INTEROP_EVIDENCE_DIR="$(NETCONF_EVIDENCE_DIR)/ncclient" PYTHON="$(PYTHON)" bash tests/netconf_clients/run_interop.sh tests/netconf_clients/ncclient_interop.py

netconf-libnetconf2-evidence: ## Run libnetconf2 NETCONF interop and write sign-off evidence
	@echo "Running libnetconf2 NETCONF interop evidence..."
	NETCONF_INTEROP_EVIDENCE_DIR="$(NETCONF_EVIDENCE_DIR)/libnetconf2" bash tests/netconf_clients/libnetconf2_interop.sh

netconf-evidence-verify: ## Verify required NETCONF client evidence files for sign-off
	@echo "Verifying NETCONF client evidence..."
	$(PYTHON) tests/netconf_clients/verify_evidence.py "$(NETCONF_EVIDENCE_DIR)"

netconf-standard-xpath-evidence: ## Run dedicated standard :xpath interop checks and write evidence
	@echo "Running standard NETCONF :xpath evidence..."
	NETCONF_STANDARD_XPATH=1 NETCONF_INTEROP_EVIDENCE_DIR="$(NETCONF_EVIDENCE_DIR)/standard-xpath/ncclient" PYTHON="$(PYTHON)" bash tests/netconf_clients/run_interop.sh tests/netconf_clients/ncclient_interop.py
	NETCONF_STANDARD_XPATH=1 NETCONF_INTEROP_EVIDENCE_DIR="$(NETCONF_EVIDENCE_DIR)/standard-xpath/libnetconf2" bash tests/netconf_clients/libnetconf2_interop.sh
	@echo "Standard NETCONF :xpath evidence complete: $(NETCONF_EVIDENCE_DIR)/standard-xpath"

netconf-standard-xpath-evidence-verify: ## Verify dedicated standard :xpath evidence files
	@echo "Verifying standard NETCONF :xpath evidence..."
	$(PYTHON) tests/netconf_clients/verify_evidence.py "$(NETCONF_EVIDENCE_DIR)/standard-xpath" --expect-standard-xpath

netconf-pyez-evidence: ## Run supplementary PyEZ NETCONF smoke and write sign-off evidence
	@echo "Running supplementary Junos PyEZ NETCONF evidence..."
	NETCONF_INTEROP_EVIDENCE_DIR="$(NETCONF_EVIDENCE_DIR)/junos-eznc" PYTHON="$(PYTHON)" bash tests/netconf_clients/run_interop.sh tests/netconf_clients/junos_eznc_smoke.py

frr-mgmtd-smoke: ## Run live FRR mgmtd transactional apply smoke test
	@echo "Running live FRR mgmtd smoke test..."
	ARCA_FRR_MGMTD_SMOKE=1 go test -v ./pkg/frr -run TestFRRMgmtdSmokeApplyAndCleanup -count=1

security-audit: ## Audit installed service users, capabilities, and file/socket permissions
	@bash scripts/security-audit.sh

package-lint: ## Validate package metadata and current service expectations
	@echo "Linting package metadata..."
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
	@if find cmd -maxdepth 1 -type d \( -name '*-v2' -o -name '*legacy*' \) | grep -q .; then \
		echo "Error: legacy or -v2 command entrypoints remain under cmd/"; \
		exit 1; \
	fi
	@if [ "$$(find build/systemd -maxdepth 1 -type f | wc -l)" -ne 1 ]; then \
		echo "Error: only the unified arca-routerd systemd service should be packaged"; \
		exit 1; \
	fi
	@grep -q 'mgmtd=yes' build/package/scripts/postinstall.sh
	@grep -q 'vrrpd=yes' build/package/scripts/postinstall.sh
	@echo "Package metadata OK"

generate-binapi: ## Generate VPP binapi (Go bindings for VPP API)
	@echo "Generating VPP binapi..."
	@bash scripts/generate-binapi.sh

all: check build rpm deb ## Run all checks, build binaries, RPM and DEB packages

packages: rpm deb ## Build both RPM and DEB packages

.DEFAULT_GOAL := help
