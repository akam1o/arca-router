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
	// Phase 3: Basic key-value matching only, single predicate per segment
	// Phase 4: Multiple predicates and complex boolean expressions
	Predicates map[int]map[string]string
	// Namespace URI per predicate key when a namespace prefix was used.
	PredicateNamespaces map[int]map[string]string
}

// ParseXPathFilter parses a simplified XPath expression
// Supported formats (Phase 3):
// - /interfaces
// - /interfaces/interface
// - /interfaces/interface[name='ge-0/0/0']
// - /routing-options/static/route[prefix='10.0.0.0/24']
// - /if:interfaces/if:interface[if:name='ge-0/0/0']
//
// Not supported (Phase 4):
// - Functions (count(), contains(), etc.)
// - Multiple predicates
// - Complex boolean expressions
func ParseXPathFilter(path string) (*XPathFilter, error) {
	return parseXPathFilter(path, nil)
}

func ParseXPathFilterWithContext(path string, namespaceAttrs []xml.Attr) (*XPathFilter, error) {
	namespaceCtx := newXPathNamespaceContext(namespaceAttrs)
	return parseXPathFilter(path, namespaceCtx)
}

func parseXPathFilter(path string, namespaceCtx map[string]string) (*XPathFilter, error) {
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

			// Extract ALL predicates (Phase 3: only store first, warn on multiple)
			predicateMap := make(map[string]string)
			predicateNamespaces := make(map[string]string)
			remaining := seg[idx:]
			predicateCount := 0

			for len(remaining) > 0 && remaining[0] == '[' {
				predEnd := strings.Index(remaining, "]")
				if predEnd == -1 {
					return nil, fmt.Errorf("unclosed predicate in: %s", seg)
				}

				predicate := remaining[1:predEnd]
				// Parse key='value' or key="value"
				if err := parsePredicate(predicate, predicateMap, predicateNamespaces, namespaceCtx); err != nil {
					return nil, fmt.Errorf("invalid predicate in %s: %w", seg, err)
				}

				predicateCount++
				remaining = remaining[predEnd+1:]
			}

			// Phase 3 limitation: only one predicate supported
			if predicateCount > 1 {
				return nil, fmt.Errorf("multiple predicates not supported in Phase 3 (found %d in %s)", predicateCount, seg)
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

	// Remove quotes
	if len(valueRaw) < 2 {
		return fmt.Errorf("predicate value must be quoted: %s", pred)
	}

	quote := valueRaw[0]
	if (quote != '\'' && quote != '"') || valueRaw[len(valueRaw)-1] != quote {
		return fmt.Errorf("predicate value must be quoted with ' or \": %s", pred)
	}

	value := valueRaw[1 : len(valueRaw)-1]
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
	if filter == nil || len(filter.Content) == 0 {
		return xmlData, nil
	}

	// Parse filter as XML to extract element structure
	filterElements, err := filter.parseTopLevelElements()
	if err != nil {
		return nil, fmt.Errorf("invalid subtree filter: %w", err)
	}

	if len(filterElements) == 0 {
		return xmlData, nil // Empty filter matches all
	}

	// Phase 3: Simple element name matching
	// For each filter element, check if it exists in the data
	// If yes, include that subtree; if no, exclude it

	// This is a simplified implementation
	// Full subtree filtering would require proper XML tree manipulation
	// For Phase 3, we use string-based matching

	var result bytes.Buffer
	result.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	result.WriteString(`<data xmlns="` + NetconfBaseNS + `">` + "\n")

	for _, elem := range filterElements {
		// Extract matching subtrees from xmlData
		subtree := extractSubtree(xmlData, elem)
		if len(subtree) > 0 {
			result.Write(subtree)
		}
	}

	result.WriteString("</data>\n")
	return result.Bytes(), nil
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
	var wrapped bytes.Buffer
	wrapped.WriteString("<filter")
	writeNamespaceDeclarationAttrs(&wrapped, namespaceAttrs, map[string]string{})
	wrapped.WriteByte('>')
	wrapped.Write(filterXML)
	wrapped.WriteString("</filter>")

	decoder := xml.NewDecoder(bytes.NewReader(wrapped.Bytes()))
	decoder.Strict = true
	decoder.Entity = nil
	elements := make([]string, 0)
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
			if depth == 2 {
				// Top-level element
				elements = append(elements, t.Name.Local)
			}
		case xml.EndElement:
			if depth > 0 {
				depth--
			}
		}
	}

	return elements, nil
}

// extractSubtree extracts a subtree matching the given element name
// Phase 3: Simple element-based extraction
func extractSubtree(xmlData []byte, elementName string) []byte {
	// Find <elementName> and </elementName>
	startTag := "<" + elementName
	endTag := "</" + elementName + ">"

	startIdx := bytes.Index(xmlData, []byte(startTag))
	if startIdx == -1 {
		return nil
	}

	// Find the matching end tag
	// Simple implementation: find the first occurrence after start
	// Phase 4 would use proper XML parsing with nesting awareness
	endIdx := bytes.Index(xmlData[startIdx:], []byte(endTag))
	if endIdx == -1 {
		return nil
	}

	endIdx += startIdx + len(endTag)
	return xmlData[startIdx:endIdx]
}

// filterMatches checks if a top-level element matches the filter
// Enhanced version with XPath-like support (Phase 3)
func filterMatchesEnhanced(filter *Filter, elementPath []string) bool {
	if filter == nil {
		return true
	}

	if filter.Type == "xpath" {
		xpathFilter, err := ParseXPathFilter(strings.TrimSpace(filter.Select))
		if err != nil {
			return false
		}
		return xpathFilter.MatchesSection(elementPath)
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
