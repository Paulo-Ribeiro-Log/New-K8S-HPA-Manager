package handlers

import (
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func mustNext(t *testing.T, expr string, after time.Time) time.Time {
	t.Helper()
	s, err := parseCron(expr)
	if err != nil {
		t.Fatalf("parseCron(%q): %v", expr, err)
	}
	return s.next(after)
}

func TestCronNext(t *testing.T) {
	base := time.Date(2026, 10, 7, 10, 7, 30, 0, time.UTC) // quarta-feira
	cases := []struct {
		expr string
		want time.Time
	}{
		{"*/5 * * * *", time.Date(2026, 10, 7, 10, 10, 0, 0, time.UTC)},
		{"0 * * * *", time.Date(2026, 10, 7, 11, 0, 0, 0, time.UTC)},
		{"30 2 * * *", time.Date(2026, 10, 8, 2, 30, 0, 0, time.UTC)},
		{"0 9 * * 1-5", time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)},
		{"0 9 * * MON", time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)},
		{"0 0 1 * *", time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)},
		{"@daily", time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)},
		{"@hourly", time.Date(2026, 10, 7, 11, 0, 0, 0, time.UTC)},
		{"0 0 * * 7", time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)},              // 7 = domingo
		{"0 0 13 * 5", time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)},              // dom e dow restritos: OU (sexta 9 vem antes do dia 13)
		{"15 10 * * *", time.Date(2026, 10, 7, 10, 15, 0, 0, time.UTC)},           // ainda hoje
		{"7 10 * * *", time.Date(2026, 10, 8, 10, 7, 0, 0, time.UTC)},             // 10:07 já passou (estritamente depois)
		{"CRON_TZ=UTC 0 12 * * *", time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)}, // prefixo aceito
	}
	for _, c := range cases {
		if got := mustNext(t, c.expr, base); !got.Equal(c.want) {
			t.Errorf("%q: next = %v, want %v", c.expr, got, c.want)
		}
	}
}

func TestCronNextTimezone(t *testing.T) {
	sp, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Skip("tzdata indisponível")
	}
	base := time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC).In(sp) // 07:00 em SP
	got := mustNext(t, "0 8 * * *", base)
	if got.Hour() != 8 || got.Day() != 7 || got.Location() != sp {
		t.Errorf("next = %v", got)
	}
}

func TestCronParseErrors(t *testing.T) {
	for _, expr := range []string{"", "* * * *", "60 * * * *", "* 24 * * *", "*/0 * * * *", "0 0 32 * *", "0 0 * FOO *"} {
		if _, err := parseCron(expr); err == nil {
			t.Errorf("%q deveria ser inválida", expr)
		}
	}
	// 31 de fevereiro nunca acontece: next devolve zero em vez de travar
	if got := mustNext(t, "0 0 31 2 *", time.Now()); !got.IsZero() {
		t.Errorf("31/02 deveria não ter próxima execução, veio %v", got)
	}
}

func TestDescribeCronSchedule(t *testing.T) {
	cases := map[string]string{
		"*/5 * * * *":    "A cada 5 minutos",
		"* * * * *":      "A cada minuto",
		"0 * * * *":      "A cada hora, no minuto 00",
		"15 */6 * * *":   "A cada 6 horas, no minuto 15",
		"30 2 * * *":     "Todo dia às 02:30",
		"0 8,20 * * *":   "Todo dia às 08:00 e 20:00",
		"0 9 * * 1-5":    "De segunda a sexta, às 09:00",
		"0 0 * * 0":      "Todo domingo, às 00:00",
		"0 7 * * 2":      "Toda terça, às 07:00",
		"0 3 1 * *":      "Todo dia 1 do mês, às 03:00",
		"@daily":         "Todo dia às 00:00",
		"7,22 1-3 * * *": "7,22 1-3 * * *", // sem padrão conhecido → expressão crua
	}
	for expr, want := range cases {
		if got := describeCronSchedule(expr); got != want {
			t.Errorf("%q: %q, want %q", expr, got, want)
		}
	}
}

