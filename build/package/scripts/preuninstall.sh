#!/bin/sh
set -e

# Pre-uninstall script
# Runs before package files are removed

# Check if this is uninstall ($1 = 0) or upgrade ($1 = 1)
if [ "$1" = "0" ]; then
    # Uninstall: stop and disable the service
    systemctl stop arca-routerd >/dev/null 2>&1 || true
    systemctl disable arca-routerd >/dev/null 2>&1 || true

    echo ""
    echo "=========================================="
    echo "WARNING: Uninstalling arca-router"
    echo ""
    echo "Note: FRR configuration (/etc/frr/frr.conf) will NOT be removed."
    echo "If you want to clean up FRR configuration, run:"
    echo "  sudo systemctl stop frr"
    echo "  sudo rm -f /etc/frr/frr.conf"
    echo "  sudo systemctl start frr"
    echo "=========================================="
    echo ""
fi
# For upgrade ($1 = 1), do nothing - let the service continue running

exit 0
