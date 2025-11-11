#!/bin/bash
# Auto-update script for k8s-hpa-manager
# This script checks for updates and automatically installs the latest version

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m'

BINARY_NAME="k8s-hpa-manager"
INSTALL_SCRIPT_URL="https://raw.githubusercontent.com/Paulo-Ribeiro-Log/Scale_HPA/main/install-from-github.sh"

# Global flags
AUTO_YES=false
DRY_RUN=false

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

print_dry_run() {
    if [ "$DRY_RUN" = true ]; then
        echo -e "${YELLOW}[DRY RUN] $1${NC}"
    fi
}

# Check if binary is installed
check_installation() {
    if ! command -v $BINARY_NAME &> /dev/null; then
        print_error "$BINARY_NAME não está instalado"
        echo ""
        echo "Para instalar pela primeira vez:"
        echo "  curl -fsSL $INSTALL_SCRIPT_URL | bash"
        exit 1
    fi
}

# Get current version
get_current_version() {
    local version_output=$($BINARY_NAME version 2>/dev/null | head -1)

    if [[ $version_output =~ versão[[:space:]]+([0-9]+\.[0-9]+\.[0-9]+) ]]; then
        echo "${BASH_REMATCH[1]}"
    else
        echo "unknown"
    fi
}

# Check for updates
check_for_updates() {
    print_info "Verificando atualizações disponíveis..."

    if [ "$DRY_RUN" = false ]; then
        # Force check by removing cache
        rm -f ~/.k8s-hpa-manager/.update-check
    else
        print_dry_run "Não removendo cache (dry run)"
    fi

    # Run version command and capture output
    local version_output=$($BINARY_NAME version 2>&1)

    # Check if update is available
    if echo "$version_output" | grep -q "Nova versão disponível"; then
        # Extract versions
        local version_line=$(echo "$version_output" | grep "Nova versão disponível")

        if [[ $version_line =~ ([0-9]+\.[0-9]+\.[0-9]+)[[:space:]]→[[:space:]]([0-9]+\.[0-9]+\.[0-9]+) ]]; then
            CURRENT_VERSION="${BASH_REMATCH[1]}"
            LATEST_VERSION="${BASH_REMATCH[2]}"
            return 0
        fi
    fi

    return 1
}

# Backup user data before update
backup_user_data() {
    local backup_dir="$HOME/.k8s-hpa-manager-backup-$(date +%Y%m%d_%H%M%S)"
    local data_dir="$HOME/.k8s-hpa-manager"

    # Check if data directory exists
    if [ ! -d "$data_dir" ]; then
        return 0  # Nothing to backup
    fi

    print_info "🔒 Criando backup de segurança dos dados do usuário..."

    if [ "$DRY_RUN" = true ]; then
        print_dry_run "Backup seria criado em: $backup_dir"
        return 0
    fi

    # Create backup
    if cp -r "$data_dir" "$backup_dir"; then
        print_success "Backup criado: $backup_dir"
        print_info "💡 Backup disponível em caso de problemas"
        echo ""
    else
        print_warning "Não foi possível criar backup (continuando...)"
    fi
}

# Perform update
perform_update() {
    print_header "Iniciando atualização"

    print_info "Versão atual: $CURRENT_VERSION"
    print_info "Versão disponível: $LATEST_VERSION"
    echo ""

    # Skip confirmation if --yes flag is set
    if [ "$AUTO_YES" = false ]; then
        read -p "Deseja atualizar agora? [Y/n]: " -n 1 -r
        echo ""

        if [[ $REPLY =~ ^[Nn]$ ]]; then
            print_info "Atualização cancelada pelo usuário"
            exit 0
        fi
    else
        print_info "Auto-confirmação ativada (--yes), prosseguindo com atualização..."
        echo ""
    fi

    if [ "$DRY_RUN" = true ]; then
        print_dry_run "Backup de segurança seria criado"
        print_dry_run "Simulando download e instalação..."
        print_dry_run "curl -fsSL $INSTALL_SCRIPT_URL | bash"
        print_dry_run "Sessões existentes seriam preservadas automaticamente"
        echo ""
        print_success "Simulação concluída! (modo dry-run)"
        print_info "Execute sem --dry-run para instalar de verdade"
        return 0
    fi

    # Backup user data BEFORE update (safety measure)
    backup_user_data

    print_info "Baixando e executando instalador..."
    print_info "✅ Suas sessões existentes serão preservadas automaticamente"
    echo ""

    # Download and execute installer
    # Note: install-from-github.sh automatically preserves existing sessions
    if curl -fsSL "$INSTALL_SCRIPT_URL" | bash; then
        print_success "Atualização concluída com sucesso!"
        echo ""

        # Verify new version
        NEW_VERSION=$(get_current_version)
        print_info "Versão instalada: $NEW_VERSION"

        if [ "$NEW_VERSION" = "$LATEST_VERSION" ]; then
            print_success "Você está usando a versão mais recente!"
        else
            print_warning "Versão instalada ($NEW_VERSION) difere da esperada ($LATEST_VERSION)"
            print_info "Execute 'k8s-hpa-manager version' para verificar"
        fi
    else
        print_error "Falha na atualização"
        print_info "💡 Backup disponível em caso de necessidade de restauração manual"
        exit 1
    fi
}

