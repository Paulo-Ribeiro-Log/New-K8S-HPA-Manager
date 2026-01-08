package helm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

const defaultBinary = "helm"

// CLIClient executes Helm commands using the helm CLI binary.
type CLIClient struct {
	binary     string
	timeout    time.Duration
	logger     zerolog.Logger
	operations sync.Map
}

// Option configures the CLI client.
type Option func(*CLIClient)

// WithBinary overrides the helm binary path.
func WithBinary(path string) Option {
	return func(c *CLIClient) {
		if path != "" {
			c.binary = path
		}
	}
}

// WithTimeout sets the default timeout for helm invocations.
func WithTimeout(d time.Duration) Option {
	return func(c *CLIClient) {
		if d > 0 {
			c.timeout = d
		}
	}
}

// WithLogger attaches a zerolog logger used for debug output.
func WithLogger(logger zerolog.Logger) Option {
	return func(c *CLIClient) {
		c.logger = logger
	}
}

// NewCLIClient builds a CLIClient with optional customizations.
func NewCLIClient(opts ...Option) *CLIClient {
	client := &CLIClient{
		binary:  defaultBinary,
		timeout: 45 * time.Second,
		logger:  zerolog.Nop(),
	}

	for _, opt := range opts {
		opt(client)
	}

	return client
}

// ListReleases executes `helm list` and returns the parsed releases.
func (c *CLIClient) ListReleases(ctx context.Context, opts ListReleasesOptions) ([]ReleaseSummary, error) {
	args := []string{"list", "--output", "json"}

	if opts.Namespace != "" {
		args = append(args, "--namespace", opts.Namespace)
	} else {
		args = append(args, "--all-namespaces")
	}

	if opts.Filter != "" {
		args = append(args, "--filter", opts.Filter)
	}

	if opts.Limit > 0 {
		args = append(args, "--max", strconv.Itoa(opts.Limit))
	}

	stdout, stderr, err := c.runCommand(ctx, opts.Cluster, args...)
	if err != nil {
		return nil, fmt.Errorf("helm list failed: %w (stderr: %s)", err, strings.TrimSpace(stderr))
	}

	type helmListEntry struct {
		Name       string `json:"name"`
		Namespace  string `json:"namespace"`
		Revision   string `json:"revision"`
		Updated    string `json:"updated"`
		Status     string `json:"status"`
		Chart      string `json:"chart"`
		AppVersion string `json:"app_version"`
	}

	var rawEntries []helmListEntry
	if err := json.Unmarshal(stdout, &rawEntries); err != nil {
		return nil, fmt.Errorf("unable to parse helm list output: %w", err)
	}

	statuses := normalizeStatuses(opts.Statuses)
	var releases []ReleaseSummary
	for _, entry := range rawEntries {
		statusKey := strings.ToLower(entry.Status)
		if len(statuses) > 0 && !statuses[statusKey] {
			continue
		}

		updatedAt := parseHelmTimestamp(entry.Updated)

		revision := entry.Revision
		revisionInt, err := strconv.Atoi(strings.TrimSpace(revision))
		if err != nil {
			revisionInt = 0
		}

		releases = append(releases, ReleaseSummary{
			Name:              entry.Name,
			Namespace:         entry.Namespace,
			Chart:             entry.Chart,
			AppVersion:        entry.AppVersion,
			Revision:          revisionInt,
			UpdatedAt:         updatedAt,
			Status:            entry.Status,
			HasPendingUpgrade: strings.HasPrefix(statusKey, "pending"),
		})
	}

	return releases, nil
}

// parseHelmTimestamp converts helm CLI timestamps into time.Time, tolerating multiple layouts.
func parseHelmTimestamp(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}

	layouts := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999 -0700 MST",
		"2006-01-02 15:04:05.999999 -0700 MST",
		"2006-01-02 15:04:05 -0700 MST",
	}

	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed
		}
	}

	return time.Time{}
}

