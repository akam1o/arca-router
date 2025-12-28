# arca-router Configuration Specification (v0.3.x)

This document specifies the configuration syntax and semantics for arca-router v0.3.x.

## Overview

arca-router uses Junos-like configuration syntax via `set` commands. Configuration is managed through:

1. **NETCONF/SSH**: Remote configuration via NETCONF protocol (RFC 6241)
2. **Interactive CLI**: Real-time configuration with commit/rollback (`arca-cli`)
3. **File-based**: Static configuration files (`/etc/arca-router/arca-router.conf`)

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
7. [Security](#security)
   - [NETCONF Server](#netconf-server)
   - [User Management](#user-management)
   - [Rate Limiting](#rate-limiting)
8. [Configuration Workflow](#configuration-workflow)
9. [Examples](#examples)

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

## Security

### NETCONF Server

#### Enable NETCONF Server

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

**Note**: NETCONF server is managed by `arca-netconfd` daemon (separate from `arca-routerd`).

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

1. Edit `/etc/arca-router/arca-router.conf`
2. Restart daemon: `sudo systemctl restart arca-routerd`
3. Verify: `sudo journalctl -u arca-routerd -n 50`

### NETCONF Configuration

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

1. Enter configuration mode:
   ```bash
   arca-cli
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
   > show configuration changes
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

## Operational Commands

### Show Commands (arca-cli)

```
# Interface status
arca-cli show interfaces

# Routing table
arca-cli show route

# BGP summary
arca-cli show bgp summary

# BGP neighbors
arca-cli show bgp neighbor <ip>

# OSPF neighbors
arca-cli show ospf neighbor

# Configuration
arca-cli show configuration
```

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
arca-cli
> configure
[edit]
# commit check
```

### Pre-deployment Checks

```
# FRR configuration is generated/applied by arca-routerd; verify on the host using vtysh.

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

### Check VPP Status

```
sudo systemctl status vpp
sudo vppctl show interface
```

### Check FRR Status

```
sudo systemctl status frr
sudo vtysh -c "show running-config"
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

- [Policy Options Guide](docs/policy-options-guide.md)
- [RBAC Guide](docs/rbac-guide.md)
- [Security Model](docs/security-model.md)
- [VPP Setup Guide](docs/vpp-setup-debian.md)
- [FRR Setup Guide](docs/frr-setup-debian.md)
- [NETCONF Implementation Plan](docs/netconf-implementation-plan.md)

---

## Version History

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
