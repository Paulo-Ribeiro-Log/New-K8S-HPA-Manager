package handlers

import (
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"k8s-hpa-manager/internal/history"
	kubeclient "k8s-hpa-manager/internal/kubernetes"
	"k8s-hpa-manager/internal/podsftp"
)

// pod_sftp.go — endpoints REST que expõem o servidor SFTP embutido (internal/podsftp) pro modal
// de transferência de arquivos do frontend. Cada chamada abre uma sessão SFTP em memória (ver
// podsftp.Session — deliberadamente sem persistência entre requisições), faz UMA operação, fecha.
// Métodos em *PodHandler (mesmo handler de pods.go) pra reaproveitar kubeManager/historyTracker
// já injetados — arquivo separado só por organização, não é um handler novo.

// openSFTPSession resolve cluster/namespace/pod/container comuns a todas as rotas + abre a
// sessão SFTP. Retorna (nil, false) já tendo escrito a resposta de erro quando algo falha.
func (h *PodHandler) openSFTPSession(c *gin.Context) (*podsftp.Session, bool) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	podName := c.Param("name")
	container := c.Query("container")

	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusBadGateway, errorResponse("CLUSTER_ERROR", err.Error()))
		return nil, false
	}
	kubeClient := kubeclient.NewClient(clientset, cluster)

	session, err := podsftp.NewSession(kubeClient, podsftp.Target{
		Cluster:   cluster,
		Namespace: namespace,
		Pod:       podName,
		Container: container,
	})
	if err != nil {
		c.JSON(http.StatusBadGateway, errorResponse("SFTP_SESSION_ERROR", err.Error()))
		return nil, false
	}
	return session, true
}

// SFTPFileEntry é a projeção JSON de uma entrada de diretório.
type SFTPFileEntry struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	IsDir   bool   `json:"is_dir"`
	ModTime string `json:"mod_time"`
	Mode    string `json:"mode"`
}

// SFTPList — GET /api/v1/pods/:cluster/:namespace/:name/sftp/list?container=&path=
func (h *PodHandler) SFTPList(c *gin.Context) {
	dirPath := c.DefaultQuery("path", "/")
	if dirPath == "" {
		dirPath = "/"
	}

	session, ok := h.openSFTPSession(c)
	if !ok {
		return
	}
	defer session.Close()

	entries, err := session.Client.ReadDir(dirPath)
	if err != nil {
		c.JSON(http.StatusBadGateway, errorResponse("SFTP_LIST_ERROR", err.Error()))
		return
	}

	result := make([]SFTPFileEntry, 0, len(entries))
	for _, e := range entries {
		result = append(result, SFTPFileEntry{
			Name:    e.Name(),
			Path:    path.Join(dirPath, e.Name()),
			Size:    e.Size(),
			IsDir:   e.IsDir(),
			ModTime: e.ModTime().Format(time.RFC3339),
			Mode:    e.Mode().String(),
		})
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "path": dirPath, "entries": result})
}

// SFTPDownload — GET /api/v1/pods/:cluster/:namespace/:name/sftp/download?container=&path=
// Streaming direto (io.Copy) — não escreve arquivo temporário do lado do handler HTTP (o
// temporário do kubectl cp já é gerenciado dentro de podHandlers.Fileread/tempFileReaderAt).
func (h *PodHandler) SFTPDownload(c *gin.Context) {
	filePath := c.Query("path")
	if filePath == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PATH", "path é obrigatório"))
		return
	}

	session, ok := h.openSFTPSession(c)
	if !ok {
		return
	}
	defer session.Close()

	f, err := session.Client.Open(filePath)
	if err != nil {
		c.JSON(http.StatusBadGateway, errorResponse("SFTP_DOWNLOAD_ERROR", err.Error()))
		return
	}
	defer f.Close()

	info, _ := f.Stat()
	filename := path.Base(filePath)
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Header("Content-Type", "application/octet-stream")
	if info != nil {
		c.Header("Content-Length", strconv.FormatInt(info.Size(), 10))
	}

	if h.historyTracker != nil {
		userInfo := GetUserInfoForHistory(c)
		_ = h.historyTracker.Log(history.HistoryEntry{
			UserEmail: userInfo.Email,
			UserName:  userInfo.Name,
			Action:    "sftp_download",
			Resource:  fmt.Sprintf("%s/%s", c.Param("namespace"), c.Param("name")),
			Cluster:   c.Param("cluster"),
			Status:    "success",
			After:     map[string]interface{}{"path": filePath},
		})
	}

	if _, err := io.Copy(c.Writer, f); err != nil {
		// Resposta já começou a ser escrita — não dá mais pra mandar um JSON de erro limpo,
		// só logar. Mesmo padrão de streaming já usado em DownloadFromPod.
		fmt.Printf("[SFTPDownload] erro durante o streaming: %v\n", err)
	}
}

