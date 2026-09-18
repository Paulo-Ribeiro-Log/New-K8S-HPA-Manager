package handlers

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	awsprovider "k8s-hpa-manager/internal/cloudprovider/aws"
)

// vm_sftp_ssm.go — generaliza pro navegador de arquivos GERAL (VMSFTPModal.tsx, "Arquivos (SFTP)"
// na aba VMs/EC2) o mesmo transporte "SSM sem SSH" já construído só pra certificados
// (certificates_vm.go, ListDirectoryViaSSM/ReadRemoteCertificateViaSSM/TransferCertificateViaSSM).
// Motivado por um pedido explícito do usuário: "na lista das VMs... a opção de SSM (sem sshd) não
// existe" — até aqui só dava pra usar esse transporte de dentro do painel de Certificados em VM,
// nunca do gerenciador de arquivos geral. Mesmos princípios já documentados em certificates_vm.go:
// nunca interpola conteúdo cru numa linha de shell (base64 + ShellQuote), timeout compartilhado
// (ssmCommandTimeout, certificates_vm.go), find/printf pra listagem (parseFindPrintfOutput,
// certificates_vm.go — mesma função, sem duplicar o parser).
//
// vmSSMFileMaxBytes — teto de segurança pra ler/escrever um arquivo inteiro via SSM Run Command
// (base64 embutido no corpo do comando OU na saída de texto de volta) — bem abaixo do limite real
// de 64KB documentado pela AWS pro documento SendCommand inteiro (MaxDocumentSizeExceeded, ver
// nota histórica em certificates_vm.go), descontando a expansão de ~33% do base64 e o
// script/wrapper ao redor. Arquivos maiores precisam de um transporte de verdade (SSH direto ou
// SSM — túnel até sshd), que já lida bem com qualquer tamanho via streaming (io.Copy).
const vmSSMFileMaxBytes = 40 * 1024

// decodeSSMBase64Output remove as quebras de linha que o comando `base64` insere a cada ~76
// caracteres (padrão POSIX, confirmado ao vivo rodando `base64` localmente) antes de decodificar —
// sem isso, encoding/base64 rejeita a string inteira por causa dos '\n' embutidos no meio.
func decodeSSMBase64Output(raw string) ([]byte, error) {
	cleaned := strings.NewReplacer("\r", "", "\n", "").Replace(raw)
	return base64.StdEncoding.DecodeString(cleaned)
}

// ssmFileTooLargeMessage — mensagem compartilhada entre download/upload quando o arquivo excede
// vmSSMFileMaxBytes, sempre citando o tamanho real e a alternativa que de fato resolve.
func ssmFileTooLargeMessage(actualBytes int64) string {
	return fmt.Sprintf(
		"arquivo tem %d bytes — maior que o limite de %d bytes suportado via SSM sem SSH. Use SSH direto ou SSM (túnel até sshd) pra arquivos grandes.",
		actualBytes, vmSSMFileMaxBytes,
	)
}

// VMSFTPListSSM — GET /api/v1/vms/:instanceId/sftp/list-ssm?profile=&region=&path=
func (h *VMSFTPHandler) VMSFTPListSSM(c *gin.Context) {
	instanceID := c.Param("instanceId")
	profile := c.Query("profile")
	region := c.Query("region")
	dirPath := c.DefaultQuery("path", "/")
	if dirPath == "" {
		dirPath = "/"
	}
	if profile == "" || region == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "INVALID_REQUEST", "message": "profile e region são obrigatórios"},
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), ssmCommandTimeout)
	defer cancel()

	cmd := fmt.Sprintf(
		"find %s -mindepth 1 -maxdepth 1 -printf '%%y\\t%%s\\t%%f\\n' 2>/dev/null | sort -k3",
		awsprovider.ShellQuote(dirPath),
	)
	result, err := awsprovider.RunShellCommand(ctx, profile, region, instanceID, []string{cmd})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"error":   gin.H{"code": "SSM_COMMAND_ERROR", "message": err.Error()},
		})
		return
	}
	if result.Status != "Success" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   gin.H{"code": "SSM_COMMAND_FAILED", "message": ssmFailureMessage(result.StandardErrorContent, result.Status)},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"path":    dirPath,
		"entries": parseFindPrintfOutput(result.StandardOutputContent, dirPath),
	})
}

