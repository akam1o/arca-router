package netconf

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/akam1o/arca-router/pkg/audit"
	"github.com/akam1o/arca-router/pkg/auth"
	"github.com/akam1o/arca-router/pkg/logger"
)

const (
	userDBFilePerms os.FileMode = 0600
	userDBDirPerms  os.FileMode = 0750
)

// dummyPasswordHash is used when authentication must spend comparable work
// without depending on a real user's stored hash.
const dummyPasswordHash = "$argon2id$v=19$m=65536,t=3,p=4$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

var verifyPasswordHash = auth.VerifyPassword

// Role constants for user authorization
const (
	RoleAdmin    = "admin"
	RoleOperator = "operator"
	RoleReadOnly = "read-only"
)

// UserDatabase manages user authentication data
type UserDatabase struct {
	db          *sql.DB
	path        string
	log         *logger.Logger
	auditLogger *audit.Logger // Optional: for audit trail to datastore
}

// User represents a user account
type User struct {
	Username     string
	PasswordHash string
	Role         string // RoleAdmin, RoleOperator, or RoleReadOnly
	Enabled      bool
	CreatedAt    int64
	UpdatedAt    int64
}

// NewUserDatabase creates a new user database connection
func NewUserDatabase(path string, log *logger.Logger) (*UserDatabase, error) {
	if log == nil {
		log = logger.New("netconf-userdb", logger.DefaultConfig())
	}

	if err := prepareSecureUserDatabaseFile(path); err != nil {
		return nil, err
	}

	// Open database
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pooling
	db.SetMaxOpenConns(10)           // Maximum number of open connections
	db.SetMaxIdleConns(5)            // Maximum number of idle connections
	db.SetConnMaxLifetime(time.Hour) // Maximum connection lifetime

	// Set SQLite pragmas
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
	}
	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			if closeErr := db.Close(); closeErr != nil {
				_ = closeErr
			}
			return nil, fmt.Errorf("failed to set pragma: %w", err)
		}
	}

	udb := &UserDatabase{
		db:   db,
		path: path,
		log:  log,
	}

	// Initialize schema
	if err := udb.Initialize(); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			_ = closeErr
		}
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}
	if err := restrictUserDatabaseFiles(path); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			_ = closeErr
		}
		return nil, err
	}

	return udb, nil
}

func prepareSecureUserDatabaseFile(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, userDBDirPerms); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	if err := validateUserDatabaseDirectoryPermissions(dir); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, userDBFilePerms)
	if err != nil {
		return fmt.Errorf("failed to create user database file: %w", err)
	}
	if closeErr := file.Close(); closeErr != nil {
		return fmt.Errorf("failed to close user database file: %w", closeErr)
	}
	if err := os.Chmod(path, userDBFilePerms); err != nil {
		return fmt.Errorf("failed to restrict user database file permissions: %w", err)
	}
	return nil
}

func validateUserDatabaseDirectoryPermissions(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("failed to stat user database directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("user database parent path is not a directory: %s", dir)
	}
	perms := info.Mode().Perm()
	if perms&0022 != 0 {
		return fmt.Errorf("insecure permissions on user database directory %s: mode=%04o", dir, perms)
	}
	return nil
}

func restrictUserDatabaseFiles(path string) error {
	for _, filePath := range []string{path, path + "-wal", path + "-shm"} {
		if _, err := os.Stat(filePath); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("failed to stat user database file %s: %w", filePath, err)
		}
		if err := os.Chmod(filePath, userDBFilePerms); err != nil {
			return fmt.Errorf("failed to restrict user database file permissions for %s: %w", filePath, err)
		}
	}
	return nil
}

// Initialize initializes the database schema
func (udb *UserDatabase) Initialize() error {
	db, err := udb.database()
	if err != nil {
		return err
	}

	schema := `
	CREATE TABLE IF NOT EXISTS users (
		username      TEXT PRIMARY KEY,
		password_hash TEXT NOT NULL,
		role          TEXT NOT NULL CHECK(role IN ('admin', 'operator', 'read-only')),
		created_at    INTEGER NOT NULL,
		updated_at    INTEGER NOT NULL,
		enabled       INTEGER NOT NULL DEFAULT 1
	);

	CREATE INDEX IF NOT EXISTS idx_users_enabled ON users(enabled);

	CREATE TABLE IF NOT EXISTS user_public_keys (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		username    TEXT NOT NULL,
		algorithm   TEXT NOT NULL,
		key_data    TEXT NOT NULL,
		fingerprint TEXT NOT NULL UNIQUE,
		comment     TEXT,
		enabled     INTEGER NOT NULL DEFAULT 1,
		created_at  INTEGER NOT NULL,
		FOREIGN KEY (username) REFERENCES users(username) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_public_keys_username ON user_public_keys(username);
	CREATE INDEX IF NOT EXISTS idx_public_keys_fingerprint ON user_public_keys(fingerprint);
	CREATE INDEX IF NOT EXISTS idx_public_keys_enabled ON user_public_keys(enabled);
	`

	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	udb.safeLog().Info("User database initialized", "path", udb.path)
	return nil
}

