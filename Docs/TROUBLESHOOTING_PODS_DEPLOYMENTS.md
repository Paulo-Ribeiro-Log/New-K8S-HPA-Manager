# Troubleshooting: Pods e Deployments com Problemas Não Aparecem

## Diagnóstico Rápido

Se você não está vendo pods ou deployments com problemas na interface web, siga este guia:

### 1️⃣ Verificar Namespace Selecionado

**Problema**: Recursos com problemas podem estar em namespace diferente.

**Solução**:
```bash
# Listar pods com problemas em todos os namespaces
kubectl get pods --all-namespaces --field-selector status.phase!=Running

# Listar deployments com réplicas não prontas
kubectl get deployments --all-namespaces --field-selector status.readyReplicas!=status.replicas
```

**Na Interface Web**:
- Verifique o **select de namespace** no topo da aba
- Tente selecionar "Todos os Namespaces" ou namespace específico onde o problema existe

---

### 2️⃣ Verificar Toggle "Sistema"

**Problema**: Namespaces de sistema estão ocultos por padrão.

**Solução**:
- Clique no botão **👁️ Sistema** (Eye icon) no header da aba
- Isso exibe namespaces como: `kube-system`, `istio-system`, `calico-system`, etc

**Namespaces de Sistema**:
```
kube-system
kube-public
kube-node-lease
gatekeeper-system
calico-system
istio-system
```

---

### 3️⃣ Verificar Cluster Selecionado

**Problema**: Recursos problemáticos podem estar em cluster diferente.

**Solução**:
```bash
# Listar todos os contextos disponíveis
kubectl config get-contexts

# Verificar cluster atual
kubectl config current-context

# Trocar para outro cluster
kubectl config use-context <cluster-name>
```

**Na Interface Web**:
- Verifique o **card de Cluster** no Dashboard
- Troque entre clusters disponíveis

---

### 4️⃣ Cache do Navegador

**Problema**: Dados antigos em cache ou assets JS desatualizados.

**Solução**:
```bash
# 1. Rebuild frontend
./rebuild-web.sh -b

# 2. Reiniciar servidor web
./build/new-k8s-hpa web

# 3. Hard refresh no navegador
# Linux/Windows: Ctrl+Shift+R
# macOS: Cmd+Shift+R
```

---

### 5️⃣ Verificar Conectividade Kubernetes

**Problema**: VPN desconectada ou cluster inacessível.

**Solução**:
```bash
# Testar conectividade
kubectl cluster-info --context <cluster-name>

# Validar acesso ao API server
kubectl get nodes

# Verificar se há erros de autenticação
kubectl auth can-i get pods --all-namespaces
```

**Na Interface Web**:
- Se aparecer banner "Clusters Kubernetes Inacessíveis", clique em "Tentar Novamente"
- Verifique se VPN está conectada

---

### 6️⃣ Filtro de Busca Ativo

**Problema**: Campo de busca pode estar filtrando recursos.

**Solução**:
- Limpe o **campo de busca** (ícone ❌ ao lado)
- Verifique se não há filtros de texto aplicados

---

### 7️⃣ Verificar Logs do Backend

**Problema**: Erros no backend podem impedir carregamento de dados.

**Solução**:
```bash
# Verificar logs do servidor web
tail -f /tmp/k8s-hpa-manager-web-*.log

# Procurar por erros relacionados a pods/deployments
grep -i "error\|failed" /tmp/k8s-hpa-manager-web-*.log | grep -i "pod\|deployment"

# Modo foreground (logs no terminal)
./build/new-k8s-hpa web -f
```

---

## 🔬 Diagnóstico Avançado

### Validar Backend Diretamente

```bash
# 1. Testar endpoint de pods
curl -s "http://localhost:8080/api/v1/pods?cluster=<cluster>&namespaces=<namespace>" | jq

# 2. Testar endpoint de deployments
curl -s "http://localhost:8080/api/v1/deployments?cluster=<cluster>&namespaces=<namespace>" | jq

# 3. Verificar se há pods com problemas
curl -s "http://localhost:8080/api/v1/pods?cluster=<cluster>&namespaces=<namespace>" | \
  jq '.data[] | select(.phase != "Running")'

# 4. Verificar deployments com réplicas não prontas
curl -s "http://localhost:8080/api/v1/deployments?cluster=<cluster>&namespaces=<namespace>" | \
  jq '.data[] | select(.readyReplicas != .replicas)'
```

### Verificar Status Real no Cluster

```bash
# Pods com problemas
kubectl get pods -A | grep -v "Running\|Completed"

# Deployments com réplicas não prontas
kubectl get deployments -A --field-selector metadata.namespace!=kube-system | \
  awk '$3 != $4 {print $0}'

# Eventos de erro recentes (5 minutos)
kubectl get events --all-namespaces --sort-by='.lastTimestamp' | \
  grep -i "error\|failed\|backoff" | tail -20
```

