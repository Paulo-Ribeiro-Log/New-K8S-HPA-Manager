#!/bin/bash
# Script de diagnóstico para problemas com Node Pools

echo "🔍 Diagnóstico New-K8S-HPA-Manager"
echo "=================================="
echo ""

# 1. Verificar binário
echo "1️⃣ Verificando binário..."
which new-k8s-hpa
new-k8s-hpa version
echo ""

# 2. Verificar arquivo de configuração
echo "2️⃣ Verificando clusters-config.json..."
CONFIG_FILE="$HOME/.k8s-hpa-manager/clusters-config.json"
if [ -f "$CONFIG_FILE" ]; then
    echo "✅ Arquivo existe: $CONFIG_FILE"
    echo "Clusters configurados:"
    jq -r '.[].clusterName' "$CONFIG_FILE" | head -5
    echo "Total: $(jq '. | length' "$CONFIG_FILE") clusters"
else
    echo "❌ Arquivo NÃO EXISTE: $CONFIG_FILE"
    echo "Execute: new-k8s-hpa autodiscover"
fi
echo ""

# 3. Verificar kubeconfig
echo "3️⃣ Verificando kubeconfig..."
KUBECONFIG_FILE="$HOME/.kube/config"
if [ -f "$KUBECONFIG_FILE" ]; then
    echo "✅ Kubeconfig existe: $KUBECONFIG_FILE"
    echo "Contextos:"
    kubectl config get-contexts --no-headers | head -5
else
    echo "❌ Kubeconfig NÃO EXISTE: $KUBECONFIG_FILE"
fi
echo ""

# 4. Verificar Azure CLI
echo "4️⃣ Verificando Azure CLI..."
if command -v az &> /dev/null; then
    echo "✅ Azure CLI instalado"
    az version --output json | jq -r '."azure-cli"' 2>/dev/null || echo "Versão: $(az version --query '["azure-cli"]' -o tsv 2>/dev/null)"

    # Verificar login
    if az account show &> /dev/null; then
        echo "✅ Azure CLI autenticado"
        echo "Subscription ativa:"
        az account show --query '{name:name, id:id}' -o json | jq .
    else
        echo "❌ Azure CLI NÃO autenticado"
        echo "Execute: az login"
    fi
else
    echo "❌ Azure CLI NÃO instalado"
fi
echo ""

# 5. Testar servidor web (se estiver rodando)
echo "5️⃣ Testando servidor web..."
if curl -s http://localhost:8080/health > /dev/null 2>&1; then
    echo "✅ Servidor web está rodando"

    # Testar endpoint de clusters
    echo ""
    echo "Testando endpoint /api/v1/clusters..."
    CLUSTERS_RESPONSE=$(curl -s -H 'Authorization: Bearer poc-token-123' 'http://localhost:8080/api/v1/clusters')
    CLUSTER_COUNT=$(echo "$CLUSTERS_RESPONSE" | jq -r '.count' 2>/dev/null)

    if [ "$CLUSTER_COUNT" != "null" ] && [ -n "$CLUSTER_COUNT" ]; then
        echo "✅ Endpoint funcional - $CLUSTER_COUNT clusters detectados"
    else
        echo "❌ Erro ao buscar clusters"
        echo "Resposta: $CLUSTERS_RESPONSE"
    fi

    # Testar endpoint de node pools
    echo ""
    echo "Testando endpoint /api/v1/nodepools..."
    FIRST_CLUSTER=$(echo "$CLUSTERS_RESPONSE" | jq -r '.data[0].name' 2>/dev/null)

    if [ "$FIRST_CLUSTER" != "null" ] && [ -n "$FIRST_CLUSTER" ]; then
        echo "Cluster de teste: $FIRST_CLUSTER"

        NODEPOOLS_RESPONSE=$(curl -s -H 'Authorization: Bearer poc-token-123' "http://localhost:8080/api/v1/nodepools?cluster=$FIRST_CLUSTER")
        NODEPOOL_COUNT=$(echo "$NODEPOOLS_RESPONSE" | jq -r '.count' 2>/dev/null)

        if [ "$NODEPOOL_COUNT" != "null" ] && [ -n "$NODEPOOL_COUNT" ]; then
            echo "✅ Endpoint funcional - $NODEPOOL_COUNT node pools detectados"
            echo "Primeiros node pools:"
            echo "$NODEPOOLS_RESPONSE" | jq -r '.data[0:2] | .[] | "\(.name) - \(.vm_size) - \(.node_count) nodes"'
        else
            echo "❌ Erro ao buscar node pools"
            echo "Resposta: $NODEPOOLS_RESPONSE"
        fi
    fi
else
    echo "⚠️  Servidor web NÃO está rodando"
    echo "Inicie com: new-k8s-hpa web"
fi
echo ""

# 6. Verificar assets embarcados
echo "6️⃣ Verificando assets embarcados no binário..."
BINARY_PATH=$(which new-k8s-hpa)
if [ -f "$BINARY_PATH" ]; then
    echo "Binário: $BINARY_PATH"
    echo "Tamanho: $(du -h "$BINARY_PATH" | cut -f1)"

    # Verificar se assets estão embarcados
    if strings "$BINARY_PATH" | grep -q "index.*\.js"; then
        echo "✅ Assets JavaScript embarcados"
        strings "$BINARY_PATH" | grep -E "index.*\.js" | head -3
    else
        echo "❌ Assets JavaScript NÃO embarcados"
    fi

    if strings "$BINARY_PATH" | grep -q "index.*\.css"; then
        echo "✅ Assets CSS embarcados"
        strings "$BINARY_PATH" | grep -E "index.*\.css" | head -3
    else
        echo "❌ Assets CSS NÃO embarcados"
    fi
else
    echo "❌ Binário não encontrado no PATH"
fi
echo ""

# 7. Resumo
echo "📊 RESUMO"
echo "========"
echo ""
echo "Para corrigir problemas comuns:"
echo "  1. Node pools não carregam:"
echo "     - Execute: new-k8s-hpa autodiscover"
echo "     - Verifique: cat ~/.k8s-hpa-manager/clusters-config.json"
echo ""
echo "  2. Azure CLI não autenticado:"
echo "     - Execute: az login"
echo ""
echo "  3. Servidor web não inicia:"
echo "     - Verifique logs: tail -f /tmp/k8s-hpa-manager-web-*.log"
echo ""
echo "  4. Interface web em branco:"
echo "     - Hard refresh: Ctrl+Shift+R"
echo "     - Console do navegador: F12"
echo ""
