package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	nbgrpc "github.com/akam1o/arca-router/internal/northbound/grpc"
)

const maxWebAuditFilterLength = 128

func writeWebAuthChallenge(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", webAuthRealm)
	http.Error(w, "authentication required", http.StatusUnauthorized)
}

func decodeWebConfigEditRequest(w http.ResponseWriter, r *http.Request) (webConfigEditRequest, bool) {
	var req webConfigEditRequest
	if !decodeWebJSONRequest(w, r, &req) {
		return req, false
	}
	if !validateWebConfigTextForEdit(w, req.ConfigText) {
		return req, false
	}
	return req, true
}

func decodeWebConfigCommitRequest(w http.ResponseWriter, r *http.Request) (webConfigCommitRequest, bool) {
	var req webConfigCommitRequest
	if !decodeWebJSONRequest(w, r, &req) {
		return req, false
	}
	if !validateWebConfigTextForEdit(w, req.ConfigText) {
		return req, false
	}
	return req, true
}

func validateWebConfigTextForEdit(w http.ResponseWriter, configText string) bool {
	if strings.TrimSpace(configText) == "" {
		writeWebJSONError(w, http.StatusBadRequest, "config_text is required")
		return false
	}
	return true
}

func decodeWebJSONRequest(w http.ResponseWriter, r *http.Request, dst any) bool {
	if !webJSONContentType(r.Header.Get("Content-Type")) {
		writeWebJSONError(w, http.StatusUnsupportedMediaType, "content-type must be application/json")
		return false
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, webConfigEditBodyLimit))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeWebJSONDecodeError(w, err)
		return false
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		if err != nil {
			writeWebJSONDecodeError(w, err)
		} else {
			writeWebJSONError(w, http.StatusBadRequest, "decode request: unexpected trailing JSON value")
		}
		return false
	}
	return true
}

func webJSONContentType(raw string) bool {
	mediaType, _, err := mime.ParseMediaType(raw)
	return err == nil && strings.EqualFold(mediaType, "application/json")
}

func writeWebJSONDecodeError(w http.ResponseWriter, err error) {
	status, code, message := webJSONDecodeErrorResponse(err)
	writeWebJSONErrorCode(w, status, code, message)
}

func webJSONDecodeErrorResponse(err error) (int, string, string) {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		return http.StatusRequestEntityTooLarge, "request_body_too_large", "request body too large"
	}
	var unmarshalTypeErr *json.UnmarshalTypeError
	if errors.As(err, &unmarshalTypeErr) {
		if unmarshalTypeErr.Field != "" {
			return http.StatusBadRequest, "invalid_json", fmt.Sprintf("invalid JSON value for %s", unmarshalTypeErr.Field)
		}
		return http.StatusBadRequest, "invalid_json", "invalid JSON value"
	}
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return http.StatusBadRequest, "invalid_json", "malformed JSON request"
	}
	if strings.HasPrefix(err.Error(), "json: unknown field ") {
		return http.StatusBadRequest, "unknown_field", "request contains unknown field"
	}
	return http.StatusBadRequest, "invalid_json", "malformed JSON request"
}

func writeWebJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeWebJSONError(w http.ResponseWriter, status int, message string) {
	writeWebJSON(w, status, map[string]string{"error": message})
}

func writeWebJSONErrorCode(w http.ResponseWriter, status int, code, message string) {
	writeWebJSON(w, status, map[string]string{
		"code":  code,
		"error": message,
	})
}

func webHistoryPaginationFromRequest(r *http.Request) (int, int, error) {
	query := r.URL.Query()
	limit, err := webHistoryLimitQuery(query.Get("limit"))
	if err != nil {
		return 0, 0, err
	}
	offset, err := boundedWebIntQuery(query.Get("offset"), 0, 0, 1<<31-1, "offset")
	if err != nil {
		return 0, 0, err
	}
	return limit, offset, nil
}

func webHistoryLimitQuery(raw string) (int, error) {
	return clampedWebIntQuery(raw, 20, 1, 100, "limit")
}

func clampedWebIntQuery(raw string, defaultValue, minValue, maxValue int, name string) (int, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return defaultValue, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	if parsed < minValue {
		return 0, fmt.Errorf("%s must be between %d and %d", name, minValue, maxValue)
	}
	if parsed > maxValue {
		return maxValue, nil
	}
	return parsed, nil
}

