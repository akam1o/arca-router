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

`arca check upgrade` validates the running configuration, rollback archive, schema compatibility, telemetry catalog metadata, QoS capability snapshot, optional backup destination, and packaged install paths when the command is running on an installed package layout. Keep a fresh running configuration backup and verify package release notes for service restart, VPP, FRR, and datastore requirements.

If an upgrade fails after package replacement, reinstall the previous package artifact or restore the previous repository pin, then use `arca show configuration rollback <N>` or `arca backup configuration rollback <N> <path>` output to recover a known-good configuration.

## Compatibility Guarantees

- Documented `set` command syntax and NETCONF configuration XML remain backward-compatible within the v0.x line unless release notes explicitly document a change.
- Documented CLI operational commands remain scriptable, but automation should prefer gRPC, NETCONF, or schema-versioned NMS JSON where available.
- gRPC remains under `arca.router.v1`; telemetry events use `arca.telemetry.v1`.
- NMS JSON schemas remain additive within their v1 schema IDs.
- Removals require release-note documentation and at least one minor release with a deprecation warning or compatibility alias.

## Management Transport Security

- Local `arca` access continues to use the restricted Unix socket by default.
- `arca-routerd --grpc-listen=<host:port> --grpc-tls-cert=<cert> --grpc-tls-key=<key>` enables TCP/TLS gRPC access. Add `--grpc-client-ca=<ca>` to require and verify client certificates. The server TLS private key must be restricted to `0600`. Add `--grpc-client-identity=<identity>[,<identity>...]` to restrict accepted client certificates to exact URI, CN, DNS SAN, or email SAN identities. Add `--grpc-client-role=<identity>=<role>[,<identity>=<role>...]` to enforce method-level RBAC for remote gRPC clients; supported roles are `read-only`, `operator`, and `admin`.
- `arca -grpc-address=<host:port>` uses TLS for remote gRPC access, with optional `-grpc-ca`, `-grpc-server-name`, `-grpc-client-cert`, and `-grpc-client-key`.
- `arca-routerd --web-api-token-file=<path>` enables Web/NMS API automation tokens. The token file must be restricted to `0600`. The file format is one `name:role:token` or `name:role:sha256:<hex>[:not-after=<RFC3339>]` entry per line, where `role` is `read-only`, `operator`, or `admin`. Plain token values must be at least 32 characters, must not contain whitespace, and should be generated from random bytes, for example `openssl rand -base64 32`. Prefer `sha256:<hex>` entries for stored files, with `not-after` for bounded rotation windows; requests still use the original bearer token through `Authorization: Bearer <token>` or `X-API-Key: <token>`.

## Support Matrix

| Component | Supported | Required | Notes |
| --- | --- | --- | --- |
| VPP | 24.10+ | `vpp`, `vpp-plugin-core`, linux-cp plugin | QoS scheduler, policer, and counter enforcement remain capability-gated by detected binapi support. Lab soak/restart evidence is deferred to v0.11. |
| FRR | 8.0+ | `bgpd`, `ospfd`, `ospf6d`, `zebra`, `staticd`, `mgmtd`, `vrrpd`, `bfdd` | Transactional mgmtd is the default apply path; file backend remains a recovery compatibility path. Lab restart recovery evidence is deferred to v0.11. |
| SQLite datastore | schema 1-2 | current schema 2 | Newer schemas are rejected so older binaries do not silently open a future datastore. |
| NETCONF | base:1.0 and base:1.1 | candidate, validate, rollback-on-error, standard `:xpath` | Standard `:xpath` is advertised by default with verified ncclient/libnetconf2 evidence. Startup datastore is intentionally unsupported and unadvertised. |

## Deferred Gates

The following gates are intentionally outside the v0.10 compatibility guarantee
and are tracked in `docs/v0.11-deferred-gates.md`:

- HA failover soak, FRR/VPP restart recovery, and 24-hour churn lab evidence.

Do not advertise or document support for these gates in v0.10 release notes
unless release-candidate lab evidence is attached. NETCONF startup datastore
support is not a deferred v0.11 gate; it remains intentionally unsupported
unless a future design explicitly reopens the capability.

## Audit Export

The authenticated Web API exposes `GET /api/audit` for admin-only audit export. Query parameters:

- `limit`: 1-1000, default 100
- `offset`: default 0
- `user`, `action`, `result`: exact filters
- `since`, `until`: RFC3339 timestamps

Responses use schema `arca.audit.v1` and return newest events first.
