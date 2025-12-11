# 🚀 Solução: Service Mesh Totalmente Integrado (Sem Páginas Externas)

**Objetivo**: Usar o Service Mesh (Kiali) **dentro da aplicação**, sem precisar abrir páginas externas ou gerenciar tokens manualmente.

---

## 🎯 A Melhor Solução: Desabilitar Autenticação do Kiali

### **Por que esta é a melhor abordagem?**

✅ **Kiali roda DENTRO do cluster** (não é exposto externamente)
✅ **Sua aplicação já tem RBAC com Azure AD** (camada de segurança externa)
✅ **Ambiente interno da empresa** (acesso via VPN)
✅ **Kubernetes já autentica** a aplicação (via kubeconfig)
✅ **Simplifica implementação** (zero configuração adicional)
✅ **Padrão em ambientes dev/staging** da maioria das empresas

### **Segurança**

O modelo de segurança fica assim:

```
Usuário
  ↓ (Login Azure AD - RBAC)
Aplicação K8s-HPA-Manager
  ↓ (Kubeconfig com permissões)
Kubernetes API Server
  ↓ (Proxy interno - cluster network)
Kiali (anonymous)
```

**Camadas de segurança:**
1. ✅ Azure AD RBAC (grupo VV_CLOUD_SRE)
2. ✅ Kubeconfig com credenciais válidas
3. ✅ Network policies do cluster (se configuradas)
4. ✅ VPN corporativa

**Kiali anonymous é seguro porque:**
- Não é exposto publicamente (ClusterIP)
- Só acessível via aplicação autenticada
- Aplicação já controla quem pode ver

---

## ⚡ Implementação Rápida (5 minutos)

### **Passo 1: Desabilitar autenticação do Kiali**

```bash
# Editar ConfigMap do Kiali
kubectl edit cm -n istio-system kiali

# Procurar pela seção auth: e alterar:
# ANTES:
#   auth:
#     strategy: token
#
# DEPOIS:
#   auth:
#     strategy: anonymous

# Salvar e sair (:wq no vim)
```

**Ou aplicar via comando direto:**

```bash
kubectl patch cm kiali -n istio-system --type=merge -p '{"data":{"config.yaml":"$(kubectl get cm kiali -n istio-system -o jsonpath='{.data.config\.yaml}' | sed 's/strategy: token/strategy: anonymous/g')"}}'
```

---

### **Passo 2: Reiniciar Kiali**

```bash
# Reiniciar deployment
kubectl rollout restart deployment/kiali -n istio-system

# Aguardar reinicialização (demora ~15 segundos)
kubectl rollout status deployment/kiali -n istio-system

# Verificar pod rodando
kubectl get pods -n istio-system -l app=kiali
```

**Saída esperada:**
```
deployment "kiali" successfully rolled out
```

---

### **Passo 3: Testar integração**

```bash
# Reiniciar aplicação
./build/new-k8s-hpa web -f
```

**No frontend:**
1. Ir na aba **Service Mesh**
2. Selecionar **cluster** e **namespace**
3. Clicar em **Atualizar**

**Deve carregar o grafo automaticamente!** ✅

---

## 🧪 Validação Rápida

### **Teste 1: API do Kiali (via proxy)**

```bash
# Iniciar kubectl proxy
kubectl proxy --port=8001 &

# Testar health (deve retornar JSON sem pedir token)
curl http://localhost:8001/api/v1/namespaces/istio-system/services/http:kiali:20001/proxy/api/status

# Matar proxy
pkill -f "port-forward.*8001"
```

**Saída esperada (sem erro 401):**
```json
{
  "status": {
    "Kiali version": "v1.79.0",
    ...
  }
}
```

---

### **Teste 2: Endpoint da aplicação**

```bash
# Testar endpoint de namespaces
curl "http://localhost:8080/api/v1/servicemesh/namespaces?cluster=akspriv-prod" | jq

# Saída esperada:
# {
#   "cluster": "akspriv-prod",
#   "namespaces": ["production", "staging"],
#   "count": 2
# }

# Testar service graph
curl "http://localhost:8080/api/v1/servicemesh/graph?cluster=akspriv-prod&namespace=production&duration=5m&graphType=workload" | jq '.elements | {nodes: (.nodes | length), edges: (.edges | length)}'

# Saída esperada:
# {
#   "nodes": 5,
#   "edges": 8
# }
```

**Se funcionar → Problema resolvido! ✅**

---

