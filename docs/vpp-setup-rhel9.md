# VPP Setup Guide for RHEL 9

**Target OS**: RHEL 9 / AlmaLinux 9 / Rocky Linux 9
**VPP Version**: 23.10 or later

## ⚠️ Phase 1 Note (IMPORTANT)

**ARCA Router v0.1.x (Phase 1) does NOT require VPP installation.**

Phase 1 runs in **mock VPP mode by default** for testing and development purposes.
No special flags are needed - mock mode is the default behavior in Phase 1.
The RPM package has NO VPP dependency and will install successfully without VPP.

This document is provided for **Phase 2 reference only** and will be required when real VPP integration is implemented.

---

## Prerequisites (Phase 2 Only)

The following prerequisites apply to Phase 2 when real VPP integration is enabled:

- RHEL 9 based system with root access
- Internet connectivity for package installation
- Compatible NIC hardware (Intel AVF or Mellanox RDMA)

---

## 1. Add FD.io PackageCloud Repository

```bash
# Add FD.io repository
curl -s https://packagecloud.io/install/repositories/fdio/release/script.rpm.sh | sudo bash

# Verify repository
sudo yum repolist | grep fdio
```

---

## 2. Install VPP Packages

```bash
# Install VPP core and plugins
sudo yum install -y vpp vpp-plugins vpp-devel

# Verify installation
vpp -version
# Expected output: vpp v23.10 or later
```

### Required VPP Plugins

The following plugins are required for arca-router:

- **avf**: Intel Adaptive Virtual Function driver
- **dpdk**: Data Plane Development Kit support
- **lcp**: Linux Control Plane plugin (for FRR integration)

These are typically included in the `vpp-plugins` package.

---

## 3. Configure VPP Startup

Edit `/etc/vpp/startup.conf`:

```bash
sudo vi /etc/vpp/startup.conf
```

### Minimal Configuration Example (Native Drivers)

```
unix {
  nodaemon
  log /var/log/vpp/vpp.log
  full-coredump
  cli-listen /run/vpp/cli.sock
  gid vpp
}

api-trace {
  on
}

api-segment {
  gid vpp
}

cpu {
  main-core 0
  corelist-workers 1-3
}

# For Intel NICs (AVF - Native driver)
dpdk {
  dev 0000:03:00.0 { name ge-0-0-0 }
  dev 0000:03:00.1 { name ge-0-0-1 }
}

# For Mellanox NICs (RDMA - Native driver)
# Uncomment if using Mellanox:
# rdma {
#   dev 0000:3b:00.0 { name xe-0-1-0 }
#   dev 0000:3b:00.1 { name xe-0-1-1 }
# }

plugins {
  plugin dpdk_plugin.so { enable }
  plugin linux_cp_plugin.so { enable }
}
```

**Note**:
- Replace PCI addresses (`0000:03:00.0`) with your actual NIC addresses
- Use `dpdk {}` section for Intel NICs with native AVF driver
- Use `rdma {}` section for Mellanox NICs with native RDMA driver

---

## 4. Configure Hugepages

VPP requires hugepages for high-performance packet processing:

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

## 5. Start VPP Service

```bash
# Enable and start VPP
sudo systemctl enable vpp
sudo systemctl start vpp

# Check status
sudo systemctl status vpp

# Verify VPP is running
sudo vppctl show version
```

---

## 6. Verify VPP Installation

```bash
# List interfaces
sudo vppctl show interface

# Check plugins
sudo vppctl show plugins

# Expected plugins: dpdk_plugin.so, linux_cp_plugin.so (enabled)
```

---

## 7. Hardware-Specific Configuration

arca-router uses **native kernel drivers** (not DPDK) for optimal compatibility and simplicity.

### For Intel NICs (AVF - Adaptive Virtual Function)

Intel NICs use the native **AVF driver** (kernel module: `iavf` or `ice`).

**No DPDK binding required** - VPP will access NICs directly through the kernel driver.

```bash
# Verify Intel NIC driver
lspci -k | grep -A 3 Ethernet

# Expected output should show kernel driver: ice or iavf
# Example:
# 03:00.0 Ethernet controller: Intel Corporation ...
#   Kernel driver in use: ice
```

**Configuration in VPP** (`/etc/vpp/startup.conf`):

```
dpdk {
  # For Intel NICs with AVF, use dev with interface name
  dev 0000:03:00.0 { name eth0 }
  dev 0000:03:00.1 { name eth1 }
}
```

### For Mellanox NICs (RDMA)

Mellanox NICs use native **RDMA drivers** (kernel module: `mlx5_core`).

**No DPDK binding required** - VPP uses native RDMA interface.

```bash
# Verify Mellanox driver
lspci -k | grep -A 3 Mellanox

# Expected output:
# Kernel driver in use: mlx5_core

# Install MLNX_OFED if needed
# Download from: https://network.nvidia.com/products/infiniband-drivers/linux/mlnx_ofed/
```

**Configuration in VPP** (`/etc/vpp/startup.conf`):

```
rdma {
  dev 0000:3b:00.0 { name mlx0 }
  dev 0000:3b:00.1 { name mlx1 }
}
```

### DPDK Mode (Optional, Advanced)

If you specifically need DPDK mode (not recommended for Phase 1):

```bash
# Bind to vfio-pci (DPDK mode only)
sudo modprobe vfio-pci
echo "0000:03:00.0" | sudo tee /sys/bus/pci/drivers/ice/unbind
echo "8086 1889" | sudo tee /sys/bus/pci/drivers/vfio-pci/new_id
```

**Note**: arca-router Phase 1 prioritizes native drivers for simplicity.

---

## 8. Troubleshooting

### VPP fails to start

```bash
# Check logs
sudo journalctl -u vpp -n 50

# Check VPP log file
sudo cat /var/log/vpp/vpp.log
```

### Hugepages not allocated

```bash
# Verify hugepage configuration
cat /proc/meminfo | grep Huge

# Check available hugepages
cat /sys/kernel/mm/hugepages/hugepages-2048kB/nr_hugepages
```

### NIC not detected

```bash
# Verify PCI device
lspci -nn | grep Ethernet

# Check if bound to correct driver
lspci -k -s 0000:03:00.0
```

---

## 9. Security Considerations

### SELinux

If SELinux is enabled, you may need to configure policies:

```bash
# Check SELinux status
getenforce

# If enforcing, allow VPP operations
sudo semanage fcontext -a -t vpp_exec_t "/usr/bin/vpp"
sudo restorecon -R /usr/bin/vpp
```

### Firewall

VPP uses UNIX socket (`/run/vpp/cli.sock`) for CLI access - no firewall configuration needed for basic operation.

If you plan to expose VPP APIs externally (not recommended for Phase 1):

```bash
# Only if needed for remote API access
sudo firewall-cmd --permanent --add-port=5002/tcp
sudo firewall-cmd --reload
```

---

## 10. Next Steps

After VPP is installed and running:

1. Return to [README.md](../README.md) for arca-router installation
2. Configure `hardware.yaml` with your NIC PCI addresses
3. Install arca-router RPM package

---

## References

- [FD.io VPP Documentation](https://fd.io/docs/)
- [VPP Wiki](https://wiki.fd.io/)
- [DPDK Getting Started Guide](https://doc.dpdk.org/guides/)
