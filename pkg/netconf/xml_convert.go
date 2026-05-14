package netconf

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/akam1o/arca-router/pkg/config"
)

// XML Namespace constants per Phase 2 plan
const (
	NetconfBaseNS    = "urn:ietf:params:xml:ns:netconf:base:1.0"
	IETFInterfacesNS = "urn:ietf:params:xml:ns:yang:ietf-interfaces"
	IETFRoutingNS    = "urn:ietf:params:xml:ns:yang:ietf-routing"
	ArcaConfigNS     = "urn:arca:router:config:1.0"
	ArcaStateNS      = "urn:arca:router:state:1.0"
)

// XML size and depth limits per Phase 2 plan Section 10.1
const (
	MaxXMLDepth      = 50
	MaxXMLElements   = 10000
	MaxXMLAttributes = 20
	MaxXMLSize       = 10 * 1024 * 1024 // 10MB
)

// ConfigToXML converts internal config to NETCONF <data> content with optional filtering
// This implements Phase 2 Step 3: XML↔Config Conversion
func ConfigToXML(cfg *config.Config, filter *Filter) ([]byte, error) {
	if cfg == nil {
		return []byte{}, nil
	}

	var buf bytes.Buffer

	// System configuration
	if cfg.System != nil && (filter == nil || filterMatches(filter, "system")) {
		if err := writeSystemXML(&buf, cfg.System); err != nil {
			return nil, fmt.Errorf("failed to serialize system config: %w", err)
		}
	}

	// Chassis clustering configuration
	if cfg.Chassis != nil && (filter == nil || filterMatches(filter, "chassis")) {
		if err := writeChassisXML(&buf, cfg.Chassis); err != nil {
			return nil, fmt.Errorf("failed to serialize chassis config: %w", err)
		}
	}

	// Interfaces configuration - use IETF interfaces namespace
	if len(cfg.Interfaces) > 0 && (filter == nil || filterMatches(filter, "interfaces")) {
		if err := writeInterfacesXML(&buf, cfg.Interfaces); err != nil {
			return nil, fmt.Errorf("failed to serialize interfaces: %w", err)
		}
	}

	// Routing options - use IETF routing namespace
	// Note: XML element is "routing" but internal name is "routing-options"
	if cfg.RoutingOptions != nil && (filter == nil || filterMatches(filter, "routing") || filterMatches(filter, "routing-options")) {
		if err := writeRoutingOptionsXML(&buf, cfg.RoutingOptions); err != nil {
			return nil, fmt.Errorf("failed to serialize routing options: %w", err)
		}
	}

	// Routing instances
	if len(cfg.RoutingInstances) > 0 && (filter == nil || filterMatches(filter, "routing-instances")) {
		if err := writeRoutingInstancesXML(&buf, cfg.RoutingInstances); err != nil {
			return nil, fmt.Errorf("failed to serialize routing instances: %w", err)
		}
	}

	// Protocols (BGP, OSPF)
	if cfg.Protocols != nil && (filter == nil || filterMatches(filter, "protocols")) {
		if err := writeProtocolsXML(&buf, cfg.Protocols); err != nil {
			return nil, fmt.Errorf("failed to serialize protocols: %w", err)
		}
	}

	// Class of service
	if cfg.ClassOfService != nil && (filter == nil || filterMatches(filter, "class-of-service")) {
		if err := writeClassOfServiceXML(&buf, cfg.ClassOfService); err != nil {
			return nil, fmt.Errorf("failed to serialize class of service: %w", err)
		}
	}

	// Security configuration; user secrets are intentionally omitted.
	if cfg.Security != nil && (filter == nil || filterMatches(filter, "security")) {
		if err := writeSecurityXML(&buf, cfg.Security); err != nil {
			return nil, fmt.Errorf("failed to serialize security config: %w", err)
		}
	}

	result := buf.Bytes()

	// Validate size
	if len(result) > MaxXMLSize {
		return nil, NewRPCError(ErrorTypeProtocol, ErrorTagInvalidValue,
			fmt.Sprintf("generated XML exceeds size limit (%d bytes)", MaxXMLSize)).
			WithPath("/rpc/get-config").
			WithAppTag("size-limit")
	}

	return result, nil
}

// writeSystemXML writes system configuration to XML
func writeSystemXML(buf *bytes.Buffer, sys *config.SystemConfig) error {
	buf.WriteString(`  <system xmlns="` + ArcaConfigNS + `">`)
	buf.WriteString("\n")

	if sys.HostName != "" {
		buf.WriteString(`    <host-name>`)
		if err := xml.EscapeText(buf, []byte(sys.HostName)); err != nil {
			return err
		}
		buf.WriteString(`</host-name>`)
		buf.WriteString("\n")
	}

	if sys.Services != nil {
		buf.WriteString(`    <services>`)
		buf.WriteString("\n")
		if err := writeSystemServicesXML(buf, sys.Services); err != nil {
			return err
		}
		buf.WriteString(`    </services>`)
		buf.WriteString("\n")
	}

	buf.WriteString(`  </system>`)
	buf.WriteString("\n")
	return nil
}

func writeSystemServicesXML(buf *bytes.Buffer, services *config.SystemServicesConfig) error {
	if services.WebUI != nil {
		if err := writeServiceXML(buf, "web-ui", services.WebUI.Enabled, services.WebUI.ListenAddress, services.WebUI.Port, ""); err != nil {
			return err
		}
	}
	if services.Prometheus != nil {
		if err := writeServiceXML(buf, "prometheus", services.Prometheus.Enabled, services.Prometheus.ListenAddress, services.Prometheus.Port, ""); err != nil {
			return err
		}
	}
	if services.SNMP != nil {
		if err := writeServiceXML(buf, "snmp", services.SNMP.Enabled, services.SNMP.ListenAddress, services.SNMP.Port, services.SNMP.Community); err != nil {
			return err
		}
	}
	return nil
}

func writeServiceXML(buf *bytes.Buffer, name string, enabled bool, listenAddress string, port int, community string) error {
	if !enabled && listenAddress == "" && port == 0 && community == "" {
		return nil
	}
	fmt.Fprintf(buf, "      <%s>\n", name)
	if enabled {
		fmt.Fprintf(buf, "        <enabled>%t</enabled>\n", enabled)
	}
	if listenAddress != "" {
		buf.WriteString(`        <listen-address>`)
		if err := xml.EscapeText(buf, []byte(listenAddress)); err != nil {
			return err
		}
		buf.WriteString(`</listen-address>`)
		buf.WriteString("\n")
	}
	if port != 0 {
		fmt.Fprintf(buf, "        <port>%d</port>\n", port)
	}
	if community != "" {
		buf.WriteString(`        <community>`)
		if err := xml.EscapeText(buf, []byte(community)); err != nil {
			return err
		}
		buf.WriteString(`</community>`)
		buf.WriteString("\n")
	}
	fmt.Fprintf(buf, "      </%s>\n", name)
	return nil
}

func writeChassisXML(buf *bytes.Buffer, chassis *config.ChassisConfig) error {
	if chassis.Cluster == nil {
		return nil
	}
	cluster := chassis.Cluster

	buf.WriteString(`  <chassis xmlns="` + ArcaConfigNS + `">`)
	buf.WriteString("\n")
	buf.WriteString(`    <cluster>`)
	buf.WriteString("\n")
	if cluster.Enabled {
		buf.WriteString(`      <enabled>true</enabled>`)
		buf.WriteString("\n")
	}
	for _, name := range sortedStringKeys(cluster.Nodes) {
		node := cluster.Nodes[name]
		if node == nil {
			continue
		}
		buf.WriteString(`      <node>`)
		buf.WriteString("\n")
		buf.WriteString(`        <name>`)
		if err := xml.EscapeText(buf, []byte(name)); err != nil {
			return err
		}
		buf.WriteString(`</name>`)
		buf.WriteString("\n")
		if node.Address != "" {
			buf.WriteString(`        <address>`)
			if err := xml.EscapeText(buf, []byte(node.Address)); err != nil {
				return err
			}
			buf.WriteString(`</address>`)
			buf.WriteString("\n")
		}
		if node.Priority != 0 {
			fmt.Fprintf(buf, "        <priority>%d</priority>\n", node.Priority)
		}
		buf.WriteString(`      </node>`)
		buf.WriteString("\n")
	}
	if cluster.Sync != nil && cluster.Sync.Etcd != nil && len(cluster.Sync.Etcd.Endpoints) > 0 {
		buf.WriteString(`      <sync>`)
		buf.WriteString("\n")
		buf.WriteString(`        <etcd>`)
		buf.WriteString("\n")
		for _, endpoint := range cluster.Sync.Etcd.Endpoints {
			buf.WriteString(`          <endpoint>`)
			if err := xml.EscapeText(buf, []byte(endpoint)); err != nil {
				return err
			}
			buf.WriteString(`</endpoint>`)
			buf.WriteString("\n")
		}
		buf.WriteString(`        </etcd>`)
		buf.WriteString("\n")
		buf.WriteString(`      </sync>`)
		buf.WriteString("\n")
	}
	buf.WriteString(`    </cluster>`)
	buf.WriteString("\n")
	buf.WriteString(`  </chassis>`)
	buf.WriteString("\n")
	return nil
}

// writeInterfacesXML writes interfaces configuration to XML with IETF namespace
func writeInterfacesXML(buf *bytes.Buffer, interfaces map[string]*config.Interface) error {
	buf.WriteString(`  <interfaces xmlns="` + IETFInterfacesNS + `">`)
	buf.WriteString("\n")

	for _, name := range sortedStringKeys(interfaces) {
		iface := interfaces[name]
		buf.WriteString(`    <interface>`)
		buf.WriteString("\n")

		buf.WriteString(`      <name>`)
		if err := xml.EscapeText(buf, []byte(name)); err != nil {
			return err
		}
		buf.WriteString(`</name>`)
		buf.WriteString("\n")

		if iface.Description != "" {
			buf.WriteString(`      <description>`)
			if err := xml.EscapeText(buf, []byte(iface.Description)); err != nil {
				return err
			}
			buf.WriteString(`</description>`)
			buf.WriteString("\n")
		}

		// Units (sub-interfaces)
		if len(iface.Units) > 0 {
			for _, unitNum := range sortedIntKeys(iface.Units) {
				unit := iface.Units[unitNum]
				buf.WriteString(`      <unit>`)
				buf.WriteString("\n")

				fmt.Fprintf(buf, "        <name>%d</name>\n", unitNum)

				// Address families
				if len(unit.Family) > 0 {
					for _, familyName := range sortedStringKeys(unit.Family) {
						family := unit.Family[familyName]
						buf.WriteString(`        <family>`)
						buf.WriteString("\n")

						buf.WriteString(`          <name>`)
						if err := xml.EscapeText(buf, []byte(familyName)); err != nil {
							return err
						}
						buf.WriteString(`</name>`)
						buf.WriteString("\n")

						// Addresses
						if len(family.Addresses) > 0 {
							for _, addr := range family.Addresses {
								buf.WriteString(`          <address>`)
								if err := xml.EscapeText(buf, []byte(addr)); err != nil {
									return err
								}
								buf.WriteString(`</address>`)
								buf.WriteString("\n")
							}
						}

						buf.WriteString(`        </family>`)
						buf.WriteString("\n")
					}
				}

				buf.WriteString(`      </unit>`)
				buf.WriteString("\n")
			}
		}

		buf.WriteString(`    </interface>`)
		buf.WriteString("\n")
	}

	buf.WriteString(`  </interfaces>`)
	buf.WriteString("\n")
	return nil
}

// writeRoutingOptionsXML writes routing options to XML with IETF routing namespace
func writeRoutingOptionsXML(buf *bytes.Buffer, ro *config.RoutingOptions) error {
	buf.WriteString(`  <routing xmlns="` + IETFRoutingNS + `">`)
	buf.WriteString("\n")

	if ro.RouterID != "" {
		buf.WriteString(`    <router-id>`)
		if err := xml.EscapeText(buf, []byte(ro.RouterID)); err != nil {
			return err
		}
		buf.WriteString(`</router-id>`)
		buf.WriteString("\n")
	}

	if ro.AutonomousSystem != 0 {
		fmt.Fprintf(buf, "    <autonomous-system>%d</autonomous-system>\n", ro.AutonomousSystem)
	}

	// Static routes
	if len(ro.StaticRoutes) > 0 {
		buf.WriteString(`    <static-routes>`)
		buf.WriteString("\n")

		for _, route := range ro.StaticRoutes {
			buf.WriteString(`      <route>`)
			buf.WriteString("\n")

			buf.WriteString(`        <prefix>`)
			if err := xml.EscapeText(buf, []byte(route.Prefix)); err != nil {
				return err
			}
			buf.WriteString(`</prefix>`)
			buf.WriteString("\n")

			buf.WriteString(`        <next-hop>`)
			if err := xml.EscapeText(buf, []byte(route.NextHop)); err != nil {
				return err
			}
			buf.WriteString(`</next-hop>`)
			buf.WriteString("\n")

			if route.Distance > 0 {
				fmt.Fprintf(buf, "        <distance>%d</distance>\n", route.Distance)
			}

			buf.WriteString(`      </route>`)
			buf.WriteString("\n")
		}

		buf.WriteString(`    </static-routes>`)
		buf.WriteString("\n")
	}

	buf.WriteString(`  </routing>`)
	buf.WriteString("\n")
	return nil
}

