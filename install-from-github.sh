#!/bin/bash
# Installer script for new-k8s-hpa
# Downloads pre-compiled binary from GitHub releases

set -e

# Ignore SIGPIPE to prevent broken pipe errors from shell plugins (gitstatus, powerlevel10k, etc)
trap '' PIPE 2>/dev/null || true

# Parse arguments
for arg in "$@"; do
    case $arg in
        --help|-h)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --help, -h    Show this help message"
            echo ""
            echo "Example:"
            echo "  curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/main/install-from-github.sh | bash"
            exit 0
            ;;
    esac
done

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Project info
BINARY_NAME="new-k8s-hpa"
REPO_OWNER="Paulo-Ribeiro-Log"
REPO_NAME="New-K8S-HPA-Manager"
INSTALL_PATH="/usr/local/bin"
SCRIPTS_DIR="$HOME/.k8s-hpa-manager/scripts"

# Fetch latest release version from GitHub API
echo -e "${BLUE}ℹ️  Buscando última versão disponível no GitHub...${NC}"

RELEASE_VERSION=$(curl -s https://api.github.com/repos/$REPO_OWNER/$REPO_NAME/releases/latest | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')

if [ -z "$RELEASE_VERSION" ] || [ "$RELEASE_VERSION" = "null" ]; then
    echo -e "${RED}❌ Não foi possível detectar a última release${NC}"
    echo -e "${BLUE}ℹ️  Verifique se existem releases publicadas em:${NC}"
    echo -e "${BLUE}    https://github.com/$REPO_OWNER/$REPO_NAME/releases${NC}"
    exit 1
fi

echo -e "${GREEN}✅ Versão detectada: $RELEASE_VERSION${NC}"

# Function to print colored messages
print_info() {
    echo -e "${BLUE}ℹ️  $1${NC}"
}

print_success() {
    echo -e "${GREEN}✅ $1${NC}"
}

print_warning() {
    echo -e "${YELLOW}⚠️  $1${NC}"
}

print_error() {
    echo -e "${RED}❌ $1${NC}"
}

print_header() {
    echo ""
    echo -e "${BLUE}$1${NC}"
    echo "=================================================="
}

# Detect OS and architecture
detect_platform() {
    print_header "Detectando plataforma"

    OS=$(uname -s | tr '[:upper:]' '[:lower:]')
    ARCH=$(uname -m)

    case "$OS" in
        linux)
            OS_NAME="Linux"
            case "$ARCH" in
                x86_64|amd64)
                    PLATFORM="linux-amd64"
                    ;;
                *)
                    print_error "Arquitetura não suportada: $ARCH"
                    print_info "Plataformas suportadas: Linux amd64, macOS Intel (amd64), macOS Apple Silicon (arm64)"
                    exit 1
                    ;;
            esac
            ;;
        darwin)
            OS_NAME="macOS"
            case "$ARCH" in
                x86_64|amd64)
                    PLATFORM="darwin-amd64"
                    ;;
                arm64)
                    PLATFORM="darwin-arm64"
                    ;;
                *)
                    print_error "Arquitetura não suportada: $ARCH"
                    print_info "Plataformas suportadas: Linux amd64, macOS Intel (amd64), macOS Apple Silicon (arm64)"
                    exit 1
                    ;;
            esac
            ;;
        *)
            print_error "Sistema operacional não suportado: $OS"
            print_info "Plataformas suportadas: Linux, macOS"
            print_warning "Para Windows, use WSL2 (Windows Subsystem for Linux)"
            exit 1
            ;;
    esac

    print_success "Plataforma detectada: $OS_NAME ($ARCH)"
    print_info "Binário: $BINARY_NAME-$PLATFORM"
}

# Check basic requirements
check_requirements() {
    print_header "Verificando requisitos básicos"

    # Check curl
    if ! command -v curl &> /dev/null; then
        print_error "curl não encontrado (necessário para download)"
        exit 1
    else
        print_success "curl instalado"
    fi

    # Check kubectl (optional but recommended)
    if ! command -v kubectl &> /dev/null; then
        print_warning "kubectl não encontrado (necessário para operações K8s)"
    else
        print_success "kubectl instalado"
    fi

    # Check Azure CLI (optional)
    if ! command -v az &> /dev/null; then
        print_warning "Azure CLI não encontrado (necessário para operações de node pools)"
    else
        print_success "Azure CLI instalado"
    fi

    print_success "Requisitos básicos satisfeitos"
}

