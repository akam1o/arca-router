package datastore

import (
	"context"
)

// CompareCandidateRunning generates a diff between candidate and running configurations.
func (ds *etcdDatastore) CompareCandidateRunning(ctx context.Context, sessionID string) (*DiffResult, error) {
	// Get candidate config
	candidate, err := ds.GetCandidate(ctx, sessionID)
	if err != nil {
		return nil, err // Already wrapped
	}

	// Get running config
	running, err := ds.GetRunning(ctx)
	if err != nil {
		// If no running config exists, treat it as empty string
		if dsErr, ok := err.(*Error); ok && dsErr.Code == ErrCodeNotFound {
			return compareConfigs("", candidate.ConfigText), nil
		}
		return nil, err
	}

	return compareConfigs(running.ConfigText, candidate.ConfigText), nil
}

// CompareCommits generates a diff between two commits.
func (ds *etcdDatastore) CompareCommits(ctx context.Context, commitID1, commitID2 string) (*DiffResult, error) {
	// Get first commit
	commit1, err := ds.GetCommit(ctx, commitID1)
	if err != nil {
		return nil, err // Already wrapped
	}

	// Get second commit
	commit2, err := ds.GetCommit(ctx, commitID2)
	if err != nil {
		return nil, err // Already wrapped
	}

	return compareConfigs(commit1.ConfigText, commit2.ConfigText), nil
}

// Note: The compareConfigs, normalizeLineEndings, generateSimplifiedDiff, and FormatJunosStyleDiff
// functions are shared between SQLite and etcd implementations and are defined in sqlite_diff.go.
// They are pure functions that don't depend on the storage backend.
