// Package grpc provides the internal gRPC client for arca to communicate
// with the arca-routerd engine over a Unix domain socket.
package grpc

import (
	"context"
	"fmt"
	"net"
	"time"

	apiv1 "github.com/akam1o/arca-router/api/v1"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
)

// Client is the gRPC client that arca uses to talk to arca-routerd.
// It provides high-level methods for config management, session control,
// and operational state queries.
type Client struct {
	conn    *googlegrpc.ClientConn
	config  apiv1.ConfigServiceClient
	session apiv1.SessionServiceClient
	state   apiv1.StateServiceClient
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

	return &Client{
		conn:    conn,
		config:  apiv1.NewConfigServiceClient(conn),
		session: apiv1.NewSessionServiceClient(conn),
		state:   apiv1.NewStateServiceClient(conn),
	}, nil
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
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	resp, err := c.config.GetRunning(ctx, &apiv1.GetRunningRequest{})
	if err != nil {
		return "", 0, err
	}
	return resp.GetConfigText(), resp.GetVersion(), nil
}

// GetCandidate returns a session's candidate configuration text.
func (c *Client) GetCandidate(ctx context.Context, sessionID string) (string, error) {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	resp, err := c.config.GetCandidate(ctx, &apiv1.GetCandidateRequest{SessionId: sessionID})
	if err != nil {
		return "", err
	}
	return resp.GetConfigText(), nil
}

// EditCandidate sends set-command text to the candidate config.
func (c *Client) EditCandidate(ctx context.Context, sessionID, configText string) error {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	_, err := c.config.EditCandidate(ctx, &apiv1.EditCandidateRequest{
		SessionId:  sessionID,
		ConfigText: configText,
	})
	return err
}

// Commit commits the candidate configuration.
func (c *Client) Commit(ctx context.Context, sessionID, user, message string) (commitID string, version uint64, err error) {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	resp, err := c.config.Commit(ctx, &apiv1.CommitRequest{
		SessionId: sessionID,
		User:      user,
		Message:   message,
	})
	if err != nil {
		return "", 0, err
	}
	return resp.GetCommitId(), resp.GetVersion(), nil
}

// ValidateCandidate validates a session's candidate configuration without committing it.
func (c *Client) ValidateCandidate(ctx context.Context, sessionID string) error {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	_, err := c.config.ValidateCandidate(ctx, &apiv1.ValidateCandidateRequest{SessionId: sessionID})
	return err
}

// Discard discards candidate changes.
func (c *Client) Discard(ctx context.Context, sessionID string) error {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	_, err := c.config.Discard(ctx, &apiv1.DiscardRequest{SessionId: sessionID})
	return err
}

// Rollback rolls back running configuration to a previous commit.
func (c *Client) Rollback(ctx context.Context, sessionID, commitID, user, message string) (newCommitID string, version uint64, err error) {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	resp, err := c.config.Rollback(ctx, &apiv1.RollbackRequest{
		SessionId: sessionID,
		CommitId:  commitID,
		User:      user,
		Message:   message,
	})
	if err != nil {
		return "", 0, err
	}
	return resp.GetNewCommitId(), resp.GetVersion(), nil
}

// Diff returns the diff between candidate and running.
func (c *Client) Diff(ctx context.Context, sessionID string) (diffText string, hasChanges bool, err error) {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	resp, err := c.config.Diff(ctx, &apiv1.DiffRequest{SessionId: sessionID})
	if err != nil {
		return "", false, err
	}
	return resp.GetDiffText(), resp.GetHasChanges(), nil
}

// ListHistory returns commit history.
func (c *Client) ListHistory(ctx context.Context, limit, offset int) ([]CommitInfo, error) {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	resp, err := c.config.ListHistory(ctx, &apiv1.ListHistoryRequest{Limit: int32(limit), Offset: int32(offset)})
	if err != nil {
		return nil, err
	}
	return commitInfosFromProto(resp.GetEntries()), nil
}

// --- Session operations ---

// CreateSession creates a new configuration session.
func (c *Client) CreateSession(ctx context.Context, user string) (sessionID string, err error) {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	resp, err := c.session.CreateSession(ctx, &apiv1.CreateSessionRequest{User: user})
	if err != nil {
		return "", err
	}
	return resp.GetSessionId(), nil
}

