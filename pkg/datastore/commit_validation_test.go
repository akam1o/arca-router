package datastore

import (
	"context"
	"errors"
	"testing"
)

func TestCommitRejectsNilRequest(t *testing.T) {
	tests := []struct {
		name   string
		commit func(context.Context, *CommitRequest) (string, error)
	}{
		{
			name: "sqlite",
			commit: func(ctx context.Context, req *CommitRequest) (string, error) {
				return (&sqliteDatastore{}).Commit(ctx, req)
			},
		},
		{
			name: "etcd",
			commit: func(ctx context.Context, req *CommitRequest) (string, error) {
				return (&etcdDatastore{}).Commit(ctx, req)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			commitID, err := tt.commit(context.Background(), nil)
			if err == nil {
				t.Fatal("Commit() error = nil, want validation error")
			}
			if commitID != "" {
				t.Fatalf("Commit() commitID = %q, want empty", commitID)
			}
			var dsErr *Error
			if !errors.As(err, &dsErr) || dsErr.Code != ErrCodeValidation {
				t.Fatalf("Commit() error = %v, want ErrCodeValidation", err)
			}
		})
	}
}
