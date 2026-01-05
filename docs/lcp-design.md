# LCP (Linux Control Plane) Design

**Version**: 1.0
**Date**: 2025-12-25
**Status**: Design Phase
**VPP Version**: 24.10 (Debian Bookworm)
**govpp Version**: v0.13.0

---

## Overview

This document describes the design and implementation strategy for integrating VPP's Linux Control Plane (LCP) into arca-router. LCP enables VPP interfaces to be visible in the Linux kernel, allowing FRR to manage dynamic routing protocols (BGP, OSPF) while leveraging VPP's high-performance dataplane.

## Goals

1. Make VPP interfaces (AVF/RDMA) visible as Linux network interfaces
2. Enable FRR to manage routing protocols on VPP-backed interfaces
3. Synchronize routes learned by FRR into VPP's FIB
4. Maintain stable interface name mapping between Junos config, Linux, and VPP
5. Provide seamless integration with existing arca-router configuration flow

---

## Architecture Decision

### Method A: LCP Pair on Existing VPP Interface (RECOMMENDED)

**Flow**:
1. Create AVF/RDMA interface in VPP → obtain `sw_if_index`
2. Call `lcp_itf_pair_add_del` with `sw_if_index` and Linux interface name
3. LCP plugin automatically creates a TAP interface in Linux kernel
4. FRR sees the Linux interface and can configure routing protocols
5. Routes learned by FRR → Linux kernel FIB → LCP route sync → VPP FIB

**Advantages**:
- Single source of truth: VPP interface lifecycle managed by VPP
- TAP creation is automatic (no explicit TAP API calls needed)
- Aligns with VPP 24.10 recommended usage
- Simpler error handling and state management

**Implementation**:
```
VPP Interface (AVF: ge-0/0/0)
    ↓ sw_if_index=1
lcp_itf_pair_add_del(sw_if_index=1, linux_if_name="ge000", host_if_type=TAP)
    ↓
Linux Kernel TAP Interface (ge000)
    ↓
FRR sees "ge000" and configures BGP/OSPF
    ↓
Routes → Linux FIB → LCP sync → VPP FIB
```

### Method B: Explicit TAP + LCP Pair (NOT RECOMMENDED)

**Why not Method B**:
- Adds unnecessary complexity (two API calls instead of one)
- Requires managing TAP lifecycle separately from VPP interface
- More potential for state inconsistencies
- Only needed for Linux-only interfaces (not our use case)

**Decision**: **Use Method A** for all AVF/RDMA interfaces.

---

## Interface Name Mapping

### Challenge

- **Junos format**: `ge-0/0/0`, `xe-1/2/3` (flexible, descriptive)
- **Linux limit**: 15 characters (IFNAMSIZ-1)
- **Requirements**:
  - Stable (same mapping across restarts)
  - Reversible (need to map back to Junos name)
  - Collision-free

### Mapping Strategy

#### Basic Mapping Rules

| Junos Format | Linux Format | Length | Notes |
|-------------|--------------|--------|-------|
| `ge-0/0/0` | `ge0-0-0` | 8 chars | Preserve FPC/PIC/port separation |
| `xe-1/2/3` | `xe1-2-3` | 8 chars | Avoid ambiguity (ge-1/11/1 ≠ ge-11/1/1) |
| `et-0/1/2` | `et0-1-2` | 8 chars | 100GbE interfaces |
| `ge-0/0/10` | `ge0-0-10` | 9 chars | Two-digit port |
| `ge-1/11/1` | `ge1-11-1` | 9 chars | Distinct from ge-11/1/1 |
| `ge-11/1/1` | `ge11-1-1` | 9 chars | Distinct from ge-1/11/1 |

#### Subinterface Mapping

| Junos Format | Linux Format | Length | Notes |
|-------------|--------------|--------|-------|
| `ge-0/0/0.10` | `ge0-0-0v10` | 11 chars | Use 'v' for VLAN |
| `ge-0/0/0.100` | `ge0-0-0v100` | 12 chars | Three-digit VLAN |

