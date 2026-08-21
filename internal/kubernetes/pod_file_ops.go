package kubernetes

import (
	"fmt"
	"os/exec"
	"strings"
)

// pod_file_ops.go — primitivas de sistema de arquivos remoto contra um pod, todas via
// `kubectl cp`/`kubectl exec` (mesmo mecanismo já usado por CopyFromPod/ListDirectory neste
// pacote). Escritas pra dar suporte ao servidor SFTP embutido (internal/podsftp/) — CopyFromPod e
// ListDirectory já existiam (usados pelo download da aba Pods); os métodos deste arquivo
// completam o conjunto que faltava (upload, mkdir, remover, renomear, stat de um único arquivo).

// shellQuote envolve o path em aspas simples pra uso seguro dentro de um `sh -c "..."` — escapa
// aspas simples literais no path (raro, mas possível) duplicando-as fora/dentro das aspas
// (técnica padrão POSIX: 'não posso ter aspas simples' vira '"'"'não posso...'"'"').
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

// execInPod roda um comando shell dentro do pod via `kubectl exec -- sh -c <cmd>`, retornando
// stdout+stderr combinados (mesmo padrão de ListDirectory/CopyFromPod).
func (c *Client) execInPod(namespace, podName, container, shCmd string) (string, error) {
	args := []string{"exec", podName, "-n", namespace, "--context", c.cluster}
	if container != "" {
		args = append(args, "-c", container)
	}
	args = append(args, "--", "sh", "-c", shCmd)

	cmd := exec.Command("kubectl", args...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

// CopyToPod copia um arquivo LOCAL pro pod via `kubectl cp` — direção oposta de CopyFromPod
// (que já existia, usado pelo download da aba Pods). Usado pelo upload do SFTP embutido.
func (c *Client) CopyToPod(namespace, podName, container, localPath, remotePath string) error {
	dest := fmt.Sprintf("%s/%s:%s", namespace, podName, remotePath)
	args := []string{"cp", localPath, dest, "--context", c.cluster}
	if container != "" {
		args = append(args, "-c", container)
	}

	cmd := exec.Command("kubectl", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("kubectl cp (upload) failed: %v - output: %s", err, string(output))
	}
	return nil
}

// MkdirInPod cria um diretório (com -p, cria pais também) no pod.
func (c *Client) MkdirInPod(namespace, podName, container, path string) error {
	out, err := c.execInPod(namespace, podName, container, "mkdir -p "+shellQuote(path))
	if err != nil {
		return fmt.Errorf("mkdir falhou: %v - %s", err, strings.TrimSpace(out))
	}
	return nil
}

// RemoveInPod remove um arquivo ou diretório (recursive=true equivale a `rm -rf`) no pod.
func (c *Client) RemoveInPod(namespace, podName, container, path string, recursive bool) error {
	flag := "-f"
	if recursive {
		flag = "-rf"
	}
	out, err := c.execInPod(namespace, podName, container, "rm "+flag+" "+shellQuote(path))
	if err != nil {
		return fmt.Errorf("remove falhou: %v - %s", err, strings.TrimSpace(out))
	}
	return nil
}

// RenameInPod renomeia/move um arquivo ou diretório dentro do pod.
func (c *Client) RenameInPod(namespace, podName, container, oldPath, newPath string) error {
	out, err := c.execInPod(namespace, podName, container, "mv "+shellQuote(oldPath)+" "+shellQuote(newPath))
	if err != nil {
		return fmt.Errorf("rename falhou: %v - %s", err, strings.TrimSpace(out))
	}
	return nil
}

// StatInPod resolve as informações de UM único arquivo/diretório — reaproveita ListDirectory
// (parsing de `ls -la` já testado em produção pelo download da aba Pods) sobre o diretório pai e
// filtra pelo nome, em vez de reimplementar parsing de `stat` (cujo formato de saída diverge
// entre GNU coreutils e busybox — muitos pods de aplicação usam imagens Alpine/busybox).
func (c *Client) StatInPod(namespace, podName, container, filePath string) (FileInfo, error) {
	dir, name := splitPath(filePath)
	entries, err := c.ListDirectory(namespace, podName, container, dir)
	if err != nil {
		return FileInfo{}, err
	}
	for _, e := range entries {
		if e.Name == name {
			return e, nil
		}
	}
	return FileInfo{}, fmt.Errorf("arquivo não encontrado: %s", filePath)
}

// splitPath separa um path absoluto em (diretório-pai, nome-base) — versão simples que sempre
// trabalha com "/" (paths de pod Linux, nunca Windows), diferente de path/filepath.Split que usa
// o separador do SO onde o SERVIDOR roda (poderia ser diferente do pod em teoria, embora hoje
// só rodemos em Linux — mantido explícito por clareza/robustez).
func splitPath(p string) (dir, name string) {
	p = strings.TrimSuffix(p, "/")
	idx := strings.LastIndex(p, "/")
	if idx < 0 {
		return "/", p
	}
	if idx == 0 {
		return "/", p[1:]
	}
	return p[:idx], p[idx+1:]
}
