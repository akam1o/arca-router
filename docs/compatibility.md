# Compatibility and Upgrade Policy

This document records the v0.10 stabilization policy exposed by `arca show compatibility` and `arca check upgrade`.

## Upgrade Sources

v0.10 supports direct, preflighted upgrades from:

- v0.8.x
- v0.9.x

Deployments on v0.7.x or older should first move through an intermediate validated v0.8/v0.9 upgrade. Before any package upgrade, run:

```bash
arca check upgrade
arca check upgrade backup /var/backups/arca-router/running.conf
```

Keep a fresh running configuration backup and verify package release notes for service restart, VPP, FRR, and datastore requirements.

## Compatibility Guarantees

- Documented `set` command syntax and NETCONF configuration XML remain backward-compatible within the v0.x line unless release notes explicitly document a change.
- Documented CLI operational commands remain scriptable, but automation should prefer gRPC, NETCONF, or schema-versioned NMS JSON where available.
- gRPC remains under `arca.router.v1`; telemetry events use `arca.telemetry.v1`.
- NMS JSON schemas remain additive within their v1 schema IDs.
- Removals require release-note documentation and at least one minor release with a deprecation warning or compatibility alias.

## Support Matrix

| Component | Supported | Required | Notes |
| --- | --- | --- | --- |
| VPP | 24.10+ | `vpp`, `vpp-plugin-core`, linux-cp plugin | QoS scheduler, policer, and counter enforcement remain capability-gated by detected binapi support. |
| FRR | 8.0+ | `bgpd`, `ospfd`, `ospf6d`, `zebra`, `staticd`, `mgmtd`, `vrrpd`, `bfdd` | Transactional mgmtd is the default apply path; file backend remains a recovery compatibility path. |
| SQLite datastore | schema 1-2 | current schema 2 | Newer schemas are rejected so older binaries do not silently open a future datastore. |
| NETCONF | base:1.0 and base:1.1 | candidate, validate, rollback-on-error | Standard `:xpath` and startup datastore capabilities remain unadvertised until full RFC behavior is implemented. |

## Audit Export

The authenticated Web API exposes `GET /api/audit` for admin-only audit export. Query parameters:

- `limit`: 1-1000, default 100
- `offset`: default 0
- `user`, `action`, `result`: exact filters
- `since`, `until`: RFC3339 timestamps

Responses use schema `arca.audit.v1` and return newest events first.
