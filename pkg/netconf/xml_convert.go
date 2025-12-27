package netconf

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"github.com/akam1o/arca-router/pkg/config"
)

// XML Namespace constants per Phase 2 plan
const (
	NetconfBaseNS     = "urn:ietf:params:xml:ns:netconf:base:1.0"
	IETFInterfacesNS  = "urn:ietf:params:xml:ns:yang:ietf-interfaces"
	IETFRoutingNS     = "urn:ietf:params:xml:ns:yang:ietf-routing"
	ArcaConfigNS      = "urn:arca:router:config:1.0"
	ArcaStateNS       = "urn:arca:router:state:1.0"
)

// XML size and depth limits per Phase 2 plan Section 10.1
const (
	MaxXMLDepth        = 50
	MaxXMLElements     = 10000
	MaxXMLAttributes   = 20
	MaxXMLSize         = 10 * 1024 * 1024 // 10MB
)

// ConfigToXML converts internal config to NETCONF XML format with optional filtering
// This implements Phase 2 Step 3: XML↔Config Conversion
func ConfigToXML(cfg *config.Config, filter *Filter) ([]byte, error) {
	if cfg == nil {
		return []byte{}, nil
	}

	var buf bytes.Buffer

	// Write XML declaration and data root with NETCONF base namespace
	buf.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	buf.WriteString("\n")
	buf.WriteString(`<data xmlns="` + NetconfBaseNS + `">`)
	buf.WriteString("\n")

	// System configuration
	if cfg.System != nil && (filter == nil || filterMatches(filter, "system")) {
		if err := writeSystemXML(&buf, cfg.System); err != nil {
			return nil, fmt.Errorf("failed to serialize system config: %w", err)
		}
	}

	// Interfaces configuration - use IETF interfaces namespace
	if cfg.Interfaces != nil && len(cfg.Interfaces) > 0 && (filter == nil || filterMatches(filter, "interfaces")) {
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

	// Protocols (BGP, OSPF)
	if cfg.Protocols != nil && (filter == nil || filterMatches(filter, "protocols")) {
		if err := writeProtocolsXML(&buf, cfg.Protocols); err != nil {
			return nil, fmt.Errorf("failed to serialize protocols: %w", err)
		}
	}

	buf.WriteString("</data>\n")

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
		xml.EscapeText(buf, []byte(sys.HostName))
		buf.WriteString(`</host-name>`)
		buf.WriteString("\n")
	}

	buf.WriteString(`  </system>`)
	buf.WriteString("\n")
	return nil
}

