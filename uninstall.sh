#!/bin/bash
# Uninstaller script for k8s-hpa-manager

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
SESSION_DIR="$HOME/.k8s-hpa-manager"

echo -e "${BLUE}🗑️  K8s HPA Manager - Uninstaller${NC}"
echo "===================================="

# Check if binary is installed
if ! command -v $BINARY_NAME &> /dev/null; then
    echo -e "${YELLOW}⚠️  $BINARY_NAME is not installed or not in PATH${NC}"
    echo "Nothing to uninstall."
    exit 0
fi

echo "📋 Found installation: $(which $BINARY_NAME)"

# Confirm uninstallation
echo ""
echo -e "${YELLOW}This will remove:${NC}"
echo "  • Binary: $INSTALL_PATH/$BINARY_NAME"
echo "  • Sessions: $SESSION_DIR (optional)"
echo ""
read -p "Continue with uninstallation? [y/N]: " -n 1 -r
echo ""

if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo "Uninstallation cancelled."
    exit 0
fi

echo ""
echo -e "${BLUE}🗑️  Removing binary...${NC}"

# Remove binary
if [[ -f "$INSTALL_PATH/$BINARY_NAME" ]]; then
    if [[ -w "$INSTALL_PATH" ]]; then
        rm "$INSTALL_PATH/$BINARY_NAME"
        echo "✅ Binary removed"
    else
        echo "🔐 Administrator privileges required"
        if sudo rm "$INSTALL_PATH/$BINARY_NAME"; then
            echo "✅ Binary removed"
        else
            echo -e "${RED}❌ Error: Failed to remove binary${NC}"
            exit 1
        fi
    fi
else
    echo -e "${YELLOW}⚠️  Binary not found at expected location${NC}"
fi

# Ask about session data
if [[ -d "$SESSION_DIR" ]]; then
    echo ""
    echo -e "${YELLOW}📁 Session data found at: $SESSION_DIR${NC}"
    read -p "Remove session data as well? [y/N]: " -n 1 -r
    echo ""
    
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        rm -rf "$SESSION_DIR"
        echo "✅ Session data removed"
    else
        echo "📁 Session data preserved"
    fi
fi

# Verify uninstallation
echo ""
echo -e "${BLUE}🧪 Verifying removal...${NC}"

if command -v $BINARY_NAME &> /dev/null; then
    echo -e "${YELLOW}⚠️  $BINARY_NAME still found in PATH${NC}"
    echo "You may have multiple installations or need to restart your terminal"
else
    echo "✅ $BINARY_NAME successfully removed from PATH"
fi

echo ""
echo -e "${GREEN}🎉 Uninstallation completed!${NC}"
echo "===================================="
echo ""
echo "Thank you for using k8s-hpa-manager! 👋"