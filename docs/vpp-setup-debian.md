# VPP Setup Guide for Debian Bookworm

This guide covers VPP 24.10 installation and configuration for arca-router v0.3.x.

**Status**: v0.3.x - VPP is **required**

---

## Prerequisites

- Debian 12 (Bookworm) x86_64
- Root or sudo access
- Internet connection for package installation

---

## Installation

### 1. Add VPP Repository

VPP 24.10 is available from FD.io's packagecloud repository:

```bash
# Install prerequisites
sudo apt-get update
sudo apt-get install -y curl gnupg2

# Add FD.io 24.10 repository
curl -s https://packagecloud.io/install/repositories/fdio/2410/script.deb.sh | sudo bash
```

### 2. Install VPP Packages

```bash
# Install VPP core and required plugins
sudo apt-get install -y \
    vpp=24.10-release \
    vpp-plugin-core=24.10-release

# Verify installation
vpp -version
# Expected output: vpp v24.10-release built by ...
```

**Required packages**:
- `vpp`: VPP core daemon
- `vpp-plugin-core`: Core plugins including `linux-cp` (LCP)

---

## Configuration

### 3. Configure VPP Startup

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
  main-core 1
  corelist-workers 2-3
}

## DPDK configuration (optional - for physical NICs)
# dpdk {
#   dev 0000:00:08.0
#   dev 0000:00:09.0
# }
```

**Key settings**:
- `api-segment { gid vpp }`: Allows `arca-router` user (member of `vpp` group) to access API socket
- `linux-cp { lcp-sync lcp-auto-subint }`: Enables LCP plugin for Linux kernel integration

### 4. Create VPP Group and Add User

**Note**: The `arca-router` user will be created automatically when you install the `arca-router` package. If you want to test VPP before installing `arca-router`, skip the `usermod` command and come back after package installation.

```bash
# Create vpp group (usually created by VPP package)
sudo groupadd vpp 2>/dev/null || true

# Add arca-router user to vpp group (after arca-router package installation)
sudo usermod -aG vpp arca-router
```

### 5. Start VPP Service

```bash
# Enable VPP to start on boot
sudo systemctl enable vpp

# Start VPP
sudo systemctl start vpp

# Check status
sudo systemctl status vpp
```

### 6. Verify VPP is Running

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
dpkg -l | grep vpp-plugin-core

# If not installed:
sudo apt-get install vpp-plugin-core=24.10-release

# Restart VPP
sudo systemctl restart vpp
```

---

## Next Steps

After VPP is running:

1. **Install FRR**: See [docs/frr-setup-debian.md](frr-setup-debian.md)
2. **Configure arca-router**: Edit `/etc/arca-router/arca-router.conf` and `/etc/arca-router/hardware.yaml`
3. **Start arca-router**: `sudo systemctl start arca-routerd`

---

## References

- [VPP Official Documentation](https://fd.io/docs/vpp/)
- [VPP Linux Control Plane (LCP) Plugin](https://fd.io/docs/vpp/latest/d0/d05/linux_cp_doc.html)
- [FD.io Package Repository](https://packagecloud.io/fdio)
