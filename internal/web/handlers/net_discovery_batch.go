package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ─── "Descoberta de Rede" — Fase 5, item P4 do roadmap de maturidade profissional
// (IP-ROUTE-DISCOVERY-PLAN.md, seção 10): múltiplos alvos em lote. Maior risco arquitetural do
// roadmap — decisão de design registrada aqui e no plano: fila SEQUENCIAL (não paralela),
// reaproveitando `runDiscovery` (net_discovery.go) SEM MODIFICAR NADA nele — cada alvo do lote é
// literalmente uma execução single-target normal, só que N delas em sequência dentro da MESMA
// goroutine, cada uma com seu próprio session_id (o frontend caminha pelos streams SSE um de cada
// vez, exatamente como já faz pra uma descoberta única). Motivos pra sequencial em vez de
// paralelo: (1) zero risco na lógica já validada — nenhuma race condition nova pra descobrir;
// (2) evita tempestade de rede/DNS/az-CLI simultânea contra N alvos de uma vez; (3) Histórico
// (Fase 5/P1) e Exportar PDF (Fase 5/P2) funcionam de graça pra cada alvo do lote, sem nenhum
// código extra — cada iteração já passa pelo mesmo `saveDiscoveryHistory` que uma busca única usa.

// netDiscoveryBatchMaxTargets — teto de alvos por lote. Cada alvo pode levar até
// computeOverallTimeout(probeTimeoutSec) no pior caso (tudo bloqueado) — um lote sem teto poderia
// levar horas se o usuário colar uma lista enorme de alvos genuinamente inalcançáveis. 10 alvos no
// timeout máximo (8s/salto) já soma ~45min de pior caso absoluto — teto generoso o bastante pro
// uso real (investigação de um punhado de hosts relacionados) sem abrir a porta pra abuso.
const netDiscoveryBatchMaxTargets = 10

// netDiscoveryMonitorIntervalMaxSec — Fase C do roadmap de maturidade profissional (modo de
// monitoramento contínuo, ver AllowDuplicateTargets/IntervalSec abaixo): teto de segundos de
// espera entre uma rodada e a próxima. 60s é generoso o bastante pra espaçar rodadas de
// monitoramento sem deixar o lote inteiro esperar por tempo morto desproporcional.
const netDiscoveryMonitorIntervalMaxSec = 60

// netDiscoveryBatchOverallTimeoutCap — teto absoluto pro LOTE inteiro, independente da soma dos
// tetos individuais — nunca deixa o contexto Go esperar indefinidamente mesmo no cenário mais
// pessimista (todos os alvos no timeout de sonda máximo, todos genuinamente bloqueados).
//
// Achado real de code review: esta era uma constante FIXA (30min) — menor que o próprio pior caso
// já documentado no comentário de netDiscoveryBatchMaxTargets acima ("10 alvos no timeout máximo
// (8s/salto) já soma ~45min"). Com 10 alvos genuinamente bloqueados no timeout de sonda máximo, o
// contexto compartilhado do lote expirava ANTES de todos rodarem — e o loop em RunBatch (que
// checa `ctx.Err()` antes de cada iteração) simplesmente RETORNAVA sem nenhum evento SSE pros
// alvos restantes, deixando o frontend esperando indefinidamente um stream que nunca chegava a
// abrir, sem nenhum erro visível. Corrigido: calculado a partir da MESMA fórmula que o comentário
// de netDiscoveryBatchMaxTargets já usa (número máximo de alvos × pior caso individual no timeout
// de sonda máximo) em vez de reafirmado como número fixo — nunca mais pode divergir
// silenciosamente do que o comentário promete, porque é literalmente a mesma conta.
var netDiscoveryBatchOverallTimeoutCap = time.Duration(netDiscoveryBatchMaxTargets) * computeOverallTimeout(netDiscoveryProbeTimeoutMaxSec, netDiscoveryProbeCountMax)

// RunNetDiscoveryBatchRequest é o body do POST /run-batch. Configurações de sonda (porta/timeout)
// e de execução (modo/cluster/namespace) são COMPARTILHADAS por todo o lote — v1 deliberadamente
// simples, sem permitir configuração por-alvo (adicionar depois se o uso real pedir).
type RunNetDiscoveryBatchRequest struct {
	Targets         []string `json:"targets"`
	Mode            string   `json:"mode"`
	Cluster         string   `json:"cluster,omitempty"`
	Namespace       string   `json:"namespace,omitempty"`
	ProbePort       int      `json:"probe_port,omitempty"`
	ProbeTimeoutSec int      `json:"probe_timeout_sec,omitempty"`
	// ProbeCount — Fase A do roadmap de maturidade profissional (múltiplas sondas por salto), mesmo
	// campo/validação de RunNetDiscoveryRequest.ProbeCount (net_discovery.go), compartilhado por
	// todo o lote.
	ProbeCount int `json:"probe_count,omitempty"`
	// ClientCertPEM/ClientKeyPEM — mTLS opcional (ver RunNetDiscoveryRequest em net_discovery.go),
	// mesmo padrão de configuração compartilhada por todo o lote das demais opções acima. Validado
	// UMA vez em RunBatch (não por alvo) — o mesmo par é reaproveitado em cada iteração da fila.
	ClientCertPEM string `json:"client_cert_pem,omitempty"`
	ClientKeyPEM  string `json:"client_key_pem,omitempty"`
	// ExtraPorts — Fase D do roadmap de maturidade profissional, mesmo campo/validação de
	// RunNetDiscoveryRequest.ExtraPorts (net_discovery.go), compartilhado por todo o lote.
	ExtraPorts []int `json:"extra_ports,omitempty"`
	// AllowDuplicateTargets — Fase C do roadmap de maturidade profissional (modo de monitoramento
	// contínuo): quando true, normalizeBatchTargets NÃO deduplica — permite o mesmo alvo aparecer
	// N vezes na lista, viabilizando "rodar o mesmo alvo repetidas vezes em sequência" reaproveitando
	// o mecanismo de lote já existente sem endpoint novo ("monitorar" é literalmente "lote com o
	// mesmo alvo repetido N vezes"). Default false preserva 100% o comportamento normal do lote
	// (alvos distintos, sem repetição, dedupe ligado).
	AllowDuplicateTargets bool `json:"allow_duplicate_targets,omitempty"`
	// IntervalSec — segundos de espera entre uma rodada e a próxima (0 = sem espera, rodadas
	// imediatamente sequenciais — mesmo comportamento do lote normal). Útil pra detectar
	// flakiness que não é constante (ex: "1 rodada a cada 10s" em vez de rajada). A espera é
	// guardada por ctx.Done() — cancelar o lote também interrompe a espera entre rodadas
	// imediatamente, sem esperar o intervalo terminar.
	IntervalSec int `json:"interval_sec,omitempty"`
}

// RunNetDiscoveryBatchResponse — `SessionIDs`/`Targets` sempre na MESMA ordem e mesmo tamanho; o
// frontend caminha pelos streams SSE nessa ordem (índice 0, depois 1, ...).
type RunNetDiscoveryBatchResponse struct {
	BatchID    string   `json:"batch_id"`
	SessionIDs []string `json:"session_ids"`
	Targets    []string `json:"targets"` // eco já normalizado (trim, dedupe) — frontend não precisa recalcular
}

// normalizeBatchTargets — trim + remove vazios + dedupe (case-insensitive, a menos que
// allowDuplicates seja true) preservando a ordem de entrada, mesma normalização usada no Histórico
// (Fase 5/P1) pra não tratar "Foo.com" e "foo.com" como alvos diferentes dentro do mesmo lote.
// Extraída como função pura pra ser testável sem precisar de um `*gin.Context` (mesmo padrão de
// `parseAzureServiceTagsDoc`, Fase 5/P3). `allowDuplicates` — Fase C (modo de monitoramento
// contínuo): pula a etapa de dedupe, permitindo o mesmo alvo repetido N vezes na lista.
func normalizeBatchTargets(raw []string, allowDuplicates bool) []string {
	seen := make(map[string]struct{}, len(raw))
	targets := make([]string, 0, len(raw))
	for _, t := range raw {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if !allowDuplicates {
			key := strings.ToLower(t)
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
		}
		targets = append(targets, t)
	}
	return targets
}

// normalizeMonitorInterval valida/normaliza IntervalSec (0 = sem espera entre rodadas, sempre
// válido) — Fase C do roadmap de maturidade profissional.
func normalizeMonitorInterval(intervalSec int) (sec int, errCode, errMsg string) {
	if intervalSec < 0 || intervalSec > netDiscoveryMonitorIntervalMaxSec {
		return 0, "INVALID_INTERVAL", fmt.Sprintf("interval_sec deve estar entre 0 e %d", netDiscoveryMonitorIntervalMaxSec)
	}
	return intervalSec, "", ""
}

// RunBatch inicia um lote de descobertas sequenciais.
// POST /api/v1/net-discovery/run-batch
func (h *NetDiscoveryHandler) RunBatch(c *gin.Context) {
	var req RunNetDiscoveryBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", err.Error()))
		return
	}

	targets := normalizeBatchTargets(req.Targets, req.AllowDuplicateTargets)
	if len(targets) == 0 {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMS", "informe ao menos 1 alvo (IP ou hostname)"))
		return
	}
	if len(targets) > netDiscoveryBatchMaxTargets {
		c.JSON(http.StatusBadRequest, errorResponse("TOO_MANY_TARGETS",
			fmt.Sprintf("máximo de %d alvos por lote", netDiscoveryBatchMaxTargets)))
		return
	}

	req.Mode = strings.ToLower(strings.TrimSpace(req.Mode))
	if req.Mode != netDiscoveryModePod && req.Mode != netDiscoveryModeLocal {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_MODE", "mode deve ser 'pod' ou 'local'"))
		return
	}
	if req.Mode == netDiscoveryModePod && (req.Cluster == "" || req.Namespace == "") {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMS", "cluster e namespace são obrigatórios no modo pod"))
		return
	}
	probePort, probeTimeoutSec, probeCount, errCode, errMsg := normalizeProbeSettings(req.ProbePort, req.ProbeTimeoutSec, req.ProbeCount)
	if errCode != "" {
		c.JSON(http.StatusBadRequest, errorResponse(errCode, errMsg))
		return
	}
	if errCode, errMsg := validateClientCertRequest(req.ClientCertPEM, req.ClientKeyPEM); errCode != "" {
		c.JSON(http.StatusBadRequest, errorResponse(errCode, errMsg))
		return
	}
	if errCode, errMsg := validateExtraPorts(req.ExtraPorts); errCode != "" {
		c.JSON(http.StatusBadRequest, errorResponse(errCode, errMsg))
		return
	}
	intervalSec, errCode, errMsg := normalizeMonitorInterval(req.IntervalSec)
	if errCode != "" {
		c.JSON(http.StatusBadRequest, errorResponse(errCode, errMsg))
		return
	}

	userInfo := GetUserInfoForHistory(c)

	// Mesmo lock "uma descoberta por vez por usuário" já existente pra busca única — um lote é,
	// pro propósito deste lock, uma única operação lógica; bloqueia tanto uma 2ª busca única
	// quanto um 2º lote enquanto este estiver rodando, e vice-versa.
	lockKey := userInfo.Email
	if lockKey == "" {
		lockKey = "unknown"
	}
	if _, alreadyRunning := h.runningUsers.LoadOrStore(lockKey, struct{}{}); alreadyRunning {
		c.JSON(http.StatusConflict, errorResponse("DISCOVERY_ALREADY_RUNNING",
			"você já tem uma descoberta de rede em andamento — aguarde terminar ou cancele antes de iniciar outra"))
		return
	}

	batchID := uuid.New().String()
	sessionIDs := make([]string, len(targets))
	for i := range sessionIDs {
		sessionIDs[i] = uuid.New().String()
	}

	// Teto do LOTE inteiro = soma do pior caso de cada alvo (mesma computeOverallTimeout já usada
	// pra busca única) + o tempo de espera entre rodadas (Fase C, IntervalSec — (N-1) intervalos
	// entre N rodadas), capado em netDiscoveryBatchOverallTimeoutCap. Um único cancelFunc
	// registrado sob o batchID (não um por sessão) — cancelar o lote cancela o alvo em andamento E
	// impede os alvos restantes de sequer começar (checado no loop abaixo); o endpoint Cancel()
	// já existente funciona sem nenhuma mudança, só chamado com batchID em vez de um sessionID.
	batchTimeout := time.Duration(len(targets))*computeOverallTimeout(probeTimeoutSec, probeCount) +
		time.Duration(len(targets)-1)*time.Duration(intervalSec)*time.Second
	if batchTimeout > netDiscoveryBatchOverallTimeoutCap {
		batchTimeout = netDiscoveryBatchOverallTimeoutCap
	}
	ctx, cancel := context.WithTimeout(context.Background(), batchTimeout)
	h.cancelFuncs.Store(batchID, cancel)

	go func() {
		defer h.cancelFuncs.Delete(batchID)
		defer h.runningUsers.Delete(lockKey)
		defer cancel()

		for i, target := range targets {
			if ctx.Err() != nil {
				return // lote cancelado (ou teto absoluto estourado) — não inicia os alvos restantes
			}
			// Fase C — espera entre rodadas (só a partir da 2ª, nunca antes da 1ª). Guardada por
			// ctx.Done() — cancelar o lote interrompe a espera imediatamente, sem esperar o
			// intervalo configurado terminar.
			if i > 0 && intervalSec > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Duration(intervalSec) * time.Second):
				}
			}
			subReq := RunNetDiscoveryRequest{
				Target: target, Mode: req.Mode, Cluster: req.Cluster, Namespace: req.Namespace,
				ProbePort: probePort, ProbeTimeoutSec: probeTimeoutSec, ProbeCount: probeCount,
				ClientCertPEM: req.ClientCertPEM, ClientKeyPEM: req.ClientKeyPEM,
				ExtraPorts: req.ExtraPorts,
			}
			// runDiscovery (net_discovery.go) roda INALTERADA — mesmo fluxo de uma busca única
			// (traceroute→fingerprint→enrich→crossref→histórico→SSE), incluindo seus próprios
			// fail()/send() internos por sessionIDs[i]. Bloqueante — a próxima iteração só começa
			// depois desta terminar (fila sequencial).
			h.runDiscovery(ctx, sessionIDs[i], subReq, userInfo)
		}
	}()

	c.JSON(http.StatusOK, RunNetDiscoveryBatchResponse{BatchID: batchID, SessionIDs: sessionIDs, Targets: targets})
}
