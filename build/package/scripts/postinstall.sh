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

# Add arca-router to VPP/FRR groups
usermod -aG vpp arca-router 2>/dev/null || echo "Warning: vpp group not found. Ensure VPP is installed."
usermod -aG frr arca-router 2>/dev/null || echo "Warning: frr group not found. Ensure FRR is installed."
usermod -aG frrvty arca-router 2>/dev/null || echo "Warning: frrvty group not found. Ensure FRR is installed."

# Reload systemd to recognize new service
systemctl daemon-reload >/dev/null 2>&1 || true

# Ensure directory permissions
mkdir -p /var/lib/arca-router /var/log/arca-router
chown arca-router:arca-router /var/lib/arca-router || true
chown arca-router:arca-router /var/log/arca-router || true
chmod 0750 /var/lib/arca-router || true
chmod 0750 /var/log/arca-router || true

# Ensure FRR config permissions (if FRR is installed)
# arca-routerd writes to /etc/frr/frr.conf directly (requires group write)
if [ -f /etc/frr/frr.conf ]; then
    chown root:frr /etc/frr/frr.conf || true
    chmod 0660 /etc/frr/frr.conf || true
fi

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
        echo "WARNING: VPP service not found. Phase 2 requires VPP 23.10+"
        echo "Install VPP: sudo dnf install vpp vpp-plugins"
    fi
    if ! systemctl list-unit-files 2>/dev/null | grep -q frr.service; then
        echo "WARNING: FRR service not found. Phase 2 requires FRR 8.0+"
        echo "Install FRR: sudo dnf install frr"
    fi
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
    echo "ARCA Router Phase 2 has been installed."
    echo ""
    echo "Prerequisites:"
    echo "- VPP 23.10+ with linux-cp plugin enabled"
    echo "- FRR 8.0+ (bgpd, ospfd, zebra, staticd)"
    echo ""
    echo "Next steps:"
    echo "1. Copy example configs:"
    echo "   cp /etc/arca-router/arca.conf.example /etc/arca-router/arca.conf"
    echo "   cp /etc/arca-router/hardware.yaml.example /etc/arca-router/hardware.yaml"
    echo ""
    echo "2. Edit the configuration files"
    echo ""
    echo "3. Ensure VPP/FRR are running:"
    echo "   systemctl start vpp frr"
    echo ""
    echo "4. Enable and start arca-router:"
    echo "   systemctl enable arca-routerd"
    echo "   systemctl start arca-routerd"
    echo ""
    echo "5. Check status:"
    echo "   systemctl status arca-routerd"
    echo "   arca-cli show interfaces"
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
