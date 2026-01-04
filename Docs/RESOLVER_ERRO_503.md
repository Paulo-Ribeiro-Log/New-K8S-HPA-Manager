# 🔧 Resolver: Erro 503 - "Cannot load the graph: server_error: 503"

**Erro**: Kiali está acessível (sem pedir token), mas retorna erro 503 ao tentar carregar o grafo.

---

## 🎯 Causa Mais Comum: Prometheus não está instalado

**Erro 503** significa que **Kiali está rodando**, mas não consegue processar a requisição porque **depende do Prometheus para obter métricas do Istio**.

---

## ⚡ Solução Rápida (Instalar Prometheus)

### **Passo 1: Verificar se Prometheus está instalado**

```bash
kubectl get pods -n istio-system -l app=prometheus
```

**Se retornar vazio** → Prometheus não está instalado!

---

### **Passo 2: Instalar Prometheus**

```bash
# Instalar Prometheus via Istio addons
kubectl apply -f https://raw.githubusercontent.com/istio/istio/release-1.20/samples/addons/prometheus.yaml

# Aguardar instalação (demora ~30 segundos)
kubectl rollout status deployment/prometheus -n istio-system

# Verificar pod rodando
kubectl get pods -n istio-system -l app=prometheus
```

**Saída esperada:**
```
NAME                          READY   STATUS    RESTARTS   AGE
prometheus-xxxx-yyyy          1/1     Running   0          30s
```

---

### **Passo 3: Reiniciar Kiali**

```bash
# Reiniciar Kiali para reconectar ao Prometheus
kubectl rollout restart deployment/kiali -n istio-system

# Aguardar reinicialização
kubectl rollout status deployment/kiali -n istio-system
```

---

### **Passo 4: Aguardar 1 minuto e testar**

```bash
# Aguardar Prometheus coletar métricas
sleep 60

# Reiniciar aplicação
./build/new-k8s-hpa web -f

# Testar no frontend
# → Aba Service Mesh → Selecionar cluster/namespace → Atualizar
```

**Deve funcionar agora!** ✅

---

## 🔍 Diagnóstico Completo (Recomendado)

Se a solução acima não resolver, execute o script de diagnóstico:

```bash
./scripts/diagnose-kiali-503.sh
```

**O script vai:**
1. ✅ Verificar se Kiali está rodando
2. ✅ Verificar se Prometheus está instalado
3. ✅ Testar conectividade Kiali → Prometheus
4. ✅ Verificar configuração do Kiali
5. ✅ Testar API do Kiali diretamente
6. ✅ Identificar a causa raiz exata

---

## 🐛 Outras Causas Possíveis

### **Causa 2: Prometheus instalado mas não acessível**

**Sintoma**: Prometheus está rodando, mas Kiali não consegue se conectar.

**Solução:**

```bash
# Testar conectividade de dentro do pod do Kiali
KIALI_POD=$(kubectl get pods -n istio-system -l app=kiali -o jsonpath='{.items[0].metadata.name}')

kubectl exec -n istio-system $KIALI_POD -- curl -s http://prometheus:9090/api/v1/query?query=up

# Deve retornar JSON com dados
# Se retornar erro → problema de rede/DNS
```

**Se falhar:**
- Verificar service do Prometheus: `kubectl get svc -n istio-system prometheus`
- Verificar NetworkPolicies: `kubectl get netpol -n istio-system`
- Verificar logs do Kiali: `kubectl logs -n istio-system -l app=kiali | grep -i prometheus`

---

### **Causa 3: Namespace selecionado não existe ou não tem Istio**

**Sintoma**: Erro 503 apenas para namespaces específicos.

**Solução:**

```bash
# Verificar se namespace existe
kubectl get ns <NAMESPACE>

# Verificar se Istio está habilitado
kubectl get ns <NAMESPACE> --show-labels | grep istio-injection

# Habilitar Istio
kubectl label namespace <NAMESPACE> istio-injection=enabled

# Reiniciar deployments para injetar sidecars
kubectl rollout restart deployment -n <NAMESPACE>

# Verificar sidecars injetados
kubectl get pods -n <NAMESPACE> -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.spec.containers[*].name}{"\n"}{end}' | grep istio-proxy
```

