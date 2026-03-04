// Package grpc provides the internal gRPC client for arca-cli to communicate
// with the arca-routerd engine over a Unix domain socket.
package grpc

import (
	"context"
	"fmt"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Client is the gRPC client that arca-cli uses to talk to arca-routerd.
// It provides high-level methods for config management, session control,
// and operational state queries. Until proto is compiled, methods work
// with the Server's exported Go API directly via forwarding.
type Client struct {
	conn *grpc.ClientConn
	// Once proto is compiled and registered, typed stubs go here:
	// config  apiv1.ConfigServiceClient
	// session apiv1.SessionServiceClient
	// state   apiv1.StateServiceClient
}

// Dial connects to the arca-routerd gRPC server via Unix socket.
func Dial(socketPath string) (*Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, "unix://"+socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return net.DialTimeout("unix", socketPath, 5*time.Second)
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("dial arca-routerd at %s: %w", socketPath, err)
	}

	return &Client{conn: conn}, nil
}

// Close closes the gRPC connection.
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// --- Config operations ---

// GetRunning returns the running configuration text and version.
func (c *Client) GetRunning(ctx context.Context) (configText string, version uint64, err error) {
	// TODO: use compiled proto client once available
	// resp, err := c.config.GetRunning(ctx, &apiv1.GetRunningRequest{})
	return "", 0, fmt.Errorf("not yet connected to compiled proto service")
}

// EditCandidate sends set-command text to the candidate config.
func (c *Client) EditCandidate(ctx context.Context, sessionID, configText string) error {
	return fmt.Errorf("not yet connected to compiled proto service")
}

// Commit commits the candidate configuration.
func (c *Client) Commit(ctx context.Context, sessionID, user, message string) (commitID string, version uint64, err error) {
	return "", 0, fmt.Errorf("not yet connected to compiled proto service")
}

// Discard discards candidate changes.
func (c *Client) Discard(ctx context.Context, sessionID string) error {
	return fmt.Errorf("not yet connected to compiled proto service")
}

// Diff returns the diff between candidate and running.
func (c *Client) Diff(ctx context.Context, sessionID string) (diffText string, hasChanges bool, err error) {
	return "", false, fmt.Errorf("not yet connected to compiled proto service")
}

// ListHistory returns commit history.
func (c *Client) ListHistory(ctx context.Context, limit, offset int) ([]CommitInfo, error) {
	return nil, fmt.Errorf("not yet connected to compiled proto service")
}

// --- Session operations ---

// CreateSession creates a new configuration session.
func (c *Client) CreateSession(ctx context.Context, user string) (sessionID string, err error) {
	return "", fmt.Errorf("not yet connected to compiled proto service")
}

// CloseSession closes a configuration session.
func (c *Client) CloseSession(ctx context.Context, sessionID string) error {
	return fmt.Errorf("not yet connected to compiled proto service")
}

// AcquireLock acquires the candidate lock.
func (c *Client) AcquireLock(ctx context.Context, sessionID, user string) error {
	return fmt.Errorf("not yet connected to compiled proto service")
}

// ReleaseLock releases the candidate lock.
func (c *Client) ReleaseLock(ctx context.Context, sessionID string) error {
	return fmt.Errorf("not yet connected to compiled proto service")
}

// --- State queries ---

// GetInterfaces returns interface operational state.
func (c *Client) GetInterfaces(ctx context.Context, nameFilter string) ([]InterfaceInfo, error) {
	return nil, fmt.Errorf("not yet connected to compiled proto service")
}

// GetRoutes returns routing table entries.
func (c *Client) GetRoutes(ctx context.Context, prefixFilter, protoFilter string) ([]RouteInfo, error) {
	return nil, fmt.Errorf("not yet connected to compiled proto service")
}

// GetBGPNeighbors returns BGP neighbor state.
func (c *Client) GetBGPNeighbors(ctx context.Context) ([]BGPNeighborInfo, error) {
	return nil, fmt.Errorf("not yet connected to compiled proto service")
}

// GetSystemInfo returns system information.
func (c *Client) GetSystemInfo(ctx context.Context) (*SystemInfo, error) {
	return nil, fmt.Errorf("not yet connected to compiled proto service")
}

// --- Response types ---

// CommitInfo represents a commit history entry.
type CommitInfo struct {
	CommitID   string
	User       string
	Timestamp  time.Time
	Message    string
	IsRollback bool
}

// InterfaceInfo represents interface operational state.
type InterfaceInfo struct {
	Name        string
	AdminStatus string
	OperStatus  string
	Speed       uint64
	MTU         uint32
	MAC         string
	RxPackets   uint64
	TxPackets   uint64
	RxBytes     uint64
	TxBytes     uint64
	RxErrors    uint64
	TxErrors    uint64
}

// RouteInfo represents a routing table entry.
type RouteInfo struct {
	Prefix    string
	NextHop   string
	Protocol  string
	Metric    uint32
	Interface string
	Active    bool
}

// BGPNeighborInfo represents BGP neighbor state.
type BGPNeighborInfo struct {
	PeerAddress    string
	PeerAS         uint32
	State          string
	UptimeSecs     uint64
	PrefixReceived uint32
	PrefixSent     uint32
}

// SystemInfo represents system information.
type SystemInfo struct {
	Hostname   string
	Version    string
	UptimeSecs uint64
}