func (udb *UserDatabase) database() (*sql.DB, error) {
	if udb == nil || udb.db == nil {
		return nil, fmt.Errorf("database connection is nil")
	}
	return udb.db, nil
}

func (udb *UserDatabase) safeLog() *logger.Logger {
	if udb != nil && udb.log != nil {
		return udb.log
	}
	return logger.New("netconf-userdb", logger.DefaultConfig())
}

// CreateUser creates a new user
func (udb *UserDatabase) CreateUser(username, passwordHash, role string) error {
	if username == "" || passwordHash == "" || role == "" {
		return fmt.Errorf("username, password_hash, and role are required")
	}

	// Validate role using constants
	if role != RoleAdmin && role != RoleOperator && role != RoleReadOnly {
		return fmt.Errorf("invalid role: %s (must be %s, %s, or %s)", role, RoleAdmin, RoleOperator, RoleReadOnly)
	}
	if err := validateStoredPasswordHash(passwordHash); err != nil {
		return err
	}

	db, err := udb.database()
	if err != nil {
		return err
	}

	now := time.Now().Unix()
	query := `INSERT INTO users (username, password_hash, role, created_at, updated_at, enabled)
	          VALUES (?, ?, ?, ?, ?, 1)`

	_, err = db.Exec(query, username, passwordHash, role, now, now)
	if err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}

	udb.safeLog().Info("User created", "username", username, "role", role)
	return nil
}

func validateStoredPasswordHash(passwordHash string) error {
	if err := auth.ValidatePasswordHash(passwordHash); err != nil {
		return fmt.Errorf("invalid password_hash: %w", err)
	}
	return nil
}

// GetUser retrieves a user by username
func (udb *UserDatabase) GetUser(username string) (*User, error) {
	db, err := udb.database()
	if err != nil {
		return nil, err
	}

	query := `SELECT username, password_hash, role, created_at, updated_at, enabled
	          FROM users WHERE username = ?`

	var user User
	var enabled int
	err = db.QueryRow(query, username).Scan(
		&user.Username,
		&user.PasswordHash,
		&user.Role,
		&user.CreatedAt,
		&user.UpdatedAt,
		&enabled,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("user not found: %s", username)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	user.Enabled = enabled == 1
	return &user, nil
}

// UpdateUser updates a user's information
func (udb *UserDatabase) UpdateUser(username, passwordHash, role string, enabled bool) error {
	// Validate role using constants
	if role != "" && role != RoleAdmin && role != RoleOperator && role != RoleReadOnly {
		return fmt.Errorf("invalid role: %s (must be %s, %s, or %s)", role, RoleAdmin, RoleOperator, RoleReadOnly)
	}
	if passwordHash != "" {
		if err := validateStoredPasswordHash(passwordHash); err != nil {
			return err
		}
	}

	db, err := udb.database()
	if err != nil {
		return err
	}

	// Build update query dynamically
	query := "UPDATE users SET updated_at = ?"
	args := []interface{}{time.Now().Unix()}

	if passwordHash != "" {
		query += ", password_hash = ?"
		args = append(args, passwordHash)
	}
	if role != "" {
		query += ", role = ?"
		args = append(args, role)
	}
	query += ", enabled = ?"
	args = append(args, boolToInt(enabled))

	query += " WHERE username = ?"
	args = append(args, username)

	result, err := db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("user not found: %s", username)
	}

	udb.safeLog().Info("User updated", "username", username)
	return nil
}

// DeleteUser deletes a user
func (udb *UserDatabase) DeleteUser(username string) error {
	db, err := udb.database()
	if err != nil {
		return err
	}

	query := "DELETE FROM users WHERE username = ?"
	result, err := db.Exec(query, username)
	if err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("user not found: %s", username)
	}

	udb.safeLog().Info("User deleted", "username", username)
	return nil
}

