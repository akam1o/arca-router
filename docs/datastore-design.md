# Config Datastore Design

**Version**: 0.3.0
**Phase**: Phase 3 - Management Interfaces & Advanced Features
**Last Updated**: 2024-12-26

## Overview

This document describes the design of the configuration datastore for arca-router, which provides candidate/running configuration management with commit/rollback capabilities.

The datastore supports **multiple backend implementations**:
- **SQLite**: File-based storage for single-node deployments (Phase 3)
- **etcd**: Distributed storage for multi-node clustering (Phase 4)

## Goals

1. **Separation of Concerns**: Clearly separate running, candidate, and startup configurations
2. **Transaction Semantics**: Support atomic commit operations with rollback capability
3. **Audit Trail**: Track all configuration changes with user attribution
4. **Session Management**: Support multiple concurrent sessions with exclusive editing locks
5. **NETCONF/CLI Integration**: Provide APIs for both NETCONF and interactive CLI
6. **Backend Flexibility**: Support both SQLite (single-node) and etcd (multi-node) backends

## Backend Selection

### SQLite Backend (Phase 3)

**Use Case**: Single-node deployment, development, small deployments

**Advantages**:
- No external dependencies
- Simple file-based storage
- ACID transactions
- Low operational overhead

**Limitations**:
- Single-node only (no clustering)
- File locking limits concurrent access
- Backup requires file copy

**Configuration**:
```yaml
datastore:
  backend: sqlite
  sqlite:
    path: /var/lib/arca-router/config.db
```

### etcd Backend (Phase 4)

**Use Case**: Multi-node clustering, high availability, distributed deployments

**Advantages**:
- Distributed consensus (Raft)
- Multi-node coordination
- Watch/notification support
- TTL-based expiration

**Limitations**:
- External dependency (etcd cluster)
- Higher operational complexity
- Network latency

**Configuration**:
```yaml
datastore:
  backend: etcd
  etcd:
    endpoints:
      - https://etcd1:2379
      - https://etcd2:2379
      - https://etcd3:2379
    prefix: /arca-router/
    timeout: 5s
    tls:
      cert_file: /etc/arca-router/etcd-client.crt
      key_file: /etc/arca-router/etcd-client.key
      ca_file: /etc/arca-router/etcd-ca.crt
```

## Configuration Datastores

### Running Configuration

- **Purpose**: Currently active configuration applied to VPP/FRR
- **Storage**: Backend-specific (SQLite table or etcd key `/arca-router/running`)
- **Format**: Text format (set-style commands) + metadata
- **Updates**: Only through successful commit operations
- **Persistence**: Persisted across daemon restarts
- **Current Version Tracking**:
  - SQLite: Row with `is_current = 1` in `running_config` table
  - etcd: Single key `/arca-router/running/current` contains current commit metadata

### Candidate Configuration

- **Purpose**: Working configuration being edited in a session
- **Storage**: Backend-specific (SQLite table or etcd keys under `/arca-router/candidates/<session-id>`)
- **Format**: Text format (set-style commands)
- **Lifecycle**: Created on lock acquire, destroyed on commit/discard
- **Isolation**: One candidate per session, exclusive lock required
- **Storage Strategy**: All candidates stored in single database/keyspace (not per-session databases)

### Startup Configuration

- **Purpose**: Configuration loaded at daemon startup
- **Storage**: File-based (`/etc/arca-router/arca-router.conf`)
- **Format**: Text format (set-style commands)
- **Loading**: Copied to running configuration on first daemon start

## Data Model

### SQLite Database Schema

#### `running_config` Table

```sql
CREATE TABLE running_config (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    commit_id TEXT NOT NULL UNIQUE,
    config_text TEXT NOT NULL,
    timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    is_current BOOLEAN NOT NULL DEFAULT 0
);

CREATE INDEX idx_running_config_current ON running_config(is_current) WHERE is_current = 1;
CREATE INDEX idx_running_config_timestamp ON running_config(timestamp DESC);
```

