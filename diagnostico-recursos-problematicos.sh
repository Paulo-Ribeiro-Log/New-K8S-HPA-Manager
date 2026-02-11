#!/bin/bash

# diagnostico-recursos-problematicos.sh
# Script para diagnosticar por que pods/deployments com problemas não aparecem na interface

set -e

echo "=============================================="
echo "🔍 Diagnóstico de Recursos Problemáticos"
echo "=============================================="
echo ""

# Cores
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Função para exibir título de seção
section() {
  echo ""
  echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  echo -e "${BLUE}$1${NC}"
  echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
}

# Função para exibir OK
ok() {
  echo -e "${GREEN}✓${NC} $1"
}

# Função para exibir WARNING
warn() {
  echo -e "${YELLOW}⚠${NC} $1"
}

# Função para exibir ERROR
error() {
  echo -e "${RED}✗${NC} $1"
}

# Função para exibir INFO
info() {
  echo -e "${BLUE}ℹ${NC} $1"
}

# ============================================================================
# 1. Verificar Conectividade Kubernetes
# ============================================================================
section "1️⃣  Verificando Conectividade Kubernetes"

if ! command -v kubectl &> /dev/null; then
  error "kubectl não encontrado! Instale kubectl primeiro."
  exit 1
fi

# Cluster atual
CURRENT_CONTEXT=$(kubectl config current-context 2>/dev/null || echo "nenhum")
if [ "$CURRENT_CONTEXT" == "nenhum" ]; then
  error "Nenhum contexto Kubernetes configurado!"
  info "Execute: kubectl config use-context <cluster-name>"
  exit 1
fi

ok "Contexto atual: ${CURRENT_CONTEXT}"

# Testar conectividade
if kubectl cluster-info --request-timeout=5s &> /dev/null; then
  ok "Cluster acessível (API server respondendo)"
else
  error "Cluster inacessível! Verifique VPN e conectividade."
  info "Teste manualmente: kubectl cluster-info"
  exit 1
fi

# ============================================================================
# 2. Buscar Pods com Problemas
# ============================================================================
section "2️⃣  Buscando Pods com Problemas"

echo ""
info "Buscando pods em CrashLoopBackOff, ImagePullBackOff, Error, etc..."
echo ""

PROBLEMATIC_PODS=$(kubectl get pods --all-namespaces --field-selector status.phase!=Running,status.phase!=Succeeded 2>/dev/null | tail -n +2)

if [ -z "$PROBLEMATIC_PODS" ]; then
  ok "✅ Nenhum pod com problemas encontrado no cluster!"
  echo ""
  info "Todos os pods estão Running ou Succeeded."
  info "Se você esperava ver pods com problemas, verifique:"
  echo "  • Cluster correto está selecionado?"
  echo "  • Namespace correto está selecionado?"
  echo "  • Pods problemáticos foram resolvidos recentemente?"
else
  warn "❌ Encontrados pods com problemas:"
  echo ""
  echo "$PROBLEMATIC_PODS" | awk '{printf "  • %s/%s → %s (%s)\n", $1, $2, $4, $5}'
  echo ""
  info "Esses pods DEVEM aparecer na interface web!"
  info "Se não aparecem, siga os próximos passos."
fi

# Pods com muitos restarts
echo ""
info "Buscando pods com muitos restarts (>3)..."
echo ""

