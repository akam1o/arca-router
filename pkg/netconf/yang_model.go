package netconf

import (
	_ "embed"
	"encoding/xml"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/openconfig/goyang/pkg/yang"
)

// Embed YANG model file at compile time
//
//go:embed yang_model_data.yang
var arcaRouterYANG string

const ietfInterfacesYANG = `
module ietf-interfaces {
  namespace "urn:ietf:params:xml:ns:yang:ietf-interfaces";
  prefix if;

  container interfaces {
    list interface {
      key "name";
      leaf name {
        type string;
      }
      leaf description {
        type string;
      }
      leaf enabled {
        type boolean;
      }
      leaf admin-status {
        type string;
      }
      leaf oper-status {
        type string;
      }
      leaf phys-address {
        type string;
      }
      leaf qos-profile {
        type string;
      }
      leaf ipv4-table-id {
        type uint32;
      }
      leaf ipv6-table-id {
        type uint32;
      }
      container statistics {
        leaf rx-packets {
          type uint64;
        }
        leaf tx-packets {
          type uint64;
        }
        leaf rx-bytes {
          type uint64;
        }
        leaf tx-bytes {
          type uint64;
        }
        leaf rx-errors {
          type uint64;
        }
        leaf tx-errors {
          type uint64;
        }
        leaf drops {
          type uint64;
        }
      }
      container queue-placements {
        container rx-queues {
          list rx-queue {
            leaf queue-id {
              type uint32;
            }
            leaf worker-id {
              type uint32;
            }
            leaf mode {
              type string;
            }
          }
        }
        container tx-queues {
          list tx-queue {
            leaf queue-id {
              type uint32;
            }
            leaf shared {
              type boolean;
            }
            container threads {
              leaf-list thread {
                type uint32;
              }
            }
          }
        }
      }
      container addresses {
        list address {
          leaf unit {
            type uint32;
          }
          leaf family {
            type string;
          }
          leaf ip {
            type string;
          }
        }
      }
    }
  }
}
`

const ietfRoutingYANG = `
module ietf-routing {
  namespace "urn:ietf:params:xml:ns:yang:ietf-routing";
  prefix rt;

  container routing {
    leaf router-id {
      type string;
    }
    leaf autonomous-system {
      type uint32;
    }
    container static-routes {
      list route {
        leaf prefix {
          type string;
        }
        leaf next-hop {
          type string;
        }
        leaf distance {
          type uint8;
        }
        leaf bfd {
          type boolean;
        }
        leaf bfd-profile {
          type string;
        }
        leaf bfd-source {
          type string;
        }
        leaf bfd-multihop {
          type boolean;
        }
      }
    }
    container routing-state {
      container routes {
        list route {
          leaf destination-prefix {
            type string;
          }
          leaf next-hop {
            type string;
          }
          leaf source-protocol {
            type string;
          }
          leaf metric {
            type uint32;
          }
        }
      }
      container routing-protocols {
        list routing-protocol {
          leaf type {
            type string;
          }
          leaf name {
            type string;
          }
          leaf admin-status {
            type string;
          }
        }
      }
    }
  }
}
`

const ietfSystemYANG = `
module ietf-system {
  namespace "urn:ietf:params:xml:ns:yang:ietf-system";
  prefix sys;

  container system {
    container system-state {
      leaf hostname {
        type string;
      }
      container platform {
        leaf os-name {
          type string;
        }
        leaf machine {
          type string;
        }
      }
      container clock {
        leaf current-datetime {
          type string;
        }
      }
    }
  }
}
`

// YANGValidator provides YANG model validation capabilities
type YANGValidator struct {
	modules          *yang.Modules
	schemaPathSchema *yangPathNode
	mu               sync.RWMutex
}

type yangPathNode struct {
	children map[string]*yangPathNode
}

var (
	globalValidator     *YANGValidator
	globalValidatorOnce sync.Once
)

// GetGlobalValidator returns the singleton YANG validator instance
// This validator is initialized once and reused across the application
func GetGlobalValidator() (*YANGValidator, error) {
	var initErr error
	globalValidatorOnce.Do(func() {
		globalValidator, initErr = NewYANGValidator()
	})
	if initErr != nil {
		return nil, fmt.Errorf("failed to initialize global YANG validator: %w", initErr)
	}
	return globalValidator, nil
}

