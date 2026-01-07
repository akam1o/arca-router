package datastore

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// etcdDatastore implements the Datastore interface using etcd.
type etcdDatastore struct {
	client    *clientv3.Client
	prefix    string        // Key prefix for all arca-router data (e.g., "/arca-router/")
	timeout   time.Duration // Default operation timeout
	closeOnce sync.Once
}

// NewEtcdDatastore creates a new etcd-backed datastore.
func NewEtcdDatastore(cfg *Config) (Datastore, error) {
	if cfg.Backend != BackendEtcd {
		return nil, fmt.Errorf("invalid backend type: %s (expected %s)", cfg.Backend, BackendEtcd)
	}

	if len(cfg.EtcdEndpoints) == 0 {
		return nil, fmt.Errorf("etcd endpoints cannot be empty")
	}

	// Set default prefix if not specified
	prefix := cfg.EtcdPrefix
	if prefix == "" {
		prefix = "/arca-router/"
	}
	// Ensure prefix ends with "/"
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	// Set default timeout if not specified
	timeout := cfg.EtcdTimeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	// Build etcd client config
	etcdCfg := clientv3.Config{
		Endpoints:   cfg.EtcdEndpoints,
		DialTimeout: timeout,
		Username:    cfg.EtcdUsername,
		Password:    cfg.EtcdPassword,
	}

	// Configure TLS if provided
	if cfg.EtcdTLS != nil {
		tlsConfig, err := buildTLSConfig(cfg.EtcdTLS)
		if err != nil {
			return nil, fmt.Errorf("failed to build TLS config: %w", err)
		}
		etcdCfg.TLS = tlsConfig
	}

	// Create etcd client
	client, err := clientv3.New(etcdCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create etcd client: %w", err)
	}

	// Test connection with a simple Get (with timeout)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	_, err = client.Get(ctx, prefix, clientv3.WithPrefix(), clientv3.WithLimit(1))
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to connect to etcd: %w", err)
	}

	ds := &etcdDatastore{
		client:  client,
		prefix:  prefix,
		timeout: timeout,
	}

	return ds, nil
}

// buildTLSConfig creates a TLS configuration from the provided TLSConfig.
func buildTLSConfig(cfg *TLSConfig) (*tls.Config, error) {
	// Load client certificate and key
	cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load client cert/key: %w", err)
	}

	// Load CA certificate
	caCert, err := os.ReadFile(cfg.CAFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA cert: %w", err)
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to parse CA cert")
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caCertPool,
		MinVersion:   tls.VersionTLS12, // Enforce TLS 1.2+
	}

	return tlsConfig, nil
}

// Close closes the etcd client connection.
// This method is idempotent and safe to call multiple times.
func (ds *etcdDatastore) Close() error {
	var closeErr error

	ds.closeOnce.Do(func() {
		if ds.client != nil {
			closeErr = ds.client.Close()
		}
	})

	return closeErr
}

// key constructs a full etcd key with the configured prefix.
func (ds *etcdDatastore) key(parts ...string) string {
	return ds.prefix + strings.Join(parts, "/")
}

// withTimeout creates a context with the default timeout if no deadline is set.
func (ds *etcdDatastore) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		// Context already has a deadline, don't wrap it
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, ds.timeout)
}
