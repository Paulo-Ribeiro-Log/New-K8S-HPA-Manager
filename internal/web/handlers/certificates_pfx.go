package handlers

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"k8s-hpa-manager/internal/certificates"
)

// pfxMaxUploadBytes limita o tamanho do arquivo .pfx aceito — 1MB é generoso o suficiente pra
// qualquer .pfx real (mesmo com uma chain longa, esses arquivos raramente passam de algumas
// dezenas de KB); evita aceitar upload de arquivo gigante por engano.
const pfxMaxUploadBytes = 1 << 20 // 1MB

// ExtractPFX decodifica um arquivo .pfx/.p12 (multipart: file/password/name/comment) e salva o
// resultado (tls.crt/tls.key) em PFXExtractStore — ver PFX-CERT-EXTRACTION-PLAN.md. A senha do
// .pfx nunca é persistida: usada uma única vez por certificates.ExtractPFX, descartada em seguida.
func (h *CertificatesHandler) ExtractPFX(c *gin.Context) {
	if h.pfxExtractStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"error":   gin.H{"code": "PFX_EXTRACT_UNAVAILABLE", "message": "Extração de .pfx indisponível neste servidor"},
		})
		return
	}

	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "MISSING_NAME", "message": "Nome do certificado é obrigatório"},
		})
		return
	}
	password := c.PostForm("password")
	comment := c.PostForm("comment")

	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "MISSING_FILE", "message": "Arquivo .pfx é obrigatório"},
		})
		return
	}
	if fileHeader.Size > pfxMaxUploadBytes {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "FILE_TOO_LARGE", "message": fmt.Sprintf("Arquivo .pfx maior que %dMB", pfxMaxUploadBytes/(1<<20))},
		})
		return
	}

	f, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "FILE_ERROR", "message": err.Error()},
		})
		return
	}
	defer f.Close() //nolint:errcheck

	pfxBytes, err := io.ReadAll(io.LimitReader(f, pfxMaxUploadBytes+1))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "FILE_ERROR", "message": err.Error()},
		})
		return
	}

	tlsCrt, tlsKey, leaf, err := certificates.ExtractPFX(pfxBytes, password)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "PFX_DECODE_ERROR", "message": err.Error()},
		})
		return
	}

	info, err := h.pfxExtractStore.Save(name, comment, fileHeader.Filename, tlsCrt, tlsKey, leaf)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   gin.H{"code": "SAVE_ERROR", "message": err.Error()},
		})
		return
	}

	// Devolve os PEMs completos junto com a metadata — evita um segundo round-trip pro frontend já
	// popular os campos tls.crt/tls.key logo após a extração, sem precisar reabrir via o picker.
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"info":    info,
			"tls_crt": string(tlsCrt),
			"tls_key": string(tlsKey),
		},
	})
}

// ListPFXNames lista os nomes que têm ao menos 1 extração de .pfx salva — alimenta a 3ª aba
// ("Extraído de PFX") do CertificateSourcePickerModal.tsx.
func (h *CertificatesHandler) ListPFXNames(c *gin.Context) {
	if h.pfxExtractStore == nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": []string{}})
		return
	}

	names, err := h.pfxExtractStore.ListNames()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   gin.H{"code": "LIST_ERROR", "message": err.Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": names})
}

// ListPFXExtracts lista as extrações de um nome específico (metadata apenas, sem chave).
func (h *CertificatesHandler) ListPFXExtracts(c *gin.Context) {
	name := c.Param("name")

	if h.pfxExtractStore == nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": []certificates.PFXExtractInfo{}})
		return
	}

	list, err := h.pfxExtractStore.List(name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   gin.H{"code": "LIST_ERROR", "message": err.Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": list})
}

// GetPFXContent devolve o PEM bruto (tls.crt/tls.key) de uma extração — expõe chave privada, RBAC
// obrigatório.
func (h *CertificatesHandler) GetPFXContent(c *gin.Context) {
	name := c.Param("name")
	extractID := c.Param("extractId")

	if h.pfxExtractStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"error":   gin.H{"code": "PFX_EXTRACT_UNAVAILABLE", "message": "Extração de .pfx indisponível neste servidor"},
		})
		return
	}

	tlsCrt, tlsKey, _, err := h.pfxExtractStore.Get(name, extractID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   gin.H{"code": "EXTRACT_NOT_FOUND", "message": err.Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"tls_crt": string(tlsCrt), "tls_key": string(tlsKey)},
	})
}

// UpdatePFXCommentRequest é o body de PUT .../pfx/:name/:extractId/comment
type UpdatePFXCommentRequest struct {
	Comment string `json:"comment"`
}

// UpdatePFXComment edita só o comentário de uma extração já salva — não mexe no PEM salvo.
func (h *CertificatesHandler) UpdatePFXComment(c *gin.Context) {
	name := c.Param("name")
	extractID := c.Param("extractId")

	if h.pfxExtractStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"error":   gin.H{"code": "PFX_EXTRACT_UNAVAILABLE", "message": "Extração de .pfx indisponível neste servidor"},
		})
		return
	}

	var req UpdatePFXCommentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "INVALID_REQUEST", "message": fmt.Sprintf("Requisicao invalida: %v", err)},
		})
		return
	}

	if err := h.pfxExtractStore.UpdateComment(name, extractID, req.Comment); err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "não encontrada") {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{
			"success": false,
			"error":   gin.H{"code": "UPDATE_ERROR", "message": err.Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// DeletePFX remove por completo uma extração (tls.crt/tls.key/metadata.json) — ação destrutiva e
// irreversível, o frontend SEMPRE confirma antes de chamar esta rota.
func (h *CertificatesHandler) DeletePFX(c *gin.Context) {
	name := c.Param("name")
	extractID := c.Param("extractId")

	if h.pfxExtractStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"error":   gin.H{"code": "PFX_EXTRACT_UNAVAILABLE", "message": "Extração de .pfx indisponível neste servidor"},
		})
		return
	}

	if err := h.pfxExtractStore.Delete(name, extractID); err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "não encontrada") {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{
			"success": false,
			"error":   gin.H{"code": "DELETE_ERROR", "message": err.Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "deleted": fmt.Sprintf("%s/%s", name, extractID)})
}
