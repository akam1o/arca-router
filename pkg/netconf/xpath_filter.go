package netconf

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// XPathFilter represents a basic XPath-like filter for NETCONF
// Phase 3: Implements simplified subtree filtering with path matching
// Phase 4: Full XPath 1.0 support with predicates and functions
type XPathFilter struct {
	// Path segments (e.g., ["interfaces", "interface", "name"])
	Segments []string
	// Namespace URI per path segment when a namespace prefix was used.
	SegmentNamespaces []string

	// Predicates per segment (e.g., map[segmentIndex]map["name"]="ge-0/0/0")
	// Supports multiple simple key-value predicates per segment.
	Predicates map[int]map[string]string
	// Namespace URI per predicate key when a namespace prefix was used.
	PredicateNamespaces map[int]map[string]string
}

type subtreeFilterElement struct {
	LocalName string
	Namespace string
}

func normalizedFilterType(filter *Filter) string {
	if filter == nil {
		return ""
	}
	return strings.TrimSpace(filter.Type)
}

// ParseXPathFilter parses a simplified XPath expression
// Supported formats (Phase 3):
// - /interfaces
// - /interfaces/interface
// - /interfaces/interface[name='ge-0/0/0']
// - /interfaces/interface[name='ge-0/0/0'][unit='0']
// - /routing-options/static/route[prefix='10.0.0.0/24']
// - /if:interfaces/if:interface[if:name='ge-0/0/0']
//
// Not supported (Phase 4):
// - Functions (count(), contains(), etc.)
// - Complex boolean expressions
func ParseXPathFilter(path string) (*XPathFilter, error) {
	return parseXPathFilter(path, nil)
}

func ParseXPathFilterWithContext(path string, namespaceAttrs []xml.Attr) (*XPathFilter, error) {
	namespaceCtx := newXPathNamespaceContext(namespaceAttrs)
	return parseXPathFilter(path, namespaceCtx)
}

func parseXPathFilter(path string, namespaceCtx map[string]string) (*XPathFilter, error) {
	path = strings.TrimSpace(path)

	if path == "" || path == "/" {
		return nil, nil // Empty filter matches all
	}

	if !strings.HasPrefix(path, "/") {
		return nil, fmt.Errorf("XPath must start with /: %s", path)
	}

	// Remove leading /
	path = strings.TrimPrefix(path, "/")
	if strings.Contains(path, "//") {
		return nil, fmt.Errorf("empty XPath segment in: /%s", path)
	}

	filter := &XPathFilter{
		Segments:            make([]string, 0),
		SegmentNamespaces:   make([]string, 0),
		Predicates:          make(map[int]map[string]string),
		PredicateNamespaces: make(map[int]map[string]string),
	}

	// Parse segments manually to handle predicates with / in values
	// e.g., interface[name='ge-0/0/0']
	remaining := path
	for len(remaining) > 0 {
		// Find next segment delimiter /
		// But we need to skip / inside [...] predicates

		segEnd := -1
		inPredicate := false
		inQuote := false
		var quoteChar byte

		for i := 0; i < len(remaining); i++ {
			ch := remaining[i]

			if ch == '\'' || ch == '"' {
				if !inQuote {
					inQuote = true
					quoteChar = ch
				} else if ch == quoteChar {
					inQuote = false
				}
			} else if ch == '[' && !inQuote {
				inPredicate = true
			} else if ch == ']' && !inQuote {
				inPredicate = false
			} else if ch == '/' && !inPredicate && !inQuote {
				segEnd = i
				break
			}
		}

		var seg string
		if segEnd == -1 {
			seg = remaining
			remaining = ""
		} else {
			seg = remaining[:segEnd]
			remaining = remaining[segEnd+1:]
		}

		if seg == "" {
			return nil, fmt.Errorf("empty XPath segment in: /%s", path)
		}

		// Check for predicate: interface[name='value']
		if idx := strings.Index(seg, "["); idx != -1 {
			// Extract element name
			rawElemName := seg[:idx]
			elemName, namespace, err := normalizeXPathName(rawElemName, namespaceCtx)
			if err != nil {
				return nil, fmt.Errorf("invalid XPath segment %q: %w", rawElemName, err)
			}
			segmentIndex := len(filter.Segments)
			filter.Segments = append(filter.Segments, elemName)
			filter.SegmentNamespaces = append(filter.SegmentNamespaces, namespace)

			// Extract all simple key-value predicates.
			predicateMap := make(map[string]string)
			predicateNamespaces := make(map[string]string)
			remaining := seg[idx:]

			for len(remaining) > 0 && remaining[0] == '[' {
				predEnd := findXPathPredicateEnd(remaining)
				if predEnd == -1 {
					return nil, fmt.Errorf("unclosed predicate in: %s", seg)
				}

				predicate := remaining[1:predEnd]
				// Parse key='value' or key="value"
				if err := parsePredicate(predicate, predicateMap, predicateNamespaces, namespaceCtx); err != nil {
					return nil, fmt.Errorf("invalid predicate in %s: %w", seg, err)
				}

				remaining = remaining[predEnd+1:]
			}

			if remaining != "" {
				return nil, fmt.Errorf("invalid predicate suffix in: %s", seg)
			}

			if len(predicateMap) > 0 {
				filter.Predicates[segmentIndex] = predicateMap
				filter.PredicateNamespaces[segmentIndex] = predicateNamespaces
			}
		} else {
			segment, namespace, err := normalizeXPathName(seg, namespaceCtx)
			if err != nil {
				return nil, fmt.Errorf("invalid XPath segment %q: %w", seg, err)
			}
			filter.Segments = append(filter.Segments, segment)
			filter.SegmentNamespaces = append(filter.SegmentNamespaces, namespace)
		}
	}

	return filter, nil
}

