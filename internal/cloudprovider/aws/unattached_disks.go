package aws

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"k8s-hpa-manager/internal/models"
)

const unattachedVolumesTimeout = 60 * time.Second

type ebsVolume struct {
	VolumeId         string `json:"VolumeId"`
	Size             int64  `json:"Size"` // GiB
	VolumeType       string `json:"VolumeType"`
	State            string `json:"State"`
	CreateTime       string `json:"CreateTime"`
	AvailabilityZone string `json:"AvailabilityZone"`
	Tags             []struct {
		Key   string `json:"Key"`
		Value string `json:"Value"`
	} `json:"Tags"`
}

// ListUnattachedVolumes lista os volumes EBS com status "available" (não atachados a nenhuma
// instância) de UMA região/profile — EBS é regional e a AWS não tem listagem multi-região, então o
// chamador varre só a região do cluster.
//
// Passa por runAWSCLI, que serializa chamadas concorrentes do mesmo profile (o AWS CLI v2 trava em
// lock do cache SQLite de SSO quando várias chamadas do mesmo profile rodam juntas).
func ListUnattachedVolumes(ctx context.Context, region, profile string) ([]models.UnattachedDisk, error) {
	listCtx, cancel := context.WithTimeout(ctx, unattachedVolumesTimeout)
	defer cancel()

	args := buildAWSArgs(region, profile, "ec2", "describe-volumes",
		"--filters", "Name=status,Values=available")
	out, err := runAWSCLI(listCtx, profile, args)
	if err != nil {
		return nil, classifyEC2Error(err, "describe-volumes", profile)
	}
	return parseEBSVolumes(out, region)
}

func parseEBSVolumes(out []byte, region string) ([]models.UnattachedDisk, error) {
	var resp struct {
		Volumes []ebsVolume `json:"Volumes"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("parse describe-volumes: %w", err)
	}
	disks := make([]models.UnattachedDisk, 0, len(resp.Volumes))
	for _, v := range resp.Volumes {
		// Defesa em profundidade: o filtro já é feito no lado da AWS, mas nunca listar como
		// "desatachado" um volume que a API devolveu em outro estado.
		if v.State != "" && v.State != "available" {
			continue
		}
		disks = append(disks, ebsVolumeToModel(v, region))
	}
	return disks, nil
}

// Tags que o driver ebs.csi.aws.com (e o in-tree aws-ebs) grava ao provisionar um PV.
const (
	ebsTagPVCName      = "kubernetes.io/created-for/pvc/name"
	ebsTagPVCNamespace = "kubernetes.io/created-for/pvc/namespace"
	ebsTagPVName       = "kubernetes.io/created-for/pv/name"
	ebsTagClusterPfx   = "kubernetes.io/cluster/"
)

func ebsVolumeToModel(v ebsVolume, region string) models.UnattachedDisk {
	disk := models.UnattachedDisk{
		Provider:  "aws",
		ID:        v.VolumeId,
		Name:      v.VolumeId,
		Location:  region,
		Zone:      v.AvailabilityZone,
		SizeGB:    float64(v.Size),
		DiskType:  v.VolumeType,
		DiskState: v.State,
		CreatedAt: v.CreateTime,
		// A AWS não informa desde quando o volume está "available" (só CloudTrail tem isso).
	}
	if len(v.Tags) > 0 {
		disk.Tags = make(map[string]string, len(v.Tags))
	}
	for _, t := range v.Tags {
		disk.Tags[t.Key] = t.Value
		switch {
		case t.Key == "Name" && t.Value != "":
			disk.Name = t.Value
		case t.Key == ebsTagPVCName:
			disk.K8sPVCName = t.Value
		case t.Key == ebsTagPVCNamespace:
			disk.K8sPVCNamespace = t.Value
		case t.Key == ebsTagPVName:
			disk.K8sPVName = t.Value
		case strings.HasPrefix(t.Key, ebsTagClusterPfx):
			disk.K8sClusterHint = strings.TrimPrefix(t.Key, ebsTagClusterPfx)
		}
	}
	return disk
}
