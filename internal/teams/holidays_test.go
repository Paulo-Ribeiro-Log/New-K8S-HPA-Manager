package teams

import (
	"testing"
	"time"
)

func TestEasterDate(t *testing.T) {
	// Domingos de Páscoa conhecidos, confirmados via `date -d` (todos caem num domingo).
	cases := map[int]string{
		2024: "2024-03-31",
		2025: "2025-04-20",
		2026: "2026-04-05",
	}
	for year, want := range cases {
		got := dateKey(easterDate(year))
		if got != want {
			t.Errorf("ano %d: esperado %s, got %s", year, want, got)
		}
	}
}

func TestIsHoliday_FixedAndMovable(t *testing.T) {
	cases := []struct {
		date string
		want bool
		name string
	}{
		{"2026-01-01", true, "Confraternização Universal"},
		{"2026-04-21", true, "Tiradentes"},
		{"2026-12-25", true, "Natal"},
		{"2026-04-05", true, "Carnaval não é isso"}, // Páscoa em si não é feriado — só checa `want` abaixo
	}
	for _, c := range cases {
		d, err := time.Parse("2006-01-02", c.date)
		if err != nil {
			t.Fatalf("data inválida no teste: %v", err)
		}
		ok, name := IsHoliday(d)
		if c.date == "2026-04-05" {
			// Domingo de Páscoa em si não está na lista de feriados (só os móveis derivados dela).
			if ok {
				t.Errorf("%s: Páscoa em si não deveria estar marcada como feriado (got %q)", c.date, name)
			}
			continue
		}
		if ok != c.want {
			t.Errorf("%s: esperado IsHoliday=%v, got %v (nome=%q)", c.date, c.want, ok, name)
		}
	}
}

func TestIsHoliday_MovableFromEaster2026(t *testing.T) {
	// Páscoa 2026 = 05/abr. Carnaval = Páscoa-47d, Paixão de Cristo = Páscoa-2d, Corpus Christi = Páscoa+60d.
	easter := easterDate(2026)
	cases := map[string]struct {
		offset int
		name   string
	}{
		"carnaval": {-47, "Carnaval"},
		"paixao":   {-2, "Paixão de Cristo"},
		"corpus":   {60, "Corpus Christi"},
	}
	for label, c := range cases {
		d := easter.AddDate(0, 0, c.offset)
		ok, name := IsHoliday(d)
		if !ok || name != c.name {
			t.Errorf("%s (%s): esperado feriado %q, got ok=%v name=%q", label, dateKey(d), c.name, ok, name)
		}
	}
}

func TestIsHoliday_NotAHoliday(t *testing.T) {
	d := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC) // quarta-feira comum de agosto
	ok, name := IsHoliday(d)
	if ok {
		t.Errorf("12/ago/2026 não deveria ser feriado, got name=%q", name)
	}
}

func TestIsBusinessDay(t *testing.T) {
	cases := []struct {
		date string
		want bool
	}{
		{"2026-08-12", true},  // quarta-feira comum
		{"2026-08-15", false}, // sábado
		{"2026-08-16", false}, // domingo
		{"2026-12-25", false}, // sexta-feira, mas feriado (Natal)
		{"2026-01-01", false}, // quinta-feira, mas feriado (Confraternização)
	}
	for _, c := range cases {
		d, _ := time.Parse("2006-01-02", c.date)
		got := IsBusinessDay(d)
		if got != c.want {
			t.Errorf("%s: esperado IsBusinessDay=%v, got %v", c.date, c.want, got)
		}
	}
}

func TestBusinessDaysAgo_SkipsWeekend(t *testing.T) {
	// Segunda-feira, 17/ago/2026 — semana sem feriado.
	monday := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	if monday.Weekday() != time.Monday {
		t.Fatalf("data de referência não é segunda-feira: %v", monday.Weekday())
	}
	got := BusinessDaysAgo(monday, 3)
	// sexta(14) -> quinta(13) -> quarta(12): pula sáb(15)/dom(16) inteiramente.
	want := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("esperado %v, got %v", want, got)
	}
}

func TestBusinessDaysAgo_SkipsHolidayAndWeekend(t *testing.T) {
	// Terça-feira, 29/dez/2026. Sexta 25/dez é Natal (feriado), sáb 26 e dom 27 são fim de semana.
	tuesday := time.Date(2026, 12, 29, 12, 0, 0, 0, time.UTC)
	if tuesday.Weekday() != time.Tuesday {
		t.Fatalf("data de referência não é terça-feira: %v", tuesday.Weekday())
	}
	got := BusinessDaysAgo(tuesday, 2)
	// segunda(28) conta 1; terça(29,pula pq é o próprio ponto de partida não recontado)... na
	// prática: dia -1 = seg(28) útil (1) -> dia -2 = dom(27) pula -> dia -3 = sáb(26) pula ->
	// dia -4 = sex(25) feriado, pula -> dia -5 = qui(24) útil (2) -> para.
	want := time.Date(2026, 12, 24, 12, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("esperado %v, got %v", want, got)
	}
}

func TestBusinessDaysAgo_ZeroReturnsUnchanged(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	got := BusinessDaysAgo(now, 0)
	if !got.Equal(now) {
		t.Errorf("N=0 deveria retornar o mesmo instante, got %v", got)
	}
}
