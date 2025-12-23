#!/bin/sh
set -e

# Post-installation script
# Runs after package files are installed

# Reload systemd to recognize new service
systemctl daemon-reload >/dev/null 2>&1 || true

# Ensure directory permissions
chmod 0755 /var/lib/arca-router || true
chmod 0750 /var/log/arca-router || true

# SELinux context for log directory (RHEL 9)
if command -v semanage >/dev/null 2>&1 && command -v restorecon >/dev/null 2>&1; then
    semanage fcontext -a -t var_log_t "/var/log/arca-router(/.*)?" 2>/dev/null || true
    restorecon -R /var/log/arca-router 2>/dev/null || true
fi

# Check if this is initial install ($1 = 1) or upgrade ($1 = 2)
if [ "$1" = "1" ]; then
    # Initial installation
    echo "=========================================="
    echo "ARCA Router has been installed successfully."
    echo ""
    echo "Phase 1 Note: This version runs in mock VPP mode."
    echo ""
    echo "Next steps:"
    echo "1. Copy example configs:"
    echo "   cp /etc/arca-router/arca.conf.example /etc/arca-router/arca.conf"
    echo "   cp /etc/arca-router/hardware.yaml.example /etc/arca-router/hardware.yaml"
    echo ""
    echo "2. Edit the configuration files for your environment"
    echo ""
    echo "3. Enable and start the service:"
    echo "   systemctl enable arca-routerd"
    echo "   systemctl start arca-routerd"
    echo ""
    echo "4. Check status:"
    echo "   systemctl status arca-routerd"
    echo "   journalctl -u arca-routerd -f"
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
