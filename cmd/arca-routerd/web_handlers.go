package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/akam1o/arca-router/internal/correlation"
	nbgrpc "github.com/akam1o/arca-router/internal/northbound/grpc"
	pkgconfig "github.com/akam1o/arca-router/pkg/config"
)

func (s metricsSource) handleWebStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.authorizeWebRead(w, r) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(newWebStatus(s.snapshot(time.Now()))); err != nil {
		s.writeWebInternalError(w, "encode status", err)
	}
}

func (s metricsSource) handleNMSStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.authorizeWebRead(w, r) {
		return
	}
	now := time.Now()
	writeWebJSON(w, http.StatusOK, newNMSStatusResponse(now, s.snapshot(now)))
}

func (s metricsSource) handleNMSTelemetryCatalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.authorizeWebRead(w, r) {
		return
	}
	writeWebJSON(w, http.StatusOK, newNMSTelemetryCatalogResponse(time.Now(), nmsTelemetryCatalogFiltersFromRequest(r)))
}

func (s metricsSource) handleNMSTelemetrySchemas(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.authorizeWebRead(w, r) {
		return
	}
	writeWebJSON(w, http.StatusOK, newNMSTelemetrySchemasResponse(time.Now(), nmsTelemetryCatalogFiltersFromRequest(r)))
}

func (s metricsSource) handleNMSTelemetrySnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.authorizeWebRead(w, r) {
		return
	}
	if s.telemetryAPI == nil {
		writeWebJSONError(w, http.StatusServiceUnavailable, "telemetry API is not available")
		return
	}
	opts, err := nmsTelemetrySnapshotOptionsFromRequest(r)
	if err != nil {
		writeWebJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	now := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), opts.timeout)
	defer cancel()
	events, payloadBytes, err := s.collectNMSTelemetrySnapshot(ctx, opts)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case strings.Contains(err.Error(), "unsupported telemetry path"):
			status = http.StatusBadRequest
		case errors.Is(err, errNMSTelemetrySnapshotTooLarge), errors.Is(err, errNMSTelemetrySnapshotTooManyEvents):
			status = http.StatusRequestEntityTooLarge
		case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
			status = http.StatusGatewayTimeout
		}
		if status == http.StatusInternalServerError {
			s.writeWebJSONInternalError(w, "collect telemetry snapshot", err)
			return
		}
		writeWebJSONError(w, status, err.Error())
		return
	}
	writeWebJSON(w, http.StatusOK, newNMSTelemetrySnapshotResponse(now, events, opts, payloadBytes))
}

func (s metricsSource) handleWebConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_, ok := s.authorizeWebReadRole(w, r)
	if !ok {
		return
	}
	cfg, err := s.runningConfig(r.Context(), true)
	if err != nil {
		s.writeWebInternalError(w, "render config", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(cfg); err != nil {
		s.writeWebInternalError(w, "encode config", err)
	}
}

func (s metricsSource) handleWebConfigHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.authorizeWebRead(w, r) {
		return
	}
	limit, offset, err := webHistoryPaginationFromRequest(r)
	if err != nil {
		writeWebJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	history, err := s.configHistory(r.Context(), limit, offset)
	if err != nil {
		s.writeWebJSONInternalError(w, "list config history", err)
		return
	}
	writeWebJSON(w, http.StatusOK, webConfigHistoryResponse{Entries: history})
}

func (s metricsSource) handleWebAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.authorizeWebAdmin(w, r) {
		return
	}
	if s.configAPI == nil {
		writeWebJSONError(w, http.StatusServiceUnavailable, "audit API is not available")
		return
	}
	opts, err := webAuditOptionsFromRequest(r)
	if err != nil {
		writeWebJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	entries, err := s.auditEvents(r.Context(), opts)
	if err != nil {
		s.writeWebJSONInternalError(w, "list audit events", err)
		return
	}
	writeWebJSON(w, http.StatusOK, webAuditResponse{
		SchemaVersion: webAuditSchemaVersion,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		Limit:         opts.Limit,
		Offset:        opts.Offset,
		Count:         len(entries),
		Entries:       entries,
	})
}

func (s metricsSource) handleWebConfigValidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	username, ok := s.authorizeWebWrite(w, r)
	if !ok {
		return
	}
	req, ok := decodeWebConfigEditRequest(w, r)
	if !ok {
		return
	}
	configText, err := s.resolveWebConfigEditText(r.Context(), req.ConfigText)
	if err != nil {
		s.writeWebConfigEditError(w, "resolve redacted config", err)
		return
	}
	diff, hasChanges, err := s.validateWebConfig(r.Context(), username, configText, req.ExpectedBaseVersion)
	if err != nil {
		s.writeWebConfigEditError(w, "validate config", err)
		return
	}
	writeWebJSON(w, http.StatusOK, webConfigValidateResponse{
		Valid:      true,
		HasChanges: hasChanges,
		DiffText:   diff,
	})
}

