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
	PipelineExecutedAt int64  `json:"pipeline_executed_at,omitempty"`
	ExecutionStatus    string `json:"execution_status,omitempty"`

	RollbackStartedAt    int64  `json:"rollback_started_at,omitempty"`
	RollbackEndedAt      int64  `json:"rollback_ended_at,omitempty"`
	FailedCHG            string `json:"failed_chg,omitempty"`
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
	return false
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
			PipelineExecutedAt:   executionTime(ex),
			ExecutionStatus:      ex.Status,
			RollbackStartedAt:    ex.StartTime,
			RollbackEndedAt:      ex.EndTime,
			FailedCHG:            ex.Trigger.CHGNumber(),
			RollbackPipelineName: ex.Name,
			SpinnakerExecutionID: ex.ID,
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
			PipelineExecutedAt:   executionTime(latest),
			ExecutionStatus:      latest.Status,
			RollbackStartedAt:    latest.StartTime,
			RollbackEndedAt:      latest.EndTime,
			FailedCHG:            latest.Trigger.CHGNumber(),
			SpinnakerExecutionID: latest.ID,
		}
	}

	// A execução mais recente bate com a versão vigente — deploy normal, não é rollback.
	if latest.Trigger.Version() == currentLiveVersion {
		isFalse := false
		return &RollbackInfo{
			Matched:              true,
			IsRollback:           &isFalse,
			LastCHGApplied:       latest.Trigger.CHGNumber(),
			PipelineExecutedAt:   executionTime(latest),
			ExecutionStatus:      latest.Status,
			SpinnakerExecutionID: latest.ID,
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
