// Package podsftp implementa um servidor SFTP real (protocolo, via github.com/pkg/sftp) contra
// um pod K8s — sem exigir NENHUM servidor SSH/SFTP dentro do pod. Cada operação SFTP (listar,
// ler, escrever, renomear, remover, mkdir) é traduzida pra `kubectl cp`/`kubectl exec` contra o
// pod (internal/kubernetes.Client, mesmas primitivas já usadas pelo download da aba Pods, mais as
// novas em pod_file_ops.go).
//
// Por que um servidor SFTP "de verdade" (não só mais um endpoint REST de arquivo): o usuário
// pediu explicitamente robustez de protocolo real, mas com interface 100% dentro da própria
// aplicação — nunca exposto numa porta de rede pra um cliente SFTP externo (WinSCP/FileZilla).
// Resolvido rodando o server E o client do pkg/sftp NO MESMO PROCESSO, conectados por um
// net.Pipe() em memória (nunca toca a rede) — o pacote internal/web/handlers/pod_sftp.go usa o
// *sftp.Client resultante pra implementar os endpoints REST que o modal do frontend consome.
// Ver SFTP-FILE-BROWSER-PLAN.md pro precedente de decisão (não confundir os dois pacotes).
package podsftp

import (
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/pkg/sftp"

	"k8s-hpa-manager/internal/kubernetes"
)

// Target identifica o pod/container alvo de uma sessão SFTP.
type Target struct {
	Cluster   string
	Namespace string
	Pod       string
	Container string
}

// podHandlers implementa as 4 interfaces que sftp.Handlers precisa (FileReader/FileWriter/
// FileCmder/FileLister), cada uma traduzindo a operação SFTP pra uma chamada em *kubernetes.Client.
type podHandlers struct {
	client *kubernetes.Client
	target Target
}

func (h *podHandlers) Fileread(r *sftp.Request) (io.ReaderAt, error) {
	tmpPath, err := h.client.CopyFromPod(h.target.Namespace, h.target.Pod, h.target.Container, r.Filepath, false)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(tmpPath)
	if err != nil {
		os.Remove(tmpPath)
		return nil, err
	}
	return &tempFileReaderAt{File: f, tmpPath: tmpPath}, nil
}

// tempFileReaderAt encapsula o arquivo temporário baixado do pod (via kubectl cp) — *os.File já
// implementa io.ReaderAt nativamente, só precisamos garantir a limpeza do arquivo temp quando o
// pkg/sftp fechar o handle (ele checa se o retorno de Fileread também satisfaz io.Closer).
type tempFileReaderAt struct {
	*os.File
	tmpPath string
}

func (t *tempFileReaderAt) Close() error {
	err := t.File.Close()
	_ = os.Remove(t.tmpPath)
	return err
}

func (h *podHandlers) Filewrite(r *sftp.Request) (io.WriterAt, error) {
	tmp, err := os.CreateTemp("", "pod-sftp-upload-*")
	if err != nil {
		return nil, err
	}
	return &tempFileWriterAt{File: tmp, client: h.client, target: h.target, remotePath: r.Filepath}, nil
}

// tempFileWriterAt acumula os bytes recebidos num arquivo temporário local (*os.File também
// implementa io.WriterAt nativamente) e só envia pro pod (via kubectl cp) quando o pkg/sftp fecha
// o handle — ponto em que sabemos que o upload terminou de verdade (SSH_FXP_CLOSE do lado do
// client SFTP).
type tempFileWriterAt struct {
	*os.File
	client     *kubernetes.Client
	target     Target
	remotePath string
}

func (t *tempFileWriterAt) Close() error {
	localPath := t.File.Name()
	defer os.Remove(localPath)
	if err := t.File.Close(); err != nil {
		return err
	}
	return t.client.CopyToPod(t.target.Namespace, t.target.Pod, t.target.Container, localPath, t.remotePath)
}

func (h *podHandlers) Filecmd(r *sftp.Request) error {
	switch r.Method {
	case "Rename":
		return h.client.RenameInPod(h.target.Namespace, h.target.Pod, h.target.Container, r.Filepath, r.Target)
	case "Rmdir":
		return h.client.RemoveInPod(h.target.Namespace, h.target.Pod, h.target.Container, r.Filepath, true)
	case "Remove":
		return h.client.RemoveInPod(h.target.Namespace, h.target.Pod, h.target.Container, r.Filepath, false)
	case "Mkdir":
		return h.client.MkdirInPod(h.target.Namespace, h.target.Pod, h.target.Container, r.Filepath)
	case "Setstat":
		// Sem suporte a chmod/chown/mtime por ora — no-op silencioso (não é uma operação que o
		// modal do frontend expõe; alguns clients SFTP tentam Setstat automaticamente após
		// upload, então retornar erro aqui quebraria o upload por um detalhe irrelevante pro
		// nosso caso de uso).
		return nil
	default:
		return sftp.ErrSSHFxOpUnsupported
	}
}

