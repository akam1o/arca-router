# Observability

arca-routerd exposes optional read-only observability endpoints. They are disabled by default.

## Prometheus

Start the daemon with a Prometheus listen address:

```bash
arca-routerd --metrics-listen=:9090
```

Prometheus can also be enabled from running configuration:

```text
set system services prometheus enabled true
set system services prometheus listen-address 127.0.0.1
set system services prometheus port 9090
```

Endpoints:

- `GET /metrics`
- `GET /healthz`

Exported metrics:

- `arca_routerd_up`
- `arca_routerd_uptime_seconds`
- `arca_router_config_version`
- `arca_router_config_sync_etcd_enabled`
- `arca_router_config_sync_etcd_healthy`
- `arca_router_config_sync_etcd_revision`
- `arca_router_config_sync_running_revision`
- `arca_router_config_sync_error`
- `arca_router_config_sync_last_check_timestamp_seconds`
- `arca_router_config_sync_last_apply_timestamp_seconds`
- `arca_router_cluster_enabled`
- `arca_router_cluster_nodes`
- `arca_router_cluster_sync_etcd_configured`
- `arca_router_cluster_sync_aligned`
- `arca_router_overlay_evpn_configured`
- `arca_router_overlay_evpn_vnis`
- `arca_router_overlay_evpn_l2_vnis`
- `arca_router_overlay_evpn_l3_vnis`
- `arca_router_overlay_evpn_multicast_vnis`
- `arca_router_ha_configured`
- `arca_router_ha_converged`
- `arca_router_ha_vrrp_groups`
- `arca_router_ha_convergence_issues`
- `arca_router_frr_vrrp_configured_groups`
- `arca_router_frr_vrrp_observed_groups`
- `arca_router_frr_vrrp_active_groups`
- `arca_router_frr_vrrp_issues`
- `arca_router_frr_vrrp_error`
- `arca_router_frr_vrrp_last_check_timestamp_seconds`
- `arca_router_frr_bfd_configured_peers`
- `arca_router_frr_bfd_observed_peers`
- `arca_router_frr_bfd_up_peers`
- `arca_router_frr_bfd_down_peers`
- `arca_router_frr_bfd_session_down_events`
- `arca_router_frr_bfd_rx_fail_packets`
- `arca_router_frr_bfd_issues`
- `arca_router_frr_bfd_error`
- `arca_router_frr_bfd_last_check_timestamp_seconds`
- `arca_router_vpp_lcp_pairs`
- `arca_router_vpp_lcp_inconsistencies`
- `arca_router_vpp_lcp_reconcile_error`
- `arca_router_vpp_lcp_last_reconcile_timestamp_seconds`
- `arca_router_class_of_service_configured`
- `arca_router_class_of_service_forwarding_classes`
- `arca_router_class_of_service_traffic_control_profiles`
- `arca_router_class_of_service_interface_bindings`
- `arca_router_class_of_service_intent_only`
- `arca_router_class_of_service_metadata_binding_supported`
- `arca_router_class_of_service_queue_scheduler_supported`
- `arca_router_class_of_service_policer_supported`
- `arca_router_class_of_service_counters_supported`
- `arca_router_class_of_service_capability_error`
- `arca_router_class_of_service_capability_last_check_timestamp_seconds`
- `arca_router_netconf_active_sessions`
- `arca_router_netconf_active_connections`
- `arca_router_netconf_total_connections`
- `arca_router_netconf_successful_handshakes`
- `arca_router_netconf_failed_handshakes`
- `arca_router_netconf_listening`

The packaged Grafana dashboard is installed at:

```text
/usr/share/arca-router/grafana/arca-routerd-dashboard.json
```

The dashboard includes EVPN/VXLAN overlay configured, total VNI, L2 VNI, L3 VNI, multicast VNI, class-of-service capability, and VPP LCP reconciliation panels backed by the Prometheus metrics endpoint.

Source path:

```text
observability/grafana/arca-routerd-dashboard.json
```

## gRPC Telemetry Stream

