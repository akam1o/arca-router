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

func TestConfigToXMLMarshalsAsSingleDataReply(t *testing.T) {
	cfg := &config.Config{
		System:     &config.SystemConfig{HostName: "router1"},
		Interfaces: map[string]*config.Interface{},
	}

	xmlData, err := ConfigToXML(cfg, nil)
	if err != nil {
		t.Fatalf("ConfigToXML() error = %v", err)
	}
	xmlStr := string(xmlData)
	if strings.Contains(xmlStr, "<?xml") || strings.Contains(xmlStr, "<data") {
		t.Fatalf("ConfigToXML() = %q, want data child XML only", xmlStr)
	}

	replyXML, err := MarshalReply(NewDataReply("102", xmlData))
	if err != nil {
		t.Fatalf("MarshalReply() error = %v", err)
	}
	assertSingleDataElement(t, replyXML)
	if !strings.Contains(string(replyXML), "<host-name>router1</host-name>") {
		t.Fatalf("MarshalReply() missing config content:\n%s", replyXML)
	}
}

func TestConfigToXMLUsesStableMapOrdering(t *testing.T) {
	cfg := &config.Config{
		Chassis: &config.ChassisConfig{
			Cluster: &config.ClusterConfig{
				Nodes: map[string]*config.ClusterNode{
					"node-b": {Name: "node-b", Address: "192.0.2.12"},
					"node-a": {Name: "node-a", Address: "192.0.2.11"},
				},
			},
		},
		Interfaces: map[string]*config.Interface{
			"ge-0/0/1": {
				Units: map[int]*config.Unit{
					10: {Family: map[string]*config.Family{"inet6": {Addresses: []string{"2001:db8::1/64"}}}},
					0:  {Family: map[string]*config.Family{"inet": {Addresses: []string{"192.0.2.1/24"}}}},
				},
			},
			"ge-0/0/0": {Units: map[int]*config.Unit{}},
		},
		RoutingInstances: map[string]*config.RoutingInstance{
			"RED":  {Name: "RED", InstanceType: "vrf"},
			"BLUE": {Name: "BLUE", InstanceType: "vrf"},
		},
		Protocols: &config.ProtocolConfig{
			BGP: &config.BGPConfig{Groups: map[string]*config.BGPGroup{
				"Z": {Type: "external", Neighbors: map[string]*config.BGPNeighbor{
					"203.0.113.2": {IP: "203.0.113.2", PeerAS: 65002},
					"203.0.113.1": {IP: "203.0.113.1", PeerAS: 65001},
				}},
				"A": {Type: "internal"},
			}},
			OSPF: &config.OSPFConfig{Areas: map[string]*config.OSPFArea{
				"1.1.1.1": {AreaID: "1.1.1.1", Interfaces: map[string]*config.OSPFInterface{
					"ge-0/0/1": {Name: "ge-0/0/1"},
					"ge-0/0/0": {Name: "ge-0/0/0"},
				}},
				"0.0.0.0": {AreaID: "0.0.0.0"},
			}},
			VRRP: &config.VRRPConfig{Groups: map[string]*config.VRRPGroup{
				"20": {Name: "20", Interface: "ge-0/0/1"},
				"10": {Name: "10", Interface: "ge-0/0/0"},
			}},
		},
		ClassOfService: &config.ClassOfServiceConfig{
			ForwardingClasses: map[string]*config.ForwardingClass{
				"ef": {Name: "ef", Queue: 5},
				"af": {Name: "af", Queue: 1},
			},
			TrafficControlProfiles: map[string]*config.TrafficControlProfile{
				"WAN-Z": {Name: "WAN-Z", ShapingRate: 2000},
				"WAN-A": {Name: "WAN-A", ShapingRate: 1000},
			},
			Interfaces: map[string]*config.CoSInterface{
				"ge-0/0/1": {Name: "ge-0/0/1", OutputTrafficControlProfile: "WAN-Z"},
				"ge-0/0/0": {Name: "ge-0/0/0", OutputTrafficControlProfile: "WAN-A"},
			},
		},
	}

	first, err := ConfigToXML(cfg, nil)
	if err != nil {
		t.Fatalf("ConfigToXML() error = %v", err)
	}
	second, err := ConfigToXML(cfg, nil)
	if err != nil {
		t.Fatalf("ConfigToXML() second call error = %v", err)
	}
	if string(first) != string(second) {
		t.Fatalf("ConfigToXML() output changed between calls\nfirst:\n%s\nsecond:\n%s", first, second)
	}

	xmlStr := string(first)
	assertXMLOrder(t, xmlStr, "<name>node-a</name>", "<name>node-b</name>")
	assertXMLOrder(t, xmlStr, "<name>ge-0/0/0</name>", "<name>ge-0/0/1</name>")
	assertXMLOrder(t, xmlStr, "<name>0</name>", "<name>10</name>")
	assertXMLOrder(t, xmlStr, "<name>inet</name>", "<name>inet6</name>")
	assertXMLOrder(t, xmlStr, "<name>BLUE</name>", "<name>RED</name>")
	assertXMLOrder(t, xmlStr, "<name>A</name>", "<name>Z</name>")
	assertXMLOrder(t, xmlStr, "<ip>203.0.113.1</ip>", "<ip>203.0.113.2</ip>")
	assertXMLOrder(t, xmlStr, "<name>0.0.0.0</name>", "<name>1.1.1.1</name>")
	assertXMLOrder(t, xmlStr, "<interface>ge-0/0/0</interface>", "<interface>ge-0/0/1</interface>")
	assertXMLOrder(t, xmlStr, "<name>af</name>", "<name>ef</name>")
	assertXMLOrder(t, xmlStr, "<name>WAN-A</name>", "<name>WAN-Z</name>")
	assertXMLOrder(t, xmlStr, "<output-traffic-control-profile>WAN-A</output-traffic-control-profile>", "<output-traffic-control-profile>WAN-Z</output-traffic-control-profile>")
}

