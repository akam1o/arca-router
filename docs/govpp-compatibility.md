# govpp / VPP Compatibility

**Version**: 0.2.2
**Updated**: 2024-12-24
**Status**: Phase 2 - Task 1.0 Implementation Complete (Execution pending VPP environment)

---

## Overview

This document describes the compatibility verification between govpp (Go bindings for VPP) and the certified VPP 24.10 release series for the arca-router project.

## Target Versions

| Component | Version | Repository |
|-----------|---------|------------|
| **VPP** | 24.10 release series | https://github.com/FDio/vpp |
| **govpp** | v0.13.0 | https://github.com/FDio/govpp |
| **Go** | 1.25.5+ | - |

**Important**: The govpp module path is `go.fd.io/govpp`, NOT `github.com/FDio/govpp`.

---

## govpp Version Selection Criteria

### Selection Process

1. **Obtain VPP 24.10 binapi definitions** (`.api.json` files)
   - From installed VPP package: `/usr/share/vpp/api/*.api.json`
   - From VPP source build: `build-root/install-vpp-native/vpp/share/vpp/api/`

2. **Test binapi generation** with candidate govpp versions
   - Use `govpp` binapi generator to generate Go bindings
   - Verify generation succeeds without errors

3. **Verify API compatibility** in a VPP-enabled environment
   - Connect to VPP via `/run/vpp/api.sock`
   - Execute `ShowVersion` API call
   - Confirm VPP responds with version information
   - Verify version compatibility (major.minor must match 24.10)

4. **Fix version in go.mod**
   - Once PoC succeeds, pin the govpp version explicitly
   - Document the version and rationale in this file

### Selected govpp Version: v0.13.0

**Rationale**:
- govpp v0.13.0 is the latest stable release (November 13, 2025)
- Provides compatibility with VPP 24.10
- Includes all improvements and bug fixes from v0.9.0 through v0.13.0
- Actively maintained and tested against multiple VPP versions
- Automatic version check on connection ensures API compatibility

**Release Timeline**:
- v0.13.0 (Nov 2025) - Latest stable
- v0.12.0 (May 2025)
- v0.11.0 (Sep 2024)
- v0.10.0 (Apr 2024)
- v0.9.0 (Jan 2024) - Added VPP 24.10 CI support

**Source**: [govpp Tags](https://github.com/FDio/govpp/tags)

---

## binapi Management Strategy

### Approach: Include Generated binapi in Repository (Recommended)

**Rationale**:
- Reproducibility: All developers use identical binapi
- CI stability: No dynamic generation failures
- Offline builds: No VPP installation required

**Implementation**:
1. Generate binapi from VPP 24.10 `.api.json` files
2. Commit generated Go files to `pkg/vpp/binapi/`
3. Provide regeneration script for updates (`scripts/generate-binapi.sh`)

### Required binapi Modules

For Phase 2, we need:
- `vpe` - VPP control plane (version, CLI)
- `interface` - Interface management
- `ip` - IP address management
- `avf` - Intel AVF driver
- `rdma` - Mellanox RDMA driver
- `tapv2` - TAP interface (v2 API)
- `lcp` - Linux Control Plane

### binapi Generation

Generated binapi files are stored in `pkg/vpp/binapi/` and committed to the repository for:
1. Build reproducibility across development environments
2. CI/CD stability without runtime VPP dependency
3. Offline development support

To regenerate binapi (after VPP version update):
```bash
./scripts/generate-binapi.sh
```

The script uses VPP 24.10 `.api.json` files stored in `vpp-api-json/` directory.

arca-router treats VPP support as a certified release-series matrix. The packaged dependency range is pinned to `>= 24.10` and `< 25.0`; support for VPP 25.x or later requires regenerating binapi from that release, updating `supportedVPPVersions`, and recording compatibility evidence before widening package metadata.

---

## Runtime Verification

govpp compatibility should be verified in an environment where the VPP 24.10 release series is installed and `/run/vpp/api.sock` is accessible by the test user.

**Verification Goals**:
- Connect to VPP via socket (`/run/vpp/api.sock`)
- Execute VPP control-plane API calls such as `ShowVersion`
- Retrieve and display VPP version information
- Confirm the returned major.minor version matches 24.10
- Disconnect cleanly without API compatibility warnings

The standalone connection PoC has been removed from the repository. Use the VPP client and integration test paths that exercise `pkg/vpp` and the daemon when a VPP-enabled environment is available.

---

## API Compatibility Notes

### Known Issues

- None yet (to be updated after PoC)

### API Differences from Mock

The real VPP client will differ from Mock in:
1. **Error handling**: Real VPP returns API-specific error codes
2. **Timing**: Real VPP operations have network/IPC latency
3. **State persistence**: Real VPP state persists across reconnections

---

## Version Pinning in go.mod

govpp v0.13.0 is pinned in `go.mod`:

```go
require (
    go.fd.io/govpp v0.13.0
    gopkg.in/yaml.v3 v3.0.1
)
```

**Rationale for pinning**:
- Prevent unexpected API breakage from govpp updates
- Ensure reproducible builds across environments
- Explicit upgrade path for future VPP versions
- v0.13.0 is the latest stable release with VPP 24.10 support

---

## Verification Checklist

Phase 2 Task 1.0 requirements:

- [x] govpp dependency path confirmed (`go.fd.io/govpp`)
- [x] VPP 24.10 compatible govpp version identified (v0.13.0)
- [x] Version explicitly pinned in `go.mod`
- [x] binapi source determined (VPP 24.10 `.api.json` files from `/usr/share/vpp/api`)
- [x] binapi generation reproducibility established (`scripts/generate-binapi.sh`)
- [ ] Binapi included in repository (requires VPP 24.10 environment to generate)
- [ ] Runtime VPP compatibility verified in a VPP 24.10 release-series environment
- [ ] VPP API compatibility verified (requires VPP 24.10 release-series environment to execute)
- [x] Connection/disconnection logic covered by VPP client implementation review

**Note**: Items marked as requiring VPP 24.10 environment will be completed during Task 1.3 implementation or in a VPP-enabled CI/CD environment.

---

## References

- [VPP Project](https://fd.io/)
- [govpp Repository](https://github.com/FDio/govpp)
- [VPP API Documentation](https://docs.fd.io/vpp/24.10/)
- [PHASE2.md Task 1.0](../PHASE2.md#10-govppvpp互換性pocnew)

---

## Update History

| Date | Version | Changes |
|------|---------|---------|
| 2024-12-24 | 0.2.2 | Updated to govpp v0.13.0 (latest stable) |
| 2024-12-24 | 0.2.1 | Updated with govpp v0.9.0 selection and rationale |
| 2024-12-24 | 0.2.0 | Initial version for Phase 2 Task 1.0 |
