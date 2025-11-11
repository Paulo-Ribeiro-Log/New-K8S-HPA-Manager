#!/bin/bash
# Complete installer script for k8s-hpa-manager
# This script clones the repository, builds, and installs the application globally
# It also copies utility scripts (web-server.sh, uninstall.sh) for easy management

set -e

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
            echo "  curl -fsSL https://raw.githubusercontent.com/.../install-from-github.sh | bash"
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
BINARY_NAME="k8s-hpa-manager"
REPO_URL="https://github.com/Paulo-Ribeiro-Log/Scale_HPA.git"
INSTALL_PATH="/usr/local/bin"
SCRIPTS_DIR="$HOME/.k8s-hpa-manager/scripts"
TEMP_DIR="/tmp/k8s-hpa-manager-install"

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

# Check system requirements
check_requirements() {
    print_header "Verificando requisitos do sistema"

    local missing_deps=()

    # Check Go
    if ! command -v go &> /dev/null; then
        missing_deps+=("Go 1.23+")
        print_error "Go não encontrado"
    else
        GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
        print_success "Go instalado: $GO_VERSION"
    fi

    # Check git
    if ! command -v git &> /dev/null; then
        missing_deps+=("git")
        print_error "Git não encontrado"
    else
        print_success "Git instalado: $(git --version | awk '{print $3}')"
    fi

    # Check kubectl
    if ! command -v kubectl &> /dev/null; then
        print_warning "kubectl não encontrado (necessário para operações K8s)"
    else
        print_success "kubectl instalado: $(kubectl version --client -o json 2>/dev/null | grep -o '"gitVersion":"[^"]*"' | head -1 | cut -d'"' -f4 || echo 'version unknown')"
    fi

    # Check Azure CLI
    if ! command -v az &> /dev/null; then
        print_warning "Azure CLI não encontrado (necessário para operações de node pools)"
    else
        print_success "Azure CLI instalado: $(az version -o tsv 2>/dev/null | head -1 || echo 'version unknown')"
    fi

    # If missing critical dependencies, exit
    if [ ${#missing_deps[@]} -gt 0 ]; then
        print_error "Dependências obrigatórias faltando:"
        for dep in "${missing_deps[@]}"; do
            echo "  • $dep"
        done
        echo ""
        echo "Por favor, instale as dependências e tente novamente."
        exit 1
    fi

    print_success "Todos os requisitos obrigatórios satisfeitos"
}

# Clone or update repository
clone_repository() {
    print_header "Clonando repositório"

    # Remove old temp directory if exists
    if [ -d "$TEMP_DIR" ]; then
        print_info "Removendo diretório temporário antigo..."
        rm -rf "$TEMP_DIR"
    fi

    # Clone repository
    print_info "Clonando de $REPO_URL..."
    CLONE_OUTPUT=$(git clone "$REPO_URL" "$TEMP_DIR" 2>&1)
    CLONE_STATUS=$?

    if [ $CLONE_STATUS -eq 0 ]; then
        print_success "Repositório clonado com sucesso"
    else
        print_error "Falha ao clonar repositório"
        echo "$CLONE_OUTPUT"
        exit 1
    fi

    cd "$TEMP_DIR"

    # Use main branch (always latest code)
    # Note: Tags will be used in future releases after v1.2.1
    print_info "Usando branch principal (main)"
}

# Build binary
build_binary() {
    print_header "Compilando aplicação"

    cd "$TEMP_DIR"

    # Detect version for build
    VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "dev")
    VERSION_CLEAN=$(echo "$VERSION" | sed 's/^v//')

    print_info "Compilando versão $VERSION_CLEAN..."

    # Build with version injection
    LDFLAGS="-X k8s-hpa-manager/internal/updater.Version=$VERSION_CLEAN"

    mkdir -p build
    if go build -ldflags "$LDFLAGS" -o "build/$BINARY_NAME" . ; then
        print_success "Compilação bem-sucedida"
    else
        print_error "Falha na compilação"
        exit 1
    fi

    # Get binary info
    BINARY_SIZE=$(du -h "build/$BINARY_NAME" | cut -f1)
    print_info "Tamanho do binário: $BINARY_SIZE"
}

# Install binary globally
install_binary() {
    print_header "Instalando aplicação globalmente"

    cd "$TEMP_DIR"

    # Check if binary already exists
    if command -v $BINARY_NAME &> /dev/null; then
        EXISTING_VERSION=$($BINARY_NAME version 2>/dev/null | head -1 || echo "versão desconhecida")
        print_info "$BINARY_NAME já instalado: $EXISTING_VERSION"
        print_info "Substituindo com nova versão..."

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
        if sudo cp "build/$BINARY_NAME" "$INSTALL_PATH/"; then
            print_success "Binário copiado para $INSTALL_PATH/"
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
        cp "build/$BINARY_NAME" "$INSTALL_PATH/"
        chmod +x "$INSTALL_PATH/$BINARY_NAME"
        print_success "Binário instalado"
    fi
}

# Copy utility scripts
copy_scripts() {
    print_header "Copiando scripts utilitários"

    cd "$TEMP_DIR"

    # Create scripts directory (but preserve sessions if they exist)
    local user_data_dir="$HOME/.k8s-hpa-manager"
    local sessions_dir="$user_data_dir/sessions"

    # Check if sessions directory already exists
    if [ -d "$sessions_dir" ]; then
        print_info "Sessões existentes detectadas - preservando dados do usuário"
        print_success "Diretório de sessões preservado: $sessions_dir"
    else
        print_info "Primeira instalação - criando estrutura de diretórios"
    fi

    # Create scripts directory
    mkdir -p "$SCRIPTS_DIR"

    # List of scripts to copy
    local scripts=("web-server.sh" "auto-update.sh" "uninstall.sh" "backup.sh" "restore.sh" "rebuild-web.sh")
    local copied_count=0

    for script in "${scripts[@]}"; do
        if [ -f "$script" ]; then
            cp "$script" "$SCRIPTS_DIR/"
            chmod +x "$SCRIPTS_DIR/$script"
            print_success "Copiado: $script"
            ((copied_count++))
        else
            print_warning "Script não encontrado: $script"
        fi
    done

    print_info "Scripts copiados para: $SCRIPTS_DIR"
    print_success "$copied_count scripts utilitários instalados"
}

# Create convenience aliases/links
create_aliases() {
    print_header "Criando atalhos convenientes"

    # Create symbolic links for commonly used scripts
    local link_created=false

    # web-server.sh -> k8s-hpa-web
    if [ -f "$SCRIPTS_DIR/web-server.sh" ]; then
        if [[ ! -w "$INSTALL_PATH" ]]; then
            if sudo ln -sf "$SCRIPTS_DIR/web-server.sh" "$INSTALL_PATH/k8s-hpa-web" 2>/dev/null; then
                print_success "Atalho criado: k8s-hpa-web"
                link_created=true
            fi
        else
            ln -sf "$SCRIPTS_DIR/web-server.sh" "$INSTALL_PATH/k8s-hpa-web" 2>/dev/null
            print_success "Atalho criado: k8s-hpa-web"
            link_created=true
        fi
    fi

    if [ "$link_created" = false ]; then
        print_info "Nenhum atalho criado (você pode usar os scripts em $SCRIPTS_DIR diretamente)"
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

    if [ -d "$TEMP_DIR" ]; then
        print_info "Removendo diretório temporário..."
        rm -rf "$TEMP_DIR"
        print_success "Limpeza concluída"
    fi
}

# Print usage instructions
print_usage() {
    print_header "Instalação Concluída com Sucesso! 🎉"

    echo ""
    echo -e "${BLUE}📋 Comandos Principais:${NC}"
    echo "  $BINARY_NAME                      # Iniciar TUI"
    echo "  $BINARY_NAME web                  # Iniciar servidor web"
    echo "  $BINARY_NAME version              # Ver versão e verificar updates"
    echo "  $BINARY_NAME autodiscover         # Auto-descobrir clusters"
    echo "  $BINARY_NAME --help               # Ver ajuda completa"
    echo ""

    echo -e "${BLUE}🌐 Servidor Web:${NC}"
    if command -v k8s-hpa-web &> /dev/null; then
        echo "  k8s-hpa-web start                 # Iniciar servidor (porta 8080)"
        echo "  k8s-hpa-web stop                  # Parar servidor"
        echo "  k8s-hpa-web status                # Ver status"
        echo "  k8s-hpa-web logs                  # Ver logs em tempo real"
    else
        echo "  $SCRIPTS_DIR/web-server.sh start  # Iniciar servidor"
        echo "  $SCRIPTS_DIR/web-server.sh stop   # Parar servidor"
        echo "  $SCRIPTS_DIR/web-server.sh status # Ver status"
    fi
    echo ""

    echo -e "${BLUE}🔧 Scripts Utilitários:${NC}"
    echo "  Localização: $SCRIPTS_DIR"
    echo "  • web-server.sh   - Gerenciar servidor web"
    echo "  • uninstall.sh    - Desinstalar aplicação"
    echo "  • backup.sh       - Fazer backup do código"
    echo "  • restore.sh      - Restaurar backup"
    echo "  • rebuild-web.sh  - Rebuild interface web"
    echo ""

    echo -e "${BLUE}📚 Recursos:${NC}"
    echo "  • Interface TUI: Terminal interativo completo"
    echo "  • Interface Web: http://localhost:8080 (após iniciar web-server)"
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

    echo -e "${GREEN}🚀 Pronto para gerenciar seus recursos Kubernetes!${NC}"
}

# Main installation flow
main() {
    clear
    print_header "🏗️  K8s HPA Manager - Instalador Completo"

    echo ""
    echo "Este script irá:"
    echo "  1. Verificar requisitos do sistema"
    echo "  2. Clonar o repositório do GitHub"
    echo "  3. Compilar a aplicação"
    echo "  4. Instalar globalmente em $INSTALL_PATH"
    echo "  5. Copiar scripts utilitários para $SCRIPTS_DIR"
    echo ""
    echo "Iniciando instalação..."
    echo ""

    # Execute installation steps
    check_requirements
    clone_repository
    build_binary
    install_binary
    copy_scripts
    create_aliases

    if test_installation; then
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
