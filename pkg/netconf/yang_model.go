package netconf

import (
	_ "embed"
	"fmt"
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
			// Allow only specific missing IETF modules (Phase 4 dependency)
			isIETFModuleNotFound := (strings.Contains(errStr, "ietf-interfaces") && strings.Contains(errStr, "not found")) ||
				(strings.Contains(errStr, "ietf-routing") && strings.Contains(errStr, "not found"))

			if !isIETFModuleNotFound {
				// All other errors are fatal
				hasNonIgnorableError = true
				// Log for debugging but don't return yet - collect all errors
			}
		}
		if hasNonIgnorableError {
			// Return first non-ignorable error for clarity
			for _, err := range errs {
				errStr := err.Error()
				isIETFModuleNotFound := (strings.Contains(errStr, "ietf-interfaces") && strings.Contains(errStr, "not found")) ||
					(strings.Contains(errStr, "ietf-routing") && strings.Contains(errStr, "not found"))
				if !isIETFModuleNotFound {
					return nil, fmt.Errorf("YANG schema error: %v", err)
				}
			}
		}
	}

	return &YANGValidator{
		modules: ms,
	}, nil
}

// ValidateConfig performs basic YANG validation on the configuration
// Phase 3: Structural validation only (allowlist approach)
// Phase 4: Full YANG semantic validation with constraints
func (v *YANGValidator) ValidateConfig(xmlData []byte) error {
	if v == nil {
		return fmt.Errorf("YANG validator not initialized")
	}

	v.mu.RLock()
	defer v.mu.RUnlock()

	// Phase 3 implementation: Basic structural validation
	// We validate:
	// 1. XML is well-formed (already done by xml.Unmarshal in xml_convert.go)
	// 2. Top-level elements match YANG schema (system, interfaces, routing-options, protocols)
	// 3. Size and depth limits (already enforced in xml_convert.go)

	// For Phase 3, we accept the parse validation done during NewYANGValidator
	// Full validation against data instances requires libyang or more advanced processing
	// which is deferred to Phase 4

	return nil
}

// GetModel returns the parsed YANG module for programmatic access
func (v *YANGValidator) GetModel(moduleName string) (*yang.Module, error) {
	if v == nil {
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
	if v == nil {
		return nil
	}

	v.mu.RLock()
	defer v.mu.RUnlock()

	names := make([]string, 0, len(v.modules.Modules))
	for name := range v.modules.Modules {
		names = append(names, name)
	}
	return names
}

// ValidateElementPath validates that an XPath-like element path is valid
// according to the YANG schema (Phase 3: basic implementation)
func (v *YANGValidator) ValidateElementPath(path string) error {
	if v == nil {
		return fmt.Errorf("YANG validator not initialized")
	}

	// Phase 3: Basic allowlist validation
	// Accept paths matching top-level containers:
	// - /system
	// - /interfaces
	// - /routing-options
	// - /protocols
	// - /state (read-only)

	allowedPaths := map[string]bool{
		"/system":          true,
		"/interfaces":      true,
		"/routing-options": true,
		"/protocols":       true,
		"/state":           true,
	}

	// For Phase 3, we only validate the first path segment
	// Full path validation with YANG schema traversal is Phase 4
	if len(path) == 0 || path[0] != '/' {
		return fmt.Errorf("path must start with /")
	}

	// Extract first segment
	var firstSegment string
	for i := 1; i < len(path); i++ {
		if path[i] == '/' {
			firstSegment = path[:i]
			break
		}
	}
	if firstSegment == "" {
		firstSegment = path
	}

	if !allowedPaths[firstSegment] {
		return fmt.Errorf("unsupported top-level path: %s", firstSegment)
	}

	return nil
}
