package cli

import (
	"reflect"
	"testing"
)

func TestParseSetCommand(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		basePath []string
		want     string
		wantErr  bool
	}{
		{
			name:     "simple set without base path",
			args:     []string{"system", "host-name", "router1"},
			basePath: []string{},
			want:     "set system host-name router1",
			wantErr:  false,
		},
		{
			name:     "set with base path",
			args:     []string{"unit", "0", "family", "inet"},
			basePath: []string{"interfaces", "ge-0/0/0"},
			want:     "set interfaces ge-0/0/0 unit 0 family inet",
			wantErr:  false,
		},
		{
			name:     "set with quoted string",
			args:     []string{"description", "test interface"},
			basePath: []string{"interfaces", "ge-0/0/0"},
			want:     "set interfaces ge-0/0/0 description \"test interface\"",
			wantErr:  false,
		},
		{
			name:     "empty args",
			args:     []string{},
			basePath: []string{},
			want:     "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSetCommand(tt.args, tt.basePath)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseSetCommand() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ParseSetCommand() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseDeleteCommand(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		basePath []string
		want     string
		wantErr  bool
	}{
		{
			name:     "simple delete",
			args:     []string{"system", "host-name"},
			basePath: []string{},
			want:     "set system host-name",
			wantErr:  false,
		},
		{
			name:     "delete with base path",
			args:     []string{"unit", "0"},
			basePath: []string{"interfaces", "ge-0/0/0"},
			want:     "set interfaces ge-0/0/0 unit 0",
			wantErr:  false,
		},
		{
			name:     "empty args",
			args:     []string{},
			basePath: []string{},
			want:     "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseDeleteCommand(tt.args, tt.basePath)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseDeleteCommand() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ParseDeleteCommand() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNormalizeConfigPath(t *testing.T) {
	tests := []struct {
		name string
		path []string
		want string
	}{
		{
			name: "simple path",
			path: []string{"system", "host-name", "router1"},
			want: "system host-name router1",
		},
		{
			name: "path with spaces",
			path: []string{"description", "test interface"},
			want: "description \"test interface\"",
		},
		{
			name: "empty path",
			path: []string{},
			want: "",
		},
		{
			name: "path with slashes",
			path: []string{"interfaces", "ge-0/0/0", "unit", "0"},
			want: "interfaces ge-0/0/0 unit 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeConfigPath(tt.path); got != tt.want {
				t.Errorf("NormalizeConfigPath() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTokenizeCommand(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		want    []string
		wantErr bool
	}{
		{
			name:    "simple tokens",
			line:    "set system host-name router1",
			want:    []string{"set", "system", "host-name", "router1"},
			wantErr: false,
		},
		{
			name:    "tokens with quoted string",
			line:    `set description "test interface"`,
			want:    []string{"set", "description", "test interface"},
			wantErr: false,
		},
		{
			name:    "empty line",
			line:    "",
			want:    nil,
			wantErr: false,
		},
		{
			name:    "multiple spaces",
			line:    "set  system   host-name",
			want:    []string{"set", "system", "host-name"},
			wantErr: false,
		},
		{
			name:    "unmatched quote",
			line:    `set description "test interface`,
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := TokenizeCommand(tt.line)
			if (err != nil) != tt.wantErr {
				t.Errorf("TokenizeCommand() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("TokenizeCommand() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Extended parser tests for edge cases
func TestParseSetCommandEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		basePath []string
		want     string
		wantErr  bool
	}{
		{
			name:     "set with IPv4 address",
			args:     []string{"address", "192.168.1.1/24"},
			basePath: []string{"interfaces", "ge-0/0/0", "unit", "0", "family", "inet"},
			want:     "set interfaces ge-0/0/0 unit 0 family inet address 192.168.1.1/24",
			wantErr:  false,
		},
		{
			name:     "set with IPv6 address",
			args:     []string{"address", "2001:db8::1/64"},
			basePath: []string{"interfaces", "ge-0/0/0", "unit", "0", "family", "inet6"},
			want:     "set interfaces ge-0/0/0 unit 0 family inet6 address 2001:db8::1/64",
			wantErr:  false,
		},
		{
			name:     "set with BGP AS number",
			args:     []string{"bgp", "as-number", "65001"},
			basePath: []string{"protocols"},
			want:     "set protocols bgp as-number 65001",
			wantErr:  false,
		},
		{
			name:     "set with long path",
			args:     []string{"neighbor", "192.168.1.2", "peer-as", "65002"},
			basePath: []string{"protocols", "bgp", "group", "external"},
			want:     "set protocols bgp group external neighbor 192.168.1.2 peer-as 65002",
			wantErr:  false,
		},
		{
			name:     "set with special characters in description",
			args:     []string{"description", "Test-Interface_01 (Primary)"},
			basePath: []string{"interfaces", "ge-0/0/0"},
			want:     "set interfaces ge-0/0/0 description \"Test-Interface_01 (Primary)\"",
			wantErr:  false,
		},
		{
			name:     "set with numeric-only value",
			args:     []string{"mtu", "9000"},
			basePath: []string{"interfaces", "ge-0/0/0"},
			want:     "set interfaces ge-0/0/0 mtu 9000",
			wantErr:  false,
		},
		{
			name:     "set with hyphenated value",
			args:     []string{"host-name", "router-01"},
			basePath: []string{"system"},
			want:     "set system host-name router-01",
			wantErr:  false,
		},
		{
			name:     "set with very long base path",
			args:     []string{"address", "10.0.0.1/32"},
			basePath: []string{"a", "b", "c", "d", "e", "f", "g", "h"},
			want:     "set a b c d e f g h address 10.0.0.1/32",
			wantErr:  false,
		},
		{
			name:     "set with single arg",
			args:     []string{"enable"},
			basePath: []string{"protocols", "ospf"},
			want:     "set protocols ospf enable",
			wantErr:  false,
		},
		{
			name:     "set with quoted string containing special chars",
			args:     []string{"description", `Server's main interface`},
			basePath: []string{"interfaces", "ge-0/0/0"},
			want:     `set interfaces ge-0/0/0 description "Server's main interface"`,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSetCommand(tt.args, tt.basePath)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseSetCommand() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ParseSetCommand() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseDeleteCommandEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		basePath []string
		want     string
		wantErr  bool
	}{
		{
			name:     "delete entire interface",
			args:     []string{"interfaces", "ge-0/0/0"},
			basePath: []string{},
			want:     "set interfaces ge-0/0/0",
			wantErr:  false,
		},
		{
			name:     "delete BGP neighbor",
			args:     []string{"neighbor", "192.168.1.2"},
			basePath: []string{"protocols", "bgp", "group", "external"},
			want:     "set protocols bgp group external neighbor 192.168.1.2",
			wantErr:  false,
		},
		{
			name:     "delete with long path",
			args:     []string{"unit", "0", "family", "inet", "address"},
			basePath: []string{"interfaces", "ge-0/0/0"},
			want:     "set interfaces ge-0/0/0 unit 0 family inet address",
			wantErr:  false,
		},
		{
			name:     "delete single attribute",
			args:     []string{"description"},
			basePath: []string{"interfaces", "ge-0/0/0"},
			want:     "set interfaces ge-0/0/0 description",
			wantErr:  false,
		},
		{
			name:     "delete with IPv6 address",
			args:     []string{"address", "2001:db8::1/64"},
			basePath: []string{"interfaces", "ge-0/0/0", "unit", "0", "family", "inet6"},
			want:     "set interfaces ge-0/0/0 unit 0 family inet6 address 2001:db8::1/64",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseDeleteCommand(tt.args, tt.basePath)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseDeleteCommand() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ParseDeleteCommand() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTokenizeCommandEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		want    []string
		wantErr bool
	}{
		{
			name:    "command with tabs (split like spaces)",
			line:    "set\tsystem\thost-name\trouter1",
			want:    []string{"set", "system", "host-name", "router1"},
			wantErr: false,
		},
		{
			name:    "quoted string with spaces at start",
			line:    `set description " leading space"`,
			want:    []string{"set", "description", " leading space"},
			wantErr: false,
		},
		{
			name:    "quoted string with spaces at end",
			line:    `set description "trailing space "`,
			want:    []string{"set", "description", "trailing space "},
			wantErr: false,
		},
		{
			name:    "empty quoted string (omitted)",
			line:    `set description ""`,
			want:    []string{"set", "description"},
			wantErr: false,
		},
		{
			name:    "multiple quoted strings",
			line:    `set description "first" "second"`,
			want:    []string{"set", "description", "first", "second"},
			wantErr: false,
		},
		{
			name:    "special chars outside quotes",
			line:    "set interfaces ge-0/0/0 unit 0",
			want:    []string{"set", "interfaces", "ge-0/0/0", "unit", "0"},
			wantErr: false,
		},
		{
			name:    "IPv6 address",
			line:    "set address 2001:db8::1/64",
			want:    []string{"set", "address", "2001:db8::1/64"},
			wantErr: false,
		},
		{
			name:    "unmatched quote at end",
			line:    `set description "test`,
			want:    nil,
			wantErr: true,
		},
		{
			name:    "unmatched quote at start",
			line:    `set "description test`,
			want:    nil,
			wantErr: true,
		},
		{
			name:    "very long line",
			line:    "set " + "a " + "b " + "c " + "d " + "e " + "f " + "g " + "h " + "i " + "j",
			want:    []string{"set", "a", "b", "c", "d", "e", "f", "g", "h", "i", "j"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := TokenizeCommand(tt.line)
			if (err != nil) != tt.wantErr {
				t.Errorf("TokenizeCommand() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("TokenizeCommand() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNormalizeConfigPathEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		path []string
		want string
	}{
		{
			name: "path with IPv4 address",
			path: []string{"interfaces", "ge-0/0/0", "address", "192.168.1.1/24"},
			want: "interfaces ge-0/0/0 address 192.168.1.1/24",
		},
		{
			name: "path with IPv6 address",
			path: []string{"address", "2001:db8::1/64"},
			want: "address 2001:db8::1/64",
		},
		{
			name: "path with numeric values",
			path: []string{"mtu", "9000"},
			want: "mtu 9000",
		},
		{
			name: "path with AS number",
			path: []string{"as-number", "65001"},
			want: "as-number 65001",
		},
		{
			name: "path with multiple quoted strings",
			path: []string{"description", "Primary Interface", "location", "Rack 1"},
			want: `description "Primary Interface" location "Rack 1"`,
		},
		{
			name: "single token",
			path: []string{"enable"},
			want: "enable",
		},
		{
			name: "path with underscore",
			path: []string{"test_interface"},
			want: "test_interface",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeConfigPath(tt.path); got != tt.want {
				t.Errorf("NormalizeConfigPath() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchesPrefix(t *testing.T) {
	tests := []struct {
		name   string
		line   string
		prefix string
		want   bool
	}{
		{
			name:   "exact match",
			line:   "set interfaces ge-0/0/0 unit 0",
			prefix: "set interfaces ge-0/0/0 unit 0",
			want:   true,
		},
		{
			name:   "prefix match",
			line:   "set interfaces ge-0/0/0 unit 0 family inet",
			prefix: "set interfaces ge-0/0/0",
			want:   true,
		},
		{
			name:   "no match",
			line:   "set system host-name router1",
			prefix: "set interfaces",
			want:   false,
		},
		{
			name:   "empty prefix",
			line:   "set system host-name",
			prefix: "",
			want:   true,
		},
		{
			name:   "token boundary check - should not match",
			line:   "set system host-name2",
			prefix: "set system host-name",
			want:   false,
		},
		{
			name:   "token boundary check - should match",
			line:   "set system host-name router1",
			prefix: "set system host-name",
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MatchesPrefix(tt.line, tt.prefix); got != tt.want {
				t.Errorf("MatchesPrefix() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchesPrefixEdgeCases(t *testing.T) {
	tests := []struct {
		name   string
		line   string
		prefix string
		want   bool
	}{
		{
			name:   "prefix with IPv4 address",
			line:   "set interfaces ge-0/0/0 address 192.168.1.1/24",
			prefix: "set interfaces ge-0/0/0 address",
			want:   true,
		},
		{
			name:   "prefix with IPv6 address",
			line:   "set interfaces ge-0/0/0 address 2001:db8::1/64",
			prefix: "set interfaces ge-0/0/0 address",
			want:   true,
		},
		{
			name:   "partial IPv4 match should not match",
			line:   "set interfaces ge-0/0/0 address 192.168.1.10/24",
			prefix: "set interfaces ge-0/0/0 address 192.168.1.1",
			want:   false,
		},
		{
			name:   "long prefix matches",
			line:   "set protocols bgp group external neighbor 192.168.1.2 peer-as 65002",
			prefix: "set protocols bgp group external neighbor 192.168.1.2",
			want:   true,
		},
		{
			name:   "line shorter than prefix",
			line:   "set system",
			prefix: "set system host-name",
			want:   false,
		},
		{
			name:   "exact match with trailing space (matches)",
			line:   "set system host-name ",
			prefix: "set system host-name",
			want:   true,
		},
		{
			name:   "prefix matches beginning of multi-level config",
			line:   "set protocols bgp as-number 65001",
			prefix: "set protocols bgp",
			want:   true,
		},
		{
			name:   "similar but different paths",
			line:   "set interfaces ge-0/0/1 unit 0",
			prefix: "set interfaces ge-0/0/0",
			want:   false,
		},
		{
			name:   "word boundary after colon",
			line:   "set address 2001:db8::1/64",
			prefix: "set address 2001:db8",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MatchesPrefix(tt.line, tt.prefix); got != tt.want {
				t.Errorf("MatchesPrefix(%q, %q) = %v, want %v", tt.line, tt.prefix, got, tt.want)
			}
		})
	}
}
