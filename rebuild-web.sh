#!/bin/bash

# rebuild-web.sh
# Script para rebuild e restart do servidor web k8s-hpa-manager
# Autor: Gerado automaticamente
# Uso: ./rebuild-web.sh [opções]

set -e  # Exit on error

# Cores para output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configurações
PORT=${PORT:-8080}
PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUILD_DIR="$PROJECT_DIR/build"
BINARY="$BUILD_DIR/k8s-hpa-manager"

# Funções
print_header() {
    echo -e "${BLUE}╔════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${BLUE}║     k8s-hpa-manager - Web Rebuild & Restart Script       ║${NC}"
    echo -e "${BLUE}╚════════════════════════════════════════════════════════════╝${NC}"
    echo ""
}

print_success() {
    echo -e "${GREEN}✅ $1${NC}"
}

print_error() {
    echo -e "${RED}❌ $1${NC}"
}

print_info() {
    echo -e "${YELLOW}ℹ️  $1${NC}"
}

print_step() {
    echo -e "${BLUE}🔧 $1${NC}"
}

kill_existing() {
    print_step "Parando processos existentes na porta $PORT..."

    # Tentar matar processos na porta
    if fuser -k ${PORT}/tcp 2>/dev/null; then
        print_success "Processos na porta $PORT encerrados"
        sleep 2  # Aguardar liberação da porta
    else
        print_info "Nenhum processo rodando na porta $PORT"
    fi
}

build_project() {
    print_step "Compilando projeto (backend + frontend)..."

    cd "$PROJECT_DIR"

    # Usar build-web para compilar frontend + backend
    if make build-web; then
        print_success "Build completo com sucesso!"

        # Mostrar versão do build
        if [ -f "$BINARY" ]; then
            VERSION=$($BINARY version 2>/dev/null | head -n1 || echo "version unknown")
            print_info "Versão: $VERSION"
        fi
    else
        print_error "Falha no build!"
        exit 1
    fi
}

start_server() {
    local mode=$1

    print_step "Iniciando servidor web na porta $PORT..."

    if [ "$mode" = "background" ]; then
        # Rodar em background
        nohup "$BINARY" web --port "$PORT" > /tmp/k8s-hpa-web.log 2>&1 &
        local pid=$!
        sleep 2

        # Verificar se processo ainda está rodando
        if ps -p $pid > /dev/null; then
            print_success "Servidor iniciado em background (PID: $pid)"
            print_info "Logs: tail -f /tmp/k8s-hpa-web.log"
            print_info "URL: http://localhost:$PORT"
            print_info "Token: poc-token-123"
        else
            print_error "Servidor falhou ao iniciar. Verifique /tmp/k8s-hpa-web.log"
            exit 1
        fi
    else
        # Rodar em foreground
        print_success "Iniciando servidor em foreground (Ctrl+C para parar)..."
        print_info "URL: http://localhost:$PORT"
        echo ""
        "$BINARY" web --port "$PORT"
    fi
}

check_health() {
    print_step "Verificando health do servidor..."

    sleep 3  # Aguardar servidor iniciar

    if curl -s "http://localhost:$PORT/health" > /dev/null 2>&1; then
        print_success "Servidor respondendo corretamente!"
        return 0
    else
        print_error "Servidor não está respondendo em http://localhost:$PORT/health"
        return 1
    fi
}

show_usage() {
    cat << EOF
Uso: $0 [opções]

Opções:
    -h, --help          Mostra esta ajuda
    -b, --background    Inicia servidor em background
    -f, --foreground    Inicia servidor em foreground (padrão)
    -n, --no-build      Pula etapa de build (apenas restart)
    -p, --port PORT     Define porta do servidor (padrão: 8080)
    -k, --kill-only     Apenas mata processos existentes
    -s, --status        Verifica status do servidor

Exemplos:
    $0                  # Build + start em foreground
    $0 -b               # Build + start em background
    $0 -n -b            # Start em background sem rebuild
    $0 -p 3000 -b       # Build + start na porta 3000 em background
    $0 -k               # Apenas mata processos na porta 8080
    $0 -s               # Verifica se servidor está rodando

EOF
}

show_status() {
    print_step "Verificando status do servidor..."

    if lsof -i :$PORT > /dev/null 2>&1; then
        print_success "Servidor está rodando na porta $PORT"
        echo ""
        lsof -i :$PORT
        echo ""

        # Tentar health check
        if curl -s "http://localhost:$PORT/health" > /dev/null 2>&1; then
            print_success "Health check OK: http://localhost:$PORT/health"
        else
            print_error "Porta em uso mas health check falhou"
        fi
    else
        print_info "Nenhum servidor rodando na porta $PORT"
    fi
}

# Parse argumentos
MODE="foreground"
DO_BUILD=true
ACTION="rebuild"

while [[ $# -gt 0 ]]; do
    case $1 in
        -h|--help)
            show_usage
            exit 0
            ;;
        -b|--background)
            MODE="background"
            shift
            ;;
        -f|--foreground)
            MODE="foreground"
            shift
            ;;
        -n|--no-build)
            DO_BUILD=false
            shift
            ;;
        -p|--port)
            PORT="$2"
            shift 2
            ;;
        -k|--kill-only)
            ACTION="kill-only"
            shift
            ;;
        -s|--status)
            ACTION="status"
            shift
            ;;
        *)
            print_error "Opção desconhecida: $1"
            show_usage
            exit 1
            ;;
    esac
done

# Execução principal
print_header

case $ACTION in
    status)
        show_status
        exit 0
        ;;
    kill-only)
        kill_existing
        print_success "Processos encerrados!"
        exit 0
        ;;
    rebuild)
        kill_existing

        if [ "$DO_BUILD" = true ]; then
            build_project
        else
            print_info "Pulando build (--no-build especificado)"
        fi

        start_server "$MODE"

        if [ "$MODE" = "background" ]; then
            check_health
        fi
        ;;
esac

print_success "Concluído!"
