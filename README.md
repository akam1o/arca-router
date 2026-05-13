# arca-router

[![Build and Test](https://github.com/akam1o/arca-router/actions/workflows/build.yml/badge.svg?branch=main)](https://github.com/akam1o/arca-router/actions/workflows/build.yml)
[![Release](https://github.com/akam1o/arca-router/actions/workflows/release.yml/badge.svg)](https://github.com/akam1o/arca-router/actions/workflows/release.yml)

English | [日本語](README.ja.md)

**High-Performance Software Router with Junos-like Configuration**

arca-router is a software router with Junos-compatible configuration syntax, powered by VPP (Vector Packet Processing) and FRR (Free Range Routing) for dynamic routing protocols.

---

## Status

arca-router is currently in the v0.6.x advanced features phase. This README
describes the current unified daemon path; detailed release history is kept in
[`CHANGELOG.md`](CHANGELOG.md), and future scope is tracked in
[`ROADMAP.md`](ROADMAP.md).

Current capabilities:

- Unified `arca-routerd` process for VPP, FRR, NETCONF, and gRPC
- Junos-like `set` configuration syntax with a thin `arca` client
- Struct-first configuration model with diff-based 2-phase commit and rollback
- FRR transactional apply through the management candidate datastore
- v0.6 config foundations for clustering, VRRP, MPLS, routing instances, and QoS
- Prometheus, health, SNMP, Web UI, and Grafana observability assets
- SQLite or etcd-backed candidate/running datastore with commit history and etcd config sync

---

## Prerequisites

### System Requirements

- **OS**: Debian 12 (Bookworm) or RHEL 9 / AlmaLinux 9 / Rocky Linux 9
- **CPU**: x86_64 with multi-core support (2+ cores recommended)
- **Memory**: 4GB+ RAM (VPP requires hugepages)
- **NIC**: Intel (AVF) or Mellanox (RDMA) compatible NICs

### Required Software

- **VPP 24.10+**: Vector Packet Processing framework
  - See [VPP Setup Guide (Debian)](docs/vpp-setup-debian.md) and [VPP Setup Guide (RHEL9)](docs/vpp-setup-rhel9.md)

- **FRR 8.0+**: Free Range Routing for dynamic routing protocols
  - See [FRR Setup Guide (Debian)](docs/frr-setup-debian.md) and [FRR Setup Guide (RHEL9)](docs/frr-setup-rhel9.md)
  - Enable `bgpd`, `ospfd`, `zebra`, `staticd`, `mgmtd`, and `vrrpd` in `/etc/frr/daemons`

- **Go 1.25+**: For building from source (optional)

---

## Quick Start (v0.6.x)

Requires VPP 24.10+ and FRR 8.0+ with the standard arca-router FRR daemon set enabled.

### 1. Install Prerequisites

**Debian Bookworm**:
```bash
# Install VPP 24.10
curl -s https://packagecloud.io/install/repositories/fdio/2410/script.deb.sh | sudo bash
sudo apt-get install -y vpp=24.10-release vpp-plugin-core=24.10-release

# Install FRR
sudo apt-get install -y frr frr-pythontools

# See detailed setup guides:
# - docs/vpp-setup-debian.md
# - docs/frr-setup-debian.md
```

> RHEL note: FD.io does not publish VPP 24.10 RPMs for RHEL9; build VPP from source per [docs/vpp-setup-rhel9.md](docs/vpp-setup-rhel9.md) before installing.

**RHEL 9 / AlmaLinux 9 / Rocky Linux 9**:
```bash
# Build VPP 24.10 RPMs from source (see docs/vpp-setup-rhel9.md), then install VPP + FRR
sudo dnf install -y /path/to/vpp-24.10-*.rpm /path/to/vpp-plugin-core-24.10-*.rpm frr frr-pythontools
```

### 2. Install arca-router

**Debian Bookworm**:
```bash
# Install DEB package
sudo dpkg -i arca-router_*.deb

# Verify installation
/usr/sbin/arca-routerd --version
arca --version
```

**RHEL 9 / AlmaLinux 9 / Rocky Linux 9**:
```bash
# Install RPM package
sudo dnf install -y ./arca-router-*.rpm

# Verify installation
/usr/sbin/arca-routerd --version
arca --version
```

To use `arca` as a non-root operator, add that login user to the
`arca-router` group and start a new login session:

```bash
sudo usermod -aG arca-router $USER
```

### 3. Configure Hardware Mapping

Copy and edit the example configuration:

```bash
# Copy example configs
sudo cp /etc/arca-router/hardware.yaml.example /etc/arca-router/hardware.yaml
sudo cp /etc/arca-router/arca-router.conf.example /etc/arca-router/arca-router.conf
```

Edit `/etc/arca-router/hardware.yaml`:

```yaml
interfaces:
  - name: "ge-0/0/0"
    pci: "0000:03:00.0"
    driver: "avf"
    description: "WAN Uplink"
  - name: "ge-0/0/1"
    pci: "0000:03:00.1"
    driver: "avf"
    description: "LAN Interface"
```

Find your NIC PCI addresses:

```bash
lspci | grep Ethernet
```

### 4. Configure Interfaces and Routing

Edit `/etc/arca-router/arca-router.conf` to configure interfaces and routing protocols:

```
# System configuration
set system host-name arca-router-01

# Interface configuration
set interfaces ge-0/0/0 description "WAN Uplink"
set interfaces ge-0/0/0 unit 0 family inet address 198.51.100.1/30
set interfaces ge-0/0/1 description "LAN Interface"
set interfaces ge-0/0/1 unit 0 family inet address 192.168.1.1/24

# Routing options
set routing-options autonomous-system 65000
set routing-options router-id 198.51.100.1

# BGP configuration
set protocols bgp group external type external
set protocols bgp group external neighbor 198.51.100.2 peer-as 65001
set protocols bgp group external neighbor 198.51.100.2 description "ISP Router"

# OSPF configuration
set protocols ospf area 0.0.0.0 interface ge-0/0/1
set protocols ospf router-id 198.51.100.1

# Static routes
set routing-options static route 0.0.0.0/0 next-hop 198.51.100.2
```

See [`examples/arca-router.conf`](examples/arca-router.conf) for a complete configuration example.

### 5. Start arca-router

```bash
# Start the service
sudo systemctl start arca-routerd

# Enable at boot
sudo systemctl enable arca-routerd

# Check status
sudo systemctl status arca-routerd

# View logs
sudo journalctl -u arca-routerd -f
```

### 6. (Optional) Configure NETCONF and Security

**Enable NETCONF Server**:

Edit `/etc/arca-router/arca-router.conf` to enable NETCONF and create users:

```
# Enable NETCONF on port 830
set security netconf ssh port 830

# Create admin user
set security users user admin password YourSecurePassword123
set security users user admin role admin

# Create operator user for automation
set security users user operator password OperatorPass456
set security users user operator role operator

# Rate limiting
set security rate-limit per-ip 10
set security rate-limit per-user 20
```

> NETCONF is built into `arca-routerd`; no separate NETCONF daemon is needed. When `--netconf-listen` is omitted, the daemon listens on the configured NETCONF port and falls back to `:830`.

**Test NETCONF connection**:

```bash
# Connect via NETCONF (requires netconf-console or similar client)
netconf-console --host localhost --port 830 --user admin --password YourSecurePassword123
```

### 7. Verify Configuration

```bash
# Check daemon logs
sudo journalctl -u arca-routerd -n 50

# View running configuration with arca
arca show configuration

# Check managed interface state, counters, QoS profile, and queue placement through arca-routerd
arca show interfaces
arca show route
arca show bgp summary
arca show ospf neighbor
arca show vrrp
arca show lcp
arca show ha

# Check VPP/FRR directly (optional)
sudo vppctl show interface
sudo vppctl show lcp
sudo vtysh -c 'show running-config'
sudo vtysh -c 'show ip route'
```

---

## Configuration Reference

The full configuration syntax and supported `set` hierarchy is documented in [`SPEC.md`](SPEC.md).

Top-level stanzas:

- `system`
- `interfaces`
- `routing-options`
- `protocols`
- `policy-options`
- `security`

### Interface Naming Convention

- `ge-X/Y/Z`: Gigabit Ethernet (1GbE)
- `xe-X/Y/Z`: 10 Gigabit Ethernet (10GbE)
- `et-X/Y/Z`: 100 Gigabit Ethernet (100GbE)

---

## Building from Source

### Prerequisites

- Go 1.25+
- NFPM 2.35.0+ (for DEB/RPM packaging)

### Build Steps

```bash
# Clone repository
git clone https://github.com/akam1o/arca-router.git
cd arca-router

# Build binaries
make build

# Run tests
make test

# Build DEB package (nfpm config: build/package/nfpm.yaml)
make deb

# Build RPM package
make rpm

# Packages will be in dist/ directory
ls -lh dist/
```

### Makefile Targets

```bash
make help             # Show all available targets
make version          # Display version information
make build            # Build v0.5.x unified daemon + CLI
make build-cli        # Build only current arca CLI
make generate-proto   # Generate typed gRPC bindings
make test             # Run unit tests
make integration-test # Run integration tests
make package-lint     # Validate package metadata and service expectations
make frr-mgmtd-smoke  # Run live FRR mgmtd smoke test
make fmt              # Format code
make vet              # Run go vet
make check            # Run all checks (fmt, vet, test)
make clean            # Clean build artifacts
make install-nfpm     # Install NFPM tool
make deb              # Build DEB package
make deb-test         # Test DEB package metadata
make deb-verify       # Verify DEB package reproducibility
make rpm              # Build RPM package
make rpm-test         # Test RPM package metadata
make rpm-verify       # Verify reproducible build
make packages         # Build both RPM and DEB packages
```

---

## Project Structure

```
arca-router/
├── api/
│   └── v1/
│       └── router.proto        # gRPC API definitions (Config/Session/State)
├── cmd/
│   ├── arca-routerd/           # Unified daemon (v0.5.x)
│   │   └── main.go             # Single process: VPP + FRR + NETCONF + gRPC
│   └── arca/                   # Thin gRPC CLI client (v0.5.x)
│       └── main.go             # Communicates via Unix socket
├── internal/                   # v0.5.x core packages
│   ├── model/                  # Canonical config & state types
│   │   ├── config.go           # RouterConfig (struct-first model)
│   │   ├── state.go            # OperationalState
│   │   ├── validate.go         # Validation logic
│   │   └── convert.go          # Text config ↔ canonical model conversion
│   ├── engine/                 # Config engine
│   │   ├── engine.go           # 2-phase commit, atomic apply
│   │   ├── diff.go             # Minimal diff computation
│   │   └── plugin.go           # Southbound plugin interface
│   ├── southbound/
│   │   ├── vpp/plugin.go       # VPP plugin (govpp)
│   │   └── frr/plugin.go       # FRR plugin (transactional/file apply)
│   ├── northbound/
│   │   └── grpc/               # gRPC server + client
│   │       ├── server.go       # Session mgmt, config ops
│   │       └── client.go       # Thin client for CLI
│   ├── store/                  # Persistence abstraction
│   │   ├── store.go            # ConfigStore interface
│   │   └── sqlite/sqlite.go    # SQLite backend
│   └── auth/auth.go            # Auth/RBAC/audit wrapper
├── pkg/                        # Reusable packages used by daemon and CLI
│   ├── config/                 # Set-command parser
│   ├── vpp/                    # VPP client interface
│   ├── frr/                    # FRR config generator
│   ├── datastore/              # SQLite/etcd datastore
│   ├── netconf/                # NETCONF/SSH server
│   ├── cli/                    # CLI session management
│   ├── auth/                   # Password/SSH key auth
│   ├── audit/                  # Audit logging
│   ├── device/                 # Hardware abstraction
│   ├── logger/                 # Structured logging
│   └── errors/                 # Error handling
├── build/
│   ├── systemd/                # systemd unit files
│   └── package/                # nfpm config and scripts
├── docs/                       # Documentation
├── examples/                   # Sample configurations
└── Makefile                    # Build automation
```

---

## Documentation

- [Documentation Index](docs/README.md) - All docs in one place
- [Roadmap](ROADMAP.md) - Planned feature scope
- [VPP Setup Guide for Debian](docs/vpp-setup-debian.md) - VPP installation for Debian
- [VPP Setup Guide for RHEL9](docs/vpp-setup-rhel9.md) - VPP installation for RHEL9
- [FRR Setup Guide for Debian](docs/frr-setup-debian.md) - FRR installation for Debian
- [FRR Setup Guide for RHEL9](docs/frr-setup-rhel9.md) - FRR installation for RHEL9
- [Design Specification](SPEC.md) - Architecture and design decisions
- [JSON Schema Convention](docs/json-schema-convention.md) - Naming conventions
- [Changelog](CHANGELOG.md) - Release history
- [Support Policy](SUPPORT.md) - Support channels and expectations

---

## Contributing

Contributions are welcome! See [`CONTRIBUTING.md`](CONTRIBUTING.md).

---

## License

Licensed under the Apache License 2.0. See [`LICENSE`](LICENSE).

---

## Support

- **Community Support**: GitHub Issues - https://github.com/akam1o/arca-router/issues
- **Support Policy**: See [`SUPPORT.md`](SUPPORT.md)
- **Security**: See [`SECURITY.md`](SECURITY.md)

---

## Acknowledgments

- **VPP**: [FD.io Vector Packet Processing](https://fd.io/)
- **FRR**: [Free Range Routing](https://frrouting.org/)
- **NFPM**: [GoReleaser NFPM](https://nfpm.goreleaser.com/)