---

### **Causa 4: Kiali ainda está inicializando**

**Sintoma**: Erro 503 logo após reiniciar o Kiali.

**Solução:**

```bash
# Aguardar pod ficar completamente pronto
kubectl wait --for=condition=ready pod -l app=kiali -n istio-system --timeout=120s

# Aguardar mais 30 segundos para Kiali inicializar completamente
sleep 30

# Testar novamente
```

---

## 🧪 Testar Manualmente

### **Teste 1: Health do Kiali**

```bash
kubectl port-forward -n istio-system svc/kiali 20001:20001 &

curl http://localhost:20001/api/status | jq

# Deve retornar: { "status": { "Kiali version": "..." } }

pkill -f "port-forward.*kiali"
```

---

### **Teste 2: API de Namespaces**

```bash
kubectl port-forward -n istio-system svc/kiali 20001:20001 &

curl http://localhost:20001/api/namespaces | jq

# Deve retornar lista de namespaces (não 503)

pkill -f "port-forward.*kiali"
```

---

### **Teste 3: Service Graph**

```bash
kubectl port-forward -n istio-system svc/kiali 20001:20001 &

# Substituir 'default' por namespace com Istio habilitado
curl "http://localhost:20001/api/namespaces/default/graph?duration=60s&graphType=workload" | jq

# Deve retornar: { "elements": { "nodes": [...], "edges": [...] } }
# Se retornar 503 → Problema com Prometheus

pkill -f "port-forward.*kiali"
```

---

## 📊 Checklist de Validação

- [ ] Prometheus está instalado: `kubectl get pods -n istio-system -l app=prometheus`
- [ ] Prometheus está Ready (1/1 Running)
- [ ] Kiali está rodando: `kubectl get pods -n istio-system -l app=kiali`
- [ ] Kiali está Ready (1/1 Running)
- [ ] Aguardou 60 segundos após instalar Prometheus
- [ ] Reiniciou Kiali: `kubectl rollout restart deployment/kiali -n istio-system`
- [ ] Testou API do Kiali via port-forward (sem erro 503)
- [ ] Namespace selecionado tem Istio habilitado
- [ ] Namespace tem pods rodando com sidecars Envoy

---

## 🔄 Instalar Stack Completo do Istio (Recomendado)

Se você quer garantir que **tudo** está instalado corretamente:

```bash
# Instalar todos os addons do Istio (Prometheus, Grafana, Jaeger, Kiali)
kubectl apply -f https://raw.githubusercontent.com/istio/istio/release-1.20/samples/addons/prometheus.yaml
kubectl apply -f https://raw.githubusercontent.com/istio/istio/release-1.20/samples/addons/grafana.yaml
kubectl apply -f https://raw.githubusercontent.com/istio/istio/release-1.20/samples/addons/jaeger.yaml
kubectl apply -f https://raw.githubusercontent.com/istio/istio/release-1.20/samples/addons/kiali.yaml

# Aguardar todos ficarem prontos
kubectl rollout status deployment/prometheus -n istio-system
kubectl rollout status deployment/grafana -n istio-system
kubectl rollout status deployment/jaeger -n istio-system
kubectl rollout status deployment/kiali -n istio-system

# Verificar todos rodando
kubectl get pods -n istio-system
```

---

## 🎯 Resumo

### **Erro 503 = Prometheus está faltando**

```bash
# 1. Instalar Prometheus
kubectl apply -f https://raw.githubusercontent.com/istio/istio/release-1.20/samples/addons/prometheus.yaml

# 2. Aguardar
kubectl rollout status deployment/prometheus -n istio-system

# 3. Reiniciar Kiali
kubectl rollout restart deployment/kiali -n istio-system

# 4. Aguardar 1 minuto
sleep 60

# 5. Testar aplicação
./build/new-k8s-hpa web -f
```

---

## 📞 Se Ainda Não Funcionar

Execute o diagnóstico completo e compartilhe a saída:

```bash
./scripts/diagnose-kiali-503.sh > diagnostico-503.txt

cat diagnostico-503.txt
```

---

**Na maioria dos casos, instalar o Prometheus resolve o problema imediatamente!** ✅