- **Current Running Config**: Exactly one row with `is_current = 1` at any time
- **Historical Configs**: Older rows with `is_current = 0` for rollback purposes
- **Constraint**: Application ensures only one `is_current = 1` row (enforced by transaction logic)
- **Index**: Fast retrieval of current config via `is_current` index

#### `candidate_configs` Table

```sql
CREATE TABLE candidate_configs (
    session_id TEXT PRIMARY KEY,
    config_text TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_candidate_configs_updated ON candidate_configs(updated_at DESC);
```

#### `commit_history` Table

```sql
CREATE TABLE commit_history (
    commit_id TEXT PRIMARY KEY,
    user TEXT NOT NULL,
    timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    message TEXT,
    config_text TEXT NOT NULL,
    is_rollback BOOLEAN NOT NULL DEFAULT 0,
    source_ip TEXT
);

CREATE INDEX idx_commit_history_timestamp ON commit_history(timestamp DESC);
CREATE INDEX idx_commit_history_user ON commit_history(user, timestamp DESC);
```

- **is_rollback**: Flag to distinguish rollback commits from normal commits
- **source_ip**: Source IP address for audit tracking

#### `audit_log` Table

```sql
CREATE TABLE audit_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    user TEXT NOT NULL,
    session_id TEXT,
    source_ip TEXT,
    correlation_id TEXT,
    action TEXT NOT NULL,
    result TEXT NOT NULL,
    error_code TEXT,
    details TEXT
);

CREATE INDEX idx_audit_log_timestamp ON audit_log(timestamp DESC);
CREATE INDEX idx_audit_log_user ON audit_log(user, timestamp DESC);
CREATE INDEX idx_audit_log_correlation ON audit_log(correlation_id);
CREATE INDEX idx_audit_log_action ON audit_log(action, timestamp DESC);
```

- **source_ip**: Source IP address for security tracking
- **correlation_id**: Correlation ID for tracing related events across multiple log entries
- **error_code**: Standardized error code for failures (e.g., "VALIDATION", "TIMEOUT")
- **Indexes**: Optimized for common queries (time-based, user-based, correlation tracking)

#### `config_locks` Table

```sql
CREATE TABLE config_locks (
    lock_id INTEGER PRIMARY KEY CHECK (lock_id = 1), -- Singleton: only one row allowed
    session_id TEXT NOT NULL,
    user TEXT NOT NULL,
    acquired_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME NOT NULL,
    last_activity DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_config_locks_expires ON config_locks(expires_at);
```

- **Singleton Lock**: `lock_id = 1` constraint ensures only one lock can exist at a time
- **Exclusive Access**: Only one session can hold the config lock globally
- **Acquire Logic**: `INSERT OR REPLACE INTO config_locks (lock_id, ...) VALUES (1, ...)`
  - Fails if lock exists and not expired (checked by application)
  - Succeeds if no lock or expired lock
- **last_activity**: Timestamp of last lock-related activity (for timeout tracking)
- **Cleanup**: Background job periodically deletes expired locks (`expires_at < NOW()`)

#### `schema_version` Table

