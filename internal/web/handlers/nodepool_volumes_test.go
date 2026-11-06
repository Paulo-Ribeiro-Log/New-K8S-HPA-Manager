package handlers

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func vtPVC(ns, name, pv, sc string, phase corev1.PersistentVolumeClaimPhase) corev1.PersistentVolumeClaim {
	return corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
		Spec:       corev1.PersistentVolumeClaimSpec{VolumeName: pv, StorageClassName: &sc, AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}},
		Status:     corev1.PersistentVolumeClaimStatus{Phase: phase, Capacity: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("10Gi")}},
	}
}

func vtPV(name, sc string, phase corev1.PersistentVolumePhase, claim *corev1.ObjectReference) corev1.PersistentVolume {
	return corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: corev1.PersistentVolumeSpec{
			StorageClassName: sc, ClaimRef: claim,
			Capacity:                      corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("20Gi")},
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimDelete,
		},
		Status: corev1.PersistentVolumeStatus{Phase: phase},
	}
}

func vtPod(ns, name, node string, owner metav1.OwnerReference, labels map[string]string, vols ...corev1.Volume) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name, Labels: labels, OwnerReferences: []metav1.OwnerReference{owner}},
		Spec:       corev1.PodSpec{NodeName: node, Volumes: vols},
	}
}