func TestV06AdvancedConfigXMLRoundTrip(t *testing.T) {
	cfg := &config.Config{
		System: &config.SystemConfig{
			HostName: "edge-01",
			Services: &config.SystemServicesConfig{
				WebUI:      &config.WebUIConfig{Enabled: true, ListenAddress: "127.0.0.1", Port: 8443},
				Prometheus: &config.PrometheusConfig{Enabled: true, ListenAddress: "127.0.0.1", Port: 9090},
				SNMP:       &config.SNMPConfig{Enabled: true, ListenAddress: "127.0.0.1", Port: 1161, Community: "public"},
			},
		},
		Chassis: &config.ChassisConfig{
			Cluster: &config.ClusterConfig{
				Enabled: true,
				Nodes: map[string]*config.ClusterNode{
					"node0": {Name: "node0", Address: "192.0.2.10", Priority: 120},
				},
				Sync: &config.ClusterSyncConfig{
					Etcd: &config.EtcdSyncConfig{Endpoints: []string{"http://127.0.0.1:2379"}},
				},
			},
		},
		Interfaces: map[string]*config.Interface{},
		Protocols: &config.ProtocolConfig{
			MPLS: &config.MPLSConfig{Interfaces: []string{"ge-0/0/0"}},
			VRRP: &config.VRRPConfig{Groups: map[string]*config.VRRPGroup{
				"10": {
					Name:           "10",
					Interface:      "ge-0/0/0",
					VirtualAddress: "192.0.2.254",
					Priority:       110,
					Preempt:        true,
				},
			}},
		},
		RoutingInstances: map[string]*config.RoutingInstance{
			"BLUE": {
				Name:               "BLUE",
				InstanceType:       "vrf",
				RouteDistinguisher: "65000:100",
				VRFTarget:          "target:65000:100",
				VRFTargetImport:    []string{"target:65000:101"},
				VRFTargetExport:    []string{"target:65000:102"},
				VRFImport:          []string{"BLUE-IN"},
				VRFExport:          []string{"BLUE-OUT"},
				Interfaces:         []string{"ge-0/0/0"},
			},
		},
		ClassOfService: &config.ClassOfServiceConfig{
			ForwardingClasses: map[string]*config.ForwardingClass{
				"expedited-forwarding": {Name: "expedited-forwarding", Queue: 5},
			},
			TrafficControlProfiles: map[string]*config.TrafficControlProfile{
				"WAN": {Name: "WAN", ShapingRate: 1000000000, SchedulerMap: "WAN-SCHED"},
			},
			Interfaces: map[string]*config.CoSInterface{
				"ge-0/0/0": {Name: "ge-0/0/0", OutputTrafficControlProfile: "WAN"},
			},
		},
		Security: &config.SecurityConfig{
			NETCONF:   &config.NETCONFConfig{SSH: &config.NETCONFSSHConfig{Port: 1830}},
			RateLimit: &config.RateLimitConfig{PerIP: 20, PerUser: 50},
			Users: map[string]*config.UserConfig{
				"admin": {Username: "admin", Password: "$2a$12$secret", Role: "admin"},
			},
		},
	}

	xmlData, err := ConfigToXML(cfg, nil)
	if err != nil {
		t.Fatalf("ConfigToXML() error = %v", err)
	}
	xmlStr := string(xmlData)
	for _, want := range []string{
		"<web-ui>",
		"<chassis",
		"<routing-instances",
		"<mpls>",
		"<vrrp>",
		"<class-of-service",
		"<security",
		"<port>1830</port>",
	} {
		if !strings.Contains(xmlStr, want) {
			t.Fatalf("ConfigToXML() missing %q:\n%s", want, xmlStr)
		}
	}
	if strings.Contains(xmlStr, "secret") || strings.Contains(xmlStr, "<users>") {
		t.Fatalf("ConfigToXML() leaked user security data:\n%s", xmlStr)
	}

	parsed, err := XMLToConfig([]byte("<config>"+xmlStr+"</config>"), DefaultOpMerge)
	if err != nil {
		t.Fatalf("XMLToConfig() error = %v\nXML:\n%s", err, xmlStr)
	}
	setCommands := config.ToSetCommands(parsed)
	for _, want := range []string{
		"set system services web-ui port 8443",
		"set system services prometheus port 9090",
		"set system services snmp community public",
		"set security netconf ssh port 1830",
		"set security rate-limit per-user 50",
		"set chassis cluster node node0 priority 120",
		"set protocols mpls interface ge-0/0/0",
		"set protocols vrrp group 10 virtual-address 192.0.2.254",
		"set routing-instances BLUE vrf-target import target:65000:101",
		"set class-of-service traffic-control-profile WAN shaping-rate 1000000000",
	} {
		if !strings.Contains(setCommands, want) {
			t.Fatalf("ToSetCommands() missing %q:\n%s", want, setCommands)
		}
	}
}