func writeRoutingInstancesXML(buf *bytes.Buffer, instances map[string]*config.RoutingInstance) error {
	buf.WriteString(`  <routing-instances xmlns="` + ArcaConfigNS + `">`)
	buf.WriteString("\n")

	for _, name := range sortedStringKeys(instances) {
		instance := instances[name]
		if instance == nil {
			continue
		}
		buf.WriteString(`    <instance>`)
		buf.WriteString("\n")
		buf.WriteString(`      <name>`)
		if err := xml.EscapeText(buf, []byte(name)); err != nil {
			return err
		}
		buf.WriteString(`</name>`)
		buf.WriteString("\n")
		if instance.InstanceType != "" {
			buf.WriteString(`      <instance-type>`)
			if err := xml.EscapeText(buf, []byte(instance.InstanceType)); err != nil {
				return err
			}
			buf.WriteString(`</instance-type>`)
			buf.WriteString("\n")
		}
		if instance.RouteDistinguisher != "" {
			buf.WriteString(`      <route-distinguisher>`)
			if err := xml.EscapeText(buf, []byte(instance.RouteDistinguisher)); err != nil {
				return err
			}
			buf.WriteString(`</route-distinguisher>`)
			buf.WriteString("\n")
		}
		if instance.VRFTarget != "" {
			buf.WriteString(`      <vrf-target>`)
			if err := xml.EscapeText(buf, []byte(instance.VRFTarget)); err != nil {
				return err
			}
			buf.WriteString(`</vrf-target>`)
			buf.WriteString("\n")
		}
		if err := writeStringListXML(buf, "vrf-target-import", instance.VRFTargetImport, "      "); err != nil {
			return err
		}
		if err := writeStringListXML(buf, "vrf-target-export", instance.VRFTargetExport, "      "); err != nil {
			return err
		}
		if err := writeStringListXML(buf, "vrf-import", instance.VRFImport, "      "); err != nil {
			return err
		}
		if err := writeStringListXML(buf, "vrf-export", instance.VRFExport, "      "); err != nil {
			return err
		}
		if err := writeStringListXML(buf, "interface", instance.Interfaces, "      "); err != nil {
			return err
		}
		buf.WriteString(`    </instance>`)
		buf.WriteString("\n")
	}

	buf.WriteString(`  </routing-instances>`)
	buf.WriteString("\n")
	return nil
}

func writeStringListXML(buf *bytes.Buffer, element string, values []string, indent string) error {
	for _, value := range values {
		buf.WriteString(indent)
		fmt.Fprintf(buf, "<%s>", element)
		if err := xml.EscapeText(buf, []byte(value)); err != nil {
			return err
		}
		fmt.Fprintf(buf, "</%s>\n", element)
	}
	return nil
}

func sortedStringKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedIntKeys[V any](m map[int]V) []int {
	keys := make([]int, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Ints(keys)
	return keys
}

// writeProtocolsXML writes protocol configuration to XML
func writeProtocolsXML(buf *bytes.Buffer, protocols *config.ProtocolConfig) error {
	buf.WriteString(`  <protocols xmlns="` + ArcaConfigNS + `">`)
	buf.WriteString("\n")

	if protocols.BFD != nil {
		if err := writeBFDXML(buf, protocols.BFD); err != nil {
			return err
		}
	}

	// BGP
	if protocols.BGP != nil {
		if err := writeBGPXML(buf, protocols.BGP); err != nil {
			return err
		}
	}

	// OSPF
	if protocols.OSPF != nil {
		if err := writeOSPFXML(buf, "ospf", protocols.OSPF); err != nil {
			return err
		}
	}
	if protocols.OSPF3 != nil {
		if err := writeOSPFXML(buf, "ospf3", protocols.OSPF3); err != nil {
			return err
		}
	}

	if protocols.MPLS != nil {
		if err := writeMPLSXML(buf, protocols.MPLS); err != nil {
			return err
		}
	}

	if protocols.VRRP != nil {
		if err := writeVRRPXML(buf, protocols.VRRP); err != nil {
			return err
		}
	}

	buf.WriteString(`  </protocols>`)
	buf.WriteString("\n")
	return nil
}

func writeBFDXML(buf *bytes.Buffer, bfd *config.BFDConfig) error {
	if len(bfd.Profiles) == 0 && len(bfd.Peers) == 0 {
		return nil
	}
	buf.WriteString(`    <bfd>`)
	buf.WriteString("\n")
	for _, name := range sortedStringKeys(bfd.Profiles) {
		profile := bfd.Profiles[name]
		if profile == nil {
			continue
		}
		buf.WriteString(`      <profile>`)
		buf.WriteString("\n")
		buf.WriteString(`        <name>`)
		if err := xml.EscapeText(buf, []byte(name)); err != nil {
			return err
		}
		buf.WriteString(`</name>`)
		buf.WriteString("\n")
		if err := writeBFDSessionXML(buf, profile.DetectMultiplier, profile.ReceiveInterval, profile.TransmitInterval, profile.EchoMode, profile.PassiveMode, "        "); err != nil {
			return err
		}
		buf.WriteString(`      </profile>`)
		buf.WriteString("\n")
	}
	for _, address := range sortedStringKeys(bfd.Peers) {
		peer := bfd.Peers[address]
		if peer == nil {
			continue
		}
		peerAddress := peer.Address
		if peerAddress == "" {
			peerAddress = address
		}
		buf.WriteString(`      <peer>`)
		buf.WriteString("\n")
		buf.WriteString(`        <address>`)
		if err := xml.EscapeText(buf, []byte(peerAddress)); err != nil {
			return err
		}
		buf.WriteString(`</address>`)
		buf.WriteString("\n")
		if peer.LocalAddress != "" {
			buf.WriteString(`        <local-address>`)
			if err := xml.EscapeText(buf, []byte(peer.LocalAddress)); err != nil {
				return err
			}
			buf.WriteString(`</local-address>`)
			buf.WriteString("\n")
		}
		if peer.Interface != "" {
			buf.WriteString(`        <interface>`)
			if err := xml.EscapeText(buf, []byte(peer.Interface)); err != nil {
				return err
			}
			buf.WriteString(`</interface>`)
			buf.WriteString("\n")
		}
		if peer.VRF != "" {
			buf.WriteString(`        <vrf>`)
			if err := xml.EscapeText(buf, []byte(peer.VRF)); err != nil {
				return err
			}
			buf.WriteString(`</vrf>`)
			buf.WriteString("\n")
		}
		if peer.Multihop {
			buf.WriteString(`        <multihop>true</multihop>`)
			buf.WriteString("\n")
		}
		if peer.Profile != "" {
			buf.WriteString(`        <profile>`)
			if err := xml.EscapeText(buf, []byte(peer.Profile)); err != nil {
				return err
			}
			buf.WriteString(`</profile>`)
			buf.WriteString("\n")
		}
		if err := writeBFDSessionXML(buf, peer.DetectMultiplier, peer.ReceiveInterval, peer.TransmitInterval, peer.EchoMode, peer.PassiveMode, "        "); err != nil {
			return err
		}
		if peer.Shutdown {
			buf.WriteString(`        <shutdown>true</shutdown>`)
			buf.WriteString("\n")
		}
		buf.WriteString(`      </peer>`)
		buf.WriteString("\n")
	}
	buf.WriteString(`    </bfd>`)
	buf.WriteString("\n")
	return nil
}

func writeBFDSessionXML(buf *bytes.Buffer, detectMultiplier, receiveInterval, transmitInterval int, echoMode, passiveMode bool, indent string) error {
	if detectMultiplier != 0 {
		fmt.Fprintf(buf, "%s<detect-multiplier>%d</detect-multiplier>\n", indent, detectMultiplier)
	}
	if receiveInterval != 0 {
		fmt.Fprintf(buf, "%s<receive-interval>%d</receive-interval>\n", indent, receiveInterval)
	}
	if transmitInterval != 0 {
		fmt.Fprintf(buf, "%s<transmit-interval>%d</transmit-interval>\n", indent, transmitInterval)
	}
	if echoMode {
		fmt.Fprintf(buf, "%s<echo-mode>true</echo-mode>\n", indent)
	}
	if passiveMode {
		fmt.Fprintf(buf, "%s<passive-mode>true</passive-mode>\n", indent)
	}
	return nil
}

// writeBGPXML writes BGP configuration to XML
func writeBGPXML(buf *bytes.Buffer, bgp *config.BGPConfig) error {
	buf.WriteString(`    <bgp>`)
	buf.WriteString("\n")

	if len(bgp.Groups) > 0 {
		for _, groupName := range sortedStringKeys(bgp.Groups) {
			group := bgp.Groups[groupName]
			if group == nil {
				continue
			}
			buf.WriteString(`      <group>`)
			buf.WriteString("\n")

			buf.WriteString(`        <name>`)
			if err := xml.EscapeText(buf, []byte(groupName)); err != nil {
				return err
			}
			buf.WriteString(`</name>`)
			buf.WriteString("\n")

			if group.Type != "" {
				buf.WriteString(`        <type>`)
				if err := xml.EscapeText(buf, []byte(group.Type)); err != nil {
					return err
				}
				buf.WriteString(`</type>`)
				buf.WriteString("\n")
			}

			if group.Import != "" {
				buf.WriteString(`        <import>`)
				if err := xml.EscapeText(buf, []byte(group.Import)); err != nil {
					return err
				}
				buf.WriteString(`</import>`)
				buf.WriteString("\n")
			}

			if group.Export != "" {
				buf.WriteString(`        <export>`)
				if err := xml.EscapeText(buf, []byte(group.Export)); err != nil {
					return err
				}
				buf.WriteString(`</export>`)
				buf.WriteString("\n")
			}

			// Neighbors
			if len(group.Neighbors) > 0 {
				for _, neighborIP := range sortedStringKeys(group.Neighbors) {
					neighbor := group.Neighbors[neighborIP]
					if neighbor == nil {
						continue
					}
					buf.WriteString(`        <neighbor>`)
					buf.WriteString("\n")

					buf.WriteString(`          <ip>`)
					if err := xml.EscapeText(buf, []byte(neighbor.IP)); err != nil {
						return err
					}
					buf.WriteString(`</ip>`)
					buf.WriteString("\n")

					fmt.Fprintf(buf, "          <peer-as>%d</peer-as>\n", neighbor.PeerAS)

					if neighbor.Description != "" {
						buf.WriteString(`          <description>`)
						if err := xml.EscapeText(buf, []byte(neighbor.Description)); err != nil {
							return err
						}
						buf.WriteString(`</description>`)
						buf.WriteString("\n")
					}

					if neighbor.LocalAddress != "" {
						buf.WriteString(`          <local-address>`)
						if err := xml.EscapeText(buf, []byte(neighbor.LocalAddress)); err != nil {
							return err
						}
						buf.WriteString(`</local-address>`)
						buf.WriteString("\n")
					}

					if neighbor.BFD || neighbor.BFDProfile != "" {
						buf.WriteString(`          <bfd>true</bfd>`)
						buf.WriteString("\n")
					}

					if neighbor.BFDProfile != "" {
						buf.WriteString(`          <bfd-profile>`)
						if err := xml.EscapeText(buf, []byte(neighbor.BFDProfile)); err != nil {
							return err
						}
						buf.WriteString(`</bfd-profile>`)
						buf.WriteString("\n")
					}

					buf.WriteString(`        </neighbor>`)
					buf.WriteString("\n")
				}
			}

			buf.WriteString(`      </group>`)
			buf.WriteString("\n")
		}
	}

	buf.WriteString(`    </bgp>`)
	buf.WriteString("\n")
	return nil
}

// writeOSPFXML writes OSPF configuration to XML
func writeOSPFXML(buf *bytes.Buffer, element string, ospf *config.OSPFConfig) error {
	fmt.Fprintf(buf, "    <%s>", element)
	buf.WriteString("\n")

	if ospf.RouterID != "" {
		buf.WriteString(`      <router-id>`)
		if err := xml.EscapeText(buf, []byte(ospf.RouterID)); err != nil {
			return err
		}
		buf.WriteString(`</router-id>`)
		buf.WriteString("\n")
	}

	if len(ospf.Areas) > 0 {
		for _, areaName := range sortedStringKeys(ospf.Areas) {
			area := ospf.Areas[areaName]
			if area == nil {
				continue
			}
			buf.WriteString(`      <area>`)
			buf.WriteString("\n")

			buf.WriteString(`        <name>`)
			if err := xml.EscapeText(buf, []byte(areaName)); err != nil {
				return err
			}
			buf.WriteString(`</name>`)
			buf.WriteString("\n")

			buf.WriteString(`        <area-id>`)
			if err := xml.EscapeText(buf, []byte(area.AreaID)); err != nil {
				return err
			}
			buf.WriteString(`</area-id>`)
			buf.WriteString("\n")

			// Interfaces
			if len(area.Interfaces) > 0 {
				for _, ifaceName := range sortedStringKeys(area.Interfaces) {
					ospfIface := area.Interfaces[ifaceName]
					if ospfIface == nil {
						continue
					}
					buf.WriteString(`        <interface>`)
					buf.WriteString("\n")

					buf.WriteString(`          <name>`)
					if err := xml.EscapeText(buf, []byte(ospfIface.Name)); err != nil {
						return err
					}
					buf.WriteString(`</name>`)
					buf.WriteString("\n")

					if ospfIface.Passive {
						buf.WriteString(`          <passive>true</passive>`)
						buf.WriteString("\n")
					}

					if ospfIface.Metric > 0 {
						fmt.Fprintf(buf, "          <metric>%d</metric>\n", ospfIface.Metric)
					}

					if ospfIface.PrioritySet || ospfIface.Priority > 0 {
						fmt.Fprintf(buf, "          <priority>%d</priority>\n", ospfIface.Priority)
					}

					if ospfIface.BFD || ospfIface.BFDProfile != "" {
						buf.WriteString(`          <bfd>true</bfd>`)
						buf.WriteString("\n")
					}

					if ospfIface.BFDProfile != "" {
						buf.WriteString(`          <bfd-profile>`)
						if err := xml.EscapeText(buf, []byte(ospfIface.BFDProfile)); err != nil {
							return err
						}
						buf.WriteString(`</bfd-profile>`)
						buf.WriteString("\n")
					}

					buf.WriteString(`        </interface>`)
					buf.WriteString("\n")
				}
			}

			buf.WriteString(`      </area>`)
			buf.WriteString("\n")
		}
	}

	fmt.Fprintf(buf, "    </%s>", element)
	buf.WriteString("\n")
	return nil
}

func writeMPLSXML(buf *bytes.Buffer, mpls *config.MPLSConfig) error {
	if len(mpls.Interfaces) == 0 {
		return nil
	}
	buf.WriteString(`    <mpls>`)
	buf.WriteString("\n")
	if err := writeStringListXML(buf, "interface", mpls.Interfaces, "      "); err != nil {
		return err
	}
	buf.WriteString(`    </mpls>`)
	buf.WriteString("\n")
	return nil
}

func writeVRRPXML(buf *bytes.Buffer, vrrp *config.VRRPConfig) error {
	if len(vrrp.Groups) == 0 {
		return nil
	}
	buf.WriteString(`    <vrrp>`)
	buf.WriteString("\n")
	for _, name := range sortedStringKeys(vrrp.Groups) {
		group := vrrp.Groups[name]
		if group == nil {
			continue
		}
		buf.WriteString(`      <group>`)
		buf.WriteString("\n")
		buf.WriteString(`        <name>`)
		if err := xml.EscapeText(buf, []byte(name)); err != nil {
			return err
		}
		buf.WriteString(`</name>`)
		buf.WriteString("\n")
		if group.Interface != "" {
			buf.WriteString(`        <interface>`)
			if err := xml.EscapeText(buf, []byte(group.Interface)); err != nil {
				return err
			}
			buf.WriteString(`</interface>`)
			buf.WriteString("\n")
		}
		if group.VirtualAddress != "" {
			buf.WriteString(`        <virtual-address>`)
			if err := xml.EscapeText(buf, []byte(group.VirtualAddress)); err != nil {
				return err
			}
			buf.WriteString(`</virtual-address>`)
			buf.WriteString("\n")
		}
		if group.Priority != 0 {
			fmt.Fprintf(buf, "        <priority>%d</priority>\n", group.Priority)
		}
		if group.Preempt {
			buf.WriteString(`        <preempt>true</preempt>`)
			buf.WriteString("\n")
		}
		buf.WriteString(`      </group>`)
		buf.WriteString("\n")
	}
	buf.WriteString(`    </vrrp>`)
	buf.WriteString("\n")
	return nil
}

func writeClassOfServiceXML(buf *bytes.Buffer, cos *config.ClassOfServiceConfig) error {
	buf.WriteString(`  <class-of-service xmlns="` + ArcaConfigNS + `">`)
	buf.WriteString("\n")

	if len(cos.ForwardingClasses) > 0 {
		buf.WriteString(`    <forwarding-classes>`)
		buf.WriteString("\n")
		for _, name := range sortedStringKeys(cos.ForwardingClasses) {
			fc := cos.ForwardingClasses[name]
			if fc == nil {
				continue
			}
			buf.WriteString(`      <forwarding-class>`)
			buf.WriteString("\n")
			buf.WriteString(`        <name>`)
			if err := xml.EscapeText(buf, []byte(name)); err != nil {
				return err
			}
			buf.WriteString(`</name>`)
			buf.WriteString("\n")
			fmt.Fprintf(buf, "        <queue>%d</queue>\n", fc.Queue)
			buf.WriteString(`      </forwarding-class>`)
			buf.WriteString("\n")
		}
		buf.WriteString(`    </forwarding-classes>`)
		buf.WriteString("\n")
	}

	if len(cos.TrafficControlProfiles) > 0 {
		buf.WriteString(`    <traffic-control-profiles>`)
		buf.WriteString("\n")
		for _, name := range sortedStringKeys(cos.TrafficControlProfiles) {
			profile := cos.TrafficControlProfiles[name]
			if profile == nil {
				continue
			}
			buf.WriteString(`      <traffic-control-profile>`)
			buf.WriteString("\n")
			buf.WriteString(`        <name>`)
			if err := xml.EscapeText(buf, []byte(name)); err != nil {
				return err
			}
			buf.WriteString(`</name>`)
			buf.WriteString("\n")
			if profile.ShapingRate != 0 {
				fmt.Fprintf(buf, "        <shaping-rate>%d</shaping-rate>\n", profile.ShapingRate)
			}
			if profile.SchedulerMap != "" {
				buf.WriteString(`        <scheduler-map>`)
				if err := xml.EscapeText(buf, []byte(profile.SchedulerMap)); err != nil {
					return err
				}
				buf.WriteString(`</scheduler-map>`)
				buf.WriteString("\n")
			}
			buf.WriteString(`      </traffic-control-profile>`)
			buf.WriteString("\n")
		}
		buf.WriteString(`    </traffic-control-profiles>`)
		buf.WriteString("\n")
	}

	if len(cos.Interfaces) > 0 {
		buf.WriteString(`    <interfaces>`)
		buf.WriteString("\n")
		for _, name := range sortedStringKeys(cos.Interfaces) {
			iface := cos.Interfaces[name]
			if iface == nil {
				continue
			}
			buf.WriteString(`      <interface>`)
			buf.WriteString("\n")
			buf.WriteString(`        <name>`)
			if err := xml.EscapeText(buf, []byte(name)); err != nil {
				return err
			}
			buf.WriteString(`</name>`)
			buf.WriteString("\n")
			if iface.OutputTrafficControlProfile != "" {
				buf.WriteString(`        <output-traffic-control-profile>`)
				if err := xml.EscapeText(buf, []byte(iface.OutputTrafficControlProfile)); err != nil {
					return err
				}
				buf.WriteString(`</output-traffic-control-profile>`)
				buf.WriteString("\n")
			}
			buf.WriteString(`      </interface>`)
			buf.WriteString("\n")
		}
		buf.WriteString(`    </interfaces>`)
		buf.WriteString("\n")
	}

	buf.WriteString(`  </class-of-service>`)
	buf.WriteString("\n")
	return nil
}

func writeSecurityXML(buf *bytes.Buffer, security *config.SecurityConfig) error {
	if (security.NETCONF == nil || security.NETCONF.SSH == nil || security.NETCONF.SSH.Port == 0) && security.RateLimit == nil {
		return nil
	}

	buf.WriteString(`  <security xmlns="` + ArcaConfigNS + `">`)
	buf.WriteString("\n")
	if security.NETCONF != nil && security.NETCONF.SSH != nil && security.NETCONF.SSH.Port != 0 {
		buf.WriteString(`    <netconf>`)
		buf.WriteString("\n")
		buf.WriteString(`      <ssh>`)
		buf.WriteString("\n")
		fmt.Fprintf(buf, "        <port>%d</port>\n", security.NETCONF.SSH.Port)
		buf.WriteString(`      </ssh>`)
		buf.WriteString("\n")
		buf.WriteString(`    </netconf>`)
		buf.WriteString("\n")
	}
	if security.RateLimit != nil {
		buf.WriteString(`    <rate-limit>`)
		buf.WriteString("\n")
		if security.RateLimit.PerIP != 0 {
			fmt.Fprintf(buf, "      <per-ip>%d</per-ip>\n", security.RateLimit.PerIP)
		}
		if security.RateLimit.PerUser != 0 {
			fmt.Fprintf(buf, "      <per-user>%d</per-user>\n", security.RateLimit.PerUser)
		}
		buf.WriteString(`    </rate-limit>`)
		buf.WriteString("\n")
	}
	buf.WriteString(`  </security>`)
	buf.WriteString("\n")
	return nil
}

// filterMatches is now implemented in xpath_filter.go
// This placeholder is kept for reference only

type xmlOSPFProtocol struct {
	RouterID string `xml:"router-id"`
	Areas    []struct {
		Name       string `xml:"name"`
		AreaID     string `xml:"area-id"`
		Interfaces []struct {
			Name       string `xml:"name"`
			Passive    bool   `xml:"passive"`
			Metric     int    `xml:"metric"`
			Priority   *int   `xml:"priority"`
			BFD        bool   `xml:"bfd"`
			BFDProfile string `xml:"bfd-profile"`
		} `xml:"interface"`
	} `xml:"area"`
}

type xmlBFDProtocol struct {
	Profiles []struct {
		Name             string `xml:"name"`
		DetectMultiplier int    `xml:"detect-multiplier"`
		ReceiveInterval  int    `xml:"receive-interval"`
		TransmitInterval int    `xml:"transmit-interval"`
		EchoMode         bool   `xml:"echo-mode"`
		PassiveMode      bool   `xml:"passive-mode"`
	} `xml:"profile"`
	Peers []struct {
		Address          string `xml:"address"`
		LocalAddress     string `xml:"local-address"`
		Interface        string `xml:"interface"`
		VRF              string `xml:"vrf"`
		Multihop         bool   `xml:"multihop"`
		Profile          string `xml:"profile"`
		DetectMultiplier int    `xml:"detect-multiplier"`
		ReceiveInterval  int    `xml:"receive-interval"`
		TransmitInterval int    `xml:"transmit-interval"`
		EchoMode         bool   `xml:"echo-mode"`
		PassiveMode      bool   `xml:"passive-mode"`
		Shutdown         bool   `xml:"shutdown"`
	} `xml:"peer"`
}

func bfdConfigFromXML(bfd *xmlBFDProtocol) *config.BFDConfig {
	if bfd == nil {
		return nil
	}
	cfgBFD := &config.BFDConfig{
		Profiles: make(map[string]*config.BFDProfile),
		Peers:    make(map[string]*config.BFDPeer),
	}
	for _, profile := range bfd.Profiles {
		cfgBFD.Profiles[profile.Name] = &config.BFDProfile{
			Name:             profile.Name,
			DetectMultiplier: profile.DetectMultiplier,
			ReceiveInterval:  profile.ReceiveInterval,
			TransmitInterval: profile.TransmitInterval,
			EchoMode:         profile.EchoMode,
			PassiveMode:      profile.PassiveMode,
		}
	}
	for _, peer := range bfd.Peers {
		cfgBFD.Peers[peer.Address] = &config.BFDPeer{
			Address:          peer.Address,
			LocalAddress:     peer.LocalAddress,
			Interface:        peer.Interface,
			VRF:              peer.VRF,
			Multihop:         peer.Multihop,
			Profile:          peer.Profile,
			DetectMultiplier: peer.DetectMultiplier,
			ReceiveInterval:  peer.ReceiveInterval,
			TransmitInterval: peer.TransmitInterval,
			EchoMode:         peer.EchoMode,
			PassiveMode:      peer.PassiveMode,
			Shutdown:         peer.Shutdown,
		}
	}
	return cfgBFD
}

func ospfConfigFromXML(ospf *xmlOSPFProtocol) *config.OSPFConfig {
	if ospf == nil {
		return nil
	}
	cfgOSPF := &config.OSPFConfig{
		RouterID: ospf.RouterID,
		Areas:    make(map[string]*config.OSPFArea),
	}
	for _, area := range ospf.Areas {
		cfgArea := &config.OSPFArea{
			AreaID:     area.AreaID,
			Interfaces: make(map[string]*config.OSPFInterface),
		}
		for _, ospfIface := range area.Interfaces {
			priority := 0
			prioritySet := false
			if ospfIface.Priority != nil {
				priority = *ospfIface.Priority
				prioritySet = true
			}
			cfgArea.Interfaces[ospfIface.Name] = &config.OSPFInterface{
				Name:        ospfIface.Name,
				Passive:     ospfIface.Passive,
				Metric:      ospfIface.Metric,
				Priority:    priority,
				PrioritySet: prioritySet,
				BFD:         ospfIface.BFD || ospfIface.BFDProfile != "",
				BFDProfile:  ospfIface.BFDProfile,
			}
		}
		cfgOSPF.Areas[area.Name] = cfgArea
	}
	return cfgOSPF
}

// XMLToConfig converts NETCONF XML to internal config structure.
func XMLToConfig(xmlData []byte, defaultOp DefaultOperation) (*config.Config, error) {
	// Security: Validate size
	if len(xmlData) > MaxXMLSize {
		return nil, NewRPCError(ErrorTypeProtocol, ErrorTagInvalidValue,
			fmt.Sprintf("XML size exceeds maximum (%d bytes)", MaxXMLSize)).
			WithPath("/rpc/edit-config/config").
			WithAppTag("size-limit")
	}

	// Security: DTD/ENTITY check before normalizing fragments.
	if err := ValidateXMLSecurity(xmlData); err != nil {
		return nil, err
	}

	normalizedXML, err := normalizeConfigXML(xmlData)
	if err != nil {
		return nil, err
	}
	if err := validateConfigXMLAllowlist(normalizedXML); err != nil {
		return nil, err
	}

	// Parse XML structure with allowlist validation
	var root struct {
		XMLName xml.Name `xml:"config"`
		System  *struct {
			HostName string `xml:"host-name"`
			Services *struct {
				WebUI *struct {
					Enabled       bool   `xml:"enabled"`
					ListenAddress string `xml:"listen-address"`
					Port          int    `xml:"port"`
				} `xml:"web-ui"`
				Prometheus *struct {
					Enabled       bool   `xml:"enabled"`
					ListenAddress string `xml:"listen-address"`
					Port          int    `xml:"port"`
				} `xml:"prometheus"`
				SNMP *struct {
					Enabled       bool   `xml:"enabled"`
					ListenAddress string `xml:"listen-address"`
					Port          int    `xml:"port"`
					Community     string `xml:"community"`
				} `xml:"snmp"`
			} `xml:"services"`
		} `xml:"system"`
		Chassis *struct {
			Cluster *struct {
				Enabled bool `xml:"enabled"`
				Nodes   []struct {
					Name     string `xml:"name"`
					Address  string `xml:"address"`
					Priority int    `xml:"priority"`
				} `xml:"node"`
				Sync *struct {
					Etcd *struct {
						Endpoints []string `xml:"endpoint"`
					} `xml:"etcd"`
				} `xml:"sync"`
			} `xml:"cluster"`
		} `xml:"chassis"`
		Interfaces []struct {
			Name        string `xml:"name"`
			Description string `xml:"description"`
			Units       []struct {
				Name   int `xml:"name"`
				Family []struct {
					Name      string   `xml:"name"`
					Addresses []string `xml:"address"`
				} `xml:"family"`
			} `xml:"unit"`
		} `xml:"interfaces>interface"`
		Routing *struct {
			RouterID         string `xml:"router-id"`
			AutonomousSystem uint32 `xml:"autonomous-system"`
			StaticRoutes     []struct {
				Prefix   string `xml:"prefix"`
				NextHop  string `xml:"next-hop"`
				Distance int    `xml:"distance"`
			} `xml:"static-routes>route"`
		} `xml:"routing"`
		RoutingInstances []struct {
			Name               string   `xml:"name"`
			InstanceType       string   `xml:"instance-type"`
			RouteDistinguisher string   `xml:"route-distinguisher"`
			VRFTarget          string   `xml:"vrf-target"`
			VRFTargetImport    []string `xml:"vrf-target-import"`
			VRFTargetExport    []string `xml:"vrf-target-export"`
			VRFImport          []string `xml:"vrf-import"`
			VRFExport          []string `xml:"vrf-export"`
			Interfaces         []string `xml:"interface"`
		} `xml:"routing-instances>instance"`
		Protocols *struct {
			BFD *xmlBFDProtocol `xml:"bfd"`
			BGP *struct {
				Groups []struct {
					Name      string `xml:"name"`
					Type      string `xml:"type"`
					Import    string `xml:"import"`
					Export    string `xml:"export"`
					Neighbors []struct {
						IP           string `xml:"ip"`
						PeerAS       uint32 `xml:"peer-as"`
						Description  string `xml:"description"`
						LocalAddress string `xml:"local-address"`
						BFD          bool   `xml:"bfd"`
						BFDProfile   string `xml:"bfd-profile"`
					} `xml:"neighbor"`
				} `xml:"group"`
			} `xml:"bgp"`
			OSPF  *xmlOSPFProtocol `xml:"ospf"`
			OSPF3 *xmlOSPFProtocol `xml:"ospf3"`
			MPLS  *struct {
				Interfaces []string `xml:"interface"`
			} `xml:"mpls"`
			VRRP *struct {
				Groups []struct {
					Name           string `xml:"name"`
					Interface      string `xml:"interface"`
					VirtualAddress string `xml:"virtual-address"`
					Priority       int    `xml:"priority"`
					Preempt        bool   `xml:"preempt"`
				} `xml:"group"`
			} `xml:"vrrp"`
		} `xml:"protocols"`
		ClassOfService *struct {
			ForwardingClasses []struct {
				Name  string `xml:"name"`
				Queue int    `xml:"queue"`
			} `xml:"forwarding-classes>forwarding-class"`
			TrafficControlProfiles []struct {
				Name         string `xml:"name"`
				ShapingRate  uint64 `xml:"shaping-rate"`
				SchedulerMap string `xml:"scheduler-map"`
			} `xml:"traffic-control-profiles>traffic-control-profile"`
			Interfaces []struct {
				Name                        string `xml:"name"`
				OutputTrafficControlProfile string `xml:"output-traffic-control-profile"`
			} `xml:"interfaces>interface"`
		} `xml:"class-of-service"`
		Security *struct {
			NETCONF *struct {
				SSH *struct {
					Port int `xml:"port"`
				} `xml:"ssh"`
			} `xml:"netconf"`
			RateLimit *struct {
				PerIP   int `xml:"per-ip"`
				PerUser int `xml:"per-user"`
			} `xml:"rate-limit"`
		} `xml:"security"`
	}

	// Parse with strict settings
	decoder := xml.NewDecoder(bytes.NewReader(normalizedXML))
	decoder.Strict = true
	decoder.Entity = nil

	if err := decoder.Decode(&root); err != nil {
		return nil, NewRPCError(ErrorTypeRPC, ErrorTagMalformedMessage,
			fmt.Sprintf("failed to parse config XML: %v", err)).
			WithPath("/rpc/edit-config/config")
	}

	// Convert to config.Config
	cfg := config.NewConfig()

	// System
	if root.System != nil {
		cfg.System = &config.SystemConfig{
			HostName: root.System.HostName,
		}
		if root.System.Services != nil {
			cfg.System.Services = &config.SystemServicesConfig{}
			if root.System.Services.WebUI != nil {
				cfg.System.Services.WebUI = &config.WebUIConfig{
					Enabled:       root.System.Services.WebUI.Enabled,
					ListenAddress: root.System.Services.WebUI.ListenAddress,
					Port:          root.System.Services.WebUI.Port,
				}
			}
			if root.System.Services.Prometheus != nil {
				cfg.System.Services.Prometheus = &config.PrometheusConfig{
					Enabled:       root.System.Services.Prometheus.Enabled,
					ListenAddress: root.System.Services.Prometheus.ListenAddress,
					Port:          root.System.Services.Prometheus.Port,
				}
			}
			if root.System.Services.SNMP != nil {
				cfg.System.Services.SNMP = &config.SNMPConfig{
					Enabled:       root.System.Services.SNMP.Enabled,
					ListenAddress: root.System.Services.SNMP.ListenAddress,
					Port:          root.System.Services.SNMP.Port,
					Community:     root.System.Services.SNMP.Community,
				}
			}
		}
	}

	// Chassis
	if root.Chassis != nil && root.Chassis.Cluster != nil {
		cfg.Chassis = &config.ChassisConfig{
			Cluster: &config.ClusterConfig{
				Enabled: root.Chassis.Cluster.Enabled,
				Nodes:   make(map[string]*config.ClusterNode),
			},
		}
		for _, node := range root.Chassis.Cluster.Nodes {
			cfg.Chassis.Cluster.Nodes[node.Name] = &config.ClusterNode{
				Name:     node.Name,
				Address:  node.Address,
				Priority: node.Priority,
			}
		}
		if root.Chassis.Cluster.Sync != nil && root.Chassis.Cluster.Sync.Etcd != nil {
			cfg.Chassis.Cluster.Sync = &config.ClusterSyncConfig{
				Etcd: &config.EtcdSyncConfig{
					Endpoints: append([]string(nil), root.Chassis.Cluster.Sync.Etcd.Endpoints...),
				},
			}
		}
	}

	// Interfaces
	for _, iface := range root.Interfaces {
		cfgIface := cfg.GetOrCreateInterface(iface.Name)
		cfgIface.Description = iface.Description

		for _, unit := range iface.Units {
			cfgUnit := cfgIface.GetOrCreateUnit(unit.Name)

			for _, family := range unit.Family {
				cfgFamily := cfgUnit.GetOrCreateFamily(family.Name)
				cfgFamily.Addresses = append(cfgFamily.Addresses, family.Addresses...)
			}
		}
	}

	// Routing options
	if root.Routing != nil {
		cfg.RoutingOptions = &config.RoutingOptions{
			RouterID:         root.Routing.RouterID,
			AutonomousSystem: root.Routing.AutonomousSystem,
		}

		for _, route := range root.Routing.StaticRoutes {
			cfg.RoutingOptions.StaticRoutes = append(cfg.RoutingOptions.StaticRoutes,
				&config.StaticRoute{
					Prefix:   route.Prefix,
					NextHop:  route.NextHop,
					Distance: route.Distance,
				})
		}
	}

	// Routing instances
	if len(root.RoutingInstances) > 0 {
		cfg.RoutingInstances = make(map[string]*config.RoutingInstance)
		for _, instance := range root.RoutingInstances {
			cfg.RoutingInstances[instance.Name] = &config.RoutingInstance{
				Name:               instance.Name,
				InstanceType:       instance.InstanceType,
				RouteDistinguisher: instance.RouteDistinguisher,
				VRFTarget:          instance.VRFTarget,
				VRFTargetImport:    append([]string(nil), instance.VRFTargetImport...),
				VRFTargetExport:    append([]string(nil), instance.VRFTargetExport...),
				VRFImport:          append([]string(nil), instance.VRFImport...),
				VRFExport:          append([]string(nil), instance.VRFExport...),
				Interfaces:         append([]string(nil), instance.Interfaces...),
			}
		}
	}

	// Protocols
	if root.Protocols != nil {
		cfg.Protocols = &config.ProtocolConfig{}

		if root.Protocols.BFD != nil {
			cfg.Protocols.BFD = bfdConfigFromXML(root.Protocols.BFD)
		}

		// BGP
		if root.Protocols.BGP != nil {
			cfg.Protocols.BGP = &config.BGPConfig{
				Groups: make(map[string]*config.BGPGroup),
			}

			for _, group := range root.Protocols.BGP.Groups {
				cfgGroup := &config.BGPGroup{
					Type:      group.Type,
					Import:    group.Import,
					Export:    group.Export,
					Neighbors: make(map[string]*config.BGPNeighbor),
				}

				for _, neighbor := range group.Neighbors {
					cfgGroup.Neighbors[neighbor.IP] = &config.BGPNeighbor{
						IP:           neighbor.IP,
						PeerAS:       neighbor.PeerAS,
						Description:  neighbor.Description,
						LocalAddress: neighbor.LocalAddress,
						BFD:          neighbor.BFD || neighbor.BFDProfile != "",
						BFDProfile:   neighbor.BFDProfile,
					}
				}

				cfg.Protocols.BGP.Groups[group.Name] = cfgGroup
			}
		}

		// OSPF
		if root.Protocols.OSPF != nil {
			cfg.Protocols.OSPF = ospfConfigFromXML(root.Protocols.OSPF)
		}
		if root.Protocols.OSPF3 != nil {
			cfg.Protocols.OSPF3 = ospfConfigFromXML(root.Protocols.OSPF3)
		}

		// MPLS
		if root.Protocols.MPLS != nil {
			cfg.Protocols.MPLS = &config.MPLSConfig{
				Interfaces: append([]string(nil), root.Protocols.MPLS.Interfaces...),
			}
		}

		// VRRP
		if root.Protocols.VRRP != nil {
			cfg.Protocols.VRRP = &config.VRRPConfig{
				Groups: make(map[string]*config.VRRPGroup),
			}
			for _, group := range root.Protocols.VRRP.Groups {
				cfg.Protocols.VRRP.Groups[group.Name] = &config.VRRPGroup{
					Name:           group.Name,
					Interface:      group.Interface,
					VirtualAddress: group.VirtualAddress,
					Priority:       group.Priority,
					Preempt:        group.Preempt,
				}
			}
		}
	}

	// Class of service
	if root.ClassOfService != nil {
		cfg.ClassOfService = &config.ClassOfServiceConfig{
			ForwardingClasses:      make(map[string]*config.ForwardingClass),
			TrafficControlProfiles: make(map[string]*config.TrafficControlProfile),
			Interfaces:             make(map[string]*config.CoSInterface),
		}
		for _, fc := range root.ClassOfService.ForwardingClasses {
			cfg.ClassOfService.ForwardingClasses[fc.Name] = &config.ForwardingClass{
				Name:  fc.Name,
				Queue: fc.Queue,
			}
		}
		for _, profile := range root.ClassOfService.TrafficControlProfiles {
			cfg.ClassOfService.TrafficControlProfiles[profile.Name] = &config.TrafficControlProfile{
				Name:         profile.Name,
				ShapingRate:  profile.ShapingRate,
				SchedulerMap: profile.SchedulerMap,
			}
		}
		for _, iface := range root.ClassOfService.Interfaces {
			cfg.ClassOfService.Interfaces[iface.Name] = &config.CoSInterface{
				Name:                        iface.Name,
				OutputTrafficControlProfile: iface.OutputTrafficControlProfile,
			}
		}
	}

	// Security
	if root.Security != nil {
		cfg.Security = &config.SecurityConfig{}
		if root.Security.NETCONF != nil && root.Security.NETCONF.SSH != nil {
			cfg.Security.NETCONF = &config.NETCONFConfig{
				SSH: &config.NETCONFSSHConfig{
					Port: root.Security.NETCONF.SSH.Port,
				},
			}
		}
		if root.Security.RateLimit != nil {
			cfg.Security.RateLimit = &config.RateLimitConfig{
				PerIP:   root.Security.RateLimit.PerIP,
				PerUser: root.Security.RateLimit.PerUser,
			}
		}
	}

	// Validate depth and element count
	if err := ValidateConfig(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

var allowedConfigElementPaths = map[string]struct{}{
	"config": {},

	"config/system":                                    {},
	"config/system/host-name":                          {},
	"config/system/services":                           {},
	"config/system/services/web-ui":                    {},
	"config/system/services/web-ui/enabled":            {},
	"config/system/services/web-ui/listen-address":     {},
	"config/system/services/web-ui/port":               {},
	"config/system/services/prometheus":                {},
	"config/system/services/prometheus/enabled":        {},
	"config/system/services/prometheus/listen-address": {},
	"config/system/services/prometheus/port":           {},
	"config/system/services/snmp":                      {},
	"config/system/services/snmp/enabled":              {},
	"config/system/services/snmp/listen-address":       {},
	"config/system/services/snmp/port":                 {},
	"config/system/services/snmp/community":            {},
	"config/chassis":                                   {},
	"config/chassis/cluster":                           {},
	"config/chassis/cluster/enabled":                   {},
	"config/chassis/cluster/node":                      {},
	"config/chassis/cluster/node/name":                 {},
	"config/chassis/cluster/node/address":              {},
	"config/chassis/cluster/node/priority":             {},
	"config/chassis/cluster/sync":                      {},
	"config/chassis/cluster/sync/etcd":                 {},
	"config/chassis/cluster/sync/etcd/endpoint":        {},

	"config/interfaces":                               {},
	"config/interfaces/interface":                     {},
	"config/interfaces/interface/name":                {},
	"config/interfaces/interface/description":         {},
	"config/interfaces/interface/unit":                {},
	"config/interfaces/interface/unit/name":           {},
	"config/interfaces/interface/unit/family":         {},
	"config/interfaces/interface/unit/family/name":    {},
	"config/interfaces/interface/unit/family/address": {},

	"config/routing":                              {},
	"config/routing/router-id":                    {},
	"config/routing/autonomous-system":            {},
	"config/routing/static-routes":                {},
	"config/routing/static-routes/route":          {},
	"config/routing/static-routes/route/prefix":   {},
	"config/routing/static-routes/route/next-hop": {},
	"config/routing/static-routes/route/distance": {},

	"config/routing-instances":                              {},
	"config/routing-instances/instance":                     {},
	"config/routing-instances/instance/name":                {},
	"config/routing-instances/instance/instance-type":       {},
	"config/routing-instances/instance/route-distinguisher": {},
	"config/routing-instances/instance/vrf-target":          {},
	"config/routing-instances/instance/vrf-target-import":   {},
	"config/routing-instances/instance/vrf-target-export":   {},
	"config/routing-instances/instance/vrf-import":          {},
	"config/routing-instances/instance/vrf-export":          {},
	"config/routing-instances/instance/interface":           {},

	"config/protocols":                                  {},
	"config/protocols/bfd":                              {},
	"config/protocols/bfd/profile":                      {},
	"config/protocols/bfd/profile/name":                 {},
	"config/protocols/bfd/profile/detect-multiplier":    {},
	"config/protocols/bfd/profile/receive-interval":     {},
	"config/protocols/bfd/profile/transmit-interval":    {},
	"config/protocols/bfd/profile/echo-mode":            {},
	"config/protocols/bfd/profile/passive-mode":         {},
	"config/protocols/bfd/peer":                         {},
	"config/protocols/bfd/peer/address":                 {},
	"config/protocols/bfd/peer/local-address":           {},
	"config/protocols/bfd/peer/interface":               {},
	"config/protocols/bfd/peer/vrf":                     {},
	"config/protocols/bfd/peer/multihop":                {},
	"config/protocols/bfd/peer/profile":                 {},
	"config/protocols/bfd/peer/detect-multiplier":       {},
	"config/protocols/bfd/peer/receive-interval":        {},
	"config/protocols/bfd/peer/transmit-interval":       {},
	"config/protocols/bfd/peer/echo-mode":               {},
	"config/protocols/bfd/peer/passive-mode":            {},
	"config/protocols/bfd/peer/shutdown":                {},
	"config/protocols/bgp":                              {},
	"config/protocols/bgp/group":                        {},
	"config/protocols/bgp/group/name":                   {},
	"config/protocols/bgp/group/type":                   {},
	"config/protocols/bgp/group/import":                 {},
	"config/protocols/bgp/group/export":                 {},
	"config/protocols/bgp/group/neighbor":               {},
	"config/protocols/bgp/group/neighbor/ip":            {},
	"config/protocols/bgp/group/neighbor/peer-as":       {},
	"config/protocols/bgp/group/neighbor/description":   {},
	"config/protocols/bgp/group/neighbor/local-address": {},
	"config/protocols/bgp/group/neighbor/bfd":           {},
	"config/protocols/bgp/group/neighbor/bfd-profile":   {},
	"config/protocols/ospf":                             {},
	"config/protocols/ospf/router-id":                   {},
	"config/protocols/ospf/area":                        {},
	"config/protocols/ospf/area/name":                   {},
	"config/protocols/ospf/area/area-id":                {},
	"config/protocols/ospf/area/interface":              {},
	"config/protocols/ospf/area/interface/name":         {},
	"config/protocols/ospf/area/interface/passive":      {},
	"config/protocols/ospf/area/interface/metric":       {},
	"config/protocols/ospf/area/interface/priority":     {},
	"config/protocols/ospf/area/interface/bfd":          {},
	"config/protocols/ospf/area/interface/bfd-profile":  {},
	"config/protocols/ospf3":                            {},
	"config/protocols/ospf3/router-id":                  {},
	"config/protocols/ospf3/area":                       {},
	"config/protocols/ospf3/area/name":                  {},
	"config/protocols/ospf3/area/area-id":               {},
	"config/protocols/ospf3/area/interface":             {},
	"config/protocols/ospf3/area/interface/name":        {},
	"config/protocols/ospf3/area/interface/passive":     {},
	"config/protocols/ospf3/area/interface/metric":      {},
	"config/protocols/ospf3/area/interface/priority":    {},
	"config/protocols/ospf3/area/interface/bfd":         {},
	"config/protocols/ospf3/area/interface/bfd-profile": {},
	"config/protocols/mpls":                             {},
	"config/protocols/mpls/interface":                   {},
	"config/protocols/vrrp":                             {},
	"config/protocols/vrrp/group":                       {},
	"config/protocols/vrrp/group/name":                  {},
	"config/protocols/vrrp/group/interface":             {},
	"config/protocols/vrrp/group/virtual-address":       {},
	"config/protocols/vrrp/group/priority":              {},
	"config/protocols/vrrp/group/preempt":               {},

	"config/class-of-service":                                                                {},
	"config/class-of-service/forwarding-classes":                                             {},
	"config/class-of-service/forwarding-classes/forwarding-class":                            {},
	"config/class-of-service/forwarding-classes/forwarding-class/name":                       {},
	"config/class-of-service/forwarding-classes/forwarding-class/queue":                      {},
	"config/class-of-service/traffic-control-profiles":                                       {},
	"config/class-of-service/traffic-control-profiles/traffic-control-profile":               {},
	"config/class-of-service/traffic-control-profiles/traffic-control-profile/name":          {},
	"config/class-of-service/traffic-control-profiles/traffic-control-profile/shaping-rate":  {},
	"config/class-of-service/traffic-control-profiles/traffic-control-profile/scheduler-map": {},
	"config/class-of-service/interfaces":                                                     {},
	"config/class-of-service/interfaces/interface":                                           {},
	"config/class-of-service/interfaces/interface/name":                                      {},
	"config/class-of-service/interfaces/interface/output-traffic-control-profile":            {},

	"config/security":                     {},
	"config/security/netconf":             {},
	"config/security/netconf/ssh":         {},
	"config/security/netconf/ssh/port":    {},
	"config/security/rate-limit":          {},
	"config/security/rate-limit/per-ip":   {},
	"config/security/rate-limit/per-user": {},
}

var configTextContentPaths = map[string]struct{}{
	"config/system/host-name":                          {},
	"config/system/services/web-ui/enabled":            {},
	"config/system/services/web-ui/listen-address":     {},
	"config/system/services/web-ui/port":               {},
	"config/system/services/prometheus/enabled":        {},
	"config/system/services/prometheus/listen-address": {},
	"config/system/services/prometheus/port":           {},
	"config/system/services/snmp/enabled":              {},
	"config/system/services/snmp/listen-address":       {},
	"config/system/services/snmp/port":                 {},
	"config/system/services/snmp/community":            {},
	"config/chassis/cluster/enabled":                   {},
	"config/chassis/cluster/node/name":                 {},
	"config/chassis/cluster/node/address":              {},
	"config/chassis/cluster/node/priority":             {},
	"config/chassis/cluster/sync/etcd/endpoint":        {},

	"config/interfaces/interface/name":                {},
	"config/interfaces/interface/description":         {},
	"config/interfaces/interface/unit/name":           {},
	"config/interfaces/interface/unit/family/name":    {},
	"config/interfaces/interface/unit/family/address": {},

	"config/routing/router-id":                    {},
	"config/routing/autonomous-system":            {},
	"config/routing/static-routes/route/prefix":   {},
	"config/routing/static-routes/route/next-hop": {},
	"config/routing/static-routes/route/distance": {},

	"config/routing-instances/instance/name":                {},
	"config/routing-instances/instance/instance-type":       {},
	"config/routing-instances/instance/route-distinguisher": {},
	"config/routing-instances/instance/vrf-target":          {},
	"config/routing-instances/instance/vrf-target-import":   {},
	"config/routing-instances/instance/vrf-target-export":   {},
	"config/routing-instances/instance/vrf-import":          {},
	"config/routing-instances/instance/vrf-export":          {},
	"config/routing-instances/instance/interface":           {},

	"config/protocols/bfd/profile/name":              {},
	"config/protocols/bfd/profile/detect-multiplier": {},
	"config/protocols/bfd/profile/receive-interval":  {},
	"config/protocols/bfd/profile/transmit-interval": {},
	"config/protocols/bfd/profile/echo-mode":         {},
	"config/protocols/bfd/profile/passive-mode":      {},
	"config/protocols/bfd/peer/address":              {},
	"config/protocols/bfd/peer/local-address":        {},
	"config/protocols/bfd/peer/interface":            {},
	"config/protocols/bfd/peer/vrf":                  {},
	"config/protocols/bfd/peer/multihop":             {},
	"config/protocols/bfd/peer/profile":              {},
	"config/protocols/bfd/peer/detect-multiplier":    {},
	"config/protocols/bfd/peer/receive-interval":     {},
	"config/protocols/bfd/peer/transmit-interval":    {},
	"config/protocols/bfd/peer/echo-mode":            {},
	"config/protocols/bfd/peer/passive-mode":         {},
	"config/protocols/bfd/peer/shutdown":             {},

	"config/protocols/bgp/group/name":                   {},
	"config/protocols/bgp/group/type":                   {},
	"config/protocols/bgp/group/import":                 {},
	"config/protocols/bgp/group/export":                 {},
	"config/protocols/bgp/group/neighbor/ip":            {},
	"config/protocols/bgp/group/neighbor/peer-as":       {},
	"config/protocols/bgp/group/neighbor/description":   {},
	"config/protocols/bgp/group/neighbor/local-address": {},
	"config/protocols/bgp/group/neighbor/bfd":           {},
	"config/protocols/bgp/group/neighbor/bfd-profile":   {},

	"config/protocols/ospf/router-id":                   {},
	"config/protocols/ospf/area/name":                   {},
	"config/protocols/ospf/area/area-id":                {},
	"config/protocols/ospf/area/interface/name":         {},
	"config/protocols/ospf/area/interface/passive":      {},
	"config/protocols/ospf/area/interface/metric":       {},
	"config/protocols/ospf/area/interface/priority":     {},
	"config/protocols/ospf/area/interface/bfd":          {},
	"config/protocols/ospf/area/interface/bfd-profile":  {},
	"config/protocols/ospf3/router-id":                  {},
	"config/protocols/ospf3/area/name":                  {},
	"config/protocols/ospf3/area/area-id":               {},
	"config/protocols/ospf3/area/interface/name":        {},
	"config/protocols/ospf3/area/interface/passive":     {},
	"config/protocols/ospf3/area/interface/metric":      {},
	"config/protocols/ospf3/area/interface/priority":    {},
	"config/protocols/ospf3/area/interface/bfd":         {},
	"config/protocols/ospf3/area/interface/bfd-profile": {},
	"config/protocols/mpls/interface":                   {},
	"config/protocols/vrrp/group/name":                  {},
	"config/protocols/vrrp/group/interface":             {},
	"config/protocols/vrrp/group/virtual-address":       {},
	"config/protocols/vrrp/group/priority":              {},
	"config/protocols/vrrp/group/preempt":               {},

	"config/class-of-service/forwarding-classes/forwarding-class/name":                       {},
	"config/class-of-service/forwarding-classes/forwarding-class/queue":                      {},
	"config/class-of-service/traffic-control-profiles/traffic-control-profile/name":          {},
	"config/class-of-service/traffic-control-profiles/traffic-control-profile/shaping-rate":  {},
	"config/class-of-service/traffic-control-profiles/traffic-control-profile/scheduler-map": {},
	"config/class-of-service/interfaces/interface/name":                                      {},
	"config/class-of-service/interfaces/interface/output-traffic-control-profile":            {},

	"config/security/netconf/ssh/port":    {},
	"config/security/rate-limit/per-ip":   {},
	"config/security/rate-limit/per-user": {},
}

func isConfigTextContentPath(path []string) bool {
	_, ok := configTextContentPaths[strings.Join(path, "/")]
	return ok
}

func normalizeConfigXML(xmlData []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(xmlData)
	if len(trimmed) == 0 {
		return []byte("<config/>"), nil
	}

	decoder := xml.NewDecoder(bytes.NewReader(trimmed))
	decoder.Strict = true
	decoder.Entity = nil
	sawProcInst := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return []byte("<config/>"), nil
		}
		if err != nil {
			return nil, NewRPCError(ErrorTypeRPC, ErrorTagMalformedMessage,
				fmt.Sprintf("failed to parse config XML: %v", err)).
				WithPath("/rpc/edit-config/config")
		}
		switch t := token.(type) {
		case xml.ProcInst:
			sawProcInst = true
		case xml.CharData:
			if len(bytes.TrimSpace(t)) > 0 {
				return nil, ErrMalformedMessage("unexpected text in /rpc/edit-config/config").
					WithPath("/rpc/edit-config/config")
			}
		case xml.StartElement:
			if t.Name.Local == "config" {
				return trimmed, nil
			}
			if sawProcInst {
				return nil, NewRPCError(ErrorTypeRPC, ErrorTagMalformedMessage,
					"XML declaration requires a config root element").
					WithPath("/rpc/edit-config/config")
			}
			normalized := make([]byte, 0, len(trimmed)+len("<config></config>"))
			normalized = append(normalized, "<config>"...)
			normalized = append(normalized, trimmed...)
			normalized = append(normalized, "</config>"...)
			return normalized, nil
		}
	}
}

func validateConfigXMLAllowlist(xmlData []byte) error {
	decoder := xml.NewDecoder(bytes.NewReader(xmlData))
	decoder.Strict = true
	decoder.Entity = nil
	stack := []string{}
	elementCount := 0

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return NewRPCError(ErrorTypeRPC, ErrorTagMalformedMessage,
				fmt.Sprintf("invalid XML: %v", err)).
				WithPath("/rpc/edit-config/config")
		}

		switch t := token.(type) {
		case xml.StartElement:
			elementCount++
			if elementCount > MaxXMLElements {
				return NewRPCError(ErrorTypeProtocol, ErrorTagInvalidValue,
					fmt.Sprintf("config XML exceeds maximum element limit (%d)", MaxXMLElements)).
					WithPath("/rpc/edit-config/config").
					WithAppTag("size-limit")
			}
			path := append(append([]string{}, stack...), t.Name.Local)
			if err := validateConfigElement(t.Name, path); err != nil {
				return err
			}
			if err := validateConfigAttributes(t, path); err != nil {
				return err
			}
			stack = append(stack, t.Name.Local)
		case xml.EndElement:
			if len(stack) == 0 {
				return NewRPCError(ErrorTypeRPC, ErrorTagMalformedMessage,
					fmt.Sprintf("unexpected closing element: %s", t.Name.Local)).
					WithPath("/rpc/edit-config/config")
			}
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if err := validateConfigTextContent(stack, t); err != nil {
				return err
			}
		}
	}
}

