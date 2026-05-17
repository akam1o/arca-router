# Roadmap

This document tracks planned feature work after the v0.5 production hardening
release. Delivered changes are tracked in [CHANGELOG.md](CHANGELOG.md).

## v0.6.x - Advanced Features

Focus: expand the hardened unified daemon into higher-level router features.

- **Multi-chassis / clustering**
  - Management-plane config model for cluster nodes and etcd sync
  - arca-routerd datastore backend selection for etcd-backed config synchronization
  - Runtime config synchronization through etcd running revision polling
  - Commit-time consistency guard between cluster sync config and the active etcd datastore backend
  - Cluster and config sync observability through Web UI status and Prometheus metrics
  - FRR VRRP config generation through the file apply backend
  - FRR VRRP transactional apply through the management candidate datastore
  - VRRP Linux macvlan preparation for FRR vrrpd
  - FRR VRRP operational state polling for control-plane HA
  - VPP LCP cache reconciliation status through Web UI, Prometheus, and SNMP
  - Post-failover FRR/VPP convergence validation through Web UI, Prometheus, and SNMP
- **MPLS / VPN**
  - Management-plane config model for MPLS interfaces and L3VPN service stanzas
  - Commit-time safety gates for unsupported routing-instance southbound apply
  - MPLS interface label forwarding through VPP
  - L3VPN integration across FRR and VPP
  - Junos-like config model for VPN services
- **QoS / Traffic Engineering**
  - Management-plane config model for forwarding classes, traffic-control profiles, and interface bindings
  - VPP class-of-service profile binding for managed interfaces
  - Bound QoS profile visibility in interface operational state
  - VPP RX/TX queue placement telemetry for managed interfaces
  - Class-of-service intent status through CLI, Web UI, Prometheus, SNMP, and Grafana
  - Scheduler and policer enforcement deferred to v0.8 capability detection and fallback until supported VPP binapi coverage is available
- **Observability services**
  - Config-driven NETCONF listen port from `security netconf ssh port`
  - Live managed VPP interface status and counters in NETCONF `<get>`
  - Config-driven Prometheus service enablement for metrics and health checks
  - Config-driven SNMP service enablement for read-only SNMPv2c monitoring
  - VPP LCP reconciliation gauges in Prometheus, Web UI, and SNMP
- **Web UI**
  - Read-only browser-based monitoring and JSON status endpoint
  - Read-only running configuration API and dashboard preview
  - gRPC-backed validate and commit API integration
  - Browser-based configuration editor
  - Commit history API and dashboard panel
  - HTTP Basic authentication and RBAC integration

## v0.7.x - Core Router Parity

Focus: close common router feature gaps before adding more advanced overlays.

- **IPv6 parity**
  - Interface addresses, static routes, BGP IPv6, and OSPFv3
  - IPv6-aware route policy and prefix-list handling
  - CLI, gRPC, and NETCONF coverage for IPv6 configuration
- **VRF / routing instances**
  - Routing-instance data model
  - FRR VRF binding and VPP table mapping
  - Per-instance policy, import, and export controls
- **BFD**
  - BFD sessions for BGP, OSPF, static routes, and HA workflows
  - CLI, gRPC, and NETCONF configuration paths
  - Operational state and failure counters

## v0.8.x - Overlay and Streaming Telemetry (implementation complete)

Focus: deliver data-center overlay support and richer external observability.

- **EVPN / VXLAN**
  - L2/L3 VNI configuration model with CLI, validation, diff, and NETCONF/YANG coverage
  - FRR EVPN control-plane generation through the FRR file backend
  - VPP VXLAN L2/L3 multicast and unicast remote-VTEP dataplane plumbing
- **Streaming telemetry**
  - Structured gRPC telemetry event stream with JSON payload schemas for selected config, daemon, and routing state paths
  - Subscription path filtering, sample intervals, one-shot snapshots, and gRPC flow-control backpressure
  - OpenTelemetry OTLP/HTTP snapshot exporter example
  - Expanded stable event schemas for additional dataplane and protocol state changes
- **QoS dataplane enforcement**
  - VPP scheduler, policer, and counter capability detection
  - Interface metadata binding for output QoS policy intent when scheduler/policer services are unavailable
  - Operational QoS counter visibility through interface telemetry where VPP stats expose them
  - Version-specific fallback and diagnostics for the VPP 24.10 binapi surface
