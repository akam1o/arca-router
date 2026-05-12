# Changelog

## v0.5.x – Production Hardening (current)

- **Generated gRPC API**: `api/v1/router.proto` is compiled into typed Go stubs
- **Typed daemon/CLI RPC wiring**: `arca-routerd-v2` and `arca` use generated gRPC clients and servers
- **CLI command rename**: the user-facing CLI command is now `arca`; packages no longer ship `arca-cli`
- **Transactional FRR apply**: `arca-routerd-v2` defaults to `--frr-apply-mode=transactional`, using FRR management commit check/apply
- **FRR file backend retained**: `--frr-apply-mode=file` keeps the legacy full-file reload backend for recovery and compatibility
- **Prometheus metrics endpoint**: optional `--metrics-listen` HTTP endpoint exposes daemon, config, and NETCONF metrics
- **Health endpoint**: optional metrics server also exposes `/healthz`
- **SNMP observability endpoint**: optional `--snmp-listen` read-only SNMPv2c endpoint exposes daemon, config, and NETCONF metrics
- **Grafana dashboard**: dashboard JSON is included for the Prometheus metrics endpoint
- **v2 test coverage**: engine, diff, plugins, gRPC server/client, and daemon tests cover the hardened v2 path
- **No migration tooling**: v0.3.x legacy binaries remain available; automatic migration tooling is intentionally not planned

## v0.4.x – Unified Architecture (previous)

- **Unified daemon** (`arca-routerd-v2`): single process managing VPP, FRR, NETCONF, and CLI API
- **Struct-first configuration model** (`internal/model`): Go structs as the canonical config representation
- **Diff-based apply engine** (`internal/engine`): computes minimal changeset between old and new config
- **Plugin southbound** (`internal/southbound`): VPP and FRR drivers implement a common `Plugin` interface
- **gRPC internal API** (`internal/northbound/grpc`): typed RPC between daemon and CLI
- **Thin CLI client** (`arca`): stateless gRPC client replacing direct SQLite access
- **2-phase commit**: validate-then-apply with automatic rollback on failure
- **Backward compatibility**: legacy daemon, CLI, and NETCONF source entrypoints remain functional

## v0.3.x – NETCONF Management & Security (previous)

- NETCONF/SSH subsystem for remote management (RFC 6241)
- Interactive CLI with commit/rollback
- Advanced policy options (prefix-lists, policy-statements)
- Security enhancements: password/SSH auth, RBAC (admin/operator/read-only), rate limiting, audit logging
- Configuration datastore with candidate/running config and commit history
- CI/CD pipeline for automated build, test, and release

## v0.2.x – Production VPP Integration (previous)

- Real VPP integration via govpp (VPP 24.10)
- FRR integration for dynamic routing protocols (BGP, OSPF, static routes)
- LCP (Linux Control Plane) exposing VPP interfaces to the Linux kernel
- Junos-like configuration syntax (`set` commands)
- arca for operational commands (`show interfaces`, `show route`, etc.)
- Static hardware definition via `hardware.yaml`
- systemd integration with VPP/FRR dependencies
- DEB/RPM distribution for Debian 12 / RHEL 9