func (h *podHandlers) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	switch r.Method {
	case "List":
		entries, err := h.client.ListDirectory(h.target.Namespace, h.target.Pod, h.target.Container, r.Filepath)
		if err != nil {
			return nil, err
		}
		infos := make([]os.FileInfo, 0, len(entries))
		for _, e := range entries {
			infos = append(infos, fileInfoAdapter{e})
		}
		return listerAt(infos), nil
	case "Stat":
		info, err := h.client.StatInPod(h.target.Namespace, h.target.Pod, h.target.Container, r.Filepath)
		if err != nil {
			return nil, err
		}
		return listerAt([]os.FileInfo{fileInfoAdapter{info}}), nil
	default:
		return nil, sftp.ErrSSHFxOpUnsupported
	}
}

// fileInfoAdapter satisfaz os.FileInfo em cima de kubernetes.FileInfo (a struct que ListDirectory
// já retorna) — sem duplicar o parsing de `ls -la`, só adapta o formato.
type fileInfoAdapter struct {
	fi kubernetes.FileInfo
}

func (a fileInfoAdapter) Name() string { return a.fi.Name }
func (a fileInfoAdapter) Size() int64  { return a.fi.Size }
func (a fileInfoAdapter) Mode() os.FileMode {
	if a.fi.IsDirectory {
		return os.ModeDir | 0o755
	}
	return 0o644
}
func (a fileInfoAdapter) ModTime() time.Time { return a.fi.ModTime }
func (a fileInfoAdapter) IsDir() bool        { return a.fi.IsDirectory }
func (a fileInfoAdapter) Sys() any           { return nil }

// listerAt implementa sftp.ListerAt (paginação da listagem) sobre um slice já pronto em memória —
// a listagem inteira já veio de uma única chamada de ListDirectory, então "paginar" aqui é
// puramente um requisito de interface do pkg/sftp, não uma paginação real contra o pod.
type listerAt []os.FileInfo

func (l listerAt) ListAt(dst []os.FileInfo, offset int64) (int, error) {
	if offset >= int64(len(l)) {
		return 0, io.EOF
	}
	n := copy(dst, l[offset:])
	if n < len(dst) {
		return n, io.EOF
	}
	return n, nil
}

// Session é uma sessão SFTP aberta contra um pod — server e client do pkg/sftp rodando no MESMO
// processo, conectados por um net.Pipe() (nunca toca rede/socket real). O *sftp.Client exposto
// aqui é usado pelos handlers REST (internal/web/handlers/pod_sftp.go) pra implementar list/
// download/upload/mkdir/rename/remove — o protocolo SFTP real garante correção (offsets,
// EOF, concorrência de request ID) sem precisarmos reimplementar nada disso à mão.
//
// Deliberadamente SEM persistência entre requisições HTTP (nada de Manager/TTL/sessionId — ver
// SFTP-FILE-BROWSER-PLAN.md pro contraste com internal/portforward, que É stateful porque mantém um
// listener TCP vivo de verdade): cada operação (listar/baixar/enviar/renomear/...) já paga o
// custo de um subprocesso `kubectl exec`/`kubectl cp` de qualquer forma — mesmo padrão do
// download original da aba Pods —, então abrir/fechar a sessão SFTP em memória a cada chamada é
// custo desprezível em comparação, e evita de vez toda a classe de bug de sessão esquecida
// aberta/vazando.
type Session struct {
	Client *sftp.Client
	Target Target

	pipeConn net.Conn // lado do client — fechar isso desliga a sessão (o server percebe e sai do Serve())
}

// NewSession abre uma nova sessão SFTP contra o pod alvo. Chamador é responsável por Close().
func NewSession(kubeClient *kubernetes.Client, target Target) (*Session, error) {
	serverConn, clientConn := net.Pipe()

	h := &podHandlers{client: kubeClient, target: target}
	handlers := sftp.Handlers{FileGet: h, FilePut: h, FileCmd: h, FileList: h}
	server := sftp.NewRequestServer(serverConn, handlers)

	go func() {
		_ = server.Serve()
		_ = serverConn.Close()
	}()

	sftpClient, err := sftp.NewClientPipe(clientConn, clientConn)
	if err != nil {
		_ = server.Close()
		_ = clientConn.Close()
		return nil, fmt.Errorf("falha ao abrir sessão SFTP: %w", err)
	}

	return &Session{
		Client:   sftpClient,
		Target:   target,
		pipeConn: clientConn,
	}, nil
}

// Close encerra a sessão SFTP — fecha o client, o que faz o pipe fechar e o server (rodando na
// goroutine de NewSession) sair do Serve() sozinho.
func (s *Session) Close() error {
	err := s.Client.Close()
	_ = s.pipeConn.Close()
	return err
}

// IsPermissionDenied é um helper pro handler HTTP distinguir "sem permissão no pod" (mensagem
// amigável) de outros erros — kubectl exec/cp devolve texto livre no stderr, não um código
// estruturado, então a checagem é por substring mesmo (mesmo padrão usado alhures no projeto pra
// erros de CLI externa sem saída estruturada).
func IsPermissionDenied(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "permission denied")
}
