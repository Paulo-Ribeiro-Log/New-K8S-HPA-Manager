#!/bin/bash

# Script de teste para verificar conectividade do shell

echo "🔍 Testando Shell e Debug Connection"
echo "======================================"
echo ""

# Verificar se o servidor está rodando
echo "1. Verificando se o servidor está rodando..."
if pgrep -f "k8s-hpa-manager web" > /dev/null; then
    echo "✅ Servidor está rodando"
else
    echo "❌ Servidor NÃO está rodando"
    echo "   Execute: ./build/k8s-hpa-manager web"
    exit 1
fi

# Verificar porta
echo ""
echo "2. Verificando porta do servidor..."
PORT=$(netstat -tuln | grep ":8080" | wc -l)
if [ "$PORT" -gt 0 ]; then
    echo "✅ Servidor escutando na porta 8080"
else
    echo "⚠️  Porta 8080 não encontrada"
fi

# Verificar rotas de API
echo ""
echo "3. Testando rotas de API..."

# Testar rota de pods
echo "   - GET /api/v1/pods"
STATUS=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/api/v1/pods?cluster=test)
if [ "$STATUS" -eq 200 ] || [ "$STATUS" -eq 400 ]; then
    echo "     ✅ Rota de pods acessível (HTTP $STATUS)"
else
    echo "     ❌ Erro na rota de pods (HTTP $STATUS)"
fi

# Verificar se WebSocket está configurado
echo ""
echo "4. Verificando suporte a WebSocket..."
UPGRADE=$(curl -s -I http://localhost:8080/api/v1/pods/test/default/test/shell?container=app | grep -i "upgrade")
if [ -n "$UPGRADE" ]; then
    echo "✅ Suporte a WebSocket detectado"
else
    echo "ℹ️  WebSocket requer upgrade durante conexão"
fi

echo ""
echo "======================================"
echo "🎯 Configuração necessária:"
echo ""
echo "Frontend conectará via WebSocket em:"
echo "  ws://localhost:8080/api/v1/pods/{cluster}/{namespace}/{pod}/shell"
echo "  ws://localhost:8080/api/v1/pods/{cluster}/{namespace}/{pod}/debug"
echo ""
echo "Parâmetros necessários:"
echo "  - container=<container-name> (obrigatório)"
echo "  - shell=<shell-path> (opcional, default: /bin/bash)"
echo "  - image=<image-name> (opcional para debug, default: nicolaka/netshoot)"
echo ""
echo "======================================"
echo ""
echo "Para ver logs em tempo real:"
echo "  tail -f logs/web-server.log"
echo ""
