package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"k8s-hpa-manager/internal/history"
)

// Ações de escrita do modal Volumes: expandir um PVC e apagar um volume ocioso. Cada ação relê o
// objeto no cluster e revalida tudo no servidor — a tela só facilita, não é a garantia.

type volumeActionReq struct {
	Cluster   string  `json:"cluster"`
	Kind      string  `json:"kind"` // delete: "pvc" | "pv"
	Namespace string  `json:"namespace"`
	Name      string  `json:"name"` // PVC ou PV
	SizeGiB   float64 `json:"size_gib"`
	Confirm   string  `json:"confirm"` // delete: precisa repetir o nome
}

// validateExpand: só cresce, só PVC Bound e só se a StorageClass permite.
func validateExpand(pvc corev1.PersistentVolumeClaim, allowExpansion bool, newGiB float64) (resource.Quantity, error) {
	cur := pvc.Status.Capacity[corev1.ResourceStorage]
	if cur.IsZero() {
		cur = pvc.Spec.Resources.Requests[corev1.ResourceStorage]
	}
	switch {
	case pvc.Status.Phase != corev1.ClaimBound:
		return cur, fmt.Errorf("o PVC está %s; só PVC Bound pode ser expandido", pvc.Status.Phase)
	case !allowExpansion:
		return cur, fmt.Errorf("a StorageClass não permite expansão (allowVolumeExpansion)")
	case newGiB != float64(int64(newGiB)) || newGiB < 1:
		return cur, fmt.Errorf("informe o novo tamanho em GiB inteiros")
	}
	want := resource.MustParse(fmt.Sprintf("%dGi", int64(newGiB)))
	if want.Cmp(cur) <= 0 {
		return cur, fmt.Errorf("o novo tamanho (%s) precisa ser maior que o atual (%s): disco não reduz", want.String(), cur.String())
	}
	return want, nil
}

// podsUsingPVC: pods (qualquer fase ativa) que montam o PVC.
func podsUsingPVC(pods []corev1.Pod, ns, name string) []string {
	var out []string
	for _, p := range pods {
		if p.Namespace != ns || p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed {
			continue
		}
		for _, v := range p.Spec.Volumes {
			if v.PersistentVolumeClaim != nil && v.PersistentVolumeClaim.ClaimName == name {
				out = append(out, p.Name)
				break
			}
		}
	}
	return out
}

// validateDeletePV: só PV sem uso (Released ou Available); um Bound ainda tem PVC.
func validateDeletePV(pv corev1.PersistentVolume) error {
	if pv.Status.Phase != corev1.VolumeReleased && pv.Status.Phase != corev1.VolumeAvailable {
		return fmt.Errorf("o PV está %s; só PV Released ou Available (sem PVC) pode ser apagado", pv.Status.Phase)
	}
	return nil
}

func (h *NodePoolHandler) volumeActionSetup(c *gin.Context) (volumeActionReq, bool) {
	var req volumeActionReq
	if err := c.ShouldBindJSON(&req); err != nil || req.Cluster == "" || req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": gin.H{"code": "INVALID_REQUEST", "message": "cluster e name são obrigatórios"}})
		return req, false
	}
	return req, true
}

func volumeActionFail(c *gin.Context, status int, code string, err error) {
	if checkForbidden(c, err) {
		return
	}
	c.JSON(status, gin.H{"success": false, "error": gin.H{"code": code, "message": err.Error()}})
}

