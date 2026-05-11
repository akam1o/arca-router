-- Migration 002: Add target-based locking (candidate/running separate locks)
-- This migration replaces the singleton lock_id=1 constraint with target-based primary key.
-- Allows independent locks for 'candidate' and 'running' datastores per NETCONF requirements.

-- Step 1: Create new table with target-based schema
CREATE TABLE IF NOT EXISTS config_locks_new (
    target TEXT NOT NULL PRIMARY KEY CHECK(target IN ('candidate', 'running')),
    session_id TEXT NOT NULL,
    user TEXT NOT NULL,
    acquired_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    last_activity INTEGER NOT NULL
);

-- Step 2: Migrate existing lock data (if any) to 'candidate' target
-- Only migrates if a lock exists with lock_id=1 (legacy singleton lock)
INSERT INTO config_locks_new (target, session_id, user, acquired_at, expires_at, last_activity)
SELECT
    'candidate',
    session_id,
    user,
    COALESCE(
        CASE WHEN typeof(acquired_at) IN ('integer', 'real') THEN CAST(acquired_at AS INTEGER) END,
        CASE WHEN typeof(acquired_at) = 'text' AND acquired_at NOT GLOB '*[^0-9]*' THEN CAST(acquired_at AS INTEGER) END,
        CAST(strftime('%s', acquired_at) AS INTEGER),
        CAST(strftime('%s', 'now') AS INTEGER)
    ),
    COALESCE(
        CASE WHEN typeof(expires_at) IN ('integer', 'real') THEN CAST(expires_at AS INTEGER) END,
        CASE WHEN typeof(expires_at) = 'text' AND expires_at NOT GLOB '*[^0-9]*' THEN CAST(expires_at AS INTEGER) END,
        CAST(strftime('%s', expires_at) AS INTEGER),
        0
    ),
    COALESCE(
        CASE WHEN typeof(last_activity) IN ('integer', 'real') THEN CAST(last_activity AS INTEGER) END,
        CASE WHEN typeof(last_activity) = 'text' AND last_activity NOT GLOB '*[^0-9]*' THEN CAST(last_activity AS INTEGER) END,
        CAST(strftime('%s', last_activity) AS INTEGER),
        CAST(strftime('%s', 'now') AS INTEGER)
    )
FROM config_locks
WHERE lock_id = 1;

-- Step 3: Drop old table
DROP TABLE config_locks;

-- Step 4: Rename new table to original name
ALTER TABLE config_locks_new RENAME TO config_locks;

-- Step 5: Create index for efficient expiry checks during background cleanup
CREATE INDEX IF NOT EXISTS idx_config_locks_expires ON config_locks(expires_at);

-- Step 6: Record this migration
INSERT OR IGNORE INTO schema_version (version) VALUES (2);
