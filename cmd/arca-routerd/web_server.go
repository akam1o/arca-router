package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/akam1o/arca-router/internal/model"
	"github.com/akam1o/arca-router/pkg/auth"
	"github.com/akam1o/arca-router/pkg/logger"
	"github.com/akam1o/arca-router/pkg/security"
)

func effectiveWebListen(flagValue string, snapshot *model.ConfigSnapshot) string {
	if listen := strings.TrimSpace(flagValue); listen != "" {
		return listen
	}
	if snapshot == nil || snapshot.Config == nil || snapshot.Config.System == nil ||
		snapshot.Config.System.Services == nil || snapshot.Config.System.Services.WebUI == nil {
		return ""
	}
	web := snapshot.Config.System.Services.WebUI
	if !web.Enabled {
		return ""
	}
	addr := strings.TrimSpace(web.ListenAddress)
	if addr == "" {
		addr = "127.0.0.1"
	}
	port := web.Port
	if port == 0 {
		port = defaultWebUIPort
	}
	return net.JoinHostPort(addr, strconv.Itoa(port))
}

func webPlainHTTPListenAllowed(listenAddr string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(listenAddr))
	if err != nil {
		return false
	}
	host = strings.TrimSpace(host)
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func newWebAPITokenCache(path string, tokens map[string]webAPIToken) *webAPITokenCache {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	return &webAPITokenCache{
		path:   path,
		tokens: tokens,
	}
}

func (c *webAPITokenCache) tokensForRequest() (map[string]webAPIToken, error) {
	if c == nil {
		return nil, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	info, err := os.Stat(c.path)
	if err != nil {
		return nil, fmt.Errorf("stat token file %s: %w", c.path, err)
	}
	if sameWebAPITokenFile(c.fileInfo, info) {
		if c.loadErr != nil {
			return nil, c.loadErr
		}
		if c.tokens != nil {
			return c.tokens, nil
		}
	}
	tokens, err := loadWebAPITokens(c.path)
	c.fileInfo = info
	if err != nil {
		c.loadErr = err
		return nil, err
	}
	c.tokens = tokens
	c.loadErr = nil
	return tokens, nil
}

func sameWebAPITokenFile(previous, current os.FileInfo) bool {
	if previous == nil || current == nil {
		return false
	}
	return os.SameFile(previous, current) &&
		previous.Size() == current.Size() &&
		previous.Mode() == current.Mode() &&
		previous.ModTime().Equal(current.ModTime())
}

func loadWebAPITokens(path string) (map[string]webAPIToken, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}
	if err := auth.ValidateKeyFilePermissions(path, 0, 0); err != nil {
		return nil, fmt.Errorf("validate token file permissions: %w", err)
	}
	data, err := auth.ReadSecretFile(path)
	if err != nil {
		return nil, fmt.Errorf("read token file %s: %w", path, err)
	}
	tokens := make(map[string]webAPIToken)
	tokenValues := make(map[string]string)
	for lineNo, rawLine := range strings.Split(string(data), "\n") {
		token, ok, err := parseWebAPITokenLine(rawLine, lineNo+1)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if _, exists := tokens[token.Name]; exists {
			return nil, fmt.Errorf("duplicate web API token name %q on line %d", token.Name, lineNo+1)
		}
		tokenFingerprint := webAPITokenFingerprint(token)
		if existingName, exists := tokenValues[tokenFingerprint]; exists {
			return nil, fmt.Errorf("duplicate web API token value on line %d: already used by token %q", lineNo+1, existingName)
		}
		tokens[token.Name] = token
		tokenValues[tokenFingerprint] = token.Name
	}
	if len(tokens) == 0 {
		return nil, fmt.Errorf("web API token file %s does not contain any tokens", path)
	}
	return tokens, nil
}

func parseWebAPITokenLine(rawLine string, lineNo int) (webAPIToken, bool, error) {
	line := strings.TrimSpace(rawLine)
	if line == "" || strings.HasPrefix(line, "#") {
		return webAPIToken{}, false, nil
	}
	parts := strings.SplitN(line, ":", 3)
	if len(parts) != 3 {
		return webAPIToken{}, false, fmt.Errorf("invalid web API token file line %d: expected name:role:token", lineNo)
	}
	token := webAPIToken{
		Name: strings.TrimSpace(parts[0]),
		Role: strings.TrimSpace(parts[1]),
	}
	rawToken := strings.TrimSpace(parts[2])
	if token.Name == "" {
		return webAPIToken{}, false, fmt.Errorf("invalid web API token file line %d: token name is required", lineNo)
	}
	if !webRoleCanRead(token.Role) {
		return webAPIToken{}, false, fmt.Errorf("invalid web API token file line %d: invalid role %q", lineNo, token.Role)
	}
	if rawToken == "" {
		return webAPIToken{}, false, fmt.Errorf("invalid web API token file line %d: token value is required", lineNo)
	}
	tokenValue, tokenSHA256, notAfter, err := parseWebAPITokenValue(rawToken)
	if err != nil {
		return webAPIToken{}, false, fmt.Errorf("invalid web API token file line %d: %w", lineNo, err)
	}
	token.Token = tokenValue
	token.TokenSHA256 = tokenSHA256
	token.NotAfter = notAfter
	return token, true, nil
}

