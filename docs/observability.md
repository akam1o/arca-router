# Observability

arca-routerd exposes optional read-only observability endpoints. They are disabled by default.

## Prometheus

Start the daemon with a Prometheus listen address:

```bash
arca-routerd --metrics-listen=:9090
```

Endpoints:

- `GET /metrics`
- `GET /healthz`

Exported metrics:

- `arca_routerd_up`
- `arca_routerd_uptime_seconds`
- `arca_router_config_version`
- `arca_router_cluster_enabled`
- `arca_router_cluster_nodes`
- `arca_router_cluster_sync_etcd_configured`
- `arca_router_cluster_sync_aligned`
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
- `GET /api/status`

The Web UI is intended for trusted management networks. It exposes the same daemon status used by the metrics endpoint, including datastore backend and cluster sync alignment, and does not provide authentication yet.

## SNMP

Start the daemon with an SNMP listen address:

```bash
arca-routerd --snmp-listen=:1161 --snmp-community=public
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

Example:

```bash
snmpget -v 2c -c public 127.0.0.1:1161 1.3.6.1.3.9950.1.3.0
snmpwalk -v 2c -c public 127.0.0.1:1161 1.3.6.1.3.9950.1
```
