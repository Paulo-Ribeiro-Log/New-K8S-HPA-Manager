# Backend - Ephemeral Debug Container Implementation

## Overview
Implementação do endpoint WebSocket para criar ephemeral debug containers com imagem nicolaka/netshoot.

## Endpoint

```
GET /api/v1/clusters/{cluster}/namespaces/{namespace}/pods/{pod}/debug
```

### Query Parameters
- `container` (required): Nome do container alvo
- `shell` (required): Shell a ser usado (/bin/bash, /bin/sh, /bin/zsh)
- `image` (optional): Imagem do debug container (default: nicolaka/netshoot)

## Kubernetes API - Ephemeral Containers

Ephemeral containers são criados usando a API de Ephemeral Containers do Kubernetes:

```go
POST /api/v1/namespaces/{namespace}/pods/{pod}/ephemeralcontainers
```

## Go Implementation

```go
package handlers

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
)

type EphemeralDebugHandler struct {
	clientset *kubernetes.Clientset
	upgrader  websocket.Upgrader
}

func NewEphemeralDebugHandler(clientset *kubernetes.Clientset) *EphemeralDebugHandler {
	return &EphemeralDebugHandler{
		clientset: clientset,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Configure properly in production
			},
		},
	}
}

func (h *EphemeralDebugHandler) HandleDebugSession(w http.ResponseWriter, r *http.Request) {
	// Parse parameters
	cluster := chi.URLParam(r, "cluster")
	namespace := chi.URLParam(r, "namespace")
	podName := chi.URLParam(r, "pod")
	container := r.URL.Query().Get("container")
	shell := r.URL.Query().Get("shell")
	image := r.URL.Query().Get("image")

	if image == "" {
		image = "nicolaka/netshoot"
	}

	if shell == "" {
		shell = "/bin/bash"
	}

	// Upgrade to WebSocket
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade connection: %v", err)
		return
	}
	defer conn.Close()

	ctx := r.Context()

	// Step 1: Create ephemeral container
	debugContainerName := fmt.Sprintf("debug-%d", time.Now().Unix())
	
	err = h.createEphemeralContainer(ctx, namespace, podName, debugContainerName, container, image)
	if err != nil {
		h.sendError(conn, fmt.Sprintf("Failed to create ephemeral container: %v", err))
		return
	}

	// Step 2: Wait for ephemeral container to be ready
	err = h.waitForEphemeralContainer(ctx, namespace, podName, debugContainerName, 30*time.Second)
	if err != nil {
		h.sendError(conn, fmt.Sprintf("Ephemeral container not ready: %v", err))
		return
	}

	// Step 3: Exec into ephemeral container
	h.execIntoEphemeralContainer(ctx, conn, namespace, podName, debugContainerName, shell)
}

func (h *EphemeralDebugHandler) createEphemeralContainer(
	ctx context.Context,
	namespace, podName, debugName, targetContainer, image string,
) error {
	// Get current pod
	pod, err := h.clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get pod: %w", err)
	}

	// Define ephemeral container
	ephemeralContainer := corev1.EphemeralContainer{
		EphemeralContainerCommon: corev1.EphemeralContainerCommon{
			Name:  debugName,
			Image: image,
			Stdin: true,
			TTY:   true,
			TargetContainerName: targetContainer, // Share process namespace
			Command: []string{"/bin/sh"}, // Initial command, will be replaced by exec
		},
	}

	// Patch pod with ephemeral container
	pod.Spec.EphemeralContainers = append(pod.Spec.EphemeralContainers, ephemeralContainer)

	// Apply patch
	patchData := []byte(fmt.Sprintf(`{
		"spec": {
			"ephemeralContainers": [
				{
					"name": "%s",
					"image": "%s",
					"stdin": true,
					"tty": true,
					"targetContainerName": "%s",
					"command": ["/bin/sh"]
				}
			]
		}
	}`, debugName, image, targetContainer))

	_, err = h.clientset.CoreV1().Pods(namespace).Patch(
		ctx,
		podName,
		types.StrategicMergePatchType,
		patchData,
		metav1.PatchOptions{},
		"ephemeralcontainers",
	)

	return err
}

func (h *EphemeralDebugHandler) waitForEphemeralContainer(
	ctx context.Context,
	namespace, podName, containerName string,
	timeout time.Duration,
) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		pod, err := h.clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		// Check ephemeral container status
		for _, status := range pod.Status.EphemeralContainerStatuses {
			if status.Name == containerName {
				if status.State.Running != nil {
					return nil // Container is running
				}
				if status.State.Terminated != nil {
					return fmt.Errorf("container terminated: %s", status.State.Terminated.Reason)
				}
			}
		}

		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("timeout waiting for ephemeral container to start")
}

func (h *EphemeralDebugHandler) execIntoEphemeralContainer(
	ctx context.Context,
	conn *websocket.Conn,
	namespace, podName, containerName, shell string,
) {
	// Create exec request
	req := h.clientset.CoreV1().RESTClient().
		Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: containerName,
			Command:   []string{shell},
			Stdin:     true,
			Stdout:    true,
			Stderr:    true,
			TTY:       true,
		}, scheme.ParameterCodec)

	// Create SPDY executor
	exec, err := remotecommand.NewSPDYExecutor(h.restConfig, "POST", req.URL())
	if err != nil {
		h.sendError(conn, fmt.Sprintf("Failed to create executor: %v", err))
		return
	}

	// Create terminal session wrapper
	session := &TerminalSession{
		conn:   conn,
		sizeCh: make(chan remotecommand.TerminalSize, 1),
	}

	// Start streaming
	err = exec.Stream(remotecommand.StreamOptions{
		Stdin:             session,
		Stdout:            session,
		Stderr:            session,
		Tty:               true,
		TerminalSizeQueue: session,
	})

	if err != nil {
		log.Printf("Stream error: %v", err)
	}
}

func (h *EphemeralDebugHandler) sendError(conn *websocket.Conn, msg string) {
	data := map[string]interface{}{
		"type": "output",
		"data": fmt.Sprintf("\r\n\x1b[1;31m❌ Error: %s\x1b[0m\r\n", msg),
	}
	conn.WriteJSON(data)
}

// TerminalSession implements io.Reader, io.Writer and remotecommand.TerminalSizeQueue
type TerminalSession struct {
	conn   *websocket.Conn
	sizeCh chan remotecommand.TerminalSize
}

func (t *TerminalSession) Read(p []byte) (int, error) {
	var msg struct {
		Type string          `json:"type"`
		Data string          `json:"data"`
		Size *TerminalSize   `json:"size,omitempty"`
	}

	err := t.conn.ReadJSON(&msg)
	if err != nil {
		return 0, err
	}

	switch msg.Type {
	case "input":
		copy(p, []byte(msg.Data))
		return len(msg.Data), nil
	case "resize":
		if msg.Size != nil {
			t.sizeCh <- remotecommand.TerminalSize{
				Width:  msg.Size.Cols,
				Height: msg.Size.Rows,
			}
		}
		return 0, nil
	default:
		return 0, fmt.Errorf("unknown message type: %s", msg.Type)
	}
}

func (t *TerminalSession) Write(p []byte) (int, error) {
	msg := map[string]interface{}{
		"type": "output",
		"data": string(p),
	}
	err := t.conn.WriteJSON(msg)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (t *TerminalSession) Next() *remotecommand.TerminalSize {
	size := <-t.sizeCh
	return &size
}

type TerminalSize struct {
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}
```

