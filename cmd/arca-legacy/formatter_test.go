package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestFormatTable(t *testing.T) {
	tests := []struct {
		name    string
		headers []string
		rows    [][]string
		want    string
	}{
		{
			name:    "empty table",
			headers: []string{"Col1", "Col2"},
			rows:    [][]string{},
			want:    "Col1  Col2\n----  ----\n",
		},
		{
			name:    "single row",
			headers: []string{"Name", "Age"},
			rows:    [][]string{{"Alice", "30"}},
			want:    "Name   Age\n----   ---\nAlice  30\n",
		},
		{
			name:    "multiple rows",
			headers: []string{"Index", "Name", "State"},
			rows: [][]string{
				{"1", "ge-0/0/0", "up"},
				{"2", "xe-0/1/0", "down"},
			},
			want: "Index  Name      State\n-----  ----      -----\n1      ge-0/0/0  up\n2      xe-0/1/0  down\n",
		},
		{
			name:    "column alignment",
			headers: []string{"Short", "LongerHeader"},
			rows: [][]string{
				{"A", "B"},
				{"VeryLongValue", "C"},
			},
			want: "Short          LongerHeader\n-----          ------------\nA              B\nVeryLongValue  C\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := FormatTable(&buf, tt.headers, tt.rows)
			if err != nil {
				t.Errorf("FormatTable() error = %v", err)
				return
			}
			got := buf.String()
			if got != tt.want {
				t.Errorf("FormatTable() output mismatch:\nGot:\n%s\nWant:\n%s", got, tt.want)
			}
		})
	}
}

func TestFormatSetConfig(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		want  string
	}{
		{
			name:  "empty lines",
			lines: []string{},
			want:  "",
		},
		{
			name:  "single line",
			lines: []string{"set interfaces ge-0/0/0 unit 0 family inet address 192.168.1.1/24"},
			want:  "set interfaces ge-0/0/0 unit 0 family inet address 192.168.1.1/24\n",
		},
		{
			name: "multiple lines",
			lines: []string{
				"set interfaces ge-0/0/0 unit 0 family inet address 192.168.1.1/24",
				"set protocols bgp group ibgp type internal",
			},
			want: "set interfaces ge-0/0/0 unit 0 family inet address 192.168.1.1/24\nset protocols bgp group ibgp type internal\n",
		},
		{
			name: "preserves order",
			lines: []string{
				"set system host-name router1",
				"set interfaces ge-0/0/0 description WAN",
				"set routing-options static route 0.0.0.0/0 next-hop 10.0.0.1",
			},
			want: "set system host-name router1\nset interfaces ge-0/0/0 description WAN\nset routing-options static route 0.0.0.0/0 next-hop 10.0.0.1\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := FormatSetConfig(&buf, tt.lines)
			if err != nil {
				t.Errorf("FormatSetConfig() error = %v", err)
				return
			}
			got := buf.String()
			if got != tt.want {
				t.Errorf("FormatSetConfig() output mismatch:\nGot:\n%s\nWant:\n%s", got, tt.want)
			}
		})
	}
}

func TestFilterConfigLines(t *testing.T) {
	lines := []string{
		"set system host-name router1",
		"set interfaces ge-0/0/0 unit 0 family inet address 192.168.1.1/24",
		"set interfaces xe-0/1/0 description WAN",
		"set protocols bgp group ibgp type internal",
		"set routing-options static route 0.0.0.0/0 next-hop 10.0.0.1",
	}

	tests := []struct {
		name   string
		prefix string
		want   []string
	}{
		{
			name:   "filter interfaces",
			prefix: "set interfaces",
			want: []string{
				"set interfaces ge-0/0/0 unit 0 family inet address 192.168.1.1/24",
				"set interfaces xe-0/1/0 description WAN",
			},
		},
		{
			name:   "filter protocols",
			prefix: "set protocols",
			want: []string{
				"set protocols bgp group ibgp type internal",
			},
		},
		{
			name:   "filter system",
			prefix: "set system",
			want: []string{
				"set system host-name router1",
			},
		},
		{
			name:   "no match",
			prefix: "set policy-options",
			want:   []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilterConfigLines(lines, tt.prefix)
			if len(got) != len(tt.want) {
				t.Errorf("FilterConfigLines() length = %d, want %d", len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("FilterConfigLines()[%d] = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestFilterConfigByPrefixes(t *testing.T) {
	lines := []string{
		"set system host-name router1",
		"set interfaces ge-0/0/0 unit 0 family inet address 192.168.1.1/24",
		"set protocols bgp group ibgp type internal",
		"set routing-options static route 0.0.0.0/0 next-hop 10.0.0.1",
		"set protocols ospf area 0.0.0.0 interface ge-0/0/0",
	}

	tests := []struct {
		name     string
		prefixes []string
		want     []string
	}{
		{
			name:     "single prefix",
			prefixes: []string{"set protocols"},
			want: []string{
				"set protocols bgp group ibgp type internal",
				"set protocols ospf area 0.0.0.0 interface ge-0/0/0",
			},
		},
		{
			name:     "multiple prefixes",
			prefixes: []string{"set protocols", "set routing-options"},
			want: []string{
				"set protocols bgp group ibgp type internal",
				"set routing-options static route 0.0.0.0/0 next-hop 10.0.0.1",
				"set protocols ospf area 0.0.0.0 interface ge-0/0/0",
			},
		},
		{
			name:     "no match",
			prefixes: []string{"set policy-options"},
			want:     []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilterConfigByPrefixes(lines, tt.prefixes)
			if len(got) != len(tt.want) {
				t.Errorf("FilterConfigByPrefixes() length = %d, want %d", len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("FilterConfigByPrefixes()[%d] = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestFilterConfigLines_WithWhitespace(t *testing.T) {
	lines := []string{
		"  set system host-name router1",
		"set interfaces ge-0/0/0 unit 0 family inet address 192.168.1.1/24  ",
		"\tset protocols bgp group ibgp type internal",
	}

	got := FilterConfigLines(lines, "set interfaces")
	want := []string{
		"set interfaces ge-0/0/0 unit 0 family inet address 192.168.1.1/24  ",
	}

	if len(got) != len(want) {
		t.Errorf("FilterConfigLines() with whitespace: length = %d, want %d", len(got), len(want))
		return
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("FilterConfigLines()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestFormatTable_WithSpecialCharacters(t *testing.T) {
	headers := []string{"Name", "Description"}
	rows := [][]string{
		{"ge-0/0/0", "WAN interface (primary)"},
		{"xe-0/1/0", "LAN: 192.168.1.0/24"},
	}

	var buf bytes.Buffer
	err := FormatTable(&buf, headers, rows)
	if err != nil {
		t.Fatalf("FormatTable() error = %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "ge-0/0/0") {
		t.Errorf("FormatTable() missing ge-0/0/0 in output")
	}
	if !strings.Contains(output, "WAN interface (primary)") {
		t.Errorf("FormatTable() missing description in output")
	}
}