func vtClaimVol(claim string) corev1.Volume {
	return corev1.Volume{Name: "data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: claim}}}
}

func TestBuildVolumeItems(t *testing.T) {
	in := volumeInput{
		PVCs: []corev1.PersistentVolumeClaim{
			vtPVC("app", "web-data", "pv-web", "managed-premium", corev1.ClaimBound),
			vtPVC("db", "data-pg-0", "pv-pg", "managed-premium", corev1.ClaimBound),
			vtPVC("app", "sem-uso", "pv-sem-uso", "managed", corev1.ClaimBound),
			vtPVC("app", "pendente", "", "managed", corev1.ClaimPending),
			func() corev1.PersistentVolumeClaim {
				c := vtPVC("db", "data-redis-1", "pv-redis", "managed", corev1.ClaimBound)
				return c
			}(),
			func() corev1.PersistentVolumeClaim {
				c := vtPVC("misc", "etiquetado", "pv-etq", "managed", corev1.ClaimBound)
				c.Labels = map[string]string{"app.kubernetes.io/name": "relatorios"}
				return c
			}(),
			vtPVC("job", "job-pod-scratch", "pv-scratch", "managed", corev1.ClaimBound),
			vtPVC("web", "nfs-claim", "pv-nfs", "nfs", corev1.ClaimBound),
		},
		PVs: []corev1.PersistentVolume{
			vtPV("pv-web", "managed-premium", corev1.VolumeBound, nil),
			vtPV("pv-pg", "managed-premium", corev1.VolumeBound, nil),
			vtPV("pv-sem-uso", "managed", corev1.VolumeBound, nil),
			vtPV("pv-scratch", "managed", corev1.VolumeBound, nil),
			vtPV("pv-redis", "managed", corev1.VolumeBound, nil),
			vtPV("pv-etq", "managed", corev1.VolumeBound, nil),
			vtPV("pv-solto", "managed", corev1.VolumeAvailable, nil),
			func() corev1.PersistentVolume {
				pv := vtPV("pv-nfs", "nfs", corev1.VolumeBound, nil)
				pv.Spec.NFS = &corev1.NFSVolumeSource{Server: "fs.local", Path: "/share"}
				return pv
			}(),
			vtPV("pv-released", "managed", corev1.VolumeReleased, &corev1.ObjectReference{Namespace: "app", Name: "apagado"}),
		},
		Pods: []corev1.Pod{
			vtPod("app", "web-7d9f8-abc", "node-a", metav1.OwnerReference{Kind: "ReplicaSet", Name: "web-7d9f8"}, map[string]string{"pod-template-hash": "7d9f8"}, vtClaimVol("web-data")),
			vtPod("db", "pg-0", "node-b", metav1.OwnerReference{Kind: "StatefulSet", Name: "pg"}, nil, vtClaimVol("data-pg-0")),
			vtPod("web", "nfs-1", "node-a", metav1.OwnerReference{Kind: "StatefulSet", Name: "nfsapp"}, nil,
				corev1.Volume{Name: "share", VolumeSource: corev1.VolumeSource{NFS: &corev1.NFSVolumeSource{Server: "10.0.0.5", Path: "/exports/a"}}}),
			vtPod("web", "nfs-2", "node-b", metav1.OwnerReference{Kind: "StatefulSet", Name: "nfsapp"}, nil,
				corev1.Volume{Name: "share", VolumeSource: corev1.VolumeSource{NFS: &corev1.NFSVolumeSource{Server: "10.0.0.5", Path: "/exports/a"}}}),
			vtPod("job", "job-pod", "node-a", metav1.OwnerReference{Kind: "Job", Name: "job"}, nil,
				corev1.Volume{Name: "scratch", VolumeSource: corev1.VolumeSource{Ephemeral: &corev1.EphemeralVolumeSource{}}}),
		},
		Nodes: []corev1.Node{
			{ObjectMeta: metav1.ObjectMeta{Name: "node-a", Labels: map[string]string{"agentpool": "userpool"}}},
			{ObjectMeta: metav1.ObjectMeta{Name: "node-b", Labels: map[string]string{"eks.amazonaws.com/nodegroup": "ng-db"}}},
		},
		Specs: []workloadSpec{
			// Deployment escalado a 0: nenhum pod, mas o spec ainda referencia o PVC
			{Namespace: "app", Kind: "Deployment", Name: "dormindo", Volumes: []corev1.Volume{vtClaimVol("sem-uso")}},
			// segundo workload usando o mesmo PVC do web (compartilhado)
			{Namespace: "app", Kind: "CronJob", Name: "backup", Volumes: []corev1.Volume{vtClaimVol("web-data")}},
			// StatefulSet com 0 réplicas: PVC criado pelo volumeClaimTemplate
			{Namespace: "db", Kind: "StatefulSet", Name: "redis", ClaimTemplates: []string{"data"}},
			// o PVC "apagado" do PV Released ainda é referenciado por um Deployment
			{Namespace: "app", Kind: "Deployment", Name: "ex-dono", Volumes: []corev1.Volume{vtClaimVol("apagado")}},
		},
		Usage: map[string]VolumeMetric{"app/web-data": {UsedBytes: 5, UsagePercentage: 50}},
	}
	items, sum := buildVolumeItems(in)

	by := map[string]VolumeItem{}
	for _, it := range items {
		if it.Kind == "pvc" {
			by["pvc:"+it.Namespace+"/"+it.PVC] = it
		} else if it.Kind == "nfs" {
			by["nfs:"+it.Namespace+"/"+it.WorkloadKind+"/"+it.Workload+"/"+it.SourceDetail] = it
		} else {
			by["pv:"+it.PV] = it
		}
	}
	web := by["pvc:app/web-data"]
	if web.WorkloadKind != "Deployment" || web.Workload != "web" || len(web.NodePools) != 1 || web.NodePools[0] != "userpool" || web.UsagePercent != 50 || len(web.Issues) != 0 {
		t.Fatalf("web: %+v", web)
	}
	if pg := by["pvc:db/data-pg-0"]; pg.WorkloadKind != "StatefulSet" || pg.Workload != "pg" || pg.NodePools[0] != "ng-db" {
		t.Fatalf("pg: %+v", pg)
	}
	if sc := by["pvc:job/job-pod-scratch"]; sc.Workload != "job" || len(sc.Pods) != 1 {
		t.Fatalf("efêmero: %+v", sc)
	}
	if u := by["pvc:app/sem-uso"]; len(u.Issues) == 0 || len(u.Pods) != 0 {
		t.Fatalf("sem-uso deveria ter problema: %+v", u)
	}
	if p := by["pvc:app/pendente"]; len(p.Issues) != 1 || p.Issues[0] != "PVC Pending (sem PV)" {
		t.Fatalf("pendente: %+v", p)
	}
	if by["pv:pv-solto"].Issues[0] != "PV sem PVC (disco ocioso)" {
		t.Fatalf("pv solto: %+v", by["pv:pv-solto"])
	}
	if r := by["pv:pv-released"]; r.Namespace != "app" || r.PVPhase != "Released" || len(r.Issues) != 1 {
		t.Fatalf("released: %+v", r)
	}
	if n := by["pvc:web/nfs-claim"]; n.Source != "NFS" || n.SourceDetail != "fs.local:/share" {
		t.Fatalf("pv nfs: %+v", n)
	}
	d := by["nfs:web/StatefulSet/nfsapp/10.0.0.5:/exports/a"]
	if d.Kind != "nfs" || len(d.Pods) != 2 || len(d.NodePools) != 2 || d.SourceDetail != "10.0.0.5:/exports/a" {
		t.Fatalf("nfs direto: %+v", d)
	}
	// dono por spec (0 réplicas), por volumeClaimTemplate, por label e compartilhado por 2 workloads
	if u := by["pvc:app/sem-uso"]; u.Workload != "dormindo" || u.OwnerSource != "spec" {
		t.Fatalf("spec: %+v", u)
	}
	if r := by["pvc:db/data-redis-1"]; r.Workload != "redis" || r.WorkloadKind != "StatefulSet" || r.OwnerSource != "statefulset" {
		t.Fatalf("vct: %+v", r)
	}
	if e := by["pvc:misc/etiquetado"]; len(e.Workloads) != 0 || e.AppHint != "relatorios" || e.OwnerSource != "label" {
		t.Fatalf("label: %+v", e)
	}
	if w := by["pvc:app/web-data"]; len(w.Workloads) != 2 || w.Workloads[0] != "Deployment/web" || w.Workloads[1] != "CronJob/backup" {
		t.Fatalf("compartilhado: %+v", w.Workloads)
	}
	if r := by["pv:pv-released"]; len(r.Workloads) != 1 || r.Workloads[0] != "Deployment/ex-dono" {
		t.Fatalf("released com dono: %+v", r)
	}
	if sum.NFS != 2 || sum.PVCs != 8 || sum.PVs != 9 || sum.OrphanPVs != 2 {
		t.Fatalf("resumo: %+v", sum)
	}
}