The internal Unix socket gRPC API exposes `TelemetryService.GetTelemetryCatalog` for stream discovery and `TelemetryService.SubscribeTelemetry` for local collectors and NMS sidecars. The catalog returns the event schema version, payload encoding, default paths, default/min/max sample interval hints in milliseconds, supported paths, descriptions, cardinality hints, per-path payload schema IDs, accepted aliases, and default membership. `GetTelemetryCatalog` accepts repeated path, cardinality, payload schema, and payload encoding filters plus a default-only filter when collectors only need a subset of the advertised paths; path filters match canonical paths or aliases such as `/evpn`. Events use the `arca.telemetry.v1` schema envelope with `sequence`, `timestamp`, `path`, `cardinality`, `payload_schema`, `event_type`, `encoding`, `json_payload`, and `payload_bytes` fields. Payloads are encoded as JSON. The `/class-of-service` payload includes VPP QoS capability support, diagnostics, errors, and last-check time alongside CoS intent.

Supported paths:

- `/system`
- `/config/running`
- `/interfaces`
- `/routes`
- `/routing/bgp/neighbors`
- `/routing/ospf/neighbors`
- `/routing/ospf3/neighbors`
- `/routing-instances`
- `/overlays/evpn`
- `/class-of-service`
- `/bfd`
- `/lcp`
- `/ha`

Subscriptions can select paths, set a sample interval, or request a one-shot snapshot. Empty path selection defaults to `/system` and `/config/running`. The server writes directly to the gRPC stream, so gRPC flow control is the backpressure boundary and arca-routerd does not build unbounded event buffers.

Local operators can inspect the same stream through the CLI. The command prints one JSON envelope per line:

```bash
arca show telemetry paths
arca show telemetry paths live
arca show telemetry paths default
arca show telemetry paths path /evpn
arca show telemetry paths cardinality per-route
arca show telemetry paths live payload-schema arca.telemetry.routes.v1
arca show telemetry paths encoding json
arca show telemetry path /system path /interfaces
arca show telemetry path /routes interval 5s count 3
arca show telemetry path /overlays/evpn
arca show evpn
```

`arca show telemetry paths` prints the same local path catalog used by the stream implementation, including sample interval hints, cardinality hints, payload schema IDs, default membership, and descriptions, without requiring a daemon connection. It accepts `default`, `path <path-or-alias>`, `cardinality <hint>`, `payload-schema <id>`, and `encoding <encoding>` filters. `arca show telemetry paths live` queries `TelemetryService.GetTelemetryCatalog` from the connected daemon and pushes those filters into that RPC.

## Web UI

Start the read-only Web UI with an explicit listen address:

```bash
arca-routerd --web-listen=127.0.0.1:8080
```

It can also be enabled through configuration:

```text
set system services web-ui enabled true
set system services web-ui listen-address 127.0.0.1
set system services web-ui port 8080
```

Endpoints:

- `GET /`
- `GET /api/config`
- `GET /api/config/history`
- `GET /api/status`
- `GET /api/nms/v1/status`
- `GET /api/nms/v1/telemetry/paths`
- `GET /api/nms/v1/telemetry/schemas`
- `GET /api/nms/v1/telemetry/snapshot`
- `POST /api/config/validate`
- `POST /api/config/commit`

The Web UI is intended for trusted management networks. It exposes the same daemon status used by the metrics endpoint, including datastore backend, etcd config sync health, cluster sync alignment, EVPN/VXLAN overlay intent counts, FRR VRRP and BFD operational state, class-of-service intent with VPP QoS capability diagnostics, HA convergence, and VPP LCP reconciliation state. It also exposes the running configuration in set-command format through `/api/config`, renders it in the dashboard editor, shows recent commit history from `/api/config/history`, and can validate or commit edited set-command text.

