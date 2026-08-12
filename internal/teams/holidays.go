package teams

import "time"

// easterDate calcula a data do Domingo de Páscoa para o ano informado via algoritmo do Computus
// (Anonymous Gregorian/Meeus) — necessário porque Carnaval, Paixão de Cristo e Corpus Christi
// (feriados nacionais móveis) são calculados a partir dela, sem data fixa no calendário.
// Conversão direta do algoritmo em Python fornecido (mesma fórmula, mesma ordem de operações;
// divisão inteira de Go em operandos positivos equivale a `//` do Python aqui).
func easterDate(year int) time.Time {
	a := year % 19
	b := year / 100
	c := year % 100
	d := b / 4
	e := b % 4
	f := (b + 8) / 25
	g := (b - f + 1) / 3
	h := (19*a + b - d - g + 15) % 30
	i := c / 4
	k := c % 4
	l := (32 + 2*e + 2*i - h - k) % 7
	m := (a + 11*h + 22*l) / 451
	month := (h + l - 7*m + 114) / 31
	day := ((h + l - 7*m + 114) % 31) + 1
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
}

// dateKey normaliza uma data para a chave "AAAA-MM-DD" usada no mapa de feriados — ignora
// horário/fuso de propósito, feriado é por dia civil, não por instante.
func dateKey(d time.Time) string {
	return d.Format("2006-01-02")
}

// nationalHolidays retorna os feriados nacionais brasileiros (fixos + móveis calculados a partir
// da Páscoa) do ano informado, chaveados por "AAAA-MM-DD". Mesma lista fixa do script Python de
// referência — só feriados NACIONAIS, não cobre feriados estaduais/municipais (sem fonte única
// confiável e não é o objetivo aqui: só evitar perder CHGs postadas ao redor de um feriado
// nacional, não servir de calendário de RH/folha).
func nationalHolidays(year int) map[string]string {
	easter := easterDate(year)

	holidays := map[string]string{
		dateKey(easter.AddDate(0, 0, -47)): "Carnaval",
		dateKey(easter.AddDate(0, 0, -2)):  "Paixão de Cristo",
		dateKey(easter.AddDate(0, 0, 60)):  "Corpus Christi",

		dateKey(time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)):   "Confraternização Universal",
		dateKey(time.Date(year, 4, 21, 0, 0, 0, 0, time.UTC)):  "Tiradentes",
		dateKey(time.Date(year, 5, 1, 0, 0, 0, 0, time.UTC)):   "Dia do Trabalho",
		dateKey(time.Date(year, 9, 7, 0, 0, 0, 0, time.UTC)):   "Independência do Brasil",
		dateKey(time.Date(year, 10, 12, 0, 0, 0, 0, time.UTC)): "Nossa Sra. Aparecida - Padroeira do Brasil",
		dateKey(time.Date(year, 11, 2, 0, 0, 0, 0, time.UTC)):  "Finados",
		dateKey(time.Date(year, 11, 15, 0, 0, 0, 0, time.UTC)): "Proclamação da República",
		dateKey(time.Date(year, 11, 20, 0, 0, 0, 0, time.UTC)): "Consciência Negra",
		dateKey(time.Date(year, 12, 25, 0, 0, 0, 0, time.UTC)): "Natal",
	}
	return holidays
}

// IsHoliday reporta se d cai num feriado nacional brasileiro, e o nome do feriado (vazio se não
// for). Compara só a parte de data (ano/mês/dia) — fuso/horário de d são ignorados.
func IsHoliday(d time.Time) (bool, string) {
	name, ok := nationalHolidays(d.Year())[dateKey(d)]
	return ok, name
}

// IsBusinessDay reporta se d é dia útil: não é sábado/domingo e não é feriado nacional.
func IsBusinessDay(d time.Time) bool {
	if wd := d.Weekday(); wd == time.Saturday || wd == time.Sunday {
		return false
	}
	isHoliday, _ := IsHoliday(d)
	return !isHoliday
}

// BusinessDaysAgo retorna o instante N dias úteis antes de `from`, pulando sábados, domingos e
// feriados nacionais — usado pra calcular a janela de coleta de CHGs (ver
// MaxMessageAgeBusinessDays em extractor.go). Numa segunda-feira, "3 dias úteis atrás" alcança a
// quarta-feira da semana anterior (pula sáb+dom), não "3 dias corridos atrás" (que cairia numa
// sexta-feira só por coincidência de calendário). N<=0 retorna `from` sem alteração.
func BusinessDaysAgo(from time.Time, n int) time.Time {
	d := from
	for counted := 0; counted < n; {
		d = d.AddDate(0, 0, -1)
		if IsBusinessDay(d) {
			counted++
		}
	}
	return d
}
