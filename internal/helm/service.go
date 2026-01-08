package helm

import (
	"context"
	"fmt"

	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/pkg/helm"
)

// Service orchestrates helm operations leveraging a Helm client and kubeconfig manager.
type Service struct {
	kubeManager *config.KubeConfigManager
	client      helm.Client
}

// NewService constructs a new Helm Service instance.
func NewService(km *config.KubeConfigManager, client helm.Client) *Service {
	return &Service{
		kubeManager: km,
		client:      client,
	}
}

// ListReleases lists helm releases for a given cluster and namespace.
func (s *Service) ListReleases(ctx context.Context, cluster string, opts helm.ListReleasesOptions) ([]helm.ReleaseSummary, error) {
	if cluster == "" {
		return nil, fmt.Errorf("cluster is required")
	}

	target := s.clusterTarget(cluster)
	opts.Cluster = target

	return s.client.ListReleases(ctx, opts)
}

// GetRelease fetches details for a specific helm release.
func (s *Service) GetRelease(ctx context.Context, cluster string, opts helm.GetReleaseOptions) (*helm.ReleaseDetail, error) {
	if cluster == "" {
		return nil, fmt.Errorf("cluster is required")
	}
	if opts.Release == "" {
		return nil, fmt.Errorf("release is required")
	}

	opts.Cluster = s.clusterTarget(cluster)

	return s.client.GetRelease(ctx, opts)
}

// History returns revisions for a helm release.
func (s *Service) History(ctx context.Context, cluster string, opts helm.HistoryOptions) ([]helm.RevisionEntry, error) {
	if cluster == "" {
		return nil, fmt.Errorf("cluster is required")
	}
	if opts.Release == "" {
		return nil, fmt.Errorf("release is required")
	}

	opts.Cluster = s.clusterTarget(cluster)

	return s.client.History(ctx, opts)
}

// Execute triggers a helm action (install, upgrade, rollback, uninstall).
func (s *Service) Execute(ctx context.Context, cluster string, request helm.HelmActionRequest) (*helm.HelmActionResponse, error) {
	if cluster == "" {
		return nil, fmt.Errorf("cluster is required")
	}
	if request.ReleaseName == "" {
		return nil, fmt.Errorf("releaseName is required")
	}

	request.Cluster = s.clusterTarget(cluster)

	return s.client.Execute(ctx, request)
}

// StreamOperation streams updates for a previously executed helm operation.
func (s *Service) StreamOperation(ctx context.Context, operationID string) (<-chan helm.OperationEvent, func(), error) {
	return s.client.StreamOperation(ctx, operationID)
}

func (s *Service) clusterTarget(cluster string) helm.ClusterTarget {
	return helm.ClusterTarget{
		KubeConfigPath: helm.ExpandHome(s.kubeManager.ConfigPath()),
		KubeContext:    cluster,
	}
}