`/api/nms/v1/status` returns the same read-only operational status in a schema-versioned envelope with `schema_version`, `generated_at`, `resource`, and `data` fields. External NMS collectors should use this endpoint when they need a stable API shape instead of scraping the dashboard page.
`/api/nms/v1/telemetry/paths` returns the supported structured telemetry paths, `generated_at`, the filtered `path_count`, default path set, event schema version, payload encoding, default/min/max sample interval hints in milliseconds, per-path cardinality hints, payload schema IDs, and accepted aliases so collectors can discover stream inputs before subscribing over gRPC or invoking the CLI for local validation. The endpoint accepts `default=true` plus repeated or comma-separated `path`, `cardinality`, `payload_schema`, and `encoding` query parameters to return a filtered catalog; empty filter values are ignored, and path filters match canonical paths or aliases, for example `?path=/evpn`.
`/api/nms/v1/telemetry/schemas` returns the structured telemetry payload schema registry for collector validation and routing. The envelope includes `generated_at`, `default_paths`, default/min/max sample interval hints, and the filtered `schema_count`. Each schema entry includes the telemetry path, description, cardinality hint, payload schema ID, accepted aliases, default membership, and stable top-level JSON field names with type hints and descriptions. The endpoint accepts the same `default=true`, `path`, `cardinality`, `payload_schema`, and `encoding` filters as the path catalog.
`/api/nms/v1/telemetry/snapshot` returns a one-shot structured telemetry snapshot for HTTP-only collectors. Use repeated `path` query parameters, for example `?path=/system&path=/interfaces`; when no metadata filter is provided, omitting `path` uses the same default telemetry paths as the gRPC stream. `default=true`, `cardinality`, `payload_schema`, and `encoding` apply the same normalized catalog metadata filters directly to the snapshot path set, for example `?cardinality=per-route&payload_schema=arca.telemetry.routes.v1`. `timeout` defaults to `5s` and is capped at `30s`. `max_payload_bytes` defaults to `8388608` and is capped at `67108864`; `max_events` defaults to `64` and is capped at `1024`. The response echoes `generated_at`, `default_paths`, default/min/max sample interval hints, `event_count`, total `payload_bytes`, per-event `cardinality`, `payload_schema`, and `payload_bytes`, `max_payload_bytes`, `max_events`, and `timeout_ms`.

An HTTP-only collector example is included in `examples/nms`. It decodes catalog, schema, and snapshot default paths plus sample interval hints, validates NMS status envelope metadata, the status `data` object, required status data fields, status build metadata, sections, nested section fields, optional status arrays, non-empty status text, optional RFC3339 status section timestamps and generated_at timing bounds, optional status diagnostic metadata, status datastore consistency, status state and boolean consistency, status sync consistency, status counter relationships, and status aggregate counts, telemetry discovery, snapshot envelope metadata, RFC3339 `generated_at` timestamps, snapshot event timing bounds, catalog path metadata, known path alias, cardinality, and payload schema mappings, schema registry entries, unique payload field declarations, and per-event snapshot sequence, timestamp, path, cardinality, payload schema, encoding, and payload byte metadata, checks telemetry result counts, unique absolute default path lists and sample interval hints, unique emitted paths, payload byte totals, and non-negative capped advertised guardrails against decoded data, can pass include-default, path, cardinality, payload schema, or encoding filters into telemetry catalog and schema discovery, and pushes the same include filters directly to the snapshot endpoint when catalog exclusion or discovery is not needed. Schema discovery responses also echo default paths and sample interval hints for collectors that validate payloads before choosing a polling set. It can also exclude selected paths, aliases, cardinalities, payload schema IDs, or encodings before requesting a bounded snapshot; exclusions resolve catalog aliases before matching metadata. When exporting snapshots to OTLP/HTTP logs, the collector adds `arca.telemetry.cardinality` and `arca.telemetry.payload_schema` as log attributes.

HA convergence is evaluated when chassis clustering is enabled and at least one VRRP group is configured. The status is converged only when there are at least two cluster nodes, etcd cluster sync is configured and aligned with the daemon datastore, the etcd config synchronizer is healthy, FRR VRRP operational state reports every configured group as active, and VPP LCP reconciliation has run without errors or inconsistencies.

When the running configuration contains password-backed `security users`, the Web UI requires HTTP Basic authentication. The `read-only`, `operator`, and `admin` roles can access the read-only dashboard and API endpoints.

