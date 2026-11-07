package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"k8s-hpa-manager/internal/history"
)

// Snapshot de um PVC pelo modal Volumes. O projeto não usa dynamic client: o VolumeSnapshot (CRD do
// snapshot-controller) é lido e criado pela REST bruta, na versão da API que o cluster serve.

const snapshotGroup = "snapshot.storage.k8s.io"

// snapshotVersions: do mais novo ao mais antigo que o código sabe usar (mesmo formato de objeto).
var snapshotVersions = []string{"v1", "v1beta1"}

// parseSnapshotVersion escolhe a versão a usar a partir de GET /apis/snapshot.storage.k8s.io.
func parseSnapshotVersion(raw []byte) string {
	var g struct {
		Versions []struct {
			Version string `json:"version"`
		} `json:"versions"`
	}
	if json.Unmarshal(raw, &g) != nil {
		return ""
	}
	served := map[string]bool{}
	for _, v := range g.Versions {
		served[v.Version] = true
	}
	for _, v := range snapshotVersions {
		if served[v] {
			return v
		}
	}
	return ""
}

// detectSnapshotVersion: "" = API de snapshot ausente (sem CRDs/controller) ou sem permissão.
func detectSnapshotVersion(ctx context.Context, client kubernetes.Interface) string {
	raw, err := client.CoreV1().RESTClient().Get().AbsPath("/apis/" + snapshotGroup).DoRaw(ctx)
	if err != nil {
		return ""
	}
	return parseSnapshotVersion(raw)
}

type SnapshotClassInfo struct {
	Name           string `json:"name"`
	Driver         string `json:"driver"`
	Default        bool   `json:"default"`
	DeletionPolicy string `json:"deletion_policy,omitempty"`
}

func parseSnapshotClasses(raw []byte) []SnapshotClassInfo {
	var l struct {
		Items []struct {
			Metadata struct {
				Name        string            `json:"name"`
				Annotations map[string]string `json:"annotations"`
			} `json:"metadata"`
			Driver         string `json:"driver"`
			DeletionPolicy string `json:"deletionPolicy"`
		} `json:"items"`
	}
	if json.Unmarshal(raw, &l) != nil {
		return nil
	}
	out := make([]SnapshotClassInfo, 0, len(l.Items))
	for _, c := range l.Items {
		out = append(out, SnapshotClassInfo{
			Name: c.Metadata.Name, Driver: c.Driver, DeletionPolicy: c.DeletionPolicy,
			Default: c.Metadata.Annotations["snapshot.storage.kubernetes.io/is-default-class"] == "true",
		})
	}
	return out
}

// pvDriver: driver CSI do PV; volumes in-tree (migrados para CSI) mapeiam para o driver equivalente.
func pvDriver(pv corev1.PersistentVolume) string {
	sp := pv.Spec
	switch {
	case sp.CSI != nil:
		return sp.CSI.Driver
	case sp.AzureDisk != nil:
		return "disk.csi.azure.com"
	case sp.AzureFile != nil:
		return "file.csi.azure.com"
	case sp.AWSElasticBlockStore != nil:
		return "ebs.csi.aws.com"
	case sp.GCEPersistentDisk != nil:
		return "pd.csi.storage.gke.io"
	}
	return ""
}

func compatibleClasses(classes []SnapshotClassInfo, driver string) []SnapshotClassInfo {
	out := []SnapshotClassInfo{}
	for _, c := range classes {
		if c.Driver == driver {
			out = append(out, c)
		}
	}
	return out
}

// pickSnapshotClass: a pedida (que precisa ser do driver do volume) ou a única/padrão compatível.
func pickSnapshotClass(classes []SnapshotClassInfo, driver, requested string) (SnapshotClassInfo, error) {
	ok := compatibleClasses(classes, driver)
	if requested != "" {
		for _, c := range ok {
			if c.Name == requested {
				return c, nil
			}
		}
		return SnapshotClassInfo{}, fmt.Errorf("a VolumeSnapshotClass %q não existe ou não é do driver %s", requested, driver)
	}
	for _, c := range ok {
		if c.Default {
			return c, nil
		}
	}
	switch len(ok) {
	case 0:
		return SnapshotClassInfo{}, fmt.Errorf("não há VolumeSnapshotClass para o driver %s neste cluster", driver)
	case 1:
		return ok[0], nil
	}
	return SnapshotClassInfo{}, fmt.Errorf("há %d VolumeSnapshotClasses para o driver %s e nenhuma padrão: escolha uma", len(ok), driver)
}

// snapshotName: "<pvc>-snap-AAAAMMDD-HHMMSS" (UTC), limitado a 253 caracteres.
func snapshotName(pvc string, t time.Time) string {
	suffix := "-snap-" + t.UTC().Format("20060102-150405")
	if max := 253 - len(suffix); len(pvc) > max {
		pvc = pvc[:max]
	}
	return strings.TrimRight(pvc, "-.") + suffix
}

func snapshotBody(version, ns, name, class, pvc string) ([]byte, error) {
	return json.Marshal(map[string]any{
		"apiVersion": snapshotGroup + "/" + version,
		"kind":       "VolumeSnapshot",
		"metadata":   map[string]any{"name": name, "namespace": ns, "labels": map[string]string{"app.kubernetes.io/managed-by": "k8s-hpa-manager"}},
		"spec":       map[string]any{"volumeSnapshotClassName": class, "source": map[string]any{"persistentVolumeClaimName": pvc}},
	})
}

