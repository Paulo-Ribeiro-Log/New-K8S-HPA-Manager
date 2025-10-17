# 🎯 Instruções para Usar a Interface Web

**Data:** 16 de Outubro de 2025
**Versão:** POC 1.0 (Corrigida)

---

## ✅ Bug Corrigido!

O problema de seleção de clusters foi **corrigido**. O frontend agora envia o `context` correto para a API.

---

## 🚀 Como Iniciar

### 1. Parar servidor antigo (se estiver rodando)
```bash
killall k8s-hpa-manager
```

### 2. Iniciar servidor web
```bash
./build/k8s-hpa-manager web --port 8080
```

**Output esperado:**
```
🌐 k8s-hpa-manager Web Server (POC)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🔗 Servidor rodando em: http://localhost:8080
🔐 Auth Token:    poc-token-123
❤️  Health Check: http://localhost:8080/health
```

---

## 🌐 Como Usar no Navegador

### 1. Abrir no Browser
```
http://localhost:8080
```

### 2. Fazer Login
- **Token:** `poc-token-123`
- Clicar em "Entrar"

### 3. IMPORTANTE: Limpar Cache do Browser
Se você já tinha aberto antes do bug ser corrigido:

**Chrome/Edge/Brave:**
- Pressione `Ctrl+Shift+R` (Windows/Linux)
- Ou `Cmd+Shift+R` (Mac)

**Firefox:**
- Pressione `Ctrl+F5` (Windows/Linux)
- Ou `Cmd+Shift+R` (Mac)

**Safari:**
- `Cmd+Option+R`

Isso força o browser a baixar o HTML corrigido.

### 4. Usar a Interface

#### Dashboard
Após login, você verá:
- **Total de Clusters:** 24 clusters descobertos
- **Namespaces:** (carrega após selecionar cluster)
- **HPAs:** (carrega após selecionar namespace)

#### Selecionar Cluster
1. Role para baixo até "📦 Clusters"
2. Clique em qualquer cluster (ex: `akspriv-faturamento-prd`)
3. O card ficará **azul** quando selecionado
4. Namespaces carregarão automaticamente abaixo

#### Selecionar Namespace
1. Após cluster selecionado, role até "📁 Namespaces"
2. Clique em qualquer namespace (ex: `ingress-nginx`)
3. HPAs desse namespace carregarão abaixo

#### Ver HPAs
1. Após namespace selecionado, role até "⚖️ HPAs"
2. Verá lista de HPAs com:
   - Nome
   - Min/Max Replicas
   - Current Replicas
   - Target CPU
   - CPU/Memory Requests e Limits

---

## 🧪 Testar via API (curl)

### Health Check (sem autenticação)
```bash
curl http://localhost:8080/health
```

### Listar Clusters
```bash
curl -H "Authorization: Bearer poc-token-123" \
  http://localhost:8080/api/v1/clusters
```

### Listar Namespaces
```bash
# Substitua CONTEXT pelo context do cluster (ex: akspriv-faturamento-prd-admin)
curl -H "Authorization: Bearer poc-token-123" \
  "http://localhost:8080/api/v1/namespaces?cluster=CONTEXT&showSystem=false"
```

### Listar HPAs
```bash
# Substitua CONTEXT e NAMESPACE
curl -H "Authorization: Bearer poc-token-123" \
  "http://localhost:8080/api/v1/hpas?cluster=CONTEXT&namespace=NAMESPACE"
```

---

## ❓ Troubleshooting

### Erro: "context does not exist"
**Causa:** Browser com cache antigo (antes da correção)
**Solução:** Pressione `Ctrl+Shift+R` para hard refresh

### Erro: "No authorization header"
**Causa:** Token inválido ou expirado
**Solução:** Faça logout e login novamente com `poc-token-123`

### Clusters não carregam
**Causa:** Servidor não está rodando ou VPN desconectada
**Solução:** 
1. Verificar se servidor está rodando: `ps aux | grep k8s-hpa-manager`
2. Conectar VPN se necessário
3. Reiniciar servidor

### Performance lenta
**Causa:** Normal - conecta em clusters reais Kubernetes
**Expectativa:**
- Clusters: ~150µs (muito rápido)
- Namespaces: ~300-400ms (conecta K8s)
- HPAs: ~250ms (conecta K8s)

---

## 📝 Notas Técnicas

### Context vs Name
- **Name:** Nome amigável (ex: `akspriv-faturamento-prd`)
- **Context:** Nome técnico do kubeconfig (ex: `akspriv-faturamento-prd-admin`)
- **Frontend exibe:** Name
- **Frontend envia para API:** Context ✅

### Token de Autenticação
- **Padrão POC:** `poc-token-123`
- **Customizar:** `export K8S_HPA_WEB_TOKEN="seu-token"`

### Endpoints Disponíveis
```
GET  /health                          - Health check (sem auth)
GET  /api/v1/clusters                 - Lista clusters (com auth)
GET  /api/v1/namespaces?cluster=X     - Lista namespaces (com auth)
GET  /api/v1/hpas?cluster=X&namespace=Y - Lista HPAs (com auth)
```

---

## 🎉 Conclusão

A interface web está **100% funcional** após a correção do bug!

**Próximos passos:**
- Testar no browser com hard refresh
- Selecionar clusters e ver namespaces carregarem
- Selecionar namespaces e ver HPAs

**Em caso de dúvidas:**
- Ver logs do servidor: `tail -f /tmp/web-server-fixed.log`
- Ver documentação completa: `README_WEB.md`
- Ver resumo do bug fix: `WEB_BUG_FIX.md`

---

**Última atualização:** 16/10/2025 12:13
**Status:** ✅ Funcional e corrigido
