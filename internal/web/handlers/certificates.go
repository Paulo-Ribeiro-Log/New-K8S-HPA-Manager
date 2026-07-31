package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
	corev1 "k8s.io/api/core/v1"

	"k8s-hpa-manager/internal/certificates"
	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/history"
)

// CertificatesHandler gerencia as rotas de certificados TLS
type CertificatesHandler struct {
	scanner           *certificates.Scanner
	rollbackStore     *certificates.RollbackStore     // pode ser nil — backup/rollback fica indisponível nesse caso
	manualBackupStore *certificates.ManualBackupStore // pode ser nil — mecanismo separado, ver manual_backup.go
	historyTracker    *history.HistoryTracker
}

// NewCertificatesHandler cria um handler de certificados. Se a criação de algum dos stores falhar
// (ex: erro ao resolver o home dir), o handler segue funcionando normalmente — só as rotas
// correspondentes ficam indisponíveis, mesmo espírito "melhor esforço" já usado no enriquecimento
// OpenSSL da Fase 1.
func NewCertificatesHandler(km *config.KubeConfigManager, historyTracker *history.HistoryTracker) *CertificatesHandler {
	rollbackStore, err := certificates.NewRollbackStore()
	if err != nil {
		log.Warn().Err(err).Msg("Erro ao inicializar RollbackStore de certificados — backup/rollback ficará indisponível")
		rollbackStore = nil
	}

	manualBackupStore, err := certificates.NewManualBackupStore()
	if err != nil {
		log.Warn().Err(err).Msg("Erro ao inicializar ManualBackupStore de certificados — backup manual ficará indisponível")
		manualBackupStore = nil
	}

	return &CertificatesHandler{
		scanner:           certificates.NewScanner(km, rollbackStore),
		rollbackStore:     rollbackStore,
		manualBackupStore: manualBackupStore,
		historyTracker:    historyTracker,
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
		// Fase 3 — só faz sentido quando há exatamente 1 cluster/namespace destino: com múltiplos
		// destinos (batch upload) não há um único cluster/namespace pra consultar no Prometheus.
		if len(req.TargetClusters) == 1 && len(req.TargetNamespaces) == 1 {
			v.LivePropagation = h.enrichLivePropagation(req.TargetClusters[0], req.TargetNamespaces[0], req.Name, []byte(req.TLSCrt))
		}
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
	result.LivePropagation = h.enrichLivePropagation(cluster, namespace, name, tlsCrt)

	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

// CheckBackendTLS é o "Diagnóstico Avançado" (Fase 8, backend_tls_check.go) — disparo manual,
// sob demanda: heurístico, mais custoso que validate-chain (lê logs de todos os pods do
// ingress-controller), por isso nunca roda automaticamente. Resolve os hosts do Secret via
// ResolveHostsForSecret (mesma função já usada pelo fallback TLS-dial da Fase 7) e delega a
// checagem em si pro Scanner.
func (h *CertificatesHandler) CheckBackendTLS(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	name := c.Param("name")

	ctx := c.Request.Context()

	hosts, err := h.scanner.ResolveHostsForSecret(ctx, cluster, namespace, name)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data": certificates.BackendTLSCheckResult{
				Checked: false,
				Notes:   []string{fmt.Sprintf("não foi possível resolver hosts para este Secret: %s", err.Error())},
			},
		})
		return
	}
	if len(hosts) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data": certificates.BackendTLSCheckResult{
				Checked: false,
				Notes:   []string{"nenhum host (Ingress/Gateway API) encontrado para este Secret"},
			},
		})
		return
	}

	result := h.scanner.CheckIngressBackendTLS(ctx, cluster, hosts)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

// tlsDialEnrichTimeout limita quanto tempo o fallback de handshake TLS direto (resolução de hosts
// + dial em paralelo) espera, mesmo teto de prometheusEnrichTimeout (prometheus_enrich.go).
const tlsDialEnrichTimeout = 8 * time.Second

