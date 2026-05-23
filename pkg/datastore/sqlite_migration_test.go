package datastore

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSQLiteMigrationConvertsLegacyLockTimestamps(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "config.db")
	db := openMigrationTestDB(t, dbPath)
	mustExec(t, db, `
		CREATE TABLE schema_version (
			version INTEGER PRIMARY KEY,
			applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		INSERT INTO schema_version (version) VALUES (1);
		CREATE TABLE config_locks (
			lock_id INTEGER PRIMARY KEY CHECK (lock_id = 1),
			session_id TEXT NOT NULL,
			user TEXT NOT NULL,
			acquired_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			expires_at DATETIME NOT NULL,
			last_activity DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		INSERT INTO config_locks (lock_id, session_id, user, acquired_at, expires_at, last_activity)
		VALUES (1, 'legacy-session', 'alice', '2026-05-11 00:00:00', '2099-01-02 03:04:05', '2026-05-11 00:01:00');
	`)
	closeDB(t, db)

	ds := openSQLiteDatastoreForTest(t, dbPath)

	var version int
	if err := ds.db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&version); err != nil {
		t.Fatalf("schema version query failed: %v", err)
	}
	if version != 2 {
		t.Fatalf("schema version = %d, want 2", version)
	}

	var storageType string
	if err := ds.db.QueryRow(`SELECT typeof(expires_at) FROM config_locks WHERE target = ?`, LockTargetCandidate).Scan(&storageType); err != nil {
		t.Fatalf("lock timestamp type query failed: %v", err)
	}
	if storageType != "integer" {
		t.Fatalf("expires_at storage type = %q, want integer", storageType)
	}

	info, err := ds.GetLockInfo(context.Background(), LockTargetCandidate)
	if err != nil {
		t.Fatalf("GetLockInfo() error = %v", err)
	}
	if !info.IsLocked || info.SessionID != "legacy-session" || info.User != "alice" {
		t.Fatalf("GetLockInfo() = %#v, want migrated legacy lock", info)
	}
}

func TestSQLiteMigrationRepairsUnrecordedTargetLockMigration(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "config.db")
	db := openMigrationTestDB(t, dbPath)
	future := time.Now().Add(time.Hour).UTC().Format("2006-01-02 15:04:05")
	mustExec(t, db, `
		CREATE TABLE schema_version (
			version INTEGER PRIMARY KEY,
			applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		INSERT INTO schema_version (version) VALUES (1);
		CREATE TABLE config_locks (
			target TEXT NOT NULL PRIMARY KEY CHECK(target IN ('candidate', 'running')),
			session_id TEXT NOT NULL,
			user TEXT NOT NULL,
			acquired_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL,
			last_activity INTEGER NOT NULL
		);
	`)
	if _, err := db.Exec(`
		INSERT INTO config_locks (target, session_id, user, acquired_at, expires_at, last_activity)
		VALUES (?, ?, ?, ?, ?, ?)
	`, LockTargetCandidate, "unrecorded-session", "bob", "2026-05-11 00:00:00", future, "2026-05-11 00:01:00"); err != nil {
		t.Fatalf("insert unrecorded target lock: %v", err)
	}
	closeDB(t, db)

	ds := openSQLiteDatastoreForTest(t, dbPath)

	var version int
	if err := ds.db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&version); err != nil {
		t.Fatalf("schema version query failed: %v", err)
	}
	if version != 2 {
		t.Fatalf("schema version = %d, want repaired version 2", version)
	}

	info, err := ds.GetLockInfo(context.Background(), LockTargetCandidate)
	if err != nil {
		t.Fatalf("GetLockInfo() error = %v", err)
	}
	if !info.IsLocked || info.SessionID != "unrecorded-session" || info.User != "bob" {
		t.Fatalf("GetLockInfo() = %#v, want existing target lock", info)
	}
}

func TestSQLiteMigrationRejectsNewerSchemaVersion(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "config.db")
	db := openMigrationTestDB(t, dbPath)
	mustExec(t, db, `
		CREATE TABLE schema_version (
			version INTEGER PRIMARY KEY,
			applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		INSERT INTO schema_version (version) VALUES (999);
	`)
	closeDB(t, db)

	ds, err := NewSQLiteDatastore(&Config{
		Backend:    BackendSQLite,
		SQLitePath: dbPath,
	})
	if err == nil {
		_ = ds.Close()
		t.Fatal("NewSQLiteDatastore() error = nil, want newer schema version rejection")
	}
	if !strings.Contains(err.Error(), "newer than supported version") {
		t.Fatalf("NewSQLiteDatastore() error = %v, want newer schema version rejection", err)
	}
}

func TestSQLiteMigrationCreateBackupBindsBackupPath(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "config'; DROP TABLE sample; --.db")
	db := openMigrationTestDB(t, dbPath)
	defer closeDB(t, db)
	mustExec(t, db, `
		CREATE TABLE sample (
			id INTEGER PRIMARY KEY,
			value TEXT NOT NULL
		);
		INSERT INTO sample (value) VALUES ('kept');
	`)

	backupPath, err := NewSQLiteMigrationManager(db, dbPath).CreateBackup()
	if err != nil {
		t.Fatalf("CreateBackup() error = %v", err)
	}
	if backupPath == "" {
		t.Fatal("CreateBackup() backupPath is empty, want backup file")
	}
	assertSQLiteFileMode(t, backupPath, secureSQLiteFilePerms)

	var sourceValue string
	if err := db.QueryRow(`SELECT value FROM sample WHERE id = 1`).Scan(&sourceValue); err != nil {
		t.Fatalf("source query failed after CreateBackup(): %v", err)
	}
	if sourceValue != "kept" {
		t.Fatalf("source value = %q, want kept", sourceValue)
	}

	backupDB := openMigrationTestDB(t, backupPath)
	defer closeDB(t, backupDB)
	var backupValue string
	if err := backupDB.QueryRow(`SELECT value FROM sample WHERE id = 1`).Scan(&backupValue); err != nil {
		t.Fatalf("backup query failed: %v", err)
	}
	if backupValue != "kept" {
		t.Fatalf("backup value = %q, want kept", backupValue)
	}
}

func openMigrationTestDB(t *testing.T, dbPath string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open sqlite test db: %v", err)
	}
	return db
}

func openSQLiteDatastoreForTest(t *testing.T, dbPath string) *sqliteDatastore {
	t.Helper()
	ds, err := NewSQLiteDatastore(&Config{
		Backend:    BackendSQLite,
		SQLitePath: dbPath,
	})
	if err != nil {
		t.Fatalf("NewSQLiteDatastore() error = %v", err)
	}
	t.Cleanup(func() { _ = ds.Close() })

	sqliteDS, ok := ds.(*sqliteDatastore)
	if !ok {
		t.Fatalf("NewSQLiteDatastore() returned %T, want *sqliteDatastore", ds)
	}
	return sqliteDS
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("exec query failed: %v", err)
	}
}

func closeDB(t *testing.T, db *sql.DB) {
	t.Helper()
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
}
