#!/bin/bash

# Script de Diagnóstico: Service Mesh (Kiali)
# Identifica problemas de conectividade com a API do Kiali

set -e

echo "======================================================"
echo "🔍 Diagnóstico: Service Mesh - Kiali API"
echo "======================================================"
echo ""

# Cores
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Variáveis
KIALI_FOUND=false
KIALI_NAMESPACE=""
KIALI_SERVICE=""
KIALI_PORT=""

# ==============================================================================
# PASSO 1: Verificar se Istio está instalado
# ==============================================================================
echo -e "${BLUE}[1/6] Verificando instalação do Istio...${NC}"

if kubectl get ns istio-system &>/dev/null; then
    echo -e "${GREEN}✅ Namespace istio-system encontrado${NC}"

    ISTIO_PODS=$(kubectl get pods -n istio-system 2>/dev/null | grep -v NAME | wc -l)
    if [ "$ISTIO_PODS" -gt 0 ]; then
        echo -e "${GREEN}✅ Istio está instalado ($ISTIO_PODS pods rodando)${NC}"
        kubectl get pods -n istio-system | head -6
    else
        echo -e "${YELLOW}⚠️  Namespace istio-system existe mas não há pods rodando${NC}"
    fi
else
    echo -e "${RED}❌ Namespace istio-system NÃO encontrado${NC}"
    echo -e "${YELLOW}   → Istio não está instalado neste cluster${NC}"
    echo ""
    echo -e "${BLUE}Para instalar Istio:${NC}"
    echo "   curl -L https://istio.io/downloadIstio | sh -"
    echo "   cd istio-*/"
    echo "   export PATH=\$PWD/bin:\$PATH"
    echo "   istioctl install --set profile=demo -y"
    exit 1
fi

echo ""

# ==============================================================================
# PASSO 2: Procurar serviço Kiali
# ==============================================================================
echo -e "${BLUE}[2/6] Procurando serviço Kiali...${NC}"

# Namespaces onde Kiali pode estar
SEARCH_NAMESPACES=("istio-system" "kiali" "kiali-operator" "observability")

for ns in "${SEARCH_NAMESPACES[@]}"; do
    echo -n "   Procurando em namespace '$ns'... "

    if kubectl get ns "$ns" &>/dev/null; then
        if kubectl get svc -n "$ns" kiali &>/dev/null; then
            echo -e "${GREEN}✅ ENCONTRADO${NC}"
            KIALI_FOUND=true
            KIALI_NAMESPACE="$ns"
            KIALI_SERVICE="kiali"
            KIALI_PORT=$(kubectl get svc -n "$ns" kiali -o jsonpath='{.spec.ports[0].port}')
            break
        else
            echo -e "${YELLOW}❌ Não encontrado${NC}"
        fi
    else
        echo -e "${YELLOW}❌ Namespace não existe${NC}"
    fi
done

if [ "$KIALI_FOUND" = false ]; then
    echo ""
    echo -e "${RED}❌ Serviço Kiali NÃO encontrado em nenhum namespace${NC}"
    echo ""
    echo -e "${YELLOW}Procurando Kiali em TODOS os namespaces...${NC}"
    KIALI_SEARCH=$(kubectl get svc --all-namespaces | grep kiali || true)

    if [ -z "$KIALI_SEARCH" ]; then
        echo -e "${RED}❌ Kiali não está instalado neste cluster${NC}"
        echo ""
        echo -e "${BLUE}Para instalar Kiali:${NC}"
        echo "   kubectl apply -f https://raw.githubusercontent.com/istio/istio/release-1.20/samples/addons/kiali.yaml"
        echo ""
        echo -e "${BLUE}Aguardar instalação:${NC}"
        echo "   kubectl rollout status deployment/kiali -n istio-system"
        echo ""
        echo -e "${BLUE}Verificar acesso:${NC}"
        echo "   kubectl port-forward -n istio-system svc/kiali 20001:20001"
        echo "   # Abrir: http://localhost:20001"
        exit 1
    else
        echo -e "${GREEN}Encontrado:${NC}"
        echo "$KIALI_SEARCH"
        echo ""
        echo -e "${YELLOW}⚠️  Kiali está em namespace não padrão.${NC}"
        echo -e "${YELLOW}   Atualize o código em: internal/web/handlers/servicemesh.go:265${NC}"
        exit 1
    fi
fi

echo ""
echo -e "${GREEN}✅ Kiali encontrado!${NC}"
echo "   Namespace: $KIALI_NAMESPACE"
echo "   Service:   $KIALI_SERVICE"
echo "   Port:      $KIALI_PORT"

echo ""

# ==============================================================================
# PASSO 3: Verificar pods do Kiali
# ==============================================================================
echo -e "${BLUE}[3/6] Verificando pods do Kiali...${NC}"

