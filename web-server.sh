#!/bin/bash

# Script para gerenciar servidor web new-k8s-hpa

LOG_FILE="/tmp/new-k8s-hpa-web.log"
BIN_PATH="/usr/local/bin/new-k8s-hpa"

# Função de ajuda
show_help() {
    echo "Uso: $0 [PORTA] {start|stop|restart|status|logs}"
    echo ""
    echo "Comandos:"
    echo "  start    - Iniciar servidor web (padrão)"
    echo "  stop     - Parar servidor web"
    echo "  restart  - Reiniciar servidor web"  
    echo "  status   - Ver status do servidor"
    echo "  logs     - Ver logs em tempo real"
    echo ""
    echo "Opções:"
    echo "  -h, --help  - Mostrar esta ajuda"
    echo ""
    echo "Exemplos:"
    echo "  $0 start         # Iniciar na porta padrão (8080)"
    echo "  $0 8080 start    # Iniciar na porta 8080"
    echo "  $0 3000 start    # Iniciar na porta 3000"
    echo "  $0 stop          # Parar servidor"
    echo "  $0 restart       # Reiniciar servidor"
    echo "  $0 status        # Ver status"
    echo "  $0 logs          # Ver logs em tempo real"
    echo ""
    echo "Porta padrão: 8080"
}

# Verificar flags de ajuda primeiro
if [[ "$1" == "-h" || "$1" == "--help" ]]; then
    show_help
    exit 0
fi

# Verificar se o binário existe
if [ ! -f "$BIN_PATH" ]; then
    echo "❌ Erro: Binário não encontrado em $BIN_PATH"
    exit 1
fi

# Parsing de argumentos
if [[ "$1" =~ ^[0-9]+$ ]]; then
    # Primeiro argumento é uma porta
    PORT=$1
    ACTION=${2:-start}
elif [[ "$1" == "start" || "$1" == "stop" || "$1" == "restart" || "$1" == "status" || "$1" == "logs" ]]; then
    # Primeiro argumento é uma ação
    PORT=8080
    ACTION=$1
else
    # Usar padrões se não especificado
    PORT=8080
    ACTION="start"
fi

case "$ACTION" in
    start)
        echo "🚀 Iniciando servidor web na porta $PORT..."

        # JWT: usar secret definido no ambiente ou aplicar padrão do projeto
        if [ -z "$K8S_HPA_JWT_SECRET" ]; then
            export K8S_HPA_JWT_SECRET="k8s-hpa-manager-jwt-viavarejo-2026-secret"
        fi
        export K8S_HPA_JWT_TTL="${K8S_HPA_JWT_TTL:-8h}"

        # Verificar se a porta já está em uso
        if netstat -tuln 2>/dev/null | grep -q ":$PORT "; then
            echo "⚠️  Porta $PORT já está em uso"
            echo "Parando processo existente..."
        fi

        # Parar servidor se estiver rodando
        killall new-k8s-hpa 3>/dev/null

        # Iniciar servidor em background
        nohup $BIN_PATH web --port $PORT > $LOG_FILE 2>&1 &

        sleep 2

        # Verificar se iniciou
        if curl -s http://localhost:$PORT/health > /dev/null 2>&1; then
            echo "✅ Servidor rodando em http://localhost:$PORT"
            echo "📝 Logs: tail -f $LOG_FILE"
            echo "🔐 Autenticação: Azure AD via JWT (TTL: $K8S_HPA_JWT_TTL)"
        else
            echo "❌ Erro ao iniciar servidor. Verifique logs:"
            tail -20 $LOG_FILE
            exit 1
        fi
        ;;

    stop)
        echo "🛑 Parando servidor web..."
        if pgrep -f "new-k8s-hpa web" > /dev/null 2>&1; then
            pkill -f "new-k8s-hpa web" 2>/dev/null
            sleep 1
            # Verificar se parou
            if pgrep -f "new-k8s-hpa web" > /dev/null 2>&1; then
                echo "⚠️  Forçando parada do processo..."
                pkill -9 -f "new-k8s-hpa web" 2>/dev/null
            fi
            echo "✅ Servidor parado"
        else
            echo "⚠️  Servidor não estava rodando"
        fi
        ;;

    restart)
        echo "🔄 Reiniciando servidor web..."
        $0 $PORT stop
        sleep 2
        $0 $PORT start
        ;;

    status)
        if pgrep -f "new-k8s-hpa web" > /dev/null 2>&1; then
            PID=$(pgrep -f "new-k8s-hpa web")

            # Detectar porta real do processo em execução
            REAL_PORT=$(ps aux | grep "[n]ew-k8s-hpa web" | grep -oP '\-\-port\s+\K[0-9]+' | head -1)
            if [ -z "$REAL_PORT" ]; then
                # Se não encontrou --port na linha de comando, usar porta padrão 8080
                REAL_PORT=8080
            fi

            echo "✅ Servidor rodando (PID: $PID)"
            echo "📍 URL: http://localhost:$REAL_PORT"
            echo "📝 Logs: $LOG_FILE"

            # Verificar se está respondendo
            if command -v curl > /dev/null 2>&1; then
                if curl -s --connect-timeout 3 http://localhost:$REAL_PORT/health > /dev/null 2>&1; then
                    echo "🟢 Servidor respondendo normalmente"
                else
                    echo "🟡 Servidor rodando mas não respondendo na porta $REAL_PORT"
                fi
            fi
        else
            echo "❌ Servidor não está rodando"
        fi
        ;;

    logs)
        echo "📝 Logs do servidor (Ctrl+C para sair):"
        if [ -f "$LOG_FILE" ]; then
            tail -f $LOG_FILE
        else
            echo "❌ Arquivo de log não encontrado: $LOG_FILE"
            exit 1
        fi
        ;;

    *)
        echo "❌ Comando inválido: $ACTION"
        echo ""
        show_help
        exit 1
        ;;
esac