// ExpandVolume aumenta o tamanho pedido de um PVC (patch em spec.resources.requests.storage).
func (h *NodePoolHandler) ExpandVolume(c *gin.Context) {
	start := time.Now()
	req, ok := h.volumeActionSetup(c)
	if !ok {
		return
	}
	if req.Namespace == "" {
		volumeActionFail(c, http.StatusBadRequest, "INVALID_REQUEST", fmt.Errorf("namespace é obrigatório"))
		return
	}
	client, err := h.kubeManager.GetClient(req.Cluster)
	if err != nil {
		volumeActionFail(c, http.StatusInternalServerError, "KUBERNETES_CLIENT_ERROR", err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	resName := req.Namespace + "/" + req.Name

	pvc, err := client.CoreV1().PersistentVolumeClaims(req.Namespace).Get(ctx, req.Name, metav1.GetOptions{})
	if err != nil {
		volumeActionFail(c, http.StatusBadGateway, "KUBERNETES_API_ERROR", err)
		return
	}
	allow := false
	if pvc.Spec.StorageClassName != nil {
		if sc, err := client.StorageV1().StorageClasses().Get(ctx, *pvc.Spec.StorageClassName, metav1.GetOptions{}); err == nil {
			allow = sc.AllowVolumeExpansion != nil && *sc.AllowVolumeExpansion
		}
	}
	want, err := validateExpand(*pvc, allow, req.SizeGiB)
	if err != nil {
		volumeActionFail(c, http.StatusUnprocessableEntity, "INVALID_VOLUME_ACTION", err)
		return
	}
	patch, _ := json.Marshal(map[string]any{"spec": map[string]any{"resources": map[string]any{"requests": map[string]string{"storage": want.String()}}}})
	before := map[string]interface{}{"size": pvc.Spec.Resources.Requests.Storage().String()}
	_, err = client.CoreV1().PersistentVolumeClaims(req.Namespace).Patch(ctx, req.Name, types.MergePatchType, patch, metav1.PatchOptions{})
	logOperation(c, h.historyTracker, history.ActionExpandVolume, resName, req.Cluster, before, map[string]interface{}{"size": want.String()}, start, err)
	if err != nil {
		volumeActionFail(c, http.StatusBadGateway, "KUBERNETES_API_ERROR", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"size": want.String()}})
}

// DeleteVolume apaga um PVC sem pod ou um PV sem uso. Exige repetir o nome. Com reclaimPolicy
// Delete, apagar o PVC apaga também o disco: a tela avisa, o servidor só confere o que é seguro.
func (h *NodePoolHandler) DeleteVolume(c *gin.Context) {
	start := time.Now()
	req, ok := h.volumeActionSetup(c)
	if !ok {
		return
	}
	if req.Confirm != req.Name {
		volumeActionFail(c, http.StatusBadRequest, "CONFIRMATION_MISMATCH", fmt.Errorf("a confirmação não confere com o nome do volume"))
		return
	}
	client, err := h.kubeManager.GetClient(req.Cluster)
	if err != nil {
		volumeActionFail(c, http.StatusInternalServerError, "KUBERNETES_CLIENT_ERROR", err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	var resName string
	switch req.Kind {
	case "pvc":
		if req.Namespace == "" {
			volumeActionFail(c, http.StatusBadRequest, "INVALID_REQUEST", fmt.Errorf("namespace é obrigatório"))
			return
		}
		resName = req.Namespace + "/" + req.Name
		pods, err := client.CoreV1().Pods(req.Namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			volumeActionFail(c, http.StatusBadGateway, "KUBERNETES_API_ERROR", err)
			return
		}
		if used := podsUsingPVC(pods.Items, req.Namespace, req.Name); len(used) > 0 {
			volumeActionFail(c, http.StatusUnprocessableEntity, "VOLUME_IN_USE", fmt.Errorf("o PVC é usado por %d pod(s) (%s); não foi apagado", len(used), used[0]))
			return
		}
		err = client.CoreV1().PersistentVolumeClaims(req.Namespace).Delete(ctx, req.Name, metav1.DeleteOptions{})
		logOperation(c, h.historyTracker, history.ActionDeleteVolume, "pvc/"+resName, req.Cluster, nil, nil, start, err)
		if err != nil {
			volumeActionFail(c, http.StatusBadGateway, "KUBERNETES_API_ERROR", err)
			return
		}
	case "pv":
		resName = req.Name
		pv, err := client.CoreV1().PersistentVolumes().Get(ctx, req.Name, metav1.GetOptions{})
		if err != nil {
			volumeActionFail(c, http.StatusBadGateway, "KUBERNETES_API_ERROR", err)
			return
		}
		if err := validateDeletePV(*pv); err != nil {
			volumeActionFail(c, http.StatusUnprocessableEntity, "INVALID_VOLUME_ACTION", err)
			return
		}
		err = client.CoreV1().PersistentVolumes().Delete(ctx, req.Name, metav1.DeleteOptions{})
		logOperation(c, h.historyTracker, history.ActionDeleteVolume, "pv/"+resName, req.Cluster, nil, nil, start, err)
		if err != nil {
			volumeActionFail(c, http.StatusBadGateway, "KUBERNETES_API_ERROR", err)
			return
		}
	default:
		volumeActionFail(c, http.StatusBadRequest, "INVALID_REQUEST", fmt.Errorf("kind deve ser pvc ou pv"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
