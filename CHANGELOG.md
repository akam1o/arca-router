# Changelog

## v0.8.x - Overlay and Streaming Telemetry (current)

- **EVPN Grafana panels**: the packaged Grafana dashboard now includes EVPN/VXLAN overlay configured, total VNI, L2 VNI, L3 VNI, and multicast VNI panels backed by the Prometheus overlay metrics
- **EVPN observability summary**: `/metrics`, `/api/status`, and `/api/nms/v1/status` now expose EVPN/VXLAN configured, total VNI, L2 VNI, L3 VNI, and multicast VNI counts for overlay intent monitoring
- **EVPN telemetry path**: structured telemetry and NMS snapshots now expose `/overlays/evpn` with sorted EVPN/VXLAN VNI intent, including L2 bridge-domain metadata, route targets, multicast source settings, and L3 routing-instance bindings
- **NMS telemetry snapshot API**: `/api/nms/v1/telemetry/snapshot` exposes one-shot structured telemetry snapshots over the Web API for HTTP-only collectors, with repeated `path` query parameters, timeout and payload byte budget guardrails, and the same telemetry event payloads used by the gRPC stream
- **VPP QoS capability diagnostics**: VPP southbound initialization now records class-of-service dataplane capability detection, including metadata binding, scheduler, policer, and operational counter support, and exposes the result through metrics and the Web/NMS status API
- **NMS telemetry catalog API**: `/api/nms/v1/telemetry/paths` exposes the supported structured telemetry paths, default path set, event schema version, and payload encoding for collector discovery
- **NMS operational status API**: the Web API now exposes `/api/nms/v1/status`, a schema-versioned JSON envelope for external NMS and collectors that need a stable read-only operational status shape
- **Telemetry CLI snapshots**: `arca show telemetry` now consumes the structured gRPC telemetry stream and prints selected telemetry events as JSON lines for local debugging and collector validation
- **Structured gRPC telemetry stream**: the internal gRPC API now exposes `TelemetryService.SubscribeTelemetry` for selected config, daemon, interface, routing, BFD, LCP, HA, routing-instance, and class-of-service paths with JSON payload events, interval sampling, one-shot snapshots, and gRPC flow-control backpressure
- **VPP VXLAN L2 dataplane plumbing**: VPP southbound validation and apply now support multicast VXLAN for L2 EVPN VNIs by creating bridge domains, creating VXLAN tunnel interfaces, bringing them up, and attaching them to L2 bridge domains with rollback coverage; L3 VNI and unicast remote-VTEP dataplane remain intentionally rejected
- **FRR EVPN control-plane generation**: FRR file-backend generation now renders global BGP `l2vpn evpn` with `advertise-all-vni`, explicit L2 VNI route-targets, L3 VNI VRF bindings, and per-VRF EVPN route-targets while transactional mgmtd support is pending
- **EVPN/VXLAN VNI intent model**: CLI parser/serializer, validation, internal model conversion/clone/diff, NETCONF XML/YANG, and commit-time southbound safety gates cover L2/L3 `protocols evpn vni` configuration with explicit rejection for unsupported VPP dataplane modes

## v0.7.x - Core Router Parity (previous)