// NewYANGValidator creates a new YANG validator with arca-router and the local
// dependency stubs needed to resolve its IETF augment/import references.
func NewYANGValidator() (*YANGValidator, error) {
	ms := yang.NewModules()

	dependencies := []struct {
		name  string
		model string
	}{
		{name: "ietf-interfaces.yang", model: ietfInterfacesYANG},
		{name: "ietf-routing.yang", model: ietfRoutingYANG},
		{name: "ietf-system.yang", model: ietfSystemYANG},
	}
	for _, dependency := range dependencies {
		if err := ms.Parse(dependency.model, dependency.name); err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", dependency.name, err)
		}
	}

	// Parse the embedded arca-router.yang model
	if err := ms.Parse(arcaRouterYANG, "arca-router.yang"); err != nil {
		return nil, fmt.Errorf("failed to parse arca-router.yang: %w", err)
	}

	// Process imports and build the module tree
	if errs := ms.Process(); len(errs) > 0 {
		return nil, fmt.Errorf("YANG schema error: %v", errs[0])
	}

	schemaPaths, err := yangModuleElementPaths(ms, "arca-router", "ietf-interfaces", "ietf-routing", "ietf-system")
	if err != nil {
		return nil, fmt.Errorf("failed to build YANG path schema: %w", err)
	}
	schemaPaths = append(schemaPaths, netconfXMLCompatibilityYANGPaths...)

	return &YANGValidator{
		modules:          ms,
		schemaPathSchema: newYANGPathSchema(schemaPaths),
	}, nil
}

// ValidateConfig validates configuration XML against the implemented NETCONF
// schema subset and the internal semantic config rules.
func (v *YANGValidator) ValidateConfig(xmlData []byte) error {
	if v == nil || v.modules == nil {
		return fmt.Errorf("YANG validator not initialized")
	}

	v.mu.RLock()
	defer v.mu.RUnlock()

	cfg, err := XMLToConfig(xmlData, DefaultOpMerge)
	if err != nil {
		return err
	}
	if rpcErr := validateConfigSemantics("validate", cfg); rpcErr != nil {
		return rpcErr
	}
	return nil
}

// GetModel returns the parsed YANG module for programmatic access
func (v *YANGValidator) GetModel(moduleName string) (*yang.Module, error) {
	if v == nil || v.modules == nil {
		return nil, fmt.Errorf("YANG validator not initialized")
	}

	v.mu.RLock()
	defer v.mu.RUnlock()

	module := v.modules.Modules[moduleName]
	if module == nil {
		return nil, fmt.Errorf("module %q not found", moduleName)
	}

	return module, nil
}

// GetArcaRouterModel returns the main arca-router YANG module
func (v *YANGValidator) GetArcaRouterModel() (*yang.Module, error) {
	return v.GetModel("arca-router")
}

