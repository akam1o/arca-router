package sqlite

import (
	"context"
	"path/filepath"
	"testing"
)

func TestGetLatestSnapshotReturnsNilWhenRunningConfigMissing(t *testing.T) {
	st, err := NewFromPath(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatalf("NewFromPath() error = %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	snap, err := st.GetLatestSnapshot(context.Background())
	if err != nil {
		t.Fatalf("GetLatestSnapshot() error = %v", err)
	}
	if snap != nil {
		t.Fatalf("GetLatestSnapshot() = %#v, want nil", snap)
	}
}
