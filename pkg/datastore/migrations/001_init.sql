-- Migration 001: Initial schema for arca-router config datastore
-- Version: 0.3.0
-- Phase: Phase 3

-- Schema version tracking
CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER PRIMARY KEY,
    applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Running configuration (current and historical)
CREATE TABLE IF NOT EXISTS running_config (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    commit_id TEXT NOT NULL UNIQUE,
    config_text TEXT NOT NULL,
    timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    is_current BOOLEAN NOT NULL DEFAULT 0
);

-- Unique partial index ensures only one row can have is_current = 1
-- This enforces the constraint that exactly one running config is current at any time
CREATE UNIQUE INDEX IF NOT EXISTS idx_running_config_current_unique
    ON running_config(is_current) WHERE is_current = 1;
CREATE INDEX IF NOT EXISTS idx_running_config_timestamp
    ON running_config(timestamp DESC);

-- Candidate configurations (per-session)
CREATE TABLE IF NOT EXISTS candidate_configs (
    session_id TEXT PRIMARY KEY,
    config_text TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_candidate_configs_updated
    ON candidate_configs(updated_at DESC);

-- Commit history (all commits including rollbacks)
CREATE TABLE IF NOT EXISTS commit_history (
    commit_id TEXT PRIMARY KEY,
    user TEXT NOT NULL,
    timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    message TEXT,
    config_text TEXT NOT NULL,
    is_rollback BOOLEAN NOT NULL DEFAULT 0,
    source_ip TEXT
);

CREATE INDEX IF NOT EXISTS idx_commit_history_timestamp
    ON commit_history(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_commit_history_user
    ON commit_history(user, timestamp DESC);

-- Audit log (all configuration-related events)
CREATE TABLE IF NOT EXISTS audit_log (
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

CREATE INDEX IF NOT EXISTS idx_audit_log_timestamp
    ON audit_log(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_audit_log_user
    ON audit_log(user, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_audit_log_correlation
    ON audit_log(correlation_id);
CREATE INDEX IF NOT EXISTS idx_audit_log_action
    ON audit_log(action, timestamp DESC);

-- Config locks (singleton lock for exclusive editing)
CREATE TABLE IF NOT EXISTS config_locks (
    lock_id INTEGER PRIMARY KEY CHECK (lock_id = 1),
    session_id TEXT NOT NULL,
    user TEXT NOT NULL,
    acquired_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME NOT NULL,
    last_activity DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_config_locks_expires
    ON config_locks(expires_at);

-- Record this migration
INSERT OR IGNORE INTO schema_version (version) VALUES (1);