#### Collision Handling

For cases where basic mapping would exceed 15 chars:
1. Generate a deterministic hash (base64-url encoding) of the full Junos name
2. Append hash suffix: `ge10uwy5t` (type prefix + 5-char hash)
3. Store mapping in persistent state file

**Note**: The separator-based format (`ge0-0-0`) prevents ambiguity without needing collision detection for most cases.

#### Mapping Function Signature

```go
// ConvertJunosToLinuxName converts a Junos interface name to Linux format
// Returns the Linux name (max 15 chars) and an error if conversion fails
func ConvertJunosToLinuxName(junosName string) (string, error)

// ConvertLinuxToJunosName reverse-maps a Linux name to Junos format
// Requires the mapping table to be loaded from persistent state
func ConvertLinuxToJunosName(linuxName string) (string, error)
```

#### Persistent Mapping Storage

Location: `/var/lib/arca-router/interface-mapping.json`

Format:
```json
{
  "version": "1.0",
  "mappings": {
    "ge-0/0/0": {
      "linux_name": "ge000",
      "vpp_sw_if_index": 1,
      "created_at": "2025-12-25T10:00:00Z"
    },
    "xe-1/2/3": {
      "linux_name": "xe123",
      "vpp_sw_if_index": 2,
      "created_at": "2025-12-25T10:01:00Z"
    }
  }
}
```

**Recovery on restart**:
1. Load mapping from JSON file
2. Query VPP for existing interfaces
3. Query Linux netlink for existing interfaces
4. Reconcile state (recreate missing LCP pairs if needed)

---

## VPP Configuration Requirements

### startup.conf

Location: `/etc/vpp/startup.conf`

**Required configuration**:
```
plugins {
  plugin linux_cp_plugin.so {
    enable
  }
}

# Optional: Configure LCP route sync (if available in VPP 24.10)
linux-cp {
  # Enable automatic route sync from Linux kernel to VPP
  # Exact syntax may vary; verify with VPP documentation
}
```

**Verification**:
```bash
# Check if linux-cp plugin is loaded
vppctl show plugins | grep linux_cp

# Check LCP interface pairs
vppctl lcp show
```

### Required Capabilities

The `arca-routerd` process requires:
- **CAP_NET_ADMIN**: For netlink operations and LCP pair creation

Set in systemd unit file:
```ini
[Service]
AmbientCapabilities=CAP_NET_ADMIN
```

---

## API Usage

### LCP API Versions

VPP 24.10 provides three versions of the LCP pair API:
- `lcp_itf_pair_add_del` (deprecated, but available)
- `lcp_itf_pair_add_del_v2` (in-progress)
- `lcp_itf_pair_add_del_v3` (latest)

**Decision**: Use **`lcp_itf_pair_add_del_v2`** for VPP 24.10 (most stable in current release).

### API Structure

```go
type LcpItfPairAddDelV2 struct {
    IsAdd      bool                           // true=create, false=delete
    SwIfIndex  interface_types.InterfaceIndex // VPP interface index
    HostIfName string                         // Linux interface name (max 16 chars)
    HostIfType LcpItfHostType                 // TAP or TUN (use TAP)
    Netns      string                         // Network namespace (empty=default)
}
```

### Create LCP Pair

```go
// After creating VPP interface (AVF/RDMA)
vppIf, err := vppClient.CreateInterface(ctx, &vpp.CreateInterfaceRequest{
    Type:           vpp.InterfaceTypeAVF,
    DeviceInstance: "0000:01:00.0",
})
if err != nil {
    return err
}

// Convert Junos name to Linux name
linuxName, err := lcp.ConvertJunosToLinuxName("ge-0/0/0")
if err != nil {
    return err
}

// Create LCP pair
err = vppClient.CreateLCPInterface(ctx, vppIf.SwIfIndex, linuxName)
if err != nil {
    return err
}

// Verify in Linux
exec.Command("ip", "link", "show", linuxName).Run()
```