## 🔍 Troubleshooting

### **Erro: "Ainda pede token"**

**Verificar se mudança foi aplicada:**

```bash
# Ver configuração atual
kubectl get cm -n istio-system kiali -o yaml | grep -A 5 "auth:"

# Deve mostrar:
# auth:
#   strategy: anonymous
```

**Se ainda mostrar `strategy: token`:**
- ConfigMap não foi salvo corretamente
- Repetir Passo 1

**Se mostrar `strategy: anonymous` mas ainda pede token:**
- Pod do Kiali não foi reiniciado
- Forçar delete do pod:

```bash
kubectl delete pod -n istio-system -l app=kiali
# Kubernetes vai criar um novo automaticamente
```

---

### **Erro: "Kiali não encontrado"**

```bash
# Verificar se Kiali está rodando
kubectl get pods -n istio-system -l app=kiali

# Se não estiver rodando, reinstalar:
kubectl apply -f https://raw.githubusercontent.com/istio/istio/release-1.20/samples/addons/kiali.yaml
```

---

### **Erro: "Grafo vazio"**

**Isso é normal se:**
- Namespace não tem pods rodando
- Pods não têm Istio sidecars (Envoy)
- Não há tráfego recente (últimos 60s)

**Habilitar Istio em namespace:**

```bash
# Habilitar injeção de sidecars
kubectl label namespace production istio-injection=enabled

# Reiniciar deployments para injetar sidecars
kubectl rollout restart deployment -n production

# Verificar sidecars injetados
kubectl get pods -n production -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.spec.containers[*].name}{"\n"}{end}' | grep istio-proxy
```

**Gerar tráfego de teste:**

```bash
# Port-forward para app
kubectl port-forward -n production svc/frontend 8080:80 &

# Fazer requests
for i in {1..100}; do curl http://localhost:8080 > /dev/null 2>&1; sleep 0.1; done

# Matar port-forward
pkill -f "port-forward.*8080"

# Aguardar 10 segundos e atualizar grafo na aplicação
```

---

## 📝 Atualizar Documentação

Após validar que funciona, adicionar ao `CLAUDE.md`:

```markdown
### Service Mesh (Istio/Kiali) - v1.3.6+

✅ **Integração com Kiali via Kubernetes Proxy**
- Aba dedicada: **Service Mesh** → Visualização de topologia e métricas
- Auto-discovery de Kiali em: `istio-system`, `kiali`, `kiali-operator`
- Autenticação: **Anonymous** (Kiali interno, aplicação já tem RBAC)
- Componente: `ServiceMeshGraph.tsx` com Cytoscape.js

**Requisitos:**
- Istio instalado no cluster
- Kiali instalado: `kubectl apply -f https://raw.githubusercontent.com/istio/istio/release-1.20/samples/addons/kiali.yaml`
- Kiali configurado com `auth.strategy: anonymous`
- Namespace com Istio habilitado: `kubectl label namespace <NS> istio-injection=enabled`

**Uso:**
1. Selecionar cluster e namespace na aba Service Mesh
2. Clicar em "Atualizar"
3. Visualizar grafo interativo com métricas de tráfego
```

---

## 🎯 Resumo da Solução

### **Antes (problemático):**
```
Usuário → Aplicação → Kiali (pede token) ❌
                          ↓
                    Abrir Kiali externo
                    Fazer login
                    Usar fora da aplicação
```

### **Depois (integrado):**
```
Usuário → Aplicação (RBAC Azure AD) → Kiali (anonymous) ✅
                                          ↓
                                   Grafo na própria aba
                                   Zero configuração
                                   Totalmente integrado
```

---

## ✅ Checklist de Validação

- [ ] Executar `kubectl edit cm -n istio-system kiali`
- [ ] Alterar `auth.strategy` para `anonymous`
- [ ] Reiniciar Kiali: `kubectl rollout restart deployment/kiali -n istio-system`
- [ ] Verificar pod rodando: `kubectl get pods -n istio-system -l app=kiali`
- [ ] Testar API via curl (sem pedir token)
- [ ] Reiniciar aplicação: `./build/new-k8s-hpa web -f`
- [ ] Testar aba Service Mesh no frontend
- [ ] Verificar logs: sem erros de autenticação
- [ ] Atualizar CLAUDE.md com documentação

---

**Resultado esperado:** Service Mesh funcionando **100% integrado na aplicação**, sem páginas externas, sem gerenciamento manual de tokens! 🚀