func validateConfigTextContent(path []string, text xml.CharData) error {
	if len(bytes.TrimSpace(text)) == 0 {
		return nil
	}
	if isConfigTextContentPath(path) {
		return nil
	}
	return ErrMalformedMessage(fmt.Sprintf("unexpected text in %s", configElementRPCPath(path))).
		WithPath(configElementRPCPath(path))
}

func validateConfigAttributes(start xml.StartElement, path []string) error {
	if len(start.Attr) > MaxXMLAttributes {
		return NewRPCError(ErrorTypeProtocol, ErrorTagInvalidValue,
			fmt.Sprintf("config element %s exceeds maximum attribute limit (%d)", start.Name.Local, MaxXMLAttributes)).
			WithPath(configElementRPCPath(path)).
			WithAppTag("attribute-limit")
	}
	for _, attr := range start.Attr {
		if isNamespaceDeclarationAttribute(attr) {
			if !isAllowedConfigNamespaceDeclaration(attr.Value) {
				return NewRPCError(ErrorTypeProtocol, ErrorTagUnknownNamespace,
					fmt.Sprintf("invalid namespace declaration for config element %s", start.Name.Local)).
					WithPath(configElementRPCPath(path)).
					WithBadNamespace(attr.Value)
			}
			continue
		}
		if attr.Name.Local == "operation" && (attr.Name.Space == "" || attr.Name.Space == NetconfBaseNS) {
			return NewRPCError(ErrorTypeProtocol, ErrorTagOperationNotSupported,
				"per-element operation attributes are not supported").
				WithPath(configElementRPCPath(path)).
				WithBadAttribute(attr.Name.Local)
		}
		err := ErrUnknownAttribute(configElementRPCPath(path), attr.Name.Local)
		if attr.Name.Space != "" {
			err = err.WithBadNamespace(attr.Name.Space)
		}
		return err
	}
	return nil
}

