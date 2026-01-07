package config

import (
	"strings"
	"testing"
)

// TestParsePrefixList tests parsing prefix-list configurations
func TestParsePrefixList(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		wantErr       bool
		expectedList  string
		expectedCount int
	}{
		{
			name:          "single prefix",
			input:         "set policy-options prefix-list MYLIST 10.0.0.0/8\n",
			wantErr:       false,
			expectedList:  "MYLIST",
			expectedCount: 1,
		},
		{
			name: "multiple prefixes same list",
			input: `set policy-options prefix-list MYLIST 10.0.0.0/8
set policy-options prefix-list MYLIST 192.168.0.0/16
`,
			wantErr:       false,
			expectedList:  "MYLIST",
			expectedCount: 2,
		},
		{
			name: "multiple prefix lists",
			input: `set policy-options prefix-list LIST1 10.0.0.0/8
set policy-options prefix-list LIST2 192.168.0.0/16
`,
			wantErr:       false,
			expectedList:  "LIST1",
			expectedCount: 1,
		},
		{
			name:          "IPv6 prefix",
			input:         "set policy-options prefix-list IPV6LIST 2001:db8::/32\n",
			wantErr:       false,
			expectedList:  "IPV6LIST",
			expectedCount: 1,
		},
		{
			name:    "missing prefix",
			input:   "set policy-options prefix-list MYLIST\n",
			wantErr: true,
		},
		{
			name:    "missing list name",
			input:   "set policy-options prefix-list\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewParser(strings.NewReader(tt.input))
			config, err := parser.Parse()

			if (err != nil) != tt.wantErr {
				t.Errorf("Parse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			if config.PolicyOptions == nil {
				t.Fatal("PolicyOptions is nil")
			}

			if config.PolicyOptions.PrefixLists == nil {
				t.Fatal("PrefixLists is nil")
			}

			list, ok := config.PolicyOptions.PrefixLists[tt.expectedList]
			if !ok {
				t.Fatalf("Prefix list %s not found", tt.expectedList)
			}

			if len(list.Prefixes) != tt.expectedCount {
				t.Errorf("Expected %d prefixes, got %d", tt.expectedCount, len(list.Prefixes))
			}
		})
	}
}

// TestParsePolicyStatementAccept tests policy-statement with accept action
func TestParsePolicyStatementAccept(t *testing.T) {
	input := `set policy-options policy-statement MYPOLICY term TERM1 from prefix-list MYLIST
set policy-options policy-statement MYPOLICY term TERM1 then accept
`
	parser := NewParser(strings.NewReader(input))
	config, err := parser.Parse()
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if config.PolicyOptions == nil {
		t.Fatal("PolicyOptions is nil")
	}

	policy, ok := config.PolicyOptions.PolicyStatements["MYPOLICY"]
	if !ok {
		t.Fatal("Policy statement MYPOLICY not found")
	}

	if len(policy.Terms) != 1 {
		t.Fatalf("Expected 1 term, got %d", len(policy.Terms))
	}

	term := policy.Terms[0]
	if term.Name != "TERM1" {
		t.Errorf("Expected term name TERM1, got %s", term.Name)
	}

	if term.From == nil {
		t.Fatal("Term From is nil")
	}

	if len(term.From.PrefixLists) != 1 {
		t.Fatalf("Expected 1 prefix-list, got %d", len(term.From.PrefixLists))
	}

	if term.From.PrefixLists[0] != "MYLIST" {
		t.Errorf("Expected prefix-list MYLIST, got %s", term.From.PrefixLists[0])
	}

	if term.Then == nil {
		t.Fatal("Term Then is nil")
	}

	if term.Then.Accept == nil {
		t.Fatal("Term Then.Accept is nil")
	}

	if !*term.Then.Accept {
		t.Error("Expected accept=true")
	}
}

// TestParsePolicyStatementReject tests policy-statement with reject action
func TestParsePolicyStatementReject(t *testing.T) {
	input := `set policy-options policy-statement MYPOLICY term TERM1 from prefix-list MYLIST
set policy-options policy-statement MYPOLICY term TERM1 then reject
`
	parser := NewParser(strings.NewReader(input))
	config, err := parser.Parse()
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	policy := config.PolicyOptions.PolicyStatements["MYPOLICY"]
	term := policy.Terms[0]

	if term.Then.Accept == nil {
		t.Fatal("Term Then.Accept is nil")
	}

	if *term.Then.Accept {
		t.Error("Expected accept=false (reject)")
	}
}

