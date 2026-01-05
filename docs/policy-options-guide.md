# Policy Options Guide

**Version**: 0.3.1
**Last Updated**: 2025-12-27
**Status**: Phase 3 - FRR Implementation Complete

---

## Overview

arca-router supports policy-based routing through `policy-options` configuration, allowing fine-grained control over route filtering, route manipulation, and traffic forwarding. This guide covers the current implementation (FRR-only) and future plans for VPP integration.

## Architecture

### Phase 3: FRR Policy Implementation (Current)

The current implementation provides comprehensive policy support through FRR (Free Range Routing):

- **Prefix-lists**: Define sets of IP prefixes for matching
- **Policy-statements**: Create routing policies with match conditions and actions
- **Route-maps**: FRR route-maps applied to BGP neighbors
- **AS-path filtering**: Match routes based on AS-path regular expressions

Policy enforcement is implemented at the BGP protocol level within FRR, providing:
- Import/export route filtering
- Route attribute modification (local-preference, community, etc.)
- Protocol-based route redistribution

### Policy Flow

```
┌──────────────┐
│ BGP Neighbor │
└──────┬───────┘
       │ incoming routes
       ▼
┌──────────────────┐      ┌────────────────┐
│ Import Policy    │◄─────│ Prefix-lists   │
│ (route-map in)   │      │ AS-path lists  │
└──────┬───────────┘      │ Match rules    │
       │                  └────────────────┘
       │ filtered/modified routes
       ▼
┌──────────────────┐
│ BGP Routing      │
│ Information Base │
│ (RIB)            │
└──────┬───────────┘
       │ advertised routes
       ▼
┌──────────────────┐      ┌────────────────┐
│ Export Policy    │◄─────│ Prefix-lists   │
│ (route-map out)  │      │ Match rules    │
└──────┬───────────┘      │ Set actions    │
       │                  └────────────────┘
       │ filtered/modified routes
       ▼
┌──────────────────┐
│ BGP Neighbor     │
└──────────────────┘
```

---

## Configuration Syntax

### Prefix-lists

Define sets of IP prefixes for matching in policy-statements.

**Syntax**:
```
set policy-options prefix-list <name> <prefix>
```

**Examples**:
```junos
# IPv4 prefix-list
set policy-options prefix-list PRIVATE 10.0.0.0/8
set policy-options prefix-list PRIVATE 172.16.0.0/12
set policy-options prefix-list PRIVATE 192.168.0.0/16

# IPv6 prefix-list
set policy-options prefix-list PUBLIC-V6 2001:db8::/32

# Mixed IPv4/IPv6 prefix-list (automatically split)
set policy-options prefix-list MIXED 10.0.0.0/8
set policy-options prefix-list MIXED 2001:db8::/32
# Results in: MIXED (IPv4) and MIXED-v6 (IPv6)
```

**Notes**:
- Prefix-lists containing both IPv4 and IPv6 prefixes are automatically split into separate lists with `-v6` suffix for IPv6
- All prefixes within a prefix-list are implicitly `permit` actions
- Sequence numbers are auto-generated (10, 20, 30, ...)

### Policy-statements

Define routing policies with match conditions and actions.

**Syntax**:
```
set policy-options policy-statement <name> term <term-name> from <condition> <value>
set policy-options policy-statement <name> term <term-name> then <action> [value]
```

**Match Conditions** (`from` clause):
- `prefix-list <name>`: Match routes in specified prefix-list
- `protocol <protocol>`: Match routes from protocol (bgp, ospf, static, connected)
- `neighbor <ip>`: Match routes from specific BGP neighbor
- `as-path "<regex>"`: Match routes with AS-path matching regex

**Actions** (`then` clause):
- `accept`: Accept the route (permit in route-map)
- `reject`: Reject the route (deny in route-map)
- `local-preference <value>`: Set BGP local-preference attribute
- `community <community>`: Set BGP community attribute

