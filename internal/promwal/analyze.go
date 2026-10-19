package promwal

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	// memoryCeiling é o limit a partir do qual subir mais memória deixa de ser a recomendação: um
	// Prometheus que ainda morre no replay com 8Gi precisa de WAL limpo (ou investigação de cardinalidade).
	memoryCeiling = int64(8) << 30
	// walTooSmall: WAL menor que isso não explica um OOM no replay.
	walTooSmall = int64(256) << 20
)

var segmentRe = regexp.MustCompile(`segment=(\d+)\s+maxSegment=(\d+)`)

var corruptionMarkers = []string{"corrupt", "wal read error", "unexpected end of segment"}

// logState resume o que o log do Prometheus diz sobre a etapa em que ele morreu.
type logState struct {
	replaying  bool // chegou a iniciar o replay do WAL
	ready      bool // chegou a servir (replay terminou)
	corruption bool
	segment    string // "N de M", do último "WAL segment loaded"
}

func parseLog(tail string) logState {
	var s logState
	lower := strings.ToLower(tail)
	s.replaying = strings.Contains(lower, "replaying wal") || strings.Contains(lower, "wal segment loaded")
	s.ready = strings.Contains(lower, "server is ready to receive web requests")
	for _, m := range corruptionMarkers {
		if strings.Contains(lower, m) {
			s.corruption = true
		}
	}
	if all := segmentRe.FindAllStringSubmatch(tail, -1); len(all) > 0 {
		last := all[len(all)-1]
		s.segment = last[1] + " de " + last[2]
	}
	return s
}

func isOOM(p PodInfo) bool { return p.LastTermReason == "OOMKilled" || p.LastExitCode == 137 }

// Analyze produz o relatório a partir do que foi coletado. Não acessa o cluster.
func Analyze(in Input, now time.Time) Report {
	r := Report{
		Cluster: in.Cluster, Namespace: in.Namespace, Name: in.Name,
		Events: in.Events, CreatedAt: now,
	}
	r.Recurrence = BuildRecurrence(in.CleanupDates, in.HistoryCleanups, in.CleanupCount, now)
	deepByPod := map[string]DeepInfo{}
	for _, d := range in.Deep {
		deepByPod[d.Pod] = d
		r.Deep = append(r.Deep, deepReport(d, now))
	}

	if in.PendingRestore {
		n := in.PendingRestoreReplicas
		r.PendingRestoreReplicas = &n
		r.Verdict = VerdictInvestigate
		r.Summary = fmt.Sprintf("Uma limpeza anterior desligou o Prometheus e não chegou a religar (réplicas originais: %d). Restaure antes de analisar de novo.", n)
		r.Warnings = recurrenceWarnings(r.Recurrence, now)
		return r
	}

	if len(in.Pods) == 0 {
		r.Verdict = VerdictInvestigate
		r.Summary = "Nenhum pod do Prometheus encontrado (replicas 0, pods ainda não criados ou selector diferente do padrão do operator)."
		return r
	}

	var healthy []string
	for _, p := range in.Pods {
		pr := analyzePod(in, p)
		if pr.Verdict == VerdictHealthy {
			healthy = append(healthy, p.Name)
		}
		if pr.Verdict == VerdictCleanWAL {
			r.AffectedReplicas = append(r.AffectedReplicas, p.Name)
		}
		r.Pods = append(r.Pods, pr)
	}

	r.Verdict = worst(r.Pods)
	switch r.Verdict {
	case VerdictHealthy:
		r.Summary = "Todos os pods do Prometheus estão Ready; não há o que limpar."
	case VerdictIncreaseMemory:
		r.SuggestedMemory = suggestMemory(in.MemoryLimitBytes)
		r.Summary = fmt.Sprintf("O Prometheus está sendo morto por falta de memória (limit atual %s). Suba o limit antes de pensar em apagar dados.",
			formatBytes(in.MemoryLimitBytes))
	case VerdictCleanWAL:
		r.Summary = "O Prometheus não consegue concluir o replay do WAL e subir mais memória não resolve (ou o WAL está corrompido). Limpar o WAL é indicado — as métricas que ainda não viraram bloco serão perdidas."
	default:
		r.Summary = "Há um problema, mas a causa não parece ser o WAL. Veja as evidências e os eventos."
	}

	r.Warnings = append(recurrenceWarnings(r.Recurrence, now), warnings(in, r, healthy, deepByPod)...)
	return r
}

