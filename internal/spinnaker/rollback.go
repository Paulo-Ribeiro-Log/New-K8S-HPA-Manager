package spinnaker

import (
	"fmt"
	"sort"
	"strings"
)

// RollbackInfo é o contrato de dados final (seção 5 do plano) — o que a Fase 2 (handler HTTP)
// devolve pro frontend. Campos de tempo em epoch ms (mesmo formato do Gate). Tags JSON já no
// formato do contrato — este struct é serializado quase direto pelo handler HTTP.
type RollbackInfo struct {
	Matched bool `json:"matched"` // achou alguma execução pra esse nameApp/namespace

	IsRollback   *bool  `json:"is_rollback"`             // nil = não determinado (nunca inferir false por omissão)
	RollbackType string `json:"rollback_type,omitempty"` // "explicit" | "implicit" | ""

	LastCHGApplied     string `json:"last_chg_applied,omitempty"`
	LastCHGAppliedURL  string `json:"last_chg_applied_url,omitempty"` // link direto pra CHG no ServiceNow (Trigger.CHGUrl)
	PipelineExecutedAt int64  `json:"pipeline_executed_at,omitempty"`
	ExecutionStatus    string `json:"execution_status,omitempty"`

	RollbackStartedAt    int64  `json:"rollback_started_at,omitempty"`
	RollbackEndedAt      int64  `json:"rollback_ended_at,omitempty"`
	FailedCHG            string `json:"failed_chg,omitempty"`
	FailedCHGURL         string `json:"failed_chg_url,omitempty"`         // link direto pra CHG no ServiceNow (Trigger.CHGUrl)
	RollbackPipelineName string `json:"rollback_pipeline_name,omitempty"` // só preenchido quando RollbackType == "explicit"

	SpinnakerExecutionID string `json:"spinnaker_execution_id,omitempty"`
	// SpinnakerExecutionURL é montada pelo handler HTTP (precisa da URL do Deck + projeto,
	// que este pacote não conhece) — deixado pra Fase 2 preencher antes de responder.
	SpinnakerExecutionURL string `json:"spinnaker_execution_url,omitempty"`

	// FromCache/CachedAt são preenchidos só pelo handler HTTP (nunca por DetectRollback) quando
	// o resultado veio do SpinnakerHistoryStore (persistência local) em vez de uma busca ao vivo
	// no Gate — achado real: `executions/search` só devolve as execuções dos últimos ~28 dias,
	// independente do "limit" pedido. Sem persistência, um deployment não redeployado há mais
	// tempo que isso perderia o dado assim que a janela do Gate rolasse pra frente, mesmo já
	// tendo sido confirmado numa consulta anterior.
	FromCache bool  `json:"from_cache,omitempty"`
	CachedAt  int64 `json:"cached_at,omitempty"` // epoch ms — última vez confirmado AO VIVO no Gate

	// RecentExecutions — seção 9 item 5 do plano: últimas execuções (mais recente primeiro,
	// até recentExecutionsLimit) desse mesmo nameApp/namespace, não só a que determinou o
	// resultado acima — útil pra ver o padrão recente (ex: "rollback seguido de deploy
	// corrigido") sem abrir o Spinnaker. Preenchido só quando Matched=true — sem execução
	// nenhuma não há nada pra listar. Não persistido no SpinnakerHistoryStore (diferente do
	// resto de RollbackInfo) — é dado auxiliar/exibição, não o "último status confirmado" que a
	// persistência existe pra preservar; FromCache=true nunca traz RecentExecutions.
	RecentExecutions []ExecutionSummary `json:"recent_executions,omitempty"`

	// Stages — detalhamento por etapa da execução que decidiu o resultado acima (mesma tabela
	// "Step / Started / Completed / Duration / Status" que a UI do Deck mostra em "Execution
	// Details"), pedido explícito do usuário depois de ver essa tela real do Spinnaker. Cada
	// etapa pode ter um status diferente do status geral da execução — ex: execução SUCCEEDED
	// com uma etapa SKIPPED, confirmado ao vivo. Não persistido no SpinnakerHistoryStore (mesmo
	// motivo de RecentExecutions — detalhe de exibição, não o "último status confirmado").
	Stages []StageSummary `json:"stages,omitempty"`
}

// ExecutionSummary é uma linha do histórico curto (RollbackInfo.RecentExecutions) — só os
// campos que a lista compacta do modal usa, não a Execution completa.
type ExecutionSummary struct {
	ExecutionID  string `json:"execution_id"`
	PipelineName string `json:"pipeline_name"`
	Status       string `json:"status"`
	ExecutedAt   int64  `json:"executed_at"`
	Version      string `json:"version,omitempty"`
	CHG          string `json:"chg,omitempty"`
	CHGUrl       string `json:"chg_url,omitempty"`
	IsRollback   bool   `json:"is_rollback"`
}

// StageSummary é uma etapa (stage) dentro de uma execução — ver comentário de RollbackInfo.Stages.
type StageSummary struct {
	Name        string `json:"name"`
	Status      string `json:"status"`
	StartedAt   int64  `json:"started_at,omitempty"`
	CompletedAt int64  `json:"completed_at,omitempty"`
}

