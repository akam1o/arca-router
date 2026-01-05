# FRR → VPP Route Synchronization Design

**Version**: 1.0
**Date**: 2025-12-25
**Status**: Design Phase
**VPP Version**: 24.10 (Debian Bookworm)
**FRR Version**: ≥ 8.0

---

## Overview

This document describes how routes learned by FRR (via BGP, OSPF, or configured as static routes) are synchronized into VPP's Forwarding Information Base (FIB) for high-performance packet forwarding.

## Background

arca-router uses a split control/data plane architecture:
- **Control Plane**: FRR manages routing protocols (BGP, OSPF) and computes best paths
- **Data Plane**: VPP performs high-performance packet forwarding

For this architecture to work, routes computed by FRR must be programmed into VPP's FIB.

---

## Architecture Comparison

### Method A: linux-cp Route Sync (SELECTED)

**Flow**:
```
FRR (BGP/OSPF) → Linux Kernel FIB → VPP linux-cp plugin → VPP FIB
```

**Advantages**:
- ✅ Simplest integration (no FRR modifications)
- ✅ Leverages VPP built-in linux-cp plugin
- ✅ Standard kernel FIB as integration layer
- ✅ Works with any routing daemon (not just FRR)
- ✅ Kernel routes from other sources also synced

**Disadvantages**:
- ⚠️ Adds kernel as intermediary (small latency)
- ⚠️ Depends on linux-cp route sync feature availability
- ⚠️ Potential scale limits (kernel FIB size)

**Use case**: Phase 2 MVP, general-purpose routing

---

### Method B: FRR VPP Dataplane Plugin

**Flow**:
```
FRR (BGP/OSPF) → FRR VPP plugin → VPP API → VPP FIB
```

**Advantages**:
- ✅ Direct FRR → VPP path (lower latency)
- ✅ Better scalability (bypass kernel limits)
- ✅ More control over route attributes

**Disadvantages**:
- ❌ Requires FRR dataplane plugin development/integration
- ❌ Complex control plane ownership (who owns VPP FIB?)
- ❌ Harder to debug (less visibility into intermediate state)
- ❌ FRR version coupling

**Use case**: Future optimization if Method A hits limits

---

### Method C: Custom Route Mirror

**Flow**:
```
FRR (BGP/OSPF) → Linux Kernel FIB → arca-routerd netlink monitor → VPP API → VPP FIB
```

**Advantages**:
- ✅ Full control over sync logic
- ✅ Can filter/transform routes before VPP

**Disadvantages**:
- ❌ High maintenance burden (reimplement what linux-cp does)
- ❌ Bug-prone (netlink event handling is complex)
- ❌ Performance overhead (userspace monitoring)

**Use case**: Avoid unless Method A is proven insufficient

---

## Selected Method: linux-cp Route Sync

### Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────┐
│  arca-router Configuration                                      │
│  (Junos-like syntax: set protocols bgp neighbor ...)            │
└────────────────────────┬────────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────────┐
│  arca-routerd (Parser & Orchestrator)                           │
│  - Parse config                                                 │
│  - Generate FRR config                                          │
│  - Apply to VPP (interfaces, LCP)                               │
│  - Apply to FRR (frr.conf)                                      │
└────────────────────────┬────────────────────────────────────────┘
                         │
         ┌───────────────┴───────────────┐
         │                               │
         ▼                               ▼
┌─────────────────────┐         ┌────────────────────┐
│  VPP                │         │  FRR               │
│  - AVF/RDMA ifaces  │         │  - BGP daemon      │
│  - LCP plugin       │◄────────┤  - OSPF daemon     │
│  - FIB (dataplane)  │ LCP     │  - zebra           │
└─────────────────────┘ pairs   └────────┬───────────┘
         ▲                               │
         │                               │ netlink
         │ linux-cp                      ▼
         │ route sync          ┌─────────────────────┐
         └─────────────────────┤  Linux Kernel FIB   │
                               │  (routing table)    │
                               └─────────────────────┘
```

### Data Flow

1. **FRR learns routes** (BGP, OSPF, static):
   - BGP neighbor advertises prefix 10.1.0.0/24 via next-hop 192.0.2.254
   - FRR zebra computes best path

2. **FRR writes to kernel FIB**:
   - zebra uses netlink `RTM_NEWROUTE` to install route in kernel
   - Kernel routing table entry: `10.1.0.0/24 via 192.0.2.254 dev ge000`

3. **linux-cp monitors kernel FIB**:
   - VPP linux-cp plugin subscribes to netlink route events
   - Receives `RTM_NEWROUTE` notification

4. **linux-cp mirrors to VPP FIB**:
   - Translates Linux interface name (`ge000`) to VPP sw_if_index
   - Calls VPP `ip_route_add_del` API
   - VPP FIB entry created: `10.1.0.0/24 via 192.0.2.254 sw_if_index=1`

5. **VPP forwards packets**:
   - Incoming packet to 10.1.0.1
   - VPP FIB lookup → matches 10.1.0.0/24
   - Forwards via sw_if_index=1 (AVF interface)

---

## VPP Configuration

### startup.conf

Location: `/etc/vpp/startup.conf`

**Required settings**:
```
plugins {
  # Enable linux-cp plugin
  plugin linux_cp_plugin.so {
    enable
  }
}