// enrichLivePropagation é o ponto único de integração da Fase 3/4 (CERT-ROLLBACK-VALIDATION-PLAN.md)
// — resolve o serial decimal do leaf cert e tenta checar a propagação real do certificado por dois
// mecanismos, nessa ordem: (1) Prometheus/ingress-nginx (EnrichWithPrometheus, mais rico quando
// disponível — dados por pod individual, mais barato); (2) se o primeiro não conseguiu checar
// (cluster sem ingress-nginx, ex: já migrado para Gateway API), cai para um handshake TLS direto
// contra os hosts (Ingress/Gateway API) que servem o Secret — universal, funciona independente de
// quem termina o TLS. Melhor-esforço: se o PEM não puder ser parseado, retorna nil (o restante da
// validação já teria capturado esse erro).
func (h *CertificatesHandler) enrichLivePropagation(cluster, namespace, secretName string, certPEM []byte) *certificates.LivePropagationResult {
	serialDecimal, err := certificates.LeafSerialDecimal(certPEM)
	if err != nil {
		return nil
	}

	promResult := certificates.EnrichWithPrometheus(cluster, namespace, secretName, serialDecimal)
	if promResult.Checked {
		return promResult
	}

	ctx, cancel := context.WithTimeout(context.Background(), tlsDialEnrichTimeout)
	defer cancel()

	hosts, hostErr := h.scanner.ResolveHostsForSecret(ctx, cluster, namespace, secretName)
	if hostErr != nil || len(hosts) == 0 {
		notes := append([]string{}, promResult.Notes...)
		if hostErr != nil {
			notes = append(notes, fmt.Sprintf("handshake TLS direto também não pôde ser tentado: %s", hostErr.Error()))
		} else {
			notes = append(notes, "handshake TLS direto também não pôde ser tentado: nenhum host (Ingress/Gateway API) encontrado para este Secret")
		}
		return &certificates.LivePropagationResult{Checked: false, Notes: notes}
	}

	return certificates.EnrichWithTLSDial(ctx, hosts, serialDecimal)
}

// Backup salva o conteúdo ATUAL de um Secret TLS já instalado, sem sobrescrever nada — usado pelo
// caminho AWX (Fase 2), já que a renovação via playbook Ansible acontece fora do controle deste
// backend e por isso precisa desse gatilho explícito ANTES do job rodar (diferente do upload
// manual, que já dispara o backup sozinho dentro de Scanner.UploadCertificate).
func (h *CertificatesHandler) Backup(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	name := c.Param("name")

	if h.rollbackStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"error":   gin.H{"code": "ROLLBACK_UNAVAILABLE", "message": "Rollback store indisponivel neste servidor"},
		})
		return
	}

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

	secret := &corev1.Secret{Data: map[string][]byte{"tls.crt": tlsCrt, "tls.key": tlsKey}}
	info, err := h.rollbackStore.Backup(cluster, namespace, name, secret)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   gin.H{"code": "BACKUP_ERROR", "message": err.Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": info})
}

// ListRollbacks lista os backups disponíveis para um Secret — filtra por cluster/namespace porque
// RollbackStore.List indexa só por secretName; sem esse filtro, secrets homônimos em
// clusters/namespaces diferentes (ex: "viavarejo-tls" em 2 clusters distintos) apareceriam
// misturados na mesma lista.
func (h *CertificatesHandler) ListRollbacks(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	name := c.Param("name")

	if h.rollbackStore == nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": []certificates.RollbackBackupInfo{}})
		return
	}

	list, err := h.rollbackStore.List(name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   gin.H{"code": "LIST_ERROR", "message": err.Error()},
		})
		return
	}

	filtered := make([]certificates.RollbackBackupInfo, 0, len(list))
	for _, b := range list {
		if b.Cluster == cluster && b.Namespace == namespace {
			filtered = append(filtered, b)
		}
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": filtered})
}

