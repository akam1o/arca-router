# NMS Examples

This directory contains minimal HTTP-only collector examples for the schema-versioned NMS API exposed by `arca-routerd` when the Web API is enabled.

## HTTP Telemetry Collector

`http_telemetry_collector.go` uses only the Go standard library. It can query the operational status envelope, telemetry path catalog, or one-shot telemetry snapshot endpoint.

```bash
# Discover supported telemetry paths, cardinality hints, and payload schema IDs.
go run ./examples/nms -mode catalog -base-url http://127.0.0.1:8080 -user monitor -password ReadOnly789

# Read the stable operational status envelope.
go run ./examples/nms -mode status -base-url http://127.0.0.1:8080 -user monitor -password ReadOnly789

# Collect a bounded one-shot telemetry snapshot.
go run ./examples/nms \
  -base-url http://127.0.0.1:8080 \
  -user monitor \
  -password ReadOnly789 \
  -path /system \
  -path /interfaces \
  -path /overlays/evpn \
  -timeout 5s \
  -max-payload-bytes 8388608

# Discover only selected default paths and path classes using server-side catalog filters.
go run ./examples/nms \
  -base-url http://127.0.0.1:8080 \
  -user monitor \
  -password ReadOnly789 \
  -include-default \
  -include-path /evpn \
  -include-cardinality per-vni \
  -include-payload-schema arca.telemetry.overlays.evpn.v1 \
  -timeout 5s \
  -max-payload-bytes 8388608

# Discover all paths from the catalog, but skip high-cardinality route snapshots.
go run ./examples/nms \
  -base-url http://127.0.0.1:8080 \
  -user monitor \
  -password ReadOnly789 \
  -discover-paths \
  -exclude-cardinality per-route \
  -timeout 5s \
  -max-payload-bytes 8388608

# Discover all paths from the catalog, but skip selected payload schemas.
go run ./examples/nms \
  -base-url http://127.0.0.1:8080 \
  -user monitor \
  -password ReadOnly789 \
  -discover-paths \
  -exclude-payload-schema arca.telemetry.routes.v1 \
  -exclude-payload-schema arca.telemetry.bfd.v1 \
  -timeout 5s \
  -max-payload-bytes 8388608
```

The example prints the returned JSON envelope with indentation so it can be piped into downstream tooling or inspected during collector integration tests.