func isNamespaceDeclarationAttribute(attr xml.Attr) bool {
	return attr.Name.Space == "xmlns" || (attr.Name.Space == "" && attr.Name.Local == "xmlns")
}

func isAllowedConfigNamespaceDeclaration(namespace string) bool {
	return namespace == "" ||
		namespace == NetconfBaseNS ||
		namespace == ArcaConfigNS ||
		namespace == IETFInterfacesNS ||
		namespace == IETFRoutingNS
}

func validateConfigElement(name xml.Name, path []string) error {
	key := strings.Join(path, "/")
	if _, ok := allowedConfigElementPaths[key]; !ok {
		return ErrUnsupportedConfigElement(name.Local)
	}
	if !isAllowedConfigNamespace(path, name.Space) {
		return NewRPCError(ErrorTypeProtocol, ErrorTagUnknownNamespace,
			fmt.Sprintf("invalid namespace for config element %s", name.Local)).
			WithPath(configElementRPCPath(path)).
			WithBadNamespace(name.Space)
	}
	return nil
}

func isAllowedConfigNamespace(path []string, namespace string) bool {
	if namespace == "" || namespace == NetconfBaseNS {
		return true
	}
	if len(path) == 1 {
		return namespace == ArcaConfigNS || namespace == IETFInterfacesNS || namespace == IETFRoutingNS
	}
	switch path[1] {
	case "system", "chassis", "protocols", "routing-instances", "class-of-service", "security":
		return namespace == ArcaConfigNS
	case "interfaces":
		return namespace == IETFInterfacesNS
	case "routing":
		return namespace == IETFRoutingNS
	default:
		return false
	}
}

