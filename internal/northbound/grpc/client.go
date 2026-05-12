// Package grpc provides the internal gRPC client for arca-cli to communicate
// with the arca-routerd engine over a Unix domain socket.
package grpc

import (
	"context"
	"fmt"
	"net"
	"time"

	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
)

// Client is the gRPC client that arca-cli uses to talk to arca-routerd.
// It provides high-level methods for config management, session control,
// and operational state queries.
type Client struct {
	conn *googlegrpc.ClientConn
	// Once proto is compiled and registered, typed stubs go here:
	// config  apiv1.ConfigServiceClient
	// session apiv1.SessionServiceClient
	// state   apiv1.StateServiceClient
}

// Dial connects to the arca-routerd gRPC server via Unix socket.
func Dial(socketPath string) (*Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := googlegrpc.NewClient("unix://"+socketPath,
		googlegrpc.WithTransportCredentials(insecure.NewCredentials()),
		googlegrpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socketPath)
		}),
		googlegrpc.WithDefaultCallOptions(googlegrpc.ForceCodec(jsonCodec{})),
	)
	if err != nil {
		return nil, fmt.Errorf("dial arca-routerd at %s: %w", socketPath, err)
	}
	conn.Connect()
	for {
		state := conn.GetState()
		if state == connectivity.Ready {
			break
		}
		if !conn.WaitForStateChange(ctx, state) {
			_ = conn.Close()
			return nil, fmt.Errorf("dial arca-routerd at %s: %w", socketPath, ctx.Err())
		}
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

func (c *Client) invoke(ctx context.Context, method string, req, resp interface{}) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
	}
	return c.conn.Invoke(ctx, method, req, resp, googlegrpc.ForceCodec(jsonCodec{}))
}

// --- Config operations ---

// GetRunning returns the running configuration text and version.
func (c *Client) GetRunning(ctx context.Context) (configText string, version uint64, err error) {
	var resp getRunningResponse
	if err := c.invoke(ctx, "/"+configServiceName+"/GetRunning", &getRunningRequest{}, &resp); err != nil {
		return "", 0, err
	}
	return resp.ConfigText, resp.Version, nil
}

// GetCandidate returns a session's candidate configuration text.
func (c *Client) GetCandidate(ctx context.Context, sessionID string) (string, error) {
	var resp getCandidateResponse
	if err := c.invoke(ctx, "/"+configServiceName+"/GetCandidate", &getCandidateRequest{SessionID: sessionID}, &resp); err != nil {
		return "", err
	}
	return resp.ConfigText, nil
}

// EditCandidate sends set-command text to the candidate config.
func (c *Client) EditCandidate(ctx context.Context, sessionID, configText string) error {
	var resp editCandidateResponse
	return c.invoke(ctx, "/"+configServiceName+"/EditCandidate", &editCandidateRequest{
		SessionID:  sessionID,
		ConfigText: configText,
	}, &resp)
}

// Commit commits the candidate configuration.
func (c *Client) Commit(ctx context.Context, sessionID, user, message string) (commitID string, version uint64, err error) {
	var resp commitResponse
	if err := c.invoke(ctx, "/"+configServiceName+"/Commit", &commitRequest{
		SessionID: sessionID,
		User:      user,
		Message:   message,
	}, &resp); err != nil {
		return "", 0, err
	}
	return resp.CommitID, resp.Version, nil
}

// ValidateCandidate validates a session's candidate configuration without committing it.
func (c *Client) ValidateCandidate(ctx context.Context, sessionID string) error {
	var resp validateCandidateResponse
	return c.invoke(ctx, "/"+configServiceName+"/ValidateCandidate", &validateCandidateRequest{SessionID: sessionID}, &resp)
}

// Discard discards candidate changes.
func (c *Client) Discard(ctx context.Context, sessionID string) error {
	var resp discardResponse
	return c.invoke(ctx, "/"+configServiceName+"/Discard", &discardRequest{SessionID: sessionID}, &resp)
}

// Rollback rolls back running configuration to a previous commit.
func (c *Client) Rollback(ctx context.Context, sessionID, commitID, user, message string) (newCommitID string, version uint64, err error) {
	var resp rollbackResponse
	if err := c.invoke(ctx, "/"+configServiceName+"/Rollback", &rollbackRequest{
		SessionID: sessionID,
		CommitID:  commitID,
		User:      user,
		Message:   message,
	}, &resp); err != nil {
		return "", 0, err
	}
	return resp.NewCommitID, resp.Version, nil
}

// Diff returns the diff between candidate and running.
func (c *Client) Diff(ctx context.Context, sessionID string) (diffText string, hasChanges bool, err error) {
	var resp diffResponse
	if err := c.invoke(ctx, "/"+configServiceName+"/Diff", &diffRequest{SessionID: sessionID}, &resp); err != nil {
		return "", false, err
	}
	return resp.DiffText, resp.HasChanges, nil
}

// ListHistory returns commit history.
func (c *Client) ListHistory(ctx context.Context, limit, offset int) ([]CommitInfo, error) {
	var resp listHistoryResponse
	if err := c.invoke(ctx, "/"+configServiceName+"/ListHistory", &listHistoryRequest{Limit: limit, Offset: offset}, &resp); err != nil {
		return nil, err
	}
	return resp.Entries, nil
}

// --- Session operations ---

// CreateSession creates a new configuration session.
func (c *Client) CreateSession(ctx context.Context, user string) (sessionID string, err error) {
	var resp createSessionResponse
	if err := c.invoke(ctx, "/"+sessionServiceName+"/CreateSession", &createSessionRequest{User: user}, &resp); err != nil {
		return "", err
	}
	return resp.SessionID, nil
}

// CloseSession closes a configuration session.
func (c *Client) CloseSession(ctx context.Context, sessionID string) error {
	var resp closeSessionResponse
	return c.invoke(ctx, "/"+sessionServiceName+"/CloseSession", &closeSessionRequest{SessionID: sessionID}, &resp)
}

// AcquireLock acquires the candidate lock.
func (c *Client) AcquireLock(ctx context.Context, sessionID, user string) error {
	var resp acquireLockResponse
	return c.invoke(ctx, "/"+sessionServiceName+"/AcquireLock", &acquireLockRequest{
		SessionID: sessionID,
		User:      user,
	}, &resp)
}

// ReleaseLock releases the candidate lock.
func (c *Client) ReleaseLock(ctx context.Context, sessionID string) error {
	var resp releaseLockResponse
	return c.invoke(ctx, "/"+sessionServiceName+"/ReleaseLock", &releaseLockRequest{SessionID: sessionID}, &resp)
}

// --- State queries ---

// GetInterfaces returns interface operational state.
func (c *Client) GetInterfaces(ctx context.Context, nameFilter string) ([]InterfaceInfo, error) {
	var resp getInterfacesResponse
	if err := c.invoke(ctx, "/"+stateServiceName+"/GetInterfaces", &getInterfacesRequest{NameFilter: nameFilter}, &resp); err != nil {
		return nil, err
	}
	return resp.Interfaces, nil
}

// GetRoutes returns routing table entries.
func (c *Client) GetRoutes(ctx context.Context, prefixFilter, protoFilter string) ([]RouteInfo, error) {
	var resp getRoutesResponse
	if err := c.invoke(ctx, "/"+stateServiceName+"/GetRoutes", &getRoutesRequest{
		PrefixFilter: prefixFilter,
		ProtoFilter:  protoFilter,
	}, &resp); err != nil {
		return nil, err
	}
	return resp.Routes, nil
}

// GetBGPNeighbors returns BGP neighbor state.
func (c *Client) GetBGPNeighbors(ctx context.Context) ([]BGPNeighborInfo, error) {
	var resp getBGPNeighborsResponse
	if err := c.invoke(ctx, "/"+stateServiceName+"/GetBGPNeighbors", &getBGPNeighborsRequest{}, &resp); err != nil {
		return nil, err
	}
	return resp.Neighbors, nil
}

// GetSystemInfo returns system information.
func (c *Client) GetSystemInfo(ctx context.Context) (*SystemInfo, error) {
	var resp getSystemInfoResponse
	if err := c.invoke(ctx, "/"+stateServiceName+"/GetSystemInfo", &getSystemInfoRequest{}, &resp); err != nil {
		return nil, err
	}
	return resp.Info, nil
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
