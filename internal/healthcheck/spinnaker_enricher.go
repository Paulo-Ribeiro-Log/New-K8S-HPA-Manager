package healthcheck

import (
	"context"
	"fmt"
	"strings"
	"time"

	"k8s-hpa-manager/internal/spinnaker"
)

// spinnakerRecentRollbackWindow — "recente" (seção 9 item 7 do SPINNAKER-INTEGRATION-PLAN.md:
// "deployment com rollback recente (ex: últimas 24-48h) poderia virar um sinal de risco extra
// no Health Check"). 48h escolhido como teto da faixa sugerida.
const spinnakerRecentRollbackWindow = 48 * time.Hour

// SpinnakerEnricher resolve, uma vez por cluster (mesmo padrão de custo do ResourceEnricher —
// login + 1 busca de execuções por application do projeto Spinnaker configurado, não por
// deployment), o histórico usado depois pra checar se cada Deployment teve rollback recente.
type SpinnakerEnricher struct {
	executionsByApp map[string][]spinnaker.Execution
}

// deriveSpinnakerEnvGo é um alias fino pra spinnaker.DeriveEnv — fonte única da lógica agora vive
// lá (ver comentário em internal/spinnaker/config.go), mantido aqui só pra não precisar trocar
// todos os call sites deste arquivo.
func deriveSpinnakerEnvGo(cluster string) string {
	return spinnaker.DeriveEnv(cluster)
}

// NewSpinnakerEnricher faz login + busca as execuções de todas as applications do projeto
// Spinnaker configurado no perfil do usuário. Retorna (nil, err) quando o Spinnaker não está
// configurado (nenhum projeto selecionado), o cluster não tem ambiente Spinnaker reconhecido, ou
// está inacessível — o orchestrator trata isso como "sem sinal", nunca como erro fatal do Health
// Check inteiro (mesmo padrão do ResourceEnricher/Prometheus, ver orchestrator.go).
func NewSpinnakerEnricher(ctx context.Context, baseDir, cluster string) (*SpinnakerEnricher, error) {
	env := deriveSpinnakerEnvGo(cluster)
	if env == "" {
		return nil, fmt.Errorf("cluster %q sem ambiente Spinnaker reconhecido (esperado sufixo -hlg/-prd)", cluster)
	}

	cfg, err := spinnaker.LoadConfig(baseDir)
	if err != nil {
		return nil, fmt.Errorf("carregar config do spinnaker: %w", err)
	}
	if cfg.SelectedProject == "" {
		return nil, fmt.Errorf("nenhum projeto Spinnaker selecionado no perfil do usuário")
	}
	deckURL, err := cfg.DeckURLForEnv(env)
	if err != nil {
		return nil, err
	}
	gateURL, err := spinnaker.ResolveGateURL(ctx, nil, deckURL)
	if err != nil {
		return nil, fmt.Errorf("resolver Gate do Spinnaker: %w", err)
	}

	e := &SpinnakerEnricher{executionsByApp: map[string][]spinnaker.Execution{}}
	err = spinnaker.WithSession(ctx, gateURL, baseDir, cfg.EffectiveLoginIdentifier(), func(cl *spinnaker.Client) error {
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
		if len(applications) == 0 {
			return fmt.Errorf("projeto '%s' não encontrado (ou sem applications) no Spinnaker", cfg.SelectedProject)
		}
		for _, app := range applications {
			// Limit=30 — mesmo valor usado em internal/web/handlers/spinnaker.go, confirmado
			// ao vivo que aumentar não muda nada (o Gate capa a busca numa janela de tempo
			// própria de ~28 dias, não pelo "limit" pedido).
			execs, err := cl.SearchExecutions(ctx, app, spinnaker.SearchOptions{Limit: 30})
			if err != nil {
				continue // uma application falhando não deve derrubar o enricher inteiro
			}
			e.executionsByApp[app] = execs
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return e, nil
}

// RecentRollback checa se currentVersion do Deployment chegou lá por rollback dentro da janela
// "recente" (spinnakerRecentRollbackWindow) — reaproveita spinnaker.DetectRollback, a mesma
// lógica já validada ao vivo (Fases 1-4 do plano). Retorna nil quando não há rollback detectado,
// quando o rollback é antigo demais, ou quando não há dado suficiente (nunca inventa sinal).
func (e *SpinnakerEnricher) RecentRollback(deploymentName, namespace, currentVersion string) *spinnaker.RollbackInfo {
	if e == nil || currentVersion == "" {
		return nil
	}
	var all []spinnaker.Execution
	for _, execs := range e.executionsByApp {
		all = append(all, execs...)
	}
	if len(all) == 0 {
		return nil
	}
	info := spinnaker.DetectRollback(all, deploymentName, namespace, currentVersion)
	if !info.Matched || info.IsRollback == nil || !*info.IsRollback {
		return nil
	}
	rollbackTime := info.RollbackEndedAt
	if rollbackTime == 0 {
		rollbackTime = info.PipelineExecutedAt
	}
	if rollbackTime == 0 {
		return nil
	}
	age := time.Since(time.UnixMilli(rollbackTime))
	if age < 0 || age > spinnakerRecentRollbackWindow {
		return nil
	}
	return info
}