// ListModules returns the names of all loaded YANG modules
func (v *YANGValidator) ListModules() []string {
	if v == nil || v.modules == nil {
		return nil
	}

	v.mu.RLock()
	defer v.mu.RUnlock()

	names := make([]string, 0, len(v.modules.Modules))
	for name := range v.modules.Modules {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ValidateElementPath validates that an XPath-like element path is valid
// according to the implemented YANG path schema.
func (v *YANGValidator) ValidateElementPath(path string) error {
	return v.ValidateElementPathWithContext(path, nil)
}

// ValidateElementPathWithContext validates an XPath-like element path using
// explicit namespace declarations for prefixed path and predicate names.
func (v *YANGValidator) ValidateElementPathWithContext(path string, namespaceAttrs []xml.Attr) error {
	if v == nil || v.modules == nil {
		return fmt.Errorf("YANG validator not initialized")
	}

	xpathFilter, err := ParseXPathFilterWithContext(path, collectNamespaceAttrs(namespaceAttrs))
	if err != nil {
		return err
	}
	if xpathFilter == nil {
		return fmt.Errorf("path must include at least one element")
	}
	if err := validateXPathFilterNamespaces(xpathFilter); err != nil {
		return err
	}

	return v.validateXPathFilterPath(xpathFilter)
}

func (v *YANGValidator) validateXPathFilterPath(xpathFilter *XPathFilter) error {
	v.mu.RLock()
	defer v.mu.RUnlock()

	if err := implementedYANGPathSchema.validate(xpathFilter); err != nil {
		return err
	}
	if v.schemaPathSchema == nil {
		return nil
	}
	if err := v.schemaPathSchema.validate(xpathFilter); err != nil {
		return fmt.Errorf("implemented path is not backed by loaded YANG schema: %w", err)
	}
	return nil
}

var implementedYANGPathSchema = newYANGPathSchema(implementedYANGElementPaths())

var netconfXMLCompatibilityYANGPaths = []string{
	"interfaces/interface/unit/name",
	"interfaces/interface/unit/family/name",
	"interfaces/interface/unit/family/address",
	"protocols/ospf/area/name",
	"protocols/ospf3/area/name",
}

func yangModuleElementPaths(ms *yang.Modules, moduleNames ...string) ([]string, error) {
	if ms == nil {
		return nil, fmt.Errorf("YANG modules are nil")
	}
	seen := map[string]struct{}{}
	for _, moduleName := range moduleNames {
		entry, errs := ms.GetModule(moduleName)
		if len(errs) > 0 {
			return nil, errs[0]
		}
		if entry == nil {
			return nil, fmt.Errorf("module %q returned nil schema entry", moduleName)
		}
		collectYANGChildPaths(entry, nil, seen)
	}
	paths := make([]string, 0, len(seen))
	for path := range seen {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}

func collectYANGChildPaths(entry *yang.Entry, prefix []string, seen map[string]struct{}) {
	if entry == nil || len(entry.Dir) == 0 {
		return
	}
	names := make([]string, 0, len(entry.Dir))
	for name := range entry.Dir {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		collectYANGEntryPaths(entry.Dir[name], prefix, seen)
	}
}

func collectYANGEntryPaths(entry *yang.Entry, prefix []string, seen map[string]struct{}) {
	if entry == nil {
		return
	}
	current := prefix
	if !entry.IsChoice() && !entry.IsCase() {
		current = append(append([]string{}, prefix...), entry.Name)
		if path := strings.Join(current, "/"); path != "" {
			seen[path] = struct{}{}
		}
	}
	collectYANGChildPaths(entry, current, seen)
}

func implementedYANGElementPaths() []string {
	paths := make([]string, 0, len(allowedConfigElementPaths)+len(routingOptionsYANGAliasPaths)+len(operationalStateYANGPaths))
	for path := range allowedConfigElementPaths {
		path = strings.TrimPrefix(path, "config")
		path = strings.TrimPrefix(path, "/")
		if path != "" {
			paths = append(paths, path)
		}
	}
	paths = append(paths, routingOptionsYANGAliasPaths...)
	paths = append(paths, operationalStateYANGPaths...)
	return paths
}

var routingOptionsYANGAliasPaths = []string{
	"routing-options",
	"routing-options/router-id",
	"routing-options/autonomous-system",
	"routing-options/static",
	"routing-options/static/route",
	"routing-options/static/route/prefix",
	"routing-options/static/route/next-hop",
	"routing-options/static/route/distance",
	"routing-options/static/route/bfd",
	"routing-options/static/route/bfd-profile",
	"routing-options/static/route/bfd-source",
	"routing-options/static/route/bfd-multihop",
}

var operationalStateYANGPaths = []string{
	"system/system-state",
	"system/system-state/hostname",
	"system/system-state/platform",
	"system/system-state/platform/os-name",
	"system/system-state/platform/machine",
	"system/system-state/clock",
	"system/system-state/clock/current-datetime",
	"interfaces/interface/admin-status",
	"interfaces/interface/oper-status",
	"interfaces/interface/phys-address",
	"interfaces/interface/qos-profile",
	"interfaces/interface/ipv4-table-id",
	"interfaces/interface/ipv6-table-id",
	"interfaces/interface/statistics",
	"interfaces/interface/statistics/rx-packets",
	"interfaces/interface/statistics/tx-packets",
	"interfaces/interface/statistics/rx-bytes",
	"interfaces/interface/statistics/tx-bytes",
	"interfaces/interface/statistics/rx-errors",
	"interfaces/interface/statistics/tx-errors",
	"interfaces/interface/statistics/drops",
	"interfaces/interface/queue-placements",
	"interfaces/interface/queue-placements/rx-queues",
	"interfaces/interface/queue-placements/rx-queues/rx-queue",
	"interfaces/interface/queue-placements/rx-queues/rx-queue/queue-id",
	"interfaces/interface/queue-placements/rx-queues/rx-queue/worker-id",
	"interfaces/interface/queue-placements/rx-queues/rx-queue/mode",
	"interfaces/interface/queue-placements/tx-queues",
	"interfaces/interface/queue-placements/tx-queues/tx-queue",
	"interfaces/interface/queue-placements/tx-queues/tx-queue/queue-id",
	"interfaces/interface/queue-placements/tx-queues/tx-queue/shared",
	"interfaces/interface/queue-placements/tx-queues/tx-queue/threads",
	"interfaces/interface/queue-placements/tx-queues/tx-queue/threads/thread",
	"interfaces/interface/addresses",
	"interfaces/interface/addresses/address",
	"interfaces/interface/addresses/address/unit",
	"interfaces/interface/addresses/address/family",
	"interfaces/interface/addresses/address/ip",
	"routing/routing-state",
	"routing/routing-state/routes",
	"routing/routing-state/routes/route",
	"routing/routing-state/routes/route/destination-prefix",
	"routing/routing-state/routes/route/next-hop",
	"routing/routing-state/routes/route/source-protocol",
	"routing/routing-state/routes/route/metric",
	"routing/routing-state/routing-protocols",
	"routing/routing-state/routing-protocols/routing-protocol",
	"routing/routing-state/routing-protocols/routing-protocol/type",
	"routing/routing-state/routing-protocols/routing-protocol/name",
	"routing/routing-state/routing-protocols/routing-protocol/admin-status",
	"state",
	"state/interfaces",
	"state/routes",
	"state/routes/route",
	"state/routes/route/prefix",
	"state/routes/route/next-hop",
	"state/routes/route/protocol",
	"state/routes/route/metric",
	"state/routes/route/interface",
	"state/routes/route/active",
	"state/routing-instances",
	"state/routing-instances/instance",
	"state/routing-instances/instance/name",
	"state/routing-instances/instance/instance-type",
	"state/routing-instances/instance/route-distinguisher",
	"state/routing-instances/instance/ipv4-table-id",
	"state/routing-instances/instance/ipv6-table-id",
	"state/routing-instances/instance/import-target",
	"state/routing-instances/instance/export-target",
	"state/routing-instances/instance/import-policy",
	"state/routing-instances/instance/export-policy",
	"state/routing-instances/instance/interface",
	"state/protocols",
	"state/protocols/bgp",
	"state/protocols/bgp/neighbor",
	"state/protocols/bgp/neighbor/peer-address",
	"state/protocols/bgp/neighbor/peer-as",
	"state/protocols/bgp/neighbor/state",
	"state/protocols/bgp/neighbor/uptime-seconds",
	"state/protocols/bgp/neighbor/prefix-received",
	"state/protocols/bgp/neighbor/prefix-sent",
	"state/protocols/ospf",
	"state/protocols/ospf/neighbor",
	"state/protocols/ospf/neighbor/router-id",
	"state/protocols/ospf/neighbor/address",
	"state/protocols/ospf/neighbor/interface",
	"state/protocols/ospf/neighbor/state",
	"state/protocols/ospf/neighbor/role",
	"state/protocols/ospf/neighbor/priority",
	"state/protocols/ospf/neighbor/dead-time-seconds",
	"state/protocols/ospf/neighbor/uptime-seconds",
	"state/protocols/ospf3",
	"state/protocols/ospf3/neighbor",
	"state/protocols/ospf3/neighbor/router-id",
	"state/protocols/ospf3/neighbor/address",
	"state/protocols/ospf3/neighbor/interface",
	"state/protocols/ospf3/neighbor/state",
	"state/protocols/ospf3/neighbor/role",
	"state/protocols/ospf3/neighbor/priority",
	"state/protocols/ospf3/neighbor/dead-time-seconds",
	"state/protocols/ospf3/neighbor/uptime-seconds",
	"state/protocols/bfd",
	"state/protocols/bfd/last-run",
	"state/protocols/bfd/configured-peers",
	"state/protocols/bfd/observed-peers",
	"state/protocols/bfd/up-peers",
	"state/protocols/bfd/down-peers",
	"state/protocols/bfd/session-down-events",
	"state/protocols/bfd/rx-fail-packets",
	"state/protocols/bfd/peer",
	"state/protocols/bfd/peer/address",
	"state/protocols/bfd/peer/local-address",
	"state/protocols/bfd/peer/interface",
	"state/protocols/bfd/peer/vrf",
	"state/protocols/bfd/peer/status",
	"state/protocols/bfd/peer/diagnostic",
	"state/protocols/bfd/peer/remote-diagnostic",
	"state/protocols/bfd/peer/observed",
	"state/protocols/bfd/peer/up",
	"state/protocols/bfd/peer/session-down-events",
	"state/protocols/bfd/peer/rx-fail-packets",
	"state/protocols/bfd/issue",
	"state/protocols/bfd/last-error",
}

func newYANGPathSchema(paths []string) *yangPathNode {
	root := &yangPathNode{children: make(map[string]*yangPathNode)}
	for _, path := range paths {
		root.add(path)
	}
	return root
}

func (n *yangPathNode) add(path string) {
	path = strings.Trim(path, "/")
	if path == "" {
		return
	}
	current := n
	for _, segment := range strings.Split(path, "/") {
		if segment == "" {
			continue
		}
		if current.children == nil {
			current.children = make(map[string]*yangPathNode)
		}
		child := current.children[segment]
		if child == nil {
			child = &yangPathNode{children: make(map[string]*yangPathNode)}
			current.children[segment] = child
		}
		current = child
	}
}

func (n *yangPathNode) validate(filter *XPathFilter) error {
	if filter == nil || len(filter.Segments) == 0 {
		return fmt.Errorf("path must include at least one element")
	}

	current := n
	traversed := make([]string, 0, len(filter.Segments))
	for index, segment := range filter.Segments {
		child := current.children[segment]
		traversed = append(traversed, segment)
		if child == nil {
			return fmt.Errorf("unsupported element path: /%s", strings.Join(traversed, "/"))
		}
		if err := validateYANGPredicates(child, filter.Predicates[index], traversed); err != nil {
			return err
		}
		current = child
	}
	return nil
}

func validateYANGPredicates(node *yangPathNode, predicates map[string]string, traversed []string) error {
	for key := range predicates {
		if node.children[key] == nil {
			return fmt.Errorf("unsupported predicate %q for /%s", key, strings.Join(traversed, "/"))
		}
	}
	return nil
}

func validateXPathFilterNamespaces(filter *XPathFilter) error {
	if filter == nil {
		return nil
	}

	for index, namespace := range filter.SegmentNamespaces {
		if namespace == "" {
			continue
		}
		path := filter.Segments[:index+1]
		if !isAllowedXPathNamespace(path, namespace) {
			return fmt.Errorf("/%s uses namespace %q, want %s", strings.Join(path, "/"), namespace, expectedXPathNamespaceDescription(path))
		}
	}

	for index, predicates := range filter.PredicateNamespaces {
		path := filter.Segments[:index+1]
		for key, namespace := range predicates {
			if namespace == "" {
				continue
			}
			predicatePath := append(append([]string{}, path...), key)
			if !isAllowedXPathNamespace(predicatePath, namespace) {
				return fmt.Errorf("predicate %q for /%s uses namespace %q, want %s", key, strings.Join(path, "/"), namespace, expectedXPathNamespaceDescription(predicatePath))
			}
		}
	}

	return nil
}

func validateSubtreeFilterPaths(paths [][]subtreeFilterElement) error {
	for _, path := range paths {
		if len(path) == 0 {
			continue
		}
		segments := subtreeFilterPathSegments(path)
		if err := implementedYANGPathSchema.validate(&XPathFilter{
			Segments:   segments,
			Predicates: map[int]map[string]string{},
		}); err != nil {
			return err
		}
		if err := validateSubtreeFilterPathNamespaces(path, segments); err != nil {
			return err
		}
	}
	return nil
}

func subtreeFilterPathSegments(path []subtreeFilterElement) []string {
	segments := make([]string, 0, len(path))
	for _, element := range path {
		segments = append(segments, element.LocalName)
	}
	return segments
}

func validateSubtreeFilterPathNamespaces(path []subtreeFilterElement, segments []string) error {
	for index, element := range path {
		if element.Namespace == "" || element.Namespace == NetconfBaseNS {
			continue
		}
		currentPath := segments[:index+1]
		if !isAllowedXPathNamespace(currentPath, element.Namespace) {
			return fmt.Errorf("/%s uses namespace %q, want %s", strings.Join(currentPath, "/"), element.Namespace, expectedXPathNamespaceDescription(currentPath))
		}
	}
	return nil
}

func isAllowedXPathNamespace(path []string, namespace string) bool {
	if namespace == "" {
		return true
	}
	for _, allowed := range allowedXPathNamespaces(path) {
		if namespace == allowed {
			return true
		}
	}
	return false
}

func allowedXPathNamespaces(path []string) []string {
	if len(path) == 0 {
		return nil
	}
	switch path[0] {
	case "interfaces":
		return []string{IETFInterfacesNS}
	case "routing":
		return []string{IETFRoutingNS}
	case "system":
		if len(path) == 1 {
			return []string{ArcaConfigNS, IETFSystemNS}
		}
		if path[1] == "system-state" {
			return []string{IETFSystemNS}
		}
		return []string{ArcaConfigNS}
	default:
		return []string{ArcaConfigNS}
	}
}

func expectedXPathNamespaceDescription(path []string) string {
	allowed := allowedXPathNamespaces(path)
	if len(allowed) == 0 {
		return `""`
	}
	quoted := make([]string, 0, len(allowed))
	for _, namespace := range allowed {
		quoted = append(quoted, fmt.Sprintf("%q", namespace))
	}
	return strings.Join(quoted, " or ")
}
