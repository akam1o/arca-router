#!/usr/bin/env bash
#
# generate-binapi.sh - Generate govpp binapi for VPP 23.10
#
# This script generates Go bindings for VPP Binary API from .api.json files.
# Generated bindings are placed in pkg/vpp/binapi/ for reproducible builds.
#
# Prerequisites:
# - VPP 23.10 installed (for .api.json files) OR .api.json files manually placed
# - govpp binapi-generator installed: go install go.fd.io/govpp/cmd/binapi-generator@v0.13.0
# - Go 1.22+
#
# Usage:
#   ./scripts/generate-binapi.sh [options]
#
# Options:
#   --api-dir PATH    Path to VPP .api.json files (default: /usr/share/vpp/api)
#   --output-dir PATH Output directory for generated binapi (default: pkg/vpp/binapi)
#   --minimal         Generate only minimal binapi for PoC (vpe only)
#   --help            Show this help message

set -e
set -o pipefail

# Default configuration
API_DIR="${VPP_API_DIR:-/usr/share/vpp/api}"
OUTPUT_DIR="pkg/vpp/binapi"
MINIMAL_MODE=false

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --api-dir)
            API_DIR="$2"
            shift 2
            ;;
        --output-dir)
            OUTPUT_DIR="$2"
            shift 2
            ;;
        --minimal)
            MINIMAL_MODE=true
            shift
            ;;
        --help)
            sed -n '2,/^$/p' "$0" | sed 's/^# \?//'
            exit 0
            ;;
        *)
            echo -e "${RED}Error: Unknown option: $1${NC}" >&2
            echo "Run with --help for usage information" >&2
            exit 1
            ;;
    esac
done

echo "=================================================="
echo "  govpp binapi Generator for VPP 23.10"
echo "=================================================="
echo ""

# Check if binapi-generator is installed
if ! command -v binapi-generator &> /dev/null; then
    echo -e "${YELLOW}Warning: binapi-generator not found${NC}"
    echo "Installing binapi-generator v0.13.0..."
    go install go.fd.io/govpp/cmd/binapi-generator@v0.13.0
    echo -e "${GREEN}binapi-generator installed${NC}"
    echo ""
fi

# Verify binapi-generator version
BINAPI_VERSION=$(binapi-generator --version 2>&1 | grep -oP 'v\d+\.\d+\.\d+' || echo "unknown")
echo "binapi-generator version: $BINAPI_VERSION"
echo ""

# Check if API directory exists
if [[ ! -d "$API_DIR" ]]; then
    echo -e "${RED}Error: API directory not found: $API_DIR${NC}" >&2
    echo ""
    echo "Possible solutions:"
    echo "1. Install VPP 23.10: sudo dnf install vpp"
    echo "2. Specify custom path: --api-dir /path/to/api"
    echo "3. See docs/vpp-setup-rhel9.md for setup instructions"
    exit 1
fi

echo "API directory: $API_DIR"
echo "Output directory: $OUTPUT_DIR"
echo "Minimal mode: $MINIMAL_MODE"
echo ""

# Create output directory
mkdir -p "$OUTPUT_DIR"

# Determine which APIs to generate
if [[ "$MINIMAL_MODE" == "true" ]]; then
    echo "Generating minimal binapi (PoC mode)..."
    API_FILES=("$API_DIR/vpe.api.json")
else
    echo "Generating full binapi (Phase 2 mode)..."
    API_FILES=(
        "$API_DIR/vpe.api.json"
        "$API_DIR/interface.api.json"
        "$API_DIR/ip.api.json"
        "$API_DIR/avf.api.json"
        "$API_DIR/rdma.api.json"
        "$API_DIR/tapv2.api.json"
        "$API_DIR/lcp.api.json"
    )
fi

# Check if API files exist
MISSING_FILES=()
for api_file in "${API_FILES[@]}"; do
    if [[ ! -f "$api_file" ]]; then
        MISSING_FILES+=("$(basename "$api_file")")
    fi
done

if [[ ${#MISSING_FILES[@]} -gt 0 ]]; then
    echo -e "${RED}Error: Missing API files:${NC}" >&2
    for missing in "${MISSING_FILES[@]}"; do
        echo "  - $missing" >&2
    done
    exit 1
fi

# Generate binapi
echo ""
echo "Generating binapi..."
echo ""

for api_file in "${API_FILES[@]}"; do
    api_name=$(basename "$api_file" .api.json)
    echo "Processing: $api_name"

    binapi-generator \
        --input="$api_file" \
        --output-dir="$OUTPUT_DIR" \
        --gen=rpc \
        --import-prefix="github.com/akam1o/arca-router/pkg/vpp/binapi"

    echo -e "${GREEN}✓${NC} Generated: $OUTPUT_DIR/$api_name/$api_name.ba.go"
done

echo ""
echo -e "${GREEN}binapi generation complete!${NC}"
echo ""
echo "Generated files:"
ls -lh "$OUTPUT_DIR"
echo ""

# Verify generated files
echo "Verifying generated files..."
if go build ./... > /dev/null 2>&1; then
    echo -e "${GREEN}✓${NC} Build verification passed"
else
    echo -e "${YELLOW}Warning: Build verification failed${NC}"
    echo "This may be expected if VPP types are not yet integrated"
fi

echo ""
echo "Next steps:"
echo "1. Review generated binapi in $OUTPUT_DIR"
echo "2. Commit generated files to repository"
if [[ "$MINIMAL_MODE" == "true" ]]; then
    echo "3. Run PoC: cd test/vpp_poc && go build && sudo ./vpp_poc"
    echo "4. Generate full binapi: ./scripts/generate-binapi.sh (without --minimal)"
else
    echo "3. Implement VPP client: pkg/vpp/govpp_client.go"
    echo "4. Run integration tests"
fi
echo ""
echo "For more information, see docs/govpp-compatibility.md"