// VMSFTPDownloadSSM — GET /api/v1/vms/:instanceId/sftp/download-ssm?profile=&region=&path=. Checa
// o tamanho ANTES de tentar ler — evita gastar uma chamada SSM inteira (latência real, ver
// ssmCommandTimeout) só pra descobrir depois que o arquivo é grande demais pra este transporte.
func (h *VMSFTPHandler) VMSFTPDownloadSSM(c *gin.Context) {
	instanceID := c.Param("instanceId")
	profile := c.Query("profile")
	region := c.Query("region")
	filePath := c.Query("path")
	if profile == "" || region == "" || filePath == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "INVALID_REQUEST", "message": "profile, region e path são obrigatórios"},
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), ssmCommandTimeout)
	defer cancel()

	// `stat -c%s` (GNU coreutils, Linux real de qualquer AMI EC2) devolve só o tamanho em bytes —
	// nunca lista/lê o conteúdo, então é seguro rodar mesmo pra um arquivo que acabará rejeitado.
	statCmd := fmt.Sprintf("stat -c%%s %s", awsprovider.ShellQuote(filePath))
	statResult, err := awsprovider.RunShellCommand(ctx, profile, region, instanceID, []string{statCmd})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"error":   gin.H{"code": "SSM_COMMAND_ERROR", "message": err.Error()},
		})
		return
	}
	if statResult.Status != "Success" {
		msg := statResult.StandardErrorContent
		if msg == "" {
			msg = "arquivo não encontrado ou sem permissão de leitura"
		}
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   gin.H{"code": "SSM_COMMAND_FAILED", "message": msg},
		})
		return
	}
	size, _ := strconv.ParseInt(strings.TrimSpace(statResult.StandardOutputContent), 10, 64)
	if size > vmSSMFileMaxBytes {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   gin.H{"code": "FILE_TOO_LARGE", "message": ssmFileTooLargeMessage(size)},
		})
		return
	}

	readCmd := fmt.Sprintf("base64 %s", awsprovider.ShellQuote(filePath))
	result, err := awsprovider.RunShellCommand(ctx, profile, region, instanceID, []string{readCmd})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"error":   gin.H{"code": "SSM_COMMAND_ERROR", "message": err.Error()},
		})
		return
	}
	if result.Status != "Success" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   gin.H{"code": "SSM_COMMAND_FAILED", "message": ssmFailureMessage(result.StandardErrorContent, result.Status)},
		})
		return
	}

	data, err := decodeSSMBase64Output(result.StandardOutputContent)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   gin.H{"code": "DECODE_ERROR", "message": "falha ao decodificar o conteúdo recebido: " + err.Error()},
		})
		return
	}

	filename := path.Base(filePath)
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Data(http.StatusOK, "application/octet-stream", data)

	h.logAction(c, "vm-sftp-download-ssm", instanceID, fmt.Sprintf("profile=%s region=%s", profile, region), "success", map[string]interface{}{"path": filePath, "bytes": len(data)}, "")
}

// VMSFTPUploadSSM — POST /api/v1/vms/:instanceId/sftp/upload-ssm?profile=&region=&path= (multipart,
// campo "file"). Checa o tamanho ANTES de gastar uma chamada SSM — mesmo racional de
// VMSFTPDownloadSSM, só que aqui o tamanho já vem de graça no próprio multipart (fileHeader.Size),
// sem precisar de uma chamada extra.
func (h *VMSFTPHandler) VMSFTPUploadSSM(c *gin.Context) {
	instanceID := c.Param("instanceId")
	profile := c.Query("profile")
	region := c.Query("region")
	remotePath := c.Query("path")
	if profile == "" || region == "" || remotePath == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "INVALID_REQUEST", "message": "profile, region e path são obrigatórios"},
		})
		return
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "MISSING_FILE", "message": "campo 'file' (multipart) é obrigatório: " + err.Error()},
		})
		return
	}
	if fileHeader.Size > vmSSMFileMaxBytes {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   gin.H{"code": "FILE_TOO_LARGE", "message": ssmFileTooLargeMessage(fileHeader.Size)},
		})
		return
	}
	src, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   gin.H{"code": "FILE_OPEN_ERROR", "message": err.Error()},
		})
		return
	}
	defer src.Close()
	content, err := io.ReadAll(src)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   gin.H{"code": "FILE_READ_ERROR", "message": err.Error()},
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), ssmCommandTimeout)
	defer cancel()

	b64 := base64.StdEncoding.EncodeToString(content)
	commands := []string{
		fmt.Sprintf("mkdir -p %s", awsprovider.ShellQuote(path.Dir(remotePath))),
		fmt.Sprintf("echo %s | base64 -d > %s", awsprovider.ShellQuote(b64), awsprovider.ShellQuote(remotePath)),
	}
	result, err := awsprovider.RunShellCommand(ctx, profile, region, instanceID, commands)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"error":   gin.H{"code": "SSM_COMMAND_ERROR", "message": err.Error()},
		})
		return
	}
	if result.Status != "Success" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   gin.H{"code": "SSM_COMMAND_FAILED", "message": ssmFailureMessage(result.StandardErrorContent, result.Status)},
		})
		return
	}

	h.logAction(c, "vm-sftp-upload-ssm", instanceID, fmt.Sprintf("profile=%s region=%s", profile, region), "success", map[string]interface{}{"path": remotePath, "bytes": len(content)}, "")
	c.JSON(http.StatusOK, gin.H{"success": true, "path": remotePath, "bytes_written": len(content)})
}

