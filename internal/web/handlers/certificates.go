package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"k8s-hpa-manager/internal/certificates"
	"k8s-hpa-manager/internal/config"
)

// CertificatesHandler gerencia as rotas de certificados TLS
type CertificatesHandler struct {
	scanner *certificates.Scanner
}

// NewCertificatesHandler cria um handler de certificados
func NewCertificatesHandler(km *config.KubeConfigManager) *CertificatesHandler {
	return &CertificatesHandler{
		scanner: certificates.NewScanner(km),
	}
}

// Scan escaneia certificados TLS nos clusters especificados
func (h *CertificatesHandler) Scan(c *gin.Context) {
	var req certificates.ScanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "INVALID_REQUEST",
				"message": fmt.Sprintf("Requisicao invalida: %v", err),
			},
		})
		return
	}

	if len(req.Clusters) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "MISSING_CLUSTERS",
				"message": "Pelo menos um cluster deve ser especificado",
			},
		})
		return
	}

	log.Info().
		Strs("clusters", req.Clusters).
		Strs("namespaces", req.Namespaces).
		Str("filter", req.Filter).
		Msg("Iniciando scan de certificados TLS")

	result, err := h.scanner.ScanClusters(c.Request.Context(), req)
	if err != nil {
		log.Error().Err(err).Msg("Erro ao escanear certificados")
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "SCAN_ERROR",
				"message": fmt.Sprintf("Erro ao escanear certificados: %v", err),
			},
		})
		return
	}

	log.Info().
		Int("total", result.Summary.Total).
		Int("valid", result.Summary.Valid).
		Int("expiring", result.Summary.Expiring).
		Int("expired", result.Summary.Expired).
		Msg("Scan de certificados concluido")

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}

// GetDetails retorna detalhes de um certificado específico
func (h *CertificatesHandler) GetDetails(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	name := c.Param("name")

	if cluster == "" || namespace == "" || name == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "MISSING_PARAMETER",
				"message": "cluster, namespace e name sao obrigatorios",
			},
		})
		return
	}

	info, err := h.scanner.GetCertificateDetails(c.Request.Context(), cluster, namespace, name)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "nao encontrado") {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "DETAILS_ERROR",
				"message": fmt.Sprintf("Erro ao obter detalhes: %v", err),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    info,
	})
}

// Copy copia um certificado para outros clusters/namespaces
func (h *CertificatesHandler) Copy(c *gin.Context) {
	var req certificates.CopyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "INVALID_REQUEST",
				"message": fmt.Sprintf("Requisicao invalida: %v", err),
			},
		})
		return
	}

	if req.SourceCluster == "" || req.SourceNamespace == "" || req.SecretName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "MISSING_SOURCE",
				"message": "sourceCluster, sourceNamespace e secretName sao obrigatorios",
			},
		})
		return
	}

	if len(req.TargetClusters) == 0 || len(req.TargetNamespaces) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "MISSING_TARGET",
				"message": "Pelo menos um cluster e namespace destino devem ser especificados",
			},
		})
		return
	}

	log.Info().
		Str("source", fmt.Sprintf("%s/%s/%s", req.SourceCluster, req.SourceNamespace, req.SecretName)).
		Strs("targetClusters", req.TargetClusters).
		Strs("targetNamespaces", req.TargetNamespaces).
		Msg("Copiando certificado TLS")

	copied, err := h.scanner.CopyCertificate(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "COPY_ERROR",
				"message": fmt.Sprintf("Erro ao copiar certificado: %v", err),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": fmt.Sprintf("Certificado copiado para %d destino(s)", copied),
		"copied":  copied,
	})
}

