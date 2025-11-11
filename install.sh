#!/bin/bash
# Installer script for k8s-hpa-manager
# This script builds and installs the k8s-hpa-manager binary globally

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Project info
BINARY_NAME="k8s-hpa-manager"
INSTALL_PATH="/usr/local/bin"

echo -e "${BLUE}🏗️  K8s HPA Manager - Installer${NC}"
echo "=================================="

# Check if we're in the right directory
if [[ ! -f "go.mod" ]] || [[ ! -f "main.go" ]]; then
    echo -e "${RED}❌ Error: Please run this script from the project root directory${NC}"
    echo "Make sure you're in the directory containing go.mod and main.go"
    exit 1
fi

# Check if Go is installed
if ! command -v go &> /dev/null; then
    echo -e "${RED}❌ Error: Go is not installed or not in PATH${NC}"
    echo "Please install Go from https://golang.org/dl/"
    exit 1
fi

echo -e "${YELLOW}📋 Pre-installation checks...${NC}"

# Check if make is available
if command -v make &> /dev/null; then
    echo "✅ Make is available"
    USE_MAKE=true
else
    echo "⚠️  Make not found, will use 'go build' directly"
    USE_MAKE=false
fi

# Check if binary already exists
if command -v $BINARY_NAME &> /dev/null; then
    EXISTING_VERSION=$($BINARY_NAME --version 2>/dev/null || echo "unknown")
    echo -e "${YELLOW}⚠️  $BINARY_NAME is already installed${NC}"
    echo "Existing installation will be replaced"
fi

echo ""
echo -e "${BLUE}🔨 Building binary...${NC}"

# Build the binary
if [[ "$USE_MAKE" == true ]]; then
    echo "Using makefile..."
    make build
    BINARY_PATH="build/$BINARY_NAME"
else
    echo "Using go build..."
    mkdir -p build
    go build -o build/$BINARY_NAME .
    BINARY_PATH="build/$BINARY_NAME"
fi

# Verify the binary was created
if [[ ! -f "$BINARY_PATH" ]]; then
    echo -e "${RED}❌ Error: Binary was not created successfully${NC}"
    exit 1
fi

echo "✅ Binary built successfully"

# Get binary info
BINARY_SIZE=$(du -h "$BINARY_PATH" | cut -f1)
echo "📦 Binary size: $BINARY_SIZE"

echo ""
echo -e "${BLUE}📥 Installing globally...${NC}"

# Check if we need sudo
if [[ ! -w "$INSTALL_PATH" ]]; then
    echo "🔐 Administrator privileges required for installation to $INSTALL_PATH"
    
    # Copy binary to install path
    if sudo cp "$BINARY_PATH" "$INSTALL_PATH/"; then
        echo "✅ Binary copied to $INSTALL_PATH/"
    else
        echo -e "${RED}❌ Error: Failed to copy binary${NC}"
        exit 1
    fi
    
    # Set permissions
    if sudo chmod +x "$INSTALL_PATH/$BINARY_NAME"; then
        echo "✅ Execute permissions set"
    else
        echo -e "${RED}❌ Error: Failed to set permissions${NC}"
        exit 1
    fi
else
    # Direct copy (if user has write permissions)
    cp "$BINARY_PATH" "$INSTALL_PATH/"
    chmod +x "$INSTALL_PATH/$BINARY_NAME"
    echo "✅ Binary installed"
fi

echo ""
echo -e "${BLUE}🧪 Testing installation...${NC}"

# Test the installation
if command -v $BINARY_NAME &> /dev/null; then
    echo "✅ $BINARY_NAME is now available globally"
    
    # Show version/help
    echo ""
    echo "📋 Testing binary:"
    if $BINARY_NAME --help >/dev/null 2>&1; then
        echo "✅ Binary executes correctly"
    else
        echo -e "${YELLOW}⚠️  Binary installed but may have runtime issues${NC}"
    fi
else
    echo -e "${RED}❌ Error: Binary not found in PATH${NC}"
    echo "You may need to restart your terminal or add $INSTALL_PATH to your PATH"
    exit 1
fi

echo ""
echo -e "${GREEN}🎉 Installation completed successfully!${NC}"
echo "=================================="
echo ""
echo -e "${BLUE}Usage:${NC}"
echo "  $BINARY_NAME                    # Start the application"
echo "  $BINARY_NAME --help            # Show help"
echo "  $BINARY_NAME --debug           # Start with debug logging"
echo "  $BINARY_NAME --kubeconfig PATH # Use custom kubeconfig"
echo ""
echo -e "${BLUE}Features:${NC}"
echo "  • Interactive TUI for HPA management"
echo "  • Multi-cluster support (akspriv-* pattern)"
echo "  • Session save/load functionality"
echo "  • Press '?' in any screen for help"
echo ""
echo -e "${GREEN}🚀 Ready to manage your HPAs!${NC}"