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
- **Observability**: Optional Prometheus `/metrics`, `/healthz`, read-only Web UI, read-only SNMPv2c, and a packaged Grafana dashboard.

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
8. [Security](#security)
   - [NETCONF Server](#netconf-server)
   - [User Management](#user-management)
   - [Rate Limiting](#rate-limiting)
9. [Configuration Workflow](#configuration-workflow)
10. [Examples](#examples)
11. [Runtime Options and Observability](#runtime-options-and-observability)
12. [Operational Commands](#operational-commands)
13. [Configuration Validation](#configuration-validation)
14. [Troubleshooting](#troubleshooting)
15. [References](#references)
16. [Version History](#version-history)

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

The following hierarchies are part of the v0.6 management-plane model. Parser, serializer, validation, clone, conversion, diff, and candidate command replacement support are implemented. Southbound application for data-plane HA, MPLS forwarding, L3VPN plumbing, and QoS enforcement is staged separately.

Until the corresponding southbound apply path is implemented, commits that leave unsupported MPLS, routing-instance, or class-of-service configuration active fail validation instead of being silently accepted. Removing those unsupported stanzas is allowed. VRRP is currently supported only by the FRR file backend (`--frr-apply-mode=file`); the transactional FRR backend still rejects active VRRP configuration.

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
set system services snmp community public
```

`listen-address` must be an IP address or `localhost`. When enabled without an explicit port, the daemon uses the standard UDP port `161`. When enabled without a community, the daemon uses `public`.

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

VRRP group IDs must be numeric and between `1` and `255`. VRRP priority must be between `1` and `254` when configured; omit it for default behavior.

### MPLS and Routing Instances

```
set protocols mpls interface ge-0/0/0

set routing-instances BLUE instance-type vrf
set routing-instances BLUE route-distinguisher 65000:100
set routing-instances BLUE vrf-target target:65000:100
set routing-instances BLUE interface ge-0/0/1
```

Only `instance-type vrf` is accepted in v0.6. Route distinguishers use `<asn>:<number>`, and VRF targets use `target:<asn>:<number>`.

### Class of Service

```
set class-of-service forwarding-class expedited-forwarding queue 5
set class-of-service traffic-control-profile WAN shaping-rate 1000000000
set class-of-service traffic-control-profile WAN scheduler-map WAN-SCHED
set class-of-service interfaces ge-0/0/0 output-traffic-control-profile WAN
```

Forwarding class queues must be between `0` and `7`. Interface bindings must reference an existing traffic-control profile.

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

**Note**: The NETCONF server is built into `arca-routerd`. The runtime bind address is controlled by the daemon flag `--netconf-listen` (default `:830`); this configuration value is retained in the model and should match the deployed NETCONF port.

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
--netconf-listen <addr>    NETCONF/SSH listen address (default: :830)
--host-key <path>          NETCONF SSH host key path
--user-db <path>           NETCONF user database path
--frr-apply-mode <mode>    FRR backend: transactional or file (default: transactional)
--metrics-listen <addr>    Prometheus listen address; disabled when empty
--web-listen <addr>        Web UI listen address; overrides system services web-ui config
--snmp-listen <addr>       SNMPv2c UDP listen address; disabled when empty
--snmp-community <value>   SNMPv2c read-only community; overrides system services snmp config (default: public)
--mock-vpp                 Use mock VPP client for tests
```

### FRR Apply Backend

The default backend is `transactional`. It requires FRR `mgmtd=yes` in `/etc/frr/daemons` and `vtysh` access for the `arca-router` service user, typically through the `frrvty` group.

The standard FRR daemon set for arca-router is `bgpd`, `ospfd`, `zebra`, `staticd`, `mgmtd`, and `vrrpd`. The `file` backend writes a full FRR config and applies it with `frr-reload.py`. It is retained for recovery and compatibility; deployments that use it must grant the service user the additional permissions needed to write `/etc/frr/frr.conf`.

### Prometheus and Health

Start the metrics endpoint with:

```bash
arca-routerd --metrics-listen=:9090
```

Endpoints:

- `GET /metrics`
- `GET /healthz`

The metrics endpoint exports daemon uptime, running config version, NETCONF counters, and cluster sync gauges for enabled state, node count, etcd sync configuration, and datastore alignment.

The packaged Grafana dashboard is installed at:

```
/usr/share/arca-router/grafana/arca-routerd-dashboard.json
```

### Web UI

Start the read-only Web UI with:

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
- `GET /api/status`
- `POST /api/config/validate`
- `POST /api/config/commit`

`/api/status` includes build metadata, uptime, running config version, datastore backend, cluster sync state, and NETCONF counters.
`/api/config` returns the running configuration as set-command text with the running config version. The dashboard renders the same running configuration in the browser editor.
`/api/config/history` returns recent configuration commits and backs the dashboard commit history panel.

When password-backed `security users` exist in running configuration, the Web UI requires HTTP Basic authentication. The built-in `read-only`, `operator`, and `admin` roles are authorized for the read-only dashboard and API endpoints.
Configuration writes require `operator` or `admin`. The dashboard editor calls `/api/config/validate` and `/api/config/commit`. `/api/config/validate` accepts `{ "config_text": "set ..." }` and returns validation status plus diff text. `/api/config/commit` accepts `{ "config_text": "set ...", "message": "..." }` and commits through the same internal gRPC candidate workflow used by the CLI.

### SNMP

Start the read-only SNMPv2c endpoint with:

```bash
arca-routerd --snmp-listen=:1161 --snmp-community=public
```

It can also be enabled from running configuration:

```
set system services snmp enabled true
set system services snmp listen-address 127.0.0.1
set system services snmp port 1161
set system services snmp community public
```

The packaged systemd unit grants `CAP_NET_BIND_SERVICE`, so the standard UDP port 161 can be used when configured:

```bash
arca-routerd --snmp-listen=:161 --snmp-community=<read-only-community>
```

SNMP is intended for monitoring only and should not be exposed on untrusted networks.

---

## Operational Commands

### Show Commands (arca)

```
# Interface status
arca show interfaces
arca show interfaces ge-0/0/0

# Routing table
arca show route
arca show route protocol bgp

# BGP summary
arca show bgp summary

# BGP neighbors
arca show bgp neighbor <ip>

# OSPF neighbors
arca show ospf neighbor

# Configuration
arca show configuration
```

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
# Prometheus and health, when --metrics-listen is enabled
curl http://127.0.0.1:9090/healthz
curl http://127.0.0.1:9090/metrics

# Web UI, when --web-listen or system services web-ui is enabled
curl http://127.0.0.1:8080/api/status
curl http://127.0.0.1:8080/api/config

# SNMP, when --snmp-listen is enabled
snmpget -v 2c -c public 127.0.0.1:1161 1.3.6.1.3.9950.1.3.0
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
  - Read-only Web UI dashboard, JSON status endpoint, and running config endpoint
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
