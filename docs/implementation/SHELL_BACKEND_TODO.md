# Backend Implementation - Pod Shell WebSocket

## Endpoint necessário

```
WebSocket: GET /api/v1/clusters/{cluster}/namespaces/{namespace}/pods/{pod}/shell
Query params:
  - container: string (nome do container)
  - shell: string (/bin/bash, /bin/sh, /bin/zsh)
```

## Protocolo WebSocket

### Mensagens do Cliente → Servidor

```json
// Redimensionar terminal
{
  "type": "resize",
  "cols": 80,
  "rows": 24
}

// Input do usuário
{
  "type": "input",
  "data": "ls -la\n"
}
```

### Mensagens do Servidor → Cliente

```json
// Output do shell
{
  "type": "output",
  "data": "total 48\ndrwxr-xr-x  6 root root 4096 Dec 19 10:30 .\n..."
}

// Erro
{
  "type": "error",
  "data": "Container not running"
}
```

## Implementação Go com client-go

### Dependências
```go
import (
    "k8s.io/client-go/kubernetes"
    "k8s.io/client-go/tools/remotecommand"
    "k8s.io/client-go/kubernetes/scheme"
    corev1 "k8s.io/api/core/v1"
)
```

### Exemplo de Implementação

```go
func HandlePodShell(c *gin.Context) {
    cluster := c.Param("cluster")
    namespace := c.Param("namespace")
    podName := c.Param("pod")
    container := c.Query("container")
    shell := c.Query("shell")
    
    // Upgrade para WebSocket
    ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
    if err != nil {
        log.Printf("Failed to upgrade: %v", err)
        return
    }
    defer ws.Close()
    
    // Get K8s clientset
    clientset, restConfig, err := getK8sClient(cluster)
    if err != nil {
        sendError(ws, err.Error())
        return
    }
    
    // Create exec request
    req := clientset.CoreV1().RESTClient().
        Post().
        Resource("pods").
        Name(podName).
        Namespace(namespace).
        SubResource("exec").
        VersionedParams(&corev1.PodExecOptions{
            Container: container,
            Command:   []string{shell},
            Stdin:     true,
            Stdout:    true,
            Stderr:    true,
            TTY:       true,
        }, scheme.ParameterCodec)
    
    // Create SPDY executor
    executor, err := remotecommand.NewSPDYExecutor(restConfig, "POST", req.URL())
    if err != nil {
        sendError(ws, err.Error())
        return
    }
    
    // Create streams
    terminalSession := &TerminalSession{
        ws: ws,
        sizeChan: make(chan remotecommand.TerminalSize),
    }
    
    // Execute
    err = executor.Stream(remotecommand.StreamOptions{
        Stdin:             terminalSession,
        Stdout:            terminalSession,
        Stderr:            terminalSession,
        Tty:               true,
        TerminalSizeQueue: terminalSession,
    })
    
    if err != nil {
        sendError(ws, err.Error())
    }
}

// TerminalSession implementa os streams necessários
type TerminalSession struct {
    ws       *websocket.Conn
    sizeChan chan remotecommand.TerminalSize
}

// Read from WebSocket (stdin)
func (t *TerminalSession) Read(p []byte) (int, error) {
    var msg struct {
        Type string `json:"type"`
        Data string `json:"data"`
    }
    
    err := t.ws.ReadJSON(&msg)
    if err != nil {
        return 0, err
    }
    
    if msg.Type == "input" {
        copy(p, []byte(msg.Data))
        return len(msg.Data), nil
    }
    
    if msg.Type == "resize" {
        // Enviar novo tamanho para sizeChan
        // (implementar parsing de cols/rows)
    }
    
    return 0, nil
}

// Write to WebSocket (stdout/stderr)
func (t *TerminalSession) Write(p []byte) (int, error) {
    msg := map[string]interface{}{
        "type": "output",
        "data": string(p),
    }
    
    err := t.ws.WriteJSON(msg)
    if err != nil {
        return 0, err
    }
    
    return len(p), nil
}

// Next retorna o próximo tamanho do terminal
func (t *TerminalSession) Next() *remotecommand.TerminalSize {
    size := <-t.sizeChan
    return &size
}
```

## Rota no Router

```go
// Em main.go ou router.go
apiV1.GET("/clusters/:cluster/namespaces/:namespace/pods/:pod/shell", HandlePodShell)
```

## Segurança e Validações

1. **RBAC**: Verificar permissões antes de permitir exec
2. **Validação**: Container existe e está running
3. **Timeout**: Implementar timeout de inatividade
4. **Audit**: Logar quem executou shell e em qual pod
5. **Rate Limiting**: Limitar número de sessões simultâneas

## Testes

```bash
# Testar se shell funciona
kubectl exec -it <pod> -n <namespace> -c <container> -- /bin/bash

# Verificar se container tem o shell
kubectl exec <pod> -n <namespace> -c <container> -- which /bin/bash
kubectl exec <pod> -n <namespace> -c <container> -- which /bin/sh
kubectl exec <pod> -n <namespace> -c <container> -- which /bin/zsh
```

## Próximos Passos

1. Implementar endpoint WebSocket no backend Go
2. Testar conectividade com diferentes tipos de shell
3. Adicionar tratamento de erros robusto
4. Implementar RBAC no endpoint
5. Adicionar logs de auditoria
6. Testar com pods que não têm bash (só sh)
7. Testar redimensionamento de terminal
8. Implementar timeout de sessão

## Referências

- [Kubernetes Exec API](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#podexecoptions-v1-core)
- [client-go remotecommand](https://github.com/kubernetes/client-go/tree/master/tools/remotecommand)
- [Example: Web Terminal](https://github.com/kubernetes/dashboard/blob/master/src/app/backend/handler/terminal.go)
