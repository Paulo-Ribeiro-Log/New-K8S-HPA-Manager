#!/bin/bash

# Helm Chart Manager for k8s-hpa-manager
# Manages local chart storage in ~/.k8s-hpa-manager/storaged-helm/

set -e

CHART_DIR="$HOME/.k8s-hpa-manager/storaged-helm"

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Create directory if it doesn't exist
mkdir -p "$CHART_DIR"

usage() {
    cat <<EOF
${BLUE}Helm Chart Manager${NC}

Usage: $0 <command> [options]

Commands:
    add <path>              Add a chart .tgz to storage
    add-from-release <release> <namespace> [cluster]
                           Extract and add chart from Kubernetes release
    list                   List all stored charts
    remove <chart-name>    Remove a chart from storage
    info <chart-name>      Show chart information
    path                   Show storage directory path

Examples:
    $0 add ./convair-helm-v0.9.0.tgz
    $0 add-from-release entrega-mais-api entrega-mais-sit
    $0 list
    $0 remove convair-helm-v0.9.0
    $0 info convair-helm-v0.9.0

Storage location: ${CHART_DIR}
EOF
}

add_chart() {
    local source_path="$1"
    
    if [[ ! -f "$source_path" ]]; then
        echo -e "${RED}Error: File not found: $source_path${NC}"
        exit 1
    fi
    
    if [[ ! "$source_path" =~ \.tgz$ ]]; then
        echo -e "${RED}Error: File must be a .tgz archive${NC}"
        exit 1
    fi
    
    local filename=$(basename "$source_path")
    local dest_path="$CHART_DIR/$filename"
    
    cp "$source_path" "$dest_path"
    echo -e "${GREEN}✓${NC} Chart added: $filename"
    echo -e "${BLUE}Location:${NC} $dest_path"
    
    # Show chart info
    echo -e "\n${BLUE}Chart Info:${NC}"
    tar -xzOf "$dest_path" "*/Chart.yaml" 2>/dev/null | grep -E "^(name|version|description):" || true
}

add_from_release() {
    local release="$1"
    local namespace="$2"
    local cluster="${3:-}"
    
    if [[ -z "$release" ]] || [[ -z "$namespace" ]]; then
        echo -e "${RED}Error: release and namespace are required${NC}"
        echo "Usage: $0 add-from-release <release> <namespace> [cluster]"
        exit 1
    fi
    
    echo -e "${BLUE}Extracting chart from release: $release (namespace: $namespace)${NC}"
    
    # Get latest revision
    local kubectl_cmd="kubectl"
    if [[ -n "$cluster" ]]; then
        kubectl_cmd="kubectl --context=$cluster"
    fi
    
    local secrets=$($kubectl_cmd get secrets -n "$namespace" -l "name=$release,owner=helm" --sort-by=.metadata.creationTimestamp -o name 2>/dev/null)
    
    if [[ -z "$secrets" ]]; then
        echo -e "${RED}Error: No helm secrets found for release '$release' in namespace '$namespace'${NC}"
        exit 1
    fi
    
    local latest_secret=$(echo "$secrets" | tail -n 1)
    echo -e "${BLUE}Using secret: $latest_secret${NC}"
    
    # Extract chart
    local tmp_file=$(mktemp)
    $kubectl_cmd get -n "$namespace" "$latest_secret" -o jsonpath='{.data.release}' | \
        base64 -d | base64 -d | gunzip | \
        jq -r '.chart' | base64 -d > "$tmp_file"
    
    # Get chart name from metadata
    local chart_name=$(tar -xzOf "$tmp_file" "*/Chart.yaml" 2>/dev/null | grep "^name:" | awk '{print $2}')
    local chart_version=$(tar -xzOf "$tmp_file" "*/Chart.yaml" 2>/dev/null | grep "^version:" | awk '{print $2}')
    
    if [[ -z "$chart_name" ]] || [[ -z "$chart_version" ]]; then
        echo -e "${RED}Error: Could not extract chart metadata${NC}"
        rm -f "$tmp_file"
        exit 1
    fi
    
    local dest_file="$CHART_DIR/${chart_name}-${chart_version}.tgz"
    mv "$tmp_file" "$dest_file"
    
    echo -e "${GREEN}✓${NC} Chart extracted: ${chart_name}-${chart_version}.tgz"
    echo -e "${BLUE}Location:${NC} $dest_file"
    echo -e "\n${BLUE}Chart Info:${NC}"
    tar -xzOf "$dest_file" "*/Chart.yaml" 2>/dev/null | grep -E "^(name|version|description):" || true
}