// GetRelease fetches release metadata via `helm status` and complementary commands.
func (c *CLIClient) GetRelease(ctx context.Context, opts GetReleaseOptions) (*ReleaseDetail, error) {
	if opts.Release == "" {
		return nil, errors.New("release name is required")
	}

	statusArgs := []string{"status", opts.Release, "--output", "json"}
	if opts.Namespace != "" {
		statusArgs = append(statusArgs, "--namespace", opts.Namespace)
	}

	stdout, stderr, err := c.runCommand(ctx, opts.Cluster, statusArgs...)
	if err != nil {
		return nil, fmt.Errorf("helm status failed: %w (stderr: %s)", err, strings.TrimSpace(stderr))
	}

	type helmHook struct {
		Name           string   `json:"name"`
		Kind           string   `json:"kind"`
		Path           string   `json:"path"`
		Weight         int      `json:"weight"`
		DeletePolicies []string `json:"delete_policies"`
		LastRun        string   `json:"last_run"`
	}

	type helmStatusInfo struct {
		FirstDeployed string `json:"first_deployed"`
		LastDeployed  string `json:"last_deployed"`
		Status        string `json:"status"`
		Description   string `json:"description"`
	}

	type helmStatus struct {
		Name       string         `json:"name"`
		Namespace  string         `json:"namespace"`
		Revision   int            `json:"revision"`
		Version    int            `json:"version"`
		Updated    string         `json:"updated"`
		Status     string         `json:"status"`
		Chart      string         `json:"chart"`
		AppVersion string         `json:"app_version"`
		Notes      string         `json:"notes"`
		Manifest   string         `json:"manifest"`
		Hooks      []helmHook     `json:"hooks"`
		Info       helmStatusInfo `json:"info"`
	}

	var parsed helmStatus
	if err := json.Unmarshal(stdout, &parsed); err != nil {
		return nil, fmt.Errorf("unable to parse helm status output: %w", err)
	}

	valuesRaw, err := c.getValues(ctx, opts.Cluster, opts.Namespace, opts.Release, true)
	if err != nil {
		return nil, err
	}

	valuesRendered, err := c.getValues(ctx, opts.Cluster, opts.Namespace, opts.Release, false)
	if err != nil {
		return nil, err
	}

	manifest := parsed.Manifest
	if manifest == "" {
		manifest, _ = c.getManifest(ctx, opts.Cluster, opts.Namespace, opts.Release)
	}

	updatedValue := parsed.Updated
	if updatedValue == "" {
		updatedValue = parsed.Info.LastDeployed
	}
	updatedAt := parseHelmTimestamp(updatedValue)

	hooks := make([]Hook, 0, len(parsed.Hooks))
	for _, raw := range parsed.Hooks {
		lastRun := parseHelmTimestamp(raw.LastRun)
		hooks = append(hooks, Hook{
			Name:           raw.Name,
			Kind:           raw.Kind,
			Path:           raw.Path,
			Weight:         raw.Weight,
			DeletePolicies: raw.DeletePolicies,
			LastRun:        lastRun,
		})
	}

	chart := parsed.Chart
	if chart == "" {
		chart = extractManifestAnnotation(manifest, "helm.sh/chart")
	}

	appVersion := parsed.AppVersion
	if appVersion == "" {
		appVersion = extractManifestAnnotation(manifest, "app.kubernetes.io/version")
	}

	revision := parsed.Revision
	if revision == 0 {
		revision = parsed.Version
	}

	status := parsed.Status
	if status == "" {
		status = parsed.Info.Status
	}

	detail := &ReleaseDetail{
		ReleaseSummary: ReleaseSummary{
			Name:              parsed.Name,
			Namespace:         parsed.Namespace,
			Chart:             chart,
			AppVersion:        appVersion,
			Revision:          revision,
			UpdatedAt:         updatedAt,
			Status:            status,
			HasPendingUpgrade: strings.HasPrefix(strings.ToLower(status), "pending"),
		},
		ValuesRaw:      valuesRaw,
		ValuesRendered: valuesRendered,
		Manifest:       manifest,
		Notes:          parsed.Notes,
		Hooks:          hooks,
		Resources:      nil,
	}

	return detail, nil
}

