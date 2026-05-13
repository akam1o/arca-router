# FRR Setup Guide for Debian Bookworm

This guide covers FRR 8.0+ installation and configuration for arca-router v0.6.x.

**Status**: v0.6.x - FRR is **required** for dynamic routing and VRRP-based HA.

---

## Prerequisites

- Debian 12 (Bookworm) x86_64
- VPP 24.10 already installed and running (see [vpp-setup-debian.md](vpp-setup-debian.md))
- Root or sudo access

---

## Installation

### 1. Install FRR from Debian Repository

```bash
# Update package list
sudo apt-get update

# Install FRR
sudo apt-get install -y frr frr-pythontools

# Verify installation
vtysh --version
# Expected: FRRouting 8.x.x ...
```

**Installed components**:
- `frr`: Main FRR package (all daemons)
- `frr-pythontools`: Includes `frr-reload.py` for the optional `--frr-apply-mode=file` recovery backend

---

## Configuration

### 2. Enable Required Daemons

Edit `/etc/frr/daemons`:

```bash
sudo vi /etc/frr/daemons
```

**Enable the following daemons**:

```bash
# /etc/frr/daemons - REQUIRED for arca-router v0.6+

bgpd=yes
ospfd=yes
zebra=yes
staticd=yes
mgmtd=yes
vrrpd=yes

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
pathd=no

# Integrated config file (used by FRR and the optional file backend)
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
- `mgmtd`: Transactional management datastore used by arca-router v0.5+
- `vrrpd`: VRRP daemon used for appliance-style control-plane HA

### 3. Configure FRR Apply Access

By default, `arca-router` applies FRR changes, including VRRP groups, through the FRR management candidate datastore via `vtysh`. This requires `mgmtd=yes`, the standard `vrrpd=yes` HA daemon, and `frrvty` group access, but does not require direct writes to `/etc/frr/frr.conf`.

When VRRP is configured, arca-routerd also prepares arca-owned Linux macvlan interfaces on the LCP interface before applying FRR. It stores prepared interface names in `/var/lib/arca-router/vrrp-interfaces.json` so cleanup survives daemon restarts. The packaged systemd unit grants `CAP_NET_ADMIN`, which is required for this macvlan and virtual-address reconciliation.

arca-routerd polls FRR VRRP operational state with `vtysh -c "show vrrp json"` for HA convergence status. The `arca-router` service user must retain `frrvty` group access so this read-only command works in addition to transactional configuration applies.

If you plan to use the recovery backend `--frr-apply-mode=file`, also allow the `frr` group to write `/etc/frr/frr.conf`:

```bash
# Optional: only required for --frr-apply-mode=file
sudo chown root:frr /etc/frr/frr.conf
sudo chmod 0660 /etc/frr/frr.conf

# Verify permissions
ls -l /etc/frr/frr.conf
# Expected: -rw-rw---- 1 root frr ... /etc/frr/frr.conf
```

### 4. Add arca-router User to FRR Groups

**Note**: The `arca-router` user will be created automatically when you install the `arca-router` package. Complete this step after installing the `arca-router` package.

```bash
# Required for transactional apply through vtysh
sudo usermod -aG frrvty arca-router

# Optional: only required for --frr-apply-mode=file
sudo usermod -aG frr arca-router

# Verify group membership
groups arca-router
# Expected: arca-router : arca-router vpp frrvty (and frr if file backend is enabled)
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
ps aux | grep -E 'zebra|bgpd|ospfd|staticd|mgmtd|vrrpd'

# Test FRR CLI (vtysh)
sudo vtysh -c 'show version'
# Expected: FRRouting 8.x.x ...

# Check running configuration
sudo vtysh -c 'show running-config'
```

---

## Configuration Management

### FRR Apply Backend

`arca-router` generates an FRR view from the router model, then applies it through the selected backend.

**Configuration apply methods**:

1. **Method A: transactional management** (default)
   - Enabled with `--frr-apply-mode=transactional`
   - Uses FRR management commands through `vtysh`
   - Runs candidate `commit check`, `commit apply`, then `write memory`
   - Requires `mgmtd=yes` and `frrvty` group access

2. **Method B: file backend** (recovery)
   - Enabled with `--frr-apply-mode=file`
   - Writes `/etc/frr/frr.conf`
   - Uses `/usr/lib/frr/frr-reload.py --reload`, then falls back to `vtysh -f /etc/frr/frr.conf`
   - Packaged systemd units do not grant `/etc/frr` write access by default; add a local drop-in with the `frr` group and `ReadWritePaths=/etc/frr` before using this backend.

**Validation**:
- Transactional apply validates with FRR management `commit check`
- File backend validates with `vtysh --check` before applying
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
- **Configuration syntax error**: Check the arca-routerd log for the failing backend command. For `--frr-apply-mode=file`, run `sudo vtysh --check /etc/frr/frr.conf` to validate the generated file.

### Permission Denied on /etc/frr/frr.conf (file backend only)

**Symptom**: `arca-routerd --frr-apply-mode=file` fails with "permission denied" writing to `/etc/frr/frr.conf`

**Solution**:
```bash
# Check file permissions
ls -l /etc/frr/frr.conf

# Fix ownership and permissions
sudo chown root:frr /etc/frr/frr.conf
sudo chmod 0660 /etc/frr/frr.conf

# Verify arca-router is in frr group for the file backend
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
3. **View running configuration**: `arca show configuration`
4. **View FRR routing state**: `sudo vtysh -c 'show ip route'`

---

## References

- [FRRouting Official Documentation](https://docs.frrouting.org/)
- [FRR Configuration Guide](https://docs.frrouting.org/en/latest/setup.html)
- [FRR Debian Installation](https://deb.frrouting.org/)
