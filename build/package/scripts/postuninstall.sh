#!/bin/sh
set -e

# Post-uninstall script
# Runs after package files are removed

# Always reload systemd
systemctl daemon-reload >/dev/null 2>&1 || true

# Check if this is uninstall ($1 = 0) or upgrade ($1 = 1)
if [ "$1" = "0" ]; then
    # Uninstall: provide cleanup information
    echo "=========================================="
    echo "ARCA Router has been uninstalled."
    echo ""
    echo "Note: Configuration files and logs have been preserved:"
    echo "  /etc/arca-router/"
    echo "  /var/log/arca-router/"
    echo ""
    echo "To completely remove all data, run:"
    echo "  rm -rf /etc/arca-router"
    echo "  rm -rf /var/log/arca-router"
    echo "  rm -rf /var/lib/arca-router"
    echo "=========================================="
fi
# For upgrade ($1 = 1), do nothing

exit 0
