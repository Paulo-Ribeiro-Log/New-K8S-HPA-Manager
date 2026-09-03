#!/bin/bash
# Installer script for new-k8s-hpa from main branch
# Clones repository and compiles from source code

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
            echo "  curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/main/install-from-main.sh | bash"
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
CLONE_DIR="/tmp/new-k8s-hpa-install"
PROJECT_SUBDIR=""  # Arquivos estão na raiz do repositório

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
}

# Check requirements
check_requirements() {
    print_header "Verificando requisitos"

    local missing_requirements=0

    # Check Git
    if ! command -v git &> /dev/null; then
        print_error "git não encontrado (necessário para clonar repositório)"
        missing_requirements=1
    else
        print_success "git instalado"
    fi

    # Check Go
    if ! command -v go &> /dev/null; then
        print_error "Go não encontrado (necessário para compilar)"
        print_info "Instale Go 1.22+ em: https://go.dev/dl/"
        missing_requirements=1
    else
        GO_VERSION=$(go version | grep -oE 'go[0-9]+\.[0-9]+' | grep -oE '[0-9]+\.[0-9]+')
        GO_MAJOR=$(echo $GO_VERSION | cut -d. -f1)
        GO_MINOR=$(echo $GO_VERSION | cut -d. -f2)

        if [ "$GO_MAJOR" -lt 1 ] || ([ "$GO_MAJOR" -eq 1 ] && [ "$GO_MINOR" -lt 22 ]); then
            print_error "Go versão $GO_VERSION instalada (necessário 1.22+)"
            print_info "Atualize em: https://go.dev/dl/"
            missing_requirements=1
        else
            print_success "Go $GO_VERSION instalado"
        fi
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

    # Exit if missing critical requirements
    if [ $missing_requirements -eq 1 ]; then
        print_error "Requisitos críticos não satisfeitos"
        exit 1
    fi

    print_success "Todos os requisitos satisfeitos"
}

# Clone repository
clone_repository() {
    print_header "Clonando repositório (branch main)"

    # Remove old clone directory if exists
    if [ -d "$CLONE_DIR" ]; then
        print_info "Removendo clone anterior..."
        rm -rf "$CLONE_DIR"
    fi

    print_info "URL: https://github.com/$REPO_OWNER/$REPO_NAME.git"
    print_info "Branch: main"
    print_info "Destino: $CLONE_DIR"

    if git clone --depth 1 --branch main "https://github.com/$REPO_OWNER/$REPO_NAME.git" "$CLONE_DIR"; then
        print_success "Repositório clonado com sucesso"

        # Verify required files exist
        if [ ! -f "$CLONE_DIR/go.mod" ] || [ ! -d "$CLONE_DIR/cmd" ]; then
            print_error "Estrutura do projeto inválida - arquivos essenciais não encontrados"
            exit 1
        fi

        print_success "Estrutura do projeto validada"
    else
        print_error "Falha ao clonar repositório"
        exit 1
    fi
}

# Compile binary
compile_binary() {
    print_header "Compilando binário"

    local project_dir="$CLONE_DIR"

    print_info "Diretório: $project_dir"

    # Change to project directory
    cd "$project_dir" || exit 1

    # Get version from latest tag or use dev
    GIT_TAG=$(git describe --tags --abbrev=0 2>/dev/null || echo "")
    GIT_COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
    GIT_BRANCH=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "main")
    BUILD_DATE=$(date -u '+%Y-%m-%d_%H:%M:%S')

    # Use tag version if available, otherwise use dev version
    if [ -n "$GIT_TAG" ]; then
        VERSION="$GIT_TAG"
        print_info "Usando versão da tag: $VERSION"
    else
        VERSION="dev-$GIT_BRANCH-$GIT_COMMIT"
        print_info "Tag não encontrada, usando versão dev: $VERSION"
    fi

    print_info "Commit: $GIT_COMMIT"
    print_info "Data: $BUILD_DATE"

    # Compile with version injection
    print_info "Compilando (isso pode demorar alguns minutos)..."

    if go build -o "$CLONE_DIR/$BINARY_NAME" \

        -ldflags "-X main.Version=$VERSION -X main.BuildDate=$BUILD_DATE -X main.GitCommit=$GIT_COMMIT" \
        . 2>&1 | tee /tmp/compile.log; then


        print_success "Compilação concluída"

        # Verify binary was created
        if [ ! -f "$CLONE_DIR/$BINARY_NAME" ]; then
            print_error "Binário não encontrado após compilação"
            exit 1
        fi

        # Get file size
        local file_size=$(du -h "$CLONE_DIR/$BINARY_NAME" | cut -f1)
        print_info "Tamanho: $file_size"

        # Make executable
        chmod +x "$CLONE_DIR/$BINARY_NAME"

        BINARY_PATH="$CLONE_DIR/$BINARY_NAME"
    else
        print_error "Falha na compilação"
        print_info "Verifique os logs em /tmp/compile.log"
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
        print_info "Substituindo com versão compilada da main..."

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

    if [ -d "$CLONE_DIR" ]; then
        print_info "Removendo diretório temporário..."
        rm -rf "$CLONE_DIR"
        print_success "Limpeza concluída"
    fi
}