**Examples**:

#### Example 1: Deny Private Prefixes
```junos
set policy-options prefix-list PRIVATE 10.0.0.0/8
set policy-options prefix-list PRIVATE 172.16.0.0/12
set policy-options prefix-list PRIVATE 192.168.0.0/16

set policy-options policy-statement DENY-PRIVATE term DENY from prefix-list PRIVATE
set policy-options policy-statement DENY-PRIVATE term DENY then reject

set policy-options policy-statement DENY-PRIVATE term ALLOW then accept
```

#### Example 2: Set Local Preference for Customer Routes
```junos
set policy-options prefix-list CUSTOMER 10.100.0.0/16

set policy-options policy-statement PREFER-CUSTOMER term CUSTOMER from prefix-list CUSTOMER
set policy-options policy-statement PREFER-CUSTOMER term CUSTOMER then local-preference 200
set policy-options policy-statement PREFER-CUSTOMER term CUSTOMER then accept

set policy-options policy-statement PREFER-CUSTOMER term DEFAULT then accept
```

#### Example 3: Filter Routes by AS-path
```junos
set policy-options policy-statement FILTER-AS term MATCH from as-path ".*65001.*"
set policy-options policy-statement FILTER-AS term MATCH then reject

set policy-options policy-statement FILTER-AS term DEFAULT then accept
```

#### Example 4: Set BGP Community
```junos
set policy-options prefix-list TRANSIT 10.200.0.0/16

set policy-options policy-statement TAG-TRANSIT term TRANSIT from prefix-list TRANSIT
set policy-options policy-statement TAG-TRANSIT term TRANSIT then community 65000:100
set policy-options policy-statement TAG-TRANSIT term TRANSIT then accept
```

### Applying Policies to BGP

Apply policy-statements to BGP neighbors using `import` and `export` directives.

**Syntax**:
```
set protocols bgp group <group-name> import <policy-name>
set protocols bgp group <group-name> export <policy-name>
```

**Example**:
```junos
set protocols bgp group external type external
set protocols bgp group external import DENY-PRIVATE
set protocols bgp group external export ANNOUNCE-CUSTOMER

set protocols bgp group external neighbor 10.0.1.2 peer-as 65002
```

**FRR Translation**:
```frr
router bgp 65001
 neighbor 10.0.1.2 remote-as 65002
 !
 address-family ipv4 unicast
  neighbor 10.0.1.2 activate
  neighbor 10.0.1.2 route-map DENY-PRIVATE in
  neighbor 10.0.1.2 route-map ANNOUNCE-CUSTOMER out
 exit-address-family
!
```

---

## FRR Policy Implementation Details

### Prefix-list Generation

**arca-router config**:
```junos
set policy-options prefix-list MYLIST 10.0.0.0/8
set policy-options prefix-list MYLIST 192.168.0.0/16
```

**FRR output**:
```frr
ip prefix-list MYLIST seq 10 permit 10.0.0.0/8
ip prefix-list MYLIST seq 20 permit 192.168.0.0/16
!
```

### Route-map Generation

**arca-router config**:
```junos
set policy-options prefix-list PRIVATE 10.0.0.0/8
set policy-options policy-statement DENY-PRIVATE term DENY from prefix-list PRIVATE
set policy-options policy-statement DENY-PRIVATE term DENY then reject
set policy-options policy-statement DENY-PRIVATE term ALLOW then accept
```

**FRR output**:
```frr
ip prefix-list PRIVATE seq 10 permit 10.0.0.0/8
!
route-map DENY-PRIVATE deny 10
 match ip address prefix-list PRIVATE
!
route-map DENY-PRIVATE permit 20
!
```

### AS-path Access-list Generation

**arca-router config**:
```junos
set policy-options policy-statement FILTER-AS term MATCH from as-path ".*65001.*"
set policy-options policy-statement FILTER-AS term MATCH then reject
```

