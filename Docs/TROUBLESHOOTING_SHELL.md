# Troubleshooting - Shell e Debug Connection

## Problema: "Erro de conexão" em todas as opções

### Checklist de Verificação

#### 1. Servidor Web Rodando?
```bash
ps aux | grep k8s-hpa-manager | grep web
```

**Se não estiver rodando:**
```bash
cd /home/paulo/Scripts/Scripts\ GO/New-K8s-HPA-Manager/Scale_HPA
./build/k8s-hpa-manager web
```

#### 2. Frontend Acessível?
Abra o navegador em: `http://localhost:8080`

#### 3. Console do Navegador
Pressione F12 e veja erros no Console:
- Erros de WebSocket (ws://)
- Erros de CORS
- Erros de rede (ERR_CONNECTION_REFUSED)

#### 4. Logs do Servidor
```bash
# Se usando o script web-server.sh
tail -f Logs/web-server.log

# Ou logs do terminal onde iniciou o servidor
```

### Possíveis Causas

#### A) URL Incorreta no Frontend
**Sintoma:** `ERR_CONNECTION_REFUSED` ou `404 Not Found`

**Solução:** Frontend agora usa:
```
ws://localhost:8080/api/v1/pods/{cluster}/{namespace}/{pod}/shell
```

**Verificar se build está atualizado:**
```bash
cd internal/web/frontend
npm run build
```

#### B) WebSocket Upgrade Falhou
**Sintoma:** `101 Switching Protocols` não aparece

**Causa:** Gin precisa fazer upgrade correto

**Verificar:** Logs do servidor devem mostrar:
```
[SHELL] Connection request - cluster=..., namespace=..., pod=...
[SHELL] Attempting WebSocket upgrade...
[SHELL] WebSocket upgrade successful
```

#### C) Cliente Kubernetes Inválido
**Sintoma:** Upgrade funciona, mas depois erro "Failed to get client"

**Verificar:**
```bash
# Kubeconfig existe?
ls -la ~/.kube/config

# Contexto correto?
kubectl config current-context

# Cluster acessível?
kubectl get pods -n default
```

#### D) Container ou Pod Não Existe
**Sintoma:** "Pod not found" ou "Container not found"

**Verificar:**
```bash
kubectl get pods -n <namespace>
kubectl describe pod <pod-name> -n <namespace>
```

#### E) Permissões RBAC
**Sintoma:** "Forbidden" ou "Unauthorized"

**Verificar:**
```bash
# Service account tem permissão para pods/exec?
kubectl auth can-i create pods/exec -n <namespace>
```

### Debug Detalhado

#### 1. Testar WebSocket Manualmente
```bash
# Instalar websocat se não tiver
# cargo install websocat

# Testar conexão
websocat ws://localhost:8080/api/v1/pods/akspriv-xxx/default/test-pod/shell?container=app&shell=/bin/bash
```

#### 2. Verificar Rotas Registradas
No código `internal/web/server.go`:
```go
pods.GET("/:cluster/:namespace/:name/shell", podExecHandler.HandleShell)
pods.GET("/:cluster/:namespace/:name/debug", podExecHandler.HandleDebug)
```

Rota completa: `/api/v1/pods/:cluster/:namespace/:name/shell`

#### 3. Verificar Handler
No código `internal/web/handlers/podexec.go`:
```go
func (h *PodExecHandler) HandleShell(c *gin.Context) {
    cluster := c.Param("cluster")      // Deve capturar o cluster
    namespace := c.Param("namespace")  // Deve capturar o namespace
    podName := c.Param("name")         // Deve capturar o pod
    containerName := c.Query("container") // Deve capturar ?container=
    ...
}
```

### Testes Passo a Passo

#### Teste 1: Servidor está respondendo?
```bash
curl http://localhost:8080/api/v1/health
```
Esperado: `{"status":"ok"}`

#### Teste 2: Rota de pods funciona?
```bash
curl "http://localhost:8080/api/v1/pods?cluster=akspriv-xxx"
```
Esperado: Lista de pods ou erro de auth

#### Teste 3: WebSocket aceita conexão?
Abrir Console do Navegador (F12) → Network → WS
- Filtrar por "shell" ou "debug"
- Ver status: deve ser `101 Switching Protocols`
- Ver frames: deve ter mensagens de input/output

### Correções Aplicadas

✅ **URL do Frontend corrigida**
   - Antes: `ws://host/api/v1/clusters/{cluster}/namespaces/{namespace}/pods/{pod}/shell`
   - Agora: `ws://host/api/v1/pods/{cluster}/{namespace}/{pod}/shell`

✅ **Logs de debug adicionados**
   ```go
   log.Printf("[SHELL] Connection request - cluster=%s, namespace=%s, pod=%s...", ...)
   log.Printf("[SHELL] Attempting WebSocket upgrade...")
   log.Printf("[SHELL] WebSocket upgrade successful")
   ```

✅ **Rotas registradas corretamente**
   ```go
   pods.GET("/:cluster/:namespace/:name/shell", podExecHandler.HandleShell)
   pods.GET("/:cluster/:namespace/:name/debug", podExecHandler.HandleDebug)
   ```

### Próximos Passos

1. **Recompilar:** ✅ Feito
2. **Reiniciar servidor:** ⚠️ Necessário
3. **Limpar cache do navegador:** `Ctrl+Shift+R`
4. **Testar conexão:** Abrir pod → shell

### Se Ainda Não Funcionar

Envie os logs completos:
```bash
# Logs do servidor
tail -50 Logs/web-server.log

# Logs do navegador (F12 → Console)
# Screenshot dos erros
```
