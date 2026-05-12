package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/akam1o/arca-router/pkg/vpp"
)

// cmdShowInterfaces displays VPP interface status with LCP information
func cmdShowInterfaces(ctx context.Context, args []string, f *flags) int {
	// Create VPP client
	client, err := createVPPClient(f)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to create VPP client: %v\n", err)
		return ExitOperationError
	}
	defer func() {
		if err := client.Close(); err != nil {
			_ = err
		}
	}()

	// Connect to VPP
	debugLog(f, "Connecting to VPP at %s", f.vppSocket)
	if err := client.Connect(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to connect to VPP: %v\n", err)
		fmt.Fprintf(os.Stderr, "Hint: Ensure VPP is running and the socket path is correct\n")
		return ExitOperationError
	}

	// List all interfaces
	debugLog(f, "Fetching VPP interfaces")
	interfaces, err := client.ListInterfaces(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to list interfaces: %v\n", err)
		return ExitOperationError
	}

	// List all LCP pairs (non-fatal if LCP plugin is unavailable)
	debugLog(f, "Fetching LCP pairs")
	lcpPairs, err := client.ListLCPInterfaces(ctx)
	lcpMap := make(map[uint32]*vpp.LCPInterface)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to list LCP interfaces (LCP plugin may not be enabled): %v\n", err)
		debugLog(f, "Continuing without LCP information")
	} else {
		// Build LCP map for efficient lookup: VPP SwIfIndex -> LCP info
		for _, lcp := range lcpPairs {
			lcpMap[lcp.VPPSwIfIndex] = lcp
		}
		debugLog(f, "Built LCP map with %d entries", len(lcpMap))
	}

	// If specific interface name is provided, show detailed info
	if len(args) > 0 {
		if len(args) > 1 {
			fmt.Fprintf(os.Stderr, "Error: 'show interfaces' accepts at most one interface name\n\n")
			showUsage()
			return ExitUsageError
		}
		return showInterfaceDetail(interfaces, lcpMap, args[0])
	}

	// Show all interfaces in table format
	return showInterfaceTable(interfaces, lcpMap)
}

// showInterfaceTable displays interfaces in table format
func showInterfaceTable(interfaces []*vpp.Interface, lcpMap map[uint32]*vpp.LCPInterface) int {
	if len(interfaces) == 0 {
		fmt.Println("No interfaces found")
		return ExitSuccess
	}

	// Sort interfaces by SwIfIndex for stable output
	sort.Slice(interfaces, func(i, j int) bool {
		return interfaces[i].SwIfIndex < interfaces[j].SwIfIndex
	})

	headers := []string{"Index", "Name", "State", "Link", "IP Addresses", "Linux Name"}
	var rows [][]string

	for _, iface := range interfaces {
		// Determine admin state
		adminState := "down"
		if iface.AdminUp {
			adminState = "up"
		}

		// Determine link state
		linkState := "down"
		if iface.LinkUp {
			linkState = "up"
		}

		// Format IP addresses
		var ipAddrs []string
		for _, addr := range iface.Addresses {
			ipAddrs = append(ipAddrs, addr.String())
		}
		ipAddrStr := strings.Join(ipAddrs, ", ")
		if ipAddrStr == "" {
			ipAddrStr = "-"
		}

		// Get LCP Linux name if exists
		linuxName := "-"
		if lcp, exists := lcpMap[iface.SwIfIndex]; exists && lcp.LinuxIfName != "" {
			linuxName = lcp.LinuxIfName
			if lcp.JunosName != "" {
				linuxName = fmt.Sprintf("%s (%s)", lcp.LinuxIfName, lcp.JunosName)
			}
		}

		row := []string{
			fmt.Sprintf("%d", iface.SwIfIndex),
			iface.Name,
			adminState,
			linkState,
			ipAddrStr,
			linuxName,
		}
		rows = append(rows, row)
	}

	if err := FormatTable(os.Stdout, headers, rows); err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to format table: %v\n", err)
		return ExitOperationError
	}

	return ExitSuccess
}

// showInterfaceDetail displays detailed information for a specific interface
func showInterfaceDetail(interfaces []*vpp.Interface, lcpMap map[uint32]*vpp.LCPInterface, name string) int {
	// Find interface by name
	var targetIface *vpp.Interface
	for _, iface := range interfaces {
		if iface.Name == name {
			targetIface = iface
			break
		}
	}

	if targetIface == nil {
		fmt.Fprintf(os.Stderr, "Error: interface '%s' not found\n", name)
		return ExitOperationError
	}

	// Display detailed information
	fmt.Printf("Interface: %s\n", targetIface.Name)
	fmt.Printf("  VPP Index: %d\n", targetIface.SwIfIndex)
	fmt.Printf("  Admin State: %s\n", ifState(targetIface.AdminUp))
	fmt.Printf("  Link State: %s\n", ifState(targetIface.LinkUp))
	fmt.Printf("  MAC Address: %s\n", targetIface.MAC.String())

	// IP addresses
	if len(targetIface.Addresses) > 0 {
		fmt.Println("  IP Addresses:")
		for _, addr := range targetIface.Addresses {
			fmt.Printf("    %s\n", addr.String())
		}
	} else {
		fmt.Println("  IP Addresses: none")
	}

	// LCP information
	if lcp, exists := lcpMap[targetIface.SwIfIndex]; exists && lcp.LinuxIfName != "" {
		fmt.Println("  LCP Information:")
		fmt.Printf("    Linux Interface: %s\n", lcp.LinuxIfName)
		if lcp.JunosName != "" {
			fmt.Printf("    Junos Name: %s\n", lcp.JunosName)
		}
		if lcp.HostIfType != "" {
			fmt.Printf("    Host Interface Type: %s\n", lcp.HostIfType)
		}
		if lcp.Netns != "" {
			fmt.Printf("    Network Namespace: %s\n", lcp.Netns)
		}
	} else {
		fmt.Println("  LCP Information: not configured")
	}

	return ExitSuccess
}

// ifState converts boolean state to string
func ifState(up bool) string {
	if up {
		return "up"
	}
	return "down"
}

// vppClientFactory is the function type for creating VPP clients
// This allows for dependency injection in tests
type vppClientFactory func(f *flags) (vpp.Client, error)

// defaultVPPClientFactory is the default factory for creating real VPP clients
var defaultVPPClientFactory vppClientFactory = createVPPClientReal

// createVPPClient creates a VPP client instance using the default factory
func createVPPClient(f *flags) (vpp.Client, error) {
	return defaultVPPClientFactory(f)
}

// createVPPClientReal is the real implementation of VPP client creation
func createVPPClientReal(f *flags) (vpp.Client, error) {
	// Set socket path BEFORE creating the client if provided
	if f.vppSocket != "" {
		// govppClient reads VPP_API_SOCKET_PATH during Connect()
		// We must set it before creating the client
		if err := os.Setenv("VPP_API_SOCKET_PATH", f.vppSocket); err != nil {
			return nil, err
		}
	}

	// Create govpp client
	client := vpp.NewGovppClient()

	return client, nil
}
