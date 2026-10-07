package handlers

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Parser mínimo de expressões cron no formato aceito pelo CronJob do Kubernetes (o controller usa
// robfig/cron com o parser padrão de 5 campos): minuto hora dia-do-mês mês dia-da-semana, com `*`,
// `?`, listas (a,b), faixas (a-b), passos (*/n, a-b/n), nomes (JAN-DEC, SUN-SAT), macros
// (@hourly, @daily...) e prefixo CRON_TZ=/TZ=. Escrito à mão para não adicionar dependência ao
// vendor/ (que tem patch manual do go-rod). Usado só para exibir próxima execução/descrição.

type cronField struct {
	bits [64]bool // valores permitidos
	star bool     // campo era * ou ? (importa para a regra dia-do-mês x dia-da-semana)
}

type cronSchedule struct {
	minute, hour, dom, month, dow cronField
	tz                            string // de CRON_TZ=/TZ= dentro da expressão (o spec.timeZone tem prioridade)
}

var cronMacros = map[string]string{
	"@yearly":   "0 0 1 1 *",
	"@annually": "0 0 1 1 *",
	"@monthly":  "0 0 1 * *",
	"@weekly":   "0 0 * * 0",
	"@daily":    "0 0 * * *",
	"@midnight": "0 0 * * *",
	"@hourly":   "0 * * * *",
}

var cronMonthNames = map[string]int{"JAN": 1, "FEB": 2, "MAR": 3, "APR": 4, "MAY": 5, "JUN": 6, "JUL": 7, "AUG": 8, "SEP": 9, "OCT": 10, "NOV": 11, "DEC": 12}
var cronDowNames = map[string]int{"SUN": 0, "MON": 1, "TUE": 2, "WED": 3, "THU": 4, "FRI": 5, "SAT": 6}

func parseCron(expr string) (*cronSchedule, error) {
	expr = strings.TrimSpace(expr)
	s := &cronSchedule{}
	if strings.HasPrefix(expr, "CRON_TZ=") || strings.HasPrefix(expr, "TZ=") {
		i := strings.IndexByte(expr, ' ')
		if i < 0 {
			return nil, fmt.Errorf("expressão incompleta")
		}
		s.tz = expr[strings.IndexByte(expr, '=')+1 : i]
		expr = strings.TrimSpace(expr[i:])
	}
	if m, ok := cronMacros[strings.ToLower(expr)]; ok {
		expr = m
	}
	f := strings.Fields(expr)
	if len(f) != 5 {
		return nil, fmt.Errorf("esperados 5 campos, encontrados %d", len(f))
	}
	var err error
	if s.minute, err = parseCronField(f[0], 0, 59, nil); err != nil {
		return nil, fmt.Errorf("minuto: %w", err)
	}
	if s.hour, err = parseCronField(f[1], 0, 23, nil); err != nil {
		return nil, fmt.Errorf("hora: %w", err)
	}
	if s.dom, err = parseCronField(f[2], 1, 31, nil); err != nil {
		return nil, fmt.Errorf("dia do mês: %w", err)
	}
	if s.month, err = parseCronField(f[3], 1, 12, cronMonthNames); err != nil {
		return nil, fmt.Errorf("mês: %w", err)
	}
	if s.dow, err = parseCronField(f[4], 0, 7, cronDowNames); err != nil {
		return nil, fmt.Errorf("dia da semana: %w", err)
	}
	if s.dow.bits[7] { // 7 = domingo também
		s.dow.bits[0] = true
	}
	return s, nil
}

func parseCronField(field string, min, max int, names map[string]int) (cronField, error) {
	var cf cronField
	if field == "*" || field == "?" {
		cf.star = true
	}
	for _, part := range strings.Split(field, ",") {
		rangePart, stepPart, hasStep := strings.Cut(part, "/")
		step := 1
		if hasStep {
			n, err := strconv.Atoi(stepPart)
			if err != nil || n <= 0 {
				return cf, fmt.Errorf("passo inválido %q", stepPart)
			}
			step = n
		}
		lo, hi := min, max
		switch {
		case rangePart == "*" || rangePart == "?":
		default:
			a, b, isRange := strings.Cut(rangePart, "-")
			var err error
			if lo, err = cronValue(a, names); err != nil {
				return cf, err
			}
			hi = lo
			if isRange {
				if hi, err = cronValue(b, names); err != nil {
					return cf, err
				}
			} else if hasStep {
				hi = max // "5/15" = de 5 até o fim, de 15 em 15
			}
		}
		if lo < min || hi > max || lo > hi {
			return cf, fmt.Errorf("valor fora da faixa %d-%d em %q", min, max, part)
		}
		for v := lo; v <= hi; v += step {
			cf.bits[v] = true
		}
	}
	return cf, nil
}

func cronValue(s string, names map[string]int) (int, error) {
	if v, ok := names[strings.ToUpper(s)]; ok {
		return v, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("valor inválido %q", s)
	}
	return n, nil
}

func (s *cronSchedule) dayMatches(t time.Time) bool {
	dom := s.dom.bits[t.Day()]
	dow := s.dow.bits[int(t.Weekday())]
	if s.dom.star || s.dow.star {
		return dom && dow
	}
	return dom || dow // os dois restritos: basta um (regra do cron/robfig)
}

// next devolve a primeira execução estritamente depois de `after`, no fuso de `after`.
// Zero se não houver nenhuma nos próximos 5 anos (ex: 31 de fevereiro).
func (s *cronSchedule) next(after time.Time) time.Time {
	loc := after.Location()
	t := after.Truncate(time.Minute).Add(time.Minute)
	limit := t.AddDate(5, 0, 0)
	for t.Before(limit) {
		if !s.month.bits[int(t.Month())] {
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, loc)
			continue
		}
		if !s.dayMatches(t) {
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, loc)
			continue
		}
		if !s.hour.bits[t.Hour()] {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, loc)
			continue
		}
		if !s.minute.bits[t.Minute()] {
			t = t.Add(time.Minute)
			continue
		}
		return t
	}
	return time.Time{}
}