func findXPathPredicateEnd(expr string) int {
	inQuote := false
	var quoteChar byte

	for i := 0; i < len(expr); i++ {
		ch := expr[i]
		switch {
		case ch == '\'' || ch == '"':
			if !inQuote {
				inQuote = true
				quoteChar = ch
			} else if ch == quoteChar {
				inQuote = false
			}
		case ch == ']' && !inQuote:
			return i
		}
	}
	return -1
}

// parsePredicate parses a single predicate expression
// Supported: key='value' or key="value"
func parsePredicate(pred string, predicates map[string]string, namespaces map[string]string, namespaceCtx map[string]string) error {
	// Look for = operator
	eqIdx := strings.Index(pred, "=")
	if eqIdx == -1 {
		return fmt.Errorf("predicate must contain =: %s", pred)
	}

	rawKey := strings.TrimSpace(pred[:eqIdx])
	valueRaw := strings.TrimSpace(pred[eqIdx+1:])
	key, namespace, err := normalizeXPathName(rawKey, namespaceCtx)
	if err != nil {
		return fmt.Errorf("invalid predicate key %q: %w", rawKey, err)
	}
	if _, exists := predicates[key]; exists {
		return fmt.Errorf("duplicate predicate key: %s", key)
	}

	// Remove quotes
	if len(valueRaw) < 2 {
		return fmt.Errorf("predicate value must be quoted: %s", pred)
	}

	quote := valueRaw[0]
	if (quote != '\'' && quote != '"') || valueRaw[len(valueRaw)-1] != quote {
		return fmt.Errorf("predicate value must be quoted with ' or \": %s", pred)
	}

	value := valueRaw[1 : len(valueRaw)-1]
	if strings.Contains(value, string(quote)) {
		return fmt.Errorf("complex predicate expressions are not supported: %s", pred)
	}
	predicates[key] = value
	namespaces[key] = namespace
	return nil
}

func normalizeXPathName(name string, namespaceCtx map[string]string) (string, string, error) {
	prefix, local, prefixed, err := splitXPathQName(name)
	if err != nil {
		return "", "", err
	}
	if !prefixed {
		return local, "", nil
	}
	namespace := namespaceCtx[prefix]
	if namespaceCtx != nil && namespace == "" {
		return "", "", fmt.Errorf("namespace prefix %q is not declared", prefix)
	}
	return local, namespace, nil
}

