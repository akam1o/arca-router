# Roadmap

This document tracks planned feature work after the v0.5 production hardening
release. Delivered changes are tracked in [CHANGELOG.md](CHANGELOG.md).

## v0.6.x - Advanced Features

Focus: expand the hardened unified daemon into higher-level router features.

- **Multi-chassis / clustering**
  - Management-plane config model for cluster nodes and etcd sync
  - Control-plane HA using FRR and VRRP
  - Config synchronization through etcd
  - Failover reconciliation for local daemon state
- **MPLS / VPN**
  - Management-plane config model for MPLS interfaces and L3VPN service stanzas
  - MPLS label switching through VPP
  - L3VPN integration across FRR and VPP
  - Junos-like config model for VPN services
- **QoS / Traffic Engineering**
  - Management-plane config model for forwarding classes, traffic-control profiles, and interface bindings
  - VPP QoS policy configuration
  - Traffic shaping and policing
  - Operational counters for queues and schedulers
- **Web UI**
  - Read-only browser-based monitoring and JSON status endpoint
  - Browser-based configuration
  - gRPC-backed API integration
  - Authentication and RBAC integration

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

## v0.8.x - Overlay and Streaming Telemetry

Focus: add data-center overlay support and richer external observability.

- **EVPN / VXLAN**
  - L2/L3 VNI configuration model
  - FRR EVPN control-plane integration
  - VPP VXLAN dataplane plumbing
- **Streaming telemetry**
  - gNMI, OpenTelemetry, or structured event stream support
  - Subscription management and backpressure handling
  - Stable event schemas for config, daemon, and routing state changes
- **NMS integration**
  - Stable operational API shape for external systems
  - Integration examples for collectors and dashboards
- **Scale validation**
  - Route scale, session count, and telemetry cardinality testing
  - Performance guardrails for high-churn environments

## v0.9.x - NETCONF/YANG and Operational Safety

Focus: mature management-plane correctness and operator safety.

- **Full YANG validation**
  - Schema-based semantic validation
  - Namespace-aware model traversal
  - XPath and subtree filter maturity
- **NETCONF maturity**
  - Candidate/running semantics hardening
  - Capability advertisement accuracy
  - Interoperability tests with external NETCONF clients
- **Operational safety**
  - Config backup and restore
  - Startup config and rollback archive
  - Upgrade preflight checks
  - Failed commit diagnostics
- **Change impact preview**
  - Route-policy and route-map dry-run
  - Route diff summaries before commit
  - Warnings for disruptive changes

## v0.10.x - Stabilization and Compatibility

Focus: complete final pre-stable stabilization and compatibility work.

- **Security hardening**
  - TLS/mTLS for gRPC
  - Token or API key authentication for automation
  - RBAC audit export
  - Crypto policy alignment where required
- **Upgrade path**
  - Supported upgrades from previous minor releases
  - Datastore schema migration guardrails
  - Package preflight checks
  - Rollback guidance for failed upgrades
- **Compatibility guarantees**
  - CLI and configuration compatibility policy
  - API versioning and deprecation policy
  - Supported VPP and FRR version matrix
- **Long-run soak and failure testing**
  - HA failover soak
  - FRR and VPP restart recovery
  - Datastore lock recovery
  - Resource leak and churn testing
- **Release readiness**
  - Documentation freeze
  - Support matrix
  - Operational runbooks
