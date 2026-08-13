package teams

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog"
)

const mrViaBotThreadID = "19:eab1be93-5589-4a3f-9f47-d6cfcbc50a0c_61740f97-9be2-4459-b054-5230364585a7@unq.gbl.spaces"

// MaxMessageAgeBusinessDays limita quão antiga uma mensagem pode ser (em dias ÚTEIS, não
// corridos) pra entrar no resultado da extração. O IndexedDB (skypexspaces) guarda o histórico
// COMPLETO do Mr.ViaBot sem paginação — sem esse corte, cada refresh reprocessa meses de
// mensagens antigas (visto em produção: 321 CHGs coletadas contra uma média real de ~30/dia no
// canal). Como o filtro é por PostedAt (data real da mensagem, não da extração), itens
// genuinamente antigos somem de vez, em vez de reaparecer a cada refresh com um novo ExtractedAt.
// Em dias úteis (não `time.Duration` fixo) porque uma janela corrida perde CHGs postadas numa
// sexta-feira antes de um fim de semana/feriado prolongado — ver businessdays.go/holidays.go
// (BusinessDaysAgo pula sábado, domingo e feriados nacionais). 3 dias úteis cobre a última CHG
// pendente de aprovação/importação sem deixar o cache crescer sem limite.
const MaxMessageAgeBusinessDays = 3

// CutoffTime retorna o instante mais antigo ainda dentro da janela de coleta (MaxMessageAgeBusinessDays
// dias úteis antes de `now`) — única fonte de verdade usada tanto pelo filtro de extração
// (filterByAge) quanto pelo merge de cache e pela listagem padrão do handler HTTP
// (GetApprovalsToday), pra nunca divergir entre os três.
//
// Corte em 00:00 do dia (não no horário exato de `now`) — decisão do usuário, ver discussão real:
// sem isso, o corte era "N dias úteis atrás, no mesmo horário de agora" (ex: 16h de quinta ->
// corte às 16h de segunda), o que excluía mensagens postadas de manhã na segunda mesmo sendo o
// dia inteiro parte da janela de "3 dias úteis" — e fazia o corte MUDAR ao longo do mesmo dia
// conforme o horário do refresh (refresh às 8h e às 20h no mesmo dia davam janelas diferentes).
// Normalizar pra meia-noite antes de subtrair os dias úteis torna o corte estável durante todo o
// dia corrente e inclui o dia inteiro de cada um dos N dias úteis anteriores.
func CutoffTime(now time.Time) time.Time {
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return BusinessDaysAgo(startOfDay, MaxMessageAgeBusinessDays)
}

// Extractor extrai aprovações SRE do Mr.ViaBot no Microsoft Teams via automação de browser.
type Extractor struct {
	SessionDir string
	Logger     *zerolog.Logger
}

// NewExtractor cria um Extractor usando o diretório de sessão exclusivo do Teams.
// Diretório separado do ServiceNow (rod-session) pois usa o Chrome do sistema,
// não o Chromium do Rod — perfis incompatíveis corrompem a sessão um do outro.
func NewExtractor(homeDir string, logger *zerolog.Logger) *Extractor {
	sessionDir := filepath.Join(homeDir, ".k8s-hpa-manager", "teams-session")
	os.MkdirAll(sessionDir, 0700) //nolint:errcheck
	return &Extractor{
		SessionDir: sessionDir,
		Logger:     logger,
	}
}

// Extract abre o Teams, navega até o Mr.ViaBot e extrai as aprovações do dia atual.
// Reutiliza a sessão Azure AD existente (compartilhada com ServiceNow).
func (e *Extractor) Extract() (*ExtractionResult, error) {
	// Garante que o diretório existe (Chrome cria a sessão na primeira execução)
	if err := os.MkdirAll(e.SessionDir, 0700); err != nil {
		return nil, fmt.Errorf("erro ao criar diretório de sessão: %v", err)
	}

	tmpDir, err := os.MkdirTemp("", "teams-extract-*")
	if err != nil {
		return nil, fmt.Errorf("erro ao criar diretório temporário: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	e.Logger.Info().Msg("[Teams] Iniciando extração de aprovações do Mr.ViaBot...")

	_, err = RunDiscovery(e.SessionDir, tmpDir, e.Logger, 10*time.Minute)
	if err != nil {
		return nil, fmt.Errorf("erro na descoberta Teams: %v", err)
	}

	// Coletar todas as mensagens (texto + data de postagem): DOM (lazy-loaded) + IndexedDB
	// (histórico completo)
	var allMessages []RawMessage

	domFile := filepath.Join(tmpDir, "viabot-dom-messages.json")
	if data, err := os.ReadFile(domFile); err == nil {
		var domData struct {
			Messages []RawMessage `json:"messages"`
		}
		if json.Unmarshal(data, &domData) == nil {
			allMessages = append(allMessages, domData.Messages...)
			e.Logger.Info().Int("count", len(domData.Messages)).Msg("[Teams] Mensagens DOM carregadas")
		}
	} else {
		e.Logger.Warn().Err(err).Msg("[Teams] Arquivo DOM não encontrado — usando apenas IndexedDB")
	}

	// IndexedDB: histórico completo sem lazy loading, cobre mensagens de dias anteriores
	idbFile := filepath.Join(tmpDir, "viabot-indexeddb-messages.json")
	if data, err := os.ReadFile(idbFile); err == nil {
		var idbData struct {
			Messages []RawMessage `json:"messages"`
		}
		if json.Unmarshal(data, &idbData) == nil {
			allMessages = append(allMessages, idbData.Messages...)
			e.Logger.Info().Int("count", len(idbData.Messages)).Msg("[Teams] Mensagens IndexedDB carregadas")
		}
	}

	if len(allMessages) == 0 {
		return nil, fmt.Errorf("nenhuma mensagem encontrada no DOM nem no IndexedDB — conversa não foi carregada")
	}

	items := ParseRawMessages(allMessages)
	e.Logger.Info().Int("count", len(items)).Msg("[Teams] Aprovações extraídas (antes do filtro de idade)")

	cutoff := CutoffTime(time.Now())
	items = filterByAge(items, cutoff)
	e.Logger.Info().Int("count", len(items)).Time("cutoff", cutoff).Msg("[Teams] Aprovações após filtro de idade")

	source := "dom"
	if _, err := os.Stat(idbFile); err == nil {
		source = "dom+indexeddb"
	}

	return &ExtractionResult{
		Items:       items,
		ExtractedAt: time.Now(),
		Source:      source,
	}, nil
}

// filterByAge descarta itens cuja mensagem foi postada antes de cutoff (ver CutoffTime) — sem
// isso o histórico completo do IndexedDB inunda o resultado com mensagens de meses atrás a cada
// refresh. Itens sem PostedAt (timestamp não capturado) são mantidos: vêm de caminhos
// inerentemente limitados ao conteúdo já carregado (DOM visível, messages[] do
// conversation-manager), não do histórico irrestrito do IndexedDB — não há como confirmar que
// são antigos.
func filterByAge(items []ApprovalItem, cutoff time.Time) []ApprovalItem {
	filtered := items[:0]
	for _, item := range items {
		if item.PostedAt != nil && item.PostedAt.Before(cutoff) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}
