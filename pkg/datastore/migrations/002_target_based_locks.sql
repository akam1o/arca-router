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
SELECT 'candidate', session_id, user, acquired_at, expires_at, last_activity
FROM config_locks
WHERE lock_id = 1;

-- Step 3: Drop old table
DROP TABLE config_locks;

-- Step 4: Rename new table to original name
ALTER TABLE config_locks_new RENAME TO config_locks;

-- Step 5: Create index for efficient expiry checks during background cleanup
CREATE INDEX IF NOT EXISTS idx_config_locks_expires ON config_locks(expires_at);