```sql
CREATE TABLE schema_version (
    version INTEGER PRIMARY KEY,
    applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

### etcd Data Model (Phase 4)

For etcd backend, data is stored as key-value pairs under `/arca-router/` prefix:

#### Key Structure

```
/arca-router/running/current          → Current running config metadata (commit_id, timestamp)
/arca-router/running/config           → Current running config text
/arca-router/candidates/<session-id>  → Candidate config text
/arca-router/commits/<commit-id>      → Commit history entry (metadata + config)
/arca-router/lock                     → Singleton lock (single key for exclusive access)
/arca-router/audit/<ulid>             → Audit log entries (ULID for sortable, unique IDs)
```

**Audit Log ID Generation**:
- Use ULID (Universally Unique Lexicographically Sortable Identifier)
- Format: 26 characters, timestamp-prefixed, sortable
- Example: `01ARYZ6S41TSV4RRFFQ69G5FAV`
- Avoids collisions in distributed environment without coordination

#### Lock Management with etcd

- **Singleton Lock Key**: `/arca-router/lock` (single key for exclusive config lock)
- **Lock Value**: JSON with `{session_id, user, acquired_at, expires_at}`
- **Acquire Logic**:
  - Use etcd transaction with CAS (Compare-And-Swap)
  - Condition: Key does not exist OR value.expires_at < now
  - Success: Write lock with lease
  - Failure: Lock already held by another session
- **Lease-Based Expiration**:
  - etcd lease attached to lock key (TTL: 30 minutes)
  - Session sends periodic keepalive to extend lease
  - If session dies/disconnects, lease expires and lock auto-released
- **Lock Extend**: Send keepalive request to etcd to extend lease TTL

#### Advantages over SQLite

- **Distributed Locking**: Native support via etcd leases
- **Watch Notifications**: Other nodes can watch for config changes
- **Atomic Operations**: Compare-and-swap (CAS) for race-free updates
- **TTL Cleanup**: Automatic expiration of stale locks/sessions

### Configuration Format

#### Text Format (Primary)

Configurations are stored as plain text in Junos-style `set` command format:

```
set system host-name router1
set interfaces ge-0/0/0 unit 0 family inet address 10.0.0.1/24
set protocols bgp group ebgp neighbor 10.0.0.2 peer-as 65001
```

**Rationale**:
- Human-readable and editable
- Preserves unknown/future syntax (forward compatibility)
- Directly usable for diff/compare operations
- Compatible with existing parser (`pkg/config/parser.go`)

#### Normalization Rules

To ensure consistent diff/compare operations:

1. **Line Order**: Sort by hierarchical path (lexicographic)
2. **Whitespace**: Single space between tokens
3. **Duplicates**: Remove exact duplicates (last one wins)
4. **Comments**: Strip comments before storage

**Example**:

```
# Before normalization
set  system   host-name   router1
set interfaces ge-0/0/0 unit 0 family inet address 10.0.0.1/24
set system host-name router1  # duplicate

# After normalization
set interfaces ge-0/0/0 unit 0 family inet address 10.0.0.1/24
set system host-name router1
```

## Transaction Semantics

### Database Transaction Boundaries

#### SQLite Transactions

All datastore operations use SQLite transactions with proper isolation:

```go
// Example: Commit transaction
BEGIN TRANSACTION;
  -- 1. Update is_current flags
  UPDATE running_config SET is_current = 0 WHERE is_current = 1;

  -- 2. Insert new running config
  INSERT INTO running_config (commit_id, config_text, is_current) VALUES (?, ?, 1);

  -- 3. Insert commit history
  INSERT INTO commit_history (commit_id, user, message, config_text, source_ip) VALUES (?, ?, ?, ?, ?);

  -- 4. Delete candidate config
  DELETE FROM candidate_configs WHERE session_id = ?;

  -- 5. Release lock
  DELETE FROM config_locks WHERE session_id = ?;

  -- 6. Log audit event
  INSERT INTO audit_log (user, session_id, source_ip, action, result) VALUES (?, ?, ?, 'commit', 'success');
COMMIT;
```

**Isolation Level**: `SERIALIZABLE` (SQLite default) - ensures full consistency

**Failure Handling**:
- If any statement fails: `ROLLBACK` entire transaction
- Database remains in consistent state
- Application retries or reports error to user

#### etcd Transactions

etcd provides atomic compare-and-swap (CAS) operations:

```go
// Example: Commit with CAS
txn := etcdClient.Txn(ctx).If(
  // Condition: Lock still held by this session
  clientv3.Compare(clientv3.Value("/arca-router/locks/"+sessionID), "=", sessionID),
).Then(
  // Success: Update running config and commit history
  clientv3.OpPut("/arca-router/running/current", commitMetadata),
  clientv3.OpPut("/arca-router/running/config", configText),
  clientv3.OpPut("/arca-router/commits/"+commitID, commitEntry),
  clientv3.OpDelete("/arca-router/candidates/"+sessionID),
  clientv3.OpDelete("/arca-router/locks/"+sessionID),
).Else(
  // Failure: Lock was stolen or expired
  clientv3.OpGet("/arca-router/locks/"+sessionID),
)
```

### Commit Flow

```
Phase 1: Pre-Validation (No Side Effects)
  1. User initiates commit (CLI/NETCONF)
  2. Verify config lock is held by current session
  3. Load candidate config from datastore
  4. Parse config text → config.Config struct
  5. Run config.Validate() - catch errors early
  6. Generate commit ID (UUID v4)