func (s metricsSource) handleWebConfigCommit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	username, ok := s.authorizeWebWrite(w, r)
	if !ok {
		return
	}
	req, ok := decodeWebConfigCommitRequest(w, r)
	if !ok {
		return
	}
	configText, err := s.resolveWebConfigEditText(r.Context(), req.ConfigText)
	if err != nil {
		s.writeWebConfigEditError(w, "resolve redacted config", err)
		return
	}
	ctx, correlationID := webCorrelationContext(r)
	w.Header().Set(correlation.HeaderName, correlationID)
	commitID, version, err := s.commitWebConfig(ctx, username, configText, req.Message, req.ExpectedBaseVersion)
	if err != nil {
		s.writeWebConfigEditError(w, "commit config", err)
		return
	}
	writeWebJSON(w, http.StatusOK, webConfigCommitResponse{
		CommitID: commitID,
		Version:  version,
	})
}

func (s metricsSource) handleWebIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_, ok := s.authorizeWebReadRole(w, r)
	if !ok {
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Method == http.MethodHead {
		return
	}
	status := newWebStatus(s.snapshot(time.Now()))
	cfg, err := s.runningConfig(r.Context(), true)
	if err != nil {
		s.writeWebInternalError(w, "render index config", err)
		return
	}
	history, err := s.configHistory(r.Context(), 5, 0)
	if err != nil {
		s.writeWebInternalError(w, "render index history", err)
		return
	}
	if err := webIndexTemplate.Execute(w, newWebIndexData(status, time.Now(), cfg.ConfigText, history)); err != nil {
		s.writeWebInternalError(w, "render index", err)
	}
}

func (s metricsSource) runningConfig(ctx context.Context, redactSecrets bool) (webConfig, error) {
	if s.configAPI != nil {
		getRunning := s.configAPI.GetRunning
		if !redactSecrets {
			if api, ok := s.configAPI.(webUnredactedConfigAPI); ok {
				getRunning = api.GetRunningUnredacted
			}
		}
		text, version, err := getRunning(ctx)
		if err != nil {
			return webConfig{}, fmt.Errorf("get running config: %w", err)
		}
		return webConfig{
			ConfigText: text,
			Version:    version,
		}, nil
	}
	if s.engine == nil {
		return webConfig{}, nil
	}
	snap := s.engine.RunningSnapshot()
	if snap == nil || snap.Config == nil {
		return webConfig{}, nil
	}
	legacyCfg := snap.Config.ToLegacyConfig()
	var (
		text string
		err  error
	)
	if redactSecrets {
		text, err = pkgconfig.ToSetCommandsRedactedWithError(legacyCfg)
	} else {
		text, err = pkgconfig.ToSetCommandsWithError(legacyCfg)
	}
	if err != nil {
		return webConfig{}, fmt.Errorf("serialize running config: %w", err)
	}
	return webConfig{
		ConfigText: text,
		Version:    snap.Version,
	}, nil
}

