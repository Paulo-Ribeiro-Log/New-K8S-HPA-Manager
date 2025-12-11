#!/bin/bash

# Script: Diagnosticar erro 503 do Kiali
# Identifica por que Kiali retorna 503 (Service Unavailable)

set -e

echo "======================================================"
echo "🔍 Diagnóstico: Kiali Erro 503 (Service Unavailable)"
echo "======================================================"
echo ""

# Cores
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

KIALI_NAMESPACE="istio-system"

# ==============================================================================
# PASSO 1: Verificar pods do Kiali
# ==============================================================================
echo -e "${BLUE}[1/6] Verificando status dos pods do Kiali...${NC}"

KIALI_PODS=$(kubectl get pods -n "$KIALI_NAMESPACE" -l app=kiali -o jsonpath='{.items[*].metadata.name}')

if [ -z "$KIALI_PODS" ]; then
    echo -e "${RED}❌ Nenhum pod do Kiali encontrado${NC}"
    echo ""
    echo "Instalar Kiali:"
    echo "  kubectl apply -f https://raw.githubusercontent.com/istio/istio/release-1.20/samples/addons/kiali.yaml"
    exit 1
fi

echo -e "${GREEN}✅ Pods encontrados: $KIALI_PODS${NC}"
echo ""

# Verificar status detalhado
kubectl get pods -n "$KIALI_NAMESPACE" -l app=kiali

# Verificar se estão Ready
NOT_READY=$(kubectl get pods -n "$KIALI_NAMESPACE" -l app=kiali -o jsonpath='{.items[?(@.status.conditions[?(@.type=="Ready")].status!="True")].metadata.name}')

if [ -n "$NOT_READY" ]; then
    echo ""
    echo -e "${RED}⚠️  Pods não estão Ready: $NOT_READY${NC}"
    echo ""
    echo "Verificar logs:"
    for pod in $NOT_READY; do
        echo "  kubectl logs -n $KIALI_NAMESPACE $pod --tail=50"
    done
    echo ""
fi

echo ""

# ==============================================================================
# PASSO 2: Verificar se Prometheus está rodando
# ==============================================================================
echo -e "${BLUE}[2/6] Verificando Prometheus (dependência do Kiali)...${NC}"

# Kiali depende do Prometheus para obter métricas do Istio
PROM_PODS=$(kubectl get pods -n "$KIALI_NAMESPACE" -l app=prometheus -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || echo "")

if [ -z "$PROM_PODS" ]; then
    echo -e "${RED}❌ Prometheus NÃO encontrado em $KIALI_NAMESPACE${NC}"
    echo ""
    echo -e "${YELLOW}Kiali precisa do Prometheus para funcionar!${NC}"
    echo ""
    echo "Instalar Prometheus:"
    echo "  kubectl apply -f https://raw.githubusercontent.com/istio/istio/release-1.20/samples/addons/prometheus.yaml"
    echo ""
    echo "Aguardar instalação:"
    echo "  kubectl rollout status deployment/prometheus -n $KIALI_NAMESPACE"
    echo ""

    PROMETHEUS_MISSING=true
else
    echo -e "${GREEN}✅ Prometheus encontrado: $PROM_PODS${NC}"
    kubectl get pods -n "$KIALI_NAMESPACE" -l app=prometheus

    # Verificar se está Ready
    PROM_NOT_READY=$(kubectl get pods -n "$KIALI_NAMESPACE" -l app=prometheus -o jsonpath='{.items[?(@.status.conditions[?(@.type=="Ready")].status!="True")].metadata.name}')

    if [ -n "$PROM_NOT_READY" ]; then
        echo ""
        echo -e "${RED}⚠️  Prometheus não está Ready: $PROM_NOT_READY${NC}"
        PROMETHEUS_MISSING=true
    fi
fi

echo ""

# ==============================================================================
# PASSO 3: Verificar logs do Kiali
# ==============================================================================
echo -e "${BLUE}[3/6] Verificando logs do Kiali (últimas 30 linhas)...${NC}"

