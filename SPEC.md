# arca-router Configuration Specification (v0.6.x)

This document specifies the configuration syntax and semantics for arca-router.

[日本語](SPEC.ja.md)

## Overview

arca-router uses Junos-like configuration syntax via `set` commands. Configuration is managed through:

1. **Unified daemon (`arca-routerd`)**: Single process handling VPP, FRR, NETCONF, gRPC, Prometheus, Web UI, and SNMP.
2. **Interactive CLI (`arca`)**: Thin gRPC client for operational commands and candidate/running configuration workflow.
3. **NETCONF/SSH**: Remote configuration via NETCONF protocol (RFC 6241), built into the daemon and backed by the same datastore/engine.
4. **File bootstrap**: `/etc/arca-router/arca-router.conf` is used at startup only when the configured datastore has no running configuration.

### v0.6.x Architecture

The v0.6.x line extends the unified daemon path:

- **Struct-first config model**: Configuration is represented as Go structs (`internal/model.RouterConfig`), not text. Text format is just one serialization.
- **SQLite or etcd candidate/running datastore**: SQLite remains the single-node default; etcd can be selected for clustered deployments.
- **Diff-based engine**: The config engine (`internal/engine`) computes minimal diffs between running and candidate configs, applying only what changed.
- **Plugin-based southbound**: VPP and FRR are `engine.Plugin` implementations, each receiving only the relevant diff.
- **Transactional FRR apply**: The default `--frr-apply-mode=transactional` backend uses the FRR management candidate datastore through `vtysh` `mgmt commit check` / `mgmt commit apply`.
- **Recovery FRR file backend**: `--frr-apply-mode=file` keeps the full-file reload path for recovery and compatibility.
- **gRPC internal API**: `arca` communicates with the daemon via Unix socket gRPC (`api/v1/router.proto`, default `/run/arca-router/routerd.sock`).
- **2-phase commit**: Validate all plugins → apply all plugins → rollback on any failure.
- **Advanced configuration model**: Clustering, MPLS, VRRP, routing instances, class of service, and Web UI service settings are represented in the struct-first model and diff engine.
- **Cluster datastore selection**: `arca-routerd` and embedded NETCONF share the same SQLite or etcd datastore backend.
- **Observability**: Optional Prometheus `/metrics`, `/healthz`, Web UI dashboard with authenticated config validate/commit APIs, read-only SNMPv2c, and a packaged Grafana dashboard.

Only the current command names are part of this specification: `arca-routerd` and `arca`. Obsolete command entrypoints are not maintained.

> **Compatibility note**: The `set` command syntax and NETCONF configuration model remain stable. Automatic migration tooling is intentionally not part of v0.6.x.

---

## Table of Contents