list_charts() {
    echo -e "${BLUE}Stored Charts in: ${CHART_DIR}${NC}\n"
    
    if [[ ! -d "$CHART_DIR" ]] || [[ -z "$(ls -A "$CHART_DIR"/*.tgz 2>/dev/null)" ]]; then
        echo -e "${YELLOW}No charts found${NC}"
        return
    fi
    
    printf "%-40s %-15s %-15s %s\n" "NAME" "VERSION" "APP VERSION" "SIZE"
    echo "--------------------------------------------------------------------------------"
    
    for chart in "$CHART_DIR"/*.tgz; do
        if [[ -f "$chart" ]]; then
            local filename=$(basename "$chart")
            local size=$(du -h "$chart" | cut -f1)
            local name=$(tar -xzOf "$chart" "*/Chart.yaml" 2>/dev/null | grep "^name:" | awk '{print $2}' || echo "unknown")
            local version=$(tar -xzOf "$chart" "*/Chart.yaml" 2>/dev/null | grep "^version:" | awk '{print $2}' || echo "unknown")
            local app_version=$(tar -xzOf "$chart" "*/Chart.yaml" 2>/dev/null | grep "^appVersion:" | awk '{print $2}' || echo "-")
            
            printf "%-40s %-15s %-15s %s\n" "$filename" "$version" "$app_version" "$size"
        fi
    done
}

remove_chart() {
    local chart_name="$1"
    
    if [[ -z "$chart_name" ]]; then
        echo -e "${RED}Error: chart name is required${NC}"
        exit 1
    fi
    
    # Try with .tgz extension if not provided
    if [[ ! "$chart_name" =~ \.tgz$ ]]; then
        chart_name="${chart_name}.tgz"
    fi
    
    local chart_path="$CHART_DIR/$chart_name"
    
    if [[ ! -f "$chart_path" ]]; then
        echo -e "${RED}Error: Chart not found: $chart_name${NC}"
        exit 1
    fi
    
    rm -f "$chart_path"
    echo -e "${GREEN}✓${NC} Chart removed: $chart_name"
}

info_chart() {
    local chart_name="$1"
    
    if [[ -z "$chart_name" ]]; then
        echo -e "${RED}Error: chart name is required${NC}"
        exit 1
    fi
    
    # Try with .tgz extension if not provided
    if [[ ! "$chart_name" =~ \.tgz$ ]]; then
        chart_name="${chart_name}.tgz"
    fi
    
    local chart_path="$CHART_DIR/$chart_name"
    
    if [[ ! -f "$chart_path" ]]; then
        echo -e "${RED}Error: Chart not found: $chart_name${NC}"
        exit 1
    fi
    
    echo -e "${BLUE}Chart: $chart_name${NC}"
    echo -e "${BLUE}Path:${NC} $chart_path"
    echo -e "${BLUE}Size:${NC} $(du -h "$chart_path" | cut -f1)"
    echo -e "\n${BLUE}Metadata:${NC}"
    tar -xzOf "$chart_path" "*/Chart.yaml" 2>/dev/null || echo "Could not read Chart.yaml"
}

show_path() {
    echo "$CHART_DIR"
}

# Main
case "${1:-}" in
    add)
        add_chart "$2"
        ;;
    add-from-release)
        add_from_release "$2" "$3" "${4:-}"
        ;;
    list)
        list_charts
        ;;
    remove)
        remove_chart "$2"
        ;;
    info)
        info_chart "$2"
        ;;
    path)
        show_path
        ;;
    -h|--help|help|"")
        usage
        ;;
    *)
        echo -e "${RED}Error: Unknown command: $1${NC}\n"
        usage
        exit 1
        ;;
esac
