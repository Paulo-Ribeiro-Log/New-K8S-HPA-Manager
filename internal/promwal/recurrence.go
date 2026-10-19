package promwal

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Anotações PERSISTENTES no recurso Prometheus (diferente de AnnotationOriginalReplicas, que só existe durante
// a limpeza): gravadas no mesmo patch que religa o Prometheus ao fim de uma limpeza em que algo foi apagado.
const (
	AnnotationCleanupCount   = "hpa-manager/wal-cleanup-count"   // total acumulado
	AnnotationCleanupLastAt  = "hpa-manager/wal-cleanup-last-at" // RFC3339
	AnnotationCleanupHistory = "hpa-manager/wal-cleanup-history" // últimas datas RFC3339, separadas por vírgula
)

const (
	// RecurrenceWindow e RecurrenceThreshold definem "problema crônico": 2 ou mais limpezas em 30 dias.
	RecurrenceWindow    = 30 * 24 * time.Hour
	RecurrenceThreshold = 2

	maxHistoryDates = 10
	// dedupeWindow: a mesma limpeza aparece na anotação (gravada ao religar) e no histórico (gravado ao
	// terminar) com horários próximos; eventos dentro desta janela são a mesma limpeza.
	dedupeWindow = 10 * time.Minute
)

// Recurrence resume as limpezas anteriores de um Prometheus.
type Recurrence struct {
	Count30d  int         `json:"count_30d"`
	Total     int         `json:"total"` // acumulado conhecido (a anotação pode ser maior que o que ainda está no histórico)
	LastAt    *time.Time  `json:"last_at,omitempty"`
	Dates     []time.Time `json:"dates,omitempty"` // dentro da janela de 30 dias, da mais recente para a mais antiga
	Recurrent bool        `json:"recurrent"`
}

// BuildRecurrence une as datas da anotação e do Change History (deduplicando a mesma limpeza) e conta as
// que caem nos últimos 30 dias. total é o contador acumulado da anotação (0 se não houver).
func BuildRecurrence(annotationDates, historyDates []time.Time, annotationTotal int, now time.Time) Recurrence {
	all := append(append([]time.Time{}, annotationDates...), historyDates...)
	sort.Slice(all, func(i, j int) bool { return all[i].Before(all[j]) })

	var merged []time.Time
	for _, d := range all {
		if n := len(merged); n > 0 && d.Sub(merged[n-1]) < dedupeWindow {
			continue
		}
		merged = append(merged, d)
	}

	r := Recurrence{Total: max(annotationTotal, len(merged))}
	for i := len(merged) - 1; i >= 0; i-- { // da mais recente para a mais antiga
		d := merged[i]
		if r.LastAt == nil {
			t := d
			r.LastAt = &t
		}
		if now.Sub(d) <= RecurrenceWindow {
			r.Dates = append(r.Dates, d)
		}
	}
	r.Count30d = len(r.Dates)
	r.Recurrent = r.Count30d >= RecurrenceThreshold
	return r
}

func parseDates(csv string) []time.Time {
	var out []time.Time
	for _, f := range strings.Split(csv, ",") {
		if t, err := time.Parse(time.RFC3339, strings.TrimSpace(f)); err == nil {
			out = append(out, t)
		}
	}
	return out
}

func parseCount(v string) int {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// cleanupAnnotations devolve as anotações persistentes atualizadas com mais uma limpeza em at, a partir das
// anotações atuais do recurso. O histórico guarda só as últimas maxHistoryDates datas.
func cleanupAnnotations(current map[string]string, at time.Time) map[string]any {
	dates := append(parseDates(current[AnnotationCleanupHistory]), at)
	if len(dates) > maxHistoryDates {
		dates = dates[len(dates)-maxHistoryDates:]
	}
	strs := make([]string, len(dates))
	for i, d := range dates {
		strs[i] = d.UTC().Format(time.RFC3339)
	}
	return map[string]any{
		AnnotationCleanupCount:   fmt.Sprint(parseCount(current[AnnotationCleanupCount]) + 1),
		AnnotationCleanupLastAt:  at.UTC().Format(time.RFC3339),
		AnnotationCleanupHistory: strings.Join(strs, ","),
	}
}

// RecurrenceWarning é o aviso mostrado na análise e no diálogo quando o problema é recorrente.
func RecurrenceWarning(r Recurrence, now time.Time) string {
	if !r.Recurrent || r.LastAt == nil {
		return ""
	}
	return fmt.Sprintf("Este Prometheus já teve o WAL limpo %d vezes nos últimos 30 dias (última há %s). Limpar de novo trata o sintoma: a causa é provavelmente recorrente (cardinalidade crescendo, memory limit baixo, disco cheio ou retenção alta). Investigue antes de repetir.",
		r.Count30d, humanSince(now.Sub(*r.LastAt)))
}

func humanSince(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h", int(d.Hours()))
	default:
		return fmt.Sprintf("%d dias", int(d.Hours()/24))
	}
}