func analyzePod(in Input, p PodInfo) PodReport {
	pr := PodReport{Pod: p.Name, PVC: p.PVC}
	if p.Restarts > 0 {
		pr.Evidence = append(pr.Evidence, fmt.Sprintf("%d restart(s)", p.Restarts))
	}
	if p.Ready {
		pr.Verdict, pr.Cause = VerdictHealthy, "Ready"
		return pr
	}

	st := parseLog(p.LogTail)
	if p.LastTermReason != "" {
		pr.Evidence = append(pr.Evidence, fmt.Sprintf("última terminação: %s (exit %d)", p.LastTermReason, p.LastExitCode))
	}
	if p.WaitingReason != "" {
		pr.Evidence = append(pr.Evidence, "estado: "+p.WaitingReason)
	}
	if st.segment != "" {
		pr.Evidence = append(pr.Evidence, "replay do WAL chegou ao segmento "+st.segment)
	}

	switch {
	case st.corruption:
		pr.Verdict, pr.Cause = VerdictCleanWAL, "WAL corrompido (o log registra erro de leitura/corrupção)"
		pr.Evidence = append(pr.Evidence, "log com marcador de corrupção do WAL")
	case isOOM(p) && st.ready:
		// Morreu depois de servir: o WAL não é a causa, apagá-lo não resolve.
		pr.Verdict, pr.Cause = VerdictIncreaseMemory, "OOM em operação normal (depois do replay): falta memória, não é problema de WAL"
	case isOOM(p) && in.MemoryLimitBytes == 0:
		pr.Verdict, pr.Cause = VerdictInvestigate, "OOM sem memory limit definido: o node ficou sem memória; veja a pressão do node antes de mexer no WAL"
	case isOOM(p) && in.MemoryLimitBytes < memoryCeiling:
		pr.Verdict, pr.Cause = VerdictIncreaseMemory, "OOM"+replayNote(st)+" com limit baixo: suba a memória antes de apagar dados"
	case isOOM(p):
		pr.Verdict, pr.Cause = VerdictCleanWAL, "OOM"+replayNote(st)+" mesmo com limit de "+formatBytes(in.MemoryLimitBytes)+": o WAL é grande demais para o replay"
	default:
		pr.Verdict, pr.Cause = VerdictInvestigate, "o pod não está Ready e não há OOM nem corrupção de WAL no log"
		if p.WaitingReason != "" {
			pr.Cause += " (" + p.WaitingReason + ")"
		}
	}
	return pr
}

func replayNote(st logState) string {
	if st.replaying {
		return " durante o replay do WAL"
	}
	return ""
}

// worst escolhe o veredito global: o que pede ação mais destrutiva só prevalece sobre os mais brandos
// quando de fato indicado (limpar > memória > investigar > saudável).
func worst(pods []PodReport) Verdict {
	rank := map[Verdict]int{VerdictHealthy: 0, VerdictInvestigate: 1, VerdictIncreaseMemory: 2, VerdictCleanWAL: 3}
	v := VerdictHealthy
	for _, p := range pods {
		if rank[p.Verdict] > rank[v] {
			v = p.Verdict
		}
	}
	return v
}

func warnings(in Input, r Report, healthy []string, deepByPod map[string]DeepInfo) []string {
	var w []string
	if in.HelmManaged || in.ArgoManaged {
		w = append(w, "O recurso Prometheus é gerenciado por Helm/Argo CD: com sync automático, o replicas 0 da limpeza será revertido. Pause o sync antes.")
	}
	if len(healthy) > 0 && r.Verdict != VerdictHealthy {
		w = append(w, fmt.Sprintf("Há réplica(s) saudável(is) (%s) que podem ter os dados: avalie limpar só a réplica com problema.", strings.Join(healthy, ", ")))
	}
	if in.ThanosSidecar {
		w = append(w, "Há sidecar do Thanos: confira se os blocos já foram enviados ao object storage; só o que está no WAL é perdido.")
	}
	if r.Verdict == VerdictCleanWAL {
		var missing []string
		for _, pod := range r.AffectedReplicas {
			d, ok := deepByPod[pod]
			if !ok {
				missing = append(missing, pod)
				continue
			}
			if d.WALBytes > 0 && d.WALBytes < walTooSmall {
				w = append(w, fmt.Sprintf("O WAL de %s tem só %s: é pequeno para explicar um OOM no replay; confira cardinalidade/limits antes de apagar.", pod, formatBytes(d.WALBytes)))
			}
			if d.LastBlockMaxTime.IsZero() {
				w = append(w, fmt.Sprintf("%s não tem nenhum bloco compactado: TODO o histórico está no WAL e seria perdido.", pod))
			}
		}
		if len(missing) > 0 {
			w = append(w, "Janela de dados perdidos desconhecida para "+strings.Join(missing, ", ")+": rode a análise profunda (monta o PVC read-only) antes de limpar.")
		}
	}
	return w
}

func deepReport(d DeepInfo, now time.Time) DeepReport {
	dr := DeepReport{Pod: d.Pod, PVC: d.PVC, Blocks: d.Blocks, WALBytes: d.WALBytes, ChunksHeadBytes: d.ChunksHeadBytes}
	if !d.LastBlockMaxTime.IsZero() {
		t := d.LastBlockMaxTime
		dr.LastBlockEndsAt = &t
		dr.LostWindowHours = now.Sub(t).Hours()
		if dr.LostWindowHours < 0 {
			dr.LostWindowHours = 0
		}
	}
	return dr
}

// suggestMemory dobra o limit, limitado ao teto, arredondando para Mi. Sem limit conhecido, não sugere.
func suggestMemory(limit int64) string {
	if limit <= 0 {
		return ""
	}
	next := limit * 2
	if next > memoryCeiling {
		next = memoryCeiling
	}
	next = (next + (1<<20 - 1)) &^ (1<<20 - 1)
	return resource.NewQuantity(next, resource.BinarySI).String()
}

func formatBytes(b int64) string {
	if b <= 0 {
		return "não definido"
	}
	return resource.NewQuantity(b, resource.BinarySI).String()
}

func recurrenceWarnings(rec Recurrence, now time.Time) []string {
	if w := RecurrenceWarning(rec, now); w != "" {
		return []string{w}
	}
	return nil
}
