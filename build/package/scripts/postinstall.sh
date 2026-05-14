#!/bin/sh
set -e

# Post-installation script
# Runs after package files are installed

# Create arca-router user if not exists
if ! id arca-router >/dev/null 2>&1; then
    useradd --system --no-create-home \
        --home-dir /var/lib/arca-router \
        --shell /sbin/nologin \
        --comment "arca-router service user" \
        arca-router
fi

# Add arca-router to groups needed by the default v0.5 backends.
usermod -aG vpp arca-router 2>/dev/null || echo "Warning: vpp group not found. Ensure VPP is installed."
usermod -aG frrvty arca-router 2>/dev/null || echo "Warning: frrvty group not found. Ensure FRR is installed."

# Reload systemd to recognize new service
systemctl daemon-reload >/dev/null 2>&1 || true

# Ensure directory permissions
mkdir -p /var/lib/arca-router /var/log/arca-router
chown arca-router:arca-router /var/lib/arca-router || true
chown arca-router:arca-router /var/log/arca-router || true
chmod 0750 /var/lib/arca-router || true
chmod 0750 /var/log/arca-router || true

# The default FRR apply backend is transactional and uses vtysh/mgmtd.
# arca-router treats vrrpd and bfdd as part of the standard appliance router daemon set.
# Do not grant /etc/frr write access by default. The legacy file backend can be
# enabled with --frr-apply-mode=file plus a local systemd/group permission override.

# SELinux context for log directory (RHEL 9)
if command -v semanage >/dev/null 2>&1 && command -v restorecon >/dev/null 2>&1; then
    semanage fcontext -a -t var_log_t "/var/log/arca-router(/.*)?" 2>/dev/null || true
    restorecon -R /var/log/arca-router 2>/dev/null || true
fi

# Verify VPP/FRR installation
if command -v systemctl >/dev/null 2>&1; then
    echo ""
    echo "Verifying VPP/FRR installation..."
    if ! systemctl list-unit-files 2>/dev/null | grep -q vpp.service; then
        echo "WARNING: VPP service not found. arca-router requires VPP 24.10+"
        echo "Install VPP (Debian): https://packagecloud.io/fdio/2410"
        echo "Install VPP (RHEL): sudo dnf install vpp vpp-plugin-core"
    fi
    if ! systemctl list-unit-files 2>/dev/null | grep -q frr.service; then
        echo "WARNING: FRR service not found. arca-router requires FRR 8.0+"
        echo "Install FRR: sudo apt install frr (Debian) or sudo dnf install frr (RHEL)"
    fi
fi

# Check required FRR daemon enablement.
if [ -f /etc/frr/daemons ] && ! grep -q '^mgmtd=yes' /etc/frr/daemons; then
    echo "WARNING: FRR mgmtd is not enabled. Set mgmtd=yes in /etc/frr/daemons for the default transactional apply backend."
fi
if [ -f /etc/frr/daemons ] && ! grep -q '^vrrpd=yes' /etc/frr/daemons; then
    echo "WARNING: FRR vrrpd is not enabled. Set vrrpd=yes in /etc/frr/daemons for standard HA/VRRP support."
fi
if [ -f /etc/frr/daemons ] && ! grep -q '^bfdd=yes' /etc/frr/daemons; then
    echo "WARNING: FRR bfdd is not enabled. Set bfdd=yes in /etc/frr/daemons for BFD peer support."
fi

# Check VPP socket permissions (if VPP is running)
if [ -e /run/vpp/api.sock ]; then
    SOCK_GROUP=$(stat -c %G /run/vpp/api.sock 2>/dev/null || echo "unknown")
    if [ "$SOCK_GROUP" != "vpp" ]; then
        echo "WARNING: /run/vpp/api.sock group is $SOCK_GROUP (expected: vpp)"
        echo "Update /etc/vpp/startup.conf: unix { api-segment { gid vpp } }"
    fi
fi

# Check if this is initial install ($1 = 1) or upgrade ($1 = 2)
if [ "$1" = "1" ]; then
    # Initial installation
    echo ""
    echo "=========================================="
    echo "ARCA Router v0.6 unified daemon has been installed."
    echo ""
    echo "Prerequisites:"
    echo "- VPP 24.10+ with linux-cp plugin enabled"
    echo "- FRR 8.0+ (bgpd, ospfd, zebra, staticd, mgmtd, vrrpd, bfdd)"
    echo ""
    echo "Next steps:"
    echo "1. Copy example configs:"
    echo "   cp /etc/arca-router/arca-router.conf.example /etc/arca-router/arca-router.conf"
    echo "   cp /etc/arca-router/hardware.yaml.example /etc/arca-router/hardware.yaml"
    echo ""
    echo "2. Edit the configuration files"
    echo ""
    echo "3. Add CLI operator users to the arca-router group:"
    echo "   usermod -aG arca-router <admin-user>"
    echo "   # log out and back in before running arca as that user"
    echo ""
    echo "4. Ensure VPP/FRR are running and required FRR daemons are enabled:"
    echo "   grep '^mgmtd=yes' /etc/frr/daemons"
    echo "   grep '^vrrpd=yes' /etc/frr/daemons"
    echo "   grep '^bfdd=yes' /etc/frr/daemons"
    echo "   systemctl start vpp frr"
    echo ""
    echo "5. Enable and start arca-router:"
    echo "   systemctl enable arca-routerd"
    echo "   systemctl start arca-routerd"
    echo ""
    echo "6. Check status:"
    echo "   systemctl status arca-routerd"
    echo "   arca show configuration"
    echo "=========================================="
elif [ "$1" = "2" ]; then
    # Upgrade
    echo "=========================================="
    echo "ARCA Router has been upgraded."
    echo ""
    echo "Please restart the service to apply changes:"
    echo "   systemctl restart arca-routerd"
    echo ""
    echo "Check status:"
    echo "   systemctl status arca-routerd"
    echo "=========================================="
fi

exit 0
