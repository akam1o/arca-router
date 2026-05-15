# NMS Examples

This directory contains minimal HTTP-only collector examples for the schema-versioned NMS API exposed by `arca-routerd` when the Web API is enabled.

## HTTP Telemetry Collector

`http_telemetry_collector.go` uses only the Go standard library. It can query the operational status envelope, telemetry path catalog, telemetry payload schema registry, or one-shot telemetry snapshot endpoint.

```bash
# Discover supported telemetry paths, sample interval hints, cardinality hints, and payload schema IDs.
go run ./examples/nms -mode catalog -base-url http://127.0.0.1:8080 -user monitor -password ReadOnly789

# Discover stable top-level payload fields for selected telemetry schemas.
go run ./examples/nms -mode schemas -base-url http://127.0.0.1:8080 -user monitor -password ReadOnly789 -include-path /evpn

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
  -max-payload-bytes 8388608 \
  -max-events 64

# Collect the same snapshot and forward its events to an OTLP/HTTP logs endpoint.
go run ./examples/nms \
  -base-url http://127.0.0.1:8080 \
  -user monitor \
  -password ReadOnly789 \
  -path /system \
  -path /interfaces \
  -timeout 5s \
  -max-payload-bytes 8388608 \
  -max-events 64 \
  -otlp-endpoint http://127.0.0.1:4318/v1/logs \
  -otlp-service-name arca-router-nms-collector

# Request selected path classes using server-side snapshot metadata filters.
go run ./examples/nms \
  -base-url http://127.0.0.1:8080 \
  -user monitor \
  -password ReadOnly789 \
  -include-path /evpn \
  -include-cardinality per-vni \
  -include-payload-schema arca.telemetry.overlays.evpn.v1 \
  -include-encoding json \
  -timeout 5s \
  -max-payload-bytes 8388608 \
  -max-events 64

# Discover all paths from the catalog, but skip selected paths and high-cardinality route snapshots.
go run ./examples/nms \
  -base-url http://127.0.0.1:8080 \
  -user monitor \
  -password ReadOnly789 \
  -discover-paths \
  -exclude-path /bfd \
  -exclude-cardinality per-route \
  -timeout 5s \
  -max-payload-bytes 8388608 \
  -max-events 64

# Discover all paths from the catalog, but skip selected payload schemas.
go run ./examples/nms \
  -base-url http://127.0.0.1:8080 \
  -user monitor \
  -password ReadOnly789 \
  -discover-paths \
  -exclude-payload-schema arca.telemetry.routes.v1 \
  -exclude-payload-schema arca.telemetry.bfd.v1 \
  -exclude-encoding protobuf \
  -timeout 5s \
  -max-payload-bytes 8388608 \
  -max-events 64
```

The example prints the returned JSON envelope with indentation so it can be piped into downstream tooling or inspected during collector integration tests. It decodes catalog, schema, and snapshot default path hints for integration coverage, validates NMS status, telemetry discovery, and snapshot `schema_version` and `resource` values, and checks telemetry result counts against decoded arrays. Catalog and schema envelopes include filtered result counts, snapshot envelopes include `event_count`, `default_paths`, and sample interval hints, events include `cardinality` and `payload_schema`, and OTLP exports copy them to the `arca.telemetry.cardinality` and `arca.telemetry.payload_schema` log attributes for routing and validation.