func configElementRPCPath(path []string) string {
	if len(path) <= 1 {
		return "/rpc/edit-config/config"
	}
	return "/rpc/edit-config/config/" + strings.Join(path[1:], "/")
}

// ApplyConfigEdit applies edit-config changes to existing config based on default-operation
// This implements Phase 2 Step 3 with merge/replace/create/delete operations
func ApplyConfigEdit(existing, edit *config.Config, defaultOp DefaultOperation) (*config.Config, error) {
	if existing == nil {
		return edit, nil
	}

	if edit == nil {
		return existing, nil
	}

	// Create a copy of existing to avoid mutating original
	merged := *existing
	if merged.Interfaces == nil {
		merged.Interfaces = make(map[string]*config.Interface)
	}

	// Apply default operation
	switch defaultOp {
	case DefaultOpMerge:
		// Merge: Add or update elements
		return mergeConfigs(&merged, edit)

	case DefaultOpReplace:
		// Replace: Replace entire subtrees
		return replaceConfigs(&merged, edit)

	case DefaultOpNone:
		// None: Only explicit operations allowed (not implemented in Phase 2)
		return &merged, nil

	default:
		return nil, NewRPCError(ErrorTypeProtocol, ErrorTagOperationNotSupported,
			fmt.Sprintf("unsupported default-operation: %s", defaultOp)).
			WithPath("/rpc/edit-config/default-operation").
			WithBadElement(string(defaultOp))
	}
}

