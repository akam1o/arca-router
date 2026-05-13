# Changelog

## v0.6.x - Advanced Features (current)

- **Advanced configuration model**: parser, serializer, validation, clone, conversion, and diff support for clustering, MPLS, VRRP, routing instances, class of service, and Web UI service settings
- **Candidate command replacement**: v0.6 scalar settings replace existing candidate lines instead of accumulating duplicates
- **etcd datastore selection**: `arca-routerd` and embedded NETCONF can use the existing etcd-backed candidate/running datastore for clustered deployments
- **Cluster sync guard**: `chassis cluster sync etcd` commits must match the daemon's active etcd datastore backend and endpoints
- **Cluster observability**: `/api/status`, the Web UI, and Prometheus metrics expose datastore and cluster sync state
- **FRR VRRP generation**: `--frr-apply-mode=file` can render `protocols vrrp` groups into FRR integrated interface configuration
- **Standard FRR VRRP daemon**: `vrrpd` is part of the documented required FRR daemon set for appliance-router HA deployments
- **v0.6 southbound safety gates**: unsupported MPLS, routing-instance, class-of-service, and transactional VRRP southbound changes fail validation instead of being silently ignored
- **Read-only Web UI**: optional `--web-listen` HTTP dashboard and `/api/status` JSON endpoint backed by daemon observability state
- **Read-only config API**: Web UI exposes `/api/config` and a running configuration preview in set-command format
- **Web UI authentication**: password-backed `security users` enable HTTP Basic authentication and read-only RBAC for the dashboard APIs
- **Web configuration API**: `/api/config/validate` and `/api/config/commit` use the internal gRPC candidate workflow for authenticated operator/admin configuration changes
- **Browser configuration editor**: the Web UI can edit running config text, validate changes, show diffs, and commit through the Web configuration API
- **Web commit history**: `/api/config/history` and the dashboard expose recent configuration commits from the internal gRPC history API
- **Web UI configuration**: `system services web-ui` can enable the dashboard without a command-line flag

## v0.5.x – Production Hardening (previous)

- **Generated gRPC API**: `api/v1/router.proto` is compiled into typed Go stubs
- **Typed daemon/CLI RPC wiring**: `arca-routerd` and `arca` use generated gRPC clients and servers
- **CLI command rename**: the user-facing CLI command is now `arca`; packages no longer ship `arca-cli`
- **Transactional FRR apply**: `arca-routerd` defaults to `--frr-apply-mode=transactional`, using FRR management commit check/apply
- **FRR file backend retained**: `--frr-apply-mode=file` keeps the legacy full-file reload backend for recovery and compatibility
- **Prometheus metrics endpoint**: optional `--metrics-listen` HTTP endpoint exposes daemon, config, and NETCONF metrics
- **Health endpoint**: optional metrics server also exposes `/healthz`
- **SNMP observability endpoint**: optional `--snmp-listen` read-only SNMPv2c endpoint exposes daemon, config, and NETCONF metrics
- **Grafana dashboard**: dashboard JSON is included for the Prometheus metrics endpoint
- **Unified test coverage**: engine, diff, plugins, gRPC server/client, and daemon tests cover the hardened path
- **Legacy build removal**: old standalone daemon, CLI, and NETCONF command entrypoints have been removed
- **No migration tooling**: automatic migration tooling is intentionally not planned

## v0.4.x – Unified Architecture (previous)

- **Unified daemon** (`arca-routerd`): single process managing VPP, FRR, NETCONF, and CLI API
- **Struct-first configuration model** (`internal/model`): Go structs as the canonical config representation
- **Diff-based apply engine** (`internal/engine`): computes minimal changeset between old and new config
- **Plugin southbound** (`internal/southbound`): VPP and FRR drivers implement a common `Plugin` interface
- **gRPC internal API** (`internal/northbound/grpc`): typed RPC between daemon and CLI
- **Thin CLI client** (`arca`): stateless gRPC client replacing direct SQLite access
- **2-phase commit**: validate-then-apply with automatic rollback on failure

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