func parseWebAPITokenValue(rawToken string) (string, []byte, time.Time, error) {
	if strings.HasPrefix(strings.ToLower(rawToken), webAPITokenSHA256Prefix) {
		rawHash, notAfter, err := parseWebAPITokenHash(rawToken)
		if err != nil {
			return "", nil, time.Time{}, err
		}
		tokenSHA256, err := hex.DecodeString(rawHash)
		if err != nil || len(tokenSHA256) != sha256.Size {
			return "", nil, time.Time{}, fmt.Errorf("web API token hash must be sha256:%d hex characters", sha256.Size*2)
		}
		return "", tokenSHA256, notAfter, nil
	}
	if err := security.ValidateWebAPIToken(rawToken); err != nil {
		return "", nil, time.Time{}, err
	}
	tokenSHA256 := sha256.Sum256([]byte(rawToken))
	return rawToken, tokenSHA256[:], time.Time{}, nil
}

func parseWebAPITokenHash(rawToken string) (string, time.Time, error) {
	raw := strings.TrimSpace(rawToken[len(webAPITokenSHA256Prefix):])
	if len(raw) < sha256.Size*2 {
		return "", time.Time{}, fmt.Errorf("web API token hash must be sha256:%d hex characters", sha256.Size*2)
	}
	rawHash := raw[:sha256.Size*2]
	suffix := raw[sha256.Size*2:]
	if suffix == "" {
		return rawHash, time.Time{}, nil
	}
	if !strings.HasPrefix(suffix, webAPITokenNotAfterPrefix) {
		return "", time.Time{}, fmt.Errorf("web API token hash suffix must be :not-after=<RFC3339>")
	}
	notAfter, err := time.Parse(time.RFC3339, strings.TrimPrefix(suffix, webAPITokenNotAfterPrefix))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("web API token not-after must be RFC3339")
	}
	return rawHash, notAfter, nil
}

func webAPITokenFingerprint(token webAPIToken) string {
	if len(token.TokenSHA256) == sha256.Size {
		return hex.EncodeToString(token.TokenSHA256)
	}
	tokenSHA256 := sha256.Sum256([]byte(token.Token))
	return hex.EncodeToString(tokenSHA256[:])
}

func startWebServer(ctx context.Context, listenAddr string, source metricsSource, log *logger.Logger) (<-chan error, error) {
	errCh, _, err := startWebServerWithShutdown(ctx, listenAddr, source, log)
	return errCh, err
}

func startWebServerWithShutdown(ctx context.Context, listenAddr string, source metricsSource, log *logger.Logger) (<-chan error, func(context.Context) error, error) {
	if !webPlainHTTPListenAllowed(listenAddr) {
		return nil, nil, fmt.Errorf("web endpoint serves plaintext HTTP and must listen on loopback, got %q", listenAddr)
	}
	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return nil, nil, fmt.Errorf("listen web endpoint: %w", err)
	}
	source.webLog = log.Logger

	srv := newObservabilityHTTPServer(newWebMux(source))
	shutdown := srv.Shutdown

	errCh := make(chan error, 1)
	go func() {
		log.Info("Web endpoint started", slog.String("listen", lis.Addr().String()))
		if err := srv.Serve(lis); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdown(shutdownCtx); err != nil {
			log.Error("Web endpoint shutdown failed", slog.Any("error", err))
		}
	}()

	return errCh, shutdown, nil
}

func newWebMux(source metricsSource) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/", source.handleWebIndex)
	mux.HandleFunc("/api/config", source.handleWebConfig)
	mux.HandleFunc("/api/config/commit", source.handleWebConfigCommit)
	mux.HandleFunc("/api/config/history", source.handleWebConfigHistory)
	mux.HandleFunc("/api/audit", source.handleWebAudit)
	mux.HandleFunc("/api/status", source.handleWebStatus)
	mux.HandleFunc("/api/nms/v1/status", source.handleNMSStatus)
	mux.HandleFunc("/api/nms/v1/telemetry/paths", source.handleNMSTelemetryCatalog)
	mux.HandleFunc("/api/nms/v1/telemetry/schemas", source.handleNMSTelemetrySchemas)
	mux.HandleFunc("/api/nms/v1/telemetry/snapshot", source.handleNMSTelemetrySnapshot)
	mux.HandleFunc("/api/config/validate", source.handleWebConfigValidate)
	return mux
}