// mergeConfigs merges edit into existing
func mergeConfigs(existing, edit *config.Config) (*config.Config, error) {
	// Merge system
	if edit.System != nil {
		if existing.System == nil {
			existing.System = &config.SystemConfig{}
		}
		if edit.System.HostName != "" {
			existing.System.HostName = edit.System.HostName
		}
		if edit.System.Services != nil {
			mergeSystemServices(existing.System, edit.System.Services)
		}
	}

	// Merge chassis
	if edit.Chassis != nil {
		existing.Chassis = edit.Chassis
	}

	// Merge interfaces
	if edit.Interfaces != nil {
		if existing.Interfaces == nil {
			existing.Interfaces = make(map[string]*config.Interface)
		}
		for name, editIface := range edit.Interfaces {
			if existing.Interfaces[name] == nil {
				existing.Interfaces[name] = &config.Interface{
					Units: make(map[int]*config.Unit),
				}
			}
			existingIface := existing.Interfaces[name]

			if editIface.Description != "" {
				existingIface.Description = editIface.Description
			}

			// Merge units
			if editIface.Units != nil {
				if existingIface.Units == nil {
					existingIface.Units = make(map[int]*config.Unit)
				}
				for unitNum, editUnit := range editIface.Units {
					if existingIface.Units[unitNum] == nil {
						existingIface.Units[unitNum] = &config.Unit{
							Family: make(map[string]*config.Family),
						}
					}
					existingUnit := existingIface.Units[unitNum]

					// Merge families
					if editUnit.Family != nil {
						if existingUnit.Family == nil {
							existingUnit.Family = make(map[string]*config.Family)
						}
						for familyName, editFamily := range editUnit.Family {
							if existingUnit.Family[familyName] == nil {
								existingUnit.Family[familyName] = &config.Family{
									Addresses: make([]string, 0),
								}
							}
							existingFamily := existingUnit.Family[familyName]

							// Merge addresses (append unique)
							for _, addr := range editFamily.Addresses {
								if !contains(existingFamily.Addresses, addr) {
									existingFamily.Addresses = append(existingFamily.Addresses, addr)
								}
							}
						}
					}
				}
			}
		}
	}

	// Merge routing options
	if edit.RoutingOptions != nil {
		if existing.RoutingOptions == nil {
			existing.RoutingOptions = &config.RoutingOptions{}
		}
		if edit.RoutingOptions.RouterID != "" {
			existing.RoutingOptions.RouterID = edit.RoutingOptions.RouterID
		}
		if edit.RoutingOptions.AutonomousSystem != 0 {
			existing.RoutingOptions.AutonomousSystem = edit.RoutingOptions.AutonomousSystem
		}
		if len(edit.RoutingOptions.StaticRoutes) > 0 {
			// Merge static routes
			existing.RoutingOptions.StaticRoutes = append(
				existing.RoutingOptions.StaticRoutes,
				edit.RoutingOptions.StaticRoutes...)
		}
	}

	// Merge routing instances
	if edit.RoutingInstances != nil {
		if existing.RoutingInstances == nil {
			existing.RoutingInstances = make(map[string]*config.RoutingInstance)
		}
		for name, instance := range edit.RoutingInstances {
			existing.RoutingInstances[name] = instance
		}
	}

	// Merge protocols
	if edit.Protocols != nil {
		if existing.Protocols == nil {
			existing.Protocols = &config.ProtocolConfig{}
		}

		if edit.Protocols.BFD != nil {
			if existing.Protocols.BFD == nil {
				existing.Protocols.BFD = &config.BFDConfig{
					Profiles: make(map[string]*config.BFDProfile),
					Peers:    make(map[string]*config.BFDPeer),
				}
			}
			if existing.Protocols.BFD.Profiles == nil {
				existing.Protocols.BFD.Profiles = make(map[string]*config.BFDProfile)
			}
			if existing.Protocols.BFD.Peers == nil {
				existing.Protocols.BFD.Peers = make(map[string]*config.BFDPeer)
			}
			for name, profile := range edit.Protocols.BFD.Profiles {
				existing.Protocols.BFD.Profiles[name] = profile
			}
			for address, peer := range edit.Protocols.BFD.Peers {
				existing.Protocols.BFD.Peers[address] = peer
			}
		}

		// Merge BGP
		if edit.Protocols.BGP != nil {
			if existing.Protocols.BGP == nil {
				existing.Protocols.BGP = &config.BGPConfig{
					Groups: make(map[string]*config.BGPGroup),
				}
			}
			if existing.Protocols.BGP.Groups == nil {
				existing.Protocols.BGP.Groups = make(map[string]*config.BGPGroup)
			}
			for groupName, editGroup := range edit.Protocols.BGP.Groups {
				existing.Protocols.BGP.Groups[groupName] = editGroup
			}
		}

		// Merge OSPF
		if edit.Protocols.OSPF != nil {
			mergeOSPFConfig(&existing.Protocols.OSPF, edit.Protocols.OSPF)
		}
		if edit.Protocols.OSPF3 != nil {
			mergeOSPFConfig(&existing.Protocols.OSPF3, edit.Protocols.OSPF3)
		}

		if edit.Protocols.MPLS != nil {
			if existing.Protocols.MPLS == nil {
				existing.Protocols.MPLS = &config.MPLSConfig{}
			}
			for _, iface := range edit.Protocols.MPLS.Interfaces {
				if !contains(existing.Protocols.MPLS.Interfaces, iface) {
					existing.Protocols.MPLS.Interfaces = append(existing.Protocols.MPLS.Interfaces, iface)
				}
			}
		}

		if edit.Protocols.VRRP != nil {
			if existing.Protocols.VRRP == nil {
				existing.Protocols.VRRP = &config.VRRPConfig{
					Groups: make(map[string]*config.VRRPGroup),
				}
			}
			if existing.Protocols.VRRP.Groups == nil {
				existing.Protocols.VRRP.Groups = make(map[string]*config.VRRPGroup)
			}
			for name, group := range edit.Protocols.VRRP.Groups {
				existing.Protocols.VRRP.Groups[name] = group
			}
		}
	}

	// Merge class of service
	if edit.ClassOfService != nil {
		if existing.ClassOfService == nil {
			existing.ClassOfService = &config.ClassOfServiceConfig{}
		}
		if len(edit.ClassOfService.ForwardingClasses) > 0 {
			if existing.ClassOfService.ForwardingClasses == nil {
				existing.ClassOfService.ForwardingClasses = make(map[string]*config.ForwardingClass)
			}
			for name, fc := range edit.ClassOfService.ForwardingClasses {
				existing.ClassOfService.ForwardingClasses[name] = fc
			}
		}
		if len(edit.ClassOfService.TrafficControlProfiles) > 0 {
			if existing.ClassOfService.TrafficControlProfiles == nil {
				existing.ClassOfService.TrafficControlProfiles = make(map[string]*config.TrafficControlProfile)
			}
			for name, profile := range edit.ClassOfService.TrafficControlProfiles {
				existing.ClassOfService.TrafficControlProfiles[name] = profile
			}
		}
		if len(edit.ClassOfService.Interfaces) > 0 {
			if existing.ClassOfService.Interfaces == nil {
				existing.ClassOfService.Interfaces = make(map[string]*config.CoSInterface)
			}
			for name, iface := range edit.ClassOfService.Interfaces {
				existing.ClassOfService.Interfaces[name] = iface
			}
		}
	}

	// Merge security
	if edit.Security != nil {
		if existing.Security == nil {
			existing.Security = &config.SecurityConfig{}
		}
		if edit.Security.NETCONF != nil {
			existing.Security.NETCONF = edit.Security.NETCONF
		}
		if edit.Security.RateLimit != nil {
			existing.Security.RateLimit = edit.Security.RateLimit
		}
	}

	return existing, nil
}

func mergeSystemServices(system *config.SystemConfig, editServices *config.SystemServicesConfig) {
	if system.Services == nil {
		system.Services = &config.SystemServicesConfig{}
	}
	if editServices.WebUI != nil {
		system.Services.WebUI = editServices.WebUI
	}
	if editServices.Prometheus != nil {
		system.Services.Prometheus = editServices.Prometheus
	}
	if editServices.SNMP != nil {
		system.Services.SNMP = editServices.SNMP
	}
}

func mergeOSPFConfig(existing **config.OSPFConfig, edit *config.OSPFConfig) {
	if edit == nil {
		return
	}
	if *existing == nil {
		*existing = &config.OSPFConfig{
			Areas: make(map[string]*config.OSPFArea),
		}
	}
	if edit.RouterID != "" {
		(*existing).RouterID = edit.RouterID
	}
	if (*existing).Areas == nil {
		(*existing).Areas = make(map[string]*config.OSPFArea)
	}
	for areaName, editArea := range edit.Areas {
		(*existing).Areas[areaName] = editArea
	}
}

// replaceConfigs replaces existing config subtrees with edit
func replaceConfigs(existing, edit *config.Config) (*config.Config, error) {
	// Replace entire subtrees
	if edit.System != nil {
		existing.System = edit.System
	}
	if edit.Interfaces != nil {
		existing.Interfaces = edit.Interfaces
	}
	if edit.Chassis != nil {
		existing.Chassis = edit.Chassis
	}
	if edit.RoutingOptions != nil {
		existing.RoutingOptions = edit.RoutingOptions
	}
	if edit.RoutingInstances != nil {
		existing.RoutingInstances = edit.RoutingInstances
	}
	if edit.Protocols != nil {
		existing.Protocols = edit.Protocols
	}
	if edit.ClassOfService != nil {
		existing.ClassOfService = edit.ClassOfService
	}
	if edit.Security != nil {
		existing.Security = edit.Security
	}
	return existing, nil
}

// contains checks if slice contains string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// ValidateConfig performs depth and size validation per Phase 2 Step 3
func ValidateConfig(cfg *config.Config) error {
	if cfg == nil {
		return NewRPCError(ErrorTypeApplication, ErrorTagInvalidValue, "config is nil").
			WithPath("/rpc/edit-config/config")
	}

	// Calculate depth
	depth := calculateConfigDepth(cfg)
	if depth > MaxXMLDepth {
		return NewRPCError(ErrorTypeProtocol, ErrorTagInvalidValue,
			fmt.Sprintf("config exceeds maximum depth limit (%d)", MaxXMLDepth)).
			WithPath("/rpc/edit-config/config").
			WithAppTag("depth-limit")
	}

	// Count elements
	count := countConfigElements(cfg)
	if count > MaxXMLElements {
		return NewRPCError(ErrorTypeProtocol, ErrorTagInvalidValue,
			fmt.Sprintf("config exceeds maximum element limit (%d)", MaxXMLElements)).
			WithPath("/rpc/edit-config/config").
			WithAppTag("size-limit")
	}

	return nil
}

// calculateConfigDepth calculates maximum nesting depth of config
func calculateConfigDepth(cfg *config.Config) int {
	maxDepth := 0

	// System: depth 2 (config > system > hostname)
	if cfg.System != nil {
		maxDepth = max(maxDepth, 2)
		if cfg.System.Services != nil {
			maxDepth = max(maxDepth, 4)
		}
	}

	// Chassis: depth 5 (config > chassis > cluster > sync > etcd > endpoint)
	if cfg.Chassis != nil && cfg.Chassis.Cluster != nil {
		maxDepth = max(maxDepth, 5)
	}

	// Interfaces: depth 5 (config > interfaces > interface > unit > family > address)
	if cfg.Interfaces != nil {
		for _, iface := range cfg.Interfaces {
			if iface.Units != nil {
				maxDepth = max(maxDepth, 5)
				break
			}
		}
	}

	// Routing options: depth 4 (config > routing > static-routes > route)
	if cfg.RoutingOptions != nil && len(cfg.RoutingOptions.StaticRoutes) > 0 {
		maxDepth = max(maxDepth, 4)
	}

	if len(cfg.RoutingInstances) > 0 {
		maxDepth = max(maxDepth, 3)
	}

	// Protocols: depth 5 (config > protocols > bgp > group > neighbor)
	if cfg.Protocols != nil {
		if cfg.Protocols.BFD != nil && (len(cfg.Protocols.BFD.Profiles) > 0 || len(cfg.Protocols.BFD.Peers) > 0) {
			maxDepth = max(maxDepth, 4)
		}
		if cfg.Protocols.BGP != nil && len(cfg.Protocols.BGP.Groups) > 0 {
			maxDepth = max(maxDepth, 5)
		}
		if cfg.Protocols.OSPF != nil && len(cfg.Protocols.OSPF.Areas) > 0 {
			maxDepth = max(maxDepth, 5)
		}
		if cfg.Protocols.OSPF3 != nil && len(cfg.Protocols.OSPF3.Areas) > 0 {
			maxDepth = max(maxDepth, 5)
		}
		if cfg.Protocols.MPLS != nil && len(cfg.Protocols.MPLS.Interfaces) > 0 {
			maxDepth = max(maxDepth, 3)
		}
		if cfg.Protocols.VRRP != nil && len(cfg.Protocols.VRRP.Groups) > 0 {
			maxDepth = max(maxDepth, 4)
		}
	}

	if cfg.ClassOfService != nil {
		maxDepth = max(maxDepth, 4)
	}

	if cfg.Security != nil {
		maxDepth = max(maxDepth, 4)
	}

	return maxDepth
}