# Optional: Configure route sync (syntax may vary by VPP build)
linux-cp {
  # Enable automatic route synchronization from kernel to VPP
  # Note: Not all VPP 24.10 builds expose this via startup.conf
  # May need runtime CLI configuration instead
}
```

### Runtime Configuration (if startup.conf not available)

Check VPP documentation or source for exact CLI commands:
```bash
vppctl set lcp route-sync on
```

### Verification

Check if route sync is working:
```bash
# Add test route in kernel
ip route add 198.51.100.0/24 via 192.0.2.254 dev ge000

# Check VPP FIB (should appear within ~1 second)
vppctl show ip fib 198.51.100.0/24

# Expected output:
# 198.51.100.0/24
#   via 192.0.2.254 ge000
```

---

## FRR Configuration

FRR doesn't need special configuration for linux-cp integration. Standard FRR setup:

### /etc/frr/daemons

```
bgpd=yes
ospfd=yes
zebra=yes
staticd=yes
```

### zebra Configuration

File: `/etc/frr/frr.conf`

```
# Standard zebra config (no VPP-specific settings needed)
!
interface ge000
 ip address 192.0.2.1/24
!
router bgp 65000
 neighbor 192.0.2.254 remote-as 65001
 !
 address-family ipv4 unicast
  network 10.0.0.0/24
 exit-address-family
!
```

zebra will automatically write learned routes to kernel using netlink, and linux-cp will pick them up.

---

## Route Directionality

### Kernel → VPP (Primary Flow)

- **Enabled**: linux-cp route sync
- **Purpose**: FRR-learned routes go to VPP for forwarding
- **Configuration**: VPP startup.conf or runtime CLI

### VPP → Kernel (Usually Disabled)

- **Purpose**: VPP-learned routes go to kernel
- **Risk**: Can cause routing loops if both directions enabled
- **Recommendation**: Keep disabled unless specific use case

### Best Practice

- **One-way sync only**: Kernel → VPP
- Treat kernel as "control plane FIB"
- Treat VPP as "dataplane FIB"

---

## Route Types

### BGP Routes

**Example**: BGP neighbor advertises 10.1.0.0/24 via 192.0.2.254

**FRR**:
```
router bgp 65000
 neighbor 192.0.2.254 remote-as 65001
 address-family ipv4 unicast
  neighbor 192.0.2.254 route-map IMPORT in
 exit-address-family
```

**Kernel FIB**:
```
10.1.0.0/24 via 192.0.2.254 dev ge000 proto bgp metric 20
```

**VPP FIB**:
```
10.1.0.0/24 via 192.0.2.254 sw_if_index=1
```

### OSPF Routes

**Example**: OSPF neighbor advertises 10.2.0.0/24 via 192.0.2.253

**FRR**:
```
router ospf
 network 192.0.2.0/24 area 0.0.0.0
```

**Kernel FIB**:
```
10.2.0.0/24 via 192.0.2.253 dev ge000 proto ospf metric 10
```

**VPP FIB**:
```
10.2.0.0/24 via 192.0.2.253 sw_if_index=1
```

### Static Routes

**Example**: Static route to 10.3.0.0/24 via 192.0.2.1

**FRR**:
```
ip route 10.3.0.0/24 192.0.2.1
```

**Kernel FIB**:
```
10.3.0.0/24 via 192.0.2.1 dev ge000 proto static
```

**VPP FIB**:
```
10.3.0.0/24 via 192.0.2.1 sw_if_index=1
```

---

## Convergence Time

### Route Addition

1. FRR learns route: ~100ms (depends on protocol)
2. FRR → kernel: ~1ms (netlink write)
3. linux-cp → VPP: ~1ms (VPP API call)
4. **Total**: ~100ms (dominated by routing protocol convergence)

### Route Withdrawal

Similar timing to route addition.

### Tuning

- FRR timers can be adjusted (BGP keepalive, OSPF hello)
- linux-cp polling interval (if not event-driven)
- VPP FIB update batching (usually not configurable)

---

## Troubleshooting

### Routes in FRR but not in VPP

**Symptoms**:
```bash
vtysh -c "show ip route"
# Shows route 10.1.0.0/24

vppctl show ip fib 10.1.0.0
# No matching route
```

**Diagnosis**:
1. Check kernel FIB:
   ```bash
   ip route show 10.1.0.0/24
   ```
   - If missing → FRR zebra not writing to kernel (check zebra logs)
   - If present → linux-cp not syncing (check VPP logs)

2. Check linux-cp plugin status:
   ```bash
   vppctl show plugins | grep linux_cp
   # Should show "loaded"
   ```

3. Check LCP pairs exist:
   ```bash
   vppctl lcp show
   # Should show ge000 paired to sw_if_index=1
   ```

4. Check VPP logs:
   ```bash
   grep -i "linux-cp" /var/log/vpp/vpp.log
   ```

**Resolution**:
- Ensure linux-cp plugin loaded
- Verify route sync enabled
- Check interface exists in both kernel and VPP

---

### Routes in VPP but packets not forwarded

**Symptoms**:
```bash
vppctl show ip fib 10.1.0.0
# Route exists