**FRR output**:
```frr
bgp as-path access-list AS-PATH-1 seq 10 permit .*65001.*
!
route-map FILTER-AS deny 10
 match as-path AS-PATH-1
!
```

### IPv4/IPv6 Prefix-list Splitting

**arca-router config**:
```junos
set policy-options prefix-list MIXED 10.0.0.0/8
set policy-options prefix-list MIXED 2001:db8::/32
set policy-options policy-statement MYPOLICY term TERM1 from prefix-list MIXED
set policy-options policy-statement MYPOLICY term TERM1 then accept
```

**FRR output**:
```frr
ip prefix-list MIXED seq 10 permit 10.0.0.0/8
!
ipv6 prefix-list MIXED-v6 seq 10 permit 2001:db8::/32
!
route-map MYPOLICY permit 10
 match ip address prefix-list MIXED
 match ipv6 address prefix-list MIXED-v6
!
```

---

## Phase 4: VPP PBR (Policy-Based Routing) - FUTURE

**Status**: Not Implemented (Planned for Phase 4)

### Overview

Phase 4 will extend policy enforcement to the VPP (Vector Packet Processing) dataplane, enabling policy-based routing (PBR) for packet forwarding decisions based on packet headers, not just routing protocol information.

### Planned Features

#### 1. VPP ACL Integration

**Purpose**: Match packets based on L3/L4 headers

**Capabilities**:
- Source/destination IP address matching
- Source/destination port matching
- Protocol matching (TCP, UDP, ICMP, etc.)
- DSCP/TOS matching

**Example** (planned syntax):
```junos
set firewall family inet filter PBR-FILTER term MATCH-WEB from source-address 10.0.0.0/24
set firewall family inet filter PBR-FILTER term MATCH-WEB from destination-port 80
set firewall family inet filter PBR-FILTER term MATCH-WEB from destination-port 443
set firewall family inet filter PBR-FILTER term MATCH-WEB then routing-instance WEB-TRAFFIC
```

#### 2. VPP Policy Route

**Purpose**: Forward matched packets to specific next-hops or interfaces, bypassing the normal routing table lookup

**Capabilities**:
- Next-hop override
- Interface-based forwarding
- Load balancing across multiple next-hops
- QoS/traffic shaping integration

**Example** (planned syntax):
```junos
set routing-instances WEB-TRAFFIC instance-type forwarding
set routing-instances WEB-TRAFFIC routing-options static route 0.0.0.0/0 next-hop 10.1.1.1
```

#### 3. VPP Integration Architecture

```
┌─────────────────┐
│ Packet Received │
└────────┬────────┘
         │
         ▼
┌──────────────────┐      ┌────────────────┐
│ VPP ACL          │◄─────│ Firewall       │
│ (Packet Match)   │      │ Filter Rules   │
└────────┬─────────┘      └────────────────┘
         │
         │ matched packets
         ▼
┌──────────────────┐      ┌────────────────┐
│ VPP Policy Route │◄─────│ Routing        │
│ (PBR Forwarding) │      │ Instance Table │
└────────┬─────────┘      └────────────────┘
         │
         │ forwarded to next-hop
         ▼
┌──────────────────┐
│ Packet Sent      │
└──────────────────┘
```

### Implementation Notes

**Phase 4 Tasks**:
- [ ] VPP ACL API integration (packet matching)
- [ ] VPP policy route API integration (forwarding override)
- [ ] Config parser extension (firewall filters, routing-instances)
- [ ] VPP ACL generation from firewall filters
- [ ] VPP policy route table management
- [ ] Integration tests with FRR routing protocols
- [ ] Documentation and examples

**Challenges**:
- Coordination between FRR routing decisions and VPP PBR forwarding
- Performance impact of ACL matching in VPP dataplane
- State synchronization between FRR and VPP
- Operational visibility (show commands, debugging)