// successStatuses — status de execução considerados sucesso (a versão-alvo dessa execução
// passa a ser candidata a "versão vigente"). Confirmado ao vivo: SUCCEEDED é o único visto em
// deploys bem-sucedidos; os demais são inferidos da documentação pública do Gate.
var successStatuses = map[string]bool{
	"SUCCEEDED": true,
}

// rollbackNameHint — substring (case-insensitive) que classifica um pipeline como de rollback
// explícito por convenção de nome. "rollback-aks-global" (confirmado ao vivo, seção 2 do plano)
// bate com esse padrão; deixado genérico ("rollback-") de propósito — item 2 da seção 6 do
// plano ainda não confirmou se outras squads usam o mesmo nome exato, então checar só o
// prefixo é a escolha mais robusta disponível até validar contra uma 2ª squad.
const rollbackNameHint = "rollback"

// rollbackManifestHint — substring no manifestArtifact.reference de um stage que confirma
// (sinal secundário/corroborativo) que aquele stage rodou um Job de rollback, mesmo quando o
// pipeline não seguir a convenção de nome. Confirmado ao vivo: "helm-rollback.yaml".
const rollbackManifestHint = "helm-rollback.yaml"

// isExplicitRollbackExecution decide se uma execução é, pela sua identidade, uma execução de
// rollback deliberado (Fase PRD, seção 2 do plano) — não confunde com rollback implícito
// (HLG, seção 0.6), que não tem uma execução própria, é inferido por ausência de sucesso.
func isExplicitRollbackExecution(ex Execution) bool {
	if strings.Contains(strings.ToLower(ex.Name), rollbackNameHint) {
		return true
	}
	for _, stage := range ex.Stages {
		if strings.Contains(stage.ManifestReference(), rollbackManifestHint) {
			return true
		}
	}
	// Sinal explícito "Is Rollback"/"isRollback" (ver Trigger.IsRollbackFlag) — reforço pra
	// squads que não seguem a convenção de nome "rollback-*"/manifesto "helm-rollback.yaml"
	// (confirmado company-wide na Fase 4, mas o nome do pipeline em si continua sendo
	// convenção, não contrato — este campo é o dado mais direto disponível).
	return ex.Trigger.IsRollbackFlag()
}

// executionsForTarget filtra e ordena (mais recente primeiro) as execuções que correspondem a
// um nameApp+namespace específico dentro de uma application Spinnaker — necessário porque uma
// única application agrupa múltiplos microsserviços (seção 3/4 do plano).
func executionsForTarget(executions []Execution, nameApp, namespace string) []Execution {
	var matched []Execution
	for _, ex := range executions {
		if ex.Trigger.AppName() == nameApp && ex.Trigger.Namespace() == namespace {
			matched = append(matched, ex)
		}
	}
	sort.SliceStable(matched, func(i, j int) bool {
		return executionTime(matched[i]) > executionTime(matched[j])
	})
	return matched
}

// executionTime prioriza StartTime (mais preciso), caindo pra BuildTime quando ausente.
func executionTime(ex Execution) int64 {
	if ex.StartTime > 0 {
		return ex.StartTime
	}
	return ex.BuildTime
}

// recentExecutionsLimit — "últimas 3-5 execuções" (seção 9 item 5 do plano); 5 escolhido como
// teto (mostra tudo quando há menos).
const recentExecutionsLimit = 5

// buildRecentExecutions resume as N execuções mais recentes de matched (já ordenado desc por
// executionTime em executionsForTarget) pro histórico curto do modal — mesmo dado que
// SearchExecutions já trouxe, só não descartado depois de achar a execução que decide o
// resultado principal.
func buildRecentExecutions(matched []Execution) []ExecutionSummary {
	n := len(matched)
	if n > recentExecutionsLimit {
		n = recentExecutionsLimit
	}
	out := make([]ExecutionSummary, 0, n)
	for _, ex := range matched[:n] {
		out = append(out, ExecutionSummary{
			ExecutionID:  ex.ID,
			PipelineName: ex.Name,
			Status:       ex.Status,
			ExecutedAt:   executionTime(ex),
			Version:      ex.Trigger.Version(),
			CHG:          ex.Trigger.CHGNumber(),
			CHGUrl:       ex.Trigger.CHGUrl(),
			IsRollback:   isExplicitRollbackExecution(ex),
		})
	}
	return out
}

// buildStageSummary extrai o detalhamento por etapa (Step/Started/Completed/Duration/Status na
// UI do Deck) de uma execução — ver comentário de RollbackInfo.Stages. Preserva a ordem original
// do Gate (que já é a ordem de execução do pipeline).
func buildStageSummary(stages []Stage) []StageSummary {
	if len(stages) == 0 {
		return nil
	}
	out := make([]StageSummary, 0, len(stages))
	for _, s := range stages {
		out = append(out, StageSummary{
			Name:        s.Name,
			Status:      s.Status,
			StartedAt:   s.StartTime,
			CompletedAt: s.EndTime,
		})
	}
	return out
}