- **BFD peer/profile configuration**: parser, serializer, validation, internal model, diff, NETCONF XML/YANG, FRR file backend generation, and transactional `frr-bfdd` operations cover `protocols bfd profile` and `protocols bfd peer`
- **BFD protocol bindings**: BGP neighbors and OSPF/OSPFv3 interfaces can enable BFD directly, including reusable `protocols bfd profile` references
- **BFD static route monitoring**: static routes can be monitored by BFD with optional source address, multihop, and BFD profile settings in the FRR apply backends
- **Transactional static route validation**: direct FRR management operations reject invalid static route prefix, next-hop, distance, address-family, and BFD option combinations before building the candidate
- **Static route generation validation**: FRR generation rejects duplicate static route paths and address-family mismatches before file or transactional apply emits route configuration
- **Static route BFD profile validation**: transactional FRR apply rejects static route BFD profile references that are not part of the generated BFD profile set
- **BFD VRF reference validation**: transactional FRR apply rejects explicit BFD peers that reference VRFs outside the generated VRF set before building the candidate
- **BFD duplicate detection**: FRR config generation rejects duplicate BFD profiles and peers before file or transactional apply emits overlapping daemon configuration
- **BFD backend fallback**: the default transactional FRR backend applies explicit BFD sessions/profiles, static route BFD, profile-less BGP BFD, and profile-less OSPF BFD; arca-routerd falls back to the file backend for BGP/OSPF BFD profile bindings and OSPFv3 until FRR exposes those management YANG paths
- **BFD candidate editing**: candidate `set` replacement handles BFD profile, peer, BGP neighbor, and OSPF/OSPFv3 interface binding paths so updated BFD settings do not leave stale set lines behind
- **Standard FRR BFD daemon**: `bfdd` is documented and checked as part of the required arca-router FRR daemon set for BFD support
- **FRR apply preflight ordering**: transactional apply now builds and validates management operations before preparing VRRP Linux interfaces, and both FRR apply backends reject invalid VRRP groups before host-side VRRP preparation
- **FRR validation preflight**: the FRR southbound plugin now generates FRR artifacts and validates transactional management operations during commit validation, while preserving file-backend fallback for unsupported mgmtd paths
- **Route policy validation**: legacy and canonical config validation reject invalid IPv4/IPv6 prefix-list entries, unknown policy-statement prefix-list references, invalid route-policy protocols, neighbors, AS-path regexes, and community values before apply
- **Policy generation validation**: FRR generation rejects invalid or duplicate prefix-list and route-map objects, unknown route-map prefix-list references, and invalid route-map peer matches before file or transactional apply emits policy configuration
- **AS-path generation validation**: FRR generation rejects invalid or duplicate AS-path access-list objects before file backend emits BGP policy configuration
- **Generated policy reference validation**: FRR config generation rejects BGP/VRF route-map references and route-map AS-path matches that point to missing generated policy objects before file backend emits configuration
- **BGP generation validation**: FRR generation rejects invalid BGP router IDs, duplicate neighbors, and neighbor address-family mismatches before file or transactional apply emits BGP configuration
- **Transactional policy object validation**: direct FRR management operations reject invalid prefix-list and route-map names, sequence numbers, actions, prefixes, and address-family mismatches before building the candidate
- **BGP policy reference validation**: BGP group import/export policies are validated against configured policy-statements before FRR generation
- **Transactional BGP validation**: direct FRR management operations reject invalid BGP ASN, router-id, neighbor IP, remote-as, and address-family mismatches before building the candidate
- **OSPF generation validation**: FRR generation rejects duplicate OSPF networks/interfaces, OSPFv2 IPv6 networks, and unsupported OSPFv3 network statements before file or transactional apply emits OSPF configuration
- **Transactional OSPF validation**: direct FRR management operations reject invalid OSPF router-id, network, area-id, interface, metric, and priority state before building the candidate
- **Route policy prefix-list aggregation**: FRR generation aggregates same-family prefix-list matches per route-map entry so IPv4 and IPv6 policy matches render deterministically through both file and transactional backends
- **Route-map backend selection**: route-map source-protocol, peer, and AS-path matches fall back to the FRR file backend instead of being silently dropped by transactional apply
- **Transactional route-map reference validation**: direct FRR management operations reject BGP and VRF route-map references that are not included in the generated route-map set
- **BFD peer backend selection**: interface-less single-hop and source-less multihop BFD peers fall back to the FRR file backend because FRR mgmtd requires those fields as transactional session keys
- **Transactional VRF VPN validation**: direct FRR management operations reject invalid VRF VPN state such as missing ASN, missing route distinguisher, invalid route-targets, and route-map import/export without matching targets

## v0.6.x - Advanced Features (previous)