**Benefits**:
- Wire-speed policy-based forwarding
- Flexible traffic engineering
- Support for multi-tenancy and VRF-like scenarios
- Integration with VPP's advanced features (QoS, NAT, encryption)

---

## Operational Commands

### Show Commands

**View prefix-lists**:
```bash
vtysh -c "show ip prefix-list"
vtysh -c "show ipv6 prefix-list"
vtysh -c "show ip prefix-list MYLIST"
```

**View route-maps**:
```bash
vtysh -c "show route-map"
vtysh -c "show route-map MYPOLICY"
```

**View AS-path access-lists**:
```bash
vtysh -c "show bgp as-path-access-list"
```

**View applied policies**:
```bash
vtysh -c "show ip bgp neighbors 10.0.1.2"
# Look for "route-map for incoming/outgoing advertisements"
```

**View BGP routes and policy effects**:
```bash
vtysh -c "show ip bgp"
vtysh -c "show ip bgp 10.0.0.0/8"
vtysh -c "show ip bgp summary"
```

### Debugging

**Debug BGP policy processing**:
```bash
vtysh -c "debug bgp updates"
vtysh -c "debug bgp filters"
```

**Test route-map matching** (FRR 8.0+):
```bash
# Test if a prefix matches a route-map
vtysh -c "test route-map MYPOLICY 10.0.0.0/8"
```

---

## Best Practices

### Policy Design

1. **Explicit Default Term**: Always include a final term with `then accept` or `then reject` to handle unmatched routes
2. **Term Ordering**: Order terms from most specific to most general
3. **Prefix-list Reuse**: Define prefix-lists separately for reuse across multiple policies
4. **Naming Conventions**: Use descriptive names (e.g., `DENY-BOGONS`, `PREFER-CUSTOMER`)

### Performance Considerations

1. **Prefix-list Size**: Large prefix-lists (>1000 entries) may impact convergence time
2. **Term Count**: Limit policy-statements to <50 terms for optimal performance
3. **AS-path Regex**: Complex regex patterns can impact BGP UPDATE processing

### Operational Guidelines

1. **Change Management**: Test policy changes in lab/staging before production
2. **Validation**: Use `show` commands to verify policy application
3. **Documentation**: Document the purpose and expected behavior of each policy
4. **Monitoring**: Monitor BGP session stability and route counts after policy changes

### Common Pitfalls

1. **Missing Default Term**: Policies without a default term may drop all unmatched routes
2. **IPv4/IPv6 Mixing**: Be aware of automatic IPv6 prefix-list splitting with `-v6` suffix
3. **Policy Ordering**: Route-map terms are evaluated sequentially; order matters
4. **Import vs Export**: Ensure policies are applied in the correct direction

---

## Examples

### Example 1: Simple BGP Filtering

**Scenario**: Accept only specific customer prefixes, reject everything else

```junos
# Define customer prefixes
set policy-options prefix-list CUSTOMER 10.100.0.0/16
set policy-options prefix-list CUSTOMER 10.101.0.0/16

# Create import policy
set policy-options policy-statement CUSTOMER-IMPORT term ALLOW from prefix-list CUSTOMER
set policy-options policy-statement CUSTOMER-IMPORT term ALLOW then accept

set policy-options policy-statement CUSTOMER-IMPORT term DEFAULT then reject

# Apply to BGP group
set protocols bgp group customers import CUSTOMER-IMPORT
set protocols bgp group customers neighbor 10.0.1.2 peer-as 65002
```

### Example 2: Local Preference Based on Prefix

**Scenario**: Prefer customer routes over peer routes