func (s metricsSource) resolveWebConfigEditText(ctx context.Context, configText string) (string, error) {
	if !pkgconfig.ContainsRedactedSecretValue(configText) {
		return configText, nil
	}
	redacted, err := s.runningConfig(ctx, true)
	if err != nil {
		return "", err
	}
	unredacted, err := s.runningConfig(ctx, false)
	if err != nil {
		return "", err
	}
	if redacted.Version != unredacted.Version {
		return "", errWebRedactedConfigText
	}
	replacements, ok := webRedactedLineReplacements(redacted.ConfigText, unredacted.ConfigText)
	if !ok {
		return "", errWebRedactedConfigText
	}
	resolved, ok := replaceWebRedactedLines(configText, replacements)
	if !ok {
		return "", errWebRedactedConfigText
	}
	return resolved, nil
}

func webRedactedLineReplacements(redactedText, unredactedText string) (map[string]string, bool) {
	redactedLines := webConfigTextLines(redactedText)
	unredactedLines := webConfigTextLines(unredactedText)
	if len(redactedLines) != len(unredactedLines) {
		return nil, false
	}
	replacements := make(map[string]string)
	for i, redactedLine := range redactedLines {
		if !pkgconfig.IsRedactedSecretLine(redactedLine) {
			continue
		}
		unredactedLine := unredactedLines[i]
		if redactedLine == unredactedLine || pkgconfig.IsRedactedSecretLine(unredactedLine) {
			return nil, false
		}
		if existing, ok := replacements[redactedLine]; ok && existing != unredactedLine {
			return nil, false
		}
		replacements[redactedLine] = unredactedLine
	}
	return replacements, true
}

func replaceWebRedactedLines(configText string, replacements map[string]string) (string, bool) {
	lines := strings.Split(configText, "\n")
	for i, line := range lines {
		content := strings.TrimSuffix(line, "\r")
		if !pkgconfig.IsRedactedSecretLine(content) {
			continue
		}
		replacement, ok := replacements[content]
		if !ok || pkgconfig.IsRedactedSecretLine(replacement) {
			return "", false
		}
		if strings.HasSuffix(line, "\r") {
			replacement += "\r"
		}
		lines[i] = replacement
	}
	return strings.Join(lines, "\n"), true
}

func webConfigTextLines(configText string) []string {
	lines := strings.Split(configText, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	for i, line := range lines {
		lines[i] = strings.TrimSuffix(line, "\r")
	}
	return lines
}

func (s metricsSource) validateWebConfig(ctx context.Context, username, configText string, expectedBaseVersion uint64) (string, bool, error) {
	api := s.configAPI
	if api == nil {
		return "", false, errWebConfigAPIUnavailable
	}
	if strings.TrimSpace(configText) == "" {
		return "", false, fmt.Errorf("config_text is required")
	}
	sessionID, err := api.CreateSession(ctx, username)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = api.CloseSession(context.Background(), sessionID) }()
	if err := api.AcquireLock(ctx, sessionID, username); err != nil {
		return "", false, err
	}
	defer func() { _ = api.ReleaseLock(context.Background(), sessionID) }()
	if err := api.ReplaceCandidateWithBase(ctx, sessionID, configText, expectedBaseVersion); err != nil {
		return "", false, err
	}
	if err := api.ValidateCandidate(ctx, sessionID); err != nil {
		return "", false, err
	}
	return s.redactedWebConfigDiff(ctx, configText)
}

func (s metricsSource) redactedWebConfigDiff(ctx context.Context, configText string) (string, bool, error) {
	runningRedacted, err := s.runningConfig(ctx, true)
	if err != nil {
		return "", false, err
	}
	runningUnredacted, err := s.runningConfig(ctx, false)
	if err != nil {
		return "", false, err
	}
	if runningRedacted.Version != runningUnredacted.Version {
		return "", false, errWebRedactedConfigText
	}
	candidateUnredacted, candidateRedacted, err := webConfigDiffTexts(configText)
	if err != nil {
		return "", false, err
	}
	diff, ok := webConfigRedactedLineDiff(
		runningUnredacted.ConfigText,
		runningRedacted.ConfigText,
		candidateUnredacted,
		candidateRedacted,
	)
	if !ok {
		return "", false, errWebRedactedConfigText
	}
	return diff, diff != "", nil
}

