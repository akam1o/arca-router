package vpp

import (
	"context"
	"fmt"
	"sync"

	"github.com/akam1o/arca-router/pkg/errors"
)

// LCPStateManager manages LCP interface pairs state and provides caching
type LCPStateManager struct {
	mu     sync.RWMutex
	client Client
	// cache maps VPP sw_if_index to LCP interface information
	cache map[uint32]*LCPInterface
}

// NewLCPStateManager creates a new LCP state manager
func NewLCPStateManager(client Client) *LCPStateManager {
	return &LCPStateManager{
		client: client,
		cache:  make(map[uint32]*LCPInterface),
	}
}

// Sync synchronizes the local cache with VPP's actual LCP state
// This should be called at startup or after connection to VPP
//
// Note: VPP does not store Junos names, so after Sync(), the JunosName field
// will be empty for LCP interfaces not created through this manager.
// Use Create() to populate both VPP and cache with Junos name associations.
func (m *LCPStateManager) Sync(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Retrieve all LCP pairs from VPP
	lcpInterfaces, err := m.client.ListLCPInterfaces(ctx)
	if err != nil {
		return errors.Wrap(err, errors.ErrCodeVPPOperation,
			"Failed to list LCP interfaces",
			"Could not retrieve LCP interface pairs from VPP",
			"Ensure VPP is running and linux-cp plugin is loaded")
	}

	// Clear existing cache
	m.cache = make(map[uint32]*LCPInterface)

	// Populate cache with current state
	for _, lcp := range lcpInterfaces {
		m.cache[lcp.VPPSwIfIndex] = lcp
	}

	return nil
}

// Get retrieves LCP interface information by VPP sw_if_index
// Returns cached data if available, otherwise queries VPP
func (m *LCPStateManager) Get(ctx context.Context, ifIndex uint32) (*LCPInterface, error) {
	m.mu.RLock()
	if lcp, exists := m.cache[ifIndex]; exists {
		m.mu.RUnlock()
		// Return a copy to prevent external modification
		return &LCPInterface{
			VPPSwIfIndex: lcp.VPPSwIfIndex,
			LinuxIfName:  lcp.LinuxIfName,
			JunosName:    lcp.JunosName,
			HostIfType:   lcp.HostIfType,
			Netns:        lcp.Netns,
		}, nil
	}
	m.mu.RUnlock()

	// Cache miss - query VPP
	lcp, err := m.client.GetLCPInterface(ctx, ifIndex)
	if err != nil {
		return nil, err
	}

	// Update cache with a copy
	cacheLCP := &LCPInterface{
		VPPSwIfIndex: lcp.VPPSwIfIndex,
		LinuxIfName:  lcp.LinuxIfName,
		JunosName:    lcp.JunosName,
		HostIfType:   lcp.HostIfType,
		Netns:        lcp.Netns,
	}
	m.mu.Lock()
	m.cache[ifIndex] = cacheLCP
	m.mu.Unlock()

	// Return a copy to prevent external modification
	return &LCPInterface{
		VPPSwIfIndex: lcp.VPPSwIfIndex,
		LinuxIfName:  lcp.LinuxIfName,
		JunosName:    lcp.JunosName,
		HostIfType:   lcp.HostIfType,
		Netns:        lcp.Netns,
	}, nil
}

// Create creates a new LCP interface pair and updates the cache
func (m *LCPStateManager) Create(ctx context.Context, ifIndex uint32, linuxIfName, junosName string) error {
	// Create LCP pair in VPP
	if err := m.client.CreateLCPInterface(ctx, ifIndex, linuxIfName); err != nil {
		return err
	}

	// Update cache
	m.mu.Lock()
	defer m.mu.Unlock()

	m.cache[ifIndex] = &LCPInterface{
		VPPSwIfIndex: ifIndex,
		LinuxIfName:  linuxIfName,
		JunosName:    junosName,
		HostIfType:   "tap",
		Netns:        "",
	}

	return nil
}

// Delete removes an LCP interface pair and updates the cache
func (m *LCPStateManager) Delete(ctx context.Context, ifIndex uint32) error {
	// Delete LCP pair from VPP
	if err := m.client.DeleteLCPInterface(ctx, ifIndex); err != nil {
		return err
	}

	// Remove from cache
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.cache, ifIndex)

	return nil
}