// History returns `helm history` entries for a release.
func (c *CLIClient) History(ctx context.Context, opts HistoryOptions) ([]RevisionEntry, error) {
	if opts.Release == "" {
		return nil, errors.New("release name is required")
	}

	historyArgs := []string{"history", opts.Release, "--output", "json"}
	if opts.Namespace != "" {
		historyArgs = append(historyArgs, "--namespace", opts.Namespace)
	}
	if opts.Limit > 0 {
		historyArgs = append(historyArgs, "--max", strconv.Itoa(opts.Limit))
	}

	stdout, stderr, err := c.runCommand(ctx, opts.Cluster, historyArgs...)
	if err != nil {
		return nil, fmt.Errorf("helm history failed: %w (stderr: %s)", err, strings.TrimSpace(stderr))
	}

	type historyEntry struct {
		Revision    int    `json:"revision"`
		Updated     string `json:"updated"`
		Status      string `json:"status"`
		Chart       string `json:"chart"`
		AppVersion  string `json:"appVersion"`
		Description string `json:"description"`
	}

	var rawEntries []historyEntry
	if err := json.Unmarshal(stdout, &rawEntries); err != nil {
		return nil, fmt.Errorf("unable to parse helm history output: %w", err)
	}

	revisions := make([]RevisionEntry, 0, len(rawEntries))
	for _, entry := range rawEntries {
		updatedAt := parseHelmTimestamp(entry.Updated)

		revisions = append(revisions, RevisionEntry{
			Revision:     entry.Revision,
			UpdatedAt:    updatedAt,
			Status:       entry.Status,
			Description:  entry.Description,
			ValuesDigest: "",
			ExecutedBy:   "",
		})
	}

	return revisions, nil
}

// Execute runs install, upgrade, rollback or uninstall based on the request.
func (c *CLIClient) Execute(ctx context.Context, req HelmActionRequest) (*HelmActionResponse, error) {
	operationID := uuid.NewString()
	startedAt := time.Now()

	response := &HelmActionResponse{
		OperationID: operationID,
		Status:      OperationRunning,
		StartedAt:   startedAt,
		Logs:        []string{},
	}

	var args []string
	var cleanup func()
	var err error

	switch req.Action {
	case ActionInstall:
		args, cleanup, err = c.buildInstallArgs(req)
	case ActionUpgrade:
		args, cleanup, err = c.buildUpgradeArgs(req)
	case ActionRollback:
		args, cleanup, err = c.buildRollbackArgs(req)
	case ActionUninstall:
		args, cleanup, err = c.buildUninstallArgs(req)
	default:
		err = fmt.Errorf("unsupported helm action: %s", req.Action)
	}

	if cleanup != nil {
		defer cleanup()
	}

	if err != nil {
		response.Status = OperationFailed
		response.CompletedAt = time.Now()
		response.Logs = append(response.Logs, err.Error())
		c.operations.Store(operationID, response)
		return response, err
	}

	stdout, stderr, runErr := c.runCommand(ctx, req.Cluster, args...)
	logs := strings.Split(strings.TrimSpace(string(stdout)), "\n")
	if len(logs) == 1 && logs[0] == "" {
		logs = nil
	}
	response.Logs = append(response.Logs, logs...)
	if trimmedErr := strings.TrimSpace(stderr); trimmedErr != "" {
		response.Logs = append(response.Logs, trimmedErr)
	}

	response.CompletedAt = time.Now()
	if runErr != nil {
		response.Status = OperationFailed
		c.operations.Store(operationID, response)
		return response, fmt.Errorf("helm %s failed: %w (stderr: %s)", req.Action, runErr, strings.TrimSpace(stderr))
	}

	response.Status = OperationSucceeded
	c.operations.Store(operationID, response)

	return response, nil
}

