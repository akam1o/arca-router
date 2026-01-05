# Development Guide

This guide covers the development workflow, local build and test procedures, and contribution guidelines for arca-router.

## Table of Contents

- [Development Environment Setup](#development-environment-setup)
- [Local Development Workflow](#local-development-workflow)
- [Building from Source](#building-from-source)
- [Running Tests](#running-tests)
- [Code Style and Linting](#code-style-and-linting)
- [Creating Pull Requests](#creating-pull-requests)
- [CI/CD Pipeline](#cicd-pipeline)
- [Package Development](#package-development)
- [Troubleshooting](#troubleshooting)

---

## Development Environment Setup

### Prerequisites

**Required Software:**
- **Go 1.25.5+**: [Download Go](https://go.dev/dl/)
- **Git**: Version control
- **Make**: Build automation (usually pre-installed on Linux/macOS)
- **Docker** (optional): For testing packages in containers

**Recommended Tools:**
- **golangci-lint**: Static analysis tool
  ```bash
  # Install golangci-lint
  curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin
  ```

- **NFPM**: Package builder (for DEB/RPM)
  ```bash
  make install-nfpm
  ```

### Clone Repository

```bash
git clone https://github.com/akam1o/arca-router.git
cd arca-router
```

### Verify Environment

```bash
# Check Go version
go version  # Should be 1.25.5+

# Check dependencies
go mod download
go mod verify

# Display Makefile targets
make help
```

---

## Local Development Workflow

### 1. Create Feature Branch

```bash
# Update main branch
git checkout main
git pull origin main

# Create feature branch
git checkout -b feature/my-new-feature
```

### 2. Make Changes

Edit code in your favorite editor. Key directories:
- `cmd/` - Main applications (arca-routerd, arca-cli)
- `pkg/` - Reusable packages
- `test/` - Integration tests
- `examples/` - Configuration examples

### 3. Run Local Checks

```bash
# Format code
make fmt

# Run static analysis
make vet

# Run unit tests
make test

# Run all checks
make check
```

### 4. Build Locally

```bash
# Build both binaries
make build

# Verify binaries
./build/bin/arca-routerd --version
./build/bin/arca-cli --version

# Build specific binary
make build-cli
```

### 5. Test Your Changes

```bash
# Run unit tests
go test ./pkg/...

# Run specific test
go test -v ./pkg/vpp/

# Run with coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

---

## Building from Source

### Quick Build

```bash
make build
```

This creates binaries in `build/bin/`:
- `arca-routerd` - Main daemon
- `arca-cli` - CLI tool

### Version Information

Build uses version from git tags:

```bash
# Display current version
make version

# Override version
VERSION=0.3.0 make build
```

**Version sources (priority order):**
1. `VERSION` environment variable
2. Latest git tag (`git describe --tags`)
3. Default: `0.1.0`

### Build Flags

The build process uses:
- **Trimpath**: Removes local path information for reproducibility
- **LDFLAGS**: Embeds version, commit hash, and build date
- **CGO_ENABLED=0**: Static linking (no external C dependencies)

### Reproducible Builds

arca-router supports reproducible builds using `SOURCE_DATE_EPOCH`:

```bash
# Build with fixed timestamp
SOURCE_DATE_EPOCH=1609459200 make build

# Verify reproducibility (DEB)
make clean
make deb-verify

# Verify reproducibility (RPM)
make clean
make rpm-verify
```

---

## Running Tests

### Unit Tests

```bash
# Run all tests
make test

# Run tests with verbose output
go test -v ./...

# Run specific package tests
go test -v ./pkg/device/
go test -v ./pkg/vpp/
go test -v ./pkg/netconf/

# Run single test
go test -v ./pkg/device/ -run TestValidateHardwareConfig
```

### Test Coverage

```bash
# Generate coverage report
go test -coverprofile=coverage.out ./...

# View coverage summary
go tool cover -func=coverage.out

# View HTML coverage report
go tool cover -html=coverage.out
```

**Coverage targets:**
- Overall: 80%+
- Core packages (device, vpp, netconf): 85%+

### Integration Tests

Integration tests require VPP and FRR installed:

```bash
# Run integration tests
make integration-test

# Or directly
bash test/integration_test.sh
```

**Note**: Integration tests may require root privileges to interact with VPP.

---

## Code Style and Linting

### Formatting

arca-router follows standard Go formatting:

```bash
# Format all code
make fmt

# Check formatting without changes
gofmt -l .

# Format specific file
gofmt -w pkg/vpp/govpp_client.go
```

### Static Analysis

```bash
# Run go vet
make vet

# Run golangci-lint (recommended)
golangci-lint run

# Run with specific linters
golangci-lint run --enable-all --disable lll,gocyclo
```

### Configuration

See [.golangci.yml](.golangci.yml) for linter configuration.

**Key rules:**
- Max line length: 120 characters
- Cyclomatic complexity: ≤15
- Cognitive complexity: ≤20
- No `fmt.Println` in production code (use structured logging)

---

## Creating Pull Requests

### Before Creating PR

1. **Run all checks**:
   ```bash
   make check
   ```

2. **Update documentation** if needed:
   - Update README.md for user-facing changes
   - Update SPEC.md for configuration changes
   - Add/update docs/ for major features

3. **Update CHANGELOG.md**:
   ```markdown
   ## [Unreleased]

   ### Added
   - Your feature description

   ### Fixed
   - Your bug fix description
   ```

4. **Commit with clear messages**:
   ```bash
   git add .
   git commit -m "feat: add support for OSPF authentication

   - Implement MD5 authentication for OSPF neighbors
   - Add configuration validation
   - Add unit tests

   Closes #123"
   ```

**Commit message format:**
- **feat**: New feature
- **fix**: Bug fix
- **docs**: Documentation changes
- **refactor**: Code refactoring (no behavior change)
- **test**: Adding or fixing tests
- **chore**: Maintenance (dependencies, build, etc.)

### Create PR

```bash
# Push to GitHub
git push origin feature/my-new-feature

# Create PR via GitHub CLI (optional)
gh pr create --title "Add OSPF authentication" --body "Closes #123"
```

### PR Checklist

- [ ] All tests passing (`make check`)
- [ ] Code coverage maintained or improved
- [ ] Documentation updated
- [ ] CHANGELOG.md updated
- [ ] Commit messages follow convention
- [ ] No merge conflicts with main

---

## CI/CD Pipeline

arca-router uses GitHub Actions for continuous integration and deployment.

### CI Workflow (build.yml)

**Triggers:** Push to main/develop, Pull Requests

**Jobs:**
1. **build** - Build binaries, run tests
   - Go version: 1.25.5
   - Runs: fmt, vet, test
   - Generates coverage report
   - Uploads binaries as artifacts

2. **build-packages** - Build DEB/RPM packages
   - Requires: build job success
   - Uses NFPM to create packages
   - Verifies package metadata
   - Uploads packages as artifacts

3. **lint** - Static analysis
   - Uses golangci-lint
   - Timeout: 5 minutes

**Viewing CI Results:**

1. Go to PR page on GitHub
2. Scroll to "Checks" section at bottom
3. Click "Details" for failed checks
4. Review logs for errors

**Common CI Failures:**

| Error | Cause | Fix |
|-------|-------|-----|
| `gofmt` check failed | Code not formatted | Run `make fmt` |
| `go vet` failed | Static analysis issues | Run `make vet` and fix warnings |
| Test failure | Broken tests | Run `make test` locally |
| Linter errors | Code quality issues | Run `golangci-lint run` |

### Release Workflow (release.yml)

**Trigger:** Tag push (`v*.*.*`)

**Jobs:**
1. **create-release** - Build and create GitHub release
   - Extracts version from tag
   - Builds binaries with version
   - Creates DEB/RPM packages
   - Generates SHA256 checksums
   - Extracts release notes from CHANGELOG.md
   - Creates GitHub Release
   - Uploads artifacts

2. **verify-packages** - Test packages on multiple distros
   - Matrix: Debian 12, Ubuntu 22.04, Rocky Linux 9
   - Installs packages
   - Verifies binaries work
   - Checks systemd unit files

**Release artifacts:**
- `arca-router_<version>-1_amd64.deb`
- `arca-router-<version>-1.x86_64.rpm`
- `SHA256SUMS`

See [Release Process Guide](release-process.md) for details.

---

## Package Development

### Building Packages Locally

**DEB Package (Debian/Ubuntu):**
```bash
# Build DEB package
make deb

# Test package contents
make deb-test

# Verify reproducibility
make deb-verify
```

**RPM Package (RHEL/Rocky/Alma):**
```bash
# Build RPM package
make rpm

# Test package contents
make rpm-test

# Verify reproducibility
make rpm-verify
```

### Package Configuration

Packages are configured via NFPM: [build/package/nfpm.yaml](../build/package/nfpm.yaml)

**Package contents:**
- Binaries: `/usr/sbin/arca-routerd`, `/usr/bin/arca-cli`
- Systemd unit: `/lib/systemd/system/arca-routerd.service`
- Configuration: `/etc/arca-router/*.yaml.example`
- Data directory: `/var/lib/arca-router/`
- Log directory: `/var/log/arca-router/`

**Post-install scripts:**
- Creates `arca-router` user/group
- Sets directory permissions
- Reloads systemd daemon

### Testing Packages in Docker

**Debian 12:**
```bash
docker run --rm -it -v $(pwd)/dist:/packages debian:12 bash
apt-get update
dpkg -i /packages/arca-router_*.deb || apt-get install -f -y
/usr/sbin/arca-routerd --version
```

**Rocky Linux 9:**
```bash
docker run --rm -it -v $(pwd)/dist:/packages rockylinux:9 bash
yum install -y /packages/arca-router-*.rpm
/usr/sbin/arca-routerd --version
```

---

## Troubleshooting

### Build Issues

**Issue: `go.mod` dependency errors**
```bash
# Solution: Update dependencies
go mod tidy
go mod verify
```

**Issue: NFPM not found**
```bash
# Solution: Install NFPM
make install-nfpm
```

**Issue: Build fails with "version script not found"**
```bash
# Solution: Ensure you're in git repository
git status
```

### Test Issues

**Issue: Test fails with "permission denied"**
```
# Solution: Some tests require VPP access
# Run tests as root or skip VPP-dependent tests
go test -short ./...
```

**Issue: Mock interface errors**
```
# Solution: Regenerate mocks if interfaces changed
go generate ./...
```

### CI/CD Issues

**Issue: PR checks not running**
- Ensure GitHub Actions is enabled in repository settings
- Check if workflows exist in `.github/workflows/`

**Issue: Artifact upload fails**
- Check retention policy in workflow
- Verify artifact path exists

---

## Additional Resources

- [Release Process Guide](release-process.md) - Version management and release procedures
- [SPEC.md](../SPEC.md) - Configuration specification
- [VPP Setup Guide](vpp-setup-debian.md) - VPP installation and configuration
- [FRR Setup Guide](frr-setup-debian.md) - FRR installation and configuration
- [Architecture Overview](../README.md#architecture) - System design

---

## Getting Help

- **Issues**: [GitHub Issues](https://github.com/akam1o/arca-router/issues)
- **Discussions**: [GitHub Discussions](https://github.com/akam1o/arca-router/discussions)
- **Documentation**: [docs/](../docs/)

---

**Quick Reference:**

```bash
# Development cycle
git checkout -b feature/my-feature
make fmt vet test           # Local checks
make build                   # Build binaries
git commit -am "feat: ..."
git push origin feature/my-feature
gh pr create                 # Create PR

# Package development
make deb                     # Build DEB
make rpm                     # Build RPM
make deb-test rpm-test       # Test packages

# Release (maintainers only)
git tag v0.3.0
git push origin v0.3.0       # Triggers release workflow
```