// List returns all cached LCP interface pairs
// Call Sync() first to ensure cache is up-to-date
func (m *LCPStateManager) List() []*LCPInterface {
	m.mu.RLock()
	defer m.mu.RUnlock()

	interfaces := make([]*LCPInterface, 0, len(m.cache))
	for _, lcp := range m.cache {
		// Return copies to prevent external modification
		interfaces = append(interfaces, &LCPInterface{
			VPPSwIfIndex: lcp.VPPSwIfIndex,
			LinuxIfName:  lcp.LinuxIfName,
			JunosName:    lcp.JunosName,
			HostIfType:   lcp.HostIfType,
			Netns:        lcp.Netns,
		})
	}

	return interfaces
}

// GetByJunosName retrieves LCP interface information by Junos interface name
func (m *LCPStateManager) GetByJunosName(junosName string) (*LCPInterface, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, lcp := range m.cache {
		if lcp.JunosName == junosName {
			// Return a copy
			return &LCPInterface{
				VPPSwIfIndex: lcp.VPPSwIfIndex,
				LinuxIfName:  lcp.LinuxIfName,
				JunosName:    lcp.JunosName,
				HostIfType:   lcp.HostIfType,
				Netns:        lcp.Netns,
			}, nil
		}
	}

	return nil, errors.New(
		errors.ErrCodeVPPOperation,
		fmt.Sprintf("LCP interface not found for Junos name: %s", junosName),
		"LCP interface with specified Junos name does not exist",
		"Ensure the interface has been created with LCP pair",
	)
}

// GetByLinuxName retrieves LCP interface information by Linux interface name
func (m *LCPStateManager) GetByLinuxName(linuxName string) (*LCPInterface, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, lcp := range m.cache {
		if lcp.LinuxIfName == linuxName {
			// Return a copy
			return &LCPInterface{
				VPPSwIfIndex: lcp.VPPSwIfIndex,
				LinuxIfName:  lcp.LinuxIfName,
				JunosName:    lcp.JunosName,
				HostIfType:   lcp.HostIfType,
				Netns:        lcp.Netns,
			}, nil
		}
	}

	return nil, errors.New(
		errors.ErrCodeVPPOperation,
		fmt.Sprintf("LCP interface not found for Linux name: %s", linuxName),
		"LCP interface with specified Linux name does not exist",
		"Ensure the interface has been created with LCP pair",
	)
}

// CheckConsistency verifies that the cache is consistent with VPP state
// Returns a list of inconsistencies found
func (m *LCPStateManager) CheckConsistency(ctx context.Context) ([]string, error) {
	// Retrieve current state from VPP
	vppLCPs, err := m.client.ListLCPInterfaces(ctx)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrCodeVPPOperation,
			"Failed to check LCP consistency",
			"Could not retrieve LCP interfaces from VPP",
			"Ensure VPP is running and accessible")
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	inconsistencies := make([]string, 0)

	// Build VPP state map
	vppState := make(map[uint32]*LCPInterface)
	for _, lcp := range vppLCPs {
		vppState[lcp.VPPSwIfIndex] = lcp
	}

	// Check for interfaces in cache but not in VPP
	for ifIndex, cachedLCP := range m.cache {
		if vppLCP, exists := vppState[ifIndex]; !exists {
			inconsistencies = append(inconsistencies,
				fmt.Sprintf("Interface %d (%s) exists in cache but not in VPP",
					ifIndex, cachedLCP.LinuxIfName))
		} else if vppLCP.LinuxIfName != cachedLCP.LinuxIfName {
			inconsistencies = append(inconsistencies,
				fmt.Sprintf("Interface %d has mismatched Linux name: cache=%s, vpp=%s",
					ifIndex, cachedLCP.LinuxIfName, vppLCP.LinuxIfName))
		}
	}

	// Check for interfaces in VPP but not in cache
	for ifIndex, vppLCP := range vppState {
		if _, exists := m.cache[ifIndex]; !exists {
			inconsistencies = append(inconsistencies,
				fmt.Sprintf("Interface %d (%s) exists in VPP but not in cache",
					ifIndex, vppLCP.LinuxIfName))
		}
	}

	return inconsistencies, nil
}

// Clear clears the cache (for testing or manual state management)
func (m *LCPStateManager) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cache = make(map[uint32]*LCPInterface)
}