Phase 2: VPP/FRR Application (Side Effects Begin)
  7. Apply to VPP (existing apply.go logic with reconciliation)
     - If VPP apply fails: Return error, keep candidate, exit
  8. Apply to FRR (existing apply.go logic)
     - If FRR apply fails: Attempt VPP rollback (best effort), return error

Phase 3: Database Commit (Atomic)
  9. Begin database transaction
  10. Update running_config (set is_current = 1 for new row)
  11. Insert into commit_history
  12. Delete candidate_configs row
  13. Delete config_locks row (release lock)
  14. Insert audit_log entry (success)
  15. Commit database transaction
      - If DB commit fails: Log critical error, manual recovery required

Phase 4: Cleanup
  16. Return success to user with commit ID
```

**Critical Invariant**: Once Phase 3 commits, the database reflects the new running config. If Phase 3 fails after Phase 2 succeeds, the system is in an inconsistent state (VPP/FRR have new config, but DB shows old config). This is logged as a critical error requiring manual intervention.

### Rollback Flow

```
Phase 1: Pre-Validation
  1. User initiates rollback to commit ID
  2. Verify user has admin role (rollback is privileged)
  3. Acquire global rollback lock (prevents concurrent rollbacks)
  4. Load target config from commit_history by commit ID
     - If not found: Return error "commit not found"
  5. Parse config text → config.Config struct
  6. Run config.Validate() (should always pass for historical configs)
  7. Generate new commit ID for rollback commit

Phase 2: VPP/FRR Application
  8. Apply to VPP (full reconfiguration)
     - If VPP apply fails: Return error, exit
  9. Apply to FRR (full reconfiguration)
     - If FRR apply fails: Attempt VPP rollback (best effort), return error

Phase 3: Database Commit (Atomic)
  10. Begin database transaction
  11. Update running_config (new row with is_current = 1, old row is_current = 0)
  12. Insert into commit_history (config_text = target, is_rollback = 1)
  13. Insert audit_log entry (success, action = "rollback")
  14. Commit database transaction

Phase 4: Cleanup
  15. Release global rollback lock
  16. Return success to user with new commit ID
```

### Atomicity Guarantees and Limitations

**What IS Atomic**:
- ✅ Database operations (single SQLite transaction or etcd TXN)
- ✅ Lock acquisition/release
- ✅ Audit log consistency

**What is NOT Atomic**:
- ❌ VPP + FRR + Database as a single unit (3-phase, not atomic)
- ❌ Rollback of partial failures (best effort only)

**Failure Scenarios**:

| Failure Point | System State | Recovery Strategy |
|---------------|--------------|-------------------|
| Validation fails | No change | User fixes config, retries |
| VPP apply fails | No change | User fixes config, retries |
| FRR apply fails | VPP updated, FRR old | Attempt VPP rollback; if fails, restart daemon |
| DB commit fails | VPP+FRR updated, DB old | **Critical**: Manual DB update or daemon restart |

**Mitigation Strategies**:

1. **Extensive Validation**: Catch errors before applying to VPP/FRR
2. **Audit Logging**: Record all commit attempts (success/failure/partial)
3. **Manual Recovery**: Document procedures for partial failure scenarios
4. **Idempotency**: VPP/FRR apply logic is idempotent (Phase 2 reconciliation)

### Manual Recovery Procedures

If commit/rollback fails partially:

1. Check audit log for failure details
2. Inspect VPP state: `arca-cli show interfaces`
3. Inspect FRR state: `vtysh -c "show running-config"`
4. Manually correct VPP/FRR to match running config
5. Or: Rollback to last known good commit
6. Document the incident in operations log

## Session Management

### Session Lifecycle

```
1. User connects (SSH/NETCONF or Interactive CLI)
2. Session ID generated (UUID)
3. User enters configuration mode
4. Acquire exclusive config lock (or wait/fail)
5. Create candidate config (copy from running)
6. User edits candidate (set/delete commands)
7. User commits → Running updated, candidate deleted
   OR User discards → Candidate deleted
