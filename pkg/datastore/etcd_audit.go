package datastore

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"github.com/oklog/ulid/v2"
)

// LogAuditEvent logs an audit event to etcd.
// For etcd backend, events are stored with ULID keys for sortable, unique identifiers.
func (ds *etcdDatastore) LogAuditEvent(ctx context.Context, event *AuditEvent) error {
	ctx, cancel := ds.withTimeout(ctx)
	defer cancel()

	// Generate ULID for the audit event
	ulidKey := generateULID()

	// Set Key field in the event (for consistency with schema)
	event.Key = ulidKey
	event.ID = 0 // ID is not used in etcd backend

	// Set timestamp if not provided
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	// Marshal event to JSON
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return NewError(ErrCodeInternal, "failed to marshal audit event", err)
	}

	// Store in etcd with ULID key
	auditKey := ds.key("audit", ulidKey)
	_, err = ds.client.Put(ctx, auditKey, string(eventJSON))
	if err != nil {
		return NewError(ErrCodeInternal, "failed to log audit event", err)
	}

	return nil
}

// generateULID generates a ULID (Universally Unique Lexicographically Sortable Identifier).
// ULIDs are 26 characters, timestamp-prefixed, and sortable.
// Example: 01ARYZ6S41TSV4RRFFQ69G5FAV
func generateULID() string {
	// Use crypto/rand for entropy
	entropy := ulid.Monotonic(rand.Reader, 0)

	// Generate ULID with current timestamp
	id := ulid.MustNew(ulid.Timestamp(time.Now()), entropy)

	return id.String()
}

// CleanupAuditLog deletes audit log entries older than the specified cutoff time
// For etcd backend, this requires listing all audit keys and deleting those with old timestamps
func (ds *etcdDatastore) CleanupAuditLog(ctx context.Context, cutoff time.Time) (int64, error) {
	ctx, cancel := ds.withTimeout(ctx)
	defer cancel()

	// List all audit events
	prefix := ds.key("audit", "")
	resp, err := ds.client.Get(ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		return 0, NewError(ErrCodeInternal, "failed to list audit events", err)
	}

	// Parse and filter events older than cutoff
	var keysToDelete []string
	for _, kv := range resp.Kvs {
		var event AuditEvent
		if err := json.Unmarshal(kv.Value, &event); err != nil {
			// Skip malformed entries
			continue
		}

		if event.Timestamp.Before(cutoff) {
			keysToDelete = append(keysToDelete, string(kv.Key))
		}
	}

	// Delete old audit events
	deletedCount := int64(0)
	for _, key := range keysToDelete {
		_, err := ds.client.Delete(ctx, key)
		if err != nil {
			// Log error but continue deletion
			continue
		}
		deletedCount++
	}

	return deletedCount, nil
}
