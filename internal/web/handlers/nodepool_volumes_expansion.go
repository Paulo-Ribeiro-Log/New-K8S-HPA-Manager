package handlers

import (
	"context"
	"encoding/json"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

// Expansão e snapshots dos volumes: se a StorageClass deixa aumentar o disco (o que decide se um
// volume cheio tem saída), se alguma expansão está travada e quais PVCs têm VolumeSnapshot.

// applyExpansion preenche CanExpand pela StorageClass (allowVolumeExpansion) e aponta expansão
// em andamento ou travada pelas conditions do PVC. expandBySC vazio = StorageClasses não lidas.
func applyExpansion(items []VolumeItem, pvcs []corev1.PersistentVolumeClaim, expandBySC map[string]bool, sum *VolumesSummary) {
	byKey := map[string]corev1.PersistentVolumeClaim{}
	for _, p := range pvcs {
		byKey[p.Namespace+"/"+p.Name] = p
	}
	for i := range items {
		it := &items[i]
		if it.Kind != "pvc" {
			continue
		}
		if allow, ok := expandBySC[it.StorageClass]; ok {
			it.CanExpand = &allow
		}
		pvc, ok := byKey[it.Namespace+"/"+it.PVC]
		if !ok {
			continue
		}
		for _, c := range pvc.Status.Conditions {
			if c.Status != corev1.ConditionTrue {
				continue
			}
			switch c.Type {
			case corev1.PersistentVolumeClaimResizing:
				it.Issues = append(it.Issues, "Expansão em andamento (Resizing)")
				sum.ResizePending++
			case corev1.PersistentVolumeClaimFileSystemResizePending:
				it.Issues = append(it.Issues, "Expansão pendente: falta o pod reiniciar para crescer o filesystem")
				sum.ResizePending++
			}
		}
	}
}

// markExpandBlocked: volume cheio (ou enchendo) numa StorageClass sem expansão não tem saída
// sem migrar os dados. Roda depois do uso, que é quem classifica o volume.
func markExpandBlocked(items []VolumeItem, sum *VolumesSummary) {
	for i := range items {
		it := &items[i]
		if (it.UsageClass == "critical" || it.UsageClass == "warning") && it.CanExpand != nil && !*it.CanExpand {
			it.Issues = append(it.Issues, "StorageClass "+it.StorageClass+" não permite expandir: para crescer é preciso migrar os dados")
			sum.ExpandBlocked++
		}
	}
}

type snapInfo struct {
	Count int
	Last  time.Time
}

// fetchSnapshots lista os VolumeSnapshots (CRD do snapshot-controller) pela REST bruta — o projeto
// não usa dynamic client, na versão detectada por detectSnapshotVersion. false = API ausente ou sem permissão (não dá para afirmar "sem snapshot").
func fetchSnapshots(ctx context.Context, client kubernetes.Interface, version string) (map[string]snapInfo, bool) {
	if version == "" {
		return nil, false
	}
	raw, err := client.CoreV1().RESTClient().Get().AbsPath("/apis/" + snapshotGroup + "/" + version + "/volumesnapshots").DoRaw(ctx)
	if err != nil {
		return nil, false
	}
	return parseSnapshots(raw)
}

func parseSnapshots(raw []byte) (map[string]snapInfo, bool) {
	var list struct {
		Items []struct {
			Metadata struct {
				Namespace string `json:"namespace"`
			} `json:"metadata"`
			Spec struct {
				Source struct {
					PVC string `json:"persistentVolumeClaimName"`
				} `json:"source"`
			} `json:"spec"`
			Status struct {
				ReadyToUse   bool      `json:"readyToUse"`
				CreationTime time.Time `json:"creationTime"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, false
	}
	out := map[string]snapInfo{}
	for _, s := range list.Items {
		if s.Spec.Source.PVC == "" || !s.Status.ReadyToUse {
			continue
		}
		k := s.Metadata.Namespace + "/" + s.Spec.Source.PVC
		si := out[k]
		si.Count++
		if s.Status.CreationTime.After(si.Last) {
			si.Last = s.Status.CreationTime
		}
		out[k] = si
	}
	return out, true
}

func applySnapshots(items []VolumeItem, snaps map[string]snapInfo, sum *VolumesSummary) {
	sum.SnapshotsKnown = true
	for i := range items {
		it := &items[i]
		if it.Kind != "pvc" {
			continue
		}
		if s, ok := snaps[it.Namespace+"/"+it.PVC]; ok {
			it.Snapshots = s.Count
			it.LastSnapshot = s.Last.UTC().Format(time.RFC3339)
			sum.WithSnapshot++
		}
	}
}