func splitXPathQName(name string) (string, string, bool, error) {
	if name == "" {
		return "", "", false, fmt.Errorf("name must not be empty")
	}
	if strings.Count(name, ":") > 1 {
		return "", "", false, fmt.Errorf("name must contain at most one namespace prefix")
	}
	prefix, local, prefixed := strings.Cut(name, ":")
	if !prefixed {
		if err := validateXPathNamePart(name); err != nil {
			return "", "", false, err
		}
		return "", name, false, nil
	}
	if prefix == "" || local == "" {
		return "", "", false, fmt.Errorf("namespace prefix and local name must not be empty")
	}
	if err := validateXPathNamePart(prefix); err != nil {
		return "", "", false, fmt.Errorf("invalid namespace prefix: %w", err)
	}
	if err := validateXPathNamePart(local); err != nil {
		return "", "", false, err
	}
	return prefix, local, true, nil
}

func newXPathNamespaceContext(attrs []xml.Attr) map[string]string {
	ctx := make(map[string]string)
	for _, attr := range attrs {
		if attr.Name.Space != "xmlns" {
			continue
		}
		ctx[attr.Name.Local] = attr.Value
	}
	return ctx
}

func validateXPathNamePart(name string) error {
	if name == "" {
		return fmt.Errorf("name must not be empty")
	}
	if !isXPathNameStart(name[0]) {
		return fmt.Errorf("name must start with a letter or underscore")
	}
	for i := 1; i < len(name); i++ {
		if !isXPathNameChar(name[i]) {
			return fmt.Errorf("name contains unsupported character %q", name[i])
		}
	}
	return nil
}

