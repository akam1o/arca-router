package netconf

import (
	"encoding/xml"
	"testing"
)

func TestParseXPathFilter(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		wantErr        bool
		wantSegments   []string
		wantPredicates map[int]map[string]string
	}{
		{
			name:         "empty path",
			path:         "",
			wantErr:      false,
			wantSegments: nil,
		},
		{
			name:         "root only",
			path:         "/",
			wantErr:      false,
			wantSegments: nil,
		},
		{
			name:           "simple path",
			path:           "/interfaces",
			wantErr:        false,
			wantSegments:   []string{"interfaces"},
			wantPredicates: map[int]map[string]string{},
		},
		{
			name:           "nested path",
			path:           "/interfaces/interface",
			wantErr:        false,
			wantSegments:   []string{"interfaces", "interface"},
			wantPredicates: map[int]map[string]string{},
		},
		{
			name:           "path with predicate (single quotes)",
			path:           "/interfaces/interface[name='ge-0/0/0']",
			wantErr:        false,
			wantSegments:   []string{"interfaces", "interface"},
			wantPredicates: map[int]map[string]string{1: {"name": "ge-0/0/0"}},
		},
		{
			name:           "path with predicate (double quotes)",
			path:           `/interfaces/interface[name="ge-0/0/0"]`,
			wantErr:        false,
			wantSegments:   []string{"interfaces", "interface"},
			wantPredicates: map[int]map[string]string{1: {"name": "ge-0/0/0"}},
		},
		{
			name:           "routing-options with predicate",
			path:           "/routing-options/static/route[prefix='10.0.0.0/24']",
			wantErr:        false,
			wantSegments:   []string{"routing-options", "static", "route"},
			wantPredicates: map[int]map[string]string{2: {"prefix": "10.0.0.0/24"}},
		},
		{
			name:    "invalid: no leading slash",
			path:    "interfaces",
			wantErr: true,
		},
		{
			name:    "invalid: empty segment",
			path:    "/interfaces//interface",
			wantErr: true,
		},
		{
			name:    "invalid: function segment",
			path:    "/interfaces/count()",
			wantErr: true,
		},
		{
			name:    "invalid: namespace prefix not supported",
			path:    "/if:interfaces",
			wantErr: true,
		},
		{
			name:    "invalid: unclosed predicate",
			path:    "/interfaces/interface[name='value'",
			wantErr: true,
		},
		{
			name:    "invalid: predicate function not supported",
			path:    "/interfaces/interface[contains(name,'ge-0/0/0')]",
			wantErr: true,
		},
		{
			name:    "invalid: predicate without quotes",
			path:    "/interfaces/interface[name=value]",
			wantErr: true,
		},
		{
			name:    "invalid: predicate key with axis",
			path:    "/interfaces/interface[@name='ge-0/0/0']",
			wantErr: true,
		},
		{
			name:    "invalid: predicate trailing text",
			path:    "/interfaces/interface[name='ge-0/0/0']junk",
			wantErr: true,
		},
		{
			name:    "invalid: multiple predicates not supported in Phase 3",
			path:    "/interfaces/interface[name='ge-0/0/0'][foo='bar']",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseXPathFilter(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseXPathFilter() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			if got == nil && tt.wantSegments == nil {
				return // Both nil, OK
			}

			if got == nil || tt.wantSegments == nil {
				t.Errorf("ParseXPathFilter() got = %v, want segments %v", got, tt.wantSegments)
				return
			}

			if len(got.Segments) != len(tt.wantSegments) {
				t.Errorf("ParseXPathFilter() segments = %v, want %v", got.Segments, tt.wantSegments)
				return
			}

			for i, seg := range got.Segments {
				if seg != tt.wantSegments[i] {
					t.Errorf("ParseXPathFilter() segment[%d] = %v, want %v", i, seg, tt.wantSegments[i])
				}
			}

			if tt.wantPredicates != nil {
				if len(got.Predicates) != len(tt.wantPredicates) {
					t.Errorf("ParseXPathFilter() predicates count = %d, want %d", len(got.Predicates), len(tt.wantPredicates))
				}
				for segIdx, wantPredMap := range tt.wantPredicates {
					gotPredMap, ok := got.Predicates[segIdx]
					if !ok {
						t.Errorf("ParseXPathFilter() missing predicates for segment %d", segIdx)
						continue
					}
					for k, v := range wantPredMap {
						if gotPredMap[k] != v {
							t.Errorf("ParseXPathFilter() predicate[%d][%s] = %v, want %v", segIdx, k, gotPredMap[k], v)
						}
					}
				}
			}
		})
	}
}

