package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	internalengine "github.com/akam1o/arca-router/internal/engine"
	nbgrpc "github.com/akam1o/arca-router/internal/northbound/grpc"
	"github.com/akam1o/arca-router/pkg/auth"
	pkgnetconf "github.com/akam1o/arca-router/pkg/netconf"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s metricsSource) authorizeWebRead(w http.ResponseWriter, r *http.Request) bool {
	_, ok := s.authorizeWebReadRole(w, r)
	return ok
}

func (s metricsSource) writeWebInternalError(w http.ResponseWriter, operation string, err error) {
	s.logWebInternalError(operation, err)
	http.Error(w, webInternalServerErrorMessage, http.StatusInternalServerError)
}

func (s metricsSource) writeWebJSONInternalError(w http.ResponseWriter, operation string, err error) {
	s.logWebInternalError(operation, err)
	writeWebJSONError(w, http.StatusInternalServerError, webInternalServerErrorMessage)
}

func (s metricsSource) writeWebConfigEditError(w http.ResponseWriter, operation string, err error) {
	status, message := webConfigEditErrorResponse(err)
	if status == http.StatusInternalServerError {
		s.writeWebJSONInternalError(w, operation, err)
		return
	}
	writeWebJSONError(w, status, message)
}

func webConfigEditErrorResponse(err error) (int, string) {
	if errors.Is(err, errWebConfigAPIUnavailable) {
		return http.StatusServiceUnavailable, errWebConfigAPIUnavailable.Error()
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return http.StatusGatewayTimeout, "configuration operation timed out"
	}
	if grpcStatus := status.Code(err); grpcStatus != codes.Unknown {
		switch grpcStatus {
		case codes.InvalidArgument:
			return http.StatusBadRequest, status.Convert(err).Message()
		case codes.FailedPrecondition, codes.Aborted:
			return http.StatusConflict, "configuration candidate is unavailable"
		case codes.Unavailable:
			return http.StatusServiceUnavailable, "configuration API is unavailable"
		case codes.DeadlineExceeded, codes.Canceled:
			return http.StatusGatewayTimeout, "configuration operation timed out"
		}
	}
	switch {
	case errors.Is(err, nbgrpc.ErrCandidateConflict):
		return http.StatusConflict, "configuration candidate is unavailable"
	case errors.Is(err, nbgrpc.ErrConfigInput), errors.Is(err, internalengine.ErrConfigValidation):
		return http.StatusBadRequest, err.Error()
	}
	message := err.Error()
	switch {
	case webConfigEditLegacyErrorIsBadRequest(message):
		return http.StatusBadRequest, message
	default:
		return http.StatusInternalServerError, webInternalServerErrorMessage
	}
}

func webConfigEditLegacyErrorIsBadRequest(message string) bool {
	return message == "config_text is required"
}

func (s metricsSource) logWebInternalError(operation string, err error) {
	s.webLogger().Error("Web request failed", slog.String("operation", operation), slog.Any("error", err))
}

func (s metricsSource) writeWebAPITokenUnavailable(w http.ResponseWriter, err error) {
	s.webLogger().Warn("Web API token authentication unavailable", slog.Any("error", err))
	http.Error(w, webAPITokenUnavailableMessage, http.StatusInternalServerError)
}

func (s metricsSource) webLogger() *slog.Logger {
	if s.webLog != nil {
		return s.webLog
	}
	return slog.Default()
}

func (s metricsSource) authorizeWebReadRole(w http.ResponseWriter, r *http.Request) (string, bool) {
	users := s.webAuthUsers()
	tokens, err := s.webAutomationTokens()
	if err != nil {
		s.writeWebAPITokenUnavailable(w, err)
		return "", false
	}
	if len(users) == 0 && len(tokens) == 0 {
		return pkgnetconf.RoleReadOnly, true
	}
	_, role, ok := authenticateWebRequest(w, r, users, tokens)
	if !ok {
		return "", false
	}
	if !webRoleCanRead(role) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return "", false
	}
	return role, true
}

func (s metricsSource) authorizeWebAdmin(w http.ResponseWriter, r *http.Request) bool {
	users := s.webAuthUsers()
	tokens, err := s.webAutomationTokens()
	if err != nil {
		s.writeWebAPITokenUnavailable(w, err)
		return false
	}
	if len(users) == 0 && len(tokens) == 0 {
		http.Error(w, "audit export requires password-backed security users or API tokens", http.StatusForbidden)
		return false
	}
	_, role, ok := authenticateWebRequest(w, r, users, tokens)
	if !ok {
		return false
	}
	if role != pkgnetconf.RoleAdmin {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}
	return true
}

func (s metricsSource) authorizeWebWrite(w http.ResponseWriter, r *http.Request) (string, bool) {
	users := s.webAuthUsers()
	tokens, err := s.webAutomationTokens()
	if err != nil {
		s.writeWebAPITokenUnavailable(w, err)
		return "", false
	}
	if len(users) == 0 && len(tokens) == 0 {
		http.Error(w, "web configuration writes require password-backed security users or API tokens", http.StatusForbidden)
		return "", false
	}
	if !webWriteOriginAllowed(r) {
		http.Error(w, "cross-origin web configuration writes are forbidden", http.StatusForbidden)
		return "", false
	}
	username, role, ok := authenticateWebRequest(w, r, users, tokens)
	if !ok {
		return "", false
	}
	if !webRoleCanWrite(role) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return "", false
	}
	return username, true
}

