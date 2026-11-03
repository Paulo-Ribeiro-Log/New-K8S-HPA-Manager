// Package appremoval descobre tudo o que pertence a uma aplicação dentro de um namespace
// (workloads, Services, Ingress, config, dados, RBAC) para a ferramenta "Remoção de Aplicação".
// Esta fase só lê: nada aqui altera o cluster. A lógica de Scan é pura (recebe os recursos já
// coletados), a coleta via kubectl fica em collect.go.
package appremoval

import (
	"encoding/json"
	"errors"
)

// ErrNotFound indica que nenhum workload nem release Helm tem o nome informado.
var ErrNotFound = errors.New("aplicação não encontrada no namespace")

// Origin é o gerenciador que criou o recurso. Kustomize não deixa marca no cluster: "kustomize"
// só vale com a label managed-by=kustomize; "kustomize-provavel" é heurística (kubectl apply +
// labels de aplicação).
type Origin string

const (
	OriginHelm              Origin = "helm"
	OriginKustomize         Origin = "kustomize"
	OriginKustomizeProvavel Origin = "kustomize-provavel"
	OriginManual            Origin = "manual"
)

// Kinds tratados pelo scanner.
const (
	KindDeployment     = "Deployment"
	KindStatefulSet    = "StatefulSet"
	KindDaemonSet      = "DaemonSet"
	KindCronJob        = "CronJob"
	KindService        = "Service"
	KindIngress        = "Ingress"
	KindHTTPRoute      = "HTTPRoute"
	KindVirtualService = "VirtualService"
	KindHPA            = "HorizontalPodAutoscaler"
	KindPDB            = "PodDisruptionBudget"
	KindNetworkPolicy  = "NetworkPolicy"
	KindServiceMonitor = "ServiceMonitor"
	KindPodMonitor     = "PodMonitor"
	KindConfigMap      = "ConfigMap"
	KindSecret         = "Secret"
	KindExternalSecret = "ExternalSecret"
	KindPVC            = "PersistentVolumeClaim"
	KindSA             = "ServiceAccount"
	KindRole           = "Role"
	KindRoleBinding    = "RoleBinding"
	KindHelmRelease    = "HelmRelease"
)

const (
	annHelmRelease  = "meta.helm.sh/release-name"
	annHelmPolicy   = "helm.sh/resource-policy"
	annLastApplied  = "kubectl.kubernetes.io/last-applied-configuration"
	labelManagedBy  = "app.kubernetes.io/managed-by"
	helmSecretType  = "helm.sh/release.v1"
	defaultSAName   = "default"
	labelAppName    = "app.kubernetes.io/name"
	labelAppInst    = "app.kubernetes.io/instance"
	labelLegacyApp  = "app"
	valueKustomize  = "kustomize"
	valueHelmPolicy = "keep"
)

// Owner é uma ownerReference do recurso.
type Owner struct {
	Kind string
	Name string
	UID  string
}

// Subject é um sujeito de RoleBinding.
type Subject struct {
	Kind      string
	Name      string
	Namespace string
}

// Resource é a visão mínima de um objeto do cluster. Dados de Secret nunca entram aqui.
type Resource struct {
	Kind        string
	Name        string
	UID         string
	Type        string // só Secret (ex.: helm.sh/release.v1)
	Labels      map[string]string
	Annotations map[string]string
	Owners      []Owner
	Spec        json.RawMessage
	Subjects    []Subject // RoleBinding
	RoleRefKind string    // RoleBinding
	RoleRefName string    // RoleBinding
}

// Item é um recurso encontrado pelo scan.
type Item struct {
	Kind            string   `json:"kind"`
	Name            string   `json:"name"`
	UID             string   `json:"uid,omitempty"`
	Origin          Origin   `json:"origin"`
	Reason          string   `json:"reason"`
	Shared          bool     `json:"shared"`
	UsedBy          []string `json:"used_by,omitempty"`
	DataRisk        bool     `json:"data_risk"`
	RemovedByHelm   bool     `json:"removed_by_helm"`
	DefaultSelected bool     `json:"default_selected"`
}

// HelmInfo descreve o release quando a aplicação é gerenciada por Helm.
type HelmInfo struct {
	Release   string `json:"release"`
	Revisions int    `json:"revisions"`
}

// Result é o resultado do scan.
type Result struct {
	Namespace string    `json:"namespace"`
	Name      string    `json:"name"`
	Origin    Origin    `json:"origin"`
	Helm      *HelmInfo `json:"helm,omitempty"`
	Items     []Item    `json:"items"`
	Warnings  []string  `json:"warnings"`
}

// Candidate é um possível alvo de remoção (workload ou release Helm) do namespace.
type Candidate struct {
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	HelmRelease string `json:"helm_release,omitempty"`
}