func TestApplyCronJobScheduleMissed(t *testing.T) {
	now := time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)
	mk := func(schedule string, last time.Time, suspend bool) *batchv1.CronJob {
		cj := &batchv1.CronJob{}
		cj.Spec.Schedule = schedule
		cj.Spec.Suspend = &suspend
		cj.CreationTimestamp = metav1.NewTime(now.AddDate(0, -1, 0))
		cj.Status.LastScheduleTime = &metav1.Time{Time: last}
		return cj
	}
	// a cada 15 min, última às 09:00 → esperada 09:15, já passou → atrasado
	var r CronJobResponse
	applyCronJobSchedule(&r, mk("*/15 * * * *", now.Add(-time.Hour), false), now)
	if !r.Missed || r.MissedSince == nil || *r.MissedSince != "2026-10-07T09:15:00Z" {
		t.Errorf("deveria estar atrasado desde 09:15: %+v", r)
	}
	if r.NextScheduleTime == nil || *r.NextScheduleTime != "2026-10-07T10:15:00Z" || r.TimeZone != "UTC" {
		t.Errorf("próxima/fuso: %v %q", r.NextScheduleTime, r.TimeZone)
	}
	// diário às 02:00, última hoje às 02:00 → próxima amanhã, não atrasado
	r = CronJobResponse{}
	applyCronJobSchedule(&r, mk("0 2 * * *", time.Date(2026, 10, 7, 2, 0, 0, 0, time.UTC), false), now)
	if r.Missed {
		t.Errorf("não deveria estar atrasado: %+v", r)
	}
	// suspenso nunca conta como atrasado
	r = CronJobResponse{}
	applyCronJobSchedule(&r, mk("*/15 * * * *", now.Add(-time.Hour), true), now)
	if r.Missed {
		t.Errorf("suspenso não deveria estar atrasado")
	}
	// expressão inválida não derruba: só preenche o erro
	r = CronJobResponse{}
	applyCronJobSchedule(&r, mk("bla", now, false), now)
	if r.ScheduleError == "" || r.NextScheduleTime != nil {
		t.Errorf("expressão inválida: %+v", r)
	}
}

func TestApplyCronJobJobs(t *testing.T) {
	now := time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)
	job := func(name string, created time.Time, cond batchv1.JobConditionType, finished time.Time) batchv1.Job {
		j := batchv1.Job{}
		j.Name = name
		j.CreationTimestamp = metav1.NewTime(created)
		j.Status.StartTime = &metav1.Time{Time: created}
		if cond != "" {
			j.Status.Conditions = []batchv1.JobCondition{{Type: cond, Status: corev1.ConditionTrue, LastTransitionTime: metav1.NewTime(finished)}}
		}
		return j
	}
	jobs := []batchv1.Job{
		job("a", now.Add(-3*time.Hour), batchv1.JobComplete, now.Add(-3*time.Hour+90*time.Second)),
		job("b", now.Add(-2*time.Hour), batchv1.JobFailed, now.Add(-2*time.Hour+30*time.Second)),
		job("c", now.Add(-10*time.Minute), "", time.Time{}), // rodando
	}
	var r CronJobResponse
	applyCronJobJobs(&r, jobs, now)
	if r.HistorySucceeded != 1 || r.HistoryFailed != 1 {
		t.Errorf("contagens: %d/%d", r.HistorySucceeded, r.HistoryFailed)
	}
	if r.LastJob == nil || r.LastJob.Name != "c" || r.LastJob.Status != "Running" || r.LastJob.DurationSeconds != 600 {
		t.Errorf("último job: %+v", r.LastJob)
	}
	r = CronJobResponse{}
	applyCronJobJobs(&r, jobs[:2], now)
	if r.LastJob.Name != "b" || r.LastJob.Status != "Failed" || r.LastJob.DurationSeconds != 30 || r.LastJob.CompletionTime == nil {
		t.Errorf("último job falho: %+v", r.LastJob)
	}
}
