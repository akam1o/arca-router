# VPP Setup Guide for RHEL 9

This guide covers VPP 24.10 installation and configuration for arca-router v0.3.x.

**Status**: v0.3.x - VPP is **required**

**Note**: FD.io does **not** publish RHEL 9 / AlmaLinux 9 / Rocky Linux 9 RPMs for VPP 24.10. You must build the RPMs from source as described below.

---

## Prerequisites

- RHEL 9 / AlmaLinux 9 / Rocky Linux 9 (x86_64)
- Root or sudo access
- Internet connection for package installation

---

## Build and Install VPP from Source

### 1. Install Build Dependencies

```bash
# Enable CodeReady Builder (for devel packages)
sudo dnf config-manager --set-enabled crb

# Base build tooling
sudo dnf groupinstall -y "Development Tools"

# VPP build dependencies (minimal set)
sudo dnf install -y \
  git cmake ninja-build python3 python3-pip \
  elfutils-libelf-devel numactl-devel libpcap-devel \
  libmnl-devel libuuid-devel libcap-ng-devel openssl-devel \
  kernel-devel kernel-headers libunwind-devel
```

### 2. Fetch VPP 24.10 Source

```bash
# Clone VPP (or download the v24.10 source tarball)
git clone https://gerrit.fd.io/r/vpp
cd vpp
git checkout v24.10
```

### 3. Build RPM Packages

```bash
# Install any remaining deps via VPP helper (optional, best effort)
make UNATTENDED=y install-dep

# Build release RPMs
make pkg-rpm BUILD_TYPE=release
```

Artifacts are placed under `build-root/` (e.g., `build-root/vpp-24.10-release.x86_64.rpm`).

### 4. Install VPP RPMs

```bash
cd build-root
sudo dnf install -y \
  vpp-24.10-*.rpm \
  vpp-plugin-core-24.10-*.rpm

# Verify installation
vpp -version
# Expected output: vpp v24.10-release built by ...
```

**Required packages**:
- `vpp`: VPP core daemon
- `vpp-plugin-core`: Core plugins including `linux-cp` (LCP)

---

## Configuration

### 5. Configure VPP Startup

Edit `/etc/vpp/startup.conf`:

```bash
sudo vi /etc/vpp/startup.conf
```

**Required configuration for arca-router**:

```
unix {
  nodaemon
  log /var/log/vpp/vpp.log
  full-coredump
  cli-listen /run/vpp/cli.sock

  ## API socket configuration
  ## IMPORTANT: arca-router requires group 'vpp' for API socket access
  api-segment {
    gid vpp
  }
}

api-trace {
  on
}

## Linux Control Plane (LCP) plugin configuration
## REQUIRED: LCP creates TAP interfaces for FRR integration
linux-cp {
  lcp-sync
  lcp-auto-subint
}

cpu {
  main-core 0
  corelist-workers 1-3
}

## Hardware-specific configuration (optional)
# dpdk {
#   dev 0000:03:00.0
#   dev 0000:03:00.1
# }

# rdma {
#   dev 0000:3b:00.0
#   dev 0000:3b:00.1
# }
```

**Key settings**:
- `api-segment { gid vpp }`: Allows `arca-router` user (member of `vpp` group) to access API socket
- `linux-cp { lcp-sync lcp-auto-subint }`: Enables LCP plugin for Linux kernel integration

### 6. Create VPP Group and Add User

**Note**: The `arca-router` user will be created automatically when you install the `arca-router` package. If you want to test VPP before installing `arca-router`, skip the `usermod` command and come back after package installation.

```bash
# Create vpp group (usually created by VPP package)
sudo groupadd vpp 2>/dev/null || true

# Add arca-router user to vpp group (after arca-router package installation)
sudo usermod -aG vpp arca-router
```

### 7. Start VPP Service

```bash
# Enable VPP to start on boot
sudo systemctl enable vpp

# Start VPP
sudo systemctl start vpp

# Check status
sudo systemctl status vpp
```

### 8. Verify VPP is Running

```bash
# Check API socket exists and has correct permissions
ls -l /run/vpp/api.sock
# Expected: srwxrwx--- 1 root vpp 0 ... /run/vpp/api.sock

# Test VPP CLI
sudo vppctl show version
# Expected: vpp v24.10-release ...

# Verify LCP plugin is loaded
sudo vppctl show plugins | grep linux-cp
# Expected: linux-cp_plugin.so
```

---

## Troubleshooting

### VPP Service Fails to Start

**Check VPP logs**:
```bash
sudo journalctl -u vpp -n 50
sudo tail -f /var/log/vpp/vpp.log
```

**Common issues**:
- **DPDK initialization failure**: Comment out `dpdk` section if no physical NICs
- **Hugepages not configured**: VPP requires hugepages for memory allocation

### API Socket Permission Denied

**Symptom**: `arca-routerd` fails with "permission denied" on `/run/vpp/api.sock`

**Solution**:
```bash
# Check socket group
ls -l /run/vpp/api.sock

# If group is not 'vpp', update /etc/vpp/startup.conf:
#   api-segment { gid vpp }
sudo systemctl restart vpp
```

### LCP Plugin Not Loaded

**Symptom**: `vppctl show plugins | grep linux-cp` returns nothing

**Solution**:
```bash
# Verify vpp-plugin-core is installed
rpm -qa | grep vpp-plugin-core

# If not installed:
sudo dnf install vpp-plugin-core

# Restart VPP
sudo systemctl restart vpp
```

### Hugepages Not Configured

**Symptom**: VPP logs show hugepage allocation failure

**Solution**:
```bash
# Configure 1024 2MB hugepages
echo 1024 | sudo tee /sys/kernel/mm/hugepages/hugepages-2048kB/nr_hugepages

# Make persistent across reboots
echo "vm.nr_hugepages=1024" | sudo tee -a /etc/sysctl.conf
sudo sysctl -p
```

Verify:
```bash
cat /proc/meminfo | grep Huge
```

---

## Next Steps

After VPP is running:

1. **Install FRR**: Install `frr`/`frr-pythontools` from your distribution repositories
2. **Configure arca-router**: Edit `/etc/arca-router/arca-router.conf` and `/etc/arca-router/hardware.yaml`
3. **Start arca-router**: `sudo systemctl start arca-routerd`
