package netconf

import (
	"strings"
	"testing"

	"github.com/akam1o/arca-router/pkg/config"
)

func TestConfigToXMLWritesExplicitOSPFPriorityZero(t *testing.T) {
	cfg := &config.Config{
		Interfaces: map[string]*config.Interface{},
		Protocols: &config.ProtocolConfig{
			OSPF: &config.OSPFConfig{
				Areas: map[string]*config.OSPFArea{
					"0.0.0.0": {
						AreaID: "0.0.0.0",
						Interfaces: map[string]*config.OSPFInterface{
							"ge-0/0/0": {Name: "ge-0/0/0", Priority: 0, PrioritySet: true},
						},
					},
				},
			},
		},
	}

	xmlData, err := ConfigToXML(cfg, nil)
	if err != nil {
		t.Fatalf("ConfigToXML() error = %v", err)
	}
	if !strings.Contains(string(xmlData), "<priority>0</priority>") {
		t.Fatalf("ConfigToXML() missing explicit priority 0:\n%s", xmlData)
	}
}

func TestXMLToConfigPreservesExplicitOSPFPriorityZero(t *testing.T) {
	xmlData := []byte(`
<config>
  <protocols>
    <ospf>
      <area>
        <name>0.0.0.0</name>
        <area-id>0.0.0.0</area-id>
        <interface>
          <name>ge-0/0/0</name>
          <priority>0</priority>
        </interface>
      </area>
    </ospf>
  </protocols>
</config>`)

	cfg, err := XMLToConfig(xmlData, DefaultOpMerge)
	if err != nil {
		t.Fatalf("XMLToConfig() error = %v", err)
	}

	ospfIface := cfg.Protocols.OSPF.Areas["0.0.0.0"].Interfaces["ge-0/0/0"]
	if !ospfIface.PrioritySet || ospfIface.Priority != 0 {
		t.Fatalf("XMLToConfig() OSPF interface = %#v, want explicit priority 0", ospfIface)
	}

	setCommands := config.ToSetCommands(cfg)
	want := "set protocols ospf area 0.0.0.0 interface ge-0/0/0 priority 0"
	if !strings.Contains(setCommands, want) {
		t.Fatalf("ToSetCommands() = %q, want %q", setCommands, want)
	}
}

func TestXMLToConfigAcceptsConfigFragments(t *testing.T) {
	xmlData := []byte(`<system><host-name>router1</host-name></system>`)

	cfg, err := XMLToConfig(xmlData, DefaultOpMerge)
	if err != nil {
		t.Fatalf("XMLToConfig() error = %v", err)
	}
	if cfg.System == nil || cfg.System.HostName != "router1" {
		t.Fatalf("XMLToConfig() system = %#v, want router1", cfg.System)
	}
}

func TestXMLToConfigRejectsUnknownElement(t *testing.T) {
	xmlData := []byte(`<config><security><user>alice</user></security></config>`)

	_, err := XMLToConfig(xmlData, DefaultOpMerge)
	if err == nil {
		t.Fatal("XMLToConfig() error = nil, want unsupported element")
	}
	rpcErr, ok := err.(*RPCError)
	if !ok {
		t.Fatalf("XMLToConfig() error = %T, want *RPCError", err)
	}
	if rpcErr.ErrorTag != ErrorTagInvalidValue || rpcErr.ErrorInfo == nil || rpcErr.ErrorInfo.BadElement != "security" {
		t.Fatalf("XMLToConfig() error = %#v, want invalid-value for security", rpcErr)
	}
}

func TestXMLToConfigRejectsUnknownNamespace(t *testing.T) {
	xmlData := []byte(`<config><system xmlns="urn:example:unknown"><host-name>router1</host-name></system></config>`)

	_, err := XMLToConfig(xmlData, DefaultOpMerge)
	if err == nil {
		t.Fatal("XMLToConfig() error = nil, want unknown namespace")
	}
	rpcErr, ok := err.(*RPCError)
	if !ok {
		t.Fatalf("XMLToConfig() error = %T, want *RPCError", err)
	}
	if rpcErr.ErrorTag != "unknown-namespace" || rpcErr.ErrorInfo == nil || rpcErr.ErrorInfo.BadNamespace != "urn:example:unknown" {
		t.Fatalf("XMLToConfig() error = %#v, want unknown namespace error", rpcErr)
	}
}

func TestCountConfigElementsIncludesExplicitOSPFPriorityZero(t *testing.T) {
	cfg := &config.Config{
		Interfaces: map[string]*config.Interface{},
		Protocols: &config.ProtocolConfig{
			OSPF: &config.OSPFConfig{
				Areas: map[string]*config.OSPFArea{
					"0.0.0.0": {
						AreaID: "0.0.0.0",
						Interfaces: map[string]*config.OSPFInterface{
							"ge-0/0/0": {Name: "ge-0/0/0", Priority: 0, PrioritySet: true},
						},
					},
				},
			},
		},
	}

	withoutPriority := &config.Config{
		Interfaces: map[string]*config.Interface{},
		Protocols: &config.ProtocolConfig{
			OSPF: &config.OSPFConfig{
				Areas: map[string]*config.OSPFArea{
					"0.0.0.0": {
						AreaID: "0.0.0.0",
						Interfaces: map[string]*config.OSPFInterface{
							"ge-0/0/0": {Name: "ge-0/0/0"},
						},
					},
				},
			},
		},
	}

	got := countConfigElements(cfg)
	want := countConfigElements(withoutPriority) + 1
	if got != want {
		t.Fatalf("countConfigElements() = %d, want %d", got, want)
	}
}