8. Release config lock
9. Session ends
```

### Lock Behavior

- **Exclusive Lock**: Only one session can hold the config lock
- **Timeout**: 30 minutes of inactivity → auto-release
- **Steal**: Admin role can forcibly acquire lock (logs audit event)
- **Persistence**: Lock state stored in `config_locks` table

### Concurrent Sessions

- Multiple read-only sessions: Allowed (show commands)
- Multiple edit sessions: Blocked by exclusive lock
- Lock holder can be identified: `show system users`

## API Design

### Core Datastore Interface

```go
type Datastore interface {
    // Running configuration
    GetRunning(ctx context.Context) (*RunningConfig, error)

    // Candidate configuration
    GetCandidate(ctx context.Context, sessionID string) (*CandidateConfig, error)
    SaveCandidate(ctx context.Context, sessionID string, configText string) error
    DeleteCandidate(ctx context.Context, sessionID string) error

    // Commit/Rollback transactions
    Commit(ctx context.Context, req *CommitRequest) (commitID string, err error)
    Rollback(ctx context.Context, req *RollbackRequest) (newCommitID string, err error)

    // Diff/Compare operations
    CompareCandidateRunning(ctx context.Context, sessionID string) (*DiffResult, error)
    CompareCommits(ctx context.Context, commitID1, commitID2 string) (*DiffResult, error)

    // Lock management
    AcquireLock(ctx context.Context, req *LockRequest) error
    ReleaseLock(ctx context.Context, sessionID string) error
    ExtendLock(ctx context.Context, sessionID string, duration time.Duration) error
    StealLock(ctx context.Context, req *StealLockRequest) error
    GetLockInfo(ctx context.Context) (*LockInfo, error)

    // Commit history
    ListCommitHistory(ctx context.Context, opts *HistoryOptions) ([]*CommitHistoryEntry, error)
    GetCommit(ctx context.Context, commitID string) (*CommitHistoryEntry, error)

    // Audit logging
    LogAuditEvent(ctx context.Context, event *AuditEvent) error

    // Close the datastore
    Close() error
}
```

**Key Changes from Original Design**:
- All methods accept `context.Context` for timeout/cancellation support
- `Commit` and `Rollback` use structured request types (`CommitRequest`, `RollbackRequest`)
- Return values simplified: `Commit` and `Rollback` return commitID directly, errors via `error`
- `ExtendLock` added for long-running CLI sessions
- `HistoryOptions` provides filtering/pagination for commit history
- `LogAuditEvent` exposed for application-level audit logging

### Request/Response Types

```go
// CommitRequest contains parameters for a commit operation
type CommitRequest struct {
    SessionID string // Session holding the candidate config
    User      string // Username performing the commit
    Message   string // Optional commit message
    SourceIP  string // Source IP address (for audit)
}

// RollbackRequest contains parameters for a rollback operation
type RollbackRequest struct {
    CommitID string // Target commit ID to rollback to
    User     string // Username performing the rollback
    Message  string // Optional rollback reason
    SourceIP string // Source IP address (for audit)
}

// HistoryOptions contains filtering options for commit history queries
type HistoryOptions struct {
    Limit            int       // Maximum entries to return (0 = no limit)
    Offset           int       // Entries to skip (for pagination)
    StartTime        time.Time // Filter commits after this time (zero = no filter)
    EndTime          time.Time // Filter commits before this time (zero = no filter)
    User             string    // Filter by username (empty = no filter)
    ExcludeRollbacks bool      // Exclude rollback commits (default: false, include all)
}

