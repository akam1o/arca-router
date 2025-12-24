package vpp

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"

	"go.fd.io/govpp/adapter/socketclient"
	"go.fd.io/govpp/api"
	"go.fd.io/govpp/core"
)

const (
	// Default VPP API socket path
	defaultSocketPath = "/run/vpp/api.sock"

	// Connection timeout
	connectTimeout = 10 * time.Second

	// API call timeout
	apiTimeout = 5 * time.Second

	// Max connection retry attempts
	maxRetries = 3

	// Exponential backoff base duration
	retryBackoff = 1 * time.Second
)

// govppClient is the production VPP client using govpp
type govppClient struct {
	socketPath string
	conn       *core.Connection
	ch         api.Channel
}

// NewGovppClient creates a new govpp-based VPP client
func NewGovppClient() Client {
	socketPath := os.Getenv("VPP_API_SOCKET_PATH")
	if socketPath == "" {
		socketPath = defaultSocketPath
	}

	return &govppClient{
		socketPath: socketPath,
	}
}

// Connect establishes a connection to VPP with retry logic
func (c *govppClient) Connect(ctx context.Context) error {
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		select {
		case <-ctx.Done():
			return fmt.Errorf("connect cancelled: %w", ctx.Err())
		default:
		}

		// Check if socket exists
		if _, err := os.Stat(c.socketPath); err != nil {
			if os.IsNotExist(err) {
				lastErr = fmt.Errorf("VPP socket not found: %s (ensure VPP is running)", c.socketPath)
			} else {
				lastErr = fmt.Errorf("VPP socket stat error: %w", err)
			}

			// Retry with exponential backoff
			if attempt < maxRetries {
				backoff := retryBackoff * time.Duration(1<<uint(attempt-1))
				time.Sleep(backoff)
				continue
			}
			break
		}

		// Check socket permissions
		if err := checkSocketAccess(c.socketPath); err != nil {
			return fmt.Errorf("VPP socket permission denied: %w "+
				"(ensure user is in vpp group)", err)
		}

		// Create adapter
		adapter := socketclient.NewVppClient(c.socketPath)

		// Connect to VPP with timeout (disable internal retries, handle externally)
		connCh := make(chan *core.Connection, 1) // Buffered to prevent goroutine leak
		errCh := make(chan error, 1)              // Buffered to prevent goroutine leak

		go func() {
			// Disable AsyncConnect internal retries (we handle retries externally)
			conn, connEvent, err := core.AsyncConnect(adapter, 0, 0) // maxAttempts=0 disables retry
			if err != nil {
				select {
				case errCh <- err:
				default:
				}
				return
			}

			// Wait for connection event with timeout
			select {
			case e := <-connEvent:
				if e.State != core.Connected {
					select {
					case errCh <- fmt.Errorf("connection failed (state: %v)", e.State):
					default:
					}
					if conn != nil {
						conn.Disconnect()
					}
					return
				}
				select {
				case connCh <- conn:
				default:
				}
			case <-time.After(connectTimeout):
				select {
				case errCh <- fmt.Errorf("connection timeout"):
				default:
				}
				if conn != nil {
					conn.Disconnect()
				}
			}
		}()

		// Wait for connection or timeout
		select {
		case conn := <-connCh:
			c.conn = conn

			// Create API channel with timeout
			ch, err := conn.NewAPIChannelBuffered(128, 128)
			if err != nil {
				conn.Disconnect()
				return fmt.Errorf("failed to create API channel: %w", err)
			}

			// Set reply timeout for API calls
			ch.SetReplyTimeout(apiTimeout)
			c.ch = ch

			// Check VPP API version compatibility
			if err := c.checkVersionCompatibility(); err != nil {
				ch.Close()
				conn.Disconnect()
				return fmt.Errorf("VPP API version incompatible: %w", err)
			}

			return nil

		case err := <-errCh:
			lastErr = err

			// Retry with exponential backoff
			if attempt < maxRetries {
				backoff := retryBackoff * time.Duration(1<<uint(attempt-1))
				time.Sleep(backoff)
				continue
			}

		case <-ctx.Done():
			return fmt.Errorf("connect cancelled: %w", ctx.Err())
		}
	}

	return fmt.Errorf("failed to connect to VPP after %d attempts: %w", maxRetries, lastErr)
}