for pod in $KIALI_PODS; do
    echo ""
    echo "=== Logs do pod: $pod ==="
    kubectl logs -n "$KIALI_NAMESPACE" "$pod" --tail=30 2>&1 | grep -E "error|Error|ERROR|warn|Warn|WARN|fail|Fail|FAIL|503|prometheus" || echo "Nenhum erro óbvio encontrado"
done

echo ""

# ==============================================================================
# PASSO 4: Testar conectividade Kiali → Prometheus
# ==============================================================================
if [ "$PROMETHEUS_MISSING" != "true" ]; then
    echo -e "${BLUE}[4/6] Testando conectividade Kiali → Prometheus...${NC}"

    # Obter endpoint do Prometheus
    PROM_SERVICE=$(kubectl get svc -n "$KIALI_NAMESPACE" prometheus -o jsonpath='{.metadata.name}' 2>/dev/null || echo "")

    if [ -n "$PROM_SERVICE" ]; then
        PROM_ENDPOINT=$(kubectl get svc -n "$KIALI_NAMESPACE" prometheus -o jsonpath='{.spec.clusterIP}:{.spec.ports[0].port}')
        echo "   Prometheus endpoint: $PROM_ENDPOINT"

        # Testar conectividade via pod do Kiali
        KIALI_POD=$(echo "$KIALI_PODS" | awk '{print $1}')

        echo "   Testando curl de dentro do pod do Kiali..."
        CURL_TEST=$(kubectl exec -n "$KIALI_NAMESPACE" "$KIALI_POD" -- curl -s -o /dev/null -w "%{http_code}" "http://prometheus:9090/api/v1/query?query=up" 2>/dev/null || echo "FAIL")

        if [ "$CURL_TEST" = "200" ]; then
            echo -e "${GREEN}   ✅ Conectividade OK (HTTP 200)${NC}"
        else
            echo -e "${RED}   ❌ Falha na conectividade (HTTP $CURL_TEST)${NC}"
            echo ""
            echo -e "${YELLOW}Prometheus não está acessível de dentro do Kiali!${NC}"
            echo "Verificar:"
            echo "  - NetworkPolicies bloqueando tráfego"
            echo "  - Service do Prometheus correto"
            echo "  - Porta do Prometheus (padrão: 9090)"
        fi
    else
        echo -e "${RED}   ❌ Service do Prometheus não encontrado${NC}"
    fi
else
    echo -e "${BLUE}[4/6] Prometheus não disponível (pulando teste de conectividade)${NC}"
fi

echo ""

# ==============================================================================
# PASSO 5: Verificar configuração do Kiali
# ==============================================================================
echo -e "${BLUE}[5/6] Verificando configuração do Kiali...${NC}"

# Ver URL do Prometheus configurada no Kiali
PROM_URL=$(kubectl get cm -n "$KIALI_NAMESPACE" kiali -o jsonpath='{.data.config\.yaml}' 2>/dev/null | grep -A 10 "external_services:" | grep -A 5 "prometheus:" | grep "url:" | awk '{print $2}')

if [ -z "$PROM_URL" ]; then
    echo -e "${YELLOW}   ⚠️  URL do Prometheus não configurada explicitamente${NC}"
    echo "   Kiali deve usar discovery automático"
else
    echo "   Prometheus URL configurada: $PROM_URL"
fi

# Verificar namespace do Istio configurado
ISTIO_NS=$(kubectl get cm -n "$KIALI_NAMESPACE" kiali -o jsonpath='{.data.config\.yaml}' 2>/dev/null | grep "istio_namespace:" | awk '{print $2}')

if [ -z "$ISTIO_NS" ]; then
    echo "   Istio namespace: (padrão: istio-system)"
else
    echo "   Istio namespace: $ISTIO_NS"
fi

echo ""

# ==============================================================================
# PASSO 6: Testar API do Kiali diretamente
# ==============================================================================
echo -e "${BLUE}[6/6] Testando API do Kiali diretamente...${NC}"