// StreamOperation currently returns the stored operation result as a single event.
func (c *CLIClient) StreamOperation(ctx context.Context, operationID string) (<-chan OperationEvent, func(), error) {
	value, ok := c.operations.Load(operationID)
	if !ok {
		return nil, func() {}, fmt.Errorf("operation %s not found", operationID)
	}

	resp, ok := value.(*HelmActionResponse)
	if !ok {
		return nil, func() {}, fmt.Errorf("operation %s stored with unexpected type", operationID)
	}

	ch := make(chan OperationEvent, 1)
	ch <- OperationEvent{
		OperationID: operationID,
		Timestamp:   resp.CompletedAt,
		Phase:       string(resp.Status),
		Message:     strings.Join(resp.Logs, "\n"),
	}
	close(ch)

	cancel := func() {}

	return ch, cancel, nil
}

func (c *CLIClient) runCommand(ctx context.Context, cluster ClusterTarget, args ...string) ([]byte, string, error) {
	ctxWithTimeout := ctx
	if _, hasDeadline := ctx.Deadline(); !hasDeadline && c.timeout > 0 {
		var cancel context.CancelFunc
		ctxWithTimeout, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}

	args = c.appendClusterArgs(cluster, args)
	cmd := exec.CommandContext(ctxWithTimeout, c.binary, args...)

	cmd.Env = os.Environ()
	if cluster.KubeConfigPath != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("KUBECONFIG=%s", cluster.KubeConfigPath))
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if c.logger.GetLevel() <= zerolog.DebugLevel {
		c.logger.Debug().Strs("args", cmd.Args).Msg("helm command")
	}

	err := cmd.Run()
	return stdout.Bytes(), stderr.String(), err
}

func (c *CLIClient) appendClusterArgs(cluster ClusterTarget, args []string) []string {
	if cluster.KubeContext != "" {
		args = append(args, "--kube-context", cluster.KubeContext)
	}
	return args
}

func (c *CLIClient) getValues(ctx context.Context, cluster ClusterTarget, namespace, release string, includeAll bool) (string, error) {
	args := []string{"get", "values", release, "--output", "yaml"}
	if includeAll {
		args = append(args, "--all")
	}
	if namespace != "" {
		args = append(args, "--namespace", namespace)
	}

	stdout, stderr, err := c.runCommand(ctx, cluster, args...)
	if err != nil {
		return "", fmt.Errorf("helm get values failed: %w (stderr: %s)", err, strings.TrimSpace(stderr))
	}

	return string(stdout), nil
}

func (c *CLIClient) getManifest(ctx context.Context, cluster ClusterTarget, namespace, release string) (string, error) {
	args := []string{"get", "manifest", release}
	if namespace != "" {
		args = append(args, "--namespace", namespace)
	}

	stdout, stderr, err := c.runCommand(ctx, cluster, args...)
	if err != nil {
		return "", fmt.Errorf("helm get manifest failed: %w (stderr: %s)", err, strings.TrimSpace(stderr))
	}

	return string(stdout), nil
}

func (c *CLIClient) buildInstallArgs(req HelmActionRequest) ([]string, func(), error) {
	if req.ChartRef == "" {
		return nil, nil, errors.New("chartRef is required for install")
	}
	if req.ReleaseName == "" {
		return nil, nil, errors.New("releaseName is required for install")
	}

	args := []string{"install", req.ReleaseName, req.ChartRef}
	cleanup, err := c.appendCommonActionArgs(&args, req)
	return args, cleanup, err
}

func (c *CLIClient) buildUpgradeArgs(req HelmActionRequest) ([]string, func(), error) {
	if req.ChartRef == "" {
		return nil, nil, errors.New("chartRef is required for upgrade")
	}
	if req.ReleaseName == "" {
		return nil, nil, errors.New("releaseName is required for upgrade")
	}

	args := []string{"upgrade", req.ReleaseName, req.ChartRef}
	cleanup, err := c.appendCommonActionArgs(&args, req)
	return args, cleanup, err
}