// TestParsePolicyStatementLocalPreference tests local-preference action
func TestParsePolicyStatementLocalPreference(t *testing.T) {
	input := `set policy-options policy-statement MYPOLICY term TERM1 from protocol bgp
set policy-options policy-statement MYPOLICY term TERM1 then local-preference 200
`
	parser := NewParser(strings.NewReader(input))
	config, err := parser.Parse()
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	policy := config.PolicyOptions.PolicyStatements["MYPOLICY"]
	term := policy.Terms[0]

	if term.From.Protocol != "bgp" {
		t.Errorf("Expected protocol bgp, got %s", term.From.Protocol)
	}

	if term.Then.LocalPreference == nil {
		t.Fatal("Term Then.LocalPreference is nil")
	}

	if *term.Then.LocalPreference != 200 {
		t.Errorf("Expected local-preference 200, got %d", *term.Then.LocalPreference)
	}
}

// TestParsePolicyStatementCommunity tests community action
func TestParsePolicyStatementCommunity(t *testing.T) {
	input := `set policy-options policy-statement MYPOLICY term TERM1 from neighbor 10.0.0.1
set policy-options policy-statement MYPOLICY term TERM1 then community 65000:100
`
	parser := NewParser(strings.NewReader(input))
	config, err := parser.Parse()
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	policy := config.PolicyOptions.PolicyStatements["MYPOLICY"]
	term := policy.Terms[0]

	if term.From.Neighbor != "10.0.0.1" {
		t.Errorf("Expected neighbor 10.0.0.1, got %s", term.From.Neighbor)
	}

	if term.Then.Community != "65000:100" {
		t.Errorf("Expected community 65000:100, got %s", term.Then.Community)
	}
}

// TestParsePolicyStatementASPath tests as-path match condition
func TestParsePolicyStatementASPath(t *testing.T) {
	input := `set policy-options policy-statement MYPOLICY term TERM1 from as-path ".*65001.*"
set policy-options policy-statement MYPOLICY term TERM1 then accept
`
	parser := NewParser(strings.NewReader(input))
	config, err := parser.Parse()
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	policy := config.PolicyOptions.PolicyStatements["MYPOLICY"]
	term := policy.Terms[0]

	if term.From.ASPath != ".*65001.*" {
		t.Errorf("Expected as-path .*65001.*, got %s", term.From.ASPath)
	}
}

// TestParsePolicyStatementMultipleTerms tests policy with multiple terms
func TestParsePolicyStatementMultipleTerms(t *testing.T) {
	input := `set policy-options policy-statement MYPOLICY term TERM1 from prefix-list LIST1
set policy-options policy-statement MYPOLICY term TERM1 then accept
set policy-options policy-statement MYPOLICY term TERM2 from prefix-list LIST2
set policy-options policy-statement MYPOLICY term TERM2 then reject
`
	parser := NewParser(strings.NewReader(input))
	config, err := parser.Parse()
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	policy := config.PolicyOptions.PolicyStatements["MYPOLICY"]
	if len(policy.Terms) != 2 {
		t.Fatalf("Expected 2 terms, got %d", len(policy.Terms))
	}

	// Check term 1
	if policy.Terms[0].Name != "TERM1" {
		t.Errorf("Expected term 1 name TERM1, got %s", policy.Terms[0].Name)
	}
	if !*policy.Terms[0].Then.Accept {
		t.Error("Expected term 1 accept=true")
	}

	// Check term 2
	if policy.Terms[1].Name != "TERM2" {
		t.Errorf("Expected term 2 name TERM2, got %s", policy.Terms[1].Name)
	}
	if *policy.Terms[1].Then.Accept {
		t.Error("Expected term 2 accept=false (reject)")
	}
}

