package datastore

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3" // SQLite driver
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// sqliteMigrationManager implements MigrationManager for SQLite.
type sqliteMigrationManager struct {
	db     *sql.DB
	dbPath string
}

// NewSQLiteMigrationManager creates a new migration manager for SQLite.
func NewSQLiteMigrationManager(db *sql.DB, dbPath string) MigrationManager {
	return &sqliteMigrationManager{
		db:     db,
		dbPath: dbPath,
	}
}

// GetCurrentVersion returns the current schema version.
func (m *sqliteMigrationManager) GetCurrentVersion() (int, error) {
	// Check if schema_version table exists
	var count int
	err := m.db.QueryRow(`
		SELECT COUNT(*) FROM sqlite_master
		WHERE type='table' AND name='schema_version'
	`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to check schema_version table: %w", err)
	}

	if count == 0 {
		// No schema_version table means version 0 (uninitialized)
		return 0, nil
	}

	// Get max version from schema_version table
	var version int
	err = m.db.QueryRow(`
		SELECT COALESCE(MAX(version), 0) FROM schema_version
	`).Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("failed to get current schema version: %w", err)
	}

	return version, nil
}

// ApplyMigrations applies all pending migrations.
func (m *sqliteMigrationManager) ApplyMigrations() error {
	currentVersion, err := m.GetCurrentVersion()
	if err != nil {
		return fmt.Errorf("failed to get current version: %w", err)
	}

	// Get list of migration files
	migrations, err := m.getMigrationFiles()
	if err != nil {
		return fmt.Errorf("failed to get migration files: %w", err)
	}

	if len(migrations) == 0 {
		return fmt.Errorf("no migration files found")
	}

	// Find pending migrations
	var pending []migration
	for _, mig := range migrations {
		if mig.version > currentVersion {
			pending = append(pending, mig)
		}
	}

	if len(pending) == 0 {
		// No pending migrations
		return nil
	}

	// Create backup before applying migrations
	backupPath, err := m.CreateBackup()
	if err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}
	if backupPath != "" {
		// Only log if backup was actually created
		fmt.Printf("Created database backup: %s\n", backupPath)
	}

	// Apply each pending migration in a transaction
	for _, mig := range pending {
		if err := m.applyMigration(mig); err != nil {
			return fmt.Errorf("failed to apply migration %03d: %w", mig.version, err)
		}
		fmt.Printf("Applied migration %03d: %s\n", mig.version, mig.name)
	}

	return nil
}

// CreateBackup creates a database backup before migrations.
// Uses SQLite's VACUUM INTO for consistent backups.
func (m *sqliteMigrationManager) CreateBackup() (string, error) {
	if m.dbPath == "" || m.dbPath == ":memory:" {
		// In-memory database, no backup needed
		return "", nil
	}

	// Check if source file exists
	if _, err := os.Stat(m.dbPath); os.IsNotExist(err) {
		// Database file doesn't exist yet (first run)
		return "", nil
	}

	// Create backup filename with timestamp
	timestamp := time.Now().Format("20060102_150405")
	backupPath := fmt.Sprintf("%s.backup.%s", m.dbPath, timestamp)

	// Use VACUUM INTO for consistent backup
	// This ensures the backup is transactionally consistent
	// Use parameter binding to prevent SQL injection (SQLite supports ? for VACUUM INTO in some drivers)
	// However, VACUUM INTO doesn't support parameter binding in go-sqlite3, so we sanitize the path
	sanitizedPath := filepath.Clean(backupPath)
	if sanitizedPath != backupPath {
		return "", fmt.Errorf("invalid backup path (possible security risk): %s", backupPath)
	}

	// Escape single quotes in the path (double them for SQL string literal)
	escapedPath := sanitizedPath
	if len(escapedPath) > 0 {
		escapedPath = ""
		for _, c := range sanitizedPath {
			if c == '\'' {
				escapedPath += "''"
			} else {
				escapedPath += string(c)
			}
		}
	}

	_, err := m.db.Exec(fmt.Sprintf("VACUUM INTO '%s'", escapedPath))
	if err != nil {
		return "", fmt.Errorf("failed to create database backup: %w", err)
	}

	// Set backup file permissions to 0600
	if err := os.Chmod(backupPath, 0600); err != nil {
		return "", fmt.Errorf("failed to set backup file permissions: %w", err)
	}

	return backupPath, nil
}

// migration represents a single migration file.
type migration struct {
	version int
	name    string
	content string
}

// getMigrationFiles returns all migration files sorted by version.
func (m *sqliteMigrationManager) getMigrationFiles() ([]migration, error) {
	var migrations []migration

	// Read migrations from embedded FS
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("failed to read migrations directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		// Parse version from filename (e.g., "001_init.sql" -> 1)
		var version int
		var name string
		_, err := fmt.Sscanf(entry.Name(), "%03d_%s", &version, &name)
		if err != nil {
			return nil, fmt.Errorf("invalid migration filename: %s", entry.Name())
		}

		// Read migration content
		content, err := fs.ReadFile(migrationsFS, filepath.Join("migrations", entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("failed to read migration %s: %w", entry.Name(), err)
		}

		migrations = append(migrations, migration{
			version: version,
			name:    entry.Name(),
			content: string(content),
		})
	}

	// Sort by version
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].version < migrations[j].version
	})

	return migrations, nil
}

// applyMigration applies a single migration in a transaction.
func (m *sqliteMigrationManager) applyMigration(mig migration) error {
	tx, err := m.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() // Rollback if not committed

	// Execute migration SQL
	if _, err := tx.Exec(mig.content); err != nil {
		return fmt.Errorf("failed to execute migration SQL: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit migration: %w", err)
	}

	return nil
}
