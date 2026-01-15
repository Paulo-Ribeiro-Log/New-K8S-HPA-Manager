#!/bin/bash
# Script para criar release standalone do New-K8S-HPA-Manager
# Compila binários para Linux e macOS e faz upload para GitHub
# Autor: Paulo Ribeiro
# Data: 2026-01-14

set -e

# Cores
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m'

# Configurações
BINARY_NAME="new-k8s-hpa"
REPO_OWNER="Paulo-Ribeiro-Log"
REPO_NAME="New-K8S-HPA-Manager"
BUILD_DIR="build/release"

print_info() { echo -e "${BLUE}[INFO]${NC} $1"; }
print_success() { echo -e "${GREEN}[OK]${NC} $1"; }
print_warning() { echo -e "${YELLOW}[WARN]${NC} $1"; }
print_error() { echo -e "${RED}[ERRO]${NC} $1"; }

# Verificar pré-requisitos
check_prerequisites() {
    print_info "Verificando pré-requisitos..."

    # Verificar Go
    if ! command -v go &> /dev/null; then
        print_error "Go não encontrado. Instale em https://go.dev/dl/"
        exit 1
    fi
    print_success "Go $(go version | awk '{print $3}')"

    # Verificar gh CLI
    if ! command -v gh &> /dev/null; then
        print_error "GitHub CLI (gh) não encontrado"
        print_info "Instale com: sudo apt install gh"
        exit 1
    fi
    print_success "GitHub CLI instalado"

    # Verificar autenticação gh
    if ! gh auth status &>/dev/null; then
        print_error "GitHub CLI não autenticado"
        print_info "Execute: gh auth login"
        exit 1
    fi
    print_success "GitHub CLI autenticado"

    # Verificar jq
    if ! command -v jq &> /dev/null; then
        print_warning "jq não encontrado (opcional para release notes)"
    fi
}

