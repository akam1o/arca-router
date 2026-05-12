package netconf

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"log"
	"os"
	"runtime"
	"sort"
	"time"

	"github.com/akam1o/arca-router/pkg/config"
	"github.com/akam1o/arca-router/pkg/datastore"
)

// GetRequest represents <get> RPC for operational data
type GetRequest struct {
	XMLName xml.Name `xml:"get"`
	Filter  *Filter  `xml:"filter"`
}

func (r *GetRequest) SetInheritedNamespaceAttrs(attrs []xml.Attr) {
	if r.Filter != nil {
		r.Filter.InheritedAttrs = cloneXMLAttrs(attrs)
	}
}

// handleGet handles <get> RPC - retrieves operational data
func (s *Server) handleGet(ctx context.Context, sess *Session, rpc *RPC) *RPCReply {
	var req GetRequest
	if err := rpc.UnmarshalOperation(&req); err != nil {
		return NewErrorReply(rpc.MessageID, err.(*RPCError))
	}

	// Validate filter
	if err := req.Filter.Validate("get"); err != nil {
		return NewErrorReply(rpc.MessageID, err.(*RPCError))
	}

	// Validate filter depth and size limits
	if err := ValidateFilterDepthAndSize("get", req.Filter); err != nil {
		return NewErrorReply(rpc.MessageID, err.(*RPCError))
	}

	operationalData, err := s.getOperationalData(ctx, req.Filter)
	if err != nil {
		log.Printf("[NETCONF] Failed to get operational data: %v", err)
		if rpcErr, ok := err.(*RPCError); ok {
			return NewErrorReply(rpc.MessageID, rpcErr)
		}
		return NewErrorReply(rpc.MessageID, ErrOperationFailed(fmt.Sprintf("failed to retrieve operational data: %v", err)))
	}

	return NewDataReply(rpc.MessageID, operationalData)
}

func (s *Server) getOperationalData(ctx context.Context, filter *Filter) ([]byte, error) {
	cfg := config.NewConfig()
	if s != nil && s.datastore != nil {
		running, err := s.datastore.GetRunning(ctx)
		if err != nil {
			var dsErr *datastore.Error
			if !errors.As(err, &dsErr) || dsErr.Code != datastore.ErrCodeNotFound {
				return nil, err
			}
		} else if running != nil {
			cfg, err = TextToConfig(running.ConfigText)
			if err != nil {
				return nil, err
			}
		}
	}
	return buildOperationalData(cfg, filter, time.Now().UTC())
}

// GetOperationalData builds operational state without a datastore-backed
// server. It is kept for tests and callers that only need local system state.
func GetOperationalData(ctx context.Context, filter *Filter) ([]byte, error) {
	_ = ctx
	return buildOperationalData(config.NewConfig(), filter, time.Now().UTC())
}

// buildAllOperationalData builds operational data XML for the inside of <data>.
func buildAllOperationalData() string {
	data, err := buildOperationalData(config.NewConfig(), nil, time.Now().UTC())
	if err != nil {
		return ""
	}
	return string(data)
}

func buildOperationalData(cfg *config.Config, filter *Filter, now time.Time) ([]byte, error) {
	if cfg == nil {
		cfg = config.NewConfig()
	}

	var buf bytes.Buffer
	if includeOperationalSection(filter, "system") {
		if err := writeSystemStateXML(&buf, cfg, now); err != nil {
			return nil, err
		}
	}
	if includeOperationalSection(filter, "interfaces") && len(cfg.Interfaces) > 0 {
		if err := writeInterfaceStateXML(&buf, cfg.Interfaces); err != nil {
			return nil, err
		}
	}
	if includeOperationalSection(filter, "routing", "routing-state", "routing-protocols", "routes") && hasRoutingState(cfg) {
		if err := writeRoutingStateXML(&buf, cfg); err != nil {
			return nil, err
		}
	}

	if buf.Len() > MaxXMLSize {
		return nil, NewRPCError(ErrorTypeProtocol, ErrorTagInvalidValue,
			fmt.Sprintf("generated operational XML exceeds size limit (%d bytes)", MaxXMLSize)).
			WithPath("/rpc/get").
			WithAppTag("size-limit")
	}

	return buf.Bytes(), nil
}

func includeOperationalSection(filter *Filter, names ...string) bool {
	if filter == nil || len(bytes.TrimSpace(filter.Content)) == 0 {
		return true
	}
	for _, name := range names {
		if filterMatches(filter, name) {
			return true
		}
	}
	return false
}

func hasRoutingState(cfg *config.Config) bool {
	return cfg.RoutingOptions != nil || cfg.Protocols != nil
}

func writeSystemStateXML(buf *bytes.Buffer, cfg *config.Config, now time.Time) error {
	hostname := ""
	if cfg.System != nil {
		hostname = cfg.System.HostName
	}
	if hostname == "" {
		if osHostname, err := os.Hostname(); err == nil {
			hostname = osHostname
		}
	}

	buf.WriteString(`  <system xmlns="urn:ietf:params:xml:ns:yang:ietf-system">` + "\n")
	buf.WriteString("    <system-state>\n")
	if hostname != "" {
		if err := writeEscapedElement(buf, "      ", "hostname", hostname); err != nil {
			return err
		}
	}
	buf.WriteString("      <platform>\n")
	if err := writeEscapedElement(buf, "        ", "os-name", runtime.GOOS); err != nil {
		return err
	}
	if err := writeEscapedElement(buf, "        ", "machine", runtime.GOARCH); err != nil {
		return err
	}
	buf.WriteString("      </platform>\n")
	buf.WriteString("      <clock>\n")
	if err := writeEscapedElement(buf, "        ", "current-datetime", now.Format(time.RFC3339)); err != nil {
		return err
	}
	buf.WriteString("      </clock>\n")
	buf.WriteString("    </system-state>\n")
	buf.WriteString("  </system>\n")
	return nil
}

