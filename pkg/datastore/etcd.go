package datastore

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/akam1o/arca-router/pkg/auth"
	"github.com/akam1o/arca-router/pkg/security"
)

// etcdDatastore implements the Datastore interface using etcd.
type etcdDatastore struct {
	client    *clientv3.Client
	endpoints []string
	prefix    string        // Key prefix for all arca-router data (e.g., "/arca-router/")
	timeout   time.Duration // Default operation timeout
	closeOnce sync.Once
}

// NewEtcdDatastore creates a new etcd-backed datastore.
func NewEtcdDatastore(cfg *Config) (Datastore, error) {
	if cfg == nil {
		return nil, fmt.Errorf("datastore config cannot be nil")
	}
	if cfg.Backend != BackendEtcd {
		return nil, fmt.Errorf("invalid backend type: %s (expected %s)", cfg.Backend, BackendEtcd)
	}

	if len(cfg.EtcdEndpoints) == 0 {
		return nil, fmt.Errorf("etcd endpoints cannot be empty")
	}
	if err := validateEtcdCredentialTransport(cfg); err != nil {
		return nil, err
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
		if closeErr := client.Close(); closeErr != nil {
			_ = closeErr
		}
		return nil, fmt.Errorf("failed to connect to etcd: %w", err)
	}

	ds := &etcdDatastore{
		client:    client,
		endpoints: append([]string(nil), cfg.EtcdEndpoints...),
		prefix:    prefix,
		timeout:   timeout,
	}
	if err := ds.ensureCommitHistoryIndex(ctx); err != nil {
		if closeErr := client.Close(); closeErr != nil {
			_ = closeErr
		}
		return nil, fmt.Errorf("ensure commit history index: %w", err)
	}

	return ds, nil
}

func validateEtcdCredentialTransport(cfg *Config) error {
	if strings.TrimSpace(cfg.EtcdUsername) == "" && cfg.EtcdPassword == "" {
		return nil
	}
	for _, endpoint := range cfg.EtcdEndpoints {
		if !etcdEndpointUsesTLS(endpoint) {
			return fmt.Errorf("etcd credentials require TLS; endpoint %q must use https", endpoint)
		}
	}
	return nil
}

func etcdEndpointUsesTLS(endpoint string) bool {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return false
	}
	return strings.EqualFold(parsed.Scheme, "https")
}

// buildTLSConfig creates a TLS configuration from the provided TLSConfig.
func buildTLSConfig(cfg *TLSConfig) (*tls.Config, error) {
	// Load client certificate and key
	cert, err := auth.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
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

	tlsConfig := security.ApplyTLSPolicy(&tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caCertPool,
	})

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

// EtcdStatus returns live revision metadata for config synchronization.
func (ds *etcdDatastore) EtcdStatus(ctx context.Context) (*EtcdStatus, error) {
	ctx, cancel := ds.withTimeout(ctx)
	defer cancel()

	resp, err := ds.client.Get(ctx, ds.key("running", "current"))
	if err != nil {
		return nil, NewError(ErrCodeInternal, "failed to get etcd running metadata", err)
	}

	status := &EtcdStatus{
		Endpoints: append([]string(nil), ds.endpoints...),
		Prefix:    ds.prefix,
	}
	if resp.Header != nil {
		status.Revision = resp.Header.Revision
	}
	if len(resp.Kvs) == 0 {
		return status, nil
	}

	status.RunningRevision = resp.Kvs[0].ModRevision
	var metadata runningMetadata
	if err := json.Unmarshal(resp.Kvs[0].Value, &metadata); err != nil {
		return nil, NewError(ErrCodeInternal, "failed to unmarshal running metadata", err)
	}
	status.RunningCommitID = metadata.CommitID
	status.RunningTimestamp = metadata.Timestamp
	return status, nil
}

var _ EtcdStatusProvider = (*etcdDatastore)(nil)
