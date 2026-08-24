package handlers

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"k8s-hpa-manager/internal/notifications"
	"k8s-hpa-manager/internal/spinnaker"
	"k8s-hpa-manager/internal/storage"
)

// SpinnakerFleetWatcher é a peça que faltava na integração Spinnaker: até aqui, a detecção de
// rollback/falha (spinnaker.DetectRollback) só rodava "puxada" — badge em DeploymentsTab.tsx (só
// se o usuário estivesse com a aba aberta no cluster/namespace exato) ou dentro de um Health
// Check manual (SpinnakerEnricher, opt-in). Não existia NENHUM mecanismo "empurrado" — achado
// real (relatado pelo usuário): uma falha de deploy real (webhook dsv-injector rejeitando por
// certificado TLS expirado, app entrega-mais-bff) e um rollback real em PRD passaram
// completamente despercebidos, porque literalmente ninguém estava olhando a tela certa no
// momento certo. Este watcher varre em background, sem depender de ninguém estar olhando, e
// empurra uma notificação in-app (+ Windows, se habilitada) na primeira vez que detecta uma
// falha/rollback que ainda não tinha sido visto.
//
// Reaproveita 100% a lógica de detecção já validada (spinnaker.DetectRollback, mesma usada por
// RolloutStatusBatch e SpinnakerEnricher) — este arquivo só adiciona o "quem varre sozinho e quem
// avisa", nada de lógica de match nova.
type SpinnakerFleetWatcher struct {
	baseDir            string
	deploymentRegistry *storage.DeploymentRegistry
	history            *storage.SpinnakerHistoryStore
	notifier           *notifications.NotificationManager
	logger             *zerolog.Logger

	// notifiedStageFailures dedupe notificações de FALHA DE ETAPA (info.RecentStageFailures, ver
	// notifyStageFailureIfNew) por chave "cluster/namespace/deployment" → ExecutionID da falha
	// mais recente já notificada. Em memória, mesmo padrão de NotificationManager.alertCache
	// (internal/notifications/manager.go) — não persiste no SpinnakerHistoryStore porque é
	// ortogonal ao "último rollout status confirmado" que aquela tabela existe pra guardar; pior
	// caso de um restart do servidor é reavisar uma vez sobre uma falha de retry já conhecida,
	// nunca perder uma nova.
	notifiedStageFailures   map[string]string
	notifiedStageFailuresMu sync.Mutex
}

// spinnakerWatchInterval — intervalo entre varreduras. 10min escolhido como equilíbrio: rápido
// o suficiente pra avisar bem antes de alguém notar sozinho (o caso relatado ficou invisível por
// horas), sem martelar o Gate (login + N buscas de execuções por ambiente configurado a cada
// tick — mesmo custo de abrir a aba Deployments manualmente).
const spinnakerWatchInterval = 10 * time.Minute

// spinnakerWatchTimeout — teto por ambiente checado (login + ListProjects + SearchExecutions de
// todas as applications do projeto) — mesma ordem de grandeza do timeout de RolloutStatusBatch.
const spinnakerWatchTimeout = 90 * time.Second

// spinnakerNotifyLogPreviewChars — o texto de notificação não pode carregar um FailureLog
// inteiro (até maxStageLogChars=20000, ver models.go) sem virar ilegível na UI de notificações;
// o suficiente pra reconhecer a causa (ex: "certificate has expired") e ir investigar via o
// modal/badge já existente pro resto do detalhe.
const spinnakerNotifyLogPreviewChars = 500

// NewSpinnakerFleetWatcher cria o watcher. registry/history/notifier podem ser nil (mesmo padrão
// de graceful degradation do resto do pacote) — nesse caso cada tick vira no-op silencioso.
func NewSpinnakerFleetWatcher(baseDir string, registry *storage.DeploymentRegistry, history *storage.SpinnakerHistoryStore, notifier *notifications.NotificationManager, logger *zerolog.Logger) *SpinnakerFleetWatcher {
	return &SpinnakerFleetWatcher{
		baseDir:               baseDir,
		deploymentRegistry:    registry,
		history:               history,
		notifier:              notifier,
		logger:                logger,
		notifiedStageFailures: make(map[string]string),
	}
}