# Download binary from GitHub release
download_binary() {
    print_header "Baixando binário da release $RELEASE_VERSION"

    local download_url="https://github.com/$REPO_OWNER/$REPO_NAME/releases/download/$RELEASE_VERSION/$BINARY_NAME-$PLATFORM"
    local temp_file="/tmp/$BINARY_NAME-$PLATFORM"

    print_info "URL: $download_url"
    print_info "Baixando..."

    if curl -L -f -o "$temp_file" "$download_url"; then
        print_success "Download concluído"

        # Get file size
        local file_size=$(du -h "$temp_file" | cut -f1)
        print_info "Tamanho: $file_size"

        # Make executable
        chmod +x "$temp_file"

        BINARY_PATH="$temp_file"
    else
        print_error "Falha ao baixar binário"
        print_info "Verifique se a release $RELEASE_VERSION existe em:"
        print_info "  https://github.com/$REPO_OWNER/$REPO_NAME/releases/tag/$RELEASE_VERSION"
        exit 1
    fi
}

# Install binary globally
install_binary() {
    print_header "Instalando aplicação globalmente"

    # Check if binary already exists
    if command -v $BINARY_NAME &> /dev/null; then
        EXISTING_VERSION=$($BINARY_NAME version 2>/dev/null | head -1 || echo "versão desconhecida")
        print_info "$BINARY_NAME já instalado: $EXISTING_VERSION"
        print_info "Substituindo com release $RELEASE_VERSION..."

        # Check if web server is running and stop it
        if lsof -ti:8080 &> /dev/null; then
            print_warning "Servidor web rodando na porta 8080"
            print_info "Parando servidor antes de atualizar..."
            lsof -ti:8080 | xargs -r kill -9 2>/dev/null
            sleep 2
            print_success "Servidor parado"
        fi
    fi

    # Check if we need sudo
    if [[ ! -w "$INSTALL_PATH" ]]; then
        print_info "Privilégios de administrador necessários para instalação em $INSTALL_PATH"

        # Copy binary
        if sudo cp "$BINARY_PATH" "$INSTALL_PATH/$BINARY_NAME"; then
            print_success "Binário copiado para $INSTALL_PATH/$BINARY_NAME"
        else
            print_error "Falha ao copiar binário"
            exit 1
        fi

        # Set permissions
        if sudo chmod +x "$INSTALL_PATH/$BINARY_NAME"; then
            print_success "Permissões de execução definidas"
        else
            print_error "Falha ao definir permissões"
            exit 1
        fi
    else
        # Direct copy (if user has write permissions)
        cp "$BINARY_PATH" "$INSTALL_PATH/$BINARY_NAME"
        chmod +x "$INSTALL_PATH/$BINARY_NAME"
        print_success "Binário instalado"
    fi
}

# Test installation
test_installation() {
    print_header "Testando instalação"

    # Test if binary is in PATH
    if ! command -v $BINARY_NAME &> /dev/null; then
        print_error "$BINARY_NAME não encontrado no PATH"
        print_warning "Você pode precisar reiniciar o terminal ou adicionar $INSTALL_PATH ao PATH"
        return 1
    fi

    print_success "$BINARY_NAME disponível globalmente"

    # Test execution
    if $BINARY_NAME --help >/dev/null 2>&1; then
        print_success "Binário executa corretamente"
    else
        print_warning "Binário instalado mas pode ter problemas de execução"
        return 1
    fi

    # Show version
    VERSION_OUTPUT=$($BINARY_NAME version 2>/dev/null | head -1 || echo "Versão não disponível")
    print_info "$VERSION_OUTPUT"

    return 0
}

# Cleanup
cleanup() {
    print_header "Limpeza"

    if [ -f "$BINARY_PATH" ]; then
        print_info "Removendo arquivo temporário..."
        rm -f "$BINARY_PATH"
        print_success "Limpeza concluída"
    fi
}

