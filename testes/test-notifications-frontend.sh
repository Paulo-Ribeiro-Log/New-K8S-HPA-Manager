#!/bin/bash
# Script de teste para notificações in-app (frontend)

BASE_URL="http://localhost:8080/api/v1"
TOKEN="poc-token-123"

echo "🧪 Testando Sistema de Notificações In-App"
echo "=========================================="
echo ""

# 1. Enviar notificação crítica
echo "1️⃣  Enviando notificação CRÍTICA..."
curl -s -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  "$BASE_URL/notifications/alert" -d '{
  "alertName": "HighCPU",
  "severity": "critical",
  "cluster": "akspriv-teste-prd",
  "namespace": "production",
  "hpaName": "web-app-hpa",
  "message": "CPU acima de 90% por mais de 5 minutos"
}' | jq
echo ""
sleep 1

# 2. Enviar notificação warning
echo "2️⃣  Enviando notificação WARNING..."
curl -s -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  "$BASE_URL/notifications/alert" -d '{
  "alertName": "MemoryPressure",
  "severity": "warning",
  "cluster": "akspriv-teste-prd",
  "namespace": "production",
  "hpaName": "api-hpa",
  "message": "Uso de memória em 75%"
}' | jq
echo ""
sleep 1

# 3. Enviar notificação info
echo "3️⃣  Enviando notificação INFO..."
curl -s -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  "$BASE_URL/notifications/alert" -d '{
  "alertName": "DeploymentScaled",
  "severity": "info",
  "cluster": "akspriv-teste-hlg",
  "namespace": "staging",
  "hpaName": "worker-hpa",
  "message": "Deployment escalado de 2 para 5 réplicas automaticamente"
}' | jq
echo ""

# 4. Buscar todas as notificações
echo "4️⃣  Buscando todas as notificações..."
curl -s -H "Authorization: Bearer $TOKEN" "$BASE_URL/notifications/in-app" | jq
echo ""

# 5. Buscar apenas não lidas
echo "5️⃣  Buscando apenas não lidas..."
curl -s -H "Authorization: Bearer $TOKEN" "$BASE_URL/notifications/in-app/unread" | jq
echo ""

echo "✅ Testes concluídos!"
echo ""
echo "📌 Próximos Passos:"
echo "   1. Abra o navegador em http://localhost:8080"
echo "   2. Clique no sino (🔔) no header (ao lado do tema)"
echo "   3. Veja as 3 notificações no painel lateral"
echo "   4. Teste marcar como lida"
echo "   5. Aguarde 10 segundos (polling automático)"
echo ""
echo "🎨 Recursos para testar:"
echo "   - Badge com contador (3 não lidas)"
echo "   - Ícones por severidade (🔴 crítico, 🟡 warning, ℹ️ info)"
echo "   - Timestamps em português (há X minutos)"
echo "   - Marcar como lida (botão em cada notificação)"
echo "   - Marcar todas como lidas"
echo "   - Limpar todas"
