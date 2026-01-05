# RBAC (Role-Based Access Control) Guide

## Overview

arca-router implements Role-Based Access Control (RBAC) for the NETCONF interface. This document describes the three built-in roles, their permissions, and how to manage user roles.

## Roles

### 1. **read-only** Role

The `read-only` role provides view-only access to the configuration and operational data.

**Allowed Operations:**
- `get-config` - Retrieve configuration data
- `get` - Retrieve operational and configuration data

**Denied Operations:**
- All configuration modification operations
- Lock/unlock operations
- Session management operations

**Use Cases:**
- Monitoring tools
- Read-only dashboard access
- Auditors who need to view configuration without modification rights
- Junior operators learning the system

**Example:** A monitoring system that periodically queries routing table status.

---

### 2. **operator** Role

The `operator` role provides full configuration management capabilities except for administrative functions.

**Allowed Operations:**
- `get-config` - Retrieve configuration data
- `get` - Retrieve operational and configuration data
- `lock` - Lock configuration datastore
- `unlock` - Unlock configuration datastore
- `edit-config` - Modify configuration
- `validate` - Validate configuration
- `commit` - Commit configuration changes
- `discard-changes` - Discard uncommitted changes
- `copy-config` - Copy configuration between datastores
- `delete-config` - Delete configuration datastore
- `close-session` - Close own NETCONF session

**Denied Operations:**
- `kill-session` - Kill another user's session (admin only)

**Use Cases:**
- Network operators who manage day-to-day configuration
- Automation systems that deploy configuration changes
- DevOps engineers managing network infrastructure
- On-call engineers who need to fix issues

**Example:** A CI/CD pipeline that deploys BGP configuration changes.

---

### 3. **admin** Role

The `admin` role provides full access to all NETCONF operations, including administrative functions.

**Allowed Operations:**
- All operations available to `operator` role, PLUS:
- `kill-session` - Forcibly terminate another user's NETCONF session

**Use Cases:**
- System administrators
- Senior network engineers
- Emergency break-glass accounts
- User management operations

**Example:** A system administrator who needs to forcibly disconnect a stuck NETCONF session.

---

## Permission Matrix

| Operation        | read-only | operator | admin |
|------------------|-----------|----------|-------|
| get-config       | ✅        | ✅       | ✅    |
| get              | ✅        | ✅       | ✅    |
| lock             | ❌        | ✅       | ✅    |
| unlock           | ❌        | ✅       | ✅    |
| edit-config      | ❌        | ✅       | ✅    |
| validate         | ❌        | ✅       | ✅    |
| commit           | ❌        | ✅       | ✅    |
| discard-changes  | ❌        | ✅       | ✅    |
| copy-config      | ❌        | ✅       | ✅    |
| delete-config    | ❌        | ✅       | ✅    |
| close-session    | ❌        | ✅       | ✅    |
| kill-session     | ❌        | ❌       | ✅    |

**Total Operations:**
- read-only: 2 operations
- operator: 11 operations
- admin: 12 operations

---

## User Management

### Creating Users with Roles

Users are created with a specific role assigned at creation time. The role determines what NETCONF operations the user can perform.

**Role Assignment:**
```go
// Create admin user
userDB.CreateUser("alice", passwordHash, netconf.RoleAdmin)

// Create operator user
userDB.CreateUser("bob", passwordHash, netconf.RoleOperator)

// Create read-only user
userDB.CreateUser("charlie", passwordHash, netconf.RoleReadOnly)
```

### Changing User Roles

User roles can be updated using the `UpdateUser` method:

```go
// Promote user to operator
userDB.UpdateUser("charlie", "", netconf.RoleOperator, true)

// Demote user to read-only
userDB.UpdateUser("bob", "", netconf.RoleReadOnly, true)
```

### Role Validation

The database enforces role validation at the schema level:

```sql
role TEXT NOT NULL CHECK(role IN ('admin', 'operator', 'read-only'))
```

Attempting to create or update a user with an invalid role will result in an error.

---

## Authentication and Authorization Flow

```
1. User connects via SSH
   ↓
2. Authentication (password or public key)
   ↓
3. Role extracted from User record
   ↓
4. SSH Permissions object populated with role
   ↓
5. NETCONF session created with role
   ↓
6. User sends NETCONF RPC request
   ↓
7. Server checks RBAC: checkRBAC(session.Role, operation)
   ↓
8. If denied: Return <rpc-error> with error-tag="access-denied"
   ↓
9. If allowed: Execute operation
```

---

## Access Denied Errors

When a user attempts an operation they're not authorized for, they receive an RFC 6241-compliant error response:

```xml
<rpc-reply message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <rpc-error>
    <error-type>protocol</error-type>
    <error-tag>access-denied</error-tag>
    <error-severity>error</error-severity>
    <error-message>access denied: read-only role cannot perform this operation</error-message>
    <error-path>/rpc/commit</error-path>
    <error-info>
      <error-app-tag>rbac-deny</error-app-tag>
    </error-info>
  </rpc-error>
</rpc-reply>
```

**Key Fields:**
- `error-type`: Always "protocol"
- `error-tag`: Always "access-denied"
- `error-app-tag`: Always "rbac-deny" (for programmatic detection)
- `error-message`: Human-readable explanation
- `error-path`: The RPC operation that was denied

---

## Example Scenarios

### Scenario 1: Read-Only Monitoring

