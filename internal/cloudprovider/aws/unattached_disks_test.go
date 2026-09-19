package aws

import "testing"

func TestParseEBSVolumes(t *testing.T) {
	out := []byte(`{"Volumes":[
	  {"VolumeId":"vol-0aaa","Size":100,"VolumeType":"gp3","State":"available","CreateTime":"2024-01-01T00:00:00+00:00",
	   "AvailabilityZone":"us-east-1a",
	   "Tags":[{"Key":"kubernetes.io/created-for/pvc/name","Value":"data-0"},
	           {"Key":"kubernetes.io/created-for/pvc/namespace","Value":"db"},
	           {"Key":"kubernetes.io/created-for/pv/name","Value":"pvc-abc"},
	           {"Key":"kubernetes.io/cluster/eks-prd","Value":"owned"}]},
	  {"VolumeId":"vol-0bbb","Size":8,"VolumeType":"gp2","State":"available","AvailabilityZone":"us-east-1b",
	   "Tags":[{"Key":"Name","Value":"backup-antigo"}]},
	  {"VolumeId":"vol-0ccc","Size":8,"VolumeType":"gp2","State":"in-use","AvailabilityZone":"us-east-1b"}
	]}`)

	disks, err := parseEBSVolumes(out, "us-east-1")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(disks) != 2 {
		t.Fatalf("volume in-use deveria ser ignorado: %d discos", len(disks))
	}

	k8s := disks[0]
	if k8s.Provider != "aws" || k8s.SizeGB != 100 || k8s.DiskType != "gp3" || k8s.Location != "us-east-1" || k8s.Zone != "us-east-1a" {
		t.Errorf("campos básicos errados: %+v", k8s)
	}
	if k8s.K8sPVCName != "data-0" || k8s.K8sPVCNamespace != "db" || k8s.K8sPVName != "pvc-abc" || k8s.K8sClusterHint != "eks-prd" {
		t.Errorf("origem K8s não extraída: %+v", k8s)
	}
	if k8s.Name != "vol-0aaa" {
		t.Errorf("sem tag Name o nome deve ser o VolumeId: %q", k8s.Name)
	}

	manual := disks[1]
	if manual.Name != "backup-antigo" || manual.IsK8sProvisioned() {
		t.Errorf("volume manual: %+v", manual)
	}
}

func TestParseEBSVolumes_Empty(t *testing.T) {
	disks, err := parseEBSVolumes([]byte(`{"Volumes":[]}`), "us-east-1")
	if err != nil || disks == nil || len(disks) != 0 {
		t.Errorf("lista vazia deve ser [] não nil: %v %v", disks, err)
	}
}
