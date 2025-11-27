#!/bin/bash
# Script de teste para o sistema de notificações Windows via WSL2

BASE_URL="http://localhost:8080/api/v1"
TOKEN="poc-token-123"

echo "🧪 Testando Sistema de Notificações Windows"
echo "=========================================="
echo ""

# 1. Status
echo "1️⃣  Verificando status..."
curl -s -H "Authorization: Bearer $TOKEN" "$BASE_URL/notifications/status" | jq
echo ""

# 2. Notificação de teste
echo "2️⃣  Enviando notificação de teste..."
curl -s -X POST -H "Authorization: Bearer $TOKEN" "$BASE_URL/notifications/test" | jq
echo ""
sleep 2

# 3. Alerta crítico
echo "3️⃣  Enviando alerta CRÍTICO..."
curl -s -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  "$BASE_URL/notifications/alert" -d '{
  "alertName": "HighCPU",
  "severity": "critical",
  "cluster": "akspriv-abastecimento-prd",
  "namespace": "default",
  "hpaName": "app-hpa",
  "message": "CPU acima de 90% por mais de 5 minutos"
}' | jq
echo ""
sleep 2

# 4. Alerta warning
echo "4️⃣  Enviando alerta WARNING..."
curl -s -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  "$BASE_URL/notifications/alert" -d '{
  "alertName": "MemoryPressure",
  "severity": "warning",
  "cluster": "akspriv-abastecimento-prd",
  "namespace": "monitoring",
  "hpaName": "prometheus-hpa",
  "message": "Uso de memória em 75%"
}' | jq
echo ""
sleep 2

# 5. Alerta info
echo "5️⃣  Enviando alerta INFO..."
curl -s -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  "$BASE_URL/notifications/alert" -d '{
  "alertName": "DeploymentScaled",
  "severity": "info",
  "cluster": "akspriv-abastecimento-hlg",
  "namespace": "default",
  "hpaName": "web-hpa",
  "message": "Deployment escalado de 2 para 5 réplicas"
}' | jq
echo ""

# 6. Status final (verificar cache)
echo "6️⃣  Status final (cache de deduplicação)..."
curl -s -H "Authorization: Bearer $TOKEN" "$BASE_URL/notifications/status" | jq
echo ""

# 7. Teste de deduplicação
echo "7️⃣  Testando deduplicação (mesmo alerta crítico novamente)..."
curl -s -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  "$BASE_URL/notifications/alert" -d '{
  "alertName": "HighCPU",
  "severity": "critical",
  "cluster": "akspriv-abastecimento-prd",
  "namespace": "default",
  "hpaName": "app-hpa",
  "message": "CPU acima de 90% por mais de 5 minutos"
}' | jq
echo ""

echo "✅ Testes concluídos!"
echo ""
echo "📊 Verificações:"
echo "   - Você deve ter recebido 4 notificações Windows Toast"
echo "   - O último alerta (deduplicação) NÃO deve ter gerado notificação"
echo "   - Cache deve mostrar 3 alertas únicos"
echo ""
echo "🎨 Estilos esperados:"
echo "   - 🔴 CRÍTICO: Som de alarme, ícone vermelho"
echo "   - 🟡 WARNING: Som de lembrete, ícone amarelo"
echo "   - ℹ️  INFO: Som padrão, ícone azul"