// countConfigElements counts total XML elements in config
func countConfigElements(cfg *config.Config) int {
	count := 1 // root <config>

	if cfg.System != nil {
		count += 2 // <system> + <hostname>
		if cfg.System.Services != nil {
			count++ // <services>
			if service := cfg.System.Services.WebUI; service != nil {
				count += serviceElementCount(service.Enabled, service.ListenAddress, service.Port, "")
			}
			if service := cfg.System.Services.Prometheus; service != nil {
				count += serviceElementCount(service.Enabled, service.ListenAddress, service.Port, "")
			}
			if service := cfg.System.Services.SNMP; service != nil {
				count += serviceElementCount(service.Enabled, service.ListenAddress, service.Port, service.Community)
			}
		}
	}

	if cfg.Chassis != nil && cfg.Chassis.Cluster != nil {
		count += 2 // <chassis> + <cluster>
		if cfg.Chassis.Cluster.Enabled {
			count++
		}
		for _, node := range cfg.Chassis.Cluster.Nodes {
			if node == nil {
				continue
			}
			count += 2 // <node> + <name>
			if node.Address != "" {
				count++
			}
			if node.Priority != 0 {
				count++
			}
		}
		if cfg.Chassis.Cluster.Sync != nil && cfg.Chassis.Cluster.Sync.Etcd != nil && len(cfg.Chassis.Cluster.Sync.Etcd.Endpoints) > 0 {
			count += 2 // <sync> + <etcd>
			count += len(cfg.Chassis.Cluster.Sync.Etcd.Endpoints)
		}
	}

	if cfg.Interfaces != nil {
		count++ // <interfaces>
		for _, iface := range cfg.Interfaces {
			count += 2 // <interface> + <name>
			if iface.Description != "" {
				count++ // <description>
			}
			if iface.Units != nil {
				for _, unit := range iface.Units {
					count += 2 // <unit> + <name>
					if unit.Family != nil {
						for _, family := range unit.Family {
							count += 2                     // <family> + <name>
							count += len(family.Addresses) // <address> elements
						}
					}
				}
			}
		}
	}

	if cfg.RoutingOptions != nil {
		count++ // <routing>
		if cfg.RoutingOptions.RouterID != "" {
			count++ // <router-id>
		}
		if cfg.RoutingOptions.AutonomousSystem != 0 {
			count++ // <autonomous-system>
		}
		if len(cfg.RoutingOptions.StaticRoutes) > 0 {
			count++ // <static-routes>
			for _, route := range cfg.RoutingOptions.StaticRoutes {
				count += 3 // <route> + <prefix> + <next-hop>
				if route.Distance > 0 {
					count++ // <distance>
				}
			}
		}
	}

	if len(cfg.RoutingInstances) > 0 {
		count++ // <routing-instances>
		for _, instance := range cfg.RoutingInstances {
			if instance == nil {
				continue
			}
			count += 2 // <instance> + <name>
			if instance.InstanceType != "" {
				count++
			}
			if instance.RouteDistinguisher != "" {
				count++
			}
			if instance.VRFTarget != "" {
				count++
			}
			count += len(instance.VRFTargetImport)
			count += len(instance.VRFTargetExport)
			count += len(instance.VRFImport)
			count += len(instance.VRFExport)
			count += len(instance.Interfaces)
		}
	}

	if cfg.Protocols != nil {
		count++ // <protocols>
		if cfg.Protocols.BFD != nil {
			count++ // <bfd>
			for _, profile := range cfg.Protocols.BFD.Profiles {
				if profile == nil {
					continue
				}
				count += 2 // <profile> + <name>
				if profile.DetectMultiplier != 0 {
					count++
				}
				if profile.ReceiveInterval != 0 {
					count++
				}
				if profile.TransmitInterval != 0 {
					count++
				}
				if profile.EchoMode {
					count++
				}
				if profile.PassiveMode {
					count++
				}
			}
			for _, peer := range cfg.Protocols.BFD.Peers {
				if peer == nil {
					continue
				}
				count += 2 // <peer> + <address>
				if peer.LocalAddress != "" {
					count++
				}
				if peer.Interface != "" {
					count++
				}
				if peer.VRF != "" {
					count++
				}
				if peer.Multihop {
					count++
				}
				if peer.Profile != "" {
					count++
				}
				if peer.DetectMultiplier != 0 {
					count++
				}
				if peer.ReceiveInterval != 0 {
					count++
				}
				if peer.TransmitInterval != 0 {
					count++
				}
				if peer.EchoMode {
					count++
				}
				if peer.PassiveMode {
					count++
				}
				if peer.Shutdown {
					count++
				}
			}
		}
		if cfg.Protocols.BGP != nil {
			count++ // <bgp>
			for _, group := range cfg.Protocols.BGP.Groups {
				count += 2 // <group> + <name>
				if group.Type != "" {
					count++
				}
				if group.Import != "" {
					count++
				}
				if group.Export != "" {
					count++
				}
				for _, neighbor := range group.Neighbors {
					count += 3 // <neighbor> + <ip> + <peer-as>
					if neighbor.Description != "" {
						count++
					}
					if neighbor.LocalAddress != "" {
						count++
					}
					if neighbor.BFD || neighbor.BFDProfile != "" {
						count++
					}
					if neighbor.BFDProfile != "" {
						count++
					}
				}
			}
		}
		if cfg.Protocols.OSPF != nil {
			count++ // <ospf>
			if cfg.Protocols.OSPF.RouterID != "" {
				count++
			}
			for _, area := range cfg.Protocols.OSPF.Areas {
				count += 3 // <area> + <name> + <area-id>
				for _, ospfIface := range area.Interfaces {
					count += 2 // <interface> + <name>
					if ospfIface.Passive {
						count++
					}
					if ospfIface.Metric > 0 {
						count++
					}
					if ospfIface.PrioritySet || ospfIface.Priority > 0 {
						count++
					}
					if ospfIface.BFD || ospfIface.BFDProfile != "" {
						count++
					}
					if ospfIface.BFDProfile != "" {
						count++
					}
				}
			}
		}
		if cfg.Protocols.OSPF3 != nil {
			count++ // <ospf3>
			if cfg.Protocols.OSPF3.RouterID != "" {
				count++
			}
			for _, area := range cfg.Protocols.OSPF3.Areas {
				count += 3 // <area> + <name> + <area-id>
				for _, ospfIface := range area.Interfaces {
					count += 2 // <interface> + <name>
					if ospfIface.Passive {
						count++
					}
					if ospfIface.Metric > 0 {
						count++
					}
					if ospfIface.PrioritySet || ospfIface.Priority > 0 {
						count++
					}
					if ospfIface.BFD || ospfIface.BFDProfile != "" {
						count++
					}
					if ospfIface.BFDProfile != "" {
						count++
					}
				}
			}
		}
		if cfg.Protocols.MPLS != nil && len(cfg.Protocols.MPLS.Interfaces) > 0 {
			count++ // <mpls>
			count += len(cfg.Protocols.MPLS.Interfaces)
		}
		if cfg.Protocols.VRRP != nil && len(cfg.Protocols.VRRP.Groups) > 0 {
			count++ // <vrrp>
			for _, group := range cfg.Protocols.VRRP.Groups {
				if group == nil {
					continue
				}
				count += 2 // <group> + <name>
				if group.Interface != "" {
					count++
				}
				if group.VirtualAddress != "" {
					count++
				}
				if group.Priority != 0 {
					count++
				}
				if group.Preempt {
					count++
				}
			}
		}
	}

	if cfg.ClassOfService != nil {
		count++ // <class-of-service>
		if len(cfg.ClassOfService.ForwardingClasses) > 0 {
			count++ // <forwarding-classes>
			for _, fc := range cfg.ClassOfService.ForwardingClasses {
				if fc != nil {
					count += 3 // <forwarding-class> + <name> + <queue>
				}
			}
		}
		if len(cfg.ClassOfService.TrafficControlProfiles) > 0 {
			count++ // <traffic-control-profiles>
			for _, profile := range cfg.ClassOfService.TrafficControlProfiles {
				if profile == nil {
					continue
				}
				count += 2 // <traffic-control-profile> + <name>
				if profile.ShapingRate != 0 {
					count++
				}
				if profile.SchedulerMap != "" {
					count++
				}
			}
		}
		if len(cfg.ClassOfService.Interfaces) > 0 {
			count++ // <interfaces>
			for _, iface := range cfg.ClassOfService.Interfaces {
				if iface == nil {
					continue
				}
				count += 2 // <interface> + <name>
				if iface.OutputTrafficControlProfile != "" {
					count++
				}
			}
		}
	}

	if cfg.Security != nil {
		if (cfg.Security.NETCONF != nil && cfg.Security.NETCONF.SSH != nil && cfg.Security.NETCONF.SSH.Port != 0) || cfg.Security.RateLimit != nil {
			count++ // <security>
		}
		if cfg.Security.NETCONF != nil && cfg.Security.NETCONF.SSH != nil && cfg.Security.NETCONF.SSH.Port != 0 {
			count += 3 // <netconf> + <ssh> + <port>
		}
		if cfg.Security.RateLimit != nil {
			count++ // <rate-limit>
			if cfg.Security.RateLimit.PerIP != 0 {
				count++
			}
			if cfg.Security.RateLimit.PerUser != 0 {
				count++
			}
		}
	}

	return count
}

func serviceElementCount(enabled bool, listenAddress string, port int, community string) int {
	if !enabled && listenAddress == "" && port == 0 && community == "" {
		return 0
	}
	count := 1
	if enabled {
		count++
	}
	if listenAddress != "" {
		count++
	}
	if port != 0 {
		count++
	}
	if community != "" {
		count++
	}
	return count
}

// ValidateXMLSecurity performs token-based DTD/ENTITY detection per Phase 2 Step 2
func ValidateXMLSecurity(data []byte) error {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = true
	decoder.Entity = nil

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return NewRPCError(ErrorTypeRPC, ErrorTagMalformedMessage,
				fmt.Sprintf("invalid XML: %v", err)).
				WithPath("/rpc")
		}

		switch t := token.(type) {
		case xml.Directive:
			// Reject DOCTYPE, ENTITY directives (case-insensitive)
			directive := strings.ToUpper(string(t))
			if strings.HasPrefix(directive, "DOCTYPE") {
				return NewRPCError(ErrorTypeRPC, ErrorTagMalformedMessage,
					"DTD declarations are not allowed").
					WithPath("/rpc").
					WithBadElement("DOCTYPE")
			}
			if strings.HasPrefix(directive, "ENTITY") {
				return NewRPCError(ErrorTypeRPC, ErrorTagMalformedMessage,
					"ENTITY declarations are not allowed").
					WithPath("/rpc").
					WithBadElement("ENTITY")
			}
		}
	}

	return nil
}

// ValidateFilterDepthAndSize validates filter depth and size per Phase 2 Step 3
func ValidateFilterDepthAndSize(rpcName string, filter *Filter) error {
	if filter == nil || len(filter.Content) == 0 {
		return nil
	}

	// Calculate depth by counting nested elements
	depth := calculateFilterDepth(filter.Content)
	if depth > MaxXMLDepth {
		return NewRPCError(ErrorTypeProtocol, ErrorTagInvalidValue,
			fmt.Sprintf("filter exceeds maximum depth limit (%d)", MaxXMLDepth)).
			WithPath(fmt.Sprintf("/rpc/%s/filter", rpcName)).
			WithAppTag("depth-limit")
	}

	// Count elements
	count := countFilterElements(filter.Content)
	if count > MaxXMLElements {
		return NewRPCError(ErrorTypeProtocol, ErrorTagInvalidValue,
			fmt.Sprintf("filter exceeds maximum element limit (%d)", MaxXMLElements)).
			WithPath(fmt.Sprintf("/rpc/%s/filter", rpcName)).
			WithAppTag("size-limit")
	}

	return nil
}

// calculateFilterDepth calculates nesting depth of filter XML
func calculateFilterDepth(content []byte) int {
	depth := 0
	maxDepth := 0

	for i := 0; i < len(content); i++ {
		if content[i] == '<' {
			if i+1 < len(content) && content[i+1] != '/' && content[i+1] != '?' && content[i+1] != '!' {
				depth++
				if depth > maxDepth {
					maxDepth = depth
				}
			} else if i+1 < len(content) && content[i+1] == '/' {
				depth--
			}
		}
	}

	return maxDepth
}

// countFilterElements counts XML elements in filter
func countFilterElements(content []byte) int {
	count := 0

	for i := 0; i < len(content); i++ {
		if content[i] == '<' {
			if i+1 < len(content) && content[i+1] != '/' && content[i+1] != '?' && content[i+1] != '!' {
				count++
			}
		}
	}

	return count
}

// max returns the maximum of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ValidateProtocolNamespace validates protocol element namespace per Phase 2 Step 2
func ValidateProtocolNamespace(elem xml.Name) error {
	if elem.Space != NetconfBaseNS {
		return NewRPCError(ErrorTypeProtocol, ErrorTagUnknownNamespace,
			"invalid namespace for protocol element").
			WithPath("/rpc/" + elem.Local).
			WithBadNamespace(elem.Space)
	}
	return nil
}
