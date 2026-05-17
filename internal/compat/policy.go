// Package compat centralizes the v0.10 compatibility policy that is shown by
// CLI preflight commands and mirrored in release documentation.
package compat

const (
	PolicyPhase               = "v0.10.x stabilization and compatibility"
	CurrentSQLiteSchema       = 2
	TelemetryEventSchema      = "arca.telemetry.v1"
	NMSOperationalSchema      = "arca.nms.operational.v1"
	NMSTelemetryCatalogSchema = "arca.nms.telemetry-catalog.v1"
	NMSTelemetrySchemaCatalog = "arca.nms.telemetry-schemas.v1"
	NMSTelemetrySnapshot      = "arca.nms.telemetry-snapshot.v1"
	AuditSchema               = "arca.audit.v1"
	GRPCAPIPackage            = "arca.router.v1"
)

// Policy describes the release compatibility guarantees for this build line.
type Policy struct {
	Phase                         string
	SupportedDirectUpgradeSources []string
	UnsupportedDirectUpgradeNote  string
	ConfigCompatibility           string
	CLICompatibility              string
	APIVersioning                 string
	DeprecationPolicy             string
}

// ComponentSupport describes one external compatibility dependency.
type ComponentSupport struct {
	Component string
	Supported string
	Required  string
	Notes     string
}

// CurrentPolicy returns the v0.10 compatibility policy.
func CurrentPolicy() Policy {
	return Policy{
		Phase:                         PolicyPhase,
		SupportedDirectUpgradeSources: []string{"v0.8.x", "v0.9.x"},
		UnsupportedDirectUpgradeNote:  "v0.7.x and older require an intermediate validated upgrade before entering v0.10.x",
		ConfigCompatibility:           "documented set-command syntax and NETCONF configuration XML remain backward-compatible within v0.x unless a release note explicitly calls out a change",
		CLICompatibility:              "documented operational commands remain scriptable; automation should prefer gRPC, NETCONF, or schema-versioned NMS JSON where available",
		APIVersioning:                 "gRPC uses arca.router.v1; telemetry and NMS JSON schemas are additive within their v1 schema IDs",
		DeprecationPolicy:             "removals require release-note documentation and at least one minor release with a deprecation warning or compatibility alias",
	}
}

// ComponentMatrix returns the supported v0.10 external dependency matrix.
func ComponentMatrix() []ComponentSupport {
	return []ComponentSupport{
		{
			Component: "VPP",
			Supported: "24.10+",
			Required:  "vpp, vpp-plugin-core, linux-cp plugin",
			Notes:     "QoS scheduler, policer, and counter enforcement stay capability-gated by the detected binapi surface",
		},
		{
			Component: "FRR",
			Supported: "8.0+",
			Required:  "bgpd, ospfd, ospf6d, zebra, staticd, mgmtd, vrrpd, bfdd",
			Notes:     "transactional mgmtd is the default apply path; file backend remains a recovery compatibility path",
		},
		{
			Component: "SQLite datastore",
			Supported: "schema 1-2",
			Required:  "current schema 2",
			Notes:     "newer schemas are rejected so older binaries do not silently open a future datastore",
		},
		{
			Component: "NETCONF",
			Supported: "base:1.0 and base:1.1",
			Required:  "candidate, validate, rollback-on-error",
			Notes:     "standard :xpath and startup datastore capabilities remain unadvertised until full RFC behavior is implemented",
		},
	}
}
