// Package auth provides authentication and authorization for the unified daemon.
// It wraps the existing pkg/auth and pkg/audit packages under a unified interface.
package auth

import (
	"context"
	"time"

	pkgaudit "github.com/akam1o/arca-router/pkg/audit"
	pkgauth "github.com/akam1o/arca-router/pkg/auth"
)

// Authenticator handles user authentication.
type Authenticator struct{}

// NewAuthenticator creates a new authenticator.
func NewAuthenticator() *Authenticator {
	return &Authenticator{}
}

// VerifyPassword checks a password against an argon2id hash.
func (a *Authenticator) VerifyPassword(password, hash string) (bool, error) {
	return pkgauth.VerifyPassword(password, hash)
}

// HashPassword creates an argon2id hash of a password.
func (a *Authenticator) HashPassword(password string) (string, error) {
	return pkgauth.HashPassword(password)
}

// Role constants for RBAC.
const (
	RoleAdmin    = "admin"
	RoleOperator = "operator"
	RoleReadOnly = "read-only"
)

// Authorizer checks if a role is permitted to perform an operation.
type Authorizer struct{}

// NewAuthorizer creates a new authorizer.
func NewAuthorizer() *Authorizer {
	return &Authorizer{}
}

// operationPermissions maps operations to the minimum required role.
var operationPermissions = map[string][]string{
	"get-config":      {RoleReadOnly, RoleOperator, RoleAdmin},
	"get":             {RoleReadOnly, RoleOperator, RoleAdmin},
	"edit-config":     {RoleOperator, RoleAdmin},
	"lock":            {RoleOperator, RoleAdmin},
	"unlock":          {RoleOperator, RoleAdmin},
	"commit":          {RoleOperator, RoleAdmin},
	"discard-changes": {RoleOperator, RoleAdmin},
	"validate":        {RoleOperator, RoleAdmin},
	"copy-config":     {RoleOperator, RoleAdmin},
	"close-session":   {RoleOperator, RoleAdmin},
	"kill-session":    {RoleAdmin},
}

// IsPermitted checks if a role is allowed to perform an operation.
func (a *Authorizer) IsPermitted(role, operation string) bool {
	allowed, ok := operationPermissions[operation]
	if !ok {
		return false
	}
	for _, r := range allowed {
		if r == role {
			return true
		}
	}
	return false
}

// AuditLogger wraps the existing pkg/audit logger for the new architecture.
type AuditLogger struct {
	logger *pkgaudit.Logger
}

// NewAuditLogger creates a new audit logger.
func NewAuditLogger(logger *pkgaudit.Logger) *AuditLogger {
	return &AuditLogger{logger: logger}
}

// LogAuth logs an authentication event.
func (l *AuditLogger) LogAuth(ctx context.Context, user, sourceIP string, success bool) error {
	if success {
		return l.logger.LogAuthSuccess(ctx, user, sourceIP, "password")
	}
	return l.logger.Log(ctx, &pkgaudit.Event{
		Timestamp: time.Now(),
		EventType: pkgaudit.EventAuthFailure,
		User:      user,
		SourceIP:  sourceIP,
		Result:    pkgaudit.ResultFailure,
	})
}

// LogCommit logs a configuration commit event.
func (l *AuditLogger) LogCommit(ctx context.Context, user, sessionID, commitID string) error {
	return l.logger.Log(ctx, &pkgaudit.Event{
		Timestamp: time.Now(),
		EventType: pkgaudit.EventCommit,
		User:      user,
		SessionID: sessionID,
		Result:    pkgaudit.ResultSuccess,
		Details:   map[string]interface{}{"commit_id": commitID},
	})
}