KIALI_PODS=$(kubectl get pods -n "$KIALI_NAMESPACE" -l app=kiali -o jsonpath='{.items[*].metadata.name}')

if [ -z "$KIALI_PODS" ]; then
    echo -e "${RED}❌ Nenhum pod do Kiali encontrado${NC}"
    echo ""
    echo -e "${YELLOW}Verificar deployment:${NC}"
    kubectl get deployment -n "$KIALI_NAMESPACE" kiali
    exit 1
fi

echo -e "${GREEN}✅ Pods encontrados:${NC}"
kubectl get pods -n "$KIALI_NAMESPACE" -l app=kiali

# Verificar se pods estão rodando
RUNNING_PODS=$(kubectl get pods -n "$KIALI_NAMESPACE" -l app=kiali -o jsonpath='{.items[*].status.phase}' | grep -c "Running" || echo "0")

if [ "$RUNNING_PODS" -eq 0 ]; then
    echo ""
    echo -e "${RED}❌ Nenhum pod do Kiali está em estado Running${NC}"
    echo ""
    echo -e "${YELLOW}Verificar logs:${NC}"
    echo "   kubectl logs -n $KIALI_NAMESPACE -l app=kiali --tail=50"
    exit 1
fi

echo ""

# ==============================================================================
# PASSO 4: Testar proxy do Kubernetes
# ==============================================================================
echo -e "${BLUE}[4/6] Testando proxy do Kubernetes...${NC}"

# Iniciar kubectl proxy em background
echo "   Iniciando kubectl proxy..."
kubectl proxy --port=8001 &>/dev/null &
PROXY_PID=$!

# Aguardar proxy ficar pronto
sleep 2

# Verificar se proxy está rodando
if ! kill -0 $PROXY_PID 2>/dev/null; then
    echo -e "${RED}❌ Falha ao iniciar kubectl proxy${NC}"
    exit 1
fi

echo -e "${GREEN}✅ Proxy iniciado (PID: $PROXY_PID)${NC}"

# Construir URL do proxy (igual ao código Go)
PROXY_URL="http://localhost:8001/api/v1/namespaces/$KIALI_NAMESPACE/services/http:$KIALI_SERVICE:$KIALI_PORT/proxy"

echo "   URL do proxy: $PROXY_URL"

# Testar health check do Kiali
echo ""
echo "   Testando /api/status (health check)..."

STATUS_RESPONSE=$(curl -s -w "\n%{http_code}" "$PROXY_URL/api/status" 2>/dev/null)
STATUS_CODE=$(echo "$STATUS_RESPONSE" | tail -1)
STATUS_BODY=$(echo "$STATUS_RESPONSE" | sed '$d')

if [ "$STATUS_CODE" = "200" ]; then
    echo -e "${GREEN}✅ Health check OK (HTTP 200)${NC}"

    # Extrair versão do Kiali
    KIALI_VERSION=$(echo "$STATUS_BODY" | jq -r '.status."Kiali version" // "Desconhecida"' 2>/dev/null || echo "Desconhecida")
    echo "   Kiali Version: $KIALI_VERSION"
else
    echo -e "${RED}❌ Health check falhou (HTTP $STATUS_CODE)${NC}"
    echo ""
    echo -e "${YELLOW}Resposta:${NC}"
    echo "$STATUS_BODY" | jq . 2>/dev/null || echo "$STATUS_BODY"

    # Matar proxy
    kill $PROXY_PID 2>/dev/null || true
    exit 1
fi

echo ""

# ==============================================================================
# PASSO 5: Testar API de namespace com Istio
# ==============================================================================
echo -e "${BLUE}[5/6] Testando API de namespaces com Istio...${NC}"

echo "   Listando namespaces com istio-injection=enabled..."

ISTIO_NAMESPACES=$(kubectl get ns -l istio-injection=enabled -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || echo "")

if [ -z "$ISTIO_NAMESPACES" ]; then
    echo -e "${YELLOW}⚠️  Nenhum namespace com istio-injection=enabled encontrado${NC}"
    echo ""
    echo -e "${YELLOW}Para habilitar Istio em um namespace:${NC}"
    echo "   kubectl label namespace <NAMESPACE> istio-injection=enabled"
    echo "   kubectl rollout restart deployment -n <NAMESPACE>"

    # Usar namespace default como fallback
    TEST_NAMESPACE="default"
    echo ""
    echo -e "${YELLOW}Usando namespace 'default' para teste...${NC}"
else
    echo -e "${GREEN}✅ Namespaces com Istio habilitado:${NC}"
    echo "   $ISTIO_NAMESPACES"

    # Usar primeiro namespace da lista
    TEST_NAMESPACE=$(echo "$ISTIO_NAMESPACES" | awk '{print $1}')
