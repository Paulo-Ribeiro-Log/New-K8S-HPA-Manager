#!/bin/bash

# Script: Configurar Kiali para modo anonymous (sem token)
# Automatiza toda a configuração necessária

set -e

echo "======================================================"
echo "🔧 Configurar Kiali: Modo Anonymous (Sem Token)"
echo "======================================================"
echo ""

# Cores
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Variáveis
KIALI_NAMESPACE="istio-system"
KIALI_CM="kiali"

# ==============================================================================
# PASSO 1: Verificar se Kiali está instalado
# ==============================================================================
echo -e "${BLUE}[1/5] Verificando instalação do Kiali...${NC}"

if ! kubectl get cm -n "$KIALI_NAMESPACE" "$KIALI_CM" &>/dev/null; then
    echo -e "${RED}❌ ConfigMap do Kiali não encontrado em $KIALI_NAMESPACE${NC}"
    echo ""
    echo -e "${YELLOW}Kiali não está instalado. Instalar agora? (s/n)${NC}"
    read -r -p "> " INSTALL_KIALI

    if [[ "$INSTALL_KIALI" =~ ^[Ss]$ ]]; then
        echo ""
        echo -e "${BLUE}Instalando Kiali...${NC}"
        kubectl apply -f https://raw.githubusercontent.com/istio/istio/release-1.20/samples/addons/kiali.yaml

        echo ""
        echo -e "${BLUE}Aguardando instalação...${NC}"
        kubectl rollout status deployment/kiali -n "$KIALI_NAMESPACE" --timeout=120s

        echo -e "${GREEN}✅ Kiali instalado com sucesso${NC}"
    else
        echo -e "${RED}Instalação cancelada${NC}"
        exit 1
    fi
else
    echo -e "${GREEN}✅ Kiali encontrado${NC}"
fi

echo ""

# ==============================================================================
# PASSO 2: Backup do ConfigMap atual
# ==============================================================================
echo -e "${BLUE}[2/5] Criando backup do ConfigMap...${NC}"

BACKUP_FILE="/tmp/kiali-configmap-backup-$(date +%Y%m%d-%H%M%S).yaml"
kubectl get cm -n "$KIALI_NAMESPACE" "$KIALI_CM" -o yaml > "$BACKUP_FILE"

echo -e "${GREEN}✅ Backup salvo em: $BACKUP_FILE${NC}"
echo ""

# ==============================================================================
# PASSO 3: Verificar estratégia atual
# ==============================================================================
echo -e "${BLUE}[3/5] Verificando estratégia de autenticação atual...${NC}"

CURRENT_STRATEGY=$(kubectl get cm -n "$KIALI_NAMESPACE" "$KIALI_CM" -o jsonpath='{.data.config\.yaml}' 2>/dev/null | grep -A 5 "auth:" | grep "strategy:" | awk '{print $2}' | tr -d '\r' || echo "unknown")

echo "   Estratégia atual: $CURRENT_STRATEGY"

if [ "$CURRENT_STRATEGY" = "anonymous" ]; then
    echo -e "${GREEN}✅ Kiali já está configurado em modo anonymous${NC}"
    echo ""
    echo -e "${YELLOW}Deseja reiniciar o Kiali mesmo assim? (s/n)${NC}"
    read -r -p "> " RESTART_ANYWAY

    if [[ ! "$RESTART_ANYWAY" =~ ^[Ss]$ ]]; then
        echo ""
        echo -e "${GREEN}Configuração já está correta. Nada a fazer.${NC}"
        exit 0
    fi
else
    echo -e "${YELLOW}⚠️  Estratégia atual: $CURRENT_STRATEGY (requer token)${NC}"
    echo ""
fi

# ==============================================================================
# PASSO 4: Alterar para modo anonymous
# ==============================================================================
echo -e "${BLUE}[4/5] Alterando para modo anonymous...${NC}"

# Obter config.yaml atual
CURRENT_CONFIG=$(kubectl get cm -n "$KIALI_NAMESPACE" "$KIALI_CM" -o jsonpath='{.data.config\.yaml}')

# Substituir strategy: token/openid/etc por strategy: anonymous
NEW_CONFIG=$(echo "$CURRENT_CONFIG" | sed 's/strategy: token/strategy: anonymous/g; s/strategy: openid/strategy: anonymous/g; s/strategy: openshift/strategy: anonymous/g')

# Criar arquivo temporário com novo config
TEMP_FILE="/tmp/kiali-config-new.yaml"
cat > "$TEMP_FILE" << EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: $KIALI_CM
  namespace: $KIALI_NAMESPACE
data:
  config.yaml: |
$(echo "$NEW_CONFIG" | sed 's/^/    /')
EOF

# Aplicar novo ConfigMap
if kubectl apply -f "$TEMP_FILE"; then
    echo -e "${GREEN}✅ ConfigMap atualizado com sucesso${NC}"
    rm -f "$TEMP_FILE"
else
    echo -e "${RED}❌ Erro ao atualizar ConfigMap${NC}"
    echo -e "${YELLOW}Backup disponível em: $BACKUP_FILE${NC}"
    echo -e "${YELLOW}Restaurar com: kubectl apply -f $BACKUP_FILE${NC}"
    exit 1