// LockRequest contains parameters for acquiring a config lock
type LockRequest struct {
    SessionID string        // Session requesting the lock
    User      string        // Username requesting the lock
    Timeout   time.Duration // Lock timeout (default: 30 minutes)
}
```

### Migration Management

```go
type MigrationManager interface {
    // Get current schema version
    GetCurrentVersion() (int, error)

    // Apply pending migrations
    ApplyMigrations() error

    // Create backup before migration
    CreateBackup() (string, error)
}
```

## Migration Strategy

### Forward-Only Migrations

- Migrations are numbered: `001_init.sql`, `002_add_indexes.sql`, etc.
- Applied in order, each only once
- No downgrade support (manual restoration from backup if needed)

### Migration Execution

```
1. Daemon startup
2. Check schema_version table
3. Identify pending migrations (version+1, version+2, ...)
4. Create database backup
5. Begin transaction
6. Apply each migration
7. Update schema_version
8. Commit transaction
9. On failure:
   - Rollback transaction
   - Log error
   - Stop daemon (manual intervention required)
```

### Migration Safety

- **Idempotency**: Each migration uses `IF NOT EXISTS` / `IF NOT EXISTS` clauses
- **Backward Compatibility**: Additive changes only (add columns with defaults, not drop)
- **Testing**: All migrations tested in CI before release

## Rollback Scenarios

### Scenario 1: FRR Apply Failure

```
State: VPP applied, FRR failed
Action:
  1. Log audit event (partial failure)
  2. Attempt VPP rollback to previous running config
  3. If VPP rollback succeeds:
     - Running config unchanged
     - Candidate preserved
     - User retries after fixing config
  4. If VPP rollback fails:
     - Running config marked as "dirty" (future feature)
     - Manual recovery required
```

### Scenario 2: Database Commit Failure

```
State: VPP/FRR applied, database update failed
Action:
  1. Log audit event (database failure)
  2. VPP/FRR are in new state, but running_config not updated
  3. On next commit, reconciliation detects mismatch
  4. Manual recovery: Restart daemon (re-applies running config)
```

### Scenario 3: Rollback Command Failure

```
State: User issues rollback, application fails
Action:
  1. Log audit event (rollback failure)
  2. Running config unchanged
  3. Manual recovery: Restart daemon or issue another rollback