// ListUsers lists all users (without password hashes)
// Use limit=0 and offset=0 to get all users (backward compatible)
func (udb *UserDatabase) ListUsers() ([]User, error) {
	return udb.ListUsersPaginated(0, 0)
}

// ListUsersPaginated lists users with pagination support
// limit=0 means no limit, offset=0 means start from beginning
func (udb *UserDatabase) ListUsersPaginated(limit, offset int) ([]User, error) {
	db, err := udb.database()
	if err != nil {
		return nil, err
	}

	query := `SELECT username, role, created_at, updated_at, enabled
	          FROM users ORDER BY username`

	var args []interface{}
	if limit > 0 {
		query += " LIMIT ? OFFSET ?"
		args = append(args, limit, offset)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list users: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			_ = err
		}
	}()

	var users []User
	for rows.Next() {
		var user User
		var enabled int
		if err := rows.Scan(&user.Username, &user.Role, &user.CreatedAt, &user.UpdatedAt, &enabled); err != nil {
			return nil, fmt.Errorf("failed to scan user: %w", err)
		}
		user.Enabled = enabled == 1
		users = append(users, user)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate users: %w", err)
	}

	return users, nil
}

// CountUsers returns the total number of users
func (udb *UserDatabase) CountUsers() (int, error) {
	db, err := udb.database()
	if err != nil {
		return 0, err
	}

	var count int
	query := "SELECT COUNT(*) FROM users"
	err = db.QueryRow(query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count users: %w", err)
	}
	return count, nil
}

// VerifyPassword verifies a user's password
// Returns generic "authentication failed" error to prevent user enumeration attacks
func (udb *UserDatabase) VerifyPassword(username, password string) (*User, error) {
	log := udb.safeLog()

	// Get user from database
	user, err := udb.GetUser(username)
	if err != nil {
		_, _ = verifyPasswordHash(password, dummyPasswordHash)
		// Log for audit but return generic error to prevent user enumeration
		log.Warn("Authentication failed", "username", username, "reason", "user_not_found")
		return nil, fmt.Errorf("authentication failed")
	}

	userDisabled := !user.Enabled
	hashToVerify := user.PasswordHash
	if userDisabled {
		hashToVerify = dummyPasswordHash
	}

	// Verify password (constant-time comparison)
	valid, err := verifyPasswordHash(password, hashToVerify)
	if err != nil {
		log.Warn("Authentication failed", "username", username, "reason", "password_verification_error", "error", err)
		return nil, fmt.Errorf("authentication failed")
	}

	if userDisabled {
		// Log for audit but return generic error
		log.Warn("Authentication failed", "username", username, "reason", "user_disabled")
		return nil, fmt.Errorf("authentication failed")
	}

	if !valid {
		log.Warn("Authentication failed", "username", username, "reason", "invalid_password")
		return nil, fmt.Errorf("authentication failed")
	}

	// Success: log and return user (without password hash for security)
	log.Info("Authentication successful", "username", username, "role", user.Role)
	user.PasswordHash = ""
	return user, nil
}

// VerifyPasswordWithReason verifies a user's password and returns a detailed reason for failure
// Used by SSH authentication callback for audit logging
// Implements timing attack mitigation by performing dummy hash verification for non-existent users
func (udb *UserDatabase) VerifyPasswordWithReason(username, password string) (*User, string, error) {
	// Get user from database
	user, err := udb.GetUser(username)
	if err != nil {
		// Perform dummy verification to maintain constant timing
		_, _ = verifyPasswordHash(password, dummyPasswordHash)
		return nil, "user_not_found", fmt.Errorf("authentication failed")
	}

	// Check if user is enabled (before password verification to maintain timing)
	userDisabled := !user.Enabled
	hashToVerify := user.PasswordHash
	if userDisabled {
		// Use dummy hash to maintain timing even for disabled users
		hashToVerify = dummyPasswordHash
	}

	// Verify password (constant-time comparison)
	valid, err := verifyPasswordHash(password, hashToVerify)
	if err != nil {
		return nil, "password_verification_error", fmt.Errorf("authentication failed")
	}

	// Check disabled status after verification (timing-safe)
	if userDisabled {
		return nil, "user_disabled", fmt.Errorf("authentication failed")
	}

	if !valid {
		return nil, "invalid_password", fmt.Errorf("authentication failed")
	}

	// Success: return user (without password hash for security)
	user.PasswordHash = ""
	return user, "", nil
}