func TestApplyConfigEditMergesV06AdvancedConfig(t *testing.T) {
	existing := config.NewConfig()
	existing.Protocols = &config.ProtocolConfig{
		MPLS: &config.MPLSConfig{Interfaces: []string{"ge-0/0/0"}},
	}

	edit, err := XMLToConfig([]byte(`
<config>
  <protocols>
    <mpls>
      <interface>ge-0/0/0</interface>
      <interface>ge-0/0/1</interface>
    </mpls>
    <vrrp>
      <group>
        <name>10</name>
        <interface>ge-0/0/1</interface>
      </group>
    </vrrp>
  </protocols>
  <class-of-service>
    <traffic-control-profiles>
      <traffic-control-profile>
        <name>WAN</name>
        <shaping-rate>1000000000</shaping-rate>
      </traffic-control-profile>
    </traffic-control-profiles>
  </class-of-service>
</config>`), DefaultOpMerge)
	if err != nil {
		t.Fatalf("XMLToConfig() error = %v", err)
	}

	merged, err := ApplyConfigEdit(existing, edit, DefaultOpMerge)
	if err != nil {
		t.Fatalf("ApplyConfigEdit() error = %v", err)
	}
	if got := merged.Protocols.MPLS.Interfaces; len(got) != 2 || got[0] != "ge-0/0/0" || got[1] != "ge-0/0/1" {
		t.Fatalf("merged MPLS interfaces = %#v, want deduplicated merge", got)
	}
	if merged.Protocols.VRRP.Groups["10"].Interface != "ge-0/0/1" {
		t.Fatalf("merged VRRP group = %#v", merged.Protocols.VRRP.Groups["10"])
	}
	if merged.ClassOfService.TrafficControlProfiles["WAN"].ShapingRate != 1000000000 {
		t.Fatalf("merged CoS = %#v", merged.ClassOfService)
	}
}