# Print usage instructions
print_usage() {
    print_header "Instalação Concluída com Sucesso! 🎉"

    echo ""
    echo -e "${GREEN}Versão instalada: $RELEASE_VERSION${NC}"
    echo ""
    echo -e "${BLUE}📋 Comandos Principais:${NC}"
    echo "  $BINARY_NAME                      # Iniciar TUI"
    echo "  $BINARY_NAME web                  # Iniciar servidor web"
    echo "  $BINARY_NAME version              # Ver versão"
    echo "  $BINARY_NAME autodiscover         # Auto-descobrir clusters"
    echo "  $BINARY_NAME --help               # Ver ajuda completa"
    echo ""

    echo -e "${BLUE}🌐 Servidor Web:${NC}"
    echo "  $BINARY_NAME web                  # Background mode (default)"
    echo "  $BINARY_NAME web -f               # Foreground mode (logs no terminal)"
    echo "  $BINARY_NAME web --port 9000      # Custom port"
    echo "  Interface: http://localhost:8080"
    echo ""

    echo -e "${BLUE}📚 Recursos:${NC}"
    echo "  • Interface TUI: Terminal interativo completo"
    echo "  • Interface Web: Dashboard moderno React/TypeScript"
    echo "  • HPAs: Gerenciamento de Horizontal Pod Autoscalers"
    echo "  • Node Pools: Gerenciamento de Azure AKS node pools"
    echo "  • CronJobs: Gerenciamento de CronJobs (F9)"
    echo "  • Prometheus: Gerenciamento de Prometheus Stack (F8)"
    echo "  • Sessões: Save/Load de configurações"
    echo ""

    echo -e "${BLUE}⚙️ Configuração Inicial:${NC}"
    echo "  1. Configurar kubeconfig: ~/.kube/config"
    echo "  2. Azure login: az login"
    echo "  3. Auto-descobrir clusters: $BINARY_NAME autodiscover"
    echo "  4. Iniciar aplicação: $BINARY_NAME"
    echo ""

    echo -e "${BLUE}📖 Documentação:${NC}"
    echo "  • GitHub: https://github.com/$REPO_OWNER/$REPO_NAME"
    echo "  • Releases: https://github.com/$REPO_OWNER/$REPO_NAME/releases"
    echo "  • Windows (WSL2): https://github.com/$REPO_OWNER/$REPO_NAME/blob/main/WINDOWS_SUPPORT.md"
    echo ""

    echo -e "${GREEN}🚀 Pronto para gerenciar seus recursos Kubernetes!${NC}"
}

# Run autodiscover after installation
run_autodiscover() {
    print_header "Executando autodiscover de clusters"

    # Check if kubeconfig exists
    if [ ! -f "$HOME/.kube/config" ]; then
        print_warning "Kubeconfig não encontrado ($HOME/.kube/config)"
        print_info "Pule esta etapa se você não tem clusters configurados ainda"
        echo ""
        return
    fi

    # Check if kubectl is available
    if ! command -v kubectl &> /dev/null; then
        print_warning "kubectl não instalado - pulando autodiscover"
        return
    fi

    print_info "Detectando clusters do kubeconfig..."

    # Run autodiscover
    if "$BINARY_NAME" autodiscover; then
        print_success "Autodiscover concluído com sucesso"

        # Show summary
        local config_file="$HOME/.k8s-hpa-manager/clusters-config.json"
        if [ -f "$config_file" ]; then
            local cluster_count=$(jq '. | length' "$config_file" 2>/dev/null || echo "0")
            print_success "Total de clusters configurados: $cluster_count"
        fi
    else
        print_warning "Autodiscover falhou ou foi cancelado"
        print_info "Você pode executar manualmente depois com: $BINARY_NAME autodiscover"
    fi

    echo ""
}

# Main installation flow
main() {
    clear
    print_header "🏗️  New K8s HPA Manager - Instalador"

    echo ""
    echo "Este script irá:"
    echo "  1. Detectar plataforma (OS e arquitetura)"
    echo "  2. Baixar binário pré-compilado da release $RELEASE_VERSION"
    echo "  3. Instalar globalmente em $INSTALL_PATH"
    echo "  4. Auto-descobrir clusters (se kubectl disponível)"
    echo ""
    echo "Iniciando instalação..."
    echo ""

    # Execute installation steps
    detect_platform
    check_requirements
    download_binary
    install_binary

    if test_installation; then
        run_autodiscover
        cleanup
        print_usage
    else
        print_warning "Instalação concluída com avisos. Verifique as mensagens acima."
        cleanup
    fi
}

# Trap errors
trap 'print_error "Erro durante a instalação. Limpando..."; cleanup; exit 1' ERR

# Run main
main "$@"
