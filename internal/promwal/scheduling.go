package promwal

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	schedTotalRe    = regexp.MustCompile(`0/(\d+) nodes are available`)
	schedTaintRe    = regexp.MustCompile(`(\d+) node\(s\) had (?:untolerated )?taints?`)
	schedAffinityRe = regexp.MustCompile(`(\d+) node\(s\) didn't match Pod's node affinity/selector`)
	schedResourceRe = regexp.MustCompile(`(\d+) Insufficient (cpu|memory)`)
	schedVolumeRe   = regexp.MustCompile(`(\d+) node\(s\) had volume node affinity conflict`)
)

// schedulingCause traduz a mensagem do scheduler (condição PodScheduled=False, a mesma do evento
// FailedScheduling) em uma causa legível e diz se vale apontar tolerations/nodeSelector/affinity. Mensagem sem
// padrão conhecido volta como veio (o texto do scheduler já é informativo).
func schedulingCause(msg string) (cause string, placement bool) {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return "", false
	}
	var parts []string
	if m := schedTaintRe.FindStringSubmatch(msg); m != nil {
		parts = append(parts, m[1]+" node(s) com taint que o pod não tolera")
		placement = true
	}
	if m := schedAffinityRe.FindStringSubmatch(msg); m != nil {
		parts = append(parts, m[1]+" node(s) não batem com o nodeSelector/nodeAffinity do pod")
		placement = true
	}
	for _, m := range schedResourceRe.FindAllStringSubmatch(msg, -1) {
		parts = append(parts, fmt.Sprintf("%s node(s) sem %s livre", m[1], m[2]))
	}
	if m := schedVolumeRe.FindStringSubmatch(msg); m != nil {
		parts = append(parts, m[1]+" node(s) em zona diferente do volume (PVC)")
	}
	if strings.Contains(msg, "unbound immediate PersistentVolumeClaims") {
		parts = append(parts, "PVC ainda não vinculado")
	}
	if len(parts) == 0 {
		return "o pod não foi agendado: " + msg, false
	}
	prefix := "o pod não foi agendado"
	if m := schedTotalRe.FindStringSubmatch(msg); m != nil {
		prefix += fmt.Sprintf(" (nenhum dos %s nodes serve)", m[1])
	}
	return prefix + ": " + strings.Join(parts, "; "), placement
}

const placementHint = "verifique tolerations, nodeSelector e affinity do recurso Prometheus e se o node pool que o atende está ativo (escala/horário)"