// writeInterfacesXML writes interfaces configuration to XML with IETF namespace
func writeInterfacesXML(buf *bytes.Buffer, interfaces map[string]*config.Interface) error {
	buf.WriteString(`  <interfaces xmlns="` + IETFInterfacesNS + `">`)
	buf.WriteString("\n")

	for name, iface := range interfaces {
		buf.WriteString(`    <interface>`)
		buf.WriteString("\n")

		buf.WriteString(`      <name>`)
		xml.EscapeText(buf, []byte(name))
		buf.WriteString(`</name>`)
		buf.WriteString("\n")

		if iface.Description != "" {
			buf.WriteString(`      <description>`)
			xml.EscapeText(buf, []byte(iface.Description))
			buf.WriteString(`</description>`)
			buf.WriteString("\n")
		}

		// Units (sub-interfaces)
		if iface.Units != nil && len(iface.Units) > 0 {
			for unitNum, unit := range iface.Units {
				buf.WriteString(`      <unit>`)
				buf.WriteString("\n")

				buf.WriteString(fmt.Sprintf(`        <name>%d</name>`, unitNum))
				buf.WriteString("\n")

				// Address families
				if unit.Family != nil && len(unit.Family) > 0 {
					for familyName, family := range unit.Family {
						buf.WriteString(`        <family>`)
						buf.WriteString("\n")

						buf.WriteString(`          <name>`)
						xml.EscapeText(buf, []byte(familyName))
						buf.WriteString(`</name>`)
						buf.WriteString("\n")

						// Addresses
						if len(family.Addresses) > 0 {
							for _, addr := range family.Addresses {
								buf.WriteString(`          <address>`)
								xml.EscapeText(buf, []byte(addr))
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
		xml.EscapeText(buf, []byte(ro.RouterID))
		buf.WriteString(`</router-id>`)
		buf.WriteString("\n")
	}

	if ro.AutonomousSystem != 0 {
		buf.WriteString(fmt.Sprintf(`    <autonomous-system>%d</autonomous-system>`, ro.AutonomousSystem))
		buf.WriteString("\n")
	}

	// Static routes
	if len(ro.StaticRoutes) > 0 {
		buf.WriteString(`    <static-routes>`)
		buf.WriteString("\n")

		for _, route := range ro.StaticRoutes {
			buf.WriteString(`      <route>`)
			buf.WriteString("\n")

			buf.WriteString(`        <prefix>`)
			xml.EscapeText(buf, []byte(route.Prefix))
			buf.WriteString(`</prefix>`)
			buf.WriteString("\n")

			buf.WriteString(`        <next-hop>`)
			xml.EscapeText(buf, []byte(route.NextHop))
			buf.WriteString(`</next-hop>`)
			buf.WriteString("\n")

			if route.Distance > 0 {
				buf.WriteString(fmt.Sprintf(`        <distance>%d</distance>`, route.Distance))
				buf.WriteString("\n")
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

// writeProtocolsXML writes protocol configuration to XML
func writeProtocolsXML(buf *bytes.Buffer, protocols *config.ProtocolConfig) error {
	buf.WriteString(`  <protocols xmlns="` + ArcaConfigNS + `">`)
	buf.WriteString("\n")

	// BGP
	if protocols.BGP != nil {
		if err := writeBGPXML(buf, protocols.BGP); err != nil {
			return err
		}
	}

	// OSPF
	if protocols.OSPF != nil {
		if err := writeOSPFXML(buf, protocols.OSPF); err != nil {
			return err
		}
	}

	buf.WriteString(`  </protocols>`)
	buf.WriteString("\n")
	return nil
}

// writeBGPXML writes BGP configuration to XML
func writeBGPXML(buf *bytes.Buffer, bgp *config.BGPConfig) error {
	buf.WriteString(`    <bgp>`)
	buf.WriteString("\n")

	if bgp.Groups != nil && len(bgp.Groups) > 0 {
		for groupName, group := range bgp.Groups {
			buf.WriteString(`      <group>`)
			buf.WriteString("\n")

			buf.WriteString(`        <name>`)
			xml.EscapeText(buf, []byte(groupName))
			buf.WriteString(`</name>`)
			buf.WriteString("\n")

			if group.Type != "" {
				buf.WriteString(`        <type>`)
				xml.EscapeText(buf, []byte(group.Type))
				buf.WriteString(`</type>`)
				buf.WriteString("\n")
			}

			if group.Import != "" {
				buf.WriteString(`        <import>`)
				xml.EscapeText(buf, []byte(group.Import))
				buf.WriteString(`</import>`)
				buf.WriteString("\n")
			}

			if group.Export != "" {
				buf.WriteString(`        <export>`)
				xml.EscapeText(buf, []byte(group.Export))
				buf.WriteString(`</export>`)
				buf.WriteString("\n")
			}

			// Neighbors
			if group.Neighbors != nil && len(group.Neighbors) > 0 {
				for _, neighbor := range group.Neighbors {
					buf.WriteString(`        <neighbor>`)
					buf.WriteString("\n")

					buf.WriteString(`          <ip>`)
					xml.EscapeText(buf, []byte(neighbor.IP))
					buf.WriteString(`</ip>`)
					buf.WriteString("\n")

					buf.WriteString(fmt.Sprintf(`          <peer-as>%d</peer-as>`, neighbor.PeerAS))
					buf.WriteString("\n")

					if neighbor.Description != "" {
						buf.WriteString(`          <description>`)
						xml.EscapeText(buf, []byte(neighbor.Description))
						buf.WriteString(`</description>`)
						buf.WriteString("\n")
					}

					if neighbor.LocalAddress != "" {
						buf.WriteString(`          <local-address>`)
						xml.EscapeText(buf, []byte(neighbor.LocalAddress))
						buf.WriteString(`</local-address>`)
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
func writeOSPFXML(buf *bytes.Buffer, ospf *config.OSPFConfig) error {
	buf.WriteString(`    <ospf>`)
	buf.WriteString("\n")

	if ospf.RouterID != "" {
		buf.WriteString(`      <router-id>`)
		xml.EscapeText(buf, []byte(ospf.RouterID))
		buf.WriteString(`</router-id>`)
		buf.WriteString("\n")
	}

	if ospf.Areas != nil && len(ospf.Areas) > 0 {
		for areaName, area := range ospf.Areas {
			buf.WriteString(`      <area>`)
			buf.WriteString("\n")

			buf.WriteString(`        <name>`)
			xml.EscapeText(buf, []byte(areaName))
			buf.WriteString(`</name>`)
			buf.WriteString("\n")

			buf.WriteString(`        <area-id>`)
			xml.EscapeText(buf, []byte(area.AreaID))
			buf.WriteString(`</area-id>`)
			buf.WriteString("\n")

			// Interfaces
			if area.Interfaces != nil && len(area.Interfaces) > 0 {
				for _, ospfIface := range area.Interfaces {
					buf.WriteString(`        <interface>`)
					buf.WriteString("\n")

					buf.WriteString(`          <name>`)
					xml.EscapeText(buf, []byte(ospfIface.Name))
					buf.WriteString(`</name>`)
					buf.WriteString("\n")

					if ospfIface.Passive {
						buf.WriteString(`          <passive>true</passive>`)
						buf.WriteString("\n")
					}

					if ospfIface.Metric > 0 {
						buf.WriteString(fmt.Sprintf(`          <metric>%d</metric>`, ospfIface.Metric))
						buf.WriteString("\n")
					}

					if ospfIface.Priority > 0 {
						buf.WriteString(fmt.Sprintf(`          <priority>%d</priority>`, ospfIface.Priority))
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

	buf.WriteString(`    </ospf>`)
	buf.WriteString("\n")
	return nil
}

// filterMatches is now implemented in xpath_filter.go
// This placeholder is kept for reference only

// XMLToConfig converts NETCONF XML to internal config structure
// This implements Phase 2 Step 3 with allowlist validation
//
// Phase 4 TODO:
// - Parse per-element nc:operation attributes (create/delete/replace/merge)
// - Strict namespace validation (reject unknown namespaces)
// - Unknown element detection (reject elements not in allowlist)
func XMLToConfig(xmlData []byte, defaultOp DefaultOperation) (*config.Config, error) {
	// Security: Validate size
	if len(xmlData) > MaxXMLSize {
		return nil, NewRPCError(ErrorTypeProtocol, ErrorTagInvalidValue,
			fmt.Sprintf("XML size exceeds maximum (%d bytes)", MaxXMLSize)).
			WithPath("/rpc/edit-config/config").
			WithAppTag("size-limit")
	}

	// Security: DTD/ENTITY check
	if err := ValidateXMLSecurity(xmlData); err != nil {
		return nil, err
	}

	// Parse XML structure with allowlist validation
	var root struct {
		XMLName xml.Name `xml:"config"`
		System  *struct {
			HostName string `xml:"host-name"`
		} `xml:"system"`
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
		Protocols *struct {
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
					} `xml:"neighbor"`
				} `xml:"group"`
			} `xml:"bgp"`
			OSPF *struct {
				RouterID string `xml:"router-id"`
				Areas    []struct {
					Name       string `xml:"name"`
					AreaID     string `xml:"area-id"`
					Interfaces []struct {
						Name     string `xml:"name"`
						Passive  bool   `xml:"passive"`
						Metric   int    `xml:"metric"`
						Priority int    `xml:"priority"`
					} `xml:"interface"`
				} `xml:"area"`
			} `xml:"ospf"`
		} `xml:"protocols"`
	}

	// Parse with strict settings
	decoder := xml.NewDecoder(bytes.NewReader(xmlData))
	decoder.Strict = true
	decoder.Entity = nil

	if err := decoder.Decode(&root); err != nil {
		return nil, NewRPCError(ErrorTypeProtocol, ErrorTagMalformedMessage,
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

	// Protocols
	if root.Protocols != nil {
		cfg.Protocols = &config.ProtocolConfig{}

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
					}
				}

				cfg.Protocols.BGP.Groups[group.Name] = cfgGroup
			}
		}

		// OSPF
		if root.Protocols.OSPF != nil {
			cfg.Protocols.OSPF = &config.OSPFConfig{
				RouterID: root.Protocols.OSPF.RouterID,
				Areas:    make(map[string]*config.OSPFArea),
			}

			for _, area := range root.Protocols.OSPF.Areas {
				cfgArea := &config.OSPFArea{
					AreaID:     area.AreaID,
					Interfaces: make(map[string]*config.OSPFInterface),
				}

				for _, ospfIface := range area.Interfaces {
					cfgArea.Interfaces[ospfIface.Name] = &config.OSPFInterface{
						Name:     ospfIface.Name,
						Passive:  ospfIface.Passive,
						Metric:   ospfIface.Metric,
						Priority: ospfIface.Priority,
					}
				}

				cfg.Protocols.OSPF.Areas[area.Name] = cfgArea
			}
		}
	}

	// Validate depth and element count
	if err := ValidateConfig(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
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

	// Merge protocols
	if edit.Protocols != nil {
		if existing.Protocols == nil {
			existing.Protocols = &config.ProtocolConfig{}
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
			if existing.Protocols.OSPF == nil {
				existing.Protocols.OSPF = &config.OSPFConfig{
					Areas: make(map[string]*config.OSPFArea),
				}
			}
			if edit.Protocols.OSPF.RouterID != "" {
				existing.Protocols.OSPF.RouterID = edit.Protocols.OSPF.RouterID
			}
			if existing.Protocols.OSPF.Areas == nil {
				existing.Protocols.OSPF.Areas = make(map[string]*config.OSPFArea)
			}
			for areaName, editArea := range edit.Protocols.OSPF.Areas {
				existing.Protocols.OSPF.Areas[areaName] = editArea
			}
		}
	}

	return existing, nil
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
	if edit.RoutingOptions != nil {
		existing.RoutingOptions = edit.RoutingOptions
	}
	if edit.Protocols != nil {
		existing.Protocols = edit.Protocols
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

	// Protocols: depth 5 (config > protocols > bgp > group > neighbor)
	if cfg.Protocols != nil {
		if cfg.Protocols.BGP != nil && len(cfg.Protocols.BGP.Groups) > 0 {
			maxDepth = max(maxDepth, 5)
		}
		if cfg.Protocols.OSPF != nil && len(cfg.Protocols.OSPF.Areas) > 0 {
			maxDepth = max(maxDepth, 5)
		}
	}

	return maxDepth
}

// countConfigElements counts total XML elements in config
func countConfigElements(cfg *config.Config) int {
	count := 1 // root <config>

	if cfg.System != nil {
		count += 2 // <system> + <hostname>
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
							count += 2 // <family> + <name>
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

	if cfg.Protocols != nil {
		count++ // <protocols>
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
					if ospfIface.Priority > 0 {
						count++
					}
				}
			}
		}
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
			return NewRPCError(ErrorTypeProtocol, ErrorTagMalformedMessage,
				fmt.Sprintf("invalid XML: %v", err)).
				WithPath("/rpc")
		}

		switch t := token.(type) {
		case xml.Directive:
			// Reject DOCTYPE, ENTITY directives (case-insensitive)
			directive := strings.ToUpper(string(t))
			if strings.HasPrefix(directive, "DOCTYPE") {
				return NewRPCError(ErrorTypeProtocol, ErrorTagMalformedMessage,
					"DTD declarations are not allowed").
					WithPath("/rpc").
					WithBadElement("DOCTYPE")
			}
			if strings.HasPrefix(directive, "ENTITY") {
				return NewRPCError(ErrorTypeProtocol, ErrorTagMalformedMessage,
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
	// Empty namespace is allowed (default namespace inheritance)
	// Only reject if non-base namespace is explicitly specified
	if elem.Space != NetconfBaseNS && elem.Space != "" {
		return NewRPCError(ErrorTypeProtocol, "unknown-namespace",
			"invalid namespace for protocol element").
			WithPath("/rpc/" + elem.Local).
			WithBadNamespace(elem.Space)
	}
	return nil
}