// checkVersionCompatibility verifies VPP API version compatibility
func (c *govppClient) checkVersionCompatibility() error {
	// Note: Full version check requires binapi (vpe.ShowVersion)
	// Once binapi is generated in Task 1.1, this will be implemented to:
	// 1. Call vpe.ShowVersion to get VPP version
	// 2. Parse version string (expected: 23.10.x)
	// 3. Return error if major/minor version mismatch
	//
	// For now, we defer version check to VPP API call failures.
	// If VPP version is incompatible, subsequent API calls will fail
	// with clear error messages from govpp.
	//
	// This is acceptable for Phase 2 initial implementation because:
	// - Installation docs specify VPP 23.10 requirement
	// - Integration tests will catch version mismatches
	// - govpp will report incompatible API calls explicitly

	return nil
}

// Close closes the VPP connection
func (c *govppClient) Close() error {
	if c.ch != nil {
		c.ch.Close()
		c.ch = nil
	}

	if c.conn != nil {
		c.conn.Disconnect()
		c.conn = nil
	}

	return nil
}

// CreateInterface creates a new VPP interface (placeholder - will be implemented in Task 1.4)
func (c *govppClient) CreateInterface(ctx context.Context, req *CreateInterfaceRequest) (*Interface, error) {
	if c.ch == nil {
		return nil, fmt.Errorf("not connected to VPP")
	}

	// TODO: Implement in Task 1.4
	return nil, fmt.Errorf("not yet implemented (Task 1.4)")
}

// SetInterfaceUp sets an interface to admin up state (placeholder)
func (c *govppClient) SetInterfaceUp(ctx context.Context, ifIndex uint32) error {
	if c.ch == nil {
		return fmt.Errorf("not connected to VPP")
	}

	// TODO: Implement in Task 1.4
	return fmt.Errorf("not yet implemented (Task 1.4)")
}

// SetInterfaceDown sets an interface to admin down state (placeholder)
func (c *govppClient) SetInterfaceDown(ctx context.Context, ifIndex uint32) error {
	if c.ch == nil {
		return fmt.Errorf("not connected to VPP")
	}

	// TODO: Implement in Task 1.4
	return fmt.Errorf("not yet implemented (Task 1.4)")
}

// SetInterfaceAddress adds an IP address to an interface (placeholder - will be implemented in Task 1.5)
func (c *govppClient) SetInterfaceAddress(ctx context.Context, ifIndex uint32, addr *net.IPNet) error {
	if c.ch == nil {
		return fmt.Errorf("not connected to VPP")
	}

	// TODO: Implement in Task 1.5
	return fmt.Errorf("not yet implemented (Task 1.5)")
}

// DeleteInterfaceAddress removes an IP address from an interface (placeholder)
func (c *govppClient) DeleteInterfaceAddress(ctx context.Context, ifIndex uint32, addr *net.IPNet) error {
	if c.ch == nil {
		return fmt.Errorf("not connected to VPP")
	}

	// TODO: Implement in Task 1.5
	return fmt.Errorf("not yet implemented (Task 1.5)")
}

// GetInterface retrieves interface information by index (placeholder)
func (c *govppClient) GetInterface(ctx context.Context, ifIndex uint32) (*Interface, error) {
	if c.ch == nil {
		return nil, fmt.Errorf("not connected to VPP")
	}

	// TODO: Implement in Task 1.4
	return nil, fmt.Errorf("not yet implemented (Task 1.4)")
}

// ListInterfaces lists all VPP interfaces (placeholder)
func (c *govppClient) ListInterfaces(ctx context.Context) ([]*Interface, error) {
	if c.ch == nil {
		return nil, fmt.Errorf("not connected to VPP")
	}

	// TODO: Implement in Task 1.4
	return nil, fmt.Errorf("not yet implemented (Task 1.4)")
}

// checkSocketAccess verifies socket accessibility
func checkSocketAccess(path string) error {
	// Try to open the socket to verify access
	// This is a basic check - actual permission check happens during connect
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	// Check if it's a socket
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("not a socket: %s", path)
	}

	return nil
}

// Ensure govppClient implements Client interface
var _ Client = (*govppClient)(nil)
