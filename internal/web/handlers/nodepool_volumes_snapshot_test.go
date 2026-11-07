package handlers

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
)

func TestParseSnapshotVersion(t *testing.T) {
	cases := map[string]string{
		`{"versions":[{"version":"v1beta1"},{"version":"v1"}]}`: "v1",
		`{"versions":[{"version":"v1beta1"}]}`:                  "v1beta1",
		`{"versions":[{"version":"v2"}]}`:                       "",
		`não é json`:                                            "",
	}
	for in, want := range cases {
		if got := parseSnapshotVersion([]byte(in)); got != want {
			t.Errorf("%s: got %q, want %q", in, got, want)
		}
	}
}

func TestPickSnapshotClass(t *testing.T) {
	classes := parseSnapshotClasses([]byte(`{"items":[
	 {"metadata":{"name":"disk-a"},"driver":"disk.csi.azure.com"},
	 {"metadata":{"name":"disk-b","annotations":{"snapshot.storage.kubernetes.io/is-default-class":"true"}},"driver":"disk.csi.azure.com","deletionPolicy":"Delete"},
	 {"metadata":{"name":"ebs"},"driver":"ebs.csi.aws.com"}]}`))
	if c, err := pickSnapshotClass(classes, "disk.csi.azure.com", ""); err != nil || c.Name != "disk-b" || c.DeletionPolicy != "Delete" {
		t.Fatalf("padrão: %+v %v", c, err)
	}
	if c, err := pickSnapshotClass(classes, "disk.csi.azure.com", "disk-a"); err != nil || c.Name != "disk-a" {
		t.Fatalf("pedida: %+v %v", c, err)
	}
	if _, err := pickSnapshotClass(classes, "disk.csi.azure.com", "ebs"); err == nil {
		t.Fatal("classe de outro driver deveria falhar")
	}
	if c, err := pickSnapshotClass(classes, "ebs.csi.aws.com", ""); err != nil || c.Name != "ebs" {
		t.Fatalf("única: %+v %v", c, err)
	}
	if _, err := pickSnapshotClass(classes, "file.csi.azure.com", ""); err == nil {
		t.Fatal("sem classe deveria falhar")
	}
	two := []SnapshotClassInfo{{Name: "a", Driver: "d"}, {Name: "b", Driver: "d"}}
	if _, err := pickSnapshotClass(two, "d", ""); err == nil {
		t.Fatal("duas classes sem padrão deveria pedir escolha")
	}
}

func TestSnapshotNameAndBody(t *testing.T) {
	ts := time.Date(2026, 11, 7, 10, 30, 5, 0, time.UTC)
	if got := snapshotName("data-0", ts); got != "data-0-snap-20261107-103005" {
		t.Fatalf("nome = %q", got)
	}
	if got := snapshotName(strings.Repeat("a", 300), ts); len(got) > 253 {
		t.Fatalf("nome longo: %d", len(got))
	}
	raw, _ := snapshotBody("v1beta1", "ns", "s", "cls", "pvc")
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil || m["apiVersion"] != "snapshot.storage.k8s.io/v1beta1" || m["kind"] != "VolumeSnapshot" {
		t.Fatalf("body = %s", raw)
	}
}

func TestPVDriver(t *testing.T) {
	csi := corev1.PersistentVolume{Spec: corev1.PersistentVolumeSpec{PersistentVolumeSource: corev1.PersistentVolumeSource{CSI: &corev1.CSIPersistentVolumeSource{Driver: "x.csi"}}}}
	nfs := corev1.PersistentVolume{Spec: corev1.PersistentVolumeSpec{PersistentVolumeSource: corev1.PersistentVolumeSource{NFS: &corev1.NFSVolumeSource{Server: "s", Path: "/p"}}}}
	if pvDriver(csi) != "x.csi" || pvDriver(nfs) != "" {
		t.Fatal("pvDriver")
	}
}