- **NMS integration**
  - Stable operational API shape for external systems
  - Telemetry payload schema registry for collector validation and routing
  - Integration examples for collectors and dashboards
- **Scale validation**
  - Route scale and telemetry cardinality regression coverage
  - NETCONF session count regression coverage
  - NMS telemetry snapshot timeout, payload byte, and event count guardrails

## v0.9.x - NETCONF/YANG and Operational Safety

Focus: mature management-plane correctness and operator safety.

- **Full YANG validation**
  - Schema-based semantic validation
  - Namespace-aware model traversal
  - XPath and subtree filter maturity
  - Externally advertised XPath filtering remains a safe absolute-path subset
    in v0.9: supported expressions are model-rooted paths with simple equality
    predicates, and the standard NETCONF `:xpath` capability is not advertised
  - Introduce an internal XPath engine to move the implementation toward Full
    XPath 1.0 behavior without advertising the standard capability yet
    - Candidate Go packages: `github.com/antchfx/xmlquery` with
      `github.com/antchfx/xpath`
    - Build complete `<get-config>` and `<get>` XML first, then evaluate XPath
      expressions against that XML and require a node-set result
    - Keep the engine experimental in v0.9 until NETCONF response shaping,
      safety limits, and client compatibility are proven
- **NETCONF maturity**
  - Candidate/running semantics hardening
  - Capability advertisement accuracy
  - Interoperability tests with external NETCONF clients: required `ncclient`
    PR CI plus scheduled/manual Junos PyEZ (`junos-eznc`) smoke coverage
- **Operational safety**
  - Config backup and restore
  - Startup config and rollback archive
  - NETCONF startup datastore is intentionally not advertised in v0.9; daemon
    startup continues to load the persisted running snapshot or config file, and
    NETCONF `<startup/>` RPC targets remain `operation-not-supported`
  - Upgrade preflight checks
  - Failed commit diagnostics
  - QoS enforcement preflight, rollback, and post-commit diagnostics
- **Change impact preview**
  - Route-policy and route-map dry-run
  - Route diff summaries before commit
  - Warnings for disruptive changes

## v0.10.x - Stabilization and Compatibility

Focus: complete final pre-stable stabilization and compatibility work.

- **Security hardening**
  - TLS/mTLS for gRPC (implemented for optional TCP listener; Unix socket remains the default local transport)
  - Token or API key authentication for automation (implemented for Web/NMS API Bearer and `X-API-Key` access)
  - RBAC audit export (implemented for admin-only Web API export)
  - Crypto policy alignment where required (implemented for etcd and gRPC TLS policy)
- **Upgrade path**
  - Supported upgrades from previous minor releases
  - Datastore schema migration guardrails
  - Package preflight checks (implemented for packaged install path detection in `arca check upgrade`)
  - Rollback guidance for failed upgrades (implemented in `arca check upgrade` output and compatibility docs)
  - Formal NETCONF startup datastore support, if required, should use a separate
    startup config record with SQLite/etcd migrations, lock/validate/copy-config
    semantics, and explicit compatibility tests instead of aliasing `startup` to
    the latest running config
- **Compatibility guarantees**
  - CLI and configuration compatibility policy
  - API versioning and deprecation policy
  - Supported VPP and FRR version matrix
  - Formal standard NETCONF `:xpath` support should advertise
    `urn:ietf:params:netconf:capability:xpath:1.0` in `<hello>` only after the
    implementation satisfies RFC 6241 response rules, interoperability
    expectations, DoS guardrails, and external client coverage
- **Long-run soak and failure testing**
  - HA failover soak (manual runbook documented; release execution still required)
  - FRR and VPP restart recovery (manual runbook documented; release execution still required)
  - Datastore lock recovery (startup cleanup covered in tests; release runbook documented)
  - Resource leak and churn testing (manual runbook documented; release execution still required)
- **Release readiness**
  - Documentation freeze (checklist documented; final release sign-off still required)
  - Support matrix (published through compatibility policy and release readiness docs)
  - Operational runbooks (v0.10 runbook documented)