func webWriteOriginAllowed(r *http.Request) bool {
	if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
		return webURLHostMatchesRequest(origin, r)
	}
	if referer := strings.TrimSpace(r.Header.Get("Referer")); referer != "" {
		return webURLHostMatchesRequest(referer, r)
	}
	return true
}

func webURLHostMatchesRequest(rawURL string, r *http.Request) bool {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || strings.TrimSpace(r.Host) == "" {
		return false
	}
	return normalizedWebHost(u.Host) == normalizedWebHost(r.Host)
}

func normalizedWebHost(host string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
}

func authenticateWebRequest(w http.ResponseWriter, r *http.Request, users map[string]webAuthUser, tokens map[string]webAPIToken) (string, string, bool) {
	if _, hasToken := webRequestToken(r); hasToken {
		username, role, ok := authenticateWebToken(r, tokens)
		if !ok {
			writeWebAuthChallenge(w)
			return "", "", false
		}
		return username, role, true
	}
	if len(users) > 0 {
		return authenticateWebUser(w, r, users)
	}
	writeWebAuthChallenge(w)
	return "", "", false
}

func authenticateWebUser(w http.ResponseWriter, r *http.Request, users map[string]webAuthUser) (string, string, bool) {
	username, password, ok := r.BasicAuth()
	if !ok {
		writeWebAuthChallenge(w)
		return "", "", false
	}

	user, found := users[username]
	passwordHash := webDummyPasswordHash
	if found {
		passwordHash = user.PasswordHash
	}
	valid, err := auth.VerifyPassword(password, passwordHash)
	if err != nil || !found || !valid {
		writeWebAuthChallenge(w)
		return "", "", false
	}
	return username, user.Role, true
}

func authenticateWebToken(r *http.Request, tokens map[string]webAPIToken) (string, string, bool) {
	presented, ok := webRequestToken(r)
	if !ok {
		return "", "", false
	}
	var matchedName string
	var matchedToken webAPIToken
	matched := false
	for name, token := range tokens {
		if token.Token == "" && len(token.TokenSHA256) != sha256.Size {
			continue
		}
		if webAPITokenMatches(presented, token) {
			matchedName = name
			matchedToken = token
			matched = true
		}
	}
	if !matched {
		return "", "", false
	}
	role := strings.TrimSpace(matchedToken.Role)
	if role == "" {
		role = pkgnetconf.RoleReadOnly
	}
	username := strings.TrimSpace(matchedToken.Name)
	if username == "" {
		username = matchedName
	}
	return username, role, true
}

func webAPITokenMatches(presented string, token webAPIToken) bool {
	if !token.NotAfter.IsZero() && !time.Now().Before(token.NotAfter) {
		return false
	}
	if len(token.TokenSHA256) == sha256.Size {
		presentedDigest := sha256.Sum256([]byte(presented))
		return subtle.ConstantTimeCompare(presentedDigest[:], token.TokenSHA256) == 1
	}
	return token.Token != "" && constantTimeWebTokenEqual(presented, token.Token)
}

func constantTimeWebTokenEqual(a, b string) bool {
	aDigest := sha256.Sum256([]byte(a))
	bDigest := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(aDigest[:], bDigest[:]) == 1
}

func webRequestToken(r *http.Request) (string, bool) {
	if authz := strings.TrimSpace(r.Header.Get("Authorization")); authz != "" {
		scheme, value, ok := strings.Cut(authz, " ")
		if ok && strings.EqualFold(scheme, "Bearer") {
			token := strings.TrimSpace(value)
			return token, token != ""
		}
	}
	if token := strings.TrimSpace(r.Header.Get("X-API-Key")); token != "" {
		return token, true
	}
	return "", false
}

func (s metricsSource) webAutomationTokens() (map[string]webAPIToken, error) {
	if s.webAPITokenCache != nil {
		return s.webAPITokenCache.tokensForRequest()
	}
	if path := strings.TrimSpace(s.webAPITokenFile); path != "" {
		tokens, err := loadWebAPITokens(path)
		if err != nil {
			return nil, err
		}
		return tokens, nil
	}
	if len(s.webAPITokens) == 0 {
		return nil, nil
	}
	return s.webAPITokens, nil
}

func (s metricsSource) webAuthUsers() map[string]webAuthUser {
	if s.engine == nil {
		return nil
	}
	snap := s.engine.RunningSnapshot()
	if snap == nil || snap.Config == nil || snap.Config.Security == nil {
		return nil
	}
	users := make(map[string]webAuthUser, len(snap.Config.Security.Users))
	for username, user := range snap.Config.Security.Users {
		if user == nil || user.Password == "" {
			continue
		}
		role := strings.TrimSpace(user.Role)
		if role == "" {
			role = pkgnetconf.RoleReadOnly
		}
		users[username] = webAuthUser{
			PasswordHash: user.Password,
			Role:         role,
		}
	}
	if len(users) == 0 {
		return nil
	}
	return users
}

func webRoleCanRead(role string) bool {
	switch role {
	case pkgnetconf.RoleReadOnly, pkgnetconf.RoleOperator, pkgnetconf.RoleAdmin:
		return true
	default:
		return false
	}
}

func webRoleCanWrite(role string) bool {
	switch role {
	case pkgnetconf.RoleOperator, pkgnetconf.RoleAdmin:
		return true
	default:
		return false
	}
}