// Run roda em background pela vida inteira do processo — chamado uma vez via `go w.Run()` na
// inicialização do servidor (mesmo padrão de startDBTestContainerReaper, db_test_docker.go).
// Faz um tick logo de cara (não espera os primeiros 10min) — se já existe uma falha/rollback sem
// notificar, não faz sentido esperar um ciclo inteiro pra avisar.
func (w *SpinnakerFleetWatcher) Run() {
	w.tick()

	ticker := time.NewTicker(spinnakerWatchInterval)
	defer ticker.Stop()
	for range ticker.C {
		w.tick()
	}
}

// tick faz uma varredura completa: carrega a config do usuário, resolve quais clusters têm
// Deployments rastreados (Deployment Registry) e ambiente Spinnaker reconhecido, e checa cada um.
// Nunca é erro fatal — Spinnaker não configurado é o caso comum (a maioria das instâncias desta
// app não tem credencial Spinnaker), tratado como silêncio, não como falha.
func (w *SpinnakerFleetWatcher) tick() {
	if w.deploymentRegistry == nil {
		return
	}

	cfg, err := spinnaker.LoadConfig(w.baseDir)
	if err != nil {
		w.logf("carregar config: %v", err)
		return
	}
	if cfg.SelectedProject == "" {
		return // Spinnaker não configurado neste perfil — nada a fazer, silencioso
	}

	clusters, err := w.deploymentRegistry.GetUniqueClusters()
	if err != nil {
		w.logf("listar clusters do Deployment Registry: %v", err)
		return
	}

	byEnv := map[string][]string{}
	for _, cluster := range clusters {
		env := spinnaker.DeriveEnv(cluster)
		if env == "" {
			continue // cluster sem ambiente Spinnaker reconhecido (ver spinnaker.DeriveEnv)
		}
		byEnv[env] = append(byEnv[env], cluster)
	}

	for env, envClusters := range byEnv {
		w.checkEnv(cfg, env, envClusters)
	}
}

// checkEnv faz UMA busca de execuções por application do projeto configurado (mesmo padrão de
// custo de RolloutStatusBatch/SpinnakerEnricher — nunca uma chamada por cluster/deployment) e
// checa todos os clusters desse ambiente contra o resultado.
func (w *SpinnakerFleetWatcher) checkEnv(cfg spinnaker.Config, env string, clusters []string) {
	deckURL, err := cfg.DeckURLForEnv(env)
	if err != nil {
		return // URL do Deck pra esse ambiente não configurada — silencioso
	}

	ctx, cancel := context.WithTimeout(context.Background(), spinnakerWatchTimeout)
	defer cancel()

	gateURL, err := spinnaker.ResolveGateURL(ctx, nil, deckURL)
	if err != nil {
		w.logf("resolver Gate (env=%s): %v", env, err)
		return
	}

	// executionsByApp (não só a lista achatada) é necessário pra montar o deep-link do Deck
	// (buildExecutionURL/applicationForExecution, spinnaker.go — precisa saber de qual
	// application veio cada execução) — pedido do usuário: "nas notificações, inclua o link para
	// abrir o problema no spinnaker".
	executionsByApp := map[string][]spinnaker.Execution{}
	err = spinnaker.WithSession(ctx, gateURL, w.baseDir, cfg.EffectiveLoginIdentifier(), func(cl *spinnaker.Client) error {
		projects, err := cl.ListProjects(ctx)
		if err != nil {
			return err
		}
		var applications []string
		for _, p := range projects {
			if strings.EqualFold(p.Name, cfg.SelectedProject) {
				applications = p.Config.Applications
				break
			}
		}
		for _, app := range applications {
			execs, err := cl.SearchExecutions(ctx, app, spinnaker.SearchOptions{Limit: 30})
			if err != nil {
				continue // uma application falhando não deve derrubar a varredura inteira
			}
			executionsByApp[app] = execs
		}
		return nil
	})
	if err != nil {
		w.logf("checar Spinnaker (env=%s): %v", env, err)
		return
	}
	var allExecutions []spinnaker.Execution
	for _, execs := range executionsByApp {
		allExecutions = append(allExecutions, execs...)
	}
	if len(allExecutions) == 0 {
		return
	}

	for _, cluster := range clusters {
		records, err := w.deploymentRegistry.GetAll(cluster, "", true)
		if err != nil {
			w.logf("listar Deployments de %s: %v", cluster, err)
			continue
		}
		for _, rec := range records {
			w.checkDeployment(cluster, rec, allExecutions, deckURL, cfg.SelectedProject, executionsByApp)
		}
	}
}