## Router Configuration

```go
r.Get("/api/v1/clusters/{cluster}/namespaces/{namespace}/pods/{pod}/debug", debugHandler.HandleDebugSession)
```

## Frontend Integration

O frontend já está configurado para usar este endpoint quando `useEphemeralDebug` é true:

```typescript
const endpoint = ephemeral ? "debug" : "shell";
const wsUrl = `${protocol}//${host}/api/v1/clusters/${cluster}/namespaces/${namespace}/pods/${pod}/${endpoint}?container=${container}&shell=${shell}${ephemeral ? '&image=nicolaka/netshoot' : ''}`;
```

## Security Considerations

1. **RBAC**: Validar permissões do usuário para criar ephemeral containers
2. **Resource Limits**: Ephemeral containers devem ter limites de CPU/memória
3. **Audit Logging**: Registrar criação de ephemeral containers
4. **Cleanup**: Ephemeral containers são removidos automaticamente quando o pod é deletado
5. **Image Validation**: Validar imagem permitida (whitelist)

## Testing

```bash
# Test ephemeral debug creation
kubectl debug -it <pod-name> \
  --image=nicolaka/netshoot \
  --target=<container-name> \
  -- /bin/bash
```

## nicolaka/netshoot Tools

A imagem nicolaka/netshoot inclui:
- **Network**: tcpdump, nmap, netstat, ss, ip, ifconfig, arp, route
- **DNS**: dig, nslookup, host
- **HTTP**: curl, wget, httpie
- **Performance**: iperf, iperf3, mtr, traceroute, ping
- **Debug**: strace, ltrace, gdb
- **Others**: vim, nano, jq, yq, grpcurl

## Advantages over kubectl exec

1. **Non-invasive**: Não modifica containers existentes
2. **Rich toolset**: nicolaka/netshoot tem todas as ferramentas de debug
3. **Process namespace sharing**: Acesso aos processos do container alvo
4. **Temporary**: Removido automaticamente
