package spinnaker

import (
	"fmt"
	"sort"
	"strings"
	"time"
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
	Version            string `json:"version,omitempty"` // versão-alvo (Trigger.Version()) da execução decisiva — pedido do usuário

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

	// PreviousVersion* — pedido do usuário: "qual a versão anterior da aplicação deployada antes
	// da execução da pipeline?". É a execução SUCCEEDED mais recente ANTES (cronologicamente) da
	// execução que decidiu o resultado principal acima — ou seja, a versão que estava rodando de
	// verdade no cluster até esse deploy substituí-la. Pula execuções falhas/skipped no meio
	// (nunca chegaram a ficar live, não são "a versão anterior" de fato). Vazio quando não há
	// nenhuma execução SUCCEEDED mais antiga dentro da janela de busca atual do Gate (achado real
	// documentado no handler HTTP: ~28 dias) — não confunda com "não existia versão anterior".
	PreviousVersion           string `json:"previous_version,omitempty"`
	PreviousVersionCHG        string `json:"previous_version_chg,omitempty"`
	PreviousVersionCHGURL     string `json:"previous_version_chg_url,omitempty"`
	PreviousVersionExecutedAt int64  `json:"previous_version_executed_at,omitempty"`

	// RegistryStale/RegistryLastSeen — achado real (usuário relatou "em hlg simplesmente não
	// funciona"): o Deployment Registry (fonte de currentLiveVersion, passado pra DetectRollback
	// pelo chamador) pode estar desatualizado — scan da aba GitHub Releases não rodou de novo
	// depois de um deploy novo. Nesse caso a versão comparada contra o Spinnaker está errada, e
	// DetectRollback cai no fallback neutro Matched:false — indistinguível de "sem sinal nenhum"
	// pro usuário (o badge simplesmente não aparece, sem nenhum aviso). DetectRollback em si não
	// sabe de registry (só recebe currentLiveVersion como string) — estes dois campos são
	// preenchidos pelo CHAMADOR (RolloutStatusBatch, internal/web/handlers/spinnaker.go) a partir
	// do DeploymentRecord, sempre, matched ou não. Nunca persistido no SpinnakerHistoryStore
	// (mesmo motivo de RecentExecutions/Stages — dado auxiliar de exibição, recalculado a cada
	// request, não "o último status confirmado" que a persistência existe pra preservar).
	RegistryStale    bool  `json:"registry_stale,omitempty"`
	RegistryLastSeen int64 `json:"registry_last_seen,omitempty"` // epoch ms — DeploymentRecord.LastSeen

	// LatestKnownExecutionAt — epoch ms da execução mais recente conhecida do Spinnaker pra esse
	// nameApp/namespace (executionTime(matched[0]), ver DetectRollback), preenchido em TODO
	// retorno onde `matched` (a lista interna de execuções filtradas) não é vazia — inclusive no
	// fallback final Matched:false ("nenhuma execução corresponde à versão vigente"). É a peça que
	// falta pro chamador decidir RegistryStale de forma precisa (ver applyRegistryFreshness,
	// internal/web/handlers/spinnaker.go): comparar contra "agora" (1ª tentativa desse mecanismo)
	// marcava como desatualizado QUALQUER deployment sem scan nas últimas 2h, mesmo os que
	// genuinamente não mudam há semanas — a maioria da frota, gerando ruído generalizado (achado
	// relatado ao vivo pelo usuário: "todos ficaram como dados desatualizados, até mesmo os que
	// conhecidamente foram executados hoje" — o inverso do que fazia sentido, já que justamente
	// esses deveriam continuar OK se o registry tivesse sido relido depois). Comparar contra a
	// execução mais recente CONHECIDA (não contra o relógio) só acende quando existe prova de que
	// o Spinnaker sabe de algo mais novo que a última leitura do registry — nunca por causa da
	// simples passagem do tempo.
	LatestKnownExecutionAt int64 `json:"-"` // uso interno do pacote handlers, não exposto na API

	// RecentStageFailures — ver comentário de StageFailureSummary. Preenchido em TODO retorno
	// onde matched não é vazio (mesmo padrão de LatestKnownExecutionAt) — é sinal complementar,
	// ortogonal ao resultado principal (Matched/IsRollback): "houve um problema real recente,
	// mesmo que autorresolvido", útil mesmo quando o resultado é "deploy normal" ou "não
	// determinado".
	RecentStageFailures []StageFailureSummary `json:"recent_stage_failures,omitempty"`
}

