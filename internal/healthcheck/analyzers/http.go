package analyzers

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// HTTPAnalyzer testa conectividade de endpoints HTTP/HTTPS
type HTTPAnalyzer struct{}

// Check executa GET request no endpoint e valida status code
func (a *HTTPAnalyzer) Check(ctx context.Context, endpoint string, timeout int) (bool, int64, error) {
	start := time.Now()

	client := &http.Client{
		Timeout: time.Duration(timeout) * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return false, 0, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return false, 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	latency := time.Since(start).Milliseconds()

	// Status 4xx/5xx = endpoint reachable mas com erro
	if resp.StatusCode >= 400 {
		return false, latency, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	return true, latency, nil
}

// GetDetails retorna informações adicionais do endpoint HTTP
func (a *HTTPAnalyzer) GetDetails(ctx context.Context, endpoint string) (map[string]interface{}, error) {
	req, err := http.NewRequestWithContext(ctx, "HEAD", endpoint, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return map[string]interface{}{
		"status_code":    resp.StatusCode,
		"content_type":   resp.Header.Get("Content-Type"),
		"content_length": resp.ContentLength,
		"server":         resp.Header.Get("Server"),
	}, nil
}
