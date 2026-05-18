# Documentation Index

This directory contains user guides, design notes, and internal drafts for `arca-router`.

## Architecture (v0.5.x)
- `SPEC.md` / `SPEC.ja.md` - Configuration specification
- `docs/datastore-design.md` - Datastore design (SQLite/etcd)
- `api/v1/router.proto` - gRPC API definitions
- `internal/` - Core v0.5.x packages:
  - `model/` - Canonical config & state types
  - `engine/` - Diff-based config engine with 2-phase commit
  - `southbound/vpp/` / `southbound/frr/` - Plugin implementations
  - `northbound/grpc/` - gRPC server & client
  - `store/` - Persistence abstraction
  - `auth/` - Authentication/RBAC/audit

## Setup (VPP / FRR)
- `docs/vpp-setup-debian.md`
- `docs/vpp-setup-rhel9.md`
- `docs/frr-setup-debian.md`
- `docs/frr-setup-rhel9.md`
- `docs/observability.md`

## Usage / Automation
- `docs/ansible-integration.md`
- `examples/nms/` - HTTP NMS collector examples

## Development / Release
- [`ROADMAP.md`](../ROADMAP.md)
- `docs/compatibility.md`
- `docs/development.md`
- `docs/netconf-xpath-interop.md`
- `docs/netconf-xpath-interop.ja.md`
- `docs/release-process.md`
- `docs/v0.10-release-readiness.md`
- `docs/v0.10-release-readiness.ja.md`
- `docs/v0.10-release-signoff.md`
- `docs/v0.10-release-signoff.ja.md`
- `docs/v0.10-operational-runbook.md`
- `docs/v0.10-operational-runbook.ja.md`
- `docs/v0.11-deferred-gates.md`
- `docs/v0.11-deferred-gates.ja.md`

## Security / Operations
- `docs/security-model.md` (Japanese)
- `docs/rbac-guide.md`
- `docs/key-management.md`

## Design Notes
- `docs/datastore-design.md`
- `docs/config-precedence.md`
- `docs/govpp-compatibility.md`
- `docs/lcp-design.md`
- `docs/frr-vpp-route-sync.md`
- `docs/policy-options-guide.md`

## Drafts (internal)
Internal working notes are kept locally under `tmp/docs-drafts/` (gitignored).
