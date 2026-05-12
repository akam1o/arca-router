package vpp

import "testing"

func TestParseCIDRAddressPreservesHostIP(t *testing.T) {
	tests := []struct {
		name string
		cidr string
		want string
	}{
		{name: "IPv4", cidr: "192.0.2.1/24", want: "192.0.2.1"},
		{name: "IPv6", cidr: "2001:db8::1/64", want: "2001:db8::1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ipNet, err := ParseCIDRAddress(tt.cidr)
			if err != nil {
				t.Fatalf("ParseCIDRAddress() error = %v", err)
			}
			if got := ipNet.IP.String(); got != tt.want {
				t.Fatalf("ParseCIDRAddress().IP = %s, want %s", got, tt.want)
			}
		})
	}
}
