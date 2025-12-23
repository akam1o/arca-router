#!/bin/sh
set -e

# Post-transaction script
# Runs after all install/upgrade/uninstall operations are complete
# This is the final script that runs after an upgrade

# For upgrades, suggest restarting the service
# We don't automatically restart to give admins control
# Guard against non-systemd environments (containers/chroots)
if command -v systemctl >/dev/null 2>&1; then
    if systemctl is-active --quiet arca-routerd 2>/dev/null; then
        echo "Note: arca-routerd service is running."
        echo "Consider restarting to apply updates:"
        echo "  systemctl restart arca-routerd"
    fi
fi

exit 0