```junos
# Define prefix-lists
set policy-options prefix-list CUSTOMER 10.100.0.0/16
set policy-options prefix-list PEER 10.200.0.0/16

# Create import policy with local-preference
set policy-options policy-statement LP-POLICY term CUSTOMER from prefix-list CUSTOMER
set policy-options policy-statement LP-POLICY term CUSTOMER then local-preference 200
set policy-options policy-statement LP-POLICY term CUSTOMER then accept

set policy-options policy-statement LP-POLICY term PEER from prefix-list PEER
set policy-options policy-statement LP-POLICY term PEER then local-preference 100
set policy-options policy-statement LP-POLICY term PEER then accept

set policy-options policy-statement LP-POLICY term DEFAULT then accept

# Apply to BGP
set protocols bgp group external import LP-POLICY
```

### Example 3: Community-based Route Tagging

**Scenario**: Tag routes with BGP community for downstream filtering

```junos
# Define prefix-lists
set policy-options prefix-list TRANSIT 10.200.0.0/16

# Create export policy with community
set policy-options policy-statement TAG-ROUTES term TRANSIT from prefix-list TRANSIT
set policy-options policy-statement TAG-ROUTES term TRANSIT then community 65000:100
set policy-options policy-statement TAG-ROUTES term TRANSIT then accept

set policy-options policy-statement TAG-ROUTES term DEFAULT then accept

# Apply to BGP
set protocols bgp group upstream export TAG-ROUTES
set protocols bgp group upstream neighbor 10.0.2.1 peer-as 65003
```

---

## Validation

### Pre-deployment Checks

1. **Config Syntax Validation**:
```bash
arca-cli config validate
```

2. **Policy Dry-run** (FRR):
```bash
# Generate FRR config without applying
arca-cli frr generate --dry-run
```

3. **BGP Session Check**:
```bash
vtysh -c "show ip bgp summary"
```

### Post-deployment Verification

1. **Verify policy application**:
```bash
vtysh -c "show ip bgp neighbors 10.0.1.2" | grep -A 5 "route-map"
```

2. **Check route filtering**:
```bash
# Before policy
vtysh -c "show ip bgp"
# After policy (compare route counts)
vtysh -c "show ip bgp summary"
```

3. **Verify route attributes**:
```bash
vtysh -c "show ip bgp 10.100.0.0/16"
# Check local-preference, community, etc.
```

---

## Troubleshooting

### Common Issues

**Issue**: Policy not applied to neighbor

**Symptoms**: Route-map not shown in `show ip bgp neighbors`

**Solution**:
- Verify BGP group has `import`/`export` configured
- Check policy-statement name matches exactly
- Restart FRR: `systemctl restart frr`

---

**Issue**: Routes still being accepted despite deny policy

**Symptoms**: Unwanted routes in BGP RIB

**Solution**:
- Verify prefix-list contains correct prefixes
- Check policy term ordering (most specific first)
- Ensure policy has explicit default term
- Enable BGP debugging: `vtysh -c "debug bgp updates"`

---

**Issue**: IPv6 routes not matched by policy

**Symptoms**: IPv6 routes bypassing policy

**Solution**:
- Check if prefix-list was split (look for `-v6` suffix)
- Verify IPv6 address-family has route-map applied
- Use `show ipv6 bgp` to verify routes

---

**Issue**: AS-path policy not working

**Symptoms**: Routes with specific AS-path not filtered

**Solution**:
- Verify AS-path regex syntax (escape special characters)
- Check AS-path access-list generation: `show bgp as-path-access-list`
- Test regex with sample AS-path

---

## References

- [FRR Route-map Documentation](http://docs.frrouting.org/en/latest/routemap.html)
- [FRR BGP Configuration](http://docs.frrouting.org/en/latest/bgp.html)
- [Junos Policy Options](https://www.juniper.net/documentation/us/en/software/junos/routing-policy/index.html)
- [arca-router SPEC.md](../SPEC.md)
- [arca-router PHASE3-2.md](../PHASE3-2.md)

---

## Version History

- **v0.3.1** (2025-12-27): Initial policy-options guide for Phase 3
  - FRR policy implementation complete
  - VPP PBR documented as Phase 4 future work
