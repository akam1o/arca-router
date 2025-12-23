#!/bin/sh
set -e

# Pre-uninstall script
# Runs before package files are removed

# Check if this is uninstall ($1 = 0) or upgrade ($1 = 1)
if [ "$1" = "0" ]; then
    # Uninstall: stop and disable the service
    systemctl stop arca-routerd >/dev/null 2>&1 || true
    systemctl disable arca-routerd >/dev/null 2>&1 || true
fi
# For upgrade ($1 = 1), do nothing - let the service continue running

exit 0