fi

echo ""

# ==============================================================================
# PASSO 5: Reiniciar Kiali
# ==============================================================================
echo -e "${BLUE}[5/5] Reiniciando Kiali...${NC}"

# Reiniciar deployment
kubectl rollout restart deployment/kiali -n "$KIALI_NAMESPACE"

# Aguardar reinicialização
echo "   Aguardando reinicialização (pode demorar ~30 segundos)..."
if kubectl rollout status deployment/kiali -n "$KIALI_NAMESPACE" --timeout=120s; then
    echo -e "${GREEN}✅ Kiali reiniciado com sucesso${NC}"
else
    echo -e "${RED}❌ Timeout ao aguardar reinicialização${NC}"
    echo -e "${YELLOW}Verificar manualmente: kubectl get pods -n $KIALI_NAMESPACE -l app=kiali${NC}"
fi

echo ""

# ==============================================================================
# PASSO 6: Validar configuração
# ==============================================================================
echo -e "${BLUE}[6/6] Validando configuração...${NC}"

# Aguardar 5 segundos para pod ficar ready
sleep 5

# Verificar estratégia aplicada
NEW_STRATEGY=$(kubectl get cm -n "$KIALI_NAMESPACE" "$KIALI_CM" -o jsonpath='{.data.config\.yaml}' 2>/dev/null | grep -A 5 "auth:" | grep "strategy:" | awk '{print $2}' | tr -d '\r' || echo "unknown")

echo "   Nova estratégia: $NEW_STRATEGY"

if [ "$NEW_STRATEGY" = "anonymous" ]; then
    echo -e "${GREEN}✅ Estratégia alterada com sucesso${NC}"
else
    echo -e "${RED}❌ Estratégia não foi alterada (ainda: $NEW_STRATEGY)${NC}"
    echo -e "${YELLOW}Pode ser necessário aguardar mais tempo para pod reiniciar${NC}"
fi

# Verificar pods rodando
echo ""
echo "   Verificando pods do Kiali..."
kubectl get pods -n "$KIALI_NAMESPACE" -l app=kiali

echo ""

# ==============================================================================
# PASSO 7: Testar acesso (opcional)
# ==============================================================================
echo -e "${BLUE}[OPCIONAL] Testar acesso ao Kiali?${NC}"
echo -e "${YELLOW}Isso vai iniciar um kubectl proxy temporário (s/n)${NC}"
read -r -p "> " TEST_ACCESS

if [[ "$TEST_ACCESS" =~ ^[Ss]$ ]]; then
    echo ""
    echo -e "${BLUE}Iniciando kubectl proxy...${NC}"

    # Iniciar proxy em background
    kubectl proxy --port=8001 &>/dev/null &
    PROXY_PID=$!

    # Aguardar proxy ficar pronto
    sleep 2

    # Obter porta do Kiali
    KIALI_PORT=$(kubectl get svc -n "$KIALI_NAMESPACE" kiali -o jsonpath='{.spec.ports[0].port}')

    # Testar health
    echo "   Testando /api/status..."
    PROXY_URL="http://localhost:8001/api/v1/namespaces/$KIALI_NAMESPACE/services/http:kiali:$KIALI_PORT/proxy"

    STATUS_RESPONSE=$(curl -s -w "\n%{http_code}" "$PROXY_URL/api/status" 2>/dev/null || echo "ERROR\n000")
    STATUS_CODE=$(echo "$STATUS_RESPONSE" | tail -1)

    if [ "$STATUS_CODE" = "200" ]; then
        echo -e "${GREEN}✅ Acesso OK (HTTP 200)${NC}"

        KIALI_VERSION=$(echo "$STATUS_RESPONSE" | sed '$d' | jq -r '.status."Kiali version" // "Desconhecida"' 2>/dev/null || echo "Desconhecida")
        echo "   Kiali Version: $KIALI_VERSION"
    else
        echo -e "${YELLOW}⚠️  HTTP $STATUS_CODE (pode demorar mais tempo para pod ficar pronto)${NC}"
    fi

    # Matar proxy
    kill $PROXY_PID 2>/dev/null || true

    echo ""
fi

# ==============================================================================
# RESUMO
# ==============================================================================
echo "======================================================"
echo -e "${GREEN}✅ CONFIGURAÇÃO COMPLETA${NC}"
echo "======================================================"
echo ""
echo -e "${GREEN}Kiali configurado em modo anonymous!${NC}"
echo ""
echo "Próximos passos:"
echo "  1. Reiniciar aplicação K8s-HPA-Manager:"
echo "     ./build/new-k8s-hpa web -f"
echo ""
echo "  2. Testar aba Service Mesh no frontend:"
echo "     - Selecionar cluster e namespace"
echo "     - Clicar em 'Atualizar'"
echo "     - Visualizar grafo interativo"
echo ""
echo -e "${BLUE}Backup do ConfigMap anterior:${NC}"
echo "  $BACKUP_FILE"
echo ""
echo -e "${BLUE}Para reverter (se necessário):${NC}"
echo "  kubectl apply -f $BACKUP_FILE"
echo "  kubectl rollout restart deployment/kiali -n $KIALI_NAMESPACE"
echo ""
echo "======================================================"
