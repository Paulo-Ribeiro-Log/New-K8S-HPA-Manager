package handlers

import (
	"context"
	"fmt"
	"strings"

	"k8s-hpa-manager/internal/storage"
)

// volumeSourceType marca no registro de Dependencies as linhas que vieram de volumes (PV/PVC/NFS),
// em vez de ConfigMap/Secret/env.
const volumeSourceType = "pvc"

// VolumeDependencies lista, para o registro da aba Dependencies, o storage externo que o cluster
// monta por volume: Azure Files, Blob (CSI) e NFS/EFS. Não usa Prometheus nem custo.
func (h *NodePoolHandler) VolumeDependencies(ctx context.Context, cluster string) ([]storage.DependencyRecord, error) {
	data, verr := h.collectVolumes(ctx, cluster, false, false)
	if verr != nil {
		return nil, fmt.Errorf("%s", verr.Message)
	}
	return volumeDependencyRecords(cluster, data.Items), nil
}

// volumeDependencyRecords converte volumes em registros de dependência. O nome do serviço segue o
// que o scanner de texto grava (<conta>.blob.core.windows.net), para a busca por conta juntar
// quem a referencia por configuração e quem a monta por volume. Uma linha por aplicação que usa o
// volume; volume sem aplicação vira uma linha sem deployment (referência órfã). O nome do PV
// (pvc-<uuid> nos provisionados dinamicamente) vai em source_key, que a busca também consulta.
func volumeDependencyRecords(cluster string, items []VolumeItem) []storage.DependencyRecord {
	var out []storage.DependencyRecord
	for _, it := range items {
		host, topic := volumeStorageTarget(it)
		if host == "" {
			continue
		}
		name := it.PVC
		if name == "" {
			name = it.PV
		}
		if name == "" {
			name = it.SourceDetail
		}
		ns := it.Namespace
		if ns == "" {
			ns = "-" // PV sem PVC não tem namespace
		}
		workloads := it.Workloads
		if len(workloads) == 0 {
			workloads = []string{""}
		}
		for _, w := range workloads {
			dep := w
			if i := strings.Index(w, "/"); i >= 0 {
				dep = w[i+1:]
			}
			key := w
			if it.PV != "" {
				key = "PV " + it.PV
				if w != "" {
					key += " · " + w
				}
			}
			out = append(out, storage.DependencyRecord{
				ServiceName: host,
				ServiceType: "storage",
				TopicName:   topic,
				Cluster:     cluster,
				Namespace:   ns,
				Deployment:  dep,
				SourceType:  volumeSourceType,
				SourceName:  name,
				SourceKey:   key,
			})
		}
	}
	return out
}

// volumeStorageTarget devolve o host e o share/container/caminho do storage externo do volume, ou
// "" quando o volume não tem backend identificável.
func volumeStorageTarget(it VolumeItem) (host, topic string) {
	if s := it.Storage; s != nil && s.Account != "" {
		kind := "file"
		if strings.Contains(strings.ToLower(s.Driver), "blob") {
			kind = "blob"
		}
		return strings.ToLower(s.Account) + "." + kind + ".core.windows.net", s.Share
	}
	// Discos (Azure Disk, EBS, GCE PD): o "serviço" é o disco; o tópico diz o tipo.
	switch {
	case it.Source == "Azure Disk" || it.Source == "EBS" || it.Source == "GCE PD":
		id := ""
		if it.Storage != nil {
			id = it.Storage.ResourceID
		}
		if id == "" {
			id = it.SourceDetail
		}
		if i := strings.LastIndex(id, "/"); i >= 0 {
			id = id[i+1:]
		}
		if id == "" {
			id = it.PV
		}
		if id == "" {
			return "", ""
		}
		return id, it.Source
	}
	if strings.HasPrefix(it.Source, "NFS") || strings.HasPrefix(it.Source, "EFS") {
		h, p, _ := strings.Cut(it.SourceDetail, ":")
		if h == "" {
			return "", ""
		}
		return h, strings.TrimPrefix(p, ":")
	}
	return "", ""
}
