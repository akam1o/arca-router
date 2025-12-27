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

	// Predicates per segment (e.g., map[segmentIndex]map["name"]="ge-0/0/0")
	// Phase 3: Basic key-value matching only, single predicate per segment
	// Phase 4: Multiple predicates and complex boolean expressions
	Predicates map[int]map[string]string
}

// ParseXPathFilter parses a simplified XPath expression
// Supported formats (Phase 3):
// - /interfaces
// - /interfaces/interface
// - /interfaces/interface[name='ge-0/0/0']
// - /routing-options/static/route[prefix='10.0.0.0/24']
//
// Not supported (Phase 4):
// - Functions (count(), contains(), etc.)
// - Multiple predicates
// - Complex boolean expressions
func ParseXPathFilter(path string) (*XPathFilter, error) {
	if path == "" || path == "/" {
		return nil, nil // Empty filter matches all
	}

	if !strings.HasPrefix(path, "/") {
		return nil, fmt.Errorf("XPath must start with /: %s", path)
	}

	// Remove leading /
	path = strings.TrimPrefix(path, "/")

	filter := &XPathFilter{
		Segments:   make([]string, 0),
		Predicates: make(map[int]map[string]string),
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
			continue
		}

		// Check for predicate: interface[name='value']
		if idx := strings.Index(seg, "["); idx != -1 {
			// Extract element name
			elemName := seg[:idx]
			segmentIndex := len(filter.Segments)
			filter.Segments = append(filter.Segments, elemName)

			// Extract ALL predicates (Phase 3: only store first, warn on multiple)
			predicateMap := make(map[string]string)
			remaining := seg[idx:]
			predicateCount := 0

			for len(remaining) > 0 && remaining[0] == '[' {
				predEnd := strings.Index(remaining, "]")
				if predEnd == -1 {
					return nil, fmt.Errorf("unclosed predicate in: %s", seg)
				}

				predicate := remaining[1:predEnd]
				// Parse key='value' or key="value"
				if err := parsePredicate(predicate, predicateMap); err != nil {
					return nil, fmt.Errorf("invalid predicate in %s: %w", seg, err)
				}

				predicateCount++
				remaining = remaining[predEnd+1:]
			}

			// Phase 3 limitation: only one predicate supported
			if predicateCount > 1 {
				return nil, fmt.Errorf("multiple predicates not supported in Phase 3 (found %d in %s)", predicateCount, seg)
			}

			if len(predicateMap) > 0 {
				filter.Predicates[segmentIndex] = predicateMap
			}
		} else {
			filter.Segments = append(filter.Segments, seg)
		}
	}

	return filter, nil
}

// parsePredicate parses a single predicate expression
// Supported: key='value' or key="value"
func parsePredicate(pred string, predicates map[string]string) error {
	// Look for = operator
	eqIdx := strings.Index(pred, "=")
	if eqIdx == -1 {
		return fmt.Errorf("predicate must contain =: %s", pred)
	}

	key := strings.TrimSpace(pred[:eqIdx])
	valueRaw := strings.TrimSpace(pred[eqIdx+1:])

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
	return nil
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

// ApplySubtreeFilter applies subtree filtering to XML data
// This implements RFC 6241 Section 6 subtree filtering
// Phase 3: Element name matching with basic predicates
// Phase 4: Full namespace-aware filtering
func ApplySubtreeFilter(xmlData []byte, filter *Filter) ([]byte, error) {
	if filter == nil || len(filter.Content) == 0 {
		return xmlData, nil
	}

	// Parse filter as XML to extract element structure
	filterElements, err := parseFilterElements(filter.Content)
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
	decoder := xml.NewDecoder(bytes.NewReader(filterXML))
	elements := make([]string, 0)
	inElement := false

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
			if !inElement {
				// Top-level element
				elements = append(elements, t.Name.Local)
				inElement = true
			}
		case xml.EndElement:
			if inElement && len(elements) > 0 {
				// Check if we're closing a top-level element
				// For simplicity, we assume one-level nesting for Phase 3
				inElement = false
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
	if filter == nil || len(filter.Content) == 0 {
		return true
	}

	// Try to parse as XPath first (if it looks like a path)
	if len(filter.Content) > 0 && filter.Content[0] == '/' {
		xpath, err := ParseXPathFilter(string(filter.Content))
		if err == nil && xpath != nil {
			return xpath.MatchesElement(elementPath)
		}
	}

	// Fallback to simple substring matching (backward compatibility)
	if len(elementPath) > 0 {
		return bytes.Contains(filter.Content, []byte("<"+elementPath[0]))
	}

	return true
}

// filterMatches is the legacy function for backward compatibility
// Phase 3: Keep for existing code, use filterMatchesEnhanced for new code
func filterMatches(filter *Filter, element string) bool {
	return filterMatchesEnhanced(filter, []string{element})
}