// SetAuditLogger sets the audit logger for persistent audit trail
func (udb *UserDatabase) SetAuditLogger(logger *audit.Logger) {
	if udb == nil {
		return
	}
	udb.auditLogger = logger
}

// LogAuthSuccess logs a successful authentication event
func (udb *UserDatabase) LogAuthSuccess(username, sourceIP string) {
	udb.LogAuthSuccessWithMethod(username, sourceIP, "password")
}

// LogAuthSuccessWithMethod logs a successful authentication event with specified method
func (udb *UserDatabase) LogAuthSuccessWithMethod(username, sourceIP, method string) {
	if udb == nil {
		return
	}
	log := udb.log
	if log == nil {
		log = logger.New("netconf-userdb", logger.DefaultConfig())
	}

	// Log to structured logger (real-time monitoring)
	log.Info("Authentication successful",
		"event_type", "auth_success",
		"username", username,
		"source_ip", sourceIP,
		"method", method,
		"timestamp", time.Now().Format(time.RFC3339))

	// Log to audit datastore (persistent audit trail)
	if udb.auditLogger != nil {
		ctx := context.Background()
		if err := udb.auditLogger.LogAuthSuccess(ctx, username, sourceIP, method); err != nil {
			log.Warn("Failed to log auth success to audit datastore",
				"username", username,
				"error", err)
		}
	}
}

// LogAuthFailure logs a failed authentication event
func (udb *UserDatabase) LogAuthFailure(username, sourceIP, reason string) {
	udb.LogAuthFailureWithMethod(username, sourceIP, "password", reason)
}

// LogAuthFailureWithMethod logs a failed authentication event with specified method
func (udb *UserDatabase) LogAuthFailureWithMethod(username, sourceIP, method, reason string) {
	if udb == nil {
		return
	}
	log := udb.log
	if log == nil {
		log = logger.New("netconf-userdb", logger.DefaultConfig())
	}

	// Log to structured logger (real-time monitoring)
	log.Warn("Authentication failed",
		"event_type", "auth_failure",
		"username", username,
		"source_ip", sourceIP,
		"method", method,
		"reason", reason,
		"timestamp", time.Now().Format(time.RFC3339))

	// Log to audit datastore (persistent audit trail)
	if udb.auditLogger != nil {
		ctx := context.Background()
		if err := udb.auditLogger.LogAuthFailure(ctx, username, sourceIP, method, reason); err != nil {
			log.Warn("Failed to log auth failure to audit datastore",
				"username", username,
				"error", err)
		}
	}
}

// HealthCheck verifies the database connection is healthy
func (udb *UserDatabase) HealthCheck() error {
	db, err := udb.database()
	if err != nil {
		return err
	}

	// Ping the database to check connectivity
	if err := db.Ping(); err != nil {
		return fmt.Errorf("database ping failed: %w", err)
	}

	// Verify we can query the schema
	query := "SELECT name FROM sqlite_master WHERE type='table' AND name='users'"
	var tableName string
	err = db.QueryRow(query).Scan(&tableName)
	if err != nil {
		return fmt.Errorf("failed to verify schema: %w", err)
	}

	return nil
}

// Close closes the database connection
func (udb *UserDatabase) Close() error {
	if udb == nil {
		return nil
	}
	if udb.db != nil {
		return udb.db.Close()
	}
	return nil
}

// boolToInt converts bool to int for SQLite
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// AddPublicKey adds a public key for a user
func (udb *UserDatabase) AddPublicKey(username, algorithm, keyData, fingerprint, comment string) error {
	if username == "" || algorithm == "" || keyData == "" || fingerprint == "" {
		return fmt.Errorf("username, algorithm, key_data, and fingerprint are required")
	}
	db, err := udb.database()
	if err != nil {
		return err
	}

	// Verify user exists
	if _, err := udb.GetUser(username); err != nil {
		return fmt.Errorf("user not found: %s", username)
	}

	now := time.Now().Unix()
	query := `INSERT INTO user_public_keys (username, algorithm, key_data, fingerprint, comment, enabled, created_at)
	          VALUES (?, ?, ?, ?, ?, 1, ?)`

	_, err = db.Exec(query, username, algorithm, keyData, fingerprint, comment, now)
	if err != nil {
		return fmt.Errorf("failed to add public key: %w", err)
	}

	udb.safeLog().Info("Public key added", "username", username, "fingerprint", fingerprint, "algorithm", algorithm)
	return nil
}

