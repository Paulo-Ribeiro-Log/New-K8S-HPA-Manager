#!/bin/bash

# Script para gerenciar servidor web k8s-hpa-manager

PORT=${1:-8080}
LOG_FILE="/tmp/k8s-hpa-web.log"

case "${2:-start}" in
    start)
        echo "🚀 Iniciando servidor web na porta $PORT..."

        # Parar servidor se estiver rodando
        killall k8s-hpa-manager 2>/dev/null

        # Iniciar servidor em background
        nohup ./build/k8s-hpa-manager web --port $PORT > $LOG_FILE 2>&1 &

        sleep 2

        # Verificar se iniciou
        if curl -s http://localhost:$PORT/health > /dev/null 2>&1; then
            echo "✅ Servidor rodando em http://localhost:$PORT"
            echo "📝 Logs: tail -f $LOG_FILE"
            echo "🔐 Token: poc-token-123"
        else
            echo "❌ Erro ao iniciar servidor. Verifique logs:"
            tail -20 $LOG_FILE
            exit 1
        fi
        ;;

    stop)
        echo "🛑 Parando servidor web..."
        killall k8s-hpa-manager 2>/dev/null
        echo "✅ Servidor parado"
        ;;

    restart)
        echo "🔄 Reiniciando servidor web..."
        $0 $PORT stop
        sleep 1
        $0 $PORT start
        ;;

    status)
        if pgrep -f "k8s-hpa-manager web" > /dev/null; then
            PID=$(pgrep -f "k8s-hpa-manager web")
            echo "✅ Servidor rodando (PID: $PID)"
            echo "📍 URL: http://localhost:$PORT"
            echo "📝 Logs: $LOG_FILE"
        else
            echo "❌ Servidor não está rodando"
        fi
        ;;

    logs)
        echo "📝 Logs do servidor (Ctrl+C para sair):"
        tail -f $LOG_FILE
        ;;

    *)
        echo "Uso: $0 [PORT] {start|stop|restart|status|logs}"
        echo ""
        echo "Exemplos:"
        echo "  $0 8080 start    # Iniciar na porta 8080"
        echo "  $0 stop          # Parar servidor"
        echo "  $0 restart       # Reiniciar servidor"
        echo "  $0 status        # Ver status"
        echo "  $0 logs          # Ver logs em tempo real"
        exit 1
        ;;
esac