// ── Descrição em pt-BR ──────────────────────────────────────────────────────

var cronDowPt = []string{"domingo", "segunda", "terça", "quarta", "quinta", "sexta", "sábado"}

func (f cronField) values(min, max int) []int {
	var out []int
	for v := min; v <= max; v++ {
		if f.bits[v] {
			out = append(out, v)
		}
	}
	return out
}

// everyStep devolve n se o campo for exatamente "de n em n a partir do mínimo" (ex: */15).
func (f cronField) everyStep(min, max int) int {
	vals := f.values(min, max)
	if len(vals) < 2 || vals[0] != min {
		return 0
	}
	step := vals[1] - vals[0]
	for i := 1; i < len(vals); i++ {
		if vals[i]-vals[i-1] != step {
			return 0
		}
	}
	if vals[len(vals)-1]+step <= max {
		return 0
	}
	return step
}

func joinPt(items []string) string {
	if len(items) <= 1 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + " e " + items[len(items)-1]
}

func describeDow(f cronField) string {
	vals := f.values(0, 6)
	if len(vals) == 5 && vals[0] == 1 && vals[4] == 5 {
		return "de segunda a sexta"
	}
	if len(vals) == 2 && vals[0] == 0 && vals[1] == 6 {
		return "aos sábados e domingos"
	}
	names := make([]string, len(vals))
	for i, v := range vals {
		names[i] = cronDowPt[v]
	}
	if len(names) == 1 {
		if vals[0] == 0 || vals[0] == 6 { // domingo e sábado são masculinos
			return "todo " + names[0]
		}
		return "toda " + names[0]
	}
	return "às " + joinPt(names)
}

// describeCronSchedule devolve uma descrição legível do agendamento; cai na própria expressão
// quando o padrão não é um dos comuns.
func describeCronSchedule(schedule string) string {
	s, err := parseCron(schedule)
	if err != nil {
		return schedule
	}
	mins := s.minute.values(0, 59)
	hours := s.hour.values(0, 23)
	allDays := s.dom.star && s.month.star && s.dow.star

	var when string
	switch {
	case s.minute.star && s.hour.star:
		when = "A cada minuto"
	case s.minute.everyStep(0, 59) > 0 && s.hour.star:
		when = fmt.Sprintf("A cada %d minutos", s.minute.everyStep(0, 59))
	case len(mins) == 1 && s.hour.star:
		when = fmt.Sprintf("A cada hora, no minuto %02d", mins[0])
	case len(mins) == 1 && s.hour.everyStep(0, 23) > 0:
		when = fmt.Sprintf("A cada %d horas, no minuto %02d", s.hour.everyStep(0, 23), mins[0])
	case len(mins) == 1 && len(hours) >= 1 && len(hours) <= 4:
		times := make([]string, len(hours))
		for i, h := range hours {
			times[i] = fmt.Sprintf("%02d:%02d", h, mins[0])
		}
		when = "às " + joinPt(times)
	default:
		return schedule
	}
	if allDays {
		if strings.HasPrefix(when, "às ") {
			return "Todo dia " + when
		}
		return when
	}
	if !s.month.star {
		return schedule // padrões com mês específico: a expressão crua é mais clara
	}
	switch {
	case s.dom.star && !s.dow.star:
		return capitalize(describeDow(s.dow)) + ", " + when
	case !s.dom.star && s.dow.star:
		days := s.dom.values(1, 31)
		if len(days) == 1 {
			return fmt.Sprintf("Todo dia %d do mês, %s", days[0], when)
		}
	}
	return schedule
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	return strings.ToUpper(string(r[0])) + string(r[1:])
}
