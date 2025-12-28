# FRR Setup Guide for RHEL 9

This guide covers FRR 8.0+ installation and configuration for arca-router v0.3.x.

**Status**: v0.3.x - FRR is **required** for dynamic routing (BGP, OSPF)

---

## Prerequisites

- RHEL 9 / AlmaLinux 9 / Rocky Linux 9 (x86_64)
- VPP 24.10 already installed and running (see [vpp-setup-rhel9.md](vpp-setup-rhel9.md))
- Root or sudo access

---

## Installation

### 1. Install FRR from Distribution Repository

```bash
# Optional: enable EPEL if not already present
sudo dnf install -y epel-release

# Install FRR
sudo dnf install -y frr frr-pythontools

# Verify installation
vtysh --version
# Expected: FRRouting 8.x.x ...
```

**Installed components**:
- `frr`: Main FRR package (all daemons)
- `frr-pythontools`: Includes `frr-reload.py` for configuration management

---

## Configuration

### 2. Enable Required Daemons

Edit `/etc/frr/daemons`:

```bash
sudo vi /etc/frr/daemons
```

**Enable the following daemons**:

```bash
# /etc/frr/daemons - REQUIRED for arca-router v0.3.x

bgpd=yes
ospfd=yes
zebra=yes
staticd=yes

# Optional daemons (set to 'no' if not needed)
ospf6d=no
ripd=no
ripngd=no
isisd=no
pimd=no
ldpd=no
nhrpd=no
eigrpd=no
babeld=no
sharpd=no
pbrd=no
bfdd=no
fabricd=no
vrrpd=no
pathd=no

# Integrated config file (arca-router writes to /etc/frr/frr.conf)
vtysh_enable=yes
zebra_options="  -A 127.0.0.1 -s 90000000"
bgpd_options="   -A 127.0.0.1"
ospfd_options="  -A 127.0.0.1"
staticd_options=""
```

**Key daemons for arca-router**:
- `zebra`: Routing information base (RIB) manager
- `bgpd`: BGP routing protocol
- `ospfd`: OSPF routing protocol
- `staticd`: Static route management

### 3. Configure FRR File Permissions

**IMPORTANT**: `arca-router` writes directly to `/etc/frr/frr.conf` and needs write access:

```bash
# Set correct ownership and permissions
sudo chown root:frr /etc/frr/frr.conf
sudo chmod 0660 /etc/frr/frr.conf

# Verify permissions
ls -l /etc/frr/frr.conf
# Expected: -rw-rw---- 1 root frr ... /etc/frr/frr.conf
```

### 4. Add arca-router User to FRR Groups

**Note**: The `arca-router` user will be created automatically when you install the `arca-router` package. Complete this step after installing the `arca-router` package.

```bash
# Add to frr group (for config file write access)
sudo usermod -aG frr arca-router

# Add to frrvty group (for vtysh CLI access)
sudo usermod -aG frrvty arca-router

# Verify group membership
groups arca-router
# Expected: arca-router : arca-router vpp frr frrvty
```

### 5. Start FRR Service

```bash
# Enable FRR to start on boot
sudo systemctl enable frr

# Start FRR
sudo systemctl start frr

# Check status
sudo systemctl status frr
```

### 6. Verify FRR is Running

```bash
# Check FRR service status
sudo systemctl status frr

# Verify daemons are running
ps aux | grep -E 'zebra|bgpd|ospfd|staticd'

# Test FRR CLI (vtysh)
sudo vtysh -c 'show version'
# Expected: FRRouting 8.x.x ...

# Check running configuration
sudo vtysh -c 'show running-config'
```

---

## Configuration Management

### FRR Configuration File

`arca-router` generates and applies FRR configuration to `/etc/frr/frr.conf`.

**Configuration apply methods** (automatic selection):

1. **Method A: frr-reload.py** (preferred)
   - Uses `/usr/lib/frr/frr-reload.py --reload`
   - Applies configuration changes incrementally
   - Minimizes routing disruption

2. **Method B: vtysh -f** (fallback)
   - Uses `vtysh -f /etc/frr/frr.conf`
   - Loads entire configuration file
   - May cause temporary routing disruption

**Validation**:
- All configurations are validated with `vtysh --check` before applying
- Invalid configurations are rejected with detailed error messages

---

## Troubleshooting

### FRR Service Fails to Start

**Check FRR logs**:
```bash
sudo journalctl -u frr -n 50
sudo tail -f /var/log/frr/frr.log
```

**Common issues**:
- **Daemon not enabled**: Check `/etc/frr/daemons` and ensure required daemons are set to `yes`
- **Configuration syntax error**: Run `sudo vtysh --check /etc/frr/frr.conf` to validate

### Permission Denied on /etc/frr/frr.conf

**Symptom**: `arca-routerd` fails with "permission denied" writing to `/etc/frr/frr.conf`

**Solution**:
```bash
# Check file permissions
ls -l /etc/frr/frr.conf

# Fix ownership and permissions
sudo chown root:frr /etc/frr/frr.conf
sudo chmod 0660 /etc/frr/frr.conf

# Verify arca-router is in frr group
groups arca-router | grep frr

# If not, add to group
sudo usermod -aG frr arca-router
```

### BGP/OSPF Routes Not Appearing

**Check FRR routing table**:
```bash
sudo vtysh -c 'show ip route'
sudo vtysh -c 'show bgp summary'
sudo vtysh -c 'show ip ospf neighbor'
```

**Check LCP status** (VPP interfaces visible in Linux):
```bash
# List LCP interfaces
sudo vppctl show lcp

# Check Linux kernel routing table
ip route show
```

**Verify route synchronization**:
- FRR → Kernel: `ip route show`
- Kernel → VPP: `sudo vppctl show ip fib`

---

## Next Steps

After FRR is running:

1. **Start arca-router**: `sudo systemctl start arca-routerd`
2. **Check arca-router status**: `sudo systemctl status arca-routerd`
3. **View interfaces**: `arca-cli show interfaces`
