# Configuration Precedence Rules

**Version**: 1.0.0
**Date**: 2025-12-25
**Status**: Phase 2 Implementation

---

## Overview

This document defines the configuration precedence rules for arca-router when multiple configuration hierarchies provide overlapping settings.

---

## Router ID Precedence

Router ID can be configured at multiple levels:

1. **`protocols ospf router-id`** - OSPF-specific router ID
2. **`routing-options router-id`** - Global router ID

### Precedence Rule

**`protocols ospf router-id` > `routing-options router-id`**

- If `protocols ospf router-id` is set, it is used for OSPF regardless of `routing-options router-id`
- If `protocols ospf router-id` is NOT set, the global `routing-options router-id` is used for OSPF
- If both are unset, configuration validation fails with an error

### Example 1: OSPF-specific router-id takes precedence

```
set routing-options router-id 10.0.1.1
set protocols ospf router-id 10.0.1.2
```

**Result**: OSPF uses `10.0.1.2`

### Example 2: Fallback to global router-id

```
set routing-options router-id 10.0.1.1
set protocols ospf area 0.0.0.0 interface ge-0/0/0
```

**Result**: OSPF uses `10.0.1.1`

### Example 3: Error - no router-id configured

```
set protocols ospf area 0.0.0.0 interface ge-0/0/0
```

**Result**: Validation error - "OSPF configured but no router-id set"

---

## Rationale

This precedence model follows Junos OS convention:
- Protocol-specific settings override global settings
- Allows flexibility to use different router IDs per protocol if needed
- Ensures clear error messages when required configuration is missing

---

## Implementation

**Validation Location**: `pkg/config/validate.go:Validate()` (OSPFConfig)

```go
// Check for router-id (from OSPF config or routing-options)
routerID := ospf.RouterID
if routerID == "" && cfg.RoutingOptions != nil {
    routerID = cfg.RoutingOptions.RouterID
}

if routerID == "" {
    return errors.New(...) // OSPF configured but no router-id set
}
```

---

## Future Extensions

### Potential Additional Precedence Rules (Phase 3+)

- **Import/Export Policy**: Group-level vs neighbor-level
- **BGP Local AS**: Global vs group-level vs neighbor-level
- **OSPF Metric**: Interface-level vs area-level defaults
- **Static Route Distance**: Route-specific vs global default

These are NOT implemented in Phase 2 and are reserved for future phases.

---

## References

- **PHASE2.md**: Task 3.2 - router-id優先順位ルール策定
- **Junos OS Documentation**: Routing Options Hierarchy
- **Implementation**: `pkg/config/validate.go`, `pkg/config/parser.go`
