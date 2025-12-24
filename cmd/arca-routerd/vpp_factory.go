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
		// Phase 2: Real VPP client via govpp
		return vpp.NewGovppClient()
	}
}