// Upload faz upload de um certificado para clusters/namespaces
func (h *CertificatesHandler) Upload(c *gin.Context) {
	var req certificates.UploadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "INVALID_REQUEST",
				"message": fmt.Sprintf("Requisicao invalida: %v", err),
			},
		})
		return
	}

	if req.Name == "" || req.TLSCrt == "" || req.TLSKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "MISSING_DATA",
				"message": "name, tlsCrt e tlsKey sao obrigatorios",
			},
		})
		return
	}

	if len(req.TargetClusters) == 0 || len(req.TargetNamespaces) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "MISSING_TARGET",
				"message": "Pelo menos um cluster e namespace destino devem ser especificados",
			},
		})
		return
	}

	log.Info().
		Str("name", req.Name).
		Strs("targetClusters", req.TargetClusters).
		Strs("targetNamespaces", req.TargetNamespaces).
		Msg("Upload de certificado TLS")

	uploaded, err := h.scanner.UploadCertificate(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "UPLOAD_ERROR",
				"message": fmt.Sprintf("Erro ao fazer upload: %v", err),
			},
		})
		return
	}

	// Validação automática pós-instalação — informativa, nunca desfaz o upload já concluído
	// (validationErr só significa "não deu pra parsear o PEM pra validar", não "cert inválido" —
	// esse caso já teria sido barrado antes pelo ValidatePEM dentro de UploadCertificate).
	var validation *certificates.ChainValidationResult
	if v, verr := certificates.ValidateCertificateChain([]byte(req.TLSCrt), []byte(req.TLSKey)); verr == nil {
		validation = v
	}

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"message":    fmt.Sprintf("Certificado uploaded para %d destino(s)", uploaded),
		"uploaded":   uploaded,
		"validation": validation,
	})
}

// ValidateChainRequest é o body de POST /certificates/validate-chain
type ValidateChainRequest struct {
	CertPEM string `json:"cert_pem"`
	KeyPEM  string `json:"key_pem"`
}

// ValidateChainPEM valida um par cert+key enviado no corpo da requisição — usado antes de
// instalar um certificado (validação ad-hoc, sem precisar já estar no cluster).
func (h *CertificatesHandler) ValidateChainPEM(c *gin.Context) {
	var req ValidateChainRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "INVALID_REQUEST", "message": fmt.Sprintf("Requisicao invalida: %v", err)},
		})
		return
	}
	if req.CertPEM == "" || req.KeyPEM == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "MISSING_DATA", "message": "cert_pem e key_pem sao obrigatorios"},
		})
		return
	}

	result, err := certificates.ValidateCertificateChain([]byte(req.CertPEM), []byte(req.KeyPEM))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   gin.H{"code": "VALIDATE_ERROR", "message": err.Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

// ValidateInstalledChain valida a cadeia de um certificado já instalado num Secret do cluster —
// cobre o disparo "manual" da validação (ex: botão "Validar Cadeia" no detalhe de um cert antigo).
func (h *CertificatesHandler) ValidateInstalledChain(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	name := c.Param("name")

	tlsCrt, tlsKey, err := h.scanner.GetRawTLSSecret(c.Request.Context(), cluster, namespace, name)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{
			"success": false,
			"error":   gin.H{"code": "SECRET_ERROR", "message": err.Error()},
		})
		return
	}

	result, err := certificates.ValidateCertificateChain(tlsCrt, tlsKey)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   gin.H{"code": "VALIDATE_ERROR", "message": err.Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

// Report gera relatório de certificados em formato Markdown
func (h *CertificatesHandler) Report(c *gin.Context) {
	clustersParam := c.Query("clusters")
	filter := c.QueryArray("filter")
	statusFilter := c.QueryArray("status")

	if clustersParam == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "MISSING_CLUSTERS",
				"message": "Parameter 'clusters' e obrigatorio",
			},
		})
		return
	}

	clusters := strings.Split(clustersParam, ",")
	filterType := "all"
	if len(filter) > 0 {
		filterType = filter[0]
	}

	result, err := h.scanner.ScanClusters(c.Request.Context(), certificates.ScanRequest{
		Clusters: clusters,
		Filter:   filterType,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "REPORT_ERROR",
				"message": fmt.Sprintf("Erro ao gerar relatorio: %v", err),
			},
		})
		return
	}

	// Filtrar por status se especificado
	if len(statusFilter) > 0 {
		statusSet := make(map[string]bool)
		for _, s := range statusFilter {
			statusSet[s] = true
		}
		var filtered []certificates.CertificateInfo
		for _, cert := range result.Certificates {
			if statusSet[cert.Status] {
				filtered = append(filtered, cert)
			}
		}
		result.Certificates = filtered
	}

	// Gerar Markdown
	md := generateMarkdownReport(result, clusters)

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"data":     result,
		"markdown": md,
	})
}