# Show current status
show_status() {
    print_header "Status da Instalação"

    CURRENT_VERSION=$(get_current_version)
    print_info "Versão atual: $CURRENT_VERSION"
    print_info "Localização: $(which $BINARY_NAME)"

    # Check for updates
    if check_for_updates; then
        echo ""
        print_warning "Nova versão disponível: $CURRENT_VERSION → $LATEST_VERSION"
        echo ""
        echo "Execute '$0 --update' para atualizar"
        echo "Ou '$0 --yes' para atualizar sem confirmação"
    else
        echo ""
        print_success "Você está usando a versão mais recente!"
    fi
}

# Main function
main() {
    # Parse flags first
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --yes|-y)
                AUTO_YES=true
                shift
                ;;
            --dry-run|-d)
                DRY_RUN=true
                shift
                ;;
            --update|-u|--check|-c|--force|-f|--help|-h)
                # These are commands, not flags - stop parsing
                break
                ;;
            *)
                # Unknown option
                shift
                ;;
        esac
    done

    if [ "$DRY_RUN" = false ]; then
        clear
    fi

    print_header "🔄 K8s HPA Manager - Auto Update"

    if [ "$DRY_RUN" = true ]; then
        print_warning "MODO DRY RUN - Nenhuma alteração será feita"
    fi

    # Check if installed
    check_installation

    # Parse commands
    case "${1:-}" in
        --update|-u)
            # Force update
            if check_for_updates; then
                perform_update
            else
                CURRENT_VERSION=$(get_current_version)
                print_success "Você já está usando a versão mais recente ($CURRENT_VERSION)"
            fi
            ;;

        --check|-c)
            # Just check, don't update
            show_status
            ;;

        --force|-f)
            # Force reinstall (even if up-to-date)
            if [ "$DRY_RUN" = false ]; then
                print_warning "Forçando reinstalação..."
            else
                print_dry_run "Forçaria reinstalação..."
            fi
            CURRENT_VERSION=$(get_current_version)
            LATEST_VERSION="latest"
            perform_update
            ;;

        --help|-h)
            echo "Uso: $0 [OPÇÕES] [COMANDO]"
            echo ""
            echo "Comandos:"
            echo "  (sem argumentos)  Verificar e atualizar se houver nova versão"
            echo "  --update, -u      Mesmo que sem argumentos"
            echo "  --check, -c       Apenas verificar, não atualizar"
            echo "  --force, -f       Forçar reinstalação (mesmo se atualizado)"
            echo "  --help, -h        Mostrar esta ajuda"
            echo ""
            echo "Opções (usar antes do comando):"
            echo "  --yes, -y         Auto-confirmar (não pedir confirmação)"
            echo "  --dry-run, -d     Simular ações sem executar (modo teste)"
            echo ""
            echo "Exemplos:"
            echo "  $0                      # Verificar e atualizar (interativo)"
            echo "  $0 --check              # Apenas verificar status"
            echo "  $0 --yes                # Atualizar sem perguntar"
            echo "  $0 --dry-run            # Simular atualização (teste)"
            echo "  $0 --yes --force        # Forçar reinstalação sem perguntar"
            echo "  $0 --dry-run --update   # Simular atualização"
            echo ""
            echo "Uso em scripts/cron:"
            echo "  # Cron para atualizar automaticamente toda segunda às 9h"
            echo "  0 9 * * 1 $0 --yes >> /var/log/k8s-hpa-update.log 2>&1"
            echo ""
            echo "  # Script bash com verificação de erro"
            echo "  if $0 --yes; then"
            echo "    echo 'Atualização bem-sucedida'"
            echo "  else"
            echo "    echo 'Falha na atualização' | mail -s 'Update Failed' admin@example.com"
            echo "  fi"
            ;;

        *)
            # Default: check and update if available
            if check_for_updates; then
                perform_update
            else
                show_status
            fi
            ;;
    esac
}

# Run main
main "$@"