# Iniciar proxy
kubectl proxy --port=8001 &>/dev/null &
PROXY_PID=$!
sleep 2

KIALI_PORT=$(kubectl get svc -n "$KIALI_NAMESPACE" kiali -o jsonpath='{.spec.ports[0].port}')
PROXY_URL="http://localhost:8001/api/v1/namespaces/$KIALI_NAMESPACE/services/http:kiali:$KIALI_PORT/proxy"

# Testar health
echo "   1. Testando /api/status (health check)..."
STATUS_RESPONSE=$(curl -s -w "\n%{http_code}" "$PROXY_URL/api/status" 2>/dev/null)
STATUS_CODE=$(echo "$STATUS_RESPONSE" | tail -1)

if [ "$STATUS_CODE" = "200" ]; then
    echo -e "      ${GREEN}✅ Health OK (HTTP 200)${NC}"
else
    echo -e "      ${RED}❌ HTTP $STATUS_CODE${NC}"
fi

# Testar namespaces
echo ""
echo "   2. Testando /api/namespaces (listar namespaces)..."
NS_RESPONSE=$(curl -s -w "\n%{http_code}" "$PROXY_URL/api/namespaces" 2>/dev/null)
NS_CODE=$(echo "$NS_RESPONSE" | tail -1)

if [ "$NS_CODE" = "200" ]; then
    echo -e "      ${GREEN}✅ Namespaces OK (HTTP 200)${NC}"

    # Mostrar namespaces disponíveis
    NS_BODY=$(echo "$NS_RESPONSE" | sed '$d')
    NS_NAMES=$(echo "$NS_BODY" | jq -r '.[].name' 2>/dev/null | head -5 || echo "")

    if [ -n "$NS_NAMES" ]; then
        echo "      Namespaces disponíveis (top 5):"
        echo "$NS_NAMES" | while read ns; do
            echo "        - $ns"
        done
    fi
else
    echo -e "      ${RED}❌ HTTP $NS_CODE${NC}"
    NS_BODY=$(echo "$NS_RESPONSE" | sed '$d')
    echo "$NS_BODY" | head -10
fi

# Testar graph (namespace production como exemplo)
echo ""
echo "   3. Testando /api/namespaces/default/graph (service graph)..."
GRAPH_RESPONSE=$(curl -s -w "\n%{http_code}" "$PROXY_URL/api/namespaces/default/graph?duration=60s&graphType=workload" 2>/dev/null)
GRAPH_CODE=$(echo "$GRAPH_RESPONSE" | tail -1)

if [ "$GRAPH_CODE" = "200" ]; then
    echo -e "      ${GREEN}✅ Service graph OK (HTTP 200)${NC}"

    GRAPH_BODY=$(echo "$GRAPH_RESPONSE" | sed '$d')
    NODES=$(echo "$GRAPH_BODY" | jq '.elements.nodes | length' 2>/dev/null || echo "0")
    EDGES=$(echo "$GRAPH_BODY" | jq '.elements.edges | length' 2>/dev/null || echo "0")

    echo "      Nodes: $NODES, Edges: $EDGES"

    if [ "$NODES" -eq 0 ]; then
        echo -e "      ${YELLOW}⚠️  Grafo vazio (normal se não há tráfego)${NC}"
    fi
elif [ "$GRAPH_CODE" = "503" ]; then
    echo -e "      ${RED}❌ HTTP 503 - Service Unavailable${NC}"
    echo ""
    echo "      Possíveis causas:"
    echo "        1. Prometheus não está rodando"
    echo "        2. Kiali não consegue se conectar ao Prometheus"
    echo "        3. Namespace não tem pods com Istio"

    GRAPH_BODY=$(echo "$GRAPH_RESPONSE" | sed '$d')
    echo ""
    echo "      Resposta do Kiali:"
    echo "$GRAPH_BODY" | jq . 2>/dev/null || echo "$GRAPH_BODY"
else
    echo -e "      ${RED}❌ HTTP $GRAPH_CODE${NC}"
fi