func webAuditOptionsFromRequest(r *http.Request) (nbgrpc.AuditLogOptions, error) {
	query := r.URL.Query()
	limit, err := clampedWebIntQuery(query.Get("limit"), 100, 1, 1000, "limit")
	if err != nil {
		return nbgrpc.AuditLogOptions{}, err
	}
	offset, err := boundedWebIntQuery(query.Get("offset"), 0, 0, 1<<31-1, "offset")
	if err != nil {
		return nbgrpc.AuditLogOptions{}, err
	}
	since, err := optionalWebTimeQuery(query.Get("since"), "since")
	if err != nil {
		return nbgrpc.AuditLogOptions{}, err
	}
	until, err := optionalWebTimeQuery(query.Get("until"), "until")
	if err != nil {
		return nbgrpc.AuditLogOptions{}, err
	}
	if !since.IsZero() && !until.IsZero() && since.After(until) {
		return nbgrpc.AuditLogOptions{}, fmt.Errorf("since must be before until")
	}
	user, err := boundedWebAuditFilterQuery(query.Get("user"), "user")
	if err != nil {
		return nbgrpc.AuditLogOptions{}, err
	}
	action, err := boundedWebAuditFilterQuery(query.Get("action"), "action")
	if err != nil {
		return nbgrpc.AuditLogOptions{}, err
	}
	result, err := boundedWebAuditFilterQuery(query.Get("result"), "result")
	if err != nil {
		return nbgrpc.AuditLogOptions{}, err
	}
	return nbgrpc.AuditLogOptions{
		Limit:     limit,
		Offset:    offset,
		StartTime: since,
		EndTime:   until,
		User:      user,
		Action:    action,
		Result:    result,
	}, nil
}

func boundedWebAuditFilterQuery(raw, name string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	for _, r := range value {
		if r < 0x21 || r > 0x7e {
			return "", fmt.Errorf("%s contains unsupported characters", name)
		}
	}
	if len(value) > maxWebAuditFilterLength {
		return "", fmt.Errorf("%s must be %d characters or fewer", name, maxWebAuditFilterLength)
	}
	return value, nil
}

func boundedWebIntQuery(raw string, defaultValue, minValue, maxValue int, name string) (int, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return defaultValue, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	if parsed < minValue || parsed > maxValue {
		return 0, fmt.Errorf("%s must be between %d and %d", name, minValue, maxValue)
	}
	return parsed, nil
}

func optionalWebTimeQuery(raw, name string) (time.Time, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s must be RFC3339 timestamp", name)
	}
	return parsed, nil
}

func newWebCommitEntry(entry nbgrpc.CommitInfo) webCommitEntry {
	message := entry.Message
	if strings.TrimSpace(message) == "" {
		message = "(no message)"
	}
	return webCommitEntry{
		CommitID:      entry.CommitID,
		ShortCommitID: shortCommitID(entry.CommitID),
		User:          entry.User,
		Timestamp:     formatWebCommitTime(entry.Timestamp),
		Message:       message,
		IsRollback:    entry.IsRollback,
	}
}

func newWebAuditEntry(event nbgrpc.AuditEventInfo) webAuditEntry {
	entry := webAuditEntry{
		ID:            event.ID,
		Key:           event.Key,
		User:          event.User,
		SessionID:     event.SessionID,
		SourceIP:      event.SourceIP,
		CorrelationID: event.CorrelationID,
		Action:        event.Action,
		Result:        event.Result,
		ErrorCode:     event.ErrorCode,
		Details:       event.Details,
		RawDetails:    event.RawDetails,
	}
	if !event.Timestamp.IsZero() {
		entry.Timestamp = event.Timestamp.UTC().Format(time.RFC3339Nano)
	}
	return entry
}

func shortCommitID(commitID string) string {
	if len(commitID) <= 12 {
		return commitID
	}
	return commitID[:12]
}

func formatWebCommitTime(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.UTC().Format(time.RFC3339)
}

func formatWebOptionalTime(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.UTC().Format(time.RFC3339)
}

func formatWebOptionalDisplayTime(value string) string {
	if value == "" {
		return "Never"
	}
	return value
}
