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
- `arca_router_ha_configured`
- `arca_router_ha_converged`
- `arca_router_ha_vrrp_groups`
- `arca_router_ha_convergence_issues`
- `arca_router_vpp_lcp_pairs`
- `arca_router_vpp_lcp_inconsistencies`
- `arca_router_vpp_lcp_reconcile_error`
- `arca_router_vpp_lcp_last_reconcile_timestamp_seconds`
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

Source path:

```text
observability/grafana/arca-routerd-dashboard.json
```

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
- `POST /api/config/validate`
- `POST /api/config/commit`

The Web UI is intended for trusted management networks. It exposes the same daemon status used by the metrics endpoint, including datastore backend, etcd config sync health, cluster sync alignment, HA convergence, and VPP LCP reconciliation state. It also exposes the running configuration in set-command format through `/api/config`, renders it in the dashboard editor, shows recent commit history from `/api/config/history`, and can validate or commit edited set-command text.

HA convergence is evaluated when chassis clustering is enabled and at least one VRRP group is configured. The status is converged only when there are at least two cluster nodes, etcd cluster sync is configured and aligned with the daemon datastore, the etcd config synchronizer is healthy, and VPP LCP reconciliation has run without errors or inconsistencies.

When the running configuration contains password-backed `security users`, the Web UI requires HTTP Basic authentication. The `read-only`, `operator`, and `admin` roles can access the read-only dashboard and API endpoints.

```bash
curl -u monitor:ReadOnly789 http://127.0.0.1:8080/api/status
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

Example:

```bash
snmpget -v 2c -c public 127.0.0.1:1161 1.3.6.1.3.9950.1.3.0
snmpwalk -v 2c -c public 127.0.0.1:1161 1.3.6.1.3.9950.1
```