// DetectRollback aplica as duas regras da seção 0.6 do plano pra decidir se a versão
// atualmente vigente no K8s (currentLiveVersion, já coletada via DeploymentRegistry — esta
// função nunca fala com o Spinnaker nem com o K8s diretamente) chegou lá por rollback:
//
//   - Regra (a) explícita: existe, entre as execuções mais recentes desse nameApp/namespace,
//     uma execução de rollback (nome/manifest) cuja versão-alvo bate com a vigente — foi ela
//     que produziu o estado atual, deliberadamente (hoje, só confirmado em PRD).
//   - Regra (b) implícita: a execução mais recente falhou (status não-sucesso) e sua
//     versão-alvo NÃO bate com a vigente — o deploy não completou e a versão anterior nunca
//     foi substituída (hoje, o caso confirmado em HLG — ver seção 0.5/0.6 do plano).
//
// executions deve vir de SearchExecutions pra uma única application Spinnaker — pode conter
// execuções de vários nameApp/namespace misturados, filtragem é feita aqui.
func DetectRollback(executions []Execution, nameApp, namespace, currentLiveVersion string) *RollbackInfo {
	matched := executionsForTarget(executions, nameApp, namespace)
	if len(matched) == 0 {
		return &RollbackInfo{Matched: false}
	}
	recent := buildRecentExecutions(matched)

	// Regra (a) — explícita: procura uma execução de rollback cuja versão-alvo já é a vigente.
	for _, ex := range matched {
		if !isExplicitRollbackExecution(ex) {
			continue
		}
		if ex.Trigger.Version() != currentLiveVersion {
			continue
		}
		isTrue := true
		return &RollbackInfo{
			Matched:              true,
			IsRollback:           &isTrue,
			RollbackType:         "explicit",
			LastCHGApplied:       ex.Trigger.CHGNumber(),
			LastCHGAppliedURL:    ex.Trigger.CHGUrl(),
			PipelineExecutedAt:   executionTime(ex),
			ExecutionStatus:      ex.Status,
			RollbackStartedAt:    ex.StartTime,
			RollbackEndedAt:      ex.EndTime,
			FailedCHG:            ex.Trigger.CHGNumber(),
			FailedCHGURL:         ex.Trigger.CHGUrl(),
			RollbackPipelineName: ex.Name,
			SpinnakerExecutionID: ex.ID,
			RecentExecutions:     recent,
			Stages:               buildStageSummary(ex.Stages),
		}
	}

	latest := matched[0]

	// Regra (b) — implícita: a execução mais recente falhou e não é a versão vigente.
	if !successStatuses[latest.Status] && latest.Trigger.Version() != currentLiveVersion {
		isTrue := true
		return &RollbackInfo{
			Matched:              true,
			IsRollback:           &isTrue,
			RollbackType:         "implicit",
			LastCHGApplied:       latest.Trigger.CHGNumber(),
			LastCHGAppliedURL:    latest.Trigger.CHGUrl(),
			PipelineExecutedAt:   executionTime(latest),
			ExecutionStatus:      latest.Status,
			RollbackStartedAt:    latest.StartTime,
			RollbackEndedAt:      latest.EndTime,
			FailedCHG:            latest.Trigger.CHGNumber(),
			FailedCHGURL:         latest.Trigger.CHGUrl(),
			SpinnakerExecutionID: latest.ID,
			RecentExecutions:     recent,
			Stages:               buildStageSummary(latest.Stages),
		}
	}

	// A execução mais recente bate com a versão vigente — deploy normal, não é rollback.
	if latest.Trigger.Version() == currentLiveVersion {
		isFalse := false
		return &RollbackInfo{
			Matched:              true,
			IsRollback:           &isFalse,
			LastCHGApplied:       latest.Trigger.CHGNumber(),
			LastCHGAppliedURL:    latest.Trigger.CHGUrl(),
			PipelineExecutedAt:   executionTime(latest),
			ExecutionStatus:      latest.Status,
			SpinnakerExecutionID: latest.ID,
			RecentExecutions:     recent,
			Stages:               buildStageSummary(latest.Stages),
		}
	}

	// Nenhuma execução conhecida corresponde à versão vigente — não dá pra afirmar nada
	// (ex: versão vigente é mais antiga que a janela de execuções buscada). Fraseologia
	// neutra de propósito, mesmo padrão do resto da app (nunca inferir ausência por falta
	// de dado) — ver TrustedByPublicCA/ChainValidationResult no CLAUDE.md.
	return &RollbackInfo{Matched: false}
}

// String — implementação auxiliar de debug, não usada em produção.
func (r RollbackInfo) String() string {
	if !r.Matched {
		return "spinnaker: não determinado (nenhuma execução correspondente encontrada)"
	}
	if r.IsRollback == nil {
		return "spinnaker: não determinado"
	}
	if !*r.IsRollback {
		return fmt.Sprintf("spinnaker: deploy normal (CHG %s)", r.LastCHGApplied)
	}
	return fmt.Sprintf("spinnaker: rollback %s (CHG %s)", r.RollbackType, r.FailedCHG)
}
