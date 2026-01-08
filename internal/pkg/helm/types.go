package helm

import "time"

// ReleaseSummary captures high-level metadata returned by `helm list`.
type ReleaseSummary struct {
	Name              string    `json:"name"`
	Namespace         string    `json:"namespace"`
	Chart             string    `json:"chart"`
	AppVersion        string    `json:"appVersion"`
	Revision          int       `json:"revision"`
	UpdatedAt         time.Time `json:"updatedAt"`
	Status            string    `json:"status"`
	HasPendingUpgrade bool      `json:"hasPendingUpgrade"`
}

// ReleaseDetail enriches ReleaseSummary with values, manifests and hooks information.
type ReleaseDetail struct {
	ReleaseSummary
	ValuesRaw      string     `json:"valuesRaw"`
	ValuesRendered string     `json:"valuesRendered"`
	Manifest       string     `json:"manifest"`
	Notes          string     `json:"notes"`
	Hooks          []Hook     `json:"hooks"`
	Resources      []Resource `json:"resources"`
}

// Hook represents a Helm hook entry.
type Hook struct {
	Name           string    `json:"name"`
	Kind           string    `json:"kind"`
	Path           string    `json:"path"`
	Weight         int       `json:"weight"`
	DeletePolicies []string  `json:"deletePolicies"`
	LastRun        time.Time `json:"lastRun"`
}

// Resource represents a rendered Kubernetes resource block.
type Resource struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Manifest  string `json:"manifest"`
}

// RevisionEntry contains audit information for helm history.
type RevisionEntry struct {
	Revision     int       `json:"revision"`
	UpdatedAt    time.Time `json:"updatedAt"`
	Status       string    `json:"status"`
	Description  string    `json:"description"`
	ValuesDigest string    `json:"valuesDigest"`
	ExecutedBy   string    `json:"executedBy"`
}

// ListReleasesOptions drives the ListReleases operation.
type ListReleasesOptions struct {
	Cluster   ClusterTarget
	Namespace string
	Filter    string
	Statuses  []string
	Limit     int
}

// GetReleaseOptions drives the GetRelease operation.
type GetReleaseOptions struct {
	Cluster    ClusterTarget
	Namespace  string
	Release    string
	IncludeAll bool
}

// HistoryOptions drives the History operation.
type HistoryOptions struct {
	Cluster   ClusterTarget
	Namespace string
	Release   string
	Limit     int
}

// HelmActionRequest describes an install/upgrade/rollback/uninstall operation.
type HelmActionRequest struct {
	Cluster        ClusterTarget `json:"cluster"`
	Namespace      string        `json:"namespace"`
	ReleaseName    string        `json:"releaseName"`
	Action         ActionType    `json:"action"`
	ChartRef       string        `json:"chartRef"`
	Version        string        `json:"version"`
	ValuesYAML     string        `json:"valuesYaml"`
	TargetRevision int           `json:"targetRevision"`
	Force          bool          `json:"force"`
	DryRun         bool          `json:"dryRun"`
}

// HelmActionResponse wraps execution metadata to feed UX feedback loops.
type HelmActionResponse struct {
	OperationID string          `json:"operationId"`
	Status      OperationStatus `json:"status"`
	StartedAt   time.Time       `json:"startedAt"`
	CompletedAt time.Time       `json:"completedAt"`
	Logs        []string        `json:"logs"`
	Warnings    []string        `json:"warnings"`
}

// OperationEvent conveys streaming updates for long-running actions.
type OperationEvent struct {
	OperationID string    `json:"operationId"`
	Timestamp   time.Time `json:"timestamp"`
	Phase       string    `json:"phase"`
	Message     string    `json:"message"`
	Error       string    `json:"error,omitempty"`
}

// ClusterTarget defines how to reach a cluster via Helm.
type ClusterTarget struct {
	KubeConfigPath string `json:"kubeConfigPath"`
	KubeContext    string `json:"kubeContext"`
}

// ActionType enumerates Helm-supported high-level actions.
type ActionType string

const (
	ActionInstall   ActionType = "install"
	ActionUpgrade   ActionType = "upgrade"
	ActionRollback  ActionType = "rollback"
	ActionUninstall ActionType = "uninstall"
)

// OperationStatus reflects lifecycle states of a helm action.
type OperationStatus string

const (
	OperationPending   OperationStatus = "pending"
	OperationRunning   OperationStatus = "running"
	OperationSucceeded OperationStatus = "succeeded"
	OperationFailed    OperationStatus = "failed"
)