**Goal:** Set up a monitoring system with read-only access.

```bash
# Create read-only user
arca-cli user create monitor --role read-only

# Add public key for automated access
arca-cli user add-key monitor ~/.ssh/monitor_key.pub

# Monitoring system can now:
# - Retrieve configuration (get-config)
# - Query operational data (get)
# But CANNOT:
# - Modify configuration
# - Lock/unlock datastores
# - Manage sessions
```

### Scenario 2: Operator for CI/CD

**Goal:** Allow CI/CD pipeline to deploy configuration changes.

```bash
# Create operator user
arca-cli user create cicd --role operator

# CI/CD pipeline can:
# - Lock datastore
# - Edit configuration
# - Validate changes
# - Commit configuration
# - Unlock datastore
# But CANNOT:
# - Kill other users' sessions
```

### Scenario 3: Emergency Admin Access

**Goal:** Provide break-glass admin access for emergencies.

```bash
# Create admin user
arca-cli user create emergency-admin --role admin

# Admin can:
# - Perform all operator operations
# - Kill stuck NETCONF sessions
# - Manage system-wide settings
```

---

## Security Considerations

### 1. **Least Privilege Principle**

Always assign the minimum role required for the task:
- Use `read-only` for monitoring and auditing
- Use `operator` for configuration management
- Reserve `admin` for system administration only

### 2. **Role Propagation**

Roles are:
- ✅ Embedded in SSH authentication response
- ✅ Stored in NETCONF session object
- ✅ Checked on every RPC operation
- ❌ NOT changeable during an active session

To change a user's role, they must disconnect and reconnect.

### 3. **Audit Logging**

RBAC denials are logged in two places:

1. **NETCONF Error Response** (visible to user)
2. **Server Audit Log** (for compliance tracking)

Example audit log entry:
```
[RBAC] Access denied: user=bob role=operator operation=kill-session session=abc-123-def-456
```

**Note:** Future versions may include structured JSON audit logs with additional fields such as source IP, timestamp, and event type.

### 4. **Multi-Factor Authentication**

RBAC works with both authentication methods:
- **Password authentication**: Role assigned after argon2id verification
- **Public key authentication**: Role assigned after key verification

Both methods provide the same RBAC guarantees.

---

## Testing RBAC

### Unit Tests

The RBAC implementation includes comprehensive unit tests:

```bash
# Run RBAC tests
go test ./pkg/netconf/ -run TestRBAC -v

# Test coverage:
# - Complete permission matrix (39 test cases)
# - Role-specific tests
# - Role hierarchy verification
# - Error message validation
# - Consistency checks
```

### Integration Tests

Test RBAC with real NETCONF clients:

```bash
# As read-only user (should succeed)
netconf-console --host 192.168.1.1 --user monitor --password xxx \
  --rpc='<get-config><source><running/></source></get-config>'

# As read-only user (should fail with access-denied)
netconf-console --host 192.168.1.1 --user monitor --password xxx \
  --rpc='<commit/>'

# As operator user (should succeed)
netconf-console --host 192.168.1.1 --user cicd --password xxx \
  --rpc='<commit/>'
```

---

## Troubleshooting

### Problem: User Can't Perform Expected Operation

**Diagnosis:**
1. Check user's role: `arca-cli user get <username>`
2. Verify role has permission for operation (see Permission Matrix above)
3. Check NETCONF error response for `error-app-tag="rbac-deny"`

**Solution:**
- If role is too restrictive, update user role
- If operation should be allowed, check RBAC matrix implementation

### Problem: Access Denied Error Not Clear

**Diagnosis:**
Check the `error-message` field in the NETCONF error response.

**Messages:**
- "read-only role cannot perform this operation" → User needs operator/admin
- "operator role cannot perform this operation" → User needs admin
- "unknown operation" → Operation not implemented
- "unknown role" → Database corruption (user has invalid role)

### Problem: Role Change Not Taking Effect

**Solution:**
User must disconnect and reconnect for role changes to take effect. Active sessions retain the role assigned at connection time.

---

## Future Enhancements

The following RBAC features are planned for future releases:

1. **Fine-Grained Permissions**
   - Per-config-element RBAC (e.g., restrict which routing protocols each role can modify)
   - Read-only access to specific configuration branches

2. **Custom Roles**
   - Define custom roles beyond the built-in three
   - Composable permission sets

3. **Session-Level Role Elevation**
   - Temporary admin privilege escalation (sudo-like)
   - Time-limited role elevation with audit trail

4. **Lock Stealing**
   - Admin ability to forcibly release locks held by other users
   - Audit trail for lock override events

5. **Dynamic Role Updates**
   - Change user role without disconnecting (with re-authentication)

---

## References

- **NETCONF RFC 6241**: Network Configuration Protocol
- **RBAC Design**: `/docs/netconf-rpc-design.md`
- **Security Model**: `/docs/security-model.md`
- **API Documentation**: `/docs/netconf-implementation-plan.md`

---

## Summary

arca-router's RBAC implementation provides:
- ✅ Three well-defined roles (read-only, operator, admin)
- ✅ Clear permission boundaries
- ✅ RFC 6241-compliant error responses
- ✅ Comprehensive test coverage
- ✅ Audit trail for access denials
- ✅ Integration with password and public key authentication

For questions or issues, refer to the troubleshooting section or consult the development team.