# Print usage instructions
print_usage() {
    print_header "Instalação Concluída com Sucesso! 🎉"

    echo ""
    echo -e "${GREEN}Instalado da branch main (compilado do código-fonte)${NC}"
    echo ""
    echo -e "${BLUE}📋 Comandos Principais:${NC}"
    echo "  $BINARY_NAME                      # Iniciar servidor web (default sem subcomando)"
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
    echo "  • Interface Web: Dashboard moderno React/TypeScript"
    echo "  • HPAs: Gerenciamento de Horizontal Pod Autoscalers"
    echo "  • Node Pools: Gerenciamento de node pools (AKS/EKS/GKE)"
    echo "  • CronJobs: Gerenciamento de CronJobs"
    echo "  • Prometheus: Métricas em tempo real (sem port-forward)"
    echo "  • Sessões: Save/Load de configurações"
    echo ""

    echo -e "${BLUE}⚙️ Configuração Inicial:${NC}"
    echo "  1. Configurar kubeconfig: ~/.kube/config"
    echo "  2. Azure login (AKS): az login"
    echo "  3. Auto-descobrir clusters: $BINARY_NAME autodiscover"
    echo "     (para AWS/EKS e GCP/GKE, o autodiscover pede login — aws sso login /"
    echo "     gcloud auth login — automaticamente, se necessário)"
    echo "  4. Iniciar aplicação: $BINARY_NAME"
    echo ""

    echo -e "${BLUE}📖 Documentação:${NC}"
    echo "  • GitHub: https://github.com/$REPO_OWNER/$REPO_NAME"
    echo "  • Releases: https://github.com/$REPO_OWNER/$REPO_NAME/releases"
    echo "  • Windows (WSL2): https://github.com/$REPO_OWNER/$REPO_NAME/blob/main/WINDOWS_SUPPORT.md"
    echo ""

    echo -e "${YELLOW}⚠️  Nota sobre atualização da branch main:${NC}"
    echo "  Esta instalação usa o código mais recente da branch 'main'."
    echo "  Para instalar versão estável, use o script oficial:"
    echo "  curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/main/install-from-github.sh | bash"
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
    print_header "🏗️  New K8s HPA Manager - Instalador (branch main)"

    echo ""
    echo "Este script irá:"
    echo "  1. Detectar plataforma (OS e arquitetura)"
    echo "  2. Clonar repositório da branch 'main'"
    echo "  3. Compilar binário do código-fonte"
    echo "  4. Instalar globalmente em $INSTALL_PATH"
    echo "  5. Auto-descobrir clusters (se kubectl disponível)"
    echo ""
    echo -e "${YELLOW}⚠️  Esta instalação usa código da branch 'main' (pode conter features experimentais)${NC}"
    echo -e "${YELLOW}   Para versão estável, use: install-from-github.sh${NC}"
    echo ""
    echo "Iniciando instalação..."
    echo ""

    # Execute installation steps
    detect_platform
    check_requirements
    clone_repository
    compile_binary
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