// checkDeployment aplica DetectRollback pra um Deployment específico e decide se o resultado é
// "novidade" (ver isNewFailureSignal) — se for, notifica. Sempre persiste o estado mais recente no
// history, novidade ou não, pra servir de baseline de comparação no próximo tick.
func (w *SpinnakerFleetWatcher) checkDeployment(cluster string, rec storage.DeploymentRecord, executions []spinnaker.Execution, deckURL, project string, executionsByApp map[string][]spinnaker.Execution) {
	// Mesma ordem de prioridade de RolloutStatusBatch (ImageTag antes de Version — ver comentário
	// lá sobre o chart convair-helm sanitizar "." em "-" no label app.kubernetes.io/version).
	version := rec.ImageTag
	if version == "" {
		version = rec.Version
	}
	if version == "" {
		return
	}

	info := spinnaker.DetectRollback(executions, rec.DeploymentName, rec.Namespace, version)
	if info.Matched && info.SpinnakerExecutionID != "" {
		info.SpinnakerExecutionURL = buildExecutionURL(deckURL, project, applicationForExecution(executionsByApp, info.SpinnakerExecutionID), info.SpinnakerExecutionID)
	}
	fillStageFailureURLs(info, deckURL, project, executionsByApp)
	applyRegistryFreshness(info, rec) // mesma checagem de spinnaker.go — ver comentário lá

	// RecentStageFailures é checado ANTES do early-return de `!info.Matched` abaixo — achado real
	// (relatado ao vivo pelo usuário): o caso mais comum de falha real e silenciosa é uma etapa
	// que falha (ex: Job Kubernetes com BackoffLimitExceeded) dentro de uma execução que
	// eventualmente termina SUCCEEDED via retry automático do Spinnaker — isso é Matched:true com
	// IsRollback:false ("deploy normal", nunca cai no branch de rollback abaixo) OU até
	// Matched:false. Sem checar aqui, esse tipo de falha nunca gerava nenhum sinal, mesmo com o
	// watcher já rodando. Não depende de RegistryStale — RecentStageFailures não usa
	// currentLiveVersion pra nada, é ortogonal à comparação de versão.
	w.notifyStageFailureIfNew(cluster, rec.Namespace, rec.DeploymentName, spinnaker.DeriveEnv(cluster), info)

	if !info.Matched {
		return
	}

	var prev *storage.SpinnakerRolloutRecord
	if w.history != nil {
		prev, _ = w.history.Get(cluster, rec.Namespace, rec.DeploymentName) // erro = nunca visto antes, tratado abaixo
	}
	// info.RegistryStale entra na decisão: um Matched:true construído em cima de uma
	// currentLiveVersion desatualizada é possível (embora raro — normalmente registry velho
	// produz Matched:false, não um falso positivo) e não deveria virar notificação sem o usuário
	// poder desconfiar do dado; mais seguro deixar o badge (RolloutStatusBatch, que já expõe
	// registry_stale) mostrar o aviso do que a app mandar um alerta "gritando lobo".
	shouldNotify := isNewFailureSignal(prev, info) && !info.RegistryStale

	if w.history != nil {
		if err := w.history.Upsert(toHistoryRecord(cluster, rec.Namespace, rec.DeploymentName, info)); err != nil {
			w.logf("persistir rollout status de %s/%s: %v", rec.DeploymentName, rec.Namespace, err)
		}
	}

	if shouldNotify {
		w.notify(cluster, spinnaker.DeriveEnv(cluster), rec.Namespace, rec.DeploymentName, info)
	}
}

