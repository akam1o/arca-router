# Release Process Guide

This document describes the release process for arca-router, including version management, release creation, and post-release procedures.

## Table of Contents

- [Versioning](#versioning)
- [Release Schedule](#release-schedule)
- [Pre-Release Checklist](#pre-release-checklist)
- [Creating a Release](#creating-a-release)
- [Post-Release Tasks](#post-release-tasks)
- [Hotfix Releases](#hotfix-releases)
- [Release Artifacts](#release-artifacts)
- [Troubleshooting](#troubleshooting)

---

## Versioning

arca-router uses **Semantic Versioning 2.0.0** ([semver.org](https://semver.org/)).

### Version Format

```
MAJOR.MINOR.PATCH[-PRERELEASE][+BUILDMETADATA]
```

Examples:
- `0.1.0` - Initial Phase 1 release
- `0.2.0` - Phase 2 (VPP/FRR integration)
- `0.2.1` - Patch release (bug fixes)
- `0.3.0-rc1` - Release candidate
- `1.0.0` - First stable release

### Version Increment Rules

**MAJOR** (X.0.0) - Breaking changes:
- Incompatible configuration format changes
- Removed features or APIs
- Major architecture changes

**MINOR** (0.X.0) - New features (backward compatible):
- New configuration options
- New CLI commands
- Performance improvements
- New optional features

**PATCH** (0.0.X) - Bug fixes only:
- Security patches
- Bug fixes
- Documentation corrections
- No new features

### Pre-Release Versions

**Release Candidates (rc)**:
```
0.3.0-rc1  → Testing
0.3.0-rc2  → Bug fixes
0.3.0      → Stable release
```

**Alpha/Beta** (for major versions):
```
1.0.0-alpha1  → Early development
1.0.0-beta1   → Feature complete, testing
1.0.0-rc1     → Release candidate
1.0.0         → Stable
```

---

## Release Schedule

### Phase-Based Releases

arca-router follows a phase-based development model:

| Phase | Version | Features | Status |
|-------|---------|----------|--------|
| Phase 1 | v0.1.x | Hardware abstraction, basic config | ✅ Complete |
| Phase 2 | v0.2.x | VPP/FRR integration, routing | ✅ Complete |
| Phase 3 | v0.3.x | NETCONF, interactive CLI, policy | 🚧 Current |
| Phase 4 | v0.4.x | Advanced VPP (HA/MPLS/QoS), observability | 🔲 Planned |
| Phase 5 | v1.0.x | Production-ready, Web UI | 🔲 Planned |

### Release Cadence

- **Minor releases**: End of each phase (~2-3 months)
- **Patch releases**: As needed for critical bugs
- **Hotfixes**: Within 24-48 hours for security issues

---

## Pre-Release Checklist

### 1. Code Freeze

**Timeline: 1 week before release**

- [ ] All planned features merged to `main`
- [ ] All PR reviews complete
- [ ] No open critical bugs
- [ ] CI/CD passing on `main`

### 2. Testing

- [ ] **Unit tests**: 80%+ coverage maintained
  ```bash
  make test
  go tool cover -func=coverage.out
  ```

- [ ] **Integration tests**: All passing
  ```bash
  make integration-test
  ```

- [ ] **Manual testing**: Key scenarios verified
  - Fresh installation (DEB/RPM)
  - Upgrade from previous version
  - Configuration migration
  - VPP/FRR integration

- [ ] **Package testing**: Verify on all supported distros
  - Debian 12 (Bookworm)
  - Ubuntu 22.04 LTS
  - RHEL 9 / AlmaLinux 9 / Rocky Linux 9

### 3. Documentation

- [ ] **README.md** updated
  - Version references
  - Feature list
  - Quick start guide

- [ ] **SPEC.md** updated
  - Configuration changes
  - New fields/options
  - Deprecated features marked

- [ ] **CHANGELOG.md** updated
  - All changes since last release
  - Grouped by type (Added, Changed, Fixed, Removed)
  - Migration notes if needed

- [ ] **docs/** updated
  - New feature guides
  - Updated architecture diagrams
  - API documentation

### 4. Version Bump

Update version in relevant files:

- [ ] **Makefile**: Default version (if needed)
- [ ] **build/package/nfpm.yaml**: Package metadata
- [ ] **CHANGELOG.md**: Add release section

**Example CHANGELOG.md update:**
```markdown
## [0.3.0] - 2024-01-15

### Added
- NETCONF/SSH subsystem for remote management
- Interactive CLI with commit/rollback
- Policy-based routing configuration
- Audit logging for all configuration changes

### Changed
- Improved error messages in arca-cli
- Enhanced VPP interface validation

### Fixed
- Fixed LCP interface creation race condition
- Corrected FRR route import handling

### Deprecated
- Legacy configuration format (will be removed in v0.4.0)

## [0.2.1] - 2023-12-20

...
```

### 5. Security Review

- [ ] **Dependency scan**:
  ```bash
  go list -m all | nancy sleuth
  ```

- [ ] **Static analysis**:
  ```bash
  golangci-lint run --enable-all
  ```

- [ ] **Security checklist**:
  - No hardcoded credentials
  - Secrets properly managed (env vars, files)
  - File permissions validated (0600 for keys)
  - Audit logging enabled
  - RBAC properly enforced

---

## Creating a Release

### Step-by-Step Process

#### 1. Final Commit

```bash
# Ensure on main branch
git checkout main
git pull origin main

# Commit final changes
git add CHANGELOG.md README.md
git commit -m "chore: prepare for v0.3.0 release

- Update CHANGELOG.md
- Update version references in README.md"

git push origin main
```

#### 2. Create and Push Tag

```bash
# Create annotated tag
git tag -a v0.3.0 -m "Release v0.3.0

Phase 3 - NETCONF and Interactive CLI

Key features:
- NETCONF/SSH subsystem
- Interactive CLI with commit/rollback
- Policy-based routing
- Audit logging

See CHANGELOG.md for full details."

# Verify tag
git tag -v v0.3.0

# Push tag to GitHub
git push origin v0.3.0
```

**Important**: Use annotated tags (`-a`), not lightweight tags.

#### 3. Monitor Release Workflow

The `release.yml` GitHub Action will automatically:
1. Build binaries
2. Create DEB/RPM packages
3. Generate SHA256 checksums
4. Extract release notes from CHANGELOG.md
5. Create GitHub Release
6. Upload artifacts
7. Verify packages on multiple distros

**Monitor progress:**
- Go to: https://github.com/akam1o/arca-router/actions
- Click on "Release" workflow
- Watch for job completion (typically 5-10 minutes)

#### 4. Verify Release

Once workflow completes:

```bash
# Check GitHub Release page
# https://github.com/akam1o/arca-router/releases/tag/v0.3.0

# Verify artifacts uploaded:
# - arca-router_0.3.0-1_amd64.deb
# - arca-router-0.3.0-1.x86_64.rpm
# - SHA256SUMS
```

**Verification checklist:**
- [ ] Release notes extracted correctly
- [ ] All artifacts present
- [ ] Checksums valid
- [ ] Download links work

#### 5. Test Release Artifacts

Download and test packages:

**Debian:**
```bash
# Download DEB package
wget https://github.com/akam1o/arca-router/releases/download/v0.3.0/arca-router_0.3.0-1_amd64.deb

# Verify checksum
sha256sum arca-router_0.3.0-1_amd64.deb
curl -sL https://github.com/akam1o/arca-router/releases/download/v0.3.0/SHA256SUMS | grep deb

# Test installation (in Docker)
docker run --rm -it debian:12 bash
# ... install and verify
```

**RHEL/Rocky:**
```bash
# Download RPM package
wget https://github.com/akam1o/arca-router/releases/download/v0.3.0/arca-router-0.3.0-1.x86_64.rpm

# Verify checksum
sha256sum arca-router-0.3.0-1.x86_64.rpm
curl -sL https://github.com/akam1o/arca-router/releases/download/v0.3.0/SHA256SUMS | grep rpm

# Test installation
sudo yum install -y ./arca-router-0.3.0-1.x86_64.rpm
```

---

## Post-Release Tasks

### 1. Announcement

- [ ] **GitHub Release**: Ensure release notes are clear and complete
- [ ] **Documentation**: Update main branch docs
- [ ] **Community**: Announce in discussions/mailing list

**Release announcement template:**
```markdown
# arca-router v0.3.0 Released 🎉

We're excited to announce arca-router v0.3.0, completing Phase 3 development!

## Highlights

- **NETCONF/SSH Subsystem**: Remote management via NETCONF protocol
- **Interactive CLI**: Real-time configuration with commit/rollback
- **Policy-Based Routing**: Advanced traffic engineering
- **Audit Logging**: Complete security audit trail

## Installation

**Debian/Ubuntu:**
```bash
wget https://github.com/akam1o/arca-router/releases/download/v0.3.0/arca-router_0.3.0-1_amd64.deb
sudo dpkg -i arca-router_0.3.0-1_amd64.deb
```

**RHEL/Rocky/Alma:**
```bash
wget https://github.com/akam1o/arca-router/releases/download/v0.3.0/arca-router-0.3.0-1.x86_64.rpm
sudo yum install -y ./arca-router-0.3.0-1.x86_64.rpm
```

See [CHANGELOG](https://github.com/akam1o/arca-router/blob/main/CHANGELOG.md) for full details.
```

### 2. Update Development Branch

```bash
# Start work on next version
git checkout main
git pull origin main

# Update CHANGELOG.md for next version
cat >> CHANGELOG.md << 'EOF'
## [Unreleased]

### Added

### Changed

### Fixed

EOF

git add CHANGELOG.md
git commit -m "chore: start v0.3.1 development"
git push origin main
```

### 3. Monitor Issues

After release:
- Monitor GitHub Issues for bug reports
- Triage critical issues for hotfix
- Update documentation FAQ if common issues arise

---

## Hotfix Releases

For critical bugs in production:

### 1. Create Hotfix Branch

```bash
# Create hotfix branch from release tag
git checkout -b hotfix/v0.3.1 v0.3.0

# Fix the issue
# ... make changes ...

# Commit fix
git commit -am "fix: resolve critical VPP crash on startup

Fixes #234"
```

### 2. Test Hotfix

```bash
# Run tests
make check

# Build and test packages
make deb rpm

# Manual testing
```

### 3. Release Hotfix

```bash
# Update CHANGELOG.md
cat > /tmp/changelog-entry << 'EOF'
## [0.3.1] - 2024-01-18

### Fixed
- Critical VPP crash on startup (#234)
- Memory leak in NETCONF session handling (#235)
EOF

# Merge to main
git checkout main
git merge --no-ff hotfix/v0.3.1
git push origin main

# Create tag
git tag -a v0.3.1 -m "Hotfix v0.3.1

Critical fixes:
- VPP crash on startup
- NETCONF memory leak"

git push origin v0.3.1

# Delete hotfix branch
git branch -d hotfix/v0.3.1
```

**Timeline for hotfixes:**
- Security issues: 24-48 hours
- Critical bugs: 2-7 days
- Important bugs: Next patch release

---

## Release Artifacts

Each release includes:

### Binary Artifacts

**DEB Package** (Debian/Ubuntu):
- **Filename**: `arca-router_<version>-1_amd64.deb`
- **Architecture**: amd64 (x86_64)
- **Size**: ~15-20 MB
- **Contents**:
  - `/usr/sbin/arca-routerd`
  - `/usr/bin/arca-cli`
  - `/lib/systemd/system/arca-routerd.service`
  - `/etc/arca-router/*.yaml.example`

**RPM Package** (RHEL/Rocky/Alma):
- **Filename**: `arca-router-<version>-1.x86_64.rpm`
- **Architecture**: x86_64
- **Size**: ~15-20 MB
- **Contents**: Same as DEB

**Checksums**:
- **Filename**: `SHA256SUMS`
- Contains SHA256 hashes for all artifacts

### Source Code

GitHub automatically creates source archives:
- `Source code (zip)`
- `Source code (tar.gz)`

These contain the full repository at the tagged commit.

---

## Troubleshooting

### Release Workflow Failed

**Issue: Build job failed**

Check logs:
1. Go to Actions → Release workflow
2. Click failed job
3. Review error logs

Common causes:
- Test failures → Fix tests and re-tag
- Dependency issues → Update go.mod
- NFPM errors → Check nfpm.yaml syntax

**Solution:**
```bash
# Delete failed tag
git tag -d v0.3.0
git push origin :refs/tags/v0.3.0

# Fix issue, commit, re-tag
git commit -am "fix: resolve build issue"
git tag -a v0.3.0 -m "..."
git push origin main v0.3.0
```

### Package Verification Failed

**Issue: verify-packages job failed**

Check which distro failed:
- Debian 12
- Ubuntu 22.04
- Rocky Linux 9

Common causes:
- Missing dependencies in package
- Incorrect file paths
- Systemd unit issues

**Solution:**
Test locally in Docker before re-releasing.

### Wrong Version in Artifacts

**Issue: Artifacts have wrong version number**

Caused by incorrect tag format.

**Requirements:**
- Tag must start with `v`
- Tag must be annotated (`-a`)
- Tag must match pattern `v*.*.*`

**Solution:**
```bash
# Delete tag
git tag -d v0.3.0
git push origin :refs/tags/v0.3.0

# Create proper tag
git tag -a v0.3.0 -m "Release v0.3.0"
git push origin v0.3.0
```

---

## Release Checklist Summary

**Pre-Release:**
- [ ] All features merged
- [ ] Tests passing (80%+ coverage)
- [ ] Documentation updated
- [ ] CHANGELOG.md complete
- [ ] Security review done
- [ ] Packages tested on all distros

**Release:**
- [ ] Create annotated tag (`git tag -a v0.3.0`)
- [ ] Push tag (`git push origin v0.3.0`)
- [ ] Monitor workflow completion
- [ ] Verify artifacts on GitHub Release
- [ ] Test installation from artifacts

**Post-Release:**
- [ ] Announce release
- [ ] Update CHANGELOG for next version
- [ ] Monitor issues
- [ ] Update documentation site

---

## Additional Resources

- [Development Guide](development.md) - Build and test procedures
- [Semantic Versioning](https://semver.org/) - Version number conventions
- [GitHub Releases Guide](https://docs.github.com/en/repositories/releasing-projects-on-github) - GitHub Release features
- [CHANGELOG Format](https://keepachangelog.com/) - Best practices for changelogs

---

**Quick Reference:**

```bash
# Create release
git tag -a v0.3.0 -m "Release v0.3.0"
git push origin v0.3.0

# Hotfix
git checkout -b hotfix/v0.3.1 v0.3.0
# ... fix ...
git checkout main
git merge --no-ff hotfix/v0.3.1
git tag -a v0.3.1 -m "Hotfix v0.3.1"
git push origin main v0.3.1

# Verify release
curl -sL https://api.github.com/repos/akam1o/arca-router/releases/latest | jq -r '.tag_name'
```
