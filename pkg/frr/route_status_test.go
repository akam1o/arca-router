package frr

import "testing"

func TestParseRouteStatusJSONAcceptsPrefixKeyedMap(t *testing.T) {
	status, err := ParseRouteStatusJSON([]byte(`{
		"10.0.0.0/24": [
			{
				"protocol": "bgp",
				"selected": true,
				"metric": 20,
				"nexthops": [
					{"ip": "192.0.2.1", "interfaceName": "ge0-0-0", "active": true}
				]
			}
		],
		"192.0.2.0/24": [
			{
				"protocol": "connected",
				"selected": true,
				"interfaceName": "ge0-0-1"
			}
		]
	}`))
	if err != nil {
		t.Fatalf("ParseRouteStatusJSON() error = %v", err)
	}
	if len(status.Routes) != 2 {
		t.Fatalf("routes = %d, want 2", len(status.Routes))
	}
	if got := status.Routes[0]; got.Prefix != "10.0.0.0/24" || got.NextHop != "192.0.2.1" ||
		got.Protocol != "bgp" || got.Metric != 20 || got.Interface != "ge0-0-0" || !got.Active {
		t.Fatalf("BGP route = %#v, want parsed route and nexthop", got)
	}
	if got := status.Routes[1]; got.Prefix != "192.0.2.0/24" || got.Protocol != "connected" ||
		got.Interface != "ge0-0-1" || !got.Active {
		t.Fatalf("connected route = %#v, want interface route", got)
	}
}

func TestParseRouteStatusJSONAcceptsRoutesArray(t *testing.T) {
	status, err := ParseRouteStatusJSON([]byte(`{
		"routes": [
			{
				"prefix": "2001:db8:100::/64",
				"routeType": "ospf6",
				"destSelected": true,
				"nexthops": [
					{"gateway": "2001:db8::2", "interface": "ge0-0-0", "active": true},
					{"gateway": "2001:db8::3", "interface": "ge0-0-1", "active": false}
				]
			}
		]
	}`))
	if err != nil {
		t.Fatalf("ParseRouteStatusJSON() error = %v", err)
	}
	if len(status.Routes) != 2 {
		t.Fatalf("routes = %d, want one route per nexthop", len(status.Routes))
	}
	if got := status.Routes[0]; got.Prefix != "2001:db8:100::/64" || got.NextHop != "2001:db8::2" ||
		got.Protocol != "ospf6" || got.Interface != "ge0-0-0" || !got.Active {
		t.Fatalf("primary route = %#v, want active IPv6 route", got)
	}
	if got := status.Routes[1]; got.NextHop != "2001:db8::3" || got.Active {
		t.Fatalf("backup route = %#v, want inactive nexthop", got)
	}
}

func TestParseRouteStatusJSONRejectsInvalidJSON(t *testing.T) {
	if _, err := ParseRouteStatusJSON([]byte(`not-json`)); err == nil {
		t.Fatal("ParseRouteStatusJSON(invalid) error = nil, want error")
	}
}
