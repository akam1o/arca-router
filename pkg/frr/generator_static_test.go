package frr

import (
	"strings"
	"testing"
)

func TestGenerateStaticRouteConfig(t *testing.T) {
	tests := []struct {
		name    string
		routes  []StaticRoute
		want    []string
		wantErr bool
	}{
		{
			name: "single IPv4 static route",
			routes: []StaticRoute{
				{
					Prefix:  "0.0.0.0/0",
					NextHop: "10.0.1.254",
				},
			},
			want: []string{
				"ip route 0.0.0.0/0 10.0.1.254",
			},
			wantErr: false,
		},
		{
			name: "IPv4 static route with distance",
			routes: []StaticRoute{
				{
					Prefix:   "192.168.100.0/24",
					NextHop:  "192.168.1.254",
					Distance: 10,
				},
			},
			want: []string{
				"ip route 192.168.100.0/24 192.168.1.254 10",
			},
			wantErr: false,
		},
		{
			name: "IPv6 static route",
			routes: []StaticRoute{
				{
					Prefix:  "2001:db8::/32",
					NextHop: "2001:db8::1",
					IsIPv6:  true,
				},
			},
			want: []string{
				"ipv6 route 2001:db8::/32 2001:db8::1",
			},
			wantErr: false,
		},
		{
			name: "IPv6 static route with distance",
			routes: []StaticRoute{
				{
					Prefix:   "2001:db8:1::/48",
					NextHop:  "2001:db8::ffff",
					Distance: 20,
					IsIPv6:   true,
				},
			},
			want: []string{
				"ipv6 route 2001:db8:1::/48 2001:db8::ffff 20",
			},
			wantErr: false,
		},
		{
			name: "multiple static routes (sorted)",
			routes: []StaticRoute{
				{Prefix: "192.168.2.0/24", NextHop: "10.0.1.1"},
				{Prefix: "0.0.0.0/0", NextHop: "10.0.1.254"},
				{Prefix: "192.168.1.0/24", NextHop: "10.0.1.2"},
			},
			want: []string{
				"ip route 0.0.0.0/0 10.0.1.254",
				"ip route 192.168.1.0/24 10.0.1.2",
				"ip route 192.168.2.0/24 10.0.1.1",
			},
			wantErr: false,
		},
		{
			name: "mixed IPv4 and IPv6 routes",
			routes: []StaticRoute{
				{Prefix: "10.0.0.0/8", NextHop: "10.0.1.254"},
				{Prefix: "2001:db8::/32", NextHop: "2001:db8::1", IsIPv6: true},
			},
			want: []string{
				"ip route 10.0.0.0/8 10.0.1.254",
				"ipv6 route 2001:db8::/32 2001:db8::1",
			},
			wantErr: false,
		},
		{
			name:    "empty routes",
			routes:  []StaticRoute{},
			want:    []string{},
			wantErr: false,
		},
		{
			name:    "nil routes",
			routes:  nil,
			want:    []string{},
			wantErr: false,
		},
		{
			name: "missing prefix",
			routes: []StaticRoute{
				{Prefix: "", NextHop: "10.0.1.254"},
			},
			wantErr: true,
		},
		{
			name: "invalid prefix (not CIDR)",
			routes: []StaticRoute{
				{Prefix: "10.0.1.0", NextHop: "10.0.1.254"},
			},
			wantErr: true,
		},
		{
			name: "missing next-hop",
			routes: []StaticRoute{
				{Prefix: "10.0.0.0/8", NextHop: ""},
			},
			wantErr: true,
		},
		{
			name: "invalid next-hop IP",
			routes: []StaticRoute{
				{Prefix: "10.0.0.0/8", NextHop: "invalid-ip"},
			},
			wantErr: true,
		},
		{
			name: "invalid distance (negative)",
			routes: []StaticRoute{
				{Prefix: "10.0.0.0/8", NextHop: "10.0.1.254", Distance: -1},
			},
			wantErr: true,
		},
		{
			name: "invalid distance (too large)",
			routes: []StaticRoute{
				{Prefix: "10.0.0.0/8", NextHop: "10.0.1.254", Distance: 256},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GenerateStaticRouteConfig(tt.routes)
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateStaticRouteConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				for _, want := range tt.want {
					if !strings.Contains(got, want) {
						t.Errorf("GenerateStaticRouteConfig() output missing expected string:\nWant: %s\nGot:\n%s", want, got)
					}
				}
			}
		})
	}
}

func TestValidateStaticRoute(t *testing.T) {
	tests := []struct {
		name    string
		route   *StaticRoute
		wantErr bool
	}{
		{
			name: "valid IPv4 route",
			route: &StaticRoute{
				Prefix:  "10.0.0.0/8",
				NextHop: "10.0.1.254",
			},
			wantErr: false,
		},
		{
			name: "valid IPv6 route",
			route: &StaticRoute{
				Prefix:  "2001:db8::/32",
				NextHop: "2001:db8::1",
				IsIPv6:  true,
			},
			wantErr: false,
		},
		{
			name: "valid route with distance",
			route: &StaticRoute{
				Prefix:   "192.168.0.0/16",
				NextHop:  "192.168.1.1",
				Distance: 100,
			},
			wantErr: false,
		},
		{
			name: "missing prefix",
			route: &StaticRoute{
				Prefix:  "",
				NextHop: "10.0.1.254",
			},
			wantErr: true,
		},
		{
			name: "invalid CIDR",
			route: &StaticRoute{
				Prefix:  "10.0.0.0",
				NextHop: "10.0.1.254",
			},
			wantErr: true,
		},
		{
			name: "missing next-hop",
			route: &StaticRoute{
				Prefix:  "10.0.0.0/8",
				NextHop: "",
			},
			wantErr: true,
		},
		{
			name: "invalid next-hop",
			route: &StaticRoute{
				Prefix:  "10.0.0.0/8",
				NextHop: "999.999.999.999",
			},
			wantErr: true,
		},
		{
			name: "distance out of range (negative)",
			route: &StaticRoute{
				Prefix:   "10.0.0.0/8",
				NextHop:  "10.0.1.254",
				Distance: -1,
			},
			wantErr: true,
		},
		{
			name: "distance out of range (too large)",
			route: &StaticRoute{
				Prefix:   "10.0.0.0/8",
				NextHop:  "10.0.1.254",
				Distance: 256,
			},
			wantErr: true,
		},
		{
			name: "max valid distance",
			route: &StaticRoute{
				Prefix:   "10.0.0.0/8",
				NextHop:  "10.0.1.254",
				Distance: 255,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateStaticRoute(tt.route)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateStaticRoute() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
