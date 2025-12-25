package datastore

import (
	"fmt"
)

// NewDatastore creates a new datastore based on the provided configuration.
// It automatically selects the appropriate backend (SQLite or etcd) based on cfg.Backend.
//
// Example usage:
//
//	cfg := &datastore.Config{
//	    Backend: datastore.BackendSQLite,
//	    SQLitePath: "/var/lib/arca-router/config.db",
//	}
//	ds, err := datastore.NewDatastore(cfg)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer ds.Close()
func NewDatastore(cfg *Config) (Datastore, error) {
	if cfg == nil {
		return nil, fmt.Errorf("datastore config cannot be nil")
	}

	switch cfg.Backend {
	case BackendSQLite:
		return NewSQLiteDatastore(cfg)

	case BackendEtcd:
		return NewEtcdDatastore(cfg)

	default:
		return nil, fmt.Errorf("unsupported datastore backend: %s", cfg.Backend)
	}
}
