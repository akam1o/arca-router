package config

import (
	"strings"
	"testing"
)

func FuzzLexer(f *testing.F) {
	for _, seed := range []string{
		"",
		"set system host-name router1\n",
		"set interfaces ge-0/0/0 unit 0 family inet address 192.0.2.1/24\n",
		"set security users user admin password \"quoted secret\"\n",
		"# comment\nset system services web-ui enabled true\n",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		lexer := NewLexer(strings.NewReader(input))
		for i := 0; i < len(input)+1; i++ {
			if tok := lexer.NextToken(); tok.Type == TokenEOF {
				return
			}
		}
		t.Fatalf("lexer did not reach EOF after %d tokens", len(input)+1)
	})
}

func FuzzParserValidate(f *testing.F) {
	for _, seed := range []string{
		"",
		"set system host-name router1\n",
		"set system services prometheus enabled true\nset system services prometheus port 9090\n",
		"set protocols bgp group EBGP type external\nset protocols bgp group EBGP peer-as 65001\n",
		"set policy-options prefix-list PL 192.0.2.0/24\n",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		cfg, err := NewParser(strings.NewReader(input)).Parse()
		if err != nil || cfg == nil {
			return
		}
		_ = cfg.Validate()
		_ = ToSetCommands(cfg)
	})
}