# Detectar próxima versão
get_next_version() {
    local current_tag=$(git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0")

    # Extrair componentes da versão
    local version="${current_tag#v}"
    local major=$(echo "$version" | cut -d. -f1)
    local minor=$(echo "$version" | cut -d. -f2)
    local patch=$(echo "$version" | cut -d. -f3)

    # Incrementar patch
    patch=$((patch + 1))

    echo "v${major}.${minor}.${patch}"
}

# Build frontend (necessário antes do backend)
build_frontend() {
    print_info "Compilando frontend React..."

    if [ -d "internal/web/frontend" ]; then
        cd internal/web/frontend

        # Instalar dependências se necessário
        if [ ! -d "node_modules" ]; then
            print_info "Instalando dependências npm..."
            npm install --silent
        fi

        # Build
        npm run build --silent

        # Copiar para static
        cd ..
        rm -rf static/*
        cp -r frontend/dist/* static/

        cd ../..
        print_success "Frontend compilado e copiado para static/"
    else
        print_warning "Diretório frontend não encontrado, pulando..."
    fi
}

# Compilar binários
build_binaries() {
    local version=$1
    local build_date=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
    local git_commit=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")

    print_info "Compilando binários para versão $version..."

    # Criar diretório de release
    mkdir -p "$BUILD_DIR"
    rm -f "$BUILD_DIR"/*

    # Plataformas (sem Windows)
    declare -A platforms=(
        ["linux-amd64"]="linux amd64"
        ["darwin-amd64"]="darwin amd64"
        ["darwin-arm64"]="darwin arm64"
    )

    # LDFLAGS para injetar versão
    LDFLAGS="-X main.Version=$version -X main.BuildDate=$build_date -X main.GitCommit=$git_commit -s -w"

    for platform in "${!platforms[@]}"; do
        read -r os arch <<< "${platforms[$platform]}"
        output="$BUILD_DIR/$BINARY_NAME-$platform"

        print_info "  Compilando $platform..."

        CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build \
            -mod=vendor \
            -ldflags "$LDFLAGS" \
            -o "$output" \
            . 2>&1

        if [ -f "$output" ]; then
            chmod +x "$output"
            local size=$(du -h "$output" | cut -f1)
            print_success "  $platform ($size)"
        else
            print_error "  Falha ao compilar $platform"
            exit 1
        fi
    done

    print_success "Todos os binários compilados em $BUILD_DIR/"
    ls -lh "$BUILD_DIR/"
}

# Criar tag Git
create_tag() {
    local version=$1

    if git tag | grep -q "^$version$"; then
        print_warning "Tag $version já existe"
        read -p "Deseja recriar a tag? (s/N): " confirm
        if [[ "$confirm" =~ ^[Ss]$ ]]; then
            git tag -d "$version"
            git push origin --delete "$version" 2>/dev/null || true
        else
            return 0
        fi
    fi

    print_info "Criando tag $version..."
    git tag -a "$version" -m "Release $version"
    git push origin "$version"
    print_success "Tag $version criada e enviada"
}

# Gerar release notes
generate_release_notes() {
    local version=$1

    cat <<EOF
# K8s HPA Manager $version

## Binários Disponíveis

| Plataforma | Arquivo | Arquitetura |
|------------|---------|-------------|
| Linux | \`$BINARY_NAME-linux-amd64\` | x86_64 |
| macOS Intel | \`$BINARY_NAME-darwin-amd64\` | x86_64 |
| macOS Apple Silicon | \`$BINARY_NAME-darwin-arm64\` | ARM64 |

## Instalacao Rapida

### Linux / macOS
\`\`\`bash
# Download automatico (detecta plataforma)
curl -fsSL https://raw.githubusercontent.com/$REPO_OWNER/$REPO_NAME/main/install-from-github.sh | bash

# Ou download manual
# Linux
wget https://github.com/$REPO_OWNER/$REPO_NAME/releases/download/$version/$BINARY_NAME-linux-amd64
chmod +x $BINARY_NAME-linux-amd64
sudo mv $BINARY_NAME-linux-amd64 /usr/local/bin/$BINARY_NAME

# macOS Intel
wget https://github.com/$REPO_OWNER/$REPO_NAME/releases/download/$version/$BINARY_NAME-darwin-amd64
chmod +x $BINARY_NAME-darwin-amd64
sudo mv $BINARY_NAME-darwin-amd64 /usr/local/bin/$BINARY_NAME

# macOS Apple Silicon (M1/M2/M3)
wget https://github.com/$REPO_OWNER/$REPO_NAME/releases/download/$version/$BINARY_NAME-darwin-arm64
chmod +x $BINARY_NAME-darwin-arm64
sudo mv $BINARY_NAME-darwin-arm64 /usr/local/bin/$BINARY_NAME
\`\`\`

## Uso

\`\`\`bash
# Interface TUI
$BINARY_NAME

# Interface Web
$BINARY_NAME web

# Auto-descobrir clusters
$BINARY_NAME autodiscover

# Verificar versao
$BINARY_NAME version
\`\`\`

## Funcionalidades

- Gerenciamento de HPAs (Horizontal Pod Autoscalers)
- Gerenciamento de Node Pools (Azure AKS)
- Interface TUI e Web completas
- Sistema de sessoes (Save/Load)
- Monitoramento com Prometheus
- AI Diagnostics (Ollama/Claude)
- Health Checking de clusters
- Analise Preditiva de deployments
- RBAC com Azure AD

---
**Documentacao**: https://github.com/$REPO_OWNER/$REPO_NAME
EOF
}

# Criar release no GitHub
create_github_release() {
    local version=$1

    print_info "Criando release $version no GitHub..."

    # Gerar release notes
    local notes=$(generate_release_notes "$version")

    # Verificar se release já existe
    if gh release view "$version" &>/dev/null; then
        print_warning "Release $version já existe"
        read -p "Deseja deletar e recriar? (s/N): " confirm
        if [[ "$confirm" =~ ^[Ss]$ ]]; then
            gh release delete "$version" --yes
            print_info "Release antiga deletada"
        else
            print_info "Atualizando assets da release existente..."
            # Deletar assets antigos
            for asset in "$BUILD_DIR"/$BINARY_NAME-*; do
                local filename=$(basename "$asset")
                gh release delete-asset "$version" "$filename" --yes 2>/dev/null || true
            done
        fi
    fi

    # Criar release
    gh release create "$version" \
        --repo "$REPO_OWNER/$REPO_NAME" \
        --title "$version - Release Standalone" \
        --notes "$notes" \
        "$BUILD_DIR"/$BINARY_NAME-linux-amd64 \
        "$BUILD_DIR"/$BINARY_NAME-darwin-amd64 \
        "$BUILD_DIR"/$BINARY_NAME-darwin-arm64

    print_success "Release $version criada com sucesso!"
    echo ""
    echo -e "${GREEN}URL: https://github.com/$REPO_OWNER/$REPO_NAME/releases/tag/$version${NC}"
}

# Menu principal
main() {
    echo ""
    echo -e "${BLUE}========================================${NC}"
    echo -e "${BLUE}  K8s HPA Manager - Release Standalone  ${NC}"
    echo -e "${BLUE}========================================${NC}"
    echo ""

    check_prerequisites

    # Detectar versão
    local current_version=$(git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0")
    local next_version=$(get_next_version)

    echo ""
    print_info "Versao atual: $current_version"
    print_info "Proxima versao sugerida: $next_version"
    echo ""

    read -p "Qual versao deseja criar? [$next_version]: " input_version
    local version=${input_version:-$next_version}

    # Validar formato da versão
    if ! [[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        print_error "Formato de versao invalido. Use: vX.Y.Z"
        exit 1
    fi

    echo ""
    echo -e "${YELLOW}Acoes a serem executadas:${NC}"
    echo "  1. Compilar frontend React"
    echo "  2. Compilar binarios para Linux e macOS"
    echo "  3. Criar tag Git $version"
    echo "  4. Criar release no GitHub com binarios"
    echo ""

    read -p "Continuar? (s/N): " confirm
    if [[ ! "$confirm" =~ ^[Ss]$ ]]; then
        print_info "Operacao cancelada"
        exit 0
    fi

    echo ""

    # Executar etapas
    build_frontend
    echo ""

    build_binaries "$version"
    echo ""

    create_tag "$version"
    echo ""

    create_github_release "$version"

    echo ""
    echo -e "${GREEN}========================================${NC}"
    echo -e "${GREEN}  Release $version criada com sucesso!  ${NC}"
    echo -e "${GREEN}========================================${NC}"
    echo ""
    echo "Os usuarios podem instalar com:"
    echo ""
    echo "  curl -fsSL https://raw.githubusercontent.com/$REPO_OWNER/$REPO_NAME/main/install-from-github.sh | bash"
    echo ""
}

# Executar
main "$@"