// RollbackRequest é o body de POST /certificates/:cluster/:namespace/:name/rollback
type RollbackRequest struct {
	BackupID string `json:"backup_id"`
}

// Rollback restaura um backup anterior por cima do Secret atual. Reaproveita
// Scanner.UploadCertificate para o próprio ato de restaurar — como UploadCertificate já faz backup
// do estado atual antes de sobrescrever (Fase 2), um rollback também vira reversível de graça, sem
// precisar duplicar a chamada de backup aqui.
func (h *CertificatesHandler) Rollback(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	name := c.Param("name")

	if h.rollbackStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"error":   gin.H{"code": "ROLLBACK_UNAVAILABLE", "message": "Rollback store indisponivel neste servidor"},
		})
		return
	}

	var req RollbackRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.BackupID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "INVALID_REQUEST", "message": "backup_id e obrigatorio"},
		})
		return
	}

	tlsCrt, tlsKey, meta, err := h.rollbackStore.Get(name, req.BackupID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   gin.H{"code": "BACKUP_NOT_FOUND", "message": err.Error()},
		})
		return
	}
	if meta.Cluster != cluster || meta.Namespace != namespace {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "BACKUP_MISMATCH", "message": "backup pertence a outro cluster/namespace"},
		})
		return
	}

	ctx := c.Request.Context()

	// Checagem de sanidade sobre o cert do backup — não bloqueia: o backup pode ser de um cert
	// que já tinha problema, quem decide se aceita mesmo assim é o usuário.
	validation, _ := certificates.ValidateCertificateChain(tlsCrt, tlsKey)

	// Estado atual, só pro "before" da auditoria — o backup de fato do estado atual acontece
	// automaticamente dentro de UploadCertificate (backupBeforeOverwrite) logo abaixo.
	var before map[string]interface{}
	if currentInfo, cerr := h.scanner.GetCertificateDetails(ctx, cluster, namespace, name); cerr == nil {
		before = map[string]interface{}{
			"subject":       currentInfo.Subject,
			"serial_number": currentInfo.SerialNumber,
			"not_after":     currentInfo.NotAfter,
		}
	}

	uploaded, err := h.scanner.UploadCertificate(ctx, certificates.UploadRequest{
		Name:             name,
		TLSCrt:           string(tlsCrt),
		TLSKey:           string(tlsKey),
		TargetClusters:   []string{cluster},
		TargetNamespaces: []string{namespace},
	})
	if err != nil || uploaded == 0 {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   gin.H{"code": "ROLLBACK_ERROR", "message": fmt.Sprintf("erro ao restaurar backup: %v", err)},
		})
		return
	}
	if validation != nil {
		// Só depois de escrito de fato — antes disso o ingress-nginx ainda estaria servindo o
		// cert anterior, e comparar contra ele daria um resultado enganoso.
		validation.LivePropagation = h.enrichLivePropagation(cluster, namespace, name, tlsCrt)
	}

	after := map[string]interface{}{
		"subject":            meta.Subject,
		"serial_number":      meta.SerialNumber,
		"not_after":          meta.NotAfter,
		"restored_backup_id": meta.BackupID,
	}
	entry := CreateHistoryEntry(c, "cert-rollback", fmt.Sprintf("%s/%s", namespace, name), cluster, "success", before, after, 0, "")
	if err := h.historyTracker.Log(entry); err != nil {
		log.Warn().Err(err).Msg("erro ao registrar rollback de certificado no history tracker")
	}

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"message":    fmt.Sprintf("Certificado restaurado a partir do backup %s", meta.BackupID),
		"validation": validation,
	})
}