func writeInterfaceStateXML(buf *bytes.Buffer, interfaces map[string]*config.Interface) error {
	buf.WriteString(`  <interfaces xmlns="` + IETFInterfacesNS + `">` + "\n")
	for _, name := range sortedConfigKeys(interfaces) {
		iface := interfaces[name]
		if iface == nil {
			continue
		}
		buf.WriteString("    <interface>\n")
		if err := writeEscapedElement(buf, "      ", "name", name); err != nil {
			return err
		}
		if err := writeEscapedElement(buf, "      ", "admin-status", "configured"); err != nil {
			return err
		}
		if err := writeEscapedElement(buf, "      ", "oper-status", "unknown"); err != nil {
			return err
		}
		if len(iface.Units) > 0 {
			buf.WriteString("      <addresses>\n")
			for _, unitNum := range sortedUnitKeys(iface.Units) {
				unit := iface.Units[unitNum]
				if unit == nil {
					continue
				}
				for _, familyName := range sortedConfigKeys(unit.Family) {
					family := unit.Family[familyName]
					if family == nil {
						continue
					}
					for _, addr := range family.Addresses {
						buf.WriteString("        <address>\n")
						fmt.Fprintf(buf, "          <unit>%d</unit>\n", unitNum)
						if err := writeEscapedElement(buf, "          ", "family", familyName); err != nil {
							return err
						}
						if err := writeEscapedElement(buf, "          ", "ip", addr); err != nil {
							return err
						}
						buf.WriteString("        </address>\n")
					}
				}
			}
			buf.WriteString("      </addresses>\n")
		}
		buf.WriteString("    </interface>\n")
	}
	buf.WriteString("  </interfaces>\n")
	return nil
}

func writeRoutingStateXML(buf *bytes.Buffer, cfg *config.Config) error {
	buf.WriteString(`  <routing xmlns="` + IETFRoutingNS + `">` + "\n")
	buf.WriteString("    <routing-state>\n")
	if cfg.RoutingOptions != nil && len(cfg.RoutingOptions.StaticRoutes) > 0 {
		buf.WriteString("      <routes>\n")
		for _, route := range cfg.RoutingOptions.StaticRoutes {
			if route == nil {
				continue
			}
			buf.WriteString("        <route>\n")
			if err := writeEscapedElement(buf, "          ", "destination-prefix", route.Prefix); err != nil {
				return err
			}
			if err := writeEscapedElement(buf, "          ", "next-hop", route.NextHop); err != nil {
				return err
			}
			if err := writeEscapedElement(buf, "          ", "source-protocol", "static"); err != nil {
				return err
			}
			if route.Distance > 0 {
				fmt.Fprintf(buf, "          <metric>%d</metric>\n", route.Distance)
			}
			buf.WriteString("        </route>\n")
		}
		buf.WriteString("      </routes>\n")
	}
	if cfg.Protocols != nil {
		buf.WriteString("      <routing-protocols>\n")
		if cfg.Protocols.BGP != nil {
			name := "BGP"
			if cfg.RoutingOptions != nil && cfg.RoutingOptions.AutonomousSystem != 0 {
				name = fmt.Sprintf("BGP-%d", cfg.RoutingOptions.AutonomousSystem)
			}
			if err := writeRoutingProtocolXML(buf, "bgp", name); err != nil {
				return err
			}
		}
		if cfg.Protocols.OSPF != nil {
			if err := writeRoutingProtocolXML(buf, "ospf", "OSPF"); err != nil {
				return err
			}
		}
		buf.WriteString("      </routing-protocols>\n")
	}
	buf.WriteString("    </routing-state>\n")
	buf.WriteString("  </routing>\n")
	return nil
}

func writeRoutingProtocolXML(buf *bytes.Buffer, protocolType, name string) error {
	buf.WriteString("        <routing-protocol>\n")
	if err := writeEscapedElement(buf, "          ", "type", protocolType); err != nil {
		return err
	}
	if err := writeEscapedElement(buf, "          ", "name", name); err != nil {
		return err
	}
	if err := writeEscapedElement(buf, "          ", "admin-status", "configured"); err != nil {
		return err
	}
	buf.WriteString("        </routing-protocol>\n")
	return nil
}

func writeEscapedElement(buf *bytes.Buffer, indent, name, value string) error {
	fmt.Fprintf(buf, "%s<%s>", indent, name)
	if err := xml.EscapeText(buf, []byte(value)); err != nil {
		return err
	}
	fmt.Fprintf(buf, "</%s>\n", name)
	return nil
}

func sortedConfigKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedUnitKeys(m map[int]*config.Unit) []int {
	keys := make([]int, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Ints(keys)
	return keys
}