### Delete LCP Pair

```go
err = vppClient.DeleteLCPInterface(ctx, vppIf.SwIfIndex)
```

---

## FRR → VPP Route Synchronization

### Method A: linux-cp Route Sync (RECOMMENDED)

**Architecture**:
```
FRR (BGP/OSPF) → Linux Kernel FIB → linux-cp route sync → VPP FIB
```

**Advantages**:
- Simplest integration
- Leverages existing VPP plugin functionality
- FRR doesn't need to know about VPP
- Standard kernel FIB as "handoff layer"

**Configuration**:
1. Enable linux-cp route sync in `/etc/vpp/startup.conf` (if available)
2. FRR writes routes to kernel using standard netlink
3. linux-cp plugin mirrors kernel routes to VPP FIB automatically

**Verification**:
```bash
# Check FRR routes
vtysh -c "show ip route"

# Check Linux kernel FIB
ip route show

# Check VPP FIB
vppctl show ip fib

# All three should be synchronized
```

### Method B: FRR VPP Dataplane Plugin (Future)

**Why not now**:
- Higher implementation complexity
- Requires FRR to directly integrate with VPP API
- Control plane ownership becomes complex
- Good for scale/performance optimization later

**Decision**: **Use Method A (linux-cp route sync)** for Phase 2.

---

## Integration Points

### 1. Client Interface Extension

File: `pkg/vpp/client.go`

```go
type Client interface {
    // ... existing methods ...

    // CreateLCPInterface creates an LCP pair for an existing VPP interface
    CreateLCPInterface(ctx context.Context, ifIndex uint32, linuxIfName string) error

    // DeleteLCPInterface removes an LCP pair
    DeleteLCPInterface(ctx context.Context, ifIndex uint32) error

    // GetLCPInterface retrieves LCP pair information
    GetLCPInterface(ctx context.Context, ifIndex uint32) (*LCPInterface, error)

    // ListLCPInterfaces lists all LCP pairs
    ListLCPInterfaces(ctx context.Context) ([]*LCPInterface, error)
}

type LCPInterface struct {
    VPPSwIfIndex uint32 // VPP interface index
    LinuxIfName  string // Linux interface name
    JunosName    string // Original Junos config name (for reference)
    HostIfType   string // "tap" or "tun"
    Netns        string // Network namespace (empty=default)
}
```

### 2. Configuration Flow Update

File: `cmd/arca-routerd/apply.go`

```go
func applyVPPConfig(cfg *config.Config, vppClient vpp.Client, log *logger.Logger) error {
    // Step 6.1a: Create VPP interface (existing)
    vppIf, err := vppClient.CreateInterface(ctx, &vpp.CreateInterfaceRequest{...})

    // Step 6.1b: Create LCP pair (NEW)
    linuxName, err := lcp.ConvertJunosToLinuxName(cfg.Interfaces[i].Name)
    if err != nil {
        return fmt.Errorf("interface name mapping failed: %w", err)
    }

    err = vppClient.CreateLCPInterface(ctx, vppIf.SwIfIndex, linuxName)
    if err != nil {
        // Rollback: delete VPP interface
        vppClient.DeleteInterface(ctx, vppIf.SwIfIndex)
        return fmt.Errorf("LCP pair creation failed: %w", err)
    }

    // Step 6.2: Set IP addresses (existing)
    err = vppClient.SetInterfaceAddress(ctx, vppIf.SwIfIndex, addr)

    // Continue with remaining config...
}
```

### 3. State Management

File: `pkg/vpp/lcp_state.go`