// isNewFailureSignal decide se `info` representa uma falha/rollback que ainda não tinha sido
// visto — comparando contra o último estado persistido (`prev`, nil quando nunca visto antes).
// Extraída como função pura (sem tocar disco/rede) pra ser testável isoladamente — ver
// spinnaker_watcher_test.go.
//
// Regra: só notifica quando IsRollback é true E (nunca vimos esse deployment antes, OU da última
// vez que vimos ele não estava em estado de falha, OU é uma execução DIFERENTE da última que já
// gerou notificação) — dedup pelo SpinnakerExecutionID evita reavisar a cada tick de 10min
// enquanto ninguém corrigir o deploy (a execução mais recente continua sendo a mesma falha já
// notificada). Primeira vez que o watcher roda contra um deployment já quebrado há dias (ex: o
// rollback em PRD que o usuário relatou) SEMPRE notifica — comportamento desejado, é exatamente o
// caso que motivou este mecanismo existir.
func isNewFailureSignal(prev *storage.SpinnakerRolloutRecord, info *spinnaker.RollbackInfo) bool {
	if info == nil || info.IsRollback == nil || !*info.IsRollback {
		return false
	}
	if prev == nil {
		return true
	}
	if !prev.IsRollback {
		return true
	}
	return prev.SpinnakerExecutionID != info.SpinnakerExecutionID
}