func isXPathNameStart(ch byte) bool {
	return (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || ch == '_'
}

func isXPathNameChar(ch byte) bool {
	return isXPathNameStart(ch) || (ch >= '0' && ch <= '9') || ch == '-' || ch == '.'
}

// MatchesElement checks if the filter matches a given element path
// For example:
// - Filter /interfaces matches <interfaces>...</interfaces>
// - Filter /interfaces/interface matches <interfaces><interface>...</interface></interfaces>
func (f *XPathFilter) MatchesElement(elementPath []string) bool {
	if f == nil || len(f.Segments) == 0 {
		return true // Empty filter matches all
	}

	// Must match all segments
	if len(elementPath) < len(f.Segments) {
		return false
	}

	for i, seg := range f.Segments {
		if elementPath[i] != seg {
			return false
		}
	}

	return true
}

// MatchesSection checks whether the XPath points at the given element path, one
// of its ancestors, or one of its descendants. Section-level pruning uses this
// to include parent containers for child XPath selections.
func (f *XPathFilter) MatchesSection(elementPath []string) bool {
	if f == nil || len(f.Segments) == 0 {
		return true
	}
	if len(elementPath) == 0 {
		return false
	}

	limit := len(f.Segments)
	if len(elementPath) < limit {
		limit = len(elementPath)
	}
	for i := 0; i < limit; i++ {
		if f.Segments[i] != elementPath[i] {
			return false
		}
	}
	return true
}

// ApplySubtreeFilter applies subtree filtering to XML data
// This implements RFC 6241 Section 6 subtree filtering
// Phase 3: Element name matching with basic predicates
// Phase 4: Full namespace-aware filtering
func ApplySubtreeFilter(xmlData []byte, filter *Filter) ([]byte, error) {
	if filter == nil {
		return append([]byte(nil), xmlData...), nil
	}
	filterType := normalizedFilterType(filter)
	switch filterType {
	case "", "subtree":
	default:
		return nil, fmt.Errorf("unsupported subtree filter type: %s", filterType)
	}
	if len(bytes.TrimSpace(filter.Content)) == 0 {
		return append([]byte(nil), xmlData...), nil
	}

	filterPaths, err := filter.parseElementPaths()
	if err != nil {
		return nil, fmt.Errorf("invalid subtree filter: %w", err)
	}
	if len(filterPaths) == 0 {
		return nil, fmt.Errorf("subtree filter must contain at least one element")
	}
	if err := validateSubtreeFilterPaths(filterPaths); err != nil {
		return nil, fmt.Errorf("invalid subtree filter path: %w", err)
	}
	filterElements := topLevelSubtreeFilterElements(filterPaths)

	var result bytes.Buffer
	result.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	result.WriteString(`<data xmlns="` + NetconfBaseNS + `">` + "\n")

	for _, elem := range filterElements {
		subtrees, err := extractMatchingSubtrees(xmlData, elem)
		if err != nil {
			return nil, err
		}
		for _, subtree := range subtrees {
			result.Write(subtree)
			result.WriteByte('\n')
		}
	}

	result.WriteString("</data>\n")
	return result.Bytes(), nil
}

func topLevelSubtreeFilterElements(paths [][]subtreeFilterElement) []subtreeFilterElement {
	elements := make([]subtreeFilterElement, 0, len(paths))
	for _, path := range paths {
		if len(path) == 1 {
			elements = append(elements, path[0])
		}
	}
	return elements
}

// parseFilterElements extracts top-level element names from filter XML
func parseFilterElements(filterXML []byte) ([]string, error) {
	return parseFilterElementsWithContext(filterXML, nil)
}

func (f *Filter) parseTopLevelElements() ([]string, error) {
	if f == nil {
		return nil, nil
	}
	namespaceAttrs := collectNamespaceAttrs(f.InheritedAttrs, f.Attrs)
	return parseFilterElementsWithContext(f.Content, namespaceAttrs)
}

func parseFilterElementsWithContext(filterXML []byte, namespaceAttrs []xml.Attr) ([]string, error) {
	specs, err := parseFilterElementSpecsWithContext(filterXML, namespaceAttrs)
	if err != nil {
		return nil, err
	}
	elements := make([]string, 0, len(specs))
	for _, spec := range specs {
		elements = append(elements, spec.LocalName)
	}
	return elements, nil
}

func (f *Filter) parseTopLevelElementSpecs() ([]subtreeFilterElement, error) {
	if f == nil {
		return nil, nil
	}
	namespaceAttrs := collectNamespaceAttrs(f.InheritedAttrs, f.Attrs)
	return parseFilterElementSpecsWithContext(f.Content, namespaceAttrs)
}

func parseFilterElementSpecsWithContext(filterXML []byte, namespaceAttrs []xml.Attr) ([]subtreeFilterElement, error) {
	paths, err := parseFilterElementPathsWithContext(filterXML, namespaceAttrs)
	if err != nil {
		return nil, err
	}
	elements := make([]subtreeFilterElement, 0, len(paths))
	for _, path := range paths {
		if len(path) == 1 {
			elements = append(elements, path[0])
		}
	}
	return elements, nil
}

func (f *Filter) parseElementPaths() ([][]subtreeFilterElement, error) {
	if f == nil {
		return nil, nil
	}
	namespaceAttrs := collectNamespaceAttrs(f.InheritedAttrs, f.Attrs)
	return parseFilterElementPathsWithContext(f.Content, namespaceAttrs)
}

func parseFilterElementPathsWithContext(filterXML []byte, namespaceAttrs []xml.Attr) ([][]subtreeFilterElement, error) {
	var wrapped bytes.Buffer
	wrapped.WriteString("<filter")
	writeNamespaceDeclarationAttrs(&wrapped, namespaceAttrs, map[string]string{})
	wrapped.WriteByte('>')
	wrapped.Write(filterXML)
	wrapped.WriteString("</filter>")

	decoder := xml.NewDecoder(bytes.NewReader(wrapped.Bytes()))
	decoder.Strict = true
	decoder.Entity = nil
	paths := make([][]subtreeFilterElement, 0)
	stack := make([]subtreeFilterElement, 0)
	depth := 0

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		switch t := token.(type) {
		case xml.StartElement:
			depth++
			if depth >= 2 {
				stack = append(stack, subtreeFilterElement{
					LocalName: t.Name.Local,
					Namespace: t.Name.Space,
				})
				paths = append(paths, append([]subtreeFilterElement(nil), stack...))
			}
		case xml.EndElement:
			if depth >= 2 && len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			if depth > 0 {
				depth--
			}
		}
	}

	return paths, nil
}

