package netconf

import (
	_ "embed"
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

// YANGValidator provides YANG model validation capabilities
type YANGValidator struct {
	modules *yang.Modules
	mu      sync.RWMutex
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

// NewYANGValidator creates a new YANG validator with the arca-router model loaded
// Phase 3 implementation: Parse validation only (full semantic validation in Phase 4)
func NewYANGValidator() (*YANGValidator, error) {
	ms := yang.NewModules()

	// Parse the embedded arca-router.yang model
	if err := ms.Parse(arcaRouterYANG, "arca-router.yang"); err != nil {
		return nil, fmt.Errorf("failed to parse arca-router.yang: %w", err)
	}

	// Process imports and build the module tree
	// Note: For Phase 3, we skip full semantic validation with external IETF models
	// This is a limitation accepted for the initial implementation
	if errs := ms.Process(); len(errs) > 0 {
		// Only tolerate "module not found" errors for IETF imports
		// All other errors (e.g., duplicate leafs, type mismatches) should fail
		hasNonIgnorableError := false
		for _, err := range errs {
			errStr := err.Error()
			// Allow missing IETF modules (Phase 4 dependency)
			// Check for "no such module" pattern which indicates missing dependency
			isModuleNotFound := strings.Contains(errStr, "no such module")
			isIETFModule := strings.Contains(errStr, "ietf-interfaces") || strings.Contains(errStr, "ietf-routing")

			if !isModuleNotFound || !isIETFModule {
				// Non-IETF errors or other types of errors are fatal
				hasNonIgnorableError = true
			}
		}
		if hasNonIgnorableError {
			// Return first non-ignorable error for clarity
			for _, err := range errs {
				errStr := err.Error()
				isModuleNotFound := strings.Contains(errStr, "no such module")
				isIETFModule := strings.Contains(errStr, "ietf-interfaces") || strings.Contains(errStr, "ietf-routing")
				if !isModuleNotFound || !isIETFModule {
					return nil, fmt.Errorf("YANG schema error: %v", err)
				}
			}
		}
	}

	return &YANGValidator{
		modules: ms,
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
// according to the YANG schema (Phase 3: basic implementation)
func (v *YANGValidator) ValidateElementPath(path string) error {
	if v == nil || v.modules == nil {
		return fmt.Errorf("YANG validator not initialized")
	}

	xpathFilter, err := ParseXPathFilter(path)
	if err != nil {
		return err
	}
	if xpathFilter == nil {
		return fmt.Errorf("path must include at least one element")
	}

	return v.validateXPathFilterPath(xpathFilter)
}

func (v *YANGValidator) validateXPathFilterPath(xpathFilter *XPathFilter) error {
	v.mu.RLock()
	defer v.mu.RUnlock()

	return implementedYANGPathSchema.validate(xpathFilter)
}

var implementedYANGPathSchema = newYANGPathSchema(implementedYANGElementPaths())

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
		if expected := expectedXPathNamespace(path); namespace != expected {
			return fmt.Errorf("/%s uses namespace %q, want %q", strings.Join(path, "/"), namespace, expected)
		}
	}

	for index, predicates := range filter.PredicateNamespaces {
		path := filter.Segments[:index+1]
		for key, namespace := range predicates {
			if namespace == "" {
				continue
			}
			predicatePath := append(append([]string{}, path...), key)
			if expected := expectedXPathNamespace(predicatePath); namespace != expected {
				return fmt.Errorf("predicate %q for /%s uses namespace %q, want %q", key, strings.Join(path, "/"), namespace, expected)
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
		if expected := expectedXPathNamespace(currentPath); element.Namespace != expected {
			return fmt.Errorf("/%s uses namespace %q, want %q", strings.Join(currentPath, "/"), element.Namespace, expected)
		}
	}
	return nil
}

func expectedXPathNamespace(path []string) string {
	if len(path) == 0 {
		return ""
	}
	switch path[0] {
	case "interfaces":
		return IETFInterfacesNS
	case "routing":
		return IETFRoutingNS
	default:
		return ArcaConfigNS
	}
}