// generateMarkdownReport gera relatório em Markdown
func generateMarkdownReport(result *certificates.ScanResult, clusters []string) string {
	var sb strings.Builder

	sb.WriteString("# Relatorio de Certificados TLS\n\n")
	sb.WriteString(fmt.Sprintf("Data: %s\n\n", result.ScannedAt.Format("02/01/2006 15:04:05")))
	sb.WriteString(fmt.Sprintf("Clusters: %s\n\n", strings.Join(clusters, ", ")))

	// Resumo
	sb.WriteString("## Resumo\n\n")
	sb.WriteString(fmt.Sprintf("| Status | Quantidade |\n"))
	sb.WriteString(fmt.Sprintf("|--------|------------|\n"))
	sb.WriteString(fmt.Sprintf("| Validos | %d |\n", result.Summary.Valid))
	sb.WriteString(fmt.Sprintf("| Expirando (<30d) | %d |\n", result.Summary.Expiring))
	sb.WriteString(fmt.Sprintf("| Expirados | %d |\n", result.Summary.Expired))
	sb.WriteString(fmt.Sprintf("| **Total** | **%d** |\n\n", result.Summary.Total))

	// Alertas
	if result.Summary.Expired > 0 || result.Summary.Expiring > 0 {
		sb.WriteString("## Alertas\n\n")
		for _, cert := range result.Certificates {
			if cert.Status == "expired" {
				sb.WriteString(fmt.Sprintf("- [EXPIRADO] %s/%s (%s) - Expirou em %s (%d dias atras)\n",
					cert.Namespace, cert.SecretName, cert.Cluster,
					cert.NotAfter.Format("02/01/2006"), -cert.DaysRemaining))
			} else if cert.Status == "expiring" {
				sb.WriteString(fmt.Sprintf("- [EXPIRANDO] %s/%s (%s) - Expira em %s (%d dias restantes)\n",
					cert.Namespace, cert.SecretName, cert.Cluster,
					cert.NotAfter.Format("02/01/2006"), cert.DaysRemaining))
			}
		}
		sb.WriteString("\n")
	}

	// Tabela detalhada
	sb.WriteString("## Certificados\n\n")
	sb.WriteString("| Status | Secret | Namespace | Cluster | Subject | Expira Em | Dias | Ingresses |\n")
	sb.WriteString("|--------|--------|-----------|---------|---------|-----------|------|----------|\n")

	for _, cert := range result.Certificates {
		statusIcon := "Valido"
		if cert.Status == "expiring" {
			statusIcon = "Expirando"
		} else if cert.Status == "expired" {
			statusIcon = "Expirado"
		}

		ingresses := "-"
		if len(cert.UsedByIngresses) > 0 {
			var names []string
			for _, ref := range cert.UsedByIngresses {
				names = append(names, ref.Name)
			}
			ingresses = strings.Join(names, ", ")
		}

		sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s | %s | %d | %s |\n",
			statusIcon, cert.SecretName, cert.Namespace, cert.Cluster,
			cert.Subject, cert.NotAfter.Format("02/01/2006"),
			cert.DaysRemaining, ingresses))
	}

	sb.WriteString(fmt.Sprintf("\n---\nGerado em %s\n", time.Now().Format("02/01/2006 15:04:05")))

	return sb.String()
}