func extractMatchingSubtrees(xmlData []byte, element subtreeFilterElement) ([][]byte, error) {
	decoder := xml.NewDecoder(bytes.NewReader(xmlData))
	decoder.Strict = true
	decoder.Entity = nil

	var subtrees [][]byte
	var subtree bytes.Buffer
	var encoder *xml.Encoder
	depth := 0
	collectingDepth := 0
	rootSeen := false
	rootIsData := false

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return subtrees, nil
		}
		if err != nil {
			return nil, fmt.Errorf("invalid XML data: %w", err)
		}

		switch t := token.(type) {
		case xml.StartElement:
			if !rootSeen {
				rootSeen = true
				rootIsData = t.Name.Local == "data"
			}
			shouldCollect := collectingDepth == 0 && element.matches(t.Name) &&
				((rootIsData && depth == 1) || (!rootIsData && depth == 0))
			switch {
			case collectingDepth > 0:
				if err := encoder.EncodeToken(t); err != nil {
					return nil, err
				}
				collectingDepth++
			case shouldCollect:
				subtree.Reset()
				encoder = xml.NewEncoder(&subtree)
				if err := encoder.EncodeToken(t); err != nil {
					return nil, err
				}
				collectingDepth = 1
			}
			depth++
		case xml.EndElement:
			if collectingDepth > 0 {
				if err := encoder.EncodeToken(t); err != nil {
					return nil, err
				}
				collectingDepth--
				if collectingDepth == 0 {
					if err := encoder.Flush(); err != nil {
						return nil, err
					}
					subtrees = append(subtrees, append([]byte(nil), subtree.Bytes()...))
					encoder = nil
				}
			}
			if depth > 0 {
				depth--
			}
		default:
			if collectingDepth > 0 {
				if err := encoder.EncodeToken(token); err != nil {
					return nil, err
				}
			}
		}
	}
}

func (e subtreeFilterElement) matches(name xml.Name) bool {
	if name.Local != e.LocalName {
		return false
	}
	if e.Namespace == "" || e.Namespace == NetconfBaseNS {
		return true
	}
	return name.Space == e.Namespace
}

// filterMatches checks if a top-level element matches the filter
// Enhanced version with XPath-like support (Phase 3)
func filterMatchesEnhanced(filter *Filter, elementPath []string) bool {
	if filter == nil {
		return true
	}

	filterType := normalizedFilterType(filter)
	switch filterType {
	case "xpath":
		xpathFilter, err := parseFilterXPathWithNamespaces(filter)
		if err != nil {
			return false
		}
		return xpathFilter.MatchesSection(elementPath)
	case "", "subtree":
	default:
		return false
	}

	content := bytes.TrimSpace(filter.Content)
	if len(content) == 0 {
		return true
	}

	filterElements, err := filter.parseTopLevelElements()
	if err != nil {
		return false
	}
	if len(filterElements) == 0 {
		return false
	}
	for _, elem := range filterElements {
		if len(elementPath) > 0 && elem == elementPath[0] {
			return true
		}
	}

	return false
}

// filterMatches is the legacy function for backward compatibility
// Phase 3: Keep for existing code, use filterMatchesEnhanced for new code
func filterMatches(filter *Filter, element string) bool {
	return filterMatchesEnhanced(filter, []string{element})
}

func parseFilterXPathWithNamespaces(filter *Filter) (*XPathFilter, error) {
	if filter == nil || normalizedFilterType(filter) != "xpath" {
		return nil, nil
	}

	selectExpr := strings.TrimSpace(filter.Select)
	if selectExpr == "" {
		return nil, fmt.Errorf("xpath filter requires select attribute")
	}
	namespaceAttrs := collectNamespaceAttrs(filter.InheritedAttrs, filter.Attrs)
	xpathFilter, err := ParseXPathFilterWithContext(selectExpr, namespaceAttrs)
	if err != nil {
		return nil, err
	}
	if xpathFilter != nil {
		if err := validateXPathFilterNamespaces(xpathFilter); err != nil {
			return nil, err
		}
	}
	return xpathFilter, nil
}
