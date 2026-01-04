# 🔧 Como Resolver: "Erro ao consultar Kiali API"

**Erro reportado:**
```
Erro ao carregar service mesh: Erro ao consultar Kiali API
```

---

## 🚀 Solução Rápida

### **Passo 1: Executar script de diagnóstico**

```bash
cd /home/paulo/Scripts/Scripts\ GO/New-K8s-HPA-Manager/Scale_HPA

./scripts/diagnose-servicemesh.sh
```

**O script vai:**
- ✅ Verificar se Istio está instalado
- ✅ Procurar serviço Kiali nos namespaces corretos
- ✅ Testar proxy do Kubernetes
- ✅ Testar API do Kiali
- ✅ Identificar a causa raiz do problema

---

### **Passo 2: Interpretar resultado**

#### **Caso A: ❌ "Kiali não está instalado"**

**Instalar Kiali via Istio addons:**

```bash
# Download do Kiali manifest
kubectl apply -f https://raw.githubusercontent.com/istio/istio/release-1.20/samples/addons/kiali.yaml

# Aguardar instalação
kubectl rollout status deployment/kiali -n istio-system

# Verificar pods
kubectl get pods -n istio-system -l app=kiali
```

**Saída esperada:**
```
NAME                     READY   STATUS    RESTARTS   AGE
kiali-xxxx-yyyy          1/1     Running   0          30s
```

**Testar acesso:**
```bash
kubectl port-forward -n istio-system svc/kiali 20001:20001
# Abrir navegador: http://localhost:20001
```

---

#### **Caso B: ⚠️ "Kiali está em namespace diferente"**

**Se Kiali foi encontrado em namespace diferente de `istio-system`:**

**Editar `internal/web/handlers/servicemesh.go:265`:**

```go
// ANTES
namespaces := []string{"istio-system", "kiali", "kiali-operator"}

// DEPOIS (adicionar namespace onde Kiali está)
namespaces := []string{"istio-system", "kiali", "kiali-operator", "observability"}
//                                                                  ^^^^^^^^^^^^^^^
//                                                                  Adicionar aqui
```

**Recompilar:**
```bash
make build
```

---

#### **Caso C: ⚠️ "Proxy funciona, mas grafo vazio"**

**Isso é NORMAL se:**
- Não há aplicações rodando no namespace
- Aplicações não têm Envoy sidecars (Istio não habilitado)
- Não há tráfego recente (últimos 60 segundos)

**Habilitar Istio em namespace:**
```bash
# Habilitar injeção automática de sidecars
kubectl label namespace production istio-injection=enabled

# Reiniciar deployments para injetar sidecars
kubectl rollout restart deployment -n production

# Verificar sidecars
kubectl get pods -n production -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.spec.containers[*].name}{"\n"}{end}'
# Deve mostrar: app-name    app istio-proxy
#                              ^^^^^^^^^^^^^ Sidecar Envoy
```

---

#### **Caso D: ❌ "Kiali requer autenticação"**

**Se Kiali retornar 401/403:**

**1. Criar ServiceAccount com permissões:**
```bash
kubectl create sa kiali-viewer -n istio-system

kubectl create clusterrolebinding kiali-viewer \
  --clusterrole=view \
  --serviceaccount=istio-system:kiali-viewer
```

**2. Obter token:**
```bash
TOKEN=$(kubectl create token kiali-viewer -n istio-system --duration=87600h)
echo $TOKEN
```

**3. Atualizar backend para usar token:**

**Editar `internal/web/handlers/servicemesh.go:308`:**

```go
// ANTES
result := clientset.CoreV1().RESTClient().Get().AbsPath(proxyPath).Do(context.Background())

// DEPOIS
result := clientset.CoreV1().RESTClient().
    Get().
    AbsPath(proxyPath).
    SetHeader("Authorization", "Bearer <TOKEN>").  // ← Adicionar
    Do(context.Background())
```

**Recompilar:**
```bash
make build
```

---

## 🧪 Testar após instalação

### **1. Verificar Kiali está rodando:**

```bash
kubectl get pods -n istio-system -l app=kiali
```

**Saída esperada:**
```
NAME                     READY   STATUS    RESTARTS   AGE
kiali-xxxx-yyyy          1/1     Running   0          2m
```

---

### **2. Testar API do Kiali manualmente:**

```bash
# Iniciar port-forward
kubectl port-forward -n istio-system svc/kiali 20001:20001 &

# Testar health
curl http://localhost:20001/api/status | jq

# Testar service graph (substitua 'production' pelo namespace correto)
curl "http://localhost:20001/api/namespaces/production/graph?duration=60s&graphType=workload" | jq

# Matar port-forward
pkill -f "port-forward.*kiali"
```

**Saída esperada (status):**
```json
{
  "status": {
    "Kiali commit hash": "abc123...",
    "Kiali version": "v1.79.0"
  }
}
```

**Saída esperada (graph):**
```json
{
  "timestamp": 1733759841,
  "duration": 60,
  "graphType": "workload",
  "elements": {
    "nodes": [...],
    "edges": [...]
  }
}
```

---

### **3. Testar na aplicação:**

```bash
# Iniciar aplicação
./build/new-k8s-hpa web -f

# Em outro terminal, testar endpoint
curl "http://localhost:8080/api/v1/servicemesh/namespaces?cluster=akspriv-prod" | jq

# Saída esperada:
# {
#   "cluster": "akspriv-prod",
#   "namespaces": ["production", "staging"],
#   "count": 2
# }
```

**Testar service graph:**
```bash
curl "http://localhost:8080/api/v1/servicemesh/graph?cluster=akspriv-prod&namespace=production&duration=5m&graphType=workload" | jq
```

**Se funcionar → Problema resolvido! ✅**

---

## 📝 Logs para Debug

Se ainda falhar, verificar logs da aplicação:

```bash
# Foreground mode (logs no terminal)
./build/new-k8s-hpa web -f 2>&1 | grep -i "servicemesh\|kiali"

# Background mode (logs em arquivo)
tail -f /tmp/k8s-hpa-manager-web-*.log | grep -i "servicemesh\|kiali"
```

**Buscar por:**
- `[ServiceMesh] Procurando serviço Kiali nos namespaces:` - Lista de namespaces buscados
- `[ServiceMesh] Kiali encontrado! Service:` - Kiali descoberto com sucesso
- `[ServiceMesh] Serviço Kiali NÃO encontrado` - Kiali não encontrado
- `[ServiceMesh] Erro no proxy do Kubernetes:` - Detalhes do erro de proxy
- `[ServiceMesh] Grafo carregado: X nós, Y arestas` - Sucesso!

---

## 🎯 Checklist Final

- [ ] Executar `./scripts/diagnose-servicemesh.sh`
- [ ] Instalar Kiali (se não instalado)
- [ ] Verificar pods do Kiali estão `Running`
- [ ] Habilitar Istio em pelo menos 1 namespace
- [ ] Testar API do Kiali via port-forward
- [ ] Testar endpoint da aplicação
- [ ] Verificar logs da aplicação

---

## 📚 Documentação

- **Kiali Installation**: https://kiali.io/docs/installation/
- **Istio Installation**: https://istio.io/latest/docs/setup/getting-started/
- **Kiali API Docs**: https://kiali.io/docs/api/

---

**Se o problema persistir após seguir este guia, compartilhe a saída do script de diagnóstico para análise detalhada.** 🔍
