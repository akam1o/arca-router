package vpp

import "net"

// ParseCIDRAddress parses a CIDR interface address while preserving the host IP.
func ParseCIDRAddress(cidr string) (*net.IPNet, error) {
	ip, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, err
	}
	ipNet.IP = ip
	return ipNet, nil
}