// StageFailureSummary é uma falha REAL (com log de exceção extraído por Stage.FailureLog) achada
// numa etapa de alguma das últimas execuções desse nameApp/namespace — mesmo quando a execução
// como um todo terminou SUCCEEDED. Achado real (usuário relatou "a pipeline teve erro e depois
// sucesso, mas não houveram sinais nem os logs das exceptions"): confirmado ao vivo contra
// entrega-mais-bff/entrega-mais-sit — 3 de 4 execuções recentes tinham o stage "deploy-helm" com
// status FAILED_CONTINUE (Kubernetes Job com BackoffLimitExceeded, container saindo com código 1),
// mas a execução como um todo terminava SUCCEEDED (o pipeline segue mesmo com essa etapa falha) —
// invisível pro resto do sistema porque (1) DetectRollback só olha ex.Status no nível da
// EXECUÇÃO, nunca por stage, e (2) mesmo quando os Stages de uma execução são expostos
// (buildStageSummary), isso só acontecia pra execução "decisiva" (a mais recente) — uma falha 2h
// atrás seguida de sucesso nunca tinha seus Stages processados. Ver StageFailureLogTruncateChars.
type StageFailureSummary struct {
	ExecutionID   string `json:"execution_id"`
	ExecutionTime int64  `json:"execution_time"`
	Version       string `json:"version,omitempty"`
	CHG           string `json:"chg,omitempty"`
	CHGUrl        string `json:"chg_url,omitempty"`
	StageName     string `json:"stage_name"`
	StageStatus   string `json:"stage_status"`
	Log           string `json:"log"`
	// ExecutionURL — mesmo padrão de RollbackInfo.SpinnakerExecutionURL: este pacote não conhece
	// a URL do Deck nem a application (precisa das duas pra montar o deep-link), preenchido pelo
	// chamador HTTP (internal/web/handlers/spinnaker.go/spinnaker_watcher.go) depois de
	// DetectRollback, nunca por findRecentStageFailures.
	ExecutionURL string `json:"execution_url,omitempty"`
}

// StageFailureRecentWindow — NÃO usado pra filtrar RecentStageFailures (ver histórico de idas e
// vindas abaixo) — usado só pelo chamador (SpinnakerFleetWatcher.notifyStageFailureIfNew,
// internal/web/handlers/spinnaker_watcher.go) pra decidir se uma falha de etapa é "recente o
// suficiente" pra JUSTIFICAR UMA NOTIFICAÇÃO — nunca pra decidir se ela aparece no badge/modal.
// Exportado (mesmo padrão de DeriveEnv) pra não duplicar a decisão de "o que conta como recente"
// com um número diferente do já usado em spinnakerRecentRollbackWindow
// (internal/healthcheck/spinnaker_enricher.go) — mesmo valor, 48h.
//
// Histórico: uma 1ª versão desta função filtrava RecentStageFailures por essa janela — motivada
// por um achado real (usuário apontou com print da própria app: uma falha de retry de 23/07
// aparecia rotulada "Falha recente em etapa" numa investigação em 21/08, quase um mês depois).
// Só que isso teve um efeito colateral não pedido: **removeu o indicador de atenção dos cards da
// lista de Deployments** pra qualquer deployment cuja última falha conhecida fosse mais antiga
// que 48h — usuário pediu explicitamente pra não tirar isso ("não era para retirar a indicação
// de atenção por erros de execução da pipeline dos cards"). Revertido: RecentStageFailures volta
// a mostrar TODO o histórico disponível (até recentExecutionsLimit execuções), sem filtro de
// tempo — o indicador nos cards continua aparecendo independente da idade. O problema real
// (rotular como "recente" algo de um mês atrás) é resolvido só na CAMADA DE APRESENTAÇÃO — o
// texto do chip/modal não afirma mais "recente" incondicionalmente (ver DeploymentsTab.tsx/
// SpinnakerRolloutModal.tsx), e cada item já carrega sua própria data (execution_time) pro
// usuário julgar a idade — e na notificação do watcher, que continua só disparando dentro desta
// janela (não faz sentido empurrar uma notificação sobre algo de um mês atrás).
const StageFailureRecentWindow = 48 * time.Hour