```bash
curl -u monitor:ReadOnly789 http://127.0.0.1:8080/api/status
curl -u monitor:ReadOnly789 http://127.0.0.1:8080/api/nms/v1/status
curl -u monitor:ReadOnly789 http://127.0.0.1:8080/api/nms/v1/telemetry/paths
curl -u monitor:ReadOnly789 'http://127.0.0.1:8080/api/nms/v1/telemetry/schemas?path=/evpn'
curl -u monitor:ReadOnly789 'http://127.0.0.1:8080/api/nms/v1/telemetry/snapshot?path=/system&path=/interfaces&path=/overlays/evpn&timeout=5s&max_payload_bytes=8388608&max_events=64'
curl -u monitor:ReadOnly789 http://127.0.0.1:8080/api/config
curl -u monitor:ReadOnly789 http://127.0.0.1:8080/api/config/history
```

Configuration writes require an `operator` or `admin` role. The Web API uses the same internal gRPC candidate workflow as the CLI: create a session, acquire the candidate lock, edit candidate text, validate, diff, and commit.

```bash
curl -u operator:OpPass456 \
  -H 'Content-Type: application/json' \
  -d '{"config_text":"set system host-name edge02"}' \
  http://127.0.0.1:8080/api/config/validate

curl -u operator:OpPass456 \
  -H 'Content-Type: application/json' \
  -d '{"config_text":"set system host-name edge02","message":"web update"}' \
  http://127.0.0.1:8080/api/config/commit
```

## SNMP

Start the daemon with an SNMP listen address:

```bash
arca-routerd --snmp-listen=:1161 --snmp-community=public
```

SNMP can also be enabled from running configuration:

```text
set system services snmp enabled true
set system services snmp listen-address 127.0.0.1
set system services snmp port 1161
set system services snmp community public
```

For the standard port 161, the packaged systemd unit already grants `CAP_NET_BIND_SERVICE`:

```bash
arca-routerd --snmp-listen=:161 --snmp-community=<read-only-community>
```

SNMP support is read-only SNMPv2c and is intended for monitoring only. Do not expose it on untrusted networks.

Standard MIB-II OIDs:

| OID | Name |
|-----|------|
| `1.3.6.1.2.1.1.1.0` | `sysDescr.0` |
| `1.3.6.1.2.1.1.2.0` | `sysObjectID.0` |
| `1.3.6.1.2.1.1.3.0` | `sysUpTime.0` |
| `1.3.6.1.2.1.1.5.0` | `sysName.0` |

arca-router custom OIDs currently use the provisional experimental base `1.3.6.1.3.9950.1` until an official enterprise OID is assigned:

| OID | Name |
|-----|------|
| `1.3.6.1.3.9950.1.1.0` | `arcaRouterdUp` |
| `1.3.6.1.3.9950.1.2.0` | `arcaRouterdUptime` |
| `1.3.6.1.3.9950.1.3.0` | `arcaRouterConfigVersion` |
| `1.3.6.1.3.9950.1.4.0` | `arcaRouterNetconfListening` |
| `1.3.6.1.3.9950.1.5.0` | `arcaRouterNetconfActiveSessions` |
| `1.3.6.1.3.9950.1.6.0` | `arcaRouterNetconfActiveConnections` |
| `1.3.6.1.3.9950.1.7.0` | `arcaRouterNetconfTotalConnections` |
| `1.3.6.1.3.9950.1.8.0` | `arcaRouterNetconfSuccessfulHandshakes` |
| `1.3.6.1.3.9950.1.9.0` | `arcaRouterNetconfFailedHandshakes` |
| `1.3.6.1.3.9950.1.10.0` | `arcaRouterdVersion` |
| `1.3.6.1.3.9950.1.11.0` | `arcaRouterVppLcpPairs` |
| `1.3.6.1.3.9950.1.12.0` | `arcaRouterVppLcpInconsistencies` |
| `1.3.6.1.3.9950.1.13.0` | `arcaRouterVppLcpReconcileError` |
| `1.3.6.1.3.9950.1.14.0` | `arcaRouterVppLcpLastReconcile` |
| `1.3.6.1.3.9950.1.15.0` | `arcaRouterHaConfigured` |
| `1.3.6.1.3.9950.1.16.0` | `arcaRouterHaConverged` |
| `1.3.6.1.3.9950.1.17.0` | `arcaRouterHaVrrpGroups` |
| `1.3.6.1.3.9950.1.18.0` | `arcaRouterHaConvergenceIssues` |
| `1.3.6.1.3.9950.1.19.0` | `arcaRouterFrrVrrpConfiguredGroups` |
| `1.3.6.1.3.9950.1.20.0` | `arcaRouterFrrVrrpObservedGroups` |
| `1.3.6.1.3.9950.1.21.0` | `arcaRouterFrrVrrpActiveGroups` |
| `1.3.6.1.3.9950.1.22.0` | `arcaRouterFrrVrrpConvergenceIssues` |
| `1.3.6.1.3.9950.1.23.0` | `arcaRouterFrrVrrpStatusError` |
| `1.3.6.1.3.9950.1.24.0` | `arcaRouterFrrVrrpLastCheck` |
| `1.3.6.1.3.9950.1.25.0` | `arcaRouterClassOfServiceConfigured` |
| `1.3.6.1.3.9950.1.26.0` | `arcaRouterClassOfServiceForwardingClasses` |
| `1.3.6.1.3.9950.1.27.0` | `arcaRouterClassOfServiceTrafficControlProfiles` |
| `1.3.6.1.3.9950.1.28.0` | `arcaRouterClassOfServiceInterfaceBindings` |
| `1.3.6.1.3.9950.1.29.0` | `arcaRouterClassOfServiceIntentOnly` |
| `1.3.6.1.3.9950.1.30.0` | `arcaRouterFrrBfdConfiguredPeers` |
| `1.3.6.1.3.9950.1.31.0` | `arcaRouterFrrBfdObservedPeers` |
| `1.3.6.1.3.9950.1.32.0` | `arcaRouterFrrBfdUpPeers` |
| `1.3.6.1.3.9950.1.33.0` | `arcaRouterFrrBfdDownPeers` |
| `1.3.6.1.3.9950.1.34.0` | `arcaRouterFrrBfdSessionDownEvents` |
| `1.3.6.1.3.9950.1.35.0` | `arcaRouterFrrBfdRxFailPackets` |
| `1.3.6.1.3.9950.1.36.0` | `arcaRouterFrrBfdConvergenceIssues` |
| `1.3.6.1.3.9950.1.37.0` | `arcaRouterFrrBfdStatusError` |
| `1.3.6.1.3.9950.1.38.0` | `arcaRouterFrrBfdLastCheck` |
| `1.3.6.1.3.9950.1.39.0` | `arcaRouterOverlayEvpnConfigured` |
| `1.3.6.1.3.9950.1.40.0` | `arcaRouterOverlayEvpnVnis` |
| `1.3.6.1.3.9950.1.41.0` | `arcaRouterOverlayEvpnL2Vnis` |
| `1.3.6.1.3.9950.1.42.0` | `arcaRouterOverlayEvpnL3Vnis` |
| `1.3.6.1.3.9950.1.43.0` | `arcaRouterOverlayEvpnMulticastVnis` |
| `1.3.6.1.3.9950.1.44.0` | `arcaRouterClassOfServiceMetadataBindingSupported` |
| `1.3.6.1.3.9950.1.45.0` | `arcaRouterClassOfServiceQueueSchedulerSupported` |
| `1.3.6.1.3.9950.1.46.0` | `arcaRouterClassOfServicePolicerSupported` |
| `1.3.6.1.3.9950.1.47.0` | `arcaRouterClassOfServiceCountersSupported` |
| `1.3.6.1.3.9950.1.48.0` | `arcaRouterClassOfServiceCapabilityError` |
| `1.3.6.1.3.9950.1.49.0` | `arcaRouterClassOfServiceCapabilityLastCheck` |

Example:

```bash
snmpget -v 2c -c public 127.0.0.1:1161 1.3.6.1.3.9950.1.3.0
snmpwalk -v 2c -c public 127.0.0.1:1161 1.3.6.1.3.9950.1
```