- **Advanced configuration model**: parser, serializer, validation, clone, conversion, and diff support for clustering, MPLS, VRRP, routing instances, class of service, and Web UI service settings
- **Candidate command replacement**: v0.6 scalar settings replace existing candidate lines instead of accumulating duplicates
- **Set-command idempotence**: repeated list-style set commands for interfaces, MPLS, routing-instances, cluster endpoints, and prefix-lists are deduplicated during parsing
- **Interface reference validation**: MPLS, VRRP, routing-instance, OSPF, and class-of-service interface references must point to configured interfaces before commit
- **L3VPN safety validation**: routing-instance VPN import/export settings fail commit validation before southbound apply when required route-targets, route distinguishers, or local AS settings are missing
- **Routing-instance policy hooks**: `vrf-import` and `vrf-export` reference configured policy statements in the v0.6 L3VPN service model
- **Directional VRF targets**: routing instances support shared `vrf-target` and directional `vrf-target import` / `vrf-target export` extended-community targets
- **NETCONF v0.6 XML model**: NETCONF get-config/edit-config and the embedded YANG model cover v0.6 system services, clustering, MPLS/VRRP, routing-instances, class-of-service, and non-sensitive security settings
- **NETCONF YANG capability**: server hello advertises the arca-router YANG module capability once the embedded model matches the implemented v0.6 XML schema
- **etcd datastore selection**: `arca-routerd` and embedded NETCONF can use the existing etcd-backed candidate/running datastore for clustered deployments
- **Cluster sync guard**: `chassis cluster sync etcd` commits must match the daemon's active etcd datastore backend and endpoints
- **Cluster observability**: `/api/status`, the Web UI, and Prometheus metrics expose datastore and cluster sync state
- **VPP LCP reconciliation observability**: VPP LCP cache reconciliation state is exposed through `/api/status`, the Web UI, Prometheus metrics, and SNMP OIDs
- **Class-of-service observability**: `/api/status`, the Web UI, Prometheus metrics, and SNMP OIDs expose CoS configured state, forwarding-class/profile counts, interface binding count, and intent-only enforcement state
- **Grafana class-of-service panels**: the packaged Grafana dashboard includes CoS configured, enforcement, forwarding-class, traffic-control profile, and interface binding panels
- **NETCONF listen configuration**: `security netconf ssh port` provides the default embedded NETCONF listen port when `--netconf-listen` is omitted
- **NETCONF interface operational state**: NETCONF `<get>` exposes live managed VPP interface status, MAC addresses, and counters when arca-routerd can collect them
- **gRPC managed interface state**: the internal gRPC API and `arca show interfaces` use the daemon's managed VPP state collector, including configured interface names such as `ge-0/0/0`
- **VPP queue placement telemetry**: managed interface operational state in gRPC, `arca show interfaces`, and NETCONF includes VPP RX/TX queue-to-worker placement when available
- **VPP QoS profile state**: managed interface operational state in gRPC, `arca show interfaces`, and NETCONF includes the bound class-of-service profile intent when available
- **FRR VRRP generation**: `--frr-apply-mode=file` can render `protocols vrrp` groups into FRR integrated interface configuration
- **Transactional FRR VRRP apply**: the default transactional backend renders `protocols vrrp` into FRR `frr-vrrpd` management candidate operations
- **VRRP Linux interface preparation**: FRR apply reconciles arca-owned macvlan interfaces with virtual MACs and host-prefix VIPs before applying VRRP configuration
- **FRR VRRP group visibility**: `/api/status` and the Web UI include per-group FRR VRRP state such as Master, Backup, missing, or inactive
- **VRRP CLI status**: `arca show vrrp` exposes FRR VRRP operational output through the daemon gRPC API
- **VPP LCP CLI status**: `arca show lcp` exposes cached VPP LCP reconciliation state through the daemon gRPC API
- **HA CLI status**: `arca show ha` exposes the control-plane HA convergence summary through the daemon gRPC API
- **Class-of-service CLI status**: `arca show class-of-service` exposes running CoS intent and intent-only scheduler/policer enforcement state through the daemon gRPC API
- **Standard FRR VRRP daemon**: `vrrpd` is part of the documented required FRR daemon set for appliance-router HA deployments
- **VPP MPLS interface forwarding**: `protocols mpls interface` enables or disables MPLS forwarding on managed VPP interfaces with rollback coverage
- **VPP L3VPN table plumbing**: routing-instance interfaces are bound to deterministic VPP IPv4/IPv6 FIB tables derived from route distinguishers
- **VPP interface counters**: operational interface state reads VPP stats socket counters for packet, byte, error, and drop visibility
- **FRR L3VPN import/export**: routing instances render FRR VRF and BGP VPN import/export configuration, including route targets and ordered policy-chain route-maps
- **VPP QoS profile binding**: class-of-service interface bindings apply output traffic-control profile intent to managed VPP interfaces with rollback coverage
- **Prometheus service configuration**: `system services prometheus` can enable the Prometheus and health endpoint from running configuration
- **SNMP service configuration**: `system services snmp` can enable the read-only SNMPv2c endpoint and set listen address, port, and community from running configuration
- **Read-only Web UI**: optional `--web-listen` HTTP dashboard and `/api/status` JSON endpoint backed by daemon observability state
- **Read-only config API**: Web UI exposes `/api/config` and a running configuration preview in set-command format
- **Web UI authentication**: password-backed `security users` enable HTTP Basic authentication and read-only RBAC for the dashboard APIs
- **Web configuration API**: `/api/config/validate` and `/api/config/commit` use the internal gRPC candidate workflow for authenticated operator/admin configuration changes
- **Browser configuration editor**: the Web UI can edit running config text, validate changes, show diffs, and commit through the Web configuration API
- **Web commit history**: `/api/config/history` and the dashboard expose recent configuration commits from the internal gRPC history API, and the browser panel refreshes after successful Web commits
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