HIGH_RESTART_PODS=$(kubectl get pods --all-namespaces -o json 2>/dev/null | \
  jq -r '.items[] | select(.status.containerStatuses != null) |
    select(.status.containerStatuses[] | .restartCount > 3) |
    "\(.metadata.namespace)/\(.metadata.name) → \(.status.containerStatuses[0].restartCount) restarts"' 2>/dev/null || echo "")

if [ -z "$HIGH_RESTART_PODS" ]; then
  ok "Nenhum pod com >3 restarts"
else
  warn "Pods com muitos restarts:"
  echo "$HIGH_RESTART_PODS" | awk '{printf "  • %s\n", $0}'
fi

# ============================================================================
# 3. Buscar Deployments com Problemas
# ============================================================================
section "3️⃣  Buscando Deployments com Problemas"

echo ""
info "Buscando deployments com réplicas não prontas..."
echo ""

PROBLEMATIC_DEPS=$(kubectl get deployments --all-namespaces -o json 2>/dev/null | \
  jq -r '.items[] | select(.status.readyReplicas != .status.replicas) |
    "\(.metadata.namespace)/\(.metadata.name) → \(.status.readyReplicas // 0)/\(.status.replicas // 0) ready"' 2>/dev/null || echo "")

if [ -z "$PROBLEMATIC_DEPS" ]; then
  ok "✅ Nenhum deployment com réplicas não prontas encontrado!"
  echo ""
  info "Todos os deployments estão com réplicas prontas."
else
  warn "❌ Encontrados deployments com problemas:"
  echo ""
  echo "$PROBLEMATIC_DEPS" | awk '{printf "  • %s\n", $0}'
  echo ""
  info "Esses deployments DEVEM aparecer na interface web!"
  info "Se não aparecem, siga os próximos passos."
fi

# ============================================================================
# 4. Verificar Namespaces de Sistema
# ============================================================================
section "4️⃣  Verificando Namespaces de Sistema"

echo ""
info "Verificando se recursos problemáticos estão em namespaces de sistema..."
echo ""

SYSTEM_NAMESPACES=("kube-system" "kube-public" "kube-node-lease" "gatekeeper-system" "calico-system" "istio-system")

SYSTEM_PROBLEMS=""
for ns in "${SYSTEM_NAMESPACES[@]}"; do
  POD_COUNT=$(kubectl get pods -n "$ns" --field-selector status.phase!=Running,status.phase!=Succeeded 2>/dev/null | tail -n +2 | wc -l)
  if [ "$POD_COUNT" -gt 0 ]; then
    SYSTEM_PROBLEMS="${SYSTEM_PROBLEMS}\n  • Namespace: ${ns} → ${POD_COUNT} pods com problema"
  fi
done

if [ -z "$SYSTEM_PROBLEMS" ]; then
  ok "Nenhum recurso problemático em namespaces de sistema"
else
  warn "Recursos problemáticos encontrados em namespaces de sistema:"
  echo -e "$SYSTEM_PROBLEMS"
  echo ""
  info "⚠️  IMPORTANTE: Na interface web, ative o toggle 'Sistema' (👁️) para ver esses namespaces!"
fi

# ============================================================================
# 5. Verificar Backend da Aplicação
# ============================================================================
section "5️⃣  Verificando Backend da Aplicação"

echo ""
info "Verificando se servidor web está rodando..."
echo ""

if curl -s http://localhost:8080/healthz &> /dev/null; then
  ok "Servidor web rodando (http://localhost:8080)"
else
  error "Servidor web NÃO está rodando!"
  info "Inicie o servidor: ./build/new-k8s-hpa web"
  exit 1
fi

# Testar endpoint de pods
echo ""
info "Testando endpoint /api/v1/pods..."
echo ""

PODS_RESPONSE=$(curl -s "http://localhost:8080/api/v1/pods?cluster=${CURRENT_CONTEXT}&namespaces=" 2>/dev/null || echo "")

if [ -z "$PODS_RESPONSE" ]; then
  error "Endpoint /api/v1/pods não respondeu!"
  info "Verifique logs: tail -f /tmp/k8s-hpa-manager-web-*.log"
else
  POD_COUNT=$(echo "$PODS_RESPONSE" | jq '.count // 0' 2>/dev/null || echo "0")
  ok "Endpoint respondeu com ${POD_COUNT} pods"

  # Verificar se há pods com problemas na resposta
  PROBLEM_COUNT=$(echo "$PODS_RESPONSE" | jq '[.data[] | select(.phase != "Running" and .phase != "Succeeded")] | length' 2>/dev/null || echo "0")

  if [ "$PROBLEM_COUNT" -gt 0 ]; then
    ok "Backend retornou ${PROBLEM_COUNT} pods com problemas"
    info "✅ Backend está funcionando corretamente!"
  else
    warn "Backend retornou 0 pods com problemas"
    info "Verifique se o namespace correto está sendo consultado."
  fi
fi

# ============================================================================
# 6. Verificar Logs do Backend
# ============================================================================
section "6️⃣  Verificando Logs do Backend"

echo ""
LOG_FILE=$(ls -t /tmp/k8s-hpa-manager-web-*.log 2>/dev/null | head -1)

if [ -z "$LOG_FILE" ]; then
  warn "Nenhum log do backend encontrado em /tmp/"
  info "Execute em modo foreground para ver logs: ./build/new-k8s-hpa web -f"
else
  ok "Log encontrado: ${LOG_FILE}"

  ERROR_COUNT=$(grep -i "error" "$LOG_FILE" | tail -5 | wc -l)

  if [ "$ERROR_COUNT" -gt 0 ]; then
    warn "Últimos 5 erros no log:"
    echo ""
    grep -i "error" "$LOG_FILE" | tail -5 | awk '{printf "  %s\n", $0}'
    echo ""
    info "Revise o log completo: tail -f $LOG_FILE"
  else
    ok "Nenhum erro recente no log"
  fi
fi

# ============================================================================
# 7. Resumo e Recomendações
# ============================================================================
section "📊 Resumo e Recomendações"

echo ""
echo "Baseado no diagnóstico acima, verifique:"
echo ""

# Recomendações baseadas nos resultados
if [ -n "$PROBLEMATIC_PODS" ] || [ -n "$PROBLEMATIC_DEPS" ]; then
  echo "✅ Recursos com problemas EXISTEM no cluster"
  echo "   → Se não aparecem na interface, verifique:"
  echo ""
  echo "   1️⃣  Namespace correto está selecionado?"
  echo "      • Select de namespace no topo da aba"
  echo "      • Tente 'Todos os Namespaces'"
  echo ""
  echo "   2️⃣  Toggle 'Sistema' está ativo? (se recursos em kube-*)"
  echo "      • Clique no botão 👁️ no header"
  echo ""
  echo "   3️⃣  Cache do navegador atualizado?"
  echo "      • Faça hard refresh: Ctrl+Shift+R"
  echo "      • Rebuild frontend: ./rebuild-web.sh -b"
  echo ""
  echo "   4️⃣  Cluster correto está selecionado?"
  echo "      • Verifique card de Cluster no Dashboard"
  echo ""
else
  echo "ℹ️  Nenhum recurso com problema encontrado no cluster"
  echo "   → Todos os pods e deployments estão saudáveis"
  echo "   → Se você esperava ver problemas:"
  echo ""
  echo "   • Cluster correto está selecionado? (atual: ${CURRENT_CONTEXT})"
  echo "   • Problemas foram resolvidos recentemente?"
  echo "   • Recursos problemáticos foram deletados?"
fi

echo ""
info "Para mais detalhes, veja: TROUBLESHOOTING_PODS_DEPLOYMENTS.md"
echo ""
echo "=============================================="
echo "✅ Diagnóstico concluído!"
echo "=============================================="