# But ping fails
ping -I ge000 10.1.0.1
# No response
```

**Diagnosis**:
1. Check VPP interface state:
   ```bash
   vppctl show interface ge000
   # Should be admin-up and link-up
   ```

2. Check ARP resolution:
   ```bash
   vppctl show ip neighbors
   # Should show next-hop 192.0.2.254 resolved
   ```

3. Trace packets:
   ```bash
   vppctl trace add avf-input 10
   ping -c 1 -I ge000 10.1.0.1
   vppctl show trace
   ```

**Resolution**:
- Bring VPP interface up
- Trigger ARP resolution (`ping 192.0.2.254`)
- Check physical link connectivity

---

### Route Flapping

**Symptoms**:
- Routes appear/disappear in VPP FIB rapidly

**Diagnosis**:
```bash
vppctl show logging
# Look for repeated route add/delete messages
```

**Possible causes**:
- FRR routing protocol instability (check BGP/OSPF neighbors)
- Kernel route installation failures (check kernel logs)
- linux-cp bug (check VPP version)

**Resolution**:
- Stabilize routing protocol (tune timers, fix flapping links)
- Check for kernel route table overflow
- Upgrade VPP if known linux-cp bugs

---

## Performance Considerations

### Scalability

**Kernel FIB limits**:
- Typical Linux systems: ~1M routes
- If exceeding: Consider Method B (FRR VPP plugin) in future

**VPP FIB limits**:
- Depends on memory configuration
- VPP can handle millions of routes efficiently

### Memory Usage

linux-cp keeps state for:
- LCP interface pairs (~1KB per pair)
- Route sync state (minimal, event-driven)

Typical memory overhead: <10 MB for hundreds of interfaces

### CPU Usage

linux-cp route sync:
- Event-driven (netlink notifications)
- CPU usage only during route updates
- Negligible during steady state

---

## Testing Plan

### Unit Tests

Not directly testable (requires VPP plugin), but can mock:
- Netlink route events
- VPP FIB API calls

### Integration Tests

File: `test/integration_test.sh`

```bash
test_frr_vpp_route_sync() {
    # 1. Start VPP with linux-cp enabled
    # 2. Create LCP interface ge000
    # 3. Start FRR with zebra + bgpd
    # 4. Configure BGP neighbor
    # 5. Advertise route from neighbor (use exabgp or GoBGP)
    # 6. Wait for convergence (5 seconds)
    # 7. Verify route in FRR: vtysh -c "show ip bgp"
    # 8. Verify route in kernel: ip route show 10.1.0.0/24
    # 9. Verify route in VPP: vppctl show ip fib 10.1.0.0
    # 10. All three should match
}

test_route_withdrawal() {
    # Similar to above, but withdraw route and verify removal
}

test_static_route_sync() {
    # Add static route via FRR
    # Verify appears in VPP
}
```

---

## Migration Path (Future)

If Method A (linux-cp) proves insufficient, migration to Method B (FRR VPP plugin):

1. Develop/integrate FRR VPP dataplane plugin
2. Configure FRR to use VPP plugin instead of kernel
3. Disable linux-cp route sync (keep interface sync only)
4. Test thoroughly (different control plane semantics)

**Estimated effort**: 3-4 weeks development + testing

**Decision point**: Monitor in production:
- Route scale (>100K routes?)
- Convergence time (>1 second?)
- Kernel FIB issues?

If no issues, stay with Method A (simpler is better).

---

## Security Considerations

### Route Injection Attacks

**Risk**: Malicious routes injected into kernel could reach VPP FIB

**Mitigation**:
- linux-cp syncs only from kernel, not external sources
- Kernel netlink requires CAP_NET_ADMIN (root)
- arca-routerd controls FRR config (only authorized routes)

### Route Validation

**Current**: No route validation in linux-cp (trusts kernel)

**Future enhancement** (if needed):
- Add route filtering in VPP (reject routes outside allowed prefixes)
- Log suspicious routes (e.g., default route changes)

---

## References

- VPP linux-cp plugin: https://fd.io/docs/vpp/v24.10/
- FRR zebra documentation: https://docs.frrouting.org/en/latest/zebra.html
- Linux netlink programming guide
- [lcp-design.md](lcp-design.md) - LCP interface design
- [PHASE2.md](../PHASE2.md) - Task 2.3 requirements

---

## Changelog

- **2025-12-25**: Initial design
  - Method A (linux-cp route sync) selected
  - Configuration requirements documented
  - Troubleshooting guide added
  - Testing plan outlined
