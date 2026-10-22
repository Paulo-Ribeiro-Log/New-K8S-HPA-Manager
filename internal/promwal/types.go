// Package promwal analisa um Prometheus (prometheus-operator) que não sobe e diz se limpar o WAL é a
// solução certa. A análise é só leitura: o único recurso criado no cluster é o pod temporário da
// análise profunda (PVC montado read-only), removido ao final.
package promwal

import (
	"time"

	corev1 "k8s.io/api/core/v1"
)

// Verdict é o resultado da análise, do menos para o mais destrutivo.
type Verdict string

const (
	VerdictHealthy        Verdict = "saudavel"
	VerdictIncreaseMemory Verdict = "aumentar_memoria"
	VerdictCleanWAL       Verdict = "limpar_wal"
	VerdictInvestigate    Verdict = "investigar"
)

// PodInfo é o que a coleta lê de cada pod do Prometheus.
type PodInfo struct {
	Name           string
	Node           string
	PVC            string // claim montado pelo pod (vazio se não achou)
	Ready          bool
	Phase          string
	WaitingReason  string // CrashLoopBackOff, ImagePullBackOff, ContainerCreating…
	Restarts       int32
	LastTermReason string // OOMKilled, Error…
	LastExitCode   int32
	// SchedulingMessage: motivo do scheduler quando o pod não foi agendado (condição PodScheduled=False).
	SchedulingMessage string
	// Restrições de agendamento do pod (para o diagnóstico por node de um pod não agendado).
	NodeSelector map[string]string
	NodeAffinity *corev1.NodeSelector
	Tolerations  []corev1.Toleration
	LogTail      string // últimas linhas do container anterior (ou do atual, se não houve anterior)
	// Requests do pod (soma dos containers; init containers pelo maior): o que o scheduler precisa achar livre.
	CPURequestMilli int64
	MemRequestBytes int64
}

// Requests é a soma dos requests dos pods que já ocupam um node.
type Requests struct {
	CPUMilli int64
	MemBytes int64
}

// PlacementFacts são os dados extras lidos só quando há pod não agendado, para validar as correções de
// agendamento além de nodeSelector/taints: recursos livres por node e a zona do volume.
type PlacementFacts struct {
	NodeRequests map[string]Requests // node -> soma dos requests dos pods ativos nele
	RequestsErr  string              // por que não foi possível somar (ex.: sem permissão para listar pods)
	PVCZone      string              // zona do volume já criado ("" = sem restrição conhecida)
	PVCNote      string              // PVC ainda não existe / não foi possível ler o volume
}

// DeepInfo é o que a análise profunda leu do PVC (somente leitura).
type DeepInfo struct {
	Pod              string
	PVC              string
	Blocks           int
	LastBlockMaxTime time.Time // zero = nenhum bloco compactado
	WALBytes         int64
	ChunksHeadBytes  int64
}

// Input reúne tudo o que foi coletado; Analyze não acessa o cluster.
type Input struct {
	Cluster   string
	Namespace string
	Name      string // recurso Prometheus (CRD)

	Replicas         int32
	MemoryLimitBytes int64
	PVCSizeBytes     int64
	HelmManaged      bool
	ArgoManaged      bool
	ThanosSidecar    bool
	// PendingRestore: uma limpeza anterior desligou o Prometheus e não chegou a religar (anotação no CRD).
	PendingRestore         bool
	PendingRestoreReplicas int32
	// Reincidência: contador acumulado e datas da anotação persistente, mais as datas do Change History
	// (preenchidas pelo handler: o pacote não depende do history).
	CleanupCount    int
	CleanupDates    []time.Time
	HistoryCleanups []time.Time

	Pods   []PodInfo
	Events []string // mensagens de eventos Warning recentes
	Deep   []DeepInfo
	// DeepSkipped: pods que ficaram fora da análise profunda por não terem nó (sem onde montar o PVC).
	DeepSkipped []string
	// Nodes do cluster, lidos só quando há pod não agendado (diagnóstico por node); NodesError se a leitura falhou.
	Nodes      []corev1.Node
	NodesError string
	Facts      PlacementFacts
}

// PodReport é o diagnóstico de um pod.
type PodReport struct {
	Pod      string   `json:"pod"`
	PVC      string   `json:"pvc,omitempty"`
	Verdict  Verdict  `json:"verdict"`
	Cause    string   `json:"cause"`
	Evidence []string `json:"evidence,omitempty"`
	// Placement: diagnóstico node a node quando o pod não foi agendado.
	Placement *Placement `json:"placement,omitempty"`
}

// DeepReport é a análise profunda de um PVC, pronta para a UI.
type DeepReport struct {
	Pod             string     `json:"pod"`
	PVC             string     `json:"pvc"`
	Blocks          int        `json:"blocks"`
	LastBlockEndsAt *time.Time `json:"last_block_ends_at,omitempty"`
	LostWindowHours float64    `json:"lost_window_hours"` // do fim do último bloco até agora
	WALBytes        int64      `json:"wal_bytes"`
	ChunksHeadBytes int64      `json:"chunks_head_bytes"`
}

// Report é a resposta da análise.
type Report struct {
	ID        string       `json:"analysis_id"`
	Cluster   string       `json:"cluster"`
	Namespace string       `json:"namespace"`
	Name      string       `json:"name"`
	Verdict   Verdict      `json:"verdict"`
	Summary   string       `json:"summary"`
	Pods      []PodReport  `json:"pods"`
	Warnings  []string     `json:"warnings,omitempty"`
	Events    []string     `json:"events,omitempty"`
	Deep      []DeepReport `json:"deep,omitempty"`
	// Recurrence: limpezas anteriores (anotação persistente + Change History). Recurrent = 2 ou mais em 30 dias.
	Recurrence Recurrence `json:"recurrence"`
	// SuggestedMemory só vem com o veredito aumentar_memoria: a ferramenta apenas informa; a alteração é
	// feita no editor de recursos da aba (PUT /prometheus/:cluster/:namespace/:type/:name).
	SuggestedMemory string `json:"suggested_memory,omitempty"`
	// PendingRestoreReplicas != nil: o Prometheus foi desligado por uma limpeza interrompida; é preciso
	// religar (POST /prometheus/wal/restore) antes de qualquer outra coisa.
	PendingRestoreReplicas *int32 `json:"pending_restore_replicas,omitempty"`
	// AffectedReplicas são os pods cuja limpeza do WAL seria indicada.
	AffectedReplicas []string  `json:"affected_replicas,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}
