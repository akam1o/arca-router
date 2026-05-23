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

func TestNewEtcdDatastoreRejectsCredentialsWithoutTLS(t *testing.T) {
	ds, err := NewEtcdDatastore(&Config{
		Backend:       BackendEtcd,
		EtcdEndpoints: []string{"http://127.0.0.1:2379"},
		EtcdUsername:  "arca",
		EtcdPassword:  "secret",
	})
	if err == nil {
		_ = ds.Close()
		t.Fatal("NewEtcdDatastore() error = nil, want TLS requirement error")
	}
	if !strings.Contains(err.Error(), "etcd credentials require TLS") {
		t.Fatalf("NewEtcdDatastore() error = %v, want TLS requirement error", err)
	}
}

func TestValidateEtcdCredentialTransport(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		wantErr bool
	}{
		{
			name: "no credentials allows http",
			cfg: &Config{
				EtcdEndpoints: []string{"http://127.0.0.1:2379"},
			},
		},
		{
			name: "password requires https",
			cfg: &Config{
				EtcdEndpoints: []string{"http://127.0.0.1:2379"},
				EtcdPassword:  "secret",
			},
			wantErr: true,
		},
		{
			name: "username requires https",
			cfg: &Config{
				EtcdEndpoints: []string{"127.0.0.1:2379"},
				EtcdUsername:  "arca",
			},
			wantErr: true,
		},
		{
			name: "credentials allow all https endpoints",
			cfg: &Config{
				EtcdEndpoints: []string{"https://etcd1:2379", "https://etcd2:2379"},
				EtcdUsername:  "arca",
				EtcdPassword:  "secret",
			},
		},
		{
			name: "credentials reject mixed endpoints",
			cfg: &Config{
				EtcdEndpoints: []string{"https://etcd1:2379", "http://etcd2:2379"},
				EtcdUsername:  "arca",
				EtcdPassword:  "secret",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateEtcdCredentialTransport(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateEtcdCredentialTransport() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