// TestParsePolicyStatementErrors tests error handling
func TestParsePolicyStatementErrors(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "missing term keyword",
			input:   "set policy-options policy-statement MYPOLICY TERM1 from prefix-list MYLIST\n",
			wantErr: true,
		},
		{
			name:    "missing term name",
			input:   "set policy-options policy-statement MYPOLICY term\n",
			wantErr: true,
		},
		{
			name:    "missing from/then keyword",
			input:   "set policy-options policy-statement MYPOLICY term TERM1\n",
			wantErr: true,
		},
		{
			name:    "invalid action",
			input:   "set policy-options policy-statement MYPOLICY term TERM1 then invalid\n",
			wantErr: true,
		},
		{
			name:    "invalid match condition",
			input:   "set policy-options policy-statement MYPOLICY term TERM1 from invalid value\n",
			wantErr: true,
		},
		{
			name:    "missing local-preference value",
			input:   "set policy-options policy-statement MYPOLICY term TERM1 then local-preference\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewParser(strings.NewReader(tt.input))
			_, err := parser.Parse()

			if (err != nil) != tt.wantErr {
				t.Errorf("Parse() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestParsePolicyOptionsIntegration tests full policy-options configuration
func TestParsePolicyOptionsIntegration(t *testing.T) {
	input := `set policy-options prefix-list PRIVATE 10.0.0.0/8
set policy-options prefix-list PRIVATE 172.16.0.0/12
set policy-options prefix-list PRIVATE 192.168.0.0/16
set policy-options policy-statement DENY-PRIVATE term DENY from prefix-list PRIVATE
set policy-options policy-statement DENY-PRIVATE term DENY then reject
set policy-options policy-statement DENY-PRIVATE term ALLOW then accept
`
	parser := NewParser(strings.NewReader(input))
	config, err := parser.Parse()
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Check prefix-list
	if config.PolicyOptions == nil {
		t.Fatal("PolicyOptions is nil")
	}

	prefixList := config.PolicyOptions.PrefixLists["PRIVATE"]
	if prefixList == nil {
		t.Fatal("Prefix list PRIVATE not found")
	}

	if len(prefixList.Prefixes) != 3 {
		t.Errorf("Expected 3 prefixes in PRIVATE, got %d", len(prefixList.Prefixes))
	}

	// Check policy-statement
	policy := config.PolicyOptions.PolicyStatements["DENY-PRIVATE"]
	if policy == nil {
		t.Fatal("Policy statement DENY-PRIVATE not found")
	}

	if len(policy.Terms) != 2 {
		t.Fatalf("Expected 2 terms, got %d", len(policy.Terms))
	}

	// Check DENY term
	denyTerm := policy.Terms[0]
	if denyTerm.Name != "DENY" {
		t.Errorf("Expected term name DENY, got %s", denyTerm.Name)
	}
	if len(denyTerm.From.PrefixLists) != 1 || denyTerm.From.PrefixLists[0] != "PRIVATE" {
		t.Error("Expected DENY term to match PRIVATE prefix-list")
	}
	if denyTerm.Then.Accept == nil || *denyTerm.Then.Accept {
		t.Error("Expected DENY term to reject")
	}

	// Check ALLOW term
	allowTerm := policy.Terms[1]
	if allowTerm.Name != "ALLOW" {
		t.Errorf("Expected term name ALLOW, got %s", allowTerm.Name)
	}
	if allowTerm.Then.Accept == nil || !*allowTerm.Then.Accept {
		t.Error("Expected ALLOW term to accept")
	}
}

// TestParseMixedConfiguration tests policy-options mixed with other config
func TestParseMixedConfiguration(t *testing.T) {
	input := `set system host-name router1
set policy-options prefix-list MYLIST 10.0.0.0/8
set interfaces ge-0/0/0 unit 0 family inet address 192.168.1.1/24
set policy-options policy-statement MYPOLICY term TERM1 from prefix-list MYLIST
set policy-options policy-statement MYPOLICY term TERM1 then accept
`
	parser := NewParser(strings.NewReader(input))
	config, err := parser.Parse()
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Check system config
	if config.System == nil || config.System.HostName != "router1" {
		t.Error("System config not parsed correctly")
	}

	// Check interface config
	if config.Interfaces == nil || config.Interfaces["ge-0/0/0"] == nil {
		t.Error("Interface config not parsed correctly")
	}

	// Check policy-options config
	if config.PolicyOptions == nil {
		t.Fatal("PolicyOptions is nil")
	}

	if config.PolicyOptions.PrefixLists["MYLIST"] == nil {
		t.Error("Prefix list MYLIST not found")
	}

	if config.PolicyOptions.PolicyStatements["MYPOLICY"] == nil {
		t.Error("Policy statement MYPOLICY not found")
	}
}