// snapshotContext lê o PVC (precisa estar Bound), o PV e as classes de snapshot do cluster.
func snapshotContext(ctx context.Context, client kubernetes.Interface, ns, pvcName string) (version, driver string, classes []SnapshotClassInfo, err error) {
	if version = detectSnapshotVersion(ctx, client); version == "" {
		return "", "", nil, fmt.Errorf("este cluster não expõe a API %s (snapshot-controller e CRDs não instalados, ou sem permissão)", snapshotGroup)
	}
	pvc, err := client.CoreV1().PersistentVolumeClaims(ns).Get(ctx, pvcName, metav1.GetOptions{})
	if err != nil {
		return "", "", nil, err
	}
	if pvc.Status.Phase != corev1.ClaimBound {
		return "", "", nil, fmt.Errorf("o PVC está %s; só PVC Bound pode ter snapshot", pvc.Status.Phase)
	}
	pv, err := client.CoreV1().PersistentVolumes().Get(ctx, pvc.Spec.VolumeName, metav1.GetOptions{})
	if err != nil {
		return "", "", nil, err
	}
	if driver = pvDriver(*pv); driver == "" {
		return "", "", nil, fmt.Errorf("este tipo de volume não suporta snapshot pelo Kubernetes (NFS, HostPath e Local ficam de fora)")
	}
	raw, err := client.CoreV1().RESTClient().Get().AbsPath("/apis/" + snapshotGroup + "/" + version + "/volumesnapshotclasses").DoRaw(ctx)
	if err != nil {
		return "", "", nil, fmt.Errorf("não consegui listar as VolumeSnapshotClasses: %w", err)
	}
	return version, driver, parseSnapshotClasses(raw), nil
}

// GetSnapshotInfo: versão da API, driver do volume e classes compatíveis (para o diálogo da tela).
func (h *NodePoolHandler) GetSnapshotInfo(c *gin.Context) {
	cluster, ns, name := c.Query("cluster"), c.Query("namespace"), c.Query("name")
	if cluster == "" || ns == "" || name == "" {
		volumeActionFail(c, http.StatusBadRequest, "INVALID_REQUEST", fmt.Errorf("cluster, namespace e name são obrigatórios"))
		return
	}
	client, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		volumeActionFail(c, http.StatusInternalServerError, "KUBERNETES_CLIENT_ERROR", err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	version, driver, classes, err := snapshotContext(ctx, client, ns, name)
	if err != nil {
		volumeActionFail(c, http.StatusUnprocessableEntity, "SNAPSHOT_UNAVAILABLE", err)
		return
	}
	def := ""
	if p, err := pickSnapshotClass(classes, driver, ""); err == nil {
		def = p.Name
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"api_version": version, "driver": driver, "classes": compatibleClasses(classes, driver), "suggested": def}})
}

// CreateSnapshot cria um VolumeSnapshot do PVC. Não espera ficar pronto: a tela mostra a contagem
// de snapshots prontos na próxima leitura.
func (h *NodePoolHandler) CreateSnapshot(c *gin.Context) {
	start := time.Now()
	var req struct {
		Cluster       string `json:"cluster"`
		Namespace     string `json:"namespace"`
		Name          string `json:"name"`
		SnapshotClass string `json:"snapshot_class"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Cluster == "" || req.Namespace == "" || req.Name == "" {
		volumeActionFail(c, http.StatusBadRequest, "INVALID_REQUEST", fmt.Errorf("cluster, namespace e name são obrigatórios"))
		return
	}
	client, err := h.kubeManager.GetClient(req.Cluster)
	if err != nil {
		volumeActionFail(c, http.StatusInternalServerError, "KUBERNETES_CLIENT_ERROR", err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	version, driver, classes, err := snapshotContext(ctx, client, req.Namespace, req.Name)
	if err != nil {
		volumeActionFail(c, http.StatusUnprocessableEntity, "SNAPSHOT_UNAVAILABLE", err)
		return
	}
	class, err := pickSnapshotClass(classes, driver, req.SnapshotClass)
	if err != nil {
		volumeActionFail(c, http.StatusUnprocessableEntity, "SNAPSHOT_UNAVAILABLE", err)
		return
	}
	snap := snapshotName(req.Name, time.Now())
	body, _ := snapshotBody(version, req.Namespace, snap, class.Name, req.Name)
	_, err = client.CoreV1().RESTClient().Post().AbsPath("/apis/"+snapshotGroup+"/"+version+"/namespaces/"+req.Namespace+"/volumesnapshots").
		SetHeader("Content-Type", "application/json").Body(body).DoRaw(ctx)
	logOperation(c, h.historyTracker, history.ActionSnapshotVolume, req.Namespace+"/"+req.Name, req.Cluster, nil,
		map[string]interface{}{"snapshot": snap, "class": class.Name, "api": version}, start, err)
	if err != nil {
		volumeActionFail(c, http.StatusBadGateway, "KUBERNETES_API_ERROR", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"snapshot": snap, "class": class.Name, "api_version": version, "deletion_policy": class.DeletionPolicy}})
}