// GetRollbackContent devolve o PEM bruto (tls.crt/tls.key) de um backup do RollbackStore — usado
// pelo seletor de "instalar a partir de um backup" no fluxo manual, pra pré-popular os campos de
// texto sem o usuário precisar copiar/colar. Expõe chave privada — RBAC obrigatório.
func (h *CertificatesHandler) GetRollbackContent(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	name := c.Param("name")
	backupID := c.Param("backupId")

	if h.rollbackStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"error":   gin.H{"code": "ROLLBACK_UNAVAILABLE", "message": "Rollback store indisponivel neste servidor"},
		})
		return
	}

	tlsCrt, tlsKey, meta, err := h.rollbackStore.Get(name, backupID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   gin.H{"code": "BACKUP_NOT_FOUND", "message": err.Error()},
		})
		return
	}
	if meta.Cluster != cluster || meta.Namespace != namespace {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "BACKUP_MISMATCH", "message": "backup pertence a outro cluster/namespace"},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"tls_crt": string(tlsCrt), "tls_key": string(tlsKey)},
	})
}

// ManualBackupRequest é o body de POST /certificates/:cluster/:namespace/:name/manual-backup
type ManualBackupRequest struct {
	Comment string `json:"comment,omitempty"`
}

// SaveManualBackup salva o conteúdo ATUAL de um Secret TLS num backup separado, disparado sob
// demanda pelo usuário (mecanismo distinto do RollbackStore, que só guarda o estado anterior
// automaticamente antes de uma sobrescrita). Aceita comentário opcional.
func (h *CertificatesHandler) SaveManualBackup(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	name := c.Param("name")

	if h.manualBackupStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"error":   gin.H{"code": "MANUAL_BACKUP_UNAVAILABLE", "message": "Backup manual indisponivel neste servidor"},
		})
		return
	}

	var req ManualBackupRequest
	_ = c.ShouldBindJSON(&req) // comentário é opcional — body ausente/vazio não é erro

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

	secret := &corev1.Secret{Data: map[string][]byte{"tls.crt": tlsCrt, "tls.key": tlsKey}}
	info, err := h.manualBackupStore.Save(cluster, namespace, name, req.Comment, secret)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   gin.H{"code": "MANUAL_BACKUP_ERROR", "message": err.Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": info})
}

// ListManualBackupSecrets lista os nomes de secret que têm ao menos 1 backup manual — alimenta a
// navegação "entre pastas" do seletor de instalação manual, que não fica restrita ao secret
// atualmente selecionado.
func (h *CertificatesHandler) ListManualBackupSecrets(c *gin.Context) {
	if h.manualBackupStore == nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": []string{}})
		return
	}

	secrets, err := h.manualBackupStore.ListSecretsWithBackups()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   gin.H{"code": "LIST_ERROR", "message": err.Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": secrets})
}

// ListManualBackups lista os backups manuais de um secret específico (metadata apenas, sem chave).
func (h *CertificatesHandler) ListManualBackups(c *gin.Context) {
	secretName := c.Param("secretName")

	if h.manualBackupStore == nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": []certificates.ManualBackupInfo{}})
		return
	}

	list, err := h.manualBackupStore.List(secretName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   gin.H{"code": "LIST_ERROR", "message": err.Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": list})
}

// GetManualBackupContent devolve o PEM bruto de um backup manual — expõe chave privada, RBAC
// obrigatório. Diferente de GetRollbackContent, não valida cluster/namespace (o backup manual é
// deliberadamente navegável entre secrets de qualquer cluster/namespace).
func (h *CertificatesHandler) GetManualBackupContent(c *gin.Context) {
	secretName := c.Param("secretName")
	backupID := c.Param("backupId")

	if h.manualBackupStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"error":   gin.H{"code": "MANUAL_BACKUP_UNAVAILABLE", "message": "Backup manual indisponivel neste servidor"},
		})
		return
	}

	tlsCrt, tlsKey, _, err := h.manualBackupStore.Get(secretName, backupID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   gin.H{"code": "BACKUP_NOT_FOUND", "message": err.Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"tls_crt": string(tlsCrt), "tls_key": string(tlsKey)},
	})
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