fi

echo ""
echo "   Testando service graph para namespace: $TEST_NAMESPACE"
echo "   URL: $PROXY_URL/api/namespaces/$TEST_NAMESPACE/graph?duration=60s&graphType=workload"

GRAPH_RESPONSE=$(curl -s -w "\n%{http_code}" "$PROXY_URL/api/namespaces/$TEST_NAMESPACE/graph?duration=60s&graphType=workload" 2>/dev/null)
GRAPH_CODE=$(echo "$GRAPH_RESPONSE" | tail -1)
GRAPH_BODY=$(echo "$GRAPH_RESPONSE" | sed '$d')

if [ "$GRAPH_CODE" = "200" ]; then
    echo -e "${GREEN}✅ Service graph obtido com sucesso (HTTP 200)${NC}"

    # Contar nodes e edges
    NODES_COUNT=$(echo "$GRAPH_BODY" | jq '.elements.nodes | length' 2>/dev/null || echo "0")
    EDGES_COUNT=$(echo "$GRAPH_BODY" | jq '.elements.edges | length' 2>/dev/null || echo "0")

    echo "   Nodes: $NODES_COUNT"
    echo "   Edges: $EDGES_COUNT"

    if [ "$NODES_COUNT" -eq 0 ]; then
        echo ""
        echo -e "${YELLOW}⚠️  Grafo vazio (sem tráfego recente no namespace)${NC}"
        echo -e "${YELLOW}   Isso é normal se não há aplicações rodando ou não há tráfego.${NC}"
    fi
else
    echo -e "${RED}❌ Erro ao obter service graph (HTTP $GRAPH_CODE)${NC}"
    echo ""
    echo -e "${YELLOW}Resposta:${NC}"
    echo "$GRAPH_BODY" | jq . 2>/dev/null || echo "$GRAPH_BODY"
fi

echo ""

# ==============================================================================
# PASSO 6: Verificar configuração do Kiali
# ==============================================================================
echo -e "${BLUE}[6/6] Verificando configuração do Kiali...${NC}"

# Obter ConfigMap do Kiali
if kubectl get cm -n "$KIALI_NAMESPACE" kiali &>/dev/null; then
    echo "   Estratégia de autenticação:"
    AUTH_STRATEGY=$(kubectl get cm -n "$KIALI_NAMESPACE" kiali -o jsonpath='{.data.config\.yaml}' 2>/dev/null | grep -A 5 "auth:" | grep "strategy:" | awk '{print $2}')

    if [ -z "$AUTH_STRATEGY" ]; then
        echo -e "${YELLOW}   ⚠️  Não foi possível determinar (padrão: anonymous)${NC}"
    else
        echo -e "${GREEN}   ✅ Strategy: $AUTH_STRATEGY${NC}"

        if [ "$AUTH_STRATEGY" != "anonymous" ]; then
            echo -e "${YELLOW}   ⚠️  Kiali requer autenticação!${NC}"
            echo -e "${YELLOW}      Backend precisa enviar token no header Authorization${NC}"
        fi
    fi
else
    echo -e "${YELLOW}   ⚠️  ConfigMap do Kiali não encontrado${NC}"
fi

# Matar proxy
kill $PROXY_PID 2>/dev/null || true

echo ""

# ==============================================================================
# RESUMO
# ==============================================================================
echo "======================================================"
echo -e "${GREEN}✅ DIAGNÓSTICO COMPLETO${NC}"
echo "======================================================"
echo ""
echo -e "${GREEN}Kiali está instalado e acessível via proxy do Kubernetes!${NC}"
echo ""
echo "Informações:"
echo "  • Namespace: $KIALI_NAMESPACE"
echo "  • Service:   $KIALI_SERVICE"
echo "  • Port:      $KIALI_PORT"
echo "  • Version:   $KIALI_VERSION"
echo ""
echo -e "${BLUE}Proxy Path (usado pelo backend Go):${NC}"
echo "  /api/v1/namespaces/$KIALI_NAMESPACE/services/http:$KIALI_SERVICE:$KIALI_PORT/proxy"
echo ""
echo -e "${BLUE}Testar manualmente:${NC}"
echo "  kubectl port-forward -n $KIALI_NAMESPACE svc/$KIALI_SERVICE $KIALI_PORT:$KIALI_PORT"
echo "  # Abrir: http://localhost:$KIALI_PORT"
echo ""
echo -e "${GREEN}Se a aplicação ainda falhar, verifique os logs:${NC}"
echo "  ./build/new-k8s-hpa web -f 2>&1 | grep -i 'servicemesh\\|kiali'"
echo ""
echo "======================================================"
