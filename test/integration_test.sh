#!/bin/bash
# Integration test for arca-router Phase 1
# Tests configuration validation and basic daemon operations

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TEST_DIR="$PROJECT_ROOT/test/tmp"
BINARY="$PROJECT_ROOT/build/bin/arca-routerd"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo "=========================================="
echo "ARCA Router Phase 1 Integration Test"
echo "=========================================="
echo ""

# Cleanup function
cleanup() {
    echo ""
    echo "Cleaning up..."
    rm -rf "$TEST_DIR"
}

trap cleanup EXIT

# Create test directory
mkdir -p "$TEST_DIR"

# Test 1: Binary exists and runs
echo -n "Test 1: Binary version check... "
if "$BINARY" --version >/dev/null 2>&1; then
    echo -e "${GREEN}PASS${NC}"
else
    echo -e "${RED}FAIL${NC}"
    exit 1
fi

# Test 2: Valid hardware.yaml
echo -n "Test 2: Valid hardware.yaml parsing... "
cat > "$TEST_DIR/hardware.yaml" <<EOF
interfaces:
  - name: "ge-0/0/0"
    pci: "0000:03:00.0"
    driver: "avf"
    description: "Test Interface 1"
  - name: "ge-0/0/1"
    pci: "0000:03:00.1"
    driver: "avf"
    description: "Test Interface 2"
EOF

cat > "$TEST_DIR/arca-router.conf" <<EOF
set system host-name test-router
set interfaces ge-0/0/0 description "WAN"
set interfaces ge-0/0/0 unit 0 family inet address 192.0.2.1/24
set interfaces ge-0/0/1 description "LAN"
set interfaces ge-0/0/1 unit 0 family inet address 198.51.100.1/24
EOF

# Note: This test will fail because PCI devices don't exist
# But we can verify that parsing works
if "$BINARY" -config "$TEST_DIR/arca-router.conf" -hardware "$TEST_DIR/hardware.yaml" -mock-vpp 2>&1 | grep -q "Hardware configuration loaded successfully\|PCI device verification failed"; then
    echo -e "${GREEN}PASS${NC}"
else
    echo -e "${RED}FAIL${NC}"
    exit 1
fi

# Test 3: Invalid hardware.yaml (duplicate PCI)
echo -n "Test 3: Invalid hardware.yaml detection... "
cat > "$TEST_DIR/hardware_invalid.yaml" <<EOF
interfaces:
  - name: "ge-0/0/0"
    pci: "0000:03:00.0"
    driver: "avf"
  - name: "ge-0/0/1"
    pci: "0000:03:00.0"  # Duplicate PCI
    driver: "avf"
EOF

if "$BINARY" -config "$TEST_DIR/arca-router.conf" -hardware "$TEST_DIR/hardware_invalid.yaml" -mock-vpp 2>&1 | grep -q "duplicate PCI address"; then
    echo -e "${GREEN}PASS${NC}"
else
    echo -e "${RED}FAIL${NC}"
    echo "Expected: 'duplicate PCI address' error"
    exit 1
fi

# Test 4: Invalid arca.conf (syntax error)
echo -n "Test 4: Invalid arca.conf detection... "
cat > "$TEST_DIR/arca-router_invalid.conf" <<EOF
set system host-name test-router
set interfaces ge-0/0/0 description
# Missing description value - should fail
EOF

if ! "$BINARY" -config "$TEST_DIR/arca-router_invalid.conf" -hardware "$TEST_DIR/hardware.yaml" -mock-vpp >/dev/null 2>&1; then
    echo -e "${GREEN}PASS${NC}"
else
    echo -e "${RED}FAIL${NC}"
    exit 1
fi

# Test 5: Invalid CIDR format
echo -n "Test 5: Invalid CIDR detection... "
cat > "$TEST_DIR/arca-router_invalid_cidr.conf" <<EOF
set system host-name test-router
set interfaces ge-0/0/0 unit 0 family inet address 192.0.2.1/33
EOF

if ! "$BINARY" -config "$TEST_DIR/arca-router_invalid_cidr.conf" -hardware "$TEST_DIR/hardware.yaml" -mock-vpp >/dev/null 2>&1; then
    echo -e "${GREEN}PASS${NC}"
else
    echo -e "${RED}FAIL${NC}"
    exit 1
fi

# Test 6: Help output
echo -n "Test 6: Help output... "
if "$BINARY" -h 2>&1 | grep -q "Usage of"; then
    echo -e "${GREEN}PASS${NC}"
else
    echo -e "${RED}FAIL${NC}"
    exit 1
fi

echo ""
echo "=========================================="
echo -e "${GREEN}All tests passed!${NC}"
echo "=========================================="
