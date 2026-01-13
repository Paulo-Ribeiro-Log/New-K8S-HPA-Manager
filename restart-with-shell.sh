#!/bin/bash

echo "🔄 Reiniciando servidor com suporte a Shell/Debug"
echo "=================================================="
echo ""

# Parar servidor antigo se estiver rodando
echo "1. Parando servidor antigo..."
pkill -f "k8s-hpa-manager web" 2>/dev/null
sleep 2

if pgrep -f "k8s-hpa-manager web" > /dev/null; then
    echo "   ⚠️  Servidor ainda rodando, forçando parada..."
    pkill -9 -f "k8s-hpa-manager web"
    sleep 1
fi
echo "   ✅ Servidor parado"

# Recompilar
echo ""
echo "2. Recompilando backend..."
go build -o build/new-k8s-hpa ./main.go
if [ $? -ne 0 ]; then
    echo "   ❌ Erro na compilação do backend"
    exit 1
fi
echo "   ✅ Backend compilado"

# Recompilar frontend
echo ""
echo "3. Recompilando frontend..."
cd internal/web/frontend
npm run build > /dev/null 2>&1
if [ $? -ne 0 ]; then
    echo "   ❌ Erro na compilação do frontend"
    exit 1
fi
cd ../../..
echo "   ✅ Frontend compilado"

# Iniciar servidor
echo ""
echo "4. Iniciando servidor..."
./build/new-k8s-hpa web &
SERVER_PID=$!
sleep 3

if pgrep -f "k8s-hpa-manager web" > /dev/null; then
    echo "   ✅ Servidor iniciado (PID: $SERVER_PID)"
else
    echo "   ❌ Falha ao iniciar servidor"
    exit 1
fi

# Testar endpoint
echo ""
echo "5. Testando endpoints..."
sleep 2

# Testar health
HEALTH=$(curl -s http://localhost:8080/api/v1/health | grep -o "ok")
if [ "$HEALTH" = "ok" ]; then
    echo "   ✅ Health check OK"
else
    echo "   ⚠️  Health check falhou"
fi

# Testar rota de shell (deve retornar 401 ou 400, não 404)
SHELL_STATUS=$(curl -s -o /dev/null -w "%{http_code}" "http://localhost:8080/api/v1/pods/test/test/test/shell?container=app")
if [ "$SHELL_STATUS" = "404" ]; then
    echo "   ❌ Rota /shell não encontrada (404)"
else
    echo "   ✅ Rota /shell existe (HTTP $SHELL_STATUS)"
fi

echo ""
echo "=================================================="
echo "✅ Servidor reiniciado com sucesso!"
echo ""
echo "Acesse: http://localhost:8080"
echo "Logs: tail -f logs do terminal"
echo ""
echo "Para testar shell:"
echo "  1. Abra http://localhost:8080"
echo "  2. Vá para aba Pods"
echo "  3. Selecione um pod"
echo "  4. Clique em 'Abrir Shell'"
echo ""