// notify monta o texto da notificação — todo o contexto (cluster/namespace/deployment/CHG/status/
// log de falha) vai no corpo, já que NotifySpinnakerRollout nunca torna a notificação clicável
// (ver comentário lá). Severidade: "critical" em PRD, "warning" nos demais ambientes.
func (w *SpinnakerFleetWatcher) notify(cluster, spinnakerEnv, namespace, deployment string, info *spinnaker.RollbackInfo) {
	if w.notifier == nil {
		return
	}

	kind := "Falha de deploy detectada"
	if info.RollbackType == "explicit" {
		kind = "Rollback executado"
	}
	title := fmt.Sprintf("🔴 Spinnaker: %s — %s", kind, deployment)

	severity := "warning"
	if spinnakerEnv == spinnaker.EnvPRD {
		severity = "critical"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Cluster: %s\nNamespace: %s\nDeployment: %s\n", cluster, namespace, deployment)
	if info.Version != "" {
		fmt.Fprintf(&b, "Versão-alvo: %s\n", info.Version)
	}
	if info.ExecutionStatus != "" {
		fmt.Fprintf(&b, "Status da execução: %s\n", info.ExecutionStatus)
	}
	// Data/hora da execução — pedido explícito do usuário: a notificação já mostrava "há quanto
	// tempo foi notificado" (timestamp da própria InAppNotification, formatDistanceToNow no
	// NotificationDrawer.tsx), mas não a data da execução da pipeline em si — as duas podem
	// divergir bastante (o watcher só roda a cada 10min, e a 1ª varredura contra um deployment já
	// quebrado há dias sempre notifica, ver isNewFailureSignal). Mesmo rótulo/formato de
	// SpinnakerRolloutModal.tsx ("Data/hora da execução", fmtDate) pra não introduzir uma segunda
	// convenção de data pro mesmo dado.
	if s := formatSpinnakerTime(info.PipelineExecutedAt); s != "" {
		fmt.Fprintf(&b, "Data/hora da execução: %s\n", s)
	}
	// Início/Fim do rollback só fazem sentido pro rollback EXPLÍCITO (pipeline dedicado
	// rollback-aks-global) — é uma execução separada da execução que falhou (FailedCHG acima),
	// mesma distinção já feita em SpinnakerRolloutModal.tsx ("Início"/"Fim" só aparecem nesse
	// bloco). Rollback implícito (RollingUpdate do K8s, sem execução própria no Spinnaker) não
	// tem esses dois campos preenchidos por DetectRollback.
	if info.RollbackType == "explicit" {
		if s := formatSpinnakerTime(info.RollbackStartedAt); s != "" {
			fmt.Fprintf(&b, "Início do rollback: %s\n", s)
		}
		if s := formatSpinnakerTime(info.RollbackEndedAt); s != "" {
			fmt.Fprintf(&b, "Fim do rollback: %s\n", s)
		}
	}
	if info.FailedCHG != "" {
		fmt.Fprintf(&b, "CHG: %s\n", info.FailedCHG)
	}
	for _, stage := range info.Stages {
		if stage.Log == "" {
			continue
		}
		log := stage.Log
		if len(log) > spinnakerNotifyLogPreviewChars {
			log = log[:spinnakerNotifyLogPreviewChars] + "\n...[ver detalhe completo no badge Spinnaker de Deployments]..."
		}
		fmt.Fprintf(&b, "\n--- %s ---\n%s\n", stage.Name, log)
	}
	if info.SpinnakerExecutionURL != "" {
		fmt.Fprintf(&b, "\nAbrir no Spinnaker: %s\n", info.SpinnakerExecutionURL)
	}

	if err := w.notifier.NotifySpinnakerRollout(title, b.String(), severity, info.SpinnakerExecutionURL); err != nil {
		w.logf("notificar %s/%s: %v", deployment, namespace, err)
	}
}

// notifyStageFailureIfNew notifica sobre a falha de etapa mais recente encontrada
// (info.RecentStageFailures[0] — a lista vem da execução mais recente pra mais antiga, ver
// findRecentStageFailures) se ainda não tiver notificado sobre essa execução específica antes.
// Achado real: pipelines com retry automático (etapa falha, Spinnaker tenta de novo, eventualmente
// sucede) nunca geravam nenhum sinal — nem no badge/modal, nem aqui — porque o resultado
// principal (Matched/IsRollback) reflete só o estado FINAL (que é sucesso, corretamente), nunca
// o que aconteceu no meio do caminho. Tom da mensagem deliberadamente mais brando que `notify`
// (emoji amarelo, não vermelho; título "auto-recuperada") — isso já se resolveu sozinho, é um
// aviso de atenção/diagnóstico, não uma cobrança de ação imediata como um rollback real.
func (w *SpinnakerFleetWatcher) notifyStageFailureIfNew(cluster, namespace, deployment, spinnakerEnv string, info *spinnaker.RollbackInfo) {
	if w.notifier == nil || info == nil || len(info.RecentStageFailures) == 0 {
		return
	}
	newest := info.RecentStageFailures[0]

	// info.RecentStageFailures não é mais filtrado por idade na origem (ver comentário de
	// spinnaker.StageFailureRecentWindow — precisa continuar aparecendo no badge/modal mesmo pra
	// falhas antigas, pedido explícito do usuário: "não era pra retirar a indicação de atenção...
	// dos cards"). Mas não faz sentido EMPURRAR uma notificação sobre algo de semanas atrás — só
	// a notificação em si continua gateada por tempo, não o dado exposto.
	if time.Since(time.UnixMilli(newest.ExecutionTime)) > spinnaker.StageFailureRecentWindow {
		return
	}

	key := cluster + "/" + namespace + "/" + deployment

	w.notifiedStageFailuresMu.Lock()
	already := w.notifiedStageFailures[key] == newest.ExecutionID
	if !already {
		w.notifiedStageFailures[key] = newest.ExecutionID
	}
	w.notifiedStageFailuresMu.Unlock()
	if already {
		return
	}

	severity := "warning"
	if spinnakerEnv == spinnaker.EnvPRD {
		severity = "critical"
	}
	title := fmt.Sprintf("🟡 Spinnaker: falha em etapa (auto-recuperada) — %s", deployment)

	log := newest.Log
	if len(log) > spinnakerNotifyLogPreviewChars {
		log = log[:spinnakerNotifyLogPreviewChars] + "\n...[ver detalhe completo no badge Spinnaker de Deployments]..."
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Cluster: %s\nNamespace: %s\nDeployment: %s\n", cluster, namespace, deployment)
	if newest.Version != "" {
		fmt.Fprintf(&b, "Versão: %s\n", newest.Version)
	}
	// Mesmo pedido/rótulo de notify() acima — a idade da notificação (timestamp da própria
	// InAppNotification) não é a mesma coisa que quando a execução rodou de fato.
	if s := formatSpinnakerTime(newest.ExecutionTime); s != "" {
		fmt.Fprintf(&b, "Data/hora da execução: %s\n", s)
	}
	if newest.CHG != "" {
		fmt.Fprintf(&b, "CHG: %s\n", newest.CHG)
	}
	fmt.Fprintf(&b, "Etapa: %s (%s)\n\n%s\n", newest.StageName, newest.StageStatus, log)
	b.WriteString("\nA execução mais recente já teve sucesso — este aviso é sobre o retry, não um estado atual quebrado.")
	if newest.ExecutionURL != "" {
		fmt.Fprintf(&b, "\n\nAbrir no Spinnaker: %s\n", newest.ExecutionURL)
	}

	if err := w.notifier.NotifySpinnakerRollout(title, b.String(), severity, newest.ExecutionURL); err != nil {
		w.logf("notificar falha de etapa %s/%s: %v", deployment, namespace, err)
	}
}

func (w *SpinnakerFleetWatcher) logf(format string, args ...interface{}) {
	if w.logger == nil {
		return
	}
	w.logger.Warn().Msgf(format, args...)
}

// spinnakerNotifyTZ — horário de Brasília fixo (UTC-3). Bug real corrigido: formatSpinnakerTime
// usava time.UnixMilli(ms).Format(...), que resolve a hora via time.Local — o timezone
// CONFIGURADO NO HOST onde o processo roda, não necessariamente São Paulo (servidor sem TZ
// setada, container sem /etc/localtime, etc. caem em UTC por padrão no Go, sem erro nem aviso).
// Nesta máquina de dev o host já está em America/Sao_Paulo, então o bug não reproduzia aqui, mas
// a notificação precisa mostrar -3 São Paulo sempre, independente de onde o servidor roda —
// Brasil não usa mais horário de verão desde 2019, então um offset fixo é sempre correto, sem
// depender de tzdata/LoadLocation (que também falharia num host sem o banco de zoneinfo).
var spinnakerNotifyTZ = time.FixedZone("-03:00", -3*60*60)

// formatSpinnakerTime formata um epoch-ms (0 = ausente, retorna "") em horário de Brasília fixo
// (ver spinnakerNotifyTZ acima), no mesmo formato usado pelo resto da app pra esse tipo de
// timestamp em texto (ver healthcheck.go:814) — não o mesmo formatador do frontend (fmtDate em
// SpinnakerRolloutModal.tsx usa toLocaleString, específico de JS, resolvido pelo timezone do
// BROWSER do usuário, não do servidor — não sofre desse bug), já que aqui o texto é montado no
// backend e vai direto pro corpo da notificação.
func formatSpinnakerTime(ms int64) string {
	if ms == 0 {
		return ""
	}
	return time.UnixMilli(ms).In(spinnakerNotifyTZ).Format("02/01/2006 15:04")
}
