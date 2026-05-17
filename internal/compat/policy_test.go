package compat

import (
	"strings"
	"testing"
)

func TestCurrentPolicyAdvertisesV010Compatibility(t *testing.T) {
	policy := CurrentPolicy()
	if policy.Phase != PolicyPhase {
		t.Fatalf("CurrentPolicy().Phase = %q, want %q", policy.Phase, PolicyPhase)
	}
	gotSources := strings.Join(policy.SupportedDirectUpgradeSources, ",")
	for _, want := range []string{"v0.8.x", "v0.9.x"} {
		if !strings.Contains(gotSources, want) {
			t.Fatalf("supported direct upgrade sources = %q, want %s", gotSources, want)
		}
	}
	for _, want := range []string{GRPCAPIPackage, "v1", "deprecation"} {
		if !strings.Contains(policy.APIVersioning+policy.DeprecationPolicy, want) {
			t.Fatalf("policy = %#v, want compatibility text containing %q", policy, want)
		}
	}
	if AuditSchema != "arca.audit.v1" {
		t.Fatalf("AuditSchema = %q, want arca.audit.v1", AuditSchema)
	}
}

func TestComponentMatrixIncludesDataplaneAndSchemaGuards(t *testing.T) {
	matrix := ComponentMatrix()
	byComponent := map[string]ComponentSupport{}
	for _, item := range matrix {
		byComponent[item.Component] = item
	}
	for _, want := range []string{"VPP", "FRR", "SQLite datastore", "NETCONF"} {
		if _, ok := byComponent[want]; !ok {
			t.Fatalf("ComponentMatrix() missing %s: %#v", want, matrix)
		}
	}
	if !strings.Contains(byComponent["SQLite datastore"].Notes, "newer schemas are rejected") {
		t.Fatalf("SQLite datastore notes = %q, want schema guardrail", byComponent["SQLite datastore"].Notes)
	}
	if !strings.Contains(byComponent["NETCONF"].Required+byComponent["NETCONF"].Notes, "standard :xpath") ||
		!strings.Contains(byComponent["NETCONF"].Notes, "explicit opt-in") {
		t.Fatalf("NETCONF notes = %q/%q, want standard :xpath opt-in policy",
			byComponent["NETCONF"].Required, byComponent["NETCONF"].Notes)
	}
	if byComponent["VPP"].Supported != "24.10+" || byComponent["FRR"].Supported != "8.0+" {
		t.Fatalf("support matrix VPP/FRR = %q/%q, want 24.10+/8.0+",
			byComponent["VPP"].Supported, byComponent["FRR"].Supported)
	}
}

func TestDeferredCompatibilityGates(t *testing.T) {
	gates := strings.Join(DeferredCompatibilityGates(), "\n")
	for _, want := range []string{
		"HA failover soak",
		"startup datastore",
	} {
		if !strings.Contains(gates, want) {
			t.Fatalf("DeferredCompatibilityGates() = %q, want %q", gates, want)
		}
	}
	if DeferredGateDocument != "docs/v0.11-deferred-gates.md" {
		t.Fatalf("DeferredGateDocument = %q, want docs/v0.11-deferred-gates.md", DeferredGateDocument)
	}
}
