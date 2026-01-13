package helm

import "context"

// Client exposes Helm operations required by the Helm tab feature.
type Client interface {
	ListReleases(ctx context.Context, opts ListReleasesOptions) ([]ReleaseSummary, error)
	GetRelease(ctx context.Context, opts GetReleaseOptions) (*ReleaseDetail, error)
	History(ctx context.Context, opts HistoryOptions) ([]RevisionEntry, error)
	Execute(ctx context.Context, req HelmActionRequest) (*HelmActionResponse, error)
	StreamOperation(ctx context.Context, operationID string) (<-chan OperationEvent, func(), error)
}