// CloseSession closes a configuration session.
func (c *Client) CloseSession(ctx context.Context, sessionID string) error {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	_, err := c.session.CloseSession(ctx, &apiv1.CloseSessionRequest{SessionId: sessionID})
	return err
}

// AcquireLock acquires the candidate lock.
func (c *Client) AcquireLock(ctx context.Context, sessionID, user string) error {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	_, err := c.session.AcquireLock(ctx, &apiv1.AcquireLockRequest{
		SessionId: sessionID,
		User:      user,
	})
	return err
}

// ReleaseLock releases the candidate lock.
func (c *Client) ReleaseLock(ctx context.Context, sessionID string) error {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	_, err := c.session.ReleaseLock(ctx, &apiv1.ReleaseLockRequest{SessionId: sessionID})
	return err
}

// --- State queries ---

// GetInterfaces returns interface operational state.
func (c *Client) GetInterfaces(ctx context.Context, nameFilter string) ([]InterfaceInfo, error) {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	resp, err := c.state.GetInterfaces(ctx, &apiv1.GetInterfacesRequest{NameFilter: nameFilter})
	if err != nil {
		return nil, err
	}
	return interfaceInfosFromProto(resp.GetInterfaces()), nil
}

// GetRoutes returns routing table entries.
func (c *Client) GetRoutes(ctx context.Context, prefixFilter, protoFilter string) ([]RouteInfo, error) {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	resp, err := c.state.GetRoutes(ctx, &apiv1.GetRoutesRequest{
		PrefixFilter:   prefixFilter,
		ProtocolFilter: protoFilter,
	})
	if err != nil {
		return nil, err
	}
	return routeInfosFromProto(resp.GetRoutes()), nil
}

// GetBGPNeighbors returns BGP neighbor state.
func (c *Client) GetBGPNeighbors(ctx context.Context) ([]BGPNeighborInfo, error) {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	resp, err := c.state.GetBGPNeighbors(ctx, &apiv1.GetBGPNeighborsRequest{})
	if err != nil {
		return nil, err
	}
	return bgpNeighborInfosFromProto(resp.GetNeighbors()), nil
}

// GetRouteText returns FRR routing table output.
func (c *Client) GetRouteText(ctx context.Context, protoFilter string) (string, error) {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	resp, err := c.state.GetRouteText(ctx, &apiv1.GetRouteTextRequest{ProtocolFilter: protoFilter})
	if err != nil {
		return "", err
	}
	return resp.GetOutput(), nil
}

// GetBGPSummaryText returns FRR BGP summary output.
func (c *Client) GetBGPSummaryText(ctx context.Context) (string, error) {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	resp, err := c.state.GetBGPSummaryText(ctx, &apiv1.GetBGPSummaryTextRequest{})
	if err != nil {
		return "", err
	}
	return resp.GetOutput(), nil
}

// GetBGPNeighborText returns FRR BGP neighbor detail output.
func (c *Client) GetBGPNeighborText(ctx context.Context, peerAddress string) (string, error) {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	resp, err := c.state.GetBGPNeighborText(ctx, &apiv1.GetBGPNeighborTextRequest{PeerAddress: peerAddress})
	if err != nil {
		return "", err
	}
	return resp.GetOutput(), nil
}

// GetOSPFNeighborsText returns FRR OSPF neighbor output.
func (c *Client) GetOSPFNeighborsText(ctx context.Context) (string, error) {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	resp, err := c.state.GetOSPFNeighborsText(ctx, &apiv1.GetOSPFNeighborsTextRequest{})
	if err != nil {
		return "", err
	}
	return resp.GetOutput(), nil
}

// GetSystemInfo returns system information.
func (c *Client) GetSystemInfo(ctx context.Context) (*SystemInfo, error) {
	ctx, cancel := contextWithDefaultTimeout(ctx)
	defer cancel()
	resp, err := c.state.GetSystemInfo(ctx, &apiv1.GetSystemInfoRequest{})
	if err != nil {
		return nil, err
	}
	return &SystemInfo{
		Hostname:   resp.GetHostname(),
		Version:    resp.GetVersion(),
		UptimeSecs: resp.GetUptimeSecs(),
	}, nil
}

func contextWithDefaultTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, 10*time.Second)
}

