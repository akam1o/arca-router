package datastore

import (
	"context"
	"encoding/json"
	"time"
)

// runningMetadata stores metadata about the current running configuration.
type runningMetadata struct {
	CommitID  string    `json:"commit_id"`
	Timestamp time.Time `json:"timestamp"`
}

// GetRunning retrieves the current running configuration.
func (ds *etcdDatastore) GetRunning(ctx context.Context) (*RunningConfig, error) {
	ctx, cancel := ds.withTimeout(ctx)
	defer cancel()

	// Get running config metadata
	metadataKey := ds.key("running", "current")
	metadataResp, err := ds.client.Get(ctx, metadataKey)
	if err != nil {
		return nil, NewError(ErrCodeInternal, "failed to get running metadata", err)
	}

	if len(metadataResp.Kvs) == 0 {
		return nil, NewError(ErrCodeNotFound, "running config not found", nil)
	}

	var metadata runningMetadata
	if err := json.Unmarshal(metadataResp.Kvs[0].Value, &metadata); err != nil {
		return nil, NewError(ErrCodeInternal, "failed to unmarshal running metadata", err)
	}

	// Get running config text
	configKey := ds.key("running", "config")
	configResp, err := ds.client.Get(ctx, configKey)
	if err != nil {
		return nil, NewError(ErrCodeInternal, "failed to get running config", err)
	}

	if len(configResp.Kvs) == 0 {
		return nil, NewError(ErrCodeInternal, "running config text not found (inconsistent state)", nil)
	}

	return &RunningConfig{
		CommitID:   metadata.CommitID,
		ConfigText: string(configResp.Kvs[0].Value),
		Timestamp:  metadata.Timestamp,
	}, nil
}

// GetCandidate retrieves a session's candidate configuration.
func (ds *etcdDatastore) GetCandidate(ctx context.Context, sessionID string) (*CandidateConfig, error) {
	ctx, cancel := ds.withTimeout(ctx)
	defer cancel()

	key := ds.key("candidates", sessionID)
	resp, err := ds.client.Get(ctx, key)
	if err != nil {
		return nil, NewError(ErrCodeInternal, "failed to get candidate config", err)
	}

	if len(resp.Kvs) == 0 {
		return nil, NewError(ErrCodeNotFound, "candidate config not found", nil)
	}

	kv := resp.Kvs[0]

	// Parse the stored JSON
	var stored struct {
		SessionID  string    `json:"session_id"`
		ConfigText string    `json:"config_text"`
		CreatedAt  time.Time `json:"created_at"`
		UpdatedAt  time.Time `json:"updated_at"`
	}

	if err := json.Unmarshal(kv.Value, &stored); err != nil {
		return nil, NewError(ErrCodeInternal, "failed to unmarshal candidate config", err)
	}

	return &CandidateConfig{
		SessionID:  stored.SessionID,
		ConfigText: stored.ConfigText,
		CreatedAt:  stored.CreatedAt,
		UpdatedAt:  stored.UpdatedAt,
	}, nil
}

// SaveCandidate saves or updates a session's candidate configuration.
func (ds *etcdDatastore) SaveCandidate(ctx context.Context, sessionID string, configText string) error {
	ctx, cancel := ds.withTimeout(ctx)
	defer cancel()

	key := ds.key("candidates", sessionID)

	// Check if candidate already exists to determine if this is create or update
	existing, err := ds.client.Get(ctx, key)
	if err != nil {
		return NewError(ErrCodeInternal, "failed to check existing candidate", err)
	}

	now := time.Now()
	var createdAt time.Time

	if len(existing.Kvs) > 0 {
		// Update existing candidate - preserve creation time
		var stored struct {
			CreatedAt time.Time `json:"created_at"`
		}
		if err := json.Unmarshal(existing.Kvs[0].Value, &stored); err != nil {
			// If unmarshal fails, use current time as fallback
			createdAt = now
		} else {
			createdAt = stored.CreatedAt
		}
	} else {
		// New candidate
		createdAt = now
	}

	// Prepare candidate data
	candidate := struct {
		SessionID  string    `json:"session_id"`
		ConfigText string    `json:"config_text"`
		CreatedAt  time.Time `json:"created_at"`
		UpdatedAt  time.Time `json:"updated_at"`
	}{
		SessionID:  sessionID,
		ConfigText: configText,
		CreatedAt:  createdAt,
		UpdatedAt:  now,
	}

	data, err := json.Marshal(candidate)
	if err != nil {
		return NewError(ErrCodeInternal, "failed to marshal candidate config", err)
	}

	// Save to etcd
	_, err = ds.client.Put(ctx, key, string(data))
	if err != nil {
		return NewError(ErrCodeInternal, "failed to save candidate config", err)
	}

	return nil
}

// DeleteCandidate deletes a session's candidate configuration.
func (ds *etcdDatastore) DeleteCandidate(ctx context.Context, sessionID string) error {
	ctx, cancel := ds.withTimeout(ctx)
	defer cancel()

	key := ds.key("candidates", sessionID)

	// Delete the candidate (idempotent - succeeds even if not exists)
	_, err := ds.client.Delete(ctx, key)
	if err != nil {
		return NewError(ErrCodeInternal, "failed to delete candidate config", err)
	}

	return nil
}
