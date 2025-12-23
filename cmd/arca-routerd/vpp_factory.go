package main

import (
	"github.com/akam1o/arca-router/pkg/vpp"
)

// vppClientFactory creates a VPP client based on configuration
type vppClientFactory func() vpp.Client

// newVPPClientFactory returns a factory function for creating VPP clients
func newVPPClientFactory(useMock bool) vppClientFactory {
	return func() vpp.Client {
		if useMock {
			return vpp.NewMockClient()
		}
		// TODO Phase 2: Implement real VPP client
		// For now, panic to make it clear that real VPP is not yet implemented
		panic("Real VPP client not yet implemented in Phase 1. Use -mock-vpp flag for testing.")
	}
}