type vmSSMFileMkdirRequest struct {
	Profile string `json:"profile" binding:"required"`
	Region  string `json:"region" binding:"required"`
	Path    string `json:"path" binding:"required"`
}

// VMSFTPMkdirSSM — POST /api/v1/vms/:instanceId/sftp/mkdir-ssm (corpo JSON)
func (h *VMSFTPHandler) VMSFTPMkdirSSM(c *gin.Context) {
	var req vmSSMFileMkdirRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "INVALID_REQUEST", "message": err.Error()},
		})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), ssmCommandTimeout)
	defer cancel()
	result, err := awsprovider.RunShellCommand(ctx, req.Profile, req.Region, c.Param("instanceId"), []string{
		fmt.Sprintf("mkdir -p %s", awsprovider.ShellQuote(req.Path)),
	})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"error":   gin.H{"code": "SSM_COMMAND_ERROR", "message": err.Error()},
		})
		return
	}
	if result.Status != "Success" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   gin.H{"code": "SSM_COMMAND_FAILED", "message": ssmFailureMessage(result.StandardErrorContent, result.Status)},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "path": req.Path})
}

type vmSSMFileRenameRequest struct {
	Profile string `json:"profile" binding:"required"`
	Region  string `json:"region" binding:"required"`
	OldPath string `json:"old_path" binding:"required"`
	NewPath string `json:"new_path" binding:"required"`
}

// VMSFTPRenameSSM — POST /api/v1/vms/:instanceId/sftp/rename-ssm (corpo JSON)
func (h *VMSFTPHandler) VMSFTPRenameSSM(c *gin.Context) {
	var req vmSSMFileRenameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "INVALID_REQUEST", "message": err.Error()},
		})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), ssmCommandTimeout)
	defer cancel()
	result, err := awsprovider.RunShellCommand(ctx, req.Profile, req.Region, c.Param("instanceId"), []string{
		fmt.Sprintf("mv %s %s", awsprovider.ShellQuote(req.OldPath), awsprovider.ShellQuote(req.NewPath)),
	})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"error":   gin.H{"code": "SSM_COMMAND_ERROR", "message": err.Error()},
		})
		return
	}
	if result.Status != "Success" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   gin.H{"code": "SSM_COMMAND_FAILED", "message": ssmFailureMessage(result.StandardErrorContent, result.Status)},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// VMSFTPRemoveSSM — DELETE /api/v1/vms/:instanceId/sftp/remove-ssm?profile=&region=&path=&is_dir=.
// `rmdir` (não-recursivo) pra diretório — mesma semântica de sftp.RemoveDirectory (VMSFTPRemove),
// só remove se já estiver vazio; `rm` simples pra arquivo. Nunca `rm -rf` — mesmo cuidado do
// caminho SFTP normal, que também nunca remove recursivamente por trás das costas do usuário.
func (h *VMSFTPHandler) VMSFTPRemoveSSM(c *gin.Context) {
	profile := c.Query("profile")
	region := c.Query("region")
	targetPath := c.Query("path")
	isDir := c.Query("is_dir") == "true"
	if profile == "" || region == "" || targetPath == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "INVALID_REQUEST", "message": "profile, region e path são obrigatórios"},
		})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), ssmCommandTimeout)
	defer cancel()
	cmd := fmt.Sprintf("rm %s", awsprovider.ShellQuote(targetPath))
	if isDir {
		cmd = fmt.Sprintf("rmdir %s", awsprovider.ShellQuote(targetPath))
	}
	result, err := awsprovider.RunShellCommand(ctx, profile, region, c.Param("instanceId"), []string{cmd})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"error":   gin.H{"code": "SSM_COMMAND_ERROR", "message": err.Error()},
		})
		return
	}
	if result.Status != "Success" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   gin.H{"code": "SSM_COMMAND_FAILED", "message": ssmFailureMessage(result.StandardErrorContent, result.Status)},
		})
		return
	}
	h.logAction(c, "vm-sftp-remove-ssm", c.Param("instanceId"), fmt.Sprintf("profile=%s region=%s", profile, region), "success", map[string]interface{}{"path": targetPath, "is_dir": isDir}, "")
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ssmFailureMessage centraliza o fallback "comando terminou com status %s" já duplicado em todo
// handler *ViaSSM deste pacote (certificates_vm.go incluso) — prioriza stderr real quando existe.
func ssmFailureMessage(stderr, status string) string {
	if stderr != "" {
		return stderr
	}
	return fmt.Sprintf("comando terminou com status %s", status)
}