func TestXPathFilter_MatchesElement(t *testing.T) {
	tests := []struct {
		name        string
		filterPath  string
		elementPath []string
		want        bool
	}{
		{
			name:        "nil filter matches all",
			filterPath:  "",
			elementPath: []string{"interfaces"},
			want:        true,
		},
		{
			name:        "exact match",
			filterPath:  "/interfaces",
			elementPath: []string{"interfaces"},
			want:        true,
		},
		{
			name:        "nested match",
			filterPath:  "/interfaces/interface",
			elementPath: []string{"interfaces", "interface"},
			want:        true,
		},
		{
			name:        "prefix match (element path longer)",
			filterPath:  "/interfaces",
			elementPath: []string{"interfaces", "interface", "name"},
			want:        true,
		},
		{
			name:        "no match (different element)",
			filterPath:  "/protocols",
			elementPath: []string{"interfaces"},
			want:        false,
		},
		{
			name:        "no match (element path shorter)",
			filterPath:  "/interfaces/interface/name",
			elementPath: []string{"interfaces"},
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter, err := ParseXPathFilter(tt.filterPath)
			if err != nil {
				t.Fatalf("ParseXPathFilter() error = %v", err)
			}

			got := filter.MatchesElement(tt.elementPath)
			if got != tt.want {
				t.Errorf("XPathFilter.MatchesElement() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestXPathFilterMatchesSection(t *testing.T) {
	tests := []struct {
		name        string
		filterPath  string
		elementPath []string
		want        bool
	}{
		{
			name:        "exact top-level match",
			filterPath:  "/interfaces",
			elementPath: []string{"interfaces"},
			want:        true,
		},
		{
			name:        "child selection includes parent section",
			filterPath:  "/interfaces/interface[name='ge-0/0/0']",
			elementPath: []string{"interfaces"},
			want:        true,
		},
		{
			name:        "parent selection includes child section",
			filterPath:  "/state",
			elementPath: []string{"state", "routes"},
			want:        true,
		},
		{
			name:        "different branch does not match",
			filterPath:  "/state/routes",
			elementPath: []string{"state", "protocols", "bgp"},
			want:        false,
		},
		{
			name:        "different top-level does not match",
			filterPath:  "/protocols/bgp",
			elementPath: []string{"interfaces"},
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter, err := ParseXPathFilter(tt.filterPath)
			if err != nil {
				t.Fatalf("ParseXPathFilter() error = %v", err)
			}

			got := filter.MatchesSection(tt.elementPath)
			if got != tt.want {
				t.Errorf("XPathFilter.MatchesSection() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFilterMatches(t *testing.T) {
	tests := []struct {
		name    string
		filter  *Filter
		element string
		want    bool
	}{
		{
			name:    "nil filter matches all",
			filter:  nil,
			element: "interfaces",
			want:    true,
		},
		{
			name:    "empty filter matches all",
			filter:  &Filter{Content: []byte{}},
			element: "interfaces",
			want:    true,
		},
		{
			name:    "subtree filter match",
			filter:  &Filter{Content: []byte("<interfaces/>")},
			element: "interfaces",
			want:    true,
		},
		{
			name: "subtree filter namespace prefix match",
			filter: &Filter{
				Content: []byte("<if:interfaces/>"),
				InheritedAttrs: []xml.Attr{
					{Name: xml.Name{Space: "xmlns", Local: "if"}, Value: IETFInterfacesNS},
				},
			},
			element: "interfaces",
			want:    true,
		},
		{
			name:    "subtree filter no match",
			filter:  &Filter{Content: []byte("<protocols/>")},
			element: "interfaces",
			want:    false,
		},
		{
			name:    "xpath filter top-level match",
			filter:  &Filter{Type: "xpath", Select: "/interfaces"},
			element: "interfaces",
			want:    true,
		},
		{
			name:    "xpath filter child selection includes top-level element",
			filter:  &Filter{Type: "xpath", Select: "/interfaces/interface[name='ge-0/0/0']"},
			element: "interfaces",
			want:    true,
		},
		{
			name:    "xpath filter no match",
			filter:  &Filter{Type: "xpath", Select: "/protocols/bgp"},
			element: "interfaces",
			want:    false,
		},
		{
			name:    "text filter does not match all",
			filter:  &Filter{Content: []byte("junk")},
			element: "interfaces",
			want:    false,
		},
		{
			name:    "bare xpath text does not match subtree",
			filter:  &Filter{Content: []byte("/interfaces")},
			element: "interfaces",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterMatches(tt.filter, tt.element)
			if got != tt.want {
				t.Errorf("filterMatches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFilterMatchesEnhancedXPathPath(t *testing.T) {
	filter := &Filter{Type: "xpath", Select: "/state/routes/route[prefix='192.0.2.0/24']"}

	if !filterMatchesEnhanced(filter, []string{"state", "routes"}) {
		t.Fatal("filterMatchesEnhanced() = false, want true for selected state routes branch")
	}
	if filterMatchesEnhanced(filter, []string{"state", "protocols", "bgp"}) {
		t.Fatal("filterMatchesEnhanced() = true, want false for sibling protocol state branch")
	}
}

func TestIncludeOperationalSectionXPath(t *testing.T) {
	filter := &Filter{Type: "xpath", Select: "/state/routes/route[prefix='192.0.2.0/24']"}

	if !includeOperationalSection(filter, "state", "routes") {
		t.Fatal("includeOperationalSection() = false, want true for selected state routes branch")
	}
	if includeOperationalSection(filter, "state", "protocols", "bgp") {
		t.Fatal("includeOperationalSection() = true, want false for sibling protocol state branch")
	}
}

func TestParseFilterElements(t *testing.T) {
	tests := []struct {
		name    string
		xml     []byte
		want    []string
		wantErr bool
	}{
		{
			name:    "single element",
			xml:     []byte("<interfaces/>"),
			want:    []string{"interfaces"},
			wantErr: false,
		},
		{
			name:    "namespace prefix element",
			xml:     []byte(`<if:interfaces xmlns:if="urn:ietf:params:xml:ns:yang:ietf-interfaces"/>`),
			want:    []string{"interfaces"},
			wantErr: false,
		},
		{
			name:    "multiple elements",
			xml:     []byte("<interfaces/><protocols/>"),
			want:    []string{"interfaces", "protocols"},
			wantErr: false,
		},
		{
			name:    "nested elements",
			xml:     []byte("<interfaces><interface><name>ge-0/0/0</name></interface></interfaces>"),
			want:    []string{"interfaces"},
			wantErr: false,
		},
		{
			name:    "invalid XML",
			xml:     []byte("<interfaces><unclosed"),
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseFilterElements(tt.xml)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseFilterElements() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			if len(got) != len(tt.want) {
				t.Errorf("parseFilterElements() = %v, want %v", got, tt.want)
				return
			}

			for i, elem := range got {
				if elem != tt.want[i] {
					t.Errorf("parseFilterElements()[%d] = %v, want %v", i, elem, tt.want[i])
				}
			}
		})
	}
}
