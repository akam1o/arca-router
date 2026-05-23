package datastore

import (
	"strings"
	"testing"
)

func TestDatastoreConstructorsRejectNilConfig(t *testing.T) {
	tests := []struct {
		name string
		fn   func() (Datastore, error)
	}{
		{
			name: "factory",
			fn: func() (Datastore, error) {
				return NewDatastore(nil)
			},
		},
		{
			name: "sqlite",
			fn: func() (Datastore, error) {
				return NewSQLiteDatastore(nil)
			},
		},
		{
			name: "etcd",
			fn: func() (Datastore, error) {
				return NewEtcdDatastore(nil)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ds, err := tt.fn()
			if err == nil {
				_ = ds.Close()
				t.Fatal("constructor error = nil, want nil config error")
			}
			if !strings.Contains(err.Error(), "config cannot be nil") {
				t.Fatalf("constructor error = %v, want nil config error", err)
			}
		})
	}
}