func webConfigDiffTexts(configText string) (string, string, error) {
	cfg, err := pkgconfig.NewParser(strings.NewReader(configText)).Parse()
	if err != nil {
		return "", "", fmt.Errorf("parse candidate config: %w", err)
	}
	unredacted, err := pkgconfig.ToSetCommandsWithError(cfg)
	if err != nil {
		return "", "", fmt.Errorf("serialize candidate config: %w", err)
	}
	redacted, err := pkgconfig.ToSetCommandsRedactedWithError(cfg)
	if err != nil {
		return "", "", fmt.Errorf("serialize redacted candidate config: %w", err)
	}
	return unredacted, redacted, nil
}

func webConfigRedactedLineDiff(oldText, oldRedactedText, newText, newRedactedText string) (string, bool) {
	oldLines, ok := webConfigDiffLineMap(oldText, oldRedactedText)
	if !ok {
		return "", false
	}
	newLines, ok := webConfigDiffLineMap(newText, newRedactedText)
	if !ok {
		return "", false
	}

	var out []string
	for line, redactedLine := range oldLines {
		if _, ok := newLines[line]; !ok {
			out = append(out, "- "+redactedLine)
		}
	}
	for line, redactedLine := range newLines {
		if _, ok := oldLines[line]; !ok {
			out = append(out, "+ "+redactedLine)
		}
	}
	sort.Strings(out)
	return strings.Join(out, "\n"), true
}

func webConfigDiffLineMap(text, redactedText string) (map[string]string, bool) {
	lines := normalizedWebConfigDiffLines(text)
	redactedLines := normalizedWebConfigDiffLines(redactedText)
	if len(lines) != len(redactedLines) {
		return nil, false
	}
	lineMap := make(map[string]string, len(lines))
	for i, line := range lines {
		redactedLine := redactedLines[i]
		if existing, ok := lineMap[line]; ok && existing != redactedLine {
			return nil, false
		}
		lineMap[line] = redactedLine
	}
	return lineMap, true
}

func normalizedWebConfigDiffLines(text string) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

func (s metricsSource) commitWebConfig(ctx context.Context, username, configText, message string, expectedBaseVersion uint64) (string, uint64, error) {
	api := s.configAPI
	if api == nil {
		return "", 0, errWebConfigAPIUnavailable
	}
	if strings.TrimSpace(configText) == "" {
		return "", 0, fmt.Errorf("config_text is required")
	}
	if strings.TrimSpace(message) == "" {
		message = "web config commit"
	}
	sessionID, err := api.CreateSession(ctx, username)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = api.CloseSession(context.Background(), sessionID) }()
	if err := api.AcquireLock(ctx, sessionID, username); err != nil {
		return "", 0, err
	}
	defer func() { _ = api.ReleaseLock(context.Background(), sessionID) }()
	if err := api.ReplaceCandidateWithBase(ctx, sessionID, configText, expectedBaseVersion); err != nil {
		return "", 0, err
	}
	return api.Commit(ctx, sessionID, username, message)
}

func webCorrelationContext(r *http.Request) (context.Context, string) {
	ctx := r.Context()
	for _, key := range []string{correlation.HeaderName, correlation.MetadataKey, correlation.AlternateMetadataKey} {
		if id := correlation.Normalize(r.Header.Get(key)); id != "" {
			ctx = correlation.WithID(ctx, id)
			return ctx, id
		}
	}
	return correlation.EnsureID(ctx)
}

func (s metricsSource) configHistory(ctx context.Context, limit, offset int) ([]webCommitEntry, error) {
	if s.configAPI == nil {
		return nil, nil
	}
	entries, err := s.configAPI.ListHistory(ctx, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list config history: %w", err)
	}
	history := make([]webCommitEntry, 0, len(entries))
	for _, entry := range entries {
		history = append(history, newWebCommitEntry(entry))
	}
	return history, nil
}

func (s metricsSource) auditEvents(ctx context.Context, opts nbgrpc.AuditLogOptions) ([]webAuditEntry, error) {
	if s.configAPI == nil {
		return nil, fmt.Errorf("audit API is unavailable")
	}
	events, err := s.configAPI.ListAuditEvents(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("list audit events: %w", err)
	}
	result := make([]webAuditEntry, 0, len(events))
	for _, event := range events {
		result = append(result, newWebAuditEntry(event))
	}
	return result, nil
}