func assertXMLOrder(t *testing.T, xmlStr, first, second string) {
	t.Helper()
	firstIndex := strings.Index(xmlStr, first)
	if firstIndex == -1 {
		t.Fatalf("XML output missing %q:\n%s", first, xmlStr)
	}
	secondIndex := strings.Index(xmlStr, second)
	if secondIndex == -1 {
		t.Fatalf("XML output missing %q:\n%s", second, xmlStr)
	}
	if firstIndex > secondIndex {
		t.Fatalf("XML output orders %q after %q:\n%s", first, second, xmlStr)
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
	xmlData := []byte(`<config><unknown><name>alice</name></unknown></config>`)

	_, err := XMLToConfig(xmlData, DefaultOpMerge)
	if err == nil {
		t.Fatal("XMLToConfig() error = nil, want unsupported element")
	}
	rpcErr, ok := err.(*RPCError)
	if !ok {
		t.Fatalf("XMLToConfig() error = %T, want *RPCError", err)
	}
	if rpcErr.ErrorTag != ErrorTagInvalidValue || rpcErr.ErrorInfo == nil || rpcErr.ErrorInfo.BadElement != "unknown" {
		t.Fatalf("XMLToConfig() error = %#v, want invalid-value for unknown", rpcErr)
	}
}

func TestXMLToConfigRejectsTextOnlyFragment(t *testing.T) {
	xmlData := []byte(`junk`)

	_, err := XMLToConfig(xmlData, DefaultOpMerge)
	if err == nil {
		t.Fatal("XMLToConfig() error = nil, want malformed-message")
	}
	rpcErr, ok := err.(*RPCError)
	if !ok {
		t.Fatalf("XMLToConfig() error = %T, want *RPCError", err)
	}
	if rpcErr.ErrorType != ErrorTypeRPC || rpcErr.ErrorTag != ErrorTagMalformedMessage {
		t.Fatalf("XMLToConfig() error = %#v, want rpc/malformed-message", rpcErr)
	}
}

func TestXMLToConfigRejectsUnexpectedConfigRootText(t *testing.T) {
	xmlData := []byte(`<config>junk<system><host-name>router1</host-name></system></config>`)

	_, err := XMLToConfig(xmlData, DefaultOpMerge)
	if err == nil {
		t.Fatal("XMLToConfig() error = nil, want malformed-message")
	}
	rpcErr, ok := err.(*RPCError)
	if !ok {
		t.Fatalf("XMLToConfig() error = %T, want *RPCError", err)
	}
	if rpcErr.ErrorType != ErrorTypeRPC || rpcErr.ErrorTag != ErrorTagMalformedMessage {
		t.Fatalf("XMLToConfig() error = %#v, want rpc/malformed-message", rpcErr)
	}
}

func TestXMLToConfigRejectsUnexpectedContainerText(t *testing.T) {
	xmlData := []byte(`<config><system>junk<host-name>router1</host-name></system></config>`)

	_, err := XMLToConfig(xmlData, DefaultOpMerge)
	if err == nil {
		t.Fatal("XMLToConfig() error = nil, want malformed-message")
	}
	rpcErr, ok := err.(*RPCError)
	if !ok {
		t.Fatalf("XMLToConfig() error = %T, want *RPCError", err)
	}
	if rpcErr.ErrorType != ErrorTypeRPC || rpcErr.ErrorTag != ErrorTagMalformedMessage {
		t.Fatalf("XMLToConfig() error = %#v, want rpc/malformed-message", rpcErr)
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
	if rpcErr.ErrorTag != ErrorTagUnknownNamespace || rpcErr.ErrorInfo == nil || rpcErr.ErrorInfo.BadNamespace != "urn:example:unknown" {
		t.Fatalf("XMLToConfig() error = %#v, want unknown namespace error", rpcErr)
	}
}

func TestXMLToConfigRejectsUnsupportedOperationAttribute(t *testing.T) {
	xmlData := []byte(`<config xmlns:nc="urn:ietf:params:xml:ns:netconf:base:1.0"><system nc:operation="replace"><host-name>router1</host-name></system></config>`)

	_, err := XMLToConfig(xmlData, DefaultOpMerge)
	if err == nil {
		t.Fatal("XMLToConfig() error = nil, want unsupported operation attribute")
	}
	rpcErr, ok := err.(*RPCError)
	if !ok {
		t.Fatalf("XMLToConfig() error = %T, want *RPCError", err)
	}
	if rpcErr.ErrorTag != ErrorTagOperationNotSupported || rpcErr.ErrorInfo == nil || rpcErr.ErrorInfo.BadAttribute != "operation" {
		t.Fatalf("XMLToConfig() error = %#v, want operation-not-supported for operation attribute", rpcErr)
	}
}

func TestXMLToConfigRejectsUnknownAttribute(t *testing.T) {
	xmlData := []byte(`<config><system foo="bar"><host-name>router1</host-name></system></config>`)

	_, err := XMLToConfig(xmlData, DefaultOpMerge)
	if err == nil {
		t.Fatal("XMLToConfig() error = nil, want unknown attribute")
	}
	rpcErr, ok := err.(*RPCError)
	if !ok {
		t.Fatalf("XMLToConfig() error = %T, want *RPCError", err)
	}
	if rpcErr.ErrorTag != ErrorTagUnknownAttribute || rpcErr.ErrorInfo == nil || rpcErr.ErrorInfo.BadAttribute != "foo" {
		t.Fatalf("XMLToConfig() error = %#v, want unknown-attribute for foo", rpcErr)
	}
}

func TestXMLToConfigRejectsUnknownAttributeNamespace(t *testing.T) {
	xmlData := []byte(`<config><system xmlns:x="urn:example:unknown" x:operation="delete"><host-name>router1</host-name></system></config>`)

	_, err := XMLToConfig(xmlData, DefaultOpMerge)
	if err == nil {
		t.Fatal("XMLToConfig() error = nil, want unknown attribute namespace")
	}
	rpcErr, ok := err.(*RPCError)
	if !ok {
		t.Fatalf("XMLToConfig() error = %T, want *RPCError", err)
	}
	if rpcErr.ErrorInfo == nil || rpcErr.ErrorInfo.BadNamespace != "urn:example:unknown" {
		t.Fatalf("XMLToConfig() error = %#v, want bad namespace urn:example:unknown", rpcErr)
	}
}

func TestEditConfigRejectsUnknownConfigRootNamespace(t *testing.T) {
	rpcXML := []byte(`<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
		<edit-config>
			<target><candidate/></target>
			<config xmlns="urn:example:unknown">
				<system><host-name>router1</host-name></system>
			</config>
		</edit-config>
	</rpc>`)

	rpc, err := ParseRPC(rpcXML)
	if err != nil {
		t.Fatalf("ParseRPC() error = %v", err)
	}
	var req EditConfigRequest
	if err := rpc.UnmarshalOperation(&req); err != nil {
		t.Fatalf("UnmarshalOperation() error = %v", err)
	}
	configXML, err := req.Config.XML()
	if err != nil {
		t.Fatalf("Config.XML() error = %v", err)
	}

	_, err = XMLToConfig(configXML, DefaultOpMerge)
	if err == nil {
		t.Fatal("XMLToConfig() error = nil, want unknown config root namespace")
	}
	rpcErr, ok := err.(*RPCError)
	if !ok {
		t.Fatalf("XMLToConfig() error = %T, want *RPCError", err)
	}
	if rpcErr.ErrorInfo == nil || rpcErr.ErrorInfo.BadNamespace != "urn:example:unknown" {
		t.Fatalf("XMLToConfig() error = %#v, want bad namespace urn:example:unknown", rpcErr)
	}
}

func TestXMLToConfigRejectsTooManyRawElements(t *testing.T) {
	var b strings.Builder
	b.WriteString("<config><system>")
	for i := 0; i < MaxXMLElements; i++ {
		b.WriteString("<host-name>router1</host-name>")
	}
	b.WriteString("</system></config>")

	_, err := XMLToConfig([]byte(b.String()), DefaultOpMerge)
	if err == nil {
		t.Fatal("XMLToConfig() error = nil, want raw element limit error")
	}
	rpcErr, ok := err.(*RPCError)
	if !ok {
		t.Fatalf("XMLToConfig() error = %T, want *RPCError", err)
	}
	if rpcErr.ErrorTag != ErrorTagInvalidValue || rpcErr.ErrorAppTag != "size-limit" {
		t.Fatalf("XMLToConfig() error = %#v, want invalid-value size-limit", rpcErr)
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