// SFTPUpload — POST /api/v1/pods/:cluster/:namespace/:name/sftp/upload?container=&path=
// (multipart, campo "file"). `path` é o caminho REMOTO completo de destino (diretório + nome do
// arquivo já resolvido pelo frontend — evita ambiguidade sobre "renomear ao enviar" vs "manter
// nome original").
func (h *PodHandler) SFTPUpload(c *gin.Context) {
	remotePath := c.Query("path")
	if remotePath == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PATH", "path é obrigatório"))
		return
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_FILE", "campo 'file' (multipart) é obrigatório: "+err.Error()))
		return
	}
	src, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("FILE_OPEN_ERROR", err.Error()))
		return
	}
	defer src.Close()

	session, ok := h.openSFTPSession(c)
	if !ok {
		return
	}
	defer session.Close()

	dst, err := session.Client.Create(remotePath)
	if err != nil {
		c.JSON(http.StatusBadGateway, errorResponse("SFTP_UPLOAD_ERROR", err.Error()))
		return
	}

	written, copyErr := io.Copy(dst, src)
	closeErr := dst.Close() // é no Close() que o upload de verdade vai pro pod (kubectl cp) — ver tempFileWriterAt
	if copyErr != nil {
		c.JSON(http.StatusBadGateway, errorResponse("SFTP_UPLOAD_ERROR", copyErr.Error()))
		return
	}
	if closeErr != nil {
		c.JSON(http.StatusBadGateway, errorResponse("SFTP_UPLOAD_ERROR", "falha ao enviar pro pod: "+closeErr.Error()))
		return
	}

	if h.historyTracker != nil {
		userInfo := GetUserInfoForHistory(c)
		_ = h.historyTracker.Log(history.HistoryEntry{
			UserEmail: userInfo.Email,
			UserName:  userInfo.Name,
			Action:    "sftp_upload",
			Resource:  fmt.Sprintf("%s/%s", c.Param("namespace"), c.Param("name")),
			Cluster:   c.Param("cluster"),
			Status:    "success",
			After:     map[string]interface{}{"path": remotePath, "bytes": written},
		})
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "path": remotePath, "bytes_written": written})
}

type sftpMkdirRequest struct {
	Path string `json:"path" binding:"required"`
}

// SFTPMkdir — POST /api/v1/pods/:cluster/:namespace/:name/sftp/mkdir?container=
func (h *PodHandler) SFTPMkdir(c *gin.Context) {
	var req sftpMkdirRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", err.Error()))
		return
	}

	session, ok := h.openSFTPSession(c)
	if !ok {
		return
	}
	defer session.Close()

	if err := session.Client.MkdirAll(req.Path); err != nil {
		c.JSON(http.StatusBadGateway, errorResponse("SFTP_MKDIR_ERROR", err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "path": req.Path})
}

type sftpRenameRequest struct {
	OldPath string `json:"old_path" binding:"required"`
	NewPath string `json:"new_path" binding:"required"`
}

// SFTPRename — POST /api/v1/pods/:cluster/:namespace/:name/sftp/rename?container=
func (h *PodHandler) SFTPRename(c *gin.Context) {
	var req sftpRenameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", err.Error()))
		return
	}

	session, ok := h.openSFTPSession(c)
	if !ok {
		return
	}
	defer session.Close()

	if err := session.Client.Rename(req.OldPath, req.NewPath); err != nil {
		c.JSON(http.StatusBadGateway, errorResponse("SFTP_RENAME_ERROR", err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// SFTPRemove — DELETE /api/v1/pods/:cluster/:namespace/:name/sftp/remove?container=&path=&is_dir=
func (h *PodHandler) SFTPRemove(c *gin.Context) {
	targetPath := c.Query("path")
	if targetPath == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PATH", "path é obrigatório"))
		return
	}
	isDir := c.Query("is_dir") == "true"

	session, ok := h.openSFTPSession(c)
	if !ok {
		return
	}
	defer session.Close()

	var err error
	if isDir {
		// Rmdir (via podHandlers.Filecmd) já faz `rm -rf` do lado do pod — remove recursivamente,
		// não só diretório vazio (diferente da semântica usual de SFTP Rmdir/RFC — decisão
		// deliberada pra dar suporte a "excluir pasta com conteúdo" no modal, sem precisar de uma
		// segunda chamada por item).
		err = session.Client.RemoveDirectory(targetPath)
	} else {
		err = session.Client.Remove(targetPath)
	}
	if err != nil {
		c.JSON(http.StatusBadGateway, errorResponse("SFTP_REMOVE_ERROR", err.Error()))
		return
	}

	if h.historyTracker != nil {
		userInfo := GetUserInfoForHistory(c)
		_ = h.historyTracker.Log(history.HistoryEntry{
			UserEmail: userInfo.Email,
			UserName:  userInfo.Name,
			Action:    "sftp_remove",
			Resource:  fmt.Sprintf("%s/%s", c.Param("namespace"), c.Param("name")),
			Cluster:   c.Param("cluster"),
			Status:    "success",
			Before:    map[string]interface{}{"path": targetPath, "is_dir": isDir},
		})
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}