1. [Configuration Syntax](#configuration-syntax)
2. [System Configuration](#system-configuration)
3. [Interface Configuration](#interface-configuration)
4. [Routing Options](#routing-options)
5. [Protocols](#protocols)
   - [BGP](#bgp-configuration)
   - [OSPF](#ospf-configuration)
   - [Static Routes](#static-routes)
6. [Policy Options](#policy-options)
   - [Prefix Lists](#prefix-lists)
   - [Policy Statements](#policy-statements)
7. [Advanced v0.6 Configuration](#advanced-v06-configuration)
8. [Overlay v0.8 Configuration](#overlay-v08-configuration)
9. [Security](#security)
   - [NETCONF Server](#netconf-server)
   - [User Management](#user-management)
   - [Rate Limiting](#rate-limiting)
10. [Configuration Workflow](#configuration-workflow)
11. [Examples](#examples)
12. [Runtime Options and Observability](#runtime-options-and-observability)
13. [Operational Commands](#operational-commands)
14. [Configuration Validation](#configuration-validation)
15. [Troubleshooting](#troubleshooting)
16. [References](#references)
17. [Version History](#version-history)

---

## Configuration Syntax

### General Format

All configuration commands use the Junos-like `set` syntax:

```
set <hierarchy-path> <value>
```

**Hierarchy Levels**:
- System-level: `set system ...`
- Interface-level: `set interfaces ...`
- Routing-level: `set routing-options ...`
- Protocol-level: `set protocols ...`
- Policy-level: `set policy-options ...`
- Security-level: `set security ...`

**Comments**:
```
# This is a comment (line starting with #)
```

**Whitespace**: Multiple spaces/tabs are treated as single space

**Case Sensitivity**: Configuration keys are case-sensitive

---

## System Configuration

### Hostname

**Syntax**:
```
set system host-name <hostname>
```

**Parameters**:
- `<hostname>`: String (alphanumeric, hyphens allowed, 1-63 characters)

**Example**:
```
set system host-name arca-router-01
```

**Default**: `localhost`

---

## Interface Configuration

### Interface Naming Convention

- `ge-X/Y/Z`: Gigabit Ethernet (1 GbE)
- `xe-X/Y/Z`: 10 Gigabit Ethernet (10 GbE)
- `et-X/Y/Z`: 100 Gigabit Ethernet (100 GbE)

Where:
- `X`: FPC (Flexible PIC Concentrator) slot
- `Y`: PIC (Physical Interface Card) slot
- `Z`: Port number

### Interface Description

**Syntax**:
```
set interfaces <name> description <text>
```

**Parameters**:
- `<name>`: Interface name (e.g., `ge-0/0/0`)
- `<text>`: Description string (any text)

**Example**:
```
set interfaces ge-0/0/0 description "WAN Uplink to ISP"
set interfaces ge-0/0/1 description "LAN Interface"
```

### Interface IP Address (IPv4)

**Syntax**:
```
set interfaces <name> unit <unit-number> family inet address <cidr>
```

**Parameters**:
- `<name>`: Interface name
- `<unit-number>`: Logical unit number (0-4095)
- `<cidr>`: IPv4 address in CIDR notation (e.g., `192.168.1.1/24`)

**Example**:
```
set interfaces ge-0/0/0 unit 0 family inet address 10.0.1.1/24
set interfaces ge-0/0/0 unit 100 family inet address 172.16.1.1/28
```

### Interface IP Address (IPv6)

**Syntax**:
```
set interfaces <name> unit <unit-number> family inet6 address <cidr>
```

**Parameters**:
- `<name>`: Interface name
- `<unit-number>`: Logical unit number (0-4095)
- `<cidr>`: IPv6 address in CIDR notation (e.g., `2001:db8::1/64`)

**Example**:
```
set interfaces ge-0/0/0 unit 0 family inet6 address 2001:db8:1::1/64
set interfaces ge-0/0/1 unit 0 family inet6 address 2001:db8:2::1/64
```

### Hardware Mapping

Interfaces are mapped to physical NICs via `/etc/arca-router/hardware.yaml`:

```yaml
interfaces:
  - name: "ge-0/0/0"
    pci: "0000:03:00.0"
    driver: "avf"          # Intel AVF driver
    description: "WAN Uplink"
  - name: "ge-0/0/1"
    pci: "0000:03:00.1"
    driver: "avf"
    description: "LAN Interface"
```

**Supported Drivers**:
- `avf`: Intel Adaptive Virtual Function (recommended for Intel NICs)
- `rdma`: Mellanox RDMA-capable NICs
- `dpdk`: Generic DPDK driver

**Finding PCI Addresses**:
```
lspci | grep Ethernet
```

---

## Routing Options

### Autonomous System (AS) Number

**Syntax**:
```
set routing-options autonomous-system <asn>
```

**Parameters**:
- `<asn>`: AS number (1-4294967295)

**Example**:
```
set routing-options autonomous-system 65000
```

**Used by**: BGP

### Router ID

**Syntax**:
```
set routing-options router-id <ip-address>
```

**Parameters**:
- `<ip-address>`: IPv4 address (used as router identifier)

**Example**:
```
set routing-options router-id 10.0.1.1
```

**Used by**: BGP, OSPF

**Best Practice**: Use loopback or stable interface IP

### Static Routes

**Syntax**:
```
set routing-options static route <prefix> next-hop <ip-address> [distance <value>]
```

**Parameters**:
- `<prefix>`: Destination network in CIDR notation
- `<ip-address>`: Next-hop router IP address
- `<value>`: Optional administrative distance (1-255, default: 1)

**Examples**:
```
# Default route
set routing-options static route 0.0.0.0/0 next-hop 10.0.1.254

# Specific route with custom distance
set routing-options static route 192.168.100.0/24 next-hop 192.168.1.254 distance 10
```

---

## Protocols

### BGP Configuration

#### BGP Group

**Syntax**:
```
set protocols bgp group <group-name> type <type>
```

**Parameters**:
- `<group-name>`: Group identifier (alphanumeric string)
- `<type>`: `internal` (IBGP) or `external` (EBGP)

**Example**:
```
set protocols bgp group IBGP type internal
set protocols bgp group EBGP type external
```

#### BGP Neighbor

**Syntax**:
```
set protocols bgp group <group-name> neighbor <ip-address> peer-as <asn>
set protocols bgp group <group-name> neighbor <ip-address> description <text>
set protocols bgp group <group-name> neighbor <ip-address> local-address <ip-address>
```

**Parameters**:
- `<group-name>`: BGP group name
- `<ip-address>`: Neighbor IP address
- `<asn>`: Neighbor AS number
- `<text>`: Description string
- `<local-address>`: Source IP for BGP session

**Examples**:
```
set protocols bgp group IBGP neighbor 10.0.1.2 peer-as 65001
set protocols bgp group IBGP neighbor 10.0.1.2 description "Internal BGP Peer"
set protocols bgp group IBGP neighbor 10.0.1.2 local-address 10.0.1.1

set protocols bgp group EBGP neighbor 10.0.2.2 peer-as 65002
set protocols bgp group EBGP neighbor 10.0.2.2 description "External BGP Peer - ISP"
```

#### BGP Policy Application

**Syntax**:
```
set protocols bgp group <group-name> import <policy-name>
set protocols bgp group <group-name> export <policy-name>
```

**Parameters**:
- `<policy-name>`: Name of policy-statement to apply

**Example**:
```
set protocols bgp group EBGP import DENY-PRIVATE
set protocols bgp group EBGP export ANNOUNCE-CUSTOMER
```

See [Policy Options](#policy-options) for policy configuration.

### OSPF Configuration

#### OSPF Router ID

**Syntax**:
```
set protocols ospf router-id <ip-address>
```

**Parameters**:
- `<ip-address>`: IPv4 address (OSPF router identifier)

**Example**:
```
set protocols ospf router-id 10.0.1.1
```

#### OSPF Area Interface

**Syntax**:
```
set protocols ospf area <area-id> interface <interface-name>
set protocols ospf area <area-id> interface <interface-name> passive
set protocols ospf area <area-id> interface <interface-name> metric <metric>
set protocols ospf area <area-id> interface <interface-name> priority <priority>
```

**Parameters**:
- `<area-id>`: OSPF area ID in dotted-decimal notation (e.g., `0.0.0.0`) or integer (e.g., `0`)
- `<interface-name>`: Interface name (e.g., `ge-0/0/0`)
- `passive`: Do not send OSPF hellos (optional)
- `<metric>`: Link metric (1-65535, optional)
- `<priority>`: DR election priority (0-255, optional)

**Examples**:
```
set protocols ospf area 0.0.0.0 interface ge-0/0/0
set protocols ospf area 0.0.0.0 interface ge-0/0/1 passive
set protocols ospf area 0.0.0.0 interface ge-0/0/1 metric 100
set protocols ospf area 0.0.0.0 interface ge-0/0/1 priority 1
```

### Static Routes

See [Routing Options - Static Routes](#static-routes)

---

## Policy Options

Policy options enable fine-grained control over route filtering, route manipulation, and traffic forwarding.

### Prefix Lists

Define sets of IP prefixes for matching in policy-statements.

**Syntax**:
```
set policy-options prefix-list <name> <prefix>
```

**Parameters**:
- `<name>`: Prefix-list identifier
- `<prefix>`: IP prefix in CIDR notation (IPv4 or IPv6)

**Examples**:
```
# IPv4 prefix-list
set policy-options prefix-list PRIVATE 10.0.0.0/8
set policy-options prefix-list PRIVATE 172.16.0.0/12
set policy-options prefix-list PRIVATE 192.168.0.0/16

# IPv6 prefix-list
set policy-options prefix-list PUBLIC-V6 2001:db8::/32
```

**Note**: If a prefix-list contains both IPv4 and IPv6 prefixes, it is split into `<name>` (IPv4) and `<name>-v6` (IPv6) when generating FRR configuration.

### Policy Statements

Define routing policies with match conditions and actions.

#### Match Conditions

**Syntax**:
```
set policy-options policy-statement <policy-name> term <term-name> from <condition> <value>
```

**Supported Conditions**:
- `prefix-list <name>`: Match routes in prefix-list
- `protocol <protocol>`: Match routes from protocol (`bgp`, `ospf`, `ospf3`, `static`, `connected`, `direct`, `kernel`, `rip`)
- `neighbor <ip>`: Match routes from specific BGP neighbor
- `as-path "<regex>"`: Match AS-path with regular expression

**Examples**:
```
set policy-options policy-statement DENY-PRIVATE term DENY from prefix-list PRIVATE
set policy-options policy-statement FILTER-BGP term MATCH from protocol bgp
set policy-options policy-statement FILTER-AS term MATCH from as-path ".*65001.*"
```

#### Actions

**Syntax**:
```
set policy-options policy-statement <policy-name> term <term-name> then <action> [value]
```

**Supported Actions**:
- `accept`: Accept the route (permit)
- `reject`: Reject the route (deny)
- `local-preference <value>`: Set BGP local-preference (0-4294967295)
- `community <community>`: Set BGP community (AS:value format)

**Examples**:
```
set policy-options policy-statement DENY-PRIVATE term DENY then reject
set policy-options policy-statement DENY-PRIVATE term ALLOW then accept

set policy-options policy-statement PREFER-CUSTOMER term CUSTOMER then local-preference 200
set policy-options policy-statement PREFER-CUSTOMER term CUSTOMER then accept

set policy-options policy-statement TAG-TRANSIT term TRANSIT then community 65000:100
set policy-options policy-statement TAG-TRANSIT term TRANSIT then accept
```

#### Complete Policy Example

```
# Define prefix-list
set policy-options prefix-list CUSTOMER 10.100.0.0/16

# Create policy with match and action
set policy-options policy-statement PREFER-CUSTOMER term CUSTOMER from prefix-list CUSTOMER
set policy-options policy-statement PREFER-CUSTOMER term CUSTOMER then local-preference 200
set policy-options policy-statement PREFER-CUSTOMER term CUSTOMER then accept

# Default term (always include)
set policy-options policy-statement PREFER-CUSTOMER term DEFAULT then accept

# Apply to BGP
set protocols bgp group external import PREFER-CUSTOMER
```

**Best Practice**: Always include a default term with `accept` or `reject` action.

---

<a id="advanced-v06-configuration"></a>
## Advanced v0.6 Configuration

The following hierarchies are part of the v0.6 management-plane model. Parser, serializer, validation, clone, conversion, diff, and candidate command replacement support are implemented. FRR VRRP application, VPP MPLS interface forwarding, VPP routing-instance table plumbing, FRR L3VPN import/export control, VPP class-of-service profile binding and operational visibility, NETCONF live interface state, and VPP queue placement telemetry are implemented; queue scheduler/policer enforcement and operational QoS counters are staged separately.

Class-of-service interface bindings are applied to managed VPP interfaces as output traffic-control profile intent. VRRP and L3VPN control-plane configuration are applied by the FRR file backend and the default transactional FRR backend.

MPLS, VRRP, OSPF, routing-instance, and class-of-service interface references must point to interfaces defined under `interfaces`. Unknown interface references fail validation before southbound apply. Routing-instance VPN import/export settings also fail validation before apply when required import/export targets, route distinguishers, or `routing-options autonomous-system` are missing.

### Prometheus Service

```
set system services prometheus enabled true
set system services prometheus listen-address 127.0.0.1
set system services prometheus port 9090
```

`listen-address` must be an IP address or `localhost`. When enabled without an explicit port, the daemon uses port `9090`.

### Web UI Service

```
set system services web-ui enabled true
set system services web-ui listen-address 127.0.0.1
set system services web-ui port 8080
```

`listen-address` must be an IP address or `localhost`. When enabled without an explicit port, the daemon uses port `8080`.

### SNMP Service

```
set system services snmp enabled true
set system services snmp listen-address 127.0.0.1
set system services snmp port 1161
set system services snmp community <read-only-community>
```

`listen-address` must be an IP address or `localhost`. When enabled without an explicit port, the daemon uses the standard UDP port `161`. SNMPv2c requires an explicit community when enabled, and the well-known default communities `public` and `private` are rejected.

### Multi-chassis and VRRP

```
set chassis cluster enabled true
set chassis cluster node node0 address 192.0.2.10
set chassis cluster node node0 priority 120
set chassis cluster sync etcd endpoint http://127.0.0.1:2379

set protocols vrrp group 10 interface ge-0/0/0
set protocols vrrp group 10 virtual-address 192.0.2.1
set protocols vrrp group 10 priority 110
set protocols vrrp group 10 preempt
```

When `chassis cluster` is enabled with `sync etcd endpoint` values, the daemon must be running with `--datastore-backend=etcd`, and the configured sync endpoints must match `--etcd-endpoints`. Commits that would leave a mismatched cluster sync configuration active fail validation.

When `--datastore-backend=etcd` is active, arca-routerd polls the etcd running configuration revision. If another chassis commits a newer running configuration, the daemon reloads the latest snapshot from etcd and applies it through the same engine and southbound plugins used by local commits. The sync loop only reacts to changes in the etcd `running/current` key revision, so a local commit that has updated the engine but has not yet persisted its new running revision is not overwritten by an older snapshot.

VRRP group IDs must be numeric and between `1` and `255`. VRRP priority must be between `1` and `254` when configured; omit it for default behavior. The configured VRRP interface must exist under `interfaces`.

Before applying FRR VRRP configuration, arca-routerd prepares the Linux state expected by FRR `vrrpd`: arca-owned macvlan interfaces named `arv4-<id>-<hash>` or `arv6-<id>-<hash>` are created on the LCP interface, assigned the RFC VRRP virtual MAC, configured with the virtual address as `/32` or `/128`, and brought up. The prepared interface names are persisted in `/var/lib/arca-router/vrrp-interfaces.json` so stale arca-owned macvlan interfaces can be removed after daemon restart. This requires `CAP_NET_ADMIN`, which is included in the packaged systemd unit.

arca-routerd reads FRR VRRP operational state through `vtysh -c "show vrrp json"`. Post-failover convergence is exposed as read-only status, including per-group FRR VRRP state in `/api/status` and the Web UI. HA convergence is considered configured when chassis clustering is enabled and at least one VRRP group exists. It is considered converged only when the cluster has at least two nodes, etcd cluster sync is configured and aligned with the daemon datastore, etcd config synchronization is healthy, every configured FRR VRRP group is observed in an active `Master` or `Backup` state, configured FRR BFD peers are observed and up, and VPP LCP reconciliation has run without errors or inconsistencies.

### MPLS and Routing Instances

```
set protocols mpls interface ge-0/0/0

set routing-options autonomous-system 65000
set routing-instances BLUE instance-type vrf
set routing-instances BLUE route-distinguisher 65000:100
set routing-instances BLUE vrf-target target:65000:100
set routing-instances BLUE vrf-target import target:65000:101
set routing-instances BLUE vrf-target export target:65000:102
set routing-instances BLUE vrf-import BLUE-IN
set routing-instances BLUE vrf-export BLUE-OUT
set routing-instances BLUE interface ge-0/0/1
```

Only `instance-type vrf` is accepted in v0.6. Route distinguishers use `<asn>:<number>`. Shared and directional VRF targets use `target:<asn>:<number>`; bare `vrf-target` applies to both import and export, while `vrf-target import` and `vrf-target export` add direction-specific extended-community targets. `vrf-import` and `vrf-export` reference configured `policy-options policy-statement` names and may be repeated to build ordered policy chains.

`protocols mpls interface` enables MPLS forwarding on the corresponding managed VPP interface. Removing the stanza disables MPLS forwarding before the interface is removed from VPP. MPLS and routing-instance interface references must resolve to configured interfaces.

For VPP dataplane plumbing, each routing instance gets IPv4 and IPv6 FIB tables. When `route-distinguisher <asn>:<number>` is configured, `<number>` is used as the deterministic VPP table ID; otherwise arca-router derives a stable non-zero table ID from the routing-instance name. Interfaces listed under the routing instance are rebound to those tables, and configured addresses are removed and restored around table changes so existing addresses move with the binding. Live interface state reports the bound IPv4 and IPv6 VPP table IDs for operator verification. The internal gRPC state API, NETCONF `<get>` `state/routing-instances`, and `arca show routing-instances [name]` summarize routing-instance table IDs, interfaces, targets, and import/export policy chains from the same deterministic table plan.

For FRR control-plane plumbing, routing instances render FRR VRF entries and per-VRF BGP VPN import/export configuration. Bare `vrf-target` applies to both `rt vpn import` and `rt vpn export`; directional targets apply only to their direction. Export requires `route-distinguisher` and automatically enables `label vpn export auto`. `vrf-import` and `vrf-export` are applied as `route-map vpn import` and `route-map vpn export`; when multiple policies are configured, arca-router generates an ordered synthetic route-map for FRR's single route-map slot.

### Class of Service

```
set class-of-service forwarding-class expedited-forwarding queue 5
set class-of-service traffic-control-profile WAN shaping-rate 1000000000
set class-of-service traffic-control-profile WAN scheduler-map WAN-SCHED
set class-of-service interfaces ge-0/0/0 output-traffic-control-profile WAN
```

Forwarding class queues must be between `0` and `7`. Interface bindings must reference an existing traffic-control profile and a configured interface.

`arca show class-of-service` exposes the running forwarding classes, traffic-control profiles, interface bindings, and current enforcement status. VPP scheduler and policer enforcement remains `intent-only` until the supported VPP binapi surface is available. The VPP southbound detects class-of-service dataplane capabilities during initialization and records whether metadata binding, queue scheduler enforcement, policer enforcement, and operational QoS counters are supported; the current bundled VPP 24.10 binapi path supports metadata binding and reports scheduler, policer, and QoS counters as unsupported diagnostics.

---

<a id="overlay-v08-configuration"></a>
## Overlay v0.8 Configuration

The v0.8 management-plane model includes EVPN/VXLAN VNI intent. Parser, serializer, validation, clone, conversion, diff, NETCONF XML/YANG coverage, and the `/overlays/evpn` structured telemetry path are implemented for L2 and L3 VNIs. FRR EVPN control-plane generation is implemented through the FRR file backend: arca-router renders global BGP `l2vpn evpn` with `advertise-all-vni`, explicit L2 VNI route-targets, L3 VNI VRF bindings, and per-VRF EVPN route-targets. VPP VXLAN dataplane apply supports L2 and L3 VNIs with multicast VXLAN or unicast remote VTEPs. For L2 VNIs, the VPP southbound creates a bridge domain using the VNI as the bridge ID, creates the VXLAN tunnel, brings the tunnel interface up, and attaches it to the bridge domain. For L3 VNIs, it creates an L3 VXLAN tunnel, binds the tunnel interface to the routing-instance IPv4/IPv6 table, and deletes stale routing-instance tables only after dependent tunnels are removed.

```
set protocols evpn vni 10010 type l2
set protocols evpn vni 10010 bridge-domain BD-10
set protocols evpn vni 10010 vlan-id 10
set protocols evpn vni 10010 route-distinguisher 65000:10010
set protocols evpn vni 10010 vrf-target target:65000:10010
set protocols evpn vni 10010 vrf-target import target:65000:10011
set protocols evpn vni 10010 vrf-target export target:65000:10012
set protocols evpn vni 10010 source-interface ge-0/0/0
set protocols evpn vni 10010 source-address 192.0.2.1
set protocols evpn vni 10010 multicast-group 239.0.0.10

set protocols evpn vni 20010 type l3
set protocols evpn vni 20010 routing-instance BLUE
set protocols evpn vni 20010 source-interface ge-0/0/0
set protocols evpn vni 20010 remote-vtep 198.51.100.20
```

VNI values must be between `1` and `16777215`. `type l2` requires `bridge-domain` and may include `vlan-id`; `type l3` requires `routing-instance` and must reference a configured routing instance. Route distinguishers use `<asn>:<number>`, route targets use `target:<asn>:<number>`, source interfaces must reference configured interfaces, multicast groups must be valid multicast IPv4 or IPv6 addresses, and remote VTEPs must be valid unicast IPv4 or IPv6 addresses. `multicast-group` and `remote-vtep` are mutually exclusive. For current VPP dataplane apply, L2 and L3 VNIs require `source-interface` plus either `multicast-group` or `remote-vtep`; `source-address` may be omitted when it can be derived from a configured source-interface address in the same address family as the VXLAN destination. L3 VNIs use the referenced routing-instance table for VXLAN encapsulation and tunnel interface table binding. FRR EVPN generation currently maps route-target intent; FRR derives EVPN route distinguishers from local EVPN state.

---

## Security

### NETCONF Server

#### NETCONF Port

**Syntax**:
```
set security netconf ssh port <port>
```

**Parameters**:
- `<port>`: TCP port number (1-65535, default: 830)

**Example**:
```
set security netconf ssh port 830
```

**Note**: The NETCONF server is built into `arca-routerd`. When `--netconf-listen` is omitted, the daemon listens on the configured `security netconf ssh port`; if that is also unset, it uses `:830`. `--netconf-listen` remains the explicit runtime override and can include a listen address.

NETCONF XML get-config/edit-config supports the v0.6 management-plane model for `system services`, `chassis cluster`, `protocols mpls`, `protocols vrrp`, `routing-instances`, `class-of-service`, the v0.8 `protocols evpn` VNI intent model, and non-sensitive `security netconf` / `security rate-limit` settings. Security user secrets are intentionally not emitted in NETCONF XML replies.

NETCONF `<get>` returns config-derived system/routing state and, when arca-routerd can collect VPP state, live managed interface admin/oper status, physical address, bound `qos-profile`, VPP table bindings (`ipv4-table-id`, `ipv6-table-id`), counters (`rx-packets`, `tx-packets`, `rx-bytes`, `tx-bytes`, `rx-errors`, `tx-errors`, `drops`), and VPP RX/TX queue placement. If live collection fails, interface output falls back to configured addresses with unknown operational status.

The internal gRPC interface state API and `arca show interfaces` use the same managed VPP interface state source, so interface filters use configured names such as `ge-0/0/0` and expose the same bound QoS profile, VPP table binding, packet counters, and queue placement summary for local operators. The internal gRPC class-of-service API, `arca show class-of-service`, and the `/class-of-service` telemetry path expose the same VPP QoS capability diagnostics used by the Web/NMS status API.

The internal gRPC route state API reads FRR JSON route output for both IPv4 and IPv6 tables and returns structured route entries with prefix, next-hop, protocol, metric, interface, and active-path status. Prefix filters must be valid CIDR prefixes; protocol filters accept the FRR protocol names used by the route table, with `ospf3` normalized to `ospf6`.

The internal gRPC BGP neighbor state API reads FRR JSON summary output and returns structured peer address, remote AS, state, uptime, received-prefix, and sent-prefix counters. When FRR reports the same peer under multiple address families, arca-router returns one peer entry with prefix counters combined and the longest observed uptime.

The internal gRPC OSPF neighbor state API reads FRR JSON neighbor output for OSPFv2 and OSPFv3 and returns structured router ID, neighbor address, interface, state, role, priority, dead timer, and uptime fields.

NETCONF `<get>` exposes the same live route table state under `state/routes`, BGP neighbor state under `state/protocols/bgp`, and OSPFv2/OSPFv3 neighbor state under `state/protocols/ospf` and `state/protocols/ospf3`, using the FRR JSON operational readers shared with the internal gRPC state APIs.

The internal gRPC routing-instance state API returns running routing-instance intent with deterministic IPv4/IPv6 VPP table IDs, interface bindings, import/export targets, and import/export policy chains.

The internal gRPC BFD state API returns arca-routerd's cached FRR BFD convergence snapshot, including configured/observed/up/down peer counts, aggregate session-down and RX-fail counters, per-peer state, diagnostics, and convergence issues.

NETCONF `<get>` exposes the same cached BFD operational state under `state/protocols/bfd`, including aggregate counters, per-peer diagnostics, convergence issues, and the last collection error.

The CLI exposes the cached BFD snapshot through `arca show bfd status`. Raw FRR BFD output remains available through `arca show bfd`, `arca show bfd brief`, `arca show bfd counters`, and `arca show bfd peer <ip> [counters]`.

The server hello advertises the arca-router YANG module capability as `urn:arca:router:config:1.0?module=arca-router&revision=2025-12-27`.

### User Management

#### Create User

**Syntax**:
```
set security users user <username> password <password>
set security users user <username> role <role>
```

**Parameters**:
- `<username>`: Username (alphanumeric, 3-32 characters)
- `<password>`: Optional password (recommended: 8+ characters); if omitted, the user can authenticate using SSH public key(s) only (key-only user)
- `<role>`: User role (`admin`, `operator`, `read-only`)

**Roles**:
- `admin`: Full access (all NETCONF operations including `kill-session`)
- `operator`: Configuration management (edit, commit, lock, unlock)
- `read-only`: View-only access (get-config, get)

**Examples**:
```
# Create admin user
set security users user alice password SuperSecret123
set security users user alice role admin

# Create operator user
set security users user bob password Operator456
set security users user bob role operator

# Create read-only user
set security users user monitor password ReadOnly789
set security users user monitor role read-only
```

**Best Practice**: Use strong passwords and follow least-privilege principle.

#### SSH Public Key Authentication

**Syntax**:
```
set security users user <username> ssh-key "<public-key>"
```

**Parameters**:
- `<public-key>`: SSH public key in OpenSSH format

**Example**:
```
set security users user alice ssh-key "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQ... alice@workstation"
```

**Note**: Public key authentication is preferred over password authentication for automated systems.

### Rate Limiting

**Syntax**:
```
set security rate-limit per-ip <limit>
set security rate-limit per-user <limit>
```

**Parameters**:
- `<limit>`: Maximum requests per second (1-1000)

**Example**:
```
set security rate-limit per-ip 10
set security rate-limit per-user 20
```

**Default**:
- Per-IP: 10 requests/second
- Per-user: 20 requests/second

---

## Configuration Workflow

### File-based Configuration

The file at `/etc/arca-router/arca-router.conf` is a bootstrap source. On startup, `arca-routerd` first attempts to load the current running configuration from the configured datastore. If no running configuration exists, it parses the file, applies it through the engine, and persists it to the datastore.

1. Edit `/etc/arca-router/arca-router.conf` before the first daemon start, or after intentionally clearing the datastore.
2. Start or restart daemon: `sudo systemctl restart arca-routerd`
3. Verify: `sudo journalctl -u arca-routerd -n 50`

After the datastore is initialized, use `arca` or NETCONF for normal configuration changes.

For clustered deployments, use the etcd datastore backend:

```bash
arca-routerd \
  --datastore-backend=etcd \
  --etcd-endpoints=https://etcd1:2379,https://etcd2:2379,https://etcd3:2379 \
  --etcd-prefix=/arca-router/
```

If `chassis cluster sync etcd endpoint` is configured, those endpoints must match the daemon's `--etcd-endpoints`; otherwise startup or commit validation fails before the configuration is accepted.

### NETCONF Configuration

NETCONF edits use the same candidate/running datastore and engine as `arca`.

1. Connect via NETCONF client:
   ```bash
   netconf-console --host 192.168.1.1 --port 830 --user alice --password xxx
   ```

2. Edit candidate configuration:
   ```xml
   <rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
     <edit-config>
       <target><candidate/></target>
       <config>
         <system xmlns="urn:arca:router:config:1.0">
           <host-name>new-hostname</host-name>
         </system>
       </config>
     </edit-config>
   </rpc>
   ```

3. Validate and commit:
   ```xml
   <rpc message-id="102"><validate><source><candidate/></source></validate></rpc>
   <rpc message-id="103"><commit/></rpc>
   ```

### Interactive CLI Configuration

`arca` talks to `arca-routerd` over the Unix socket gRPC API. The default socket is `/run/arca-router/routerd.sock`; use `arca -socket <path>` when the daemon is started with a custom `--grpc-socket`.

1. Enter configuration mode:
   ```bash
   arca
   > configure
   [edit]
   ```

2. Make changes:
   ```bash
   # set system host-name router-new
   # set interfaces ge-0/0/0 unit 0 family inet address 10.0.2.1/24
   ```

3. Validate and commit:
   ```bash
   # commit check
   # commit
   # exit
   ```

4. View changes:
   ```bash
   # show | compare
   ```

Supported configuration mode commands:

```
set <config>              Add or modify configuration
delete <config>           Delete configuration by prefix
show                      Show candidate configuration
show | compare            Show candidate vs running diff
commit                    Commit candidate configuration
commit check              Validate without committing
commit and-quit           Commit and exit configuration mode
commit comment <msg>      Commit with custom message
rollback <N>              Roll back N commits
discard-changes           Discard candidate changes
show history [N]          Show commit history
edit <path>               Enter a hierarchy path
up                        Move up one hierarchy level
top                       Return to the top hierarchy
```

### Rollback Configuration

**NETCONF**:
```xml
<rpc message-id="104"><discard-changes/></rpc>
```

**Interactive CLI**:
```
[edit]
# rollback 1
# commit
```

`rollback 0` is equivalent to `discard-changes`. `rollback <N>` creates a new commit that restores the target commit from history.

**File-based**:
```
# Restore from backup
sudo cp /etc/arca-router/arca-router.conf.backup /etc/arca-router/arca-router.conf
sudo systemctl restart arca-routerd
```

---

## Examples

### Example 1: Basic Router with BGP

```
# System configuration
set system host-name edge-router-01

# Interface configuration
set interfaces ge-0/0/0 description "WAN Uplink"
set interfaces ge-0/0/0 unit 0 family inet address 198.51.100.1/30
set interfaces ge-0/0/1 description "LAN Interface"
set interfaces ge-0/0/1 unit 0 family inet address 192.168.1.1/24

# Routing options
set routing-options autonomous-system 65000
set routing-options router-id 198.51.100.1

# BGP configuration
set protocols bgp group external type external
set protocols bgp group external neighbor 198.51.100.2 peer-as 65001
set protocols bgp group external neighbor 198.51.100.2 description "ISP Router"

# Static default route
set routing-options static route 0.0.0.0/0 next-hop 198.51.100.2
```

### Example 2: Router with OSPF and Policy

```
# System configuration
set system host-name core-router-01

# Interface configuration
set interfaces ge-0/0/0 description "Core Link"
set interfaces ge-0/0/0 unit 0 family inet address 10.0.1.1/24
set interfaces ge-0/0/1 description "LAN Interface"
set interfaces ge-0/0/1 unit 0 family inet address 192.168.1.1/24

# Routing options
set routing-options router-id 10.0.1.1

# OSPF configuration
set protocols ospf router-id 10.0.1.1
set protocols ospf area 0.0.0.0 interface ge-0/0/0
set protocols ospf area 0.0.0.0 interface ge-0/0/1 passive

# Policy: Deny private prefixes
set policy-options prefix-list PRIVATE 10.0.0.0/8
set policy-options prefix-list PRIVATE 172.16.0.0/12
set policy-options prefix-list PRIVATE 192.168.0.0/16

set policy-options policy-statement DENY-PRIVATE term DENY from prefix-list PRIVATE
set policy-options policy-statement DENY-PRIVATE term DENY then reject

set policy-options policy-statement DENY-PRIVATE term ALLOW then accept
```

### Example 3: Multi-protocol Router with Security

```
# System configuration
set system host-name mpls-pe-router-01

# Interface configuration
set interfaces ge-0/0/0 description "WAN Uplink"
set interfaces ge-0/0/0 unit 0 family inet address 198.51.100.1/30
set interfaces ge-0/0/0 unit 0 family inet6 address 2001:db8:1::1/64

set interfaces ge-0/0/1 description "LAN Interface"
set interfaces ge-0/0/1 unit 0 family inet address 192.168.1.1/24
set interfaces ge-0/0/1 unit 0 family inet6 address 2001:db8:2::1/64

# Routing options
set routing-options autonomous-system 65000
set routing-options router-id 198.51.100.1

# BGP configuration (IPv4 and IPv6)
set protocols bgp group external type external
set protocols bgp group external neighbor 198.51.100.2 peer-as 65001
set protocols bgp group external neighbor 198.51.100.2 description "ISP Router - IPv4"
set protocols bgp group external neighbor 2001:db8:1::2 peer-as 65001
set protocols bgp group external neighbor 2001:db8:1::2 description "ISP Router - IPv6"

# OSPF configuration
set protocols ospf router-id 198.51.100.1
set protocols ospf area 0.0.0.0 interface ge-0/0/1 passive

# Security configuration
set security netconf ssh port 830

set security users user admin password AdminPass123
set security users user admin role admin

set security users user operator password OpPass456
set security users user operator role operator

set security rate-limit per-ip 10
set security rate-limit per-user 20
```

---

## Runtime Options and Observability

### arca-routerd Runtime Options

The packaged service runs `/usr/sbin/arca-routerd`. The source build produces `build/bin/arca-routerd`.

Common options:

```
--config <path>            Bootstrap config file (default: /etc/arca-router/arca-router.conf)
--hardware <path>          Hardware mapping file (default: /etc/arca-router/hardware.yaml)
--datastore <path>         SQLite datastore (default: /var/lib/arca-router/config.db)
--datastore-backend <mode> Configuration datastore backend: sqlite or etcd (default: sqlite)
--etcd-endpoints <list>    Comma-separated etcd endpoints for --datastore-backend=etcd
--etcd-prefix <prefix>     etcd key prefix (default: /arca-router/)
--etcd-timeout <duration>  etcd connection and operation timeout (default: 5s)
--etcd-username <value>    etcd username
--etcd-password <value>    etcd password
--etcd-cert <path>         etcd TLS client certificate
--etcd-key <path>          etcd TLS client key
--etcd-ca <path>           etcd TLS CA certificate
--grpc-socket <path>       Internal gRPC Unix socket (default: /run/arca-router/routerd.sock)
--grpc-listen <addr>       TCP/TLS gRPC listen address
--grpc-tls-cert <path>     gRPC server TLS certificate
--grpc-tls-key <path>      gRPC server TLS private key
--grpc-client-ca <path>    CA certificate for verifying gRPC client certificates
--grpc-client-identity <value>
                           Comma-separated allowed gRPC client certificate identities
--grpc-client-role <value> Comma-separated gRPC client certificate identity=role mappings
--netconf-listen <addr>    NETCONF/SSH listen address; overrides security netconf ssh port (default: :830)
--host-key <path>          NETCONF SSH host key path
--user-db <path>           NETCONF user database path
--frr-apply-mode <mode>    FRR backend: transactional or file (default: transactional)
--metrics-listen <addr>    Prometheus listen address; overrides system services prometheus config
--web-listen <addr>        Web UI listen address; overrides system services web-ui config
--snmp-listen <addr>       SNMPv2c UDP listen address; disabled when empty
--snmp-community <value>   SNMPv2c read-only community; overrides system services snmp config; required when SNMP is enabled
--mock-vpp                 Use mock VPP client for tests
```

### FRR Apply Backend

The default backend is `transactional`. It requires FRR `mgmtd=yes` in `/etc/frr/daemons` and `vtysh` access for the `arca-router` service user, typically through the `frrvty` group.

The standard FRR daemon set for arca-router is `bgpd`, `ospfd`, `ospf6d`, `zebra`, `staticd`, `mgmtd`, `vrrpd`, and `bfdd`. The transactional backend applies VRRP through the FRR `frr-vrrpd` YANG model under the interface tree, explicit BFD profiles/sessions through `frr-bfdd`, static route BFD monitoring through `frr-staticd`, profile-less BGP neighbor BFD enablement through `frr-bgp`, and profile-less OSPF interface BFD through `frr-ospfd`. arca-routerd automatically falls back to the file backend for OSPFv3 and BGP/OSPF BFD profile bindings until FRR exposes those management YANG paths. The `file` backend writes a full FRR config and applies it with `frr-reload.py`. It is retained for recovery and compatibility; deployments that use it directly or through automatic fallback must grant the service user the additional permissions needed to write `/etc/frr/frr.conf`.

### Prometheus and Health

Start the metrics endpoint with:

```bash
arca-routerd --metrics-listen=:9090
```

It can also be enabled from running configuration:

```
set system services prometheus enabled true
set system services prometheus listen-address 127.0.0.1
set system services prometheus port 9090
```

Endpoints:

- `GET /metrics`
- `GET /healthz`

The metrics endpoint exports daemon uptime, running config version, NETCONF counters, config sync gauges for etcd health and running revision, cluster sync gauges for enabled state, node count, etcd sync configuration, datastore alignment, EVPN/VXLAN overlay intent gauges for configured state and VNI counts, FRR VRRP operational gauges, HA convergence gauges, class-of-service intent and VPP QoS capability gauges, and VPP LCP reconciliation gauges for pair count, inconsistency count, check failures, and latest check timestamp.

The packaged Grafana dashboard is installed at:

```
/usr/share/arca-router/grafana/arca-routerd-dashboard.json
```

It includes daemon, NETCONF, config sync, HA, FRR VRRP, EVPN/VXLAN overlay intent, class-of-service intent and VPP QoS capability, and VPP LCP panels backed by the Prometheus metrics endpoint.

### gRPC Telemetry Stream

The internal Unix socket gRPC API includes `TelemetryService.GetTelemetryCatalog` for stream discovery and `TelemetryService.SubscribeTelemetry` for structured streaming telemetry. The catalog returns the event schema version, payload encoding, default paths, default/min/max sample interval hints in milliseconds, supported paths, descriptions, cardinality hints, per-path payload schema IDs, accepted path aliases, and default membership. `GetTelemetryCatalog` accepts repeated path, cardinality, payload schema, and payload encoding filters, plus a default-only filter, so collectors can discover only the paths or path classes they plan to subscribe to; path filters match canonical paths or advertised aliases such as `/evpn`. Events use the `arca.telemetry.v1` envelope with `sequence`, `timestamp`, `path`, `cardinality`, `payload_schema`, `event_type`, `encoding`, `json_payload`, and `payload_bytes`; payloads are JSON. Subscriptions can select paths, set a sample interval, or request a one-shot snapshot. Empty path selection defaults to `/system` and `/config/running`.

Supported paths are `/system`, `/config/running`, `/interfaces`, `/routes`, `/routing/bgp/neighbors`, `/routing/ospf/neighbors`, `/routing/ospf3/neighbors`, `/routing-instances`, `/overlays/evpn`, `/class-of-service`, `/bfd`, `/lcp`, and `/ha`. The server writes events synchronously to the gRPC stream, so gRPC flow control provides the backpressure boundary and the daemon does not keep unbounded per-subscriber event buffers.

Local operators can inspect the same stream with `arca show telemetry path /system path /interfaces`; the CLI prints one JSON envelope per line. `interval <duration>` and `count <events>` request a sampled stream for a bounded number of events, for example `arca show telemetry path /routes interval 5s count 3`. `arca show telemetry paths` prints the local telemetry path catalog with default/min/max sample interval hints, cardinality hints, payload schema IDs, default membership, and descriptions before operators subscribe to high-cardinality paths, and does not require a daemon connection. It accepts `default`, `path <path-or-alias>`, `cardinality <hint>`, `payload-schema <id>`, and `encoding <encoding>` catalog filters, for example `arca show telemetry paths default` or `arca show telemetry paths encoding json`. `arca show telemetry paths live` queries `TelemetryService.GetTelemetryCatalog` to show the connected daemon's advertised catalog and pushes the same filters into the RPC, for example `arca show telemetry paths live cardinality per-route`.

For external NMS polling, the Web API exposes `GET /api/nms/v1/status`. The response is a stable JSON envelope with `schema_version` set to `arca.nms.operational.v1`, `generated_at`, `resource`, and `data`. The `data` object contains the same read-only operational status as `/api/status`, including build metadata, config version, datastore state, config sync, HA, CoS, FRR, VPP LCP, and NETCONF counters.

The Web API also exposes `GET /api/nms/v1/telemetry/paths` for collector discovery. The response is a stable JSON envelope with `schema_version` set to `arca.nms.telemetry-catalog.v1`, `generated_at`, `event_schema_version`, `encoding`, `default_paths`, default/min/max sample interval hints in milliseconds, filtered `path_count`, and the ordered telemetry `paths` catalog with descriptions, cardinality hints, payload schema IDs, accepted aliases, and default membership. Repeated `path`, `cardinality`, `payload_schema`, and `encoding` query parameters filter the returned catalog; `default=true` returns only default subscription paths. `path` values match canonical paths or advertised aliases. For example, `?path=/evpn` returns the EVPN path and `?cardinality=per-route&payload_schema=arca.telemetry.routes.v1&encoding=json` returns JSON route snapshots.

Collectors that need stable payload field metadata can call `GET /api/nms/v1/telemetry/schemas`. The response is a stable JSON envelope with `schema_version` set to `arca.nms.telemetry-schemas.v1`, `generated_at`, `event_schema_version`, `encoding`, `default_paths`, default/min/max sample interval hints in milliseconds, filtered `schema_count`, and ordered `schemas` entries for each telemetry path. Each schema entry repeats the path description, cardinality hint, payload schema ID, accepted aliases, default membership, and the stable top-level JSON `fields` with field name, type hint, and description. The endpoint accepts the same repeated `path`, `cardinality`, `payload_schema`, `encoding`, and `default=true` filters as `/api/nms/v1/telemetry/paths`.

HTTP-only collectors can request one-shot telemetry through `GET /api/nms/v1/telemetry/snapshot`. The endpoint accepts repeated `path` query parameters, such as `?path=/system&path=/interfaces`; when no metadata filter is provided, omitting `path` uses the same default path set as the gRPC telemetry stream. It also accepts `default=true`, repeated `cardinality`, repeated `payload_schema`, and repeated `encoding` query parameters to apply catalog metadata filters directly to the snapshot path set, for example `?cardinality=per-route&payload_schema=arca.telemetry.routes.v1`. It also accepts `timeout` as a Go duration string, defaulting to `5s` with a maximum of `30s`, `max_payload_bytes`, defaulting to `8388608` with a maximum of `67108864`, and `max_events`, defaulting to `64` with a maximum of `1024`, so large paths such as `/routes` and unexpected event fan-out stay bounded. The response is a stable JSON envelope with `schema_version` set to `arca.nms.telemetry-snapshot.v1`, `generated_at`, `event_schema_version`, `encoding`, default paths, default/min/max sample interval hints in milliseconds, emitted `paths`, `event_count`, total `payload_bytes`, `max_payload_bytes`, `max_events`, `timeout_ms`, and `events` carrying the same structured telemetry event fields, per-event `cardinality`, `payload_schema`, and `payload_bytes`, and JSON payloads as the gRPC stream.

`examples/nms` includes a standard-library HTTP collector example for the status, telemetry catalog, telemetry payload schema registry, and bounded telemetry snapshot endpoints. The example decodes catalog, schema, and snapshot default paths plus sample interval hints, validates NMS status envelope metadata, the status `data` object, required status data fields, sections, nested section fields, optional status arrays, non-empty status text, optional RFC3339 status section timestamps and generated_at timing bounds, optional status diagnostic metadata, status datastore consistency, status state and boolean consistency, status sync consistency, status counter relationships, and status aggregate counts, telemetry discovery, snapshot envelope metadata, RFC3339 `generated_at` timestamps, catalog path metadata, schema registry entries, payload field declarations, and per-event snapshot sequence, timestamp, path, cardinality, payload schema, encoding, and payload byte metadata, checks telemetry result counts, default path lists and sample interval hints, emitted paths, payload byte totals, and advertised guardrails against decoded data, can request default-only paths, selected paths or aliases, cardinalities, payload schema IDs, or payload encodings from the telemetry catalog and schema registry, and pushes the same include filters directly to the snapshot endpoint when catalog exclusion or discovery is not needed. It can also exclude selected paths or aliases, cardinalities such as `per-route`, payload schema IDs such as `arca.telemetry.routes.v1`, or payload encodings before requesting a snapshot. Exclude filters use the catalog aliases, so a snapshot path selected as `evpn` can still be filtered with `/overlays/evpn` metadata. When `-otlp-endpoint` is set in snapshot mode, the collector also exports snapshot events as OTLP/HTTP JSON log records to an OpenTelemetry logs endpoint such as `/v1/logs`, using `service.name` from `-otlp-service-name` and `arca.telemetry.cardinality` plus `arca.telemetry.payload_schema` from each event.

### Web UI

Start the Web UI with:

```bash
arca-routerd --web-listen=127.0.0.1:8080
```

It can also be enabled from configuration:

```
set system services web-ui enabled true
set system services web-ui listen-address 127.0.0.1
set system services web-ui port 8080
```

Endpoints:

- `GET /`
- `GET /api/config`
- `GET /api/config/history`
- `GET /api/audit`
- `GET /api/status`
- `GET /api/nms/v1/status`
- `GET /api/nms/v1/telemetry/paths`
- `GET /api/nms/v1/telemetry/schemas`
- `GET /api/nms/v1/telemetry/snapshot`
- `POST /api/config/validate`
- `POST /api/config/commit`

`/api/status` includes build metadata, uptime, running config version, datastore backend, cluster sync state, EVPN/VXLAN overlay intent counts, class-of-service intent state with VPP QoS capability diagnostics, FRR VRRP operational state with per-group state details, HA convergence state, VPP LCP reconciliation state, and NETCONF counters.
`/api/nms/v1/status` wraps the same read-only status in the `arca.nms.operational.v1` schema envelope for external NMS collectors.
`/api/nms/v1/telemetry/paths` wraps the structured telemetry path catalog in the `arca.nms.telemetry-catalog.v1` schema envelope for collector discovery.
`/api/nms/v1/telemetry/schemas` wraps the structured telemetry payload schema registry in the `arca.nms.telemetry-schemas.v1` schema envelope for collector validation and routing.
`/api/nms/v1/telemetry/snapshot` wraps one-shot structured telemetry events in the `arca.nms.telemetry-snapshot.v1` schema envelope for HTTP-only collectors and enforces configurable timeout, payload byte, and event count guardrails.
`/api/config` returns the running configuration as set-command text with the running config version. The dashboard renders the same running configuration in the browser editor.
`/api/config/history` returns recent configuration commits and backs the dashboard commit history panel.
`/api/audit` returns newest-first audit events in the `arca.audit.v1` schema envelope with optional `limit`, `offset`, `user`, `action`, `result`, `since`, and `until` filters. Audit export requires password-backed `admin` authentication.

When password-backed `security users` exist in running configuration, the Web UI requires HTTP Basic authentication. The built-in `read-only`, `operator`, and `admin` roles are authorized for the read-only dashboard and API endpoints.
Configuration writes require `operator` or `admin`. The dashboard editor calls `/api/config/validate` and `/api/config/commit`. `/api/config/validate` accepts `{ "config_text": "set ..." }` and returns validation status plus diff text. `/api/config/commit` accepts `{ "config_text": "set ...", "message": "..." }` and commits through the same internal gRPC candidate workflow used by the CLI.

### SNMP

Start the read-only SNMPv2c endpoint with:

```bash
arca-routerd --snmp-listen=:1161 --snmp-community=<read-only-community>
```

It can also be enabled from running configuration:

```
set system services snmp enabled true
set system services snmp listen-address 127.0.0.1
set system services snmp port 1161
set system services snmp community <read-only-community>
```

The packaged systemd unit grants `CAP_NET_BIND_SERVICE`, so the standard UDP port 161 can be used when configured. The well-known default communities `public` and `private` are rejected:

```bash
arca-routerd --snmp-listen=:161 --snmp-community=<read-only-community>
```

SNMP is intended for monitoring only and should not be exposed on untrusted networks. The custom arca-router OID subtree exposes daemon, config, NETCONF, EVPN/VXLAN overlay intent, class-of-service intent and VPP QoS capability, FRR VRRP operational, HA convergence, and VPP LCP reconciliation counters.

---

## Operational Commands

### Show Commands (arca)

```
# Interface status
arca show interfaces
arca show interfaces ge-0/0/0

# Routing table
arca show routes
arca show routes protocol bgp
arca show routes prefix 2001:db8::/64
arca show route
arca show route protocol bgp

# BGP summary
arca show bgp neighbors
arca show bgp summary

# BGP neighbors
arca show bgp neighbor <ip>

# OSPF neighbors
arca show ospf neighbor

# VRRP status
arca show vrrp

# EVPN/VXLAN overlay intent
arca show evpn

# VPP LCP reconciliation status
arca show lcp

# HA convergence status
arca show ha

# Class-of-service intent
arca show class-of-service

# Configuration
arca show configuration
```

`show interfaces` prints live managed VPP admin/oper status, bound QoS profile, packet counters, and RX/TX queue placement when available. Name filters use configured interface names such as `ge-0/0/0`. `show routes` prints structured IPv4/IPv6 route state from the internal gRPC state API and supports optional `prefix <cidr>` and `protocol <proto>` filters; `show route` retains raw FRR route output. `show bgp neighbors` prints structured BGP neighbor state from the internal gRPC state API, while `show bgp summary` and `show bgp neighbor <ip>` retain raw FRR output. `show ospf neighbor` and `show ospf3 neighbor` print structured OSPF neighbor state from the same gRPC state API. `show vrrp` prints FRR `show vrrp` output through arca-routerd for local HA inspection. `show evpn` renders the `/overlays/evpn` telemetry snapshot as a VNI summary for local overlay inspection. `show lcp` prints the cached VPP LCP reconciliation state used by HA convergence checks. `show ha` prints the same HA convergence summary used by Web UI, Prometheus, and SNMP, including FRR VRRP, configured FRR BFD peer health, and VPP LCP reconciliation status. `show class-of-service` prints running CoS intent, reports `intent-only` for scheduler/policer enforcement while VPP enforcement support is staged separately, and includes VPP QoS capability diagnostics.

Interactive mode also supports `show history [N]` in configuration mode for commit history.

### Direct VPP Commands

```
# Interface status
sudo vppctl show interface

# Linux Control Plane (LCP) status
sudo vppctl show lcp

# IP forwarding table
sudo vppctl show ip fib

# IPv6 forwarding table
sudo vppctl show ip6 fib
```

### Direct FRR Commands

```
# Enter FRR CLI
sudo vtysh

# Show running config
show running-config

# Show IP routes
show ip route

# Show BGP summary
show ip bgp summary

# Show BGP neighbors
show ip bgp neighbors

# Show OSPF neighbors
show ip ospf neighbor
```

---

## Configuration Validation

### Syntax Validation

```
# Interactive candidate validation
arca
> configure
[edit]
# commit check
```

### Pre-deployment Checks

```
# Validate local package metadata and service expectations
make package-lint

# Run the live FRR transactional apply smoke test on a host with FRR mgmtd enabled
make frr-mgmtd-smoke

# FRR configuration is generated/applied by arca-routerd; verify on the host using vtysh

# Check BGP session status
sudo vtysh -c "show ip bgp summary"

# Check OSPF neighbors
sudo vtysh -c "show ip ospf neighbor"
```

---

## Troubleshooting

### Check Daemon Status

```
sudo systemctl status arca-routerd
sudo journalctl -u arca-routerd -n 50
```

### Check Datastore and Socket

```
# Running/candidate datastore
sudo ls -l /var/lib/arca-router/config.db

# Internal gRPC socket used by arca
sudo ls -l /run/arca-router/routerd.sock
```

### Check VPP Status

```
sudo systemctl status vpp
sudo vppctl show interface
```

### Check FRR Status

```
sudo systemctl status frr
grep '^mgmtd=yes' /etc/frr/daemons
sudo vtysh -c "show running-config"
```

### Check Observability Endpoints

```
# Prometheus and health, when --metrics-listen or system services prometheus is enabled
curl http://127.0.0.1:9090/healthz
curl http://127.0.0.1:9090/metrics

# Web UI, when --web-listen or system services web-ui is enabled
curl http://127.0.0.1:8080/api/status
curl http://127.0.0.1:8080/api/config

# SNMP, when --snmp-listen or system services snmp is enabled
snmpget -v 2c -c <read-only-community> 127.0.0.1:1161 1.3.6.1.3.9950.1.3.0
```

### Verify Interface Mapping

```
# Check hardware.yaml mappings
cat /etc/arca-router/hardware.yaml

# Verify PCI addresses
lspci | grep Ethernet

# Check VPP interface binding
sudo vppctl show interface addr
```

---

## References

- [Roadmap](ROADMAP.md)
- [Changelog](CHANGELOG.md)
- [Observability](docs/observability.md)
- [Datastore Design](docs/datastore-design.md)
- [Configuration Precedence Rules](docs/config-precedence.md)
- [Policy Options Guide](docs/policy-options-guide.md)
- [RBAC Guide](docs/rbac-guide.md)
- [Security Model](docs/security-model.md)
- [VPP Setup Guide](docs/vpp-setup-debian.md)
- [FRR Setup Guide](docs/frr-setup-debian.md)

---

## Version History

- **v0.6.x**: Advanced feature foundations
  - Management-plane config model for clustering, MPLS, VRRP, routing instances, class of service, and Web UI
  - etcd datastore backend selection for clustered candidate/running configuration
  - Web UI dashboard, JSON status/config endpoints, authenticated validate/commit API, and commit history panel
  - v0.6 config diff and candidate replacement coverage

- **v0.5.x**: Production hardening
  - Current command names: `arca-routerd` and `arca`
  - Generated gRPC API between daemon and CLI
  - SQLite-backed candidate/running datastore with commit history
  - FRR transactional apply through management candidate datastore
  - Prometheus, health, SNMP, and Grafana observability
  - Obsolete command entrypoints removed

- **v0.4.x**: Unified architecture
  - Single daemon for VPP, FRR, NETCONF, and gRPC
  - Struct-first configuration model
  - Diff-based apply engine and plugin southbound
  - Thin gRPC CLI client

- **v0.3.1** (2025-12-28): Complete specification
  - NETCONF/SSH subsystem
  - Interactive CLI
  - Policy options (prefix-lists, policy-statements)
  - Security features (RBAC, rate limiting, audit logging)
  - Configuration workflow (commit/rollback)

- **v0.2.x**: VPP and FRR integration
  - Real VPP integration
  - FRR routing protocols (BGP, OSPF)
  - LCP (Linux Control Plane)

- **v0.1.x**: MVP with mock VPP
  - Configuration parser
  - systemd integration
  - RPM/DEB packaging