// GetPublicKey retrieves a specific public key by fingerprint
func (udb *UserDatabase) GetPublicKey(fingerprint string) (*PublicKeyRecord, error) {
	db, err := udb.database()
	if err != nil {
		return nil, err
	}

	query := `SELECT id, username, algorithm, key_data, fingerprint, comment, enabled, created_at
	          FROM user_public_keys WHERE fingerprint = ?`

	var record PublicKeyRecord
	var enabled int
	err = db.QueryRow(query, fingerprint).Scan(
		&record.ID,
		&record.Username,
		&record.Algorithm,
		&record.KeyData,
		&record.Fingerprint,
		&record.Comment,
		&enabled,
		&record.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("public key not found: %s", fingerprint)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get public key: %w", err)
	}

	record.Enabled = enabled == 1
	return &record, nil
}

// ListPublicKeys lists all public keys for a user
func (udb *UserDatabase) ListPublicKeys(username string) ([]PublicKeyRecord, error) {
	db, err := udb.database()
	if err != nil {
		return nil, err
	}

	query := `SELECT id, username, algorithm, key_data, fingerprint, comment, enabled, created_at
	          FROM user_public_keys WHERE username = ? ORDER BY created_at DESC, fingerprint ASC`

	rows, err := db.Query(query, username)
	if err != nil {
		return nil, fmt.Errorf("failed to list public keys: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			_ = err
		}
	}()

	var keys []PublicKeyRecord
	for rows.Next() {
		var record PublicKeyRecord
		var enabled int
		if err := rows.Scan(&record.ID, &record.Username, &record.Algorithm, &record.KeyData,
			&record.Fingerprint, &record.Comment, &enabled, &record.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan public key: %w", err)
		}
		record.Enabled = enabled == 1
		keys = append(keys, record)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate public keys: %w", err)
	}

	return keys, nil
}

// RemovePublicKey removes a public key by fingerprint
func (udb *UserDatabase) RemovePublicKey(fingerprint string) error {
	db, err := udb.database()
	if err != nil {
		return err
	}

	query := "DELETE FROM user_public_keys WHERE fingerprint = ?"
	result, err := db.Exec(query, fingerprint)
	if err != nil {
		return fmt.Errorf("failed to remove public key: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("public key not found: %s", fingerprint)
	}

	udb.safeLog().Info("Public key removed", "fingerprint", fingerprint)
	return nil
}

// UpdatePublicKeyStatus updates the enabled status of a public key
func (udb *UserDatabase) UpdatePublicKeyStatus(fingerprint string, enabled bool) error {
	db, err := udb.database()
	if err != nil {
		return err
	}

	query := "UPDATE user_public_keys SET enabled = ? WHERE fingerprint = ?"
	result, err := db.Exec(query, boolToInt(enabled), fingerprint)
	if err != nil {
		return fmt.Errorf("failed to update public key status: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("public key not found: %s", fingerprint)
	}

	udb.safeLog().Info("Public key status updated", "fingerprint", fingerprint, "enabled", enabled)
	return nil
}

// VerifyPublicKeyAuth verifies public key authentication for a user
// Returns the user and reason string (empty if successful)
func (udb *UserDatabase) VerifyPublicKeyAuth(username, keyData string) (*User, string, error) {
	// Get user from database
	user, err := udb.GetUser(username)
	if err != nil {
		return nil, "user_not_found", fmt.Errorf("authentication failed")
	}
	db, err := udb.database()
	if err != nil {
		return nil, "user_not_found", fmt.Errorf("authentication failed")
	}

	// Check if user is enabled
	if !user.Enabled {
		return nil, "user_disabled", fmt.Errorf("authentication failed")
	}

	// Find matching public key
	query := `SELECT fingerprint FROM user_public_keys
	          WHERE username = ? AND key_data = ? AND enabled = 1`

	var fingerprint string
	err = db.QueryRow(query, username, keyData).Scan(&fingerprint)
	if err == sql.ErrNoRows {
		return nil, "key_not_found", fmt.Errorf("authentication failed")
	}
	if err != nil {
		return nil, "key_verification_error", fmt.Errorf("authentication failed")
	}

	// Success: return user (without password hash for security)
	user.PasswordHash = ""
	return user, "", nil
}

// PublicKeyRecord represents a stored public key record
type PublicKeyRecord struct {
	ID          int64
	Username    string
	Algorithm   string
	KeyData     string
	Fingerprint string
	Comment     string
	Enabled     bool
	CreatedAt   int64
}