// findRecentStageFailures varre as recentExecutionsLimit execuções mais recentes de `matched`
// (já ordenado desc por tempo) procurando stages com FailureLog() não-vazio — reaproveita 100% o
// filtro já existente em Stage.FailureLog (só considera stage com Exception real, nunca history
// solta) sem duplicar lógica. Retorna nil quando nada é achado (sem "achado nenhum recorde" —
// omitempty no JSON já cobre isso). Sem filtro de tempo — ver comentário de StageFailureRecentWindow.
func findRecentStageFailures(matched []Execution) []StageFailureSummary {
	n := len(matched)
	if n > recentExecutionsLimit {
		n = recentExecutionsLimit
	}
	var out []StageFailureSummary
	for _, ex := range matched[:n] {
		for _, st := range ex.Stages {
			log := st.FailureLog()
			if log == "" {
				continue
			}
			out = append(out, StageFailureSummary{
				ExecutionID:   ex.ID,
				ExecutionTime: executionTime(ex),
				Version:       ex.Trigger.Version(),
				CHG:           ex.Trigger.CHGNumber(),
				CHGUrl:        ex.Trigger.CHGUrl(),
				StageName:     st.Name,
				StageStatus:   st.Status,
				Log:           log,
			})
		}
	}
	return out
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
	// Log — reconstrução legível da causa de falha (ver Stage.FailureLog), pedido do usuário
	// pra investigar execuções que falharam sem precisar abrir o Spinnaker. Vazio em etapas
	// bem-sucedidas/puladas — não é log bruto de pod, é a mesma informação de causa-raiz que a
	// UI do Deck usa pra montar "Execution Details".
	Log string `json:"log,omitempty"`
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
			Log:         s.FailureLog(),
		})
	}
	return out
}

// previousSuccessfulExecution acha, a partir de fromIndex (índice em matched — já ordenado desc
// por tempo em executionsForTarget) pra frente (índices maiores = execuções mais antigas), a
// primeira execução com status de sucesso E versão-alvo DIFERENTE de currentVersion — essa é a
// versão anterior de verdade, a última vez que o deployment rodou algo diferente do que roda
// hoje. Pula execuções falhas/canceladas/skipped no meio (nunca chegaram a ficar live) e também
// execuções SUCCEEDED que reimplantaram a MESMA versão (achado real, reportado pelo usuário: o
// pipeline pode ser re-executado pra mesma versão sob uma CHG diferente — ex: reprocessamento,
// mudança de infra sem bump de versão — e isso não é "a versão anterior", é a mesma versão de
// novo; mostrar como se fosse diferente é enganoso).
func previousSuccessfulExecution(matched []Execution, fromIndex int, currentVersion string) *Execution {
	for i := fromIndex + 1; i < len(matched); i++ {
		if !successStatuses[matched[i].Status] {
			continue
		}
		if matched[i].Trigger.Version() == currentVersion {
			continue
		}
		return &matched[i]
	}
	return nil
}

