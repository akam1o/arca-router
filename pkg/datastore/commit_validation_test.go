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

func TestRollbackRejectsNilRequest(t *testing.T) {
	tests := []struct {
		name     string
		rollback func(context.Context, *RollbackRequest) (string, error)
	}{
		{
			name: "sqlite",
			rollback: func(ctx context.Context, req *RollbackRequest) (string, error) {
				return (&sqliteDatastore{}).Rollback(ctx, req)
			},
		},
		{
			name: "etcd",
			rollback: func(ctx context.Context, req *RollbackRequest) (string, error) {
				return (&etcdDatastore{}).Rollback(ctx, req)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			commitID, err := tt.rollback(context.Background(), nil)
			if err == nil {
				t.Fatal("Rollback() error = nil, want validation error")
			}
			if commitID != "" {
				t.Fatalf("Rollback() commitID = %q, want empty", commitID)
			}
			var dsErr *Error
			if !errors.As(err, &dsErr) || dsErr.Code != ErrCodeValidation {
				t.Fatalf("Rollback() error = %v, want ErrCodeValidation", err)
			}
		})
	}
}

func TestAcquireLockRejectsNilRequest(t *testing.T) {
	tests := []struct {
		name        string
		acquireLock func(context.Context, *LockRequest) error
	}{
		{
			name: "sqlite",
			acquireLock: func(ctx context.Context, req *LockRequest) error {
				return (&sqliteDatastore{}).AcquireLock(ctx, req)
			},
		},
		{
			name: "etcd",
			acquireLock: func(ctx context.Context, req *LockRequest) error {
				return (&etcdDatastore{}).AcquireLock(ctx, req)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.acquireLock(context.Background(), nil)
			if err == nil {
				t.Fatal("AcquireLock() error = nil, want validation error")
			}
			var dsErr *Error
			if !errors.As(err, &dsErr) || dsErr.Code != ErrCodeValidation {
				t.Fatalf("AcquireLock() error = %v, want ErrCodeValidation", err)
			}
		})
	}
}