```

## Security Considerations

### SQLite Backend Security

#### Database File Permissions

- **Path**: `/var/lib/arca-router/config.db`
- **Ownership**: `arca-router:arca-router`
- **Permissions**: `0600` (read/write for owner only)
- **Directory**: `/var/lib/arca-router/` with `0750` permissions

#### Backup Protection

- Database backups (for migrations) stored in same directory
- Same ownership and `0600` permissions
- Backups named `config.db.backup.<timestamp>`

### etcd Backend Security (Phase 4)

#### Transport Security (mTLS)

- **Mandatory**: All etcd connections must use TLS
- **Client Certificates**: Each arca-router node has unique client certificate
- **CA Validation**: Verify etcd server certificates against trusted CA
- **Configuration**:
  ```yaml
  datastore:
    etcd:
      tls:
        cert_file: /etc/arca-router/etcd-client.crt
        key_file: /etc/arca-router/etcd-client.key  # 0600 permissions
        ca_file: /etc/arca-router/etcd-ca.crt
  ```

#### Authentication & Authorization

- **etcd User Authentication**: Use etcd username/password or client cert
- **RBAC**: Configure etcd roles for arca-router:
  - Read/write access to `/arca-router/*` prefix only
  - No access to other prefixes (isolation)
- **Key Prefix Isolation**: Multiple arca-router instances can share etcd cluster with different prefixes

#### Data Encryption at Rest

- **etcd Encryption**: Enable etcd's encryption-at-rest feature
- **Key Management**: Rotate encryption keys periodically
- **Future**: Application-level encryption for sensitive config fields

### Secrets in Configuration

- **Current**: Passwords (BGP MD5 auth) stored in plain text in config
- **Protection**: Rely on datastore file permissions / etcd TLS + RBAC
- **Future (Phase 4)**:
  - Encrypt sensitive fields at application level
  - Integration with external secret stores (HashiCorp Vault, etc.)
  - Separate secrets from main config (reference by ID)

### Audit Trail Integrity

- **Append-Only**: Audit log is append-only (no DELETE operations at application level)
- **SQLite**: Use `PRAGMA journal_mode=WAL` for durability
- **etcd**: Audit entries written with monotonic ULID keys (sortable, no overwrites)
- **Retention**: Automatic cleanup of old entries (>90 days) with separate archive
- **Future**: Write-once storage (WORM) for compliance environments

## Integration Points

### NETCONF Server

- `<get-config source="running">` → `Datastore.GetRunning()`
- `<get-config source="candidate">` → `Datastore.GetCandidate(sessionID)`
- `<edit-config target="candidate">` → Parse XML → `Datastore.SaveCandidate()`
- `<commit>` → `Datastore.Commit()`
- `<discard-changes>` → `Datastore.DeleteCandidate()`
- `<lock target="candidate">` → `Datastore.AcquireLock()`
- `<unlock target="candidate">` → `Datastore.ReleaseLock()`

### Interactive CLI

- `configure` → `Datastore.AcquireLock()` + `GetCandidate()`
- `set ...` → Parse + `Datastore.SaveCandidate()`
- `delete ...` → Parse + `Datastore.SaveCandidate()`
- `show | compare` → `Datastore.CompareCandidateRunning()`
- `commit` → `Datastore.Commit()`
- `rollback N` → `Datastore.Rollback()`

### arca-routerd Startup

```
1. Load startup config (/etc/arca-router/arca-router.conf)
2. Check if running_config table is empty
3. If empty:
   - Generate initial commit ID
   - Insert startup config as first running config
4. If not empty:
   - Load latest running config
5. Apply running config to VPP/FRR
```

## Performance Considerations

### Database Size

- Estimate: 100 commits/day × 10KB/commit × 365 days = ~365MB/year
- Retention policy: Keep last 1000 commits, archive older (future)

### Lock Contention

- Single writer (exclusive lock) limits throughput
- Acceptable for typical operator workflow (one admin editing at a time)

### Diff Performance

- Text-based diff is fast for typical configs (1-10KB)
- Use efficient diff algorithm (Myers' algorithm, unified diff format)

## Future Enhancements

### Phase 4 Candidates

1. **Structured Storage**: Store parsed `config.Config` as JSON alongside text
   - Benefit: Faster validation, easier partial updates
   - Challenge: Maintain text as source of truth for unknown syntax

2. **Confirmed Commit**: Auto-rollback unless explicitly confirmed within timeout
   - Benefit: Prevents lockout from misconfigurations
   - Challenge: Requires timer management

3. **Config Diff Optimization**: Semantic diff (not just text diff)
   - Benefit: Show "set X → Y" instead of "- X" + "+ Y"
   - Challenge: Requires deep understanding of config structure

4. **Distributed Datastore**: etcd for multi-node coordination
   - Benefit: Enables clustering
   - Challenge: Adds complexity, external dependency

5. **Secret Encryption**: Encrypt sensitive fields at rest
   - Benefit: Compliance, security hardening
   - Challenge: Key management

## References

- `PHASE3.md`: Phase 3 implementation plan
- `SPEC.md`: arca-router specification
- `pkg/config/parser.go`: Existing config parser
- `cmd/arca-routerd/apply.go`: VPP/FRR application logic
- RFC 6241: NETCONF Protocol
- RFC 6242: NETCONF over SSH

---

## Summary

This datastore design provides:

- ✅ Separation of running/candidate/startup configurations
- ✅ Commit/rollback transaction semantics
- ✅ Audit trail for all configuration changes
- ✅ Exclusive locking for safe concurrent access
- ✅ NETCONF and CLI integration APIs
- ⚠️ Best-effort atomicity (VPP/FRR independence limits full ACID)
- ⚠️ Manual recovery procedures for partial failures

The design balances operational safety, implementation simplicity, and integration with existing arca-router components.