// applyPreviousVersion preenche os campos PreviousVersion* de info a partir da execução anterior
// bem-sucedida com versão diferente (ou não faz nada se não achar nenhuma dentro da janela de
// busca atual, ou se todo o histórico disponível for da mesma versão).
func applyPreviousVersion(info *RollbackInfo, matched []Execution, decisiveIndex int) {
	prev := previousSuccessfulExecution(matched, decisiveIndex, info.Version)
	if prev == nil {
		return
	}
	info.PreviousVersion = prev.Trigger.Version()
	info.PreviousVersionCHG = prev.Trigger.CHGNumber()
	info.PreviousVersionCHGURL = prev.Trigger.CHGUrl()
	info.PreviousVersionExecutedAt = executionTime(*prev)
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
	latestKnownExecutionAt := executionTime(matched[0]) // matched já ordenado desc por tempo
	recentStageFailures := findRecentStageFailures(matched)

	// Regra (a) — explícita: procura uma execução de rollback cuja versão-alvo já é a vigente.
	for i, ex := range matched {
		if !isExplicitRollbackExecution(ex) {
			continue
		}
		if ex.Trigger.Version() != currentLiveVersion {
			continue
		}
		isTrue := true
		info := &RollbackInfo{
			Matched:                true,
			IsRollback:             &isTrue,
			RollbackType:           "explicit",
			LastCHGApplied:         ex.Trigger.CHGNumber(),
			LastCHGAppliedURL:      ex.Trigger.CHGUrl(),
			PipelineExecutedAt:     executionTime(ex),
			ExecutionStatus:        ex.Status,
			Version:                ex.Trigger.Version(),
			RollbackStartedAt:      ex.StartTime,
			RollbackEndedAt:        ex.EndTime,
			FailedCHG:              ex.Trigger.CHGNumber(),
			FailedCHGURL:           ex.Trigger.CHGUrl(),
			RollbackPipelineName:   ex.Name,
			SpinnakerExecutionID:   ex.ID,
			RecentExecutions:       recent,
			Stages:                 buildStageSummary(ex.Stages),
			LatestKnownExecutionAt: latestKnownExecutionAt,
			RecentStageFailures:    recentStageFailures,
		}
		applyPreviousVersion(info, matched, i)
		return info
	}

	latest := matched[0]

	// Regra (b) — implícita: a execução mais recente falhou e não é a versão vigente.
	if !successStatuses[latest.Status] && latest.Trigger.Version() != currentLiveVersion {
		isTrue := true
		info := &RollbackInfo{
			Matched:                true,
			IsRollback:             &isTrue,
			RollbackType:           "implicit",
			LastCHGApplied:         latest.Trigger.CHGNumber(),
			LastCHGAppliedURL:      latest.Trigger.CHGUrl(),
			PipelineExecutedAt:     executionTime(latest),
			ExecutionStatus:        latest.Status,
			Version:                latest.Trigger.Version(),
			RollbackStartedAt:      latest.StartTime,
			RollbackEndedAt:        latest.EndTime,
			FailedCHG:              latest.Trigger.CHGNumber(),
			FailedCHGURL:           latest.Trigger.CHGUrl(),
			SpinnakerExecutionID:   latest.ID,
			RecentExecutions:       recent,
			Stages:                 buildStageSummary(latest.Stages),
			LatestKnownExecutionAt: latestKnownExecutionAt,
			RecentStageFailures:    recentStageFailures,
		}
		applyPreviousVersion(info, matched, 0)
		return info
	}

	// A execução mais recente bate com a versão vigente — deploy normal, não é rollback.
	if latest.Trigger.Version() == currentLiveVersion {
		isFalse := false
		info := &RollbackInfo{
			Matched:                true,
			IsRollback:             &isFalse,
			LastCHGApplied:         latest.Trigger.CHGNumber(),
			LastCHGAppliedURL:      latest.Trigger.CHGUrl(),
			PipelineExecutedAt:     executionTime(latest),
			ExecutionStatus:        latest.Status,
			Version:                latest.Trigger.Version(),
			SpinnakerExecutionID:   latest.ID,
			RecentExecutions:       recent,
			Stages:                 buildStageSummary(latest.Stages),
			LatestKnownExecutionAt: latestKnownExecutionAt,
			RecentStageFailures:    recentStageFailures,
		}
		applyPreviousVersion(info, matched, 0)
		return info
	}

	// Nenhuma execução conhecida corresponde à versão vigente — não dá pra afirmar nada
	// (ex: versão vigente é mais antiga que a janela de execuções buscada, OU o registry ainda
	// não viu a versão nova — ver LatestKnownExecutionAt). Fraseologia neutra de propósito, mesmo
	// padrão do resto da app (nunca inferir ausência por falta de dado) — ver
	// TrustedByPublicCA/ChainValidationResult no CLAUDE.md. LatestKnownExecutionAt SEMPRE
	// preenchido aqui (matched não é vazio, checado no topo da função) — é o caso mais comum em
	// que esse campo realmente importa pro chamador.
	return &RollbackInfo{Matched: false, LatestKnownExecutionAt: latestKnownExecutionAt, RecentStageFailures: recentStageFailures}
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
