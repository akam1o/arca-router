package vpp

import (
	"context"
	"fmt"
	"sync"

	"github.com/akam1o/arca-router/pkg/errors"
)

// LCPStateManager manages LCP interface pairs state and provides caching
type LCPStateManager struct {
	mu          sync.RWMutex
	persistMu   sync.Mutex // Separate lock for persistence to serialize snapshots
	client      Client
	persistence *LCPPersistence
	// cache maps VPP sw_if_index to LCP interface information
	cache map[uint32]*LCPInterface
}

// NewLCPStateManager creates a new LCP state manager
func NewLCPStateManager(client Client) *LCPStateManager {
	return &LCPStateManager{
		client:      client,
		persistence: NewLCPPersistence(),
		cache:       make(map[uint32]*LCPInterface),
	}
}

// NewLCPStateManagerWithPersistence creates a new LCP state manager with custom persistence path
func NewLCPStateManagerWithPersistence(client Client, persistencePath string) *LCPStateManager {
	return &LCPStateManager{
		client:      client,
		persistence: NewLCPPersistenceWithPath(persistencePath),
		cache:       make(map[uint32]*LCPInterface),
	}
}

// Sync synchronizes the local cache with VPP's actual LCP state
// This should be called at startup or after connection to VPP
//
// Sync now restores Junos names from persistent storage, allowing Junos name
// mappings to survive daemon restarts.
//
// IMPORTANT: Sync should only be called at startup before any Create/Delete operations.
// Calling Sync concurrently with Create/Delete may result in stale Junos name mappings.
//
// Returns an error if VPP sync fails (critical) or persistence load fails (warning).
// If persistence fails, VPP sync still succeeds but Junos names may be incomplete.
func (m *LCPStateManager) Sync(ctx context.Context) error {
	// Step 1: Retrieve all LCP pairs from VPP (don't hold lock during I/O)
	lcpInterfaces, err := m.client.ListLCPInterfaces(ctx)
	if err != nil {
		return errors.Wrap(err, errors.ErrCodeVPPOperation,
			"Failed to list LCP interfaces",
			"Could not retrieve LCP interface pairs from VPP",
			"Ensure VPP is running and linux-cp plugin is loaded")
	}

	// Step 2: Load persisted Junos name mappings (don't hold lock during I/O)
	persistedMappings, persistErr := m.persistence.LoadMapping()
	if persistErr != nil {
		// Log error but continue - we can still sync from VPP without Junos names
		// Caller should log this warning
		persistedMappings = []*LCPMapping{}
	}

	// Validate persisted mappings
	validationErrors := ValidateMapping(persistedMappings)
	if len(validationErrors) > 0 {
		// Log validation errors but continue with empty mappings
		// Caller should log this warning
		if persistErr == nil {
			persistErr = fmt.Errorf("persisted LCP mappings validation failed: %v", validationErrors)
		}
		persistedMappings = []*LCPMapping{}
	}

	// Step 3: Build map of Junos names by linux_name for safe lookup
	// Using linux_name as key instead of sw_if_index because sw_if_index
	// may change across VPP restarts, while linux_name is stable
	junosNameMap := make(map[string]string)
	for _, mapping := range persistedMappings {
		junosNameMap[mapping.LinuxName] = mapping.JunosName
	}

	// Step 4: Build new cache (now holding lock for cache update only)
	m.mu.Lock()
	defer m.mu.Unlock()

	// Clear existing cache
	m.cache = make(map[uint32]*LCPInterface)

	// Populate cache with current state, restoring Junos names from persistence
	for _, lcp := range lcpInterfaces {
		// Restore Junos name from persistence if available (matched by linux_name)
		if junosName, exists := junosNameMap[lcp.LinuxIfName]; exists {
			lcp.JunosName = junosName
		}
		m.cache[lcp.VPPSwIfIndex] = lcp
	}

	// Return persistence error as warning (VPP sync succeeded)
	return persistErr
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
// Also persists the Junos name mapping to disk for restart resilience
func (m *LCPStateManager) Create(ctx context.Context, ifIndex uint32, linuxIfName, junosName string) error {
	// Create LCP pair in VPP
	if err := m.client.CreateLCPInterface(ctx, ifIndex, linuxIfName); err != nil {
		return err
	}

	// Acquire persist lock first to serialize persistence operations
	// This prevents concurrent Create/Delete from saving out-of-order snapshots
	// Order: persistMu → mu → snapshot → mu unlock (avoids blocking readers during I/O)
	m.persistMu.Lock()
	defer m.persistMu.Unlock()

	// Update cache (hold lock only for cache update and snapshot)
	m.mu.Lock()
	m.cache[ifIndex] = &LCPInterface{
		VPPSwIfIndex: ifIndex,
		LinuxIfName:  linuxIfName,
		JunosName:    junosName,
		HostIfType:   "tap",
		Netns:        "",
	}

	// Snapshot cache for persistence (to release cache lock before I/O)
	mappings := make([]*LCPMapping, 0, len(m.cache))
	for _, lcp := range m.cache {
		mappings = append(mappings, &LCPMapping{
			SwIfIndex:  lcp.VPPSwIfIndex,
			LinuxName:  lcp.LinuxIfName,
			JunosName:  lcp.JunosName,
			HostIfType: lcp.HostIfType,
			Netns:      lcp.Netns,
		})
	}
	m.mu.Unlock()

	// Persist mapping to disk (I/O outside of cache lock, inside persist lock)
	if persistErr := m.persistence.SaveMapping(mappings); persistErr != nil {
		// Persistence failure is not fatal - LCP is created in VPP
		// Caller should log this as a warning
		// Return error so caller can decide how to handle
		return errors.Wrap(persistErr, errors.ErrCodeSystemError,
			"LCP created but failed to persist Junos name mapping",
			"Junos name will be lost on daemon restart",
			"Check /var/lib/arca-router/ permissions and disk space")
	}

	return nil
}

// RegisterExisting registers an existing LCP pair in the cache without creating it in VPP
// This is used during reconciliation to update the cache with existing LCP pairs
func (m *LCPStateManager) RegisterExisting(ifIndex uint32, linuxIfName, junosName string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.cache[ifIndex] = &LCPInterface{
		VPPSwIfIndex: ifIndex,
		LinuxIfName:  linuxIfName,
		JunosName:    junosName,
		HostIfType:   "tap",
		Netns:        "",
	}
}

// Delete removes an LCP interface pair and updates the cache
// Also updates the persisted Junos name mapping
func (m *LCPStateManager) Delete(ctx context.Context, ifIndex uint32) error {
	// Delete LCP pair from VPP
	if err := m.client.DeleteLCPInterface(ctx, ifIndex); err != nil {
		return err
	}

	// Acquire persist lock first to serialize persistence operations
	// This prevents concurrent Create/Delete from saving out-of-order snapshots
	// Order: persistMu → mu → snapshot → mu unlock (avoids blocking readers during I/O)
	m.persistMu.Lock()
	defer m.persistMu.Unlock()

	// Remove from cache (hold lock only for cache update and snapshot)
	m.mu.Lock()
	delete(m.cache, ifIndex)

	// Snapshot cache for persistence (to release cache lock before I/O)
	mappings := make([]*LCPMapping, 0, len(m.cache))
	for _, lcp := range m.cache {
		mappings = append(mappings, &LCPMapping{
			SwIfIndex:  lcp.VPPSwIfIndex,
			LinuxName:  lcp.LinuxIfName,
			JunosName:  lcp.JunosName,
			HostIfType: lcp.HostIfType,
			Netns:      lcp.Netns,
		})
	}
	m.mu.Unlock()

	// Persist mapping to disk (I/O outside of cache lock, inside persist lock)
	if persistErr := m.persistence.SaveMapping(mappings); persistErr != nil {
		// Persistence failure is not fatal - LCP is deleted from VPP
		// Caller should log this as a warning
		return errors.Wrap(persistErr, errors.ErrCodeSystemError,
			"LCP deleted but failed to update persisted Junos name mapping",
			"Stale entry may remain in persistence file",
			"Check /var/lib/arca-router/ permissions and disk space")
	}

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