```go
type LCPStateManager struct {
    mappingFile string
    mappings    map[string]*InterfaceMapping
    mu          sync.RWMutex
}

type InterfaceMapping struct {
    JunosName    string
    LinuxName    string
    VPPSwIfIndex uint32
    CreatedAt    time.Time
}

func (m *LCPStateManager) Save() error
func (m *LCPStateManager) Load() error
func (m *LCPStateManager) AddMapping(junosName, linuxName string, swIfIndex uint32) error
func (m *LCPStateManager) GetByJunosName(junosName string) (*InterfaceMapping, error)
func (m *LCPStateManager) GetByLinuxName(linuxName string) (*InterfaceMapping, error)
func (m *LCPStateManager) ReconcileState(ctx context.Context, vppClient vpp.Client) error
```

---

## Error Handling

### Common Errors and Mitigation

| Error | Cause | Mitigation |
|-------|-------|------------|
| `operation not permitted` | Missing CAP_NET_ADMIN | Add capability to systemd unit |
| `name too long` | Linux name >15 chars | Use hash suffix in mapping |
| `interface already exists` | Stale Linux interface | Check/cleanup before create |
| `VPP interface not found` | Invalid sw_if_index | Verify VPP interface exists first |
| `linux-cp plugin not loaded` | Missing plugin config | Check startup.conf |

### Rollback Strategy

