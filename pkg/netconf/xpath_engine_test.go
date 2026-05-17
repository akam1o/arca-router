package netconf

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestExperimentalXPathFilterSupportsFunctions(t *testing.T) {
	xmlData := []byte(`<interfaces xmlns="` + IETFInterfacesNS + `">
  <interface><name>ge-0/0/0</name><description>uplink</description></interface>
  <interface><name>xe-0/0/0</name><description>peer</description></interface>
</interfaces>`)
	filter := prefixedXPathFilter("/if:interfaces/if:interface[contains(if:name, 'ge-0/0/0')]")

	got, err := applyExperimentalXPathFilter("get-config", xmlData, filter)
	if err != nil {
		t.Fatalf("applyExperimentalXPathFilter() error = %v", err)
	}
	gotXML := string(got)
	for _, want := range []string{"<interfaces", "ge-0/0/0", "uplink"} {
		if !strings.Contains(gotXML, want) {
			t.Fatalf("filtered XML missing %q:\n%s", want, gotXML)
		}
	}
	for _, unwanted := range []string{"xe-0/0/0", "peer"} {
		if strings.Contains(gotXML, unwanted) {
			t.Fatalf("filtered XML contains %q:\n%s", unwanted, gotXML)
		}
	}
}

func TestExperimentalXPathFilterSupportsBooleanPredicates(t *testing.T) {
	xmlData := []byte(`<interfaces xmlns="` + IETFInterfacesNS + `">
  <interface><name>ge-0/0/0</name><description>uplink</description></interface>
  <interface><name>xe-0/0/0</name><description>peer</description></interface>
  <interface><name>et-0/0/0</name><description>core</description></interface>
</interfaces>`)
	filter := prefixedXPathFilter("/if:interfaces/if:interface[if:description='uplink' or contains(if:name, 'xe-')]")

	got, err := applyExperimentalXPathFilter("get-config", xmlData, filter)
	if err != nil {
		t.Fatalf("applyExperimentalXPathFilter() error = %v", err)
	}
	gotXML := string(got)
	for _, want := range []string{"ge-0/0/0", "xe-0/0/0"} {
		if !strings.Contains(gotXML, want) {
			t.Fatalf("filtered XML missing %q:\n%s", want, gotXML)
		}
	}
	if strings.Contains(gotXML, "et-0/0/0") {
		t.Fatalf("filtered XML contains boolean predicate mismatch:\n%s", gotXML)
	}
}

func TestExperimentalXPathFilterRejectsNonNodeSet(t *testing.T) {
	filter := prefixedXPathFilter("/if:interfaces/if:interface = 'ge-0/0/0'")

	err := filter.Validate("get-config")
	if err == nil {
		t.Fatal("Validate() error = nil, want non-node-set error")
	}
	if rpcErr, ok := err.(*RPCError); !ok || rpcErr.ErrorTag != ErrorTagInvalidValue {
		t.Fatalf("Validate() error = %#v, want invalid-value RPCError", err)
	}
}

func TestExperimentalXPathFilterRejectsAttributeSelection(t *testing.T) {
	xmlData := []byte(`<interfaces xmlns="` + IETFInterfacesNS + `"><interface enabled="true"><name>ge-0/0/0</name></interface></interfaces>`)
	filter := prefixedXPathFilter("/if:interfaces/if:interface/@enabled")

	_, err := applyExperimentalXPathFilter("get-config", xmlData, filter)
	if err == nil {
		t.Fatal("applyExperimentalXPathFilter() error = nil, want attribute selection error")
	}
	if rpcErr, ok := err.(*RPCError); !ok || rpcErr.ErrorTag != ErrorTagInvalidValue {
		t.Fatalf("applyExperimentalXPathFilter() error = %#v, want invalid-value RPCError", err)
	}
}

func TestFilterValidateAllowsExperimentalXPathFunctions(t *testing.T) {
	filter := prefixedXPathFilter("/if:interfaces/if:interface[contains(if:name, 'ge-0/0/0')]")

	if err := filter.Validate("get-config"); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if err := ValidateFilterDepthAndSize("get-config", filter); err != nil {
		t.Fatalf("ValidateFilterDepthAndSize() error = %v", err)
	}
}

func prefixedXPathFilter(selectExpr string) *Filter {
	return &Filter{
		Type:   "xpath",
		Select: selectExpr,
		Attrs: []xml.Attr{
			{Name: xml.Name{Space: "xmlns", Local: "if"}, Value: IETFInterfacesNS},
		},
	}
}
