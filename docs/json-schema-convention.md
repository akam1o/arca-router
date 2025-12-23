# JSON Schema Naming Convention

## Overview

arca-router uses **kebab-case** for JSON field names to align with Junos-style configuration naming.

## Examples

### Correct (kebab-case)
```json
{
  "system": {
    "host-name": "router-01"
  },
  "interfaces": {
    "ge-0/0/0": {
      "description": "WAN Uplink"
    }
  }
}
```

### Incorrect (snake_case)
```json
{
  "system": {
    "host_name": "router-01"  // Wrong
  }
}
```

## Implementation

All Go struct JSON tags should use kebab-case:

```go
type SystemConfig struct {
    HostName string `json:"host-name,omitempty"`  // Correct
    // NOT: `json:"host_name,omitempty"`
}
```

## Rationale

- Consistency with Junos CLI naming (`host-name`, not `host_name`)
- Easier mapping between configuration and internal representation
- Better alignment with NETCONF/YANG conventions (Phase 3)