# Matar proxy
kill $PROXY_PID 2>/dev/null || true

echo ""

# ==============================================================================
# RESUMO E RECOMENDAÇÕES
# ==============================================================================
echo "======================================================"
echo -e "${BLUE}📊 RESUMO DO DIAGNÓSTICO${NC}"
echo "======================================================"
echo ""

if [ "$PROMETHEUS_MISSING" = "true" ]; then
    echo -e "${RED}❌ PROBLEMA IDENTIFICADO: Prometheus não está disponível${NC}"
    echo ""
    echo -e "${YELLOW}SOLUÇÃO:${NC}"
    echo "  1. Instalar Prometheus:"
    echo "     kubectl apply -f https://raw.githubusercontent.com/istio/istio/release-1.20/samples/addons/prometheus.yaml"
    echo ""
    echo "  2. Aguardar instalação:"
    echo "     kubectl rollout status deployment/prometheus -n $KIALI_NAMESPACE"
    echo ""
    echo "  3. Reiniciar Kiali:"
    echo "     kubectl rollout restart deployment/kiali -n $KIALI_NAMESPACE"
    echo ""
    echo "  4. Aguardar ~30 segundos e testar novamente"
    echo ""
elif [ "$GRAPH_CODE" = "503" ]; then
    echo -e "${RED}❌ PROBLEMA IDENTIFICADO: Kiali retorna 503 ao buscar service graph${NC}"
    echo ""
    echo -e "${YELLOW}CAUSAS POSSÍVEIS:${NC}"
    echo "  • Prometheus instalado mas não acessível"
    echo "  • NetworkPolicy bloqueando tráfego Kiali → Prometheus"
    echo "  • ConfigMap do Kiali com URL incorreta do Prometheus"
    echo "  • Namespace selecionado não existe ou não tem Istio habilitado"
    echo ""
    echo -e "${YELLOW}SOLUÇÕES:${NC}"
    echo "  1. Verificar logs do Kiali:"
    echo "     kubectl logs -n $KIALI_NAMESPACE -l app=kiali --tail=100 | grep -i prometheus"
    echo ""
    echo "  2. Verificar conectividade manual:"
    echo "     kubectl exec -n $KIALI_NAMESPACE deploy/kiali -- curl -v http://prometheus:9090/api/v1/query?query=up"
    echo ""
    echo "  3. Se Prometheus estiver em namespace diferente, atualizar ConfigMap do Kiali"
    echo ""
elif [ "$GRAPH_CODE" = "200" ] && [ "$NODES" -eq 0 ]; then
    echo -e "${GREEN}✅ Kiali está funcionando, mas grafo está vazio${NC}"
    echo ""
    echo -e "${YELLOW}CAUSA:${NC}"
    echo "  • Namespace não tem aplicações rodando"
    echo "  • Aplicações não têm Istio sidecars injetados"
    echo "  • Não há tráfego recente (últimos 60 segundos)"
    echo ""
    echo -e "${YELLOW}SOLUÇÃO:${NC}"
    echo "  1. Habilitar Istio em um namespace:"
    echo "     kubectl label namespace <NAMESPACE> istio-injection=enabled"
    echo ""
    echo "  2. Reiniciar deployments:"
    echo "     kubectl rollout restart deployment -n <NAMESPACE>"
    echo ""
    echo "  3. Gerar tráfego (ex: fazer requests para aplicações)"
    echo ""
    echo "  4. Aguardar 1 minuto e atualizar grafo"
else
    echo -e "${GREEN}✅ Kiali parece estar funcionando corretamente!${NC}"
    echo ""
    echo "Se ainda vê erro 503 na aplicação:"
    echo "  1. Verificar namespace selecionado existe e tem Istio"
    echo "  2. Verificar logs da aplicação: ./build/new-k8s-hpa web -f"
    echo "  3. Testar endpoint direto:"
    echo "     curl 'http://localhost:8080/api/v1/servicemesh/graph?cluster=<CLUSTER>&namespace=default&duration=60s'"
fi

echo ""
echo "======================================================"