func (c *CLIClient) buildRollbackArgs(req HelmActionRequest) ([]string, func(), error) {
	if req.ReleaseName == "" {
		return nil, nil, errors.New("releaseName is required for rollback")
	}
	if req.TargetRevision <= 0 {
		return nil, nil, errors.New("targetRevision must be a positive integer for rollback")
	}

	args := []string{"rollback", req.ReleaseName, strconv.Itoa(req.TargetRevision)}
	if req.Force {
		args = append(args, "--force")
	}
	if req.Namespace != "" {
		args = append(args, "--namespace", req.Namespace)
	}

	return args, nil, nil
}

func (c *CLIClient) buildUninstallArgs(req HelmActionRequest) ([]string, func(), error) {
	if req.ReleaseName == "" {
		return nil, nil, errors.New("releaseName is required for uninstall")
	}

	args := []string{"uninstall", req.ReleaseName}
	if req.Namespace != "" {
		args = append(args, "--namespace", req.Namespace)
	}

	return args, nil, nil
}

func (c *CLIClient) appendCommonActionArgs(args *[]string, req HelmActionRequest) (func(), error) {
	if req.Namespace != "" {
		*args = append(*args, "--namespace", req.Namespace, "--create-namespace")
	}

	if req.Version != "" {
		*args = append(*args, "--version", req.Version)
	}

	if req.DryRun {
		*args = append(*args, "--dry-run")
	}

	if req.Force {
		*args = append(*args, "--force")
	}

	if strings.TrimSpace(req.ValuesYAML) == "" {
		return nil, nil
	}

	tempDir := os.TempDir()
	file, err := os.CreateTemp(tempDir, "helm-values-*.yaml")
	if err != nil {
		return nil, fmt.Errorf("unable to create temporary values file: %w", err)
	}

	if _, err := file.WriteString(req.ValuesYAML); err != nil {
		file.Close()
		os.Remove(file.Name())
		return nil, fmt.Errorf("unable to write temporary values file: %w", err)
	}

	if err := file.Close(); err != nil {
		os.Remove(file.Name())
		return nil, fmt.Errorf("unable to close temporary values file: %w", err)
	}

	*args = append(*args, "--values", file.Name())

	cleanup := func() {
		_ = os.Remove(file.Name())
	}

	return cleanup, nil
}

func normalizeStatuses(statuses []string) map[string]bool {
	if len(statuses) == 0 {
		return nil
	}

	result := make(map[string]bool, len(statuses))
	for _, status := range statuses {
		trimmed := strings.TrimSpace(strings.ToLower(status))
		if trimmed == "" {
			continue
		}
		result[trimmed] = true
	}
	return result
}

func extractManifestAnnotation(manifest, key string) string {
	if manifest == "" || key == "" {
		return ""
	}

	scanner := bufio.NewScanner(strings.NewReader(manifest))
	keyWithColon := key + ":"
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, keyWithColon) {
			continue
		}

		value := strings.TrimSpace(strings.TrimPrefix(line, keyWithColon))
		value = strings.Trim(value, `"`)
		return value
	}

	return ""
}

// ResolveBinary ensures the helm binary exists on disk.
func ResolveBinary(path string) (string, error) {
	candidate := path
	if candidate == "" {
		candidate = defaultBinary
	}

	if strings.ContainsRune(candidate, os.PathSeparator) {
		if _, err := os.Stat(candidate); err != nil {
			return "", fmt.Errorf("helm binary not found at %s: %w", candidate, err)
		}
		return candidate, nil
	}

	resolved, err := exec.LookPath(candidate)
	if err != nil {
		return "", fmt.Errorf("helm binary %s not found in PATH: %w", candidate, err)
	}

	return resolved, nil
}

// ExpandHome attempts to resolve ~ to the user home directory.
func ExpandHome(path string) string {
	if path == "" || path[0] != '~' {
		return path
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return path
	}

	if len(path) == 1 {
		return homeDir
	}

	return filepath.Join(homeDir, path[1:])
}