On LCP creation failure:
1. Delete VPP interface (if just created)
2. Remove mapping from state file
3. Log detailed error with VPP sw_if_index and Linux name
4. Return error to caller (don't continue with broken state)

---

## PoC Implementation Plan

### PoC Goals

1. Create one AVF interface in VPP
2. Create LCP pair for that interface
3. Verify Linux interface appears with correct name
4. Set IP address on VPP interface
5. Verify IP address visible in Linux
6. Demonstrate FRR can see the interface

### PoC Steps

File: `test/lcp_poc/main.go`

```go
// 1. Connect to VPP
vppClient := govpp.NewClient()
err := vppClient.Connect(ctx)

// 2. Create AVF interface
vppIf, err := vppClient.CreateInterface(ctx, &vpp.CreateInterfaceRequest{
    Type:           vpp.InterfaceTypeAVF,
    DeviceInstance: "0000:01:00.0", // Adjust to your PCI address
})

// 3. Create LCP pair
linuxName := "ge000"
err = vppClient.CreateLCPInterface(ctx, vppIf.SwIfIndex, linuxName)

// 4. Set IP address in VPP
_, ipNet, _ := net.ParseCIDR("192.0.2.1/24")
err = vppClient.SetInterfaceAddress(ctx, vppIf.SwIfIndex, ipNet)

// 5. Bring interface up
err = vppClient.SetInterfaceUp(ctx, vppIf.SwIfIndex)

// 6. Verify in Linux
cmd := exec.Command("ip", "addr", "show", linuxName)
output, err := cmd.CombinedOutput()
fmt.Printf("Linux interface:\n%s\n", output)

// 7. Verify FRR can see it
cmd = exec.Command("vtysh", "-c", "show interface "+linuxName)
output, err = cmd.CombinedOutput()
fmt.Printf("FRR interface:\n%s\n", output)
```

### PoC Success Criteria

- [ ] VPP interface created successfully
- [ ] LCP pair created without errors
- [ ] Linux interface `ge000` appears in `ip link show`
- [ ] IP address 192.0.2.1/24 visible in `ip addr show ge000`
- [ ] FRR can query interface via `show interface ge000`
- [ ] No errors in VPP logs or FRR logs

---

## Testing Strategy

### Unit Tests

File: `pkg/vpp/lcp_naming_test.go`

```go
func TestConvertJunosToLinuxName(t *testing.T)
func TestConvertLinuxToJunosName(t *testing.T)
func TestNameCollisionHandling(t *testing.T)
func TestSubinterfaceMapping(t *testing.T)
```

File: `pkg/vpp/govpp_client_test.go` (extend existing)

```go
func TestCreateLCPInterface(t *testing.T)
func TestDeleteLCPInterface(t *testing.T)
func TestLCPInterfaceErrors(t *testing.T)
```

### Integration Tests

File: `test/integration_test.sh`

```bash
# Test LCP creation with real VPP
test_lcp_creation() {
    # Create config with one interface
    # Apply config
    # Verify VPP interface exists
    # Verify Linux interface exists
    # Verify mapping file created
}

# Test LCP with FRR
test_lcp_frr_integration() {
    # Create LCP interface
    # Configure FRR BGP on that interface
    # Verify FRR can send/receive packets
}
```

---

## Security Considerations

1. **Capabilities**: Require CAP_NET_ADMIN (documented in [docs/security-model.md](security-model.md))
2. **Mapping file permissions**: `/var/lib/arca-router/interface-mapping.json` → root:arca-router 0640
3. **VPP socket permissions**: Ensure arca-router user in `vpp` group
4. **FRR config permissions**: `/etc/frr/frr.conf` → root:frr 0640 (writable by arca-router)

---

## Common Gotchas

### 1. Admin State Propagation

**Issue**: Setting interface admin-up in VPP doesn't automatically bring up Linux side.

**Solution**: Set admin-up on both sides:
```go
vppClient.SetInterfaceUp(ctx, swIfIndex)
exec.Command("ip", "link", "set", linuxName, "up").Run()
```

### 2. MTU Mismatches

**Issue**: VPP default MTU (9000) ≠ Linux default MTU (1500).

**Solution**: Set consistent MTU during LCP creation:
```go
// Set VPP MTU first
vppClient.SetInterfaceMTU(ctx, swIfIndex, 1500)
// Then create LCP pair
```

### 3. Stale Linux Interfaces After VPP Restart

**Issue**: VPP restart leaves stale TAP interfaces in Linux.

**Solution**: Reconciliation on startup:
```go
func ReconcileState(ctx context.Context) error {
    // Load expected state from mapping file
    // Query Linux netlink for existing interfaces
    // Delete stale interfaces
    // Recreate missing LCP pairs
}
```

### 4. Interface Name Truncation

**Issue**: Linux silently truncates names >15 chars without error.

**Solution**: Validate length before calling LCP API:
```go
if len(linuxName) > 15 {
    return fmt.Errorf("Linux interface name too long: %s (max 15 chars)", linuxName)
}
```

---

## Next Steps (Implementation Tasks)

### Task 2.1: LCP Interface Definition Extension
- Extend `pkg/vpp/client.go` with LCP methods
- Define `LCPInterface` struct in `pkg/vpp/types.go`

### Task 2.2: govpp LCP API Implementation
- Implement `CreateLCPInterface()` in `pkg/vpp/govpp_client.go`
- Implement `DeleteLCPInterface()`
- Implement `GetLCPInterface()` and `ListLCPInterfaces()`

### Task 2.3: FRR→VPP Route Sync Design
- Document linux-cp route sync configuration
- Create `docs/frr-vpp-route-sync.md`

### Task 2.4: Configuration Flow Integration
- Update `cmd/arca-routerd/apply.go` to create LCP pairs
- Add rollback on failure

### Task 2.5: Interface Name Conversion Logic
- Implement `pkg/vpp/lcp_naming.go`
- Add collision detection
- Write comprehensive tests

### Task 2.6: LCP State Management
- Implement `pkg/vpp/lcp_state.go`
- Add persistent mapping storage
- Implement reconciliation logic

### Task 2.7: Unit Tests
- Write tests for all LCP functions
- Achieve >70% coverage

---

## References

- VPP Linux-CP plugin documentation: https://fd.io/docs/vpp/v24.10/
- govpp v0.13.0 documentation: https://pkg.go.dev/go.fd.io/govpp@v0.13.0
- [security-model.md](security-model.md) - Permissions and capabilities
- [govpp-compatibility.md](govpp-compatibility.md) - VPP/govpp version compatibility

---

## Changelog

- **2025-12-25**: Initial design
  - Method A (LCP pair on existing interface) selected
  - Interface naming strategy defined
  - Route sync via linux-cp chosen
  - PoC implementation plan created