---

## 🐛 Bugs Conhecidos (já corrigidos)

### ✅ Filtros Indevidos (NÃO EXISTE NO CÓDIGO)
- ❌ **Mito**: Sistema filtra apenas pods `Running`
- ✅ **Realidade**: Todos os pods são exibidos, independente do status
- 📄 **Código**: `PodsPanel.tsx` linhas 540-565, `DeploymentsTab.tsx` linhas 366-378

### ✅ Backend Oculta Recursos (NÃO EXISTE)
- ❌ **Mito**: Backend filtra recursos com problemas
- ✅ **Realidade**: Backend retorna TODOS os recursos encontrados
- 📄 **Código**: `internal/kubernetes/client.go` linhas 905-951

### ✅ UI Não Exibe Erros (FALSO)
- ❌ **Mito**: Interface não destaca problemas
- ✅ **Realidade**: Sistema completo de badges, cores e alertas visuais
- 📄 **Código**: Deployments com borda vermelha, pods com badges de status coloridos

---

## 📋 Checklist de Verificação

Antes de reportar bug, confirme:

- [ ] Selecionei o **namespace correto** onde o problema existe
- [ ] Ativei toggle **"Sistema"** se recursos estão em `kube-*` namespaces
- [ ] Verifiquei o **cluster correto** está selecionado
- [ ] **Limpei cache** do navegador (Ctrl+Shift+R)
- [ ] **VPN está conectada** e cluster acessível
- [ ] Campo de **busca está vazio** (sem filtros ativos)
- [ ] Testei endpoint do backend diretamente (`curl`)
- [ ] Validei que recursos com problema **realmente existem** no cluster (`kubectl`)

---

## 💡 Exemplo Prático

### Cenário: "Não vejo pod em CrashLoopBackOff"

**1. Validar que pod existe no cluster:**
```bash
kubectl get pods -n <namespace> | grep -i "crash\|backoff"
# Saída esperada:
# meu-pod-abc123   0/1   CrashLoopBackOff   5   10m
```

**2. Verificar se namespace está visível na interface:**
- Abrir aba "Pods"
- Select de namespace → escolher `<namespace>`
- Se não aparecer, ativar toggle "Sistema" (se for namespace kube-*)

**3. Verificar se pod aparece na lista:**
- Buscar por nome: digitar "meu-pod" no campo de busca
- Se aparecer, deve ter badge **vermelho** com "CrashLoopBackOff"

**4. Se não aparecer, testar backend:**
```bash
curl -s "http://localhost:8080/api/v1/pods?cluster=<cluster>&namespaces=<namespace>" | \
  jq '.data[] | select(.name | contains("meu-pod"))'
```

**5. Verificar resposta do backend:**
```json
{
  "name": "meu-pod-abc123",
  "namespace": "default",
  "phase": "Running",              // ← Pode estar Running mesmo em CrashLoopBackOff
  "statusReason": "CrashLoopBackOff",  // ← Razão do problema aqui
  "containers": [
    {
      "name": "app",
      "state": "waiting",
      "stateReason": "CrashLoopBackOff",
      "restartCount": 5
    }
  ]
}
```

**Nota**: Pod pode ter `phase: "Running"` mas `statusReason: "CrashLoopBackOff"` ao mesmo tempo.

---

## 🚀 Recursos para Diagnóstico em Tempo Real

### 1. Health Checking (Aba Health)
- Executa scan completo do cluster
- Detecta automaticamente todos os problemas
- Mostra lista de deployments, pods, services com erro

### 2. AI Diagnostics (Botão "Analisar com AI")
- Analisa causa raiz de pods em CrashLoopBackOff
- Disponível no painel de detalhes de Pods
- Coleta logs, events, manifests automaticamente

### 3. History Tracker (Aba History)
- Rastreia todas as operações de apply/delete
- Logs persistentes de cada ação
- Filtro por user, cluster, namespace, data

---

## 📞 Suporte

Se após seguir este guia o problema persistir, colete as seguintes informações:

```bash
# 1. Versão da aplicação
./build/new-k8s-hpa version

# 2. Pods com problema no cluster
kubectl get pods --all-namespaces --field-selector status.phase!=Running > pods-problematicos.txt

# 3. Logs do backend
tail -100 /tmp/k8s-hpa-manager-web-*.log > backend-logs.txt

# 4. Response do endpoint
curl -s "http://localhost:8080/api/v1/pods?cluster=<cluster>&namespaces=<namespace>" > api-response.json

# 5. Screenshot da interface web (incluir console do navegador - F12)
```

Abra issue no GitHub: https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/issues