func commitInfosFromProto(entries []*apiv1.CommitEntry) []CommitInfo {
	infos := make([]CommitInfo, 0, len(entries))
	for _, entry := range entries {
		timestamp, err := time.Parse(time.RFC3339Nano, entry.GetTimestamp())
		if err != nil {
			timestamp = time.Time{}
		}
		infos = append(infos, CommitInfo{
			CommitID:   entry.GetCommitId(),
			User:       entry.GetUser(),
			Timestamp:  timestamp,
			Message:    entry.GetMessage(),
			IsRollback: entry.GetIsRollback(),
		})
	}
	return infos
}

func interfaceInfosFromProto(interfaces []*apiv1.InterfaceState) []InterfaceInfo {
	infos := make([]InterfaceInfo, 0, len(interfaces))
	for _, iface := range interfaces {
		infos = append(infos, InterfaceInfo{
			Name:        iface.GetName(),
			AdminStatus: iface.GetAdminStatus(),
			OperStatus:  iface.GetOperStatus(),
			Speed:       iface.GetSpeed(),
			MTU:         iface.GetMtu(),
			MAC:         iface.GetMac(),
			RxPackets:   iface.GetRxPackets(),
			TxPackets:   iface.GetTxPackets(),
			RxBytes:     iface.GetRxBytes(),
			TxBytes:     iface.GetTxBytes(),
			RxErrors:    iface.GetRxErrors(),
			TxErrors:    iface.GetTxErrors(),
			RxQueues:    rxQueueInfosFromProto(iface.GetRxQueues()),
			TxQueues:    txQueueInfosFromProto(iface.GetTxQueues()),
		})
	}
	return infos
}

func rxQueueInfosFromProto(queues []*apiv1.InterfaceRxQueue) []InterfaceRxQueueInfo {
	infos := make([]InterfaceRxQueueInfo, 0, len(queues))
	for _, queue := range queues {
		infos = append(infos, InterfaceRxQueueInfo{
			QueueID:  queue.GetQueueId(),
			WorkerID: queue.GetWorkerId(),
			Mode:     queue.GetMode(),
		})
	}
	return infos
}

func txQueueInfosFromProto(queues []*apiv1.InterfaceTxQueue) []InterfaceTxQueueInfo {
	infos := make([]InterfaceTxQueueInfo, 0, len(queues))
	for _, queue := range queues {
		infos = append(infos, InterfaceTxQueueInfo{
			QueueID: queue.GetQueueId(),
			Shared:  queue.GetShared(),
			Threads: append([]uint32(nil), queue.GetThreads()...),
		})
	}
	return infos
}

func routeInfosFromProto(routes []*apiv1.RouteEntry) []RouteInfo {
	infos := make([]RouteInfo, 0, len(routes))
	for _, route := range routes {
		infos = append(infos, RouteInfo{
			Prefix:    route.GetPrefix(),
			NextHop:   route.GetNextHop(),
			Protocol:  route.GetProtocol(),
			Metric:    route.GetMetric(),
			Interface: route.GetInterface(),
			Active:    route.GetActive(),
		})
	}
	return infos
}

func bgpNeighborInfosFromProto(neighbors []*apiv1.BGPNeighborState) []BGPNeighborInfo {
	infos := make([]BGPNeighborInfo, 0, len(neighbors))
	for _, neighbor := range neighbors {
		infos = append(infos, BGPNeighborInfo{
			PeerAddress:    neighbor.GetPeerAddress(),
			PeerAS:         neighbor.GetPeerAs(),
			State:          neighbor.GetState(),
			UptimeSecs:     neighbor.GetUptimeSecs(),
			PrefixReceived: neighbor.GetPrefixReceived(),
			PrefixSent:     neighbor.GetPrefixSent(),
		})
	}
	return infos
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
	RxQueues    []InterfaceRxQueueInfo
	TxQueues    []InterfaceTxQueueInfo
}

// InterfaceRxQueueInfo maps an RX queue to a VPP worker.
type InterfaceRxQueueInfo struct {
	QueueID  uint32
	WorkerID uint32
	Mode     string
}

// InterfaceTxQueueInfo maps a TX queue to VPP worker threads.
type InterfaceTxQueueInfo struct {
	QueueID uint32
	Shared  bool
	Threads []uint32
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
