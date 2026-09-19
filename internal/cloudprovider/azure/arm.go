package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os/exec"
	"sort"
	"strings"
	"time"
)

const (
	armBaseURL = "https://management.azure.com"
	// Teto de segurança contra loop de paginação (~página de 50–100 itens).
	armMaxPages = 200
)

// ARMClient faz GETs autenticados na Azure Resource Manager REST API usando o token do `az` já
// logado na máquina (mesmo mecanismo do lookup de grupos AAD via Graph). Preferido ao `az <grupo>
// list`: o contrato de saída do CLI muda entre versões (o `az disk list` passou a exigir
// --resource-group), a REST devolve `properties` completos e o token é obtido uma vez só.
type ARMClient struct {
	SubscriptionID string
	token          string
	http           *http.Client
}

// NewARMClient resolve a subscription (UUID ou nome) e obtém o token de acesso do ARM.
func NewARMClient(ctx context.Context, subscription string) (*ARMClient, error) {
	subscription = strings.TrimSpace(subscription)
	// Subscription vazia = token da conta padrão do `az`, sem cliente preso a uma subscription: serve
	// pra operações entre subscriptions (listar, procurar um RG). O token do ARM é do TENANT, então
	// o mesmo token vale pra qualquer subscription do tenant (ver ForSubscription).
	if subscription == "" {
		token, err := azCLIOutput(ctx, "account", "get-access-token", "--resource", armBaseURL+"/", "--query", "accessToken", "--output", "tsv")
		if err != nil {
			return nil, fmt.Errorf("obter token do Azure (az login expirado?): %w", err)
		}
		return &ARMClient{token: token, http: &http.Client{Timeout: 60 * time.Second}}, nil
	}
	subID := subscription
	if !uuidRe.MatchString(subID) {
		resolved, err := azCLIOutput(ctx, "account", "show", "--subscription", subscription, "--query", "id", "--output", "tsv")
		if err != nil {
			return nil, fmt.Errorf("resolver subscription %q: %w", subscription, err)
		}
		subID = resolved
	}
	token, err := azCLIOutput(ctx, "account", "get-access-token",
		"--subscription", subID, "--resource", armBaseURL+"/", "--query", "accessToken", "--output", "tsv")
	if err != nil {
		return nil, fmt.Errorf("obter token do Azure (az login expirado?): %w", err)
	}
	return &ARMClient{SubscriptionID: subID, token: token, http: &http.Client{Timeout: 60 * time.Second}}, nil
}

// Get faz um GET numa URL absoluta do ARM. Recusa qualquer outro host: nextLink vem do corpo da
// resposta e o token nunca pode ser enviado pra fora de management.azure.com.
func (c *ARMClient) Get(ctx context.Context, rawURL string) ([]byte, error) {
	if !strings.HasPrefix(rawURL, armBaseURL+"/") {
		return nil, fmt.Errorf("URL inesperada (fora de %s): %s", armBaseURL, rawURL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &ARMError{Status: resp.StatusCode, Body: strings.TrimSpace(string(body))}
	}
	return body, nil
}

// ARMError é uma resposta HTTP não-200 do ARM — tipado pra o chamador distinguir 404 (não existe)
// de 403 (sem acesso) de falha de verdade.
type ARMError struct {
	Status int
	Body   string
}

func (e *ARMError) Error() string { return fmt.Sprintf("ARM retornou %d: %s", e.Status, e.Body) }

// ForSubscription devolve um cliente que reaproveita o token e aponta pra outra subscription.
func (c *ARMClient) ForSubscription(subscriptionID string) *ARMClient {
	cp := *c
	cp.SubscriptionID = subscriptionID
	return &cp
}

// Subscription é uma subscription visível pro usuário logado.
type Subscription struct {
	ID    string
	Name  string
	State string
}

// ListSubscriptions lista as subscriptions acessíveis (só as habilitadas).
func (c *ARMClient) ListSubscriptions(ctx context.Context) ([]Subscription, error) {
	var out []Subscription
	err := c.ListPages(ctx, armBaseURL+"/subscriptions?api-version=2022-12-01", func(body []byte) (string, error) {
		var resp struct {
			Value []struct {
				SubscriptionID string `json:"subscriptionId"`
				DisplayName    string `json:"displayName"`
				State          string `json:"state"`
			} `json:"value"`
			NextLink string `json:"nextLink"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return "", fmt.Errorf("parse subscriptions: %w", err)
		}
		for _, v := range resp.Value {
			if strings.EqualFold(v.State, "Enabled") {
				out = append(out, Subscription{ID: v.SubscriptionID, Name: v.DisplayName, State: v.State})
			}
		}
		return resp.NextLink, nil
	})
	return out, err
}

// RGExists diz se o Resource Group existe na subscription. 404 e 403 (sem acesso àquela
// subscription) contam como "não existe pra este usuário"; qualquer outra falha é erro.
func (c *ARMClient) RGExists(ctx context.Context, subscriptionID, resourceGroup string) (bool, error) {
	_, err := c.Get(ctx, fmt.Sprintf("%s/subscriptions/%s/resourcegroups/%s?api-version=2021-04-01",
		armBaseURL, subscriptionID, url.PathEscape(resourceGroup)))
	if err == nil {
		return true, nil
	}
	if ae, ok := err.(*ARMError); ok && (ae.Status == http.StatusNotFound || ae.Status == http.StatusForbidden) {
		return false, nil
	}
	return false, err
}

// ListPages percorre uma listagem paginada: fn recebe o corpo de cada página e devolve o
// nextLink ("" encerra).
func (c *ARMClient) ListPages(ctx context.Context, firstURL string, fn func(body []byte) (nextLink string, err error)) error {
	next := firstURL
	for page := 0; next != "" && page < armMaxPages; page++ {
		body, err := c.Get(ctx, next)
		if err != nil {
			if ctx.Err() != nil {
				return fmt.Errorf("listagem excedeu o tempo limite: %w", ctx.Err())
			}
			return err
		}
		if next, err = fn(body); err != nil {
			return err
		}
	}
	if next != "" {
		return fmt.Errorf("listagem passou de %d páginas — abortada", armMaxPages)
	}
	return nil
}

func azCLIOutput(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "az", args...).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// MetricSummary resume uma métrica do Azure Monitor numa janela: média, pico e P95 das médias
// horárias (P95 é mais estável que o máximo, que pode ser um ponto isolado).
type MetricSummary struct {
	Avg    float64
	Min    float64 // menor valor horário observado (ex: memória disponível no pior momento)
	Max    float64
	P5     float64 // 5º percentil das médias horárias — usado pra "memória usada P95" a partir de "disponível"
	P95    float64
	Points int
}

// ResourceMetrics consulta métricas do Azure Monitor pra um recurso (resource ID completo) numa
// janela de `days` dias, granularidade de 1h. Métrica sem nenhum ponto no período (recurso novo,
// desligado, ou sem essa métrica) simplesmente não aparece no mapa — nunca vira um zero inventado.
func (c *ARMClient) ResourceMetrics(ctx context.Context, resourceID string, metricNames []string, days int) (map[string]MetricSummary, error) {
	if days <= 0 {
		days = 14
	}
	end := time.Now().UTC()
	start := end.Add(-time.Duration(days) * 24 * time.Hour)
	q := url.Values{}
	q.Set("api-version", "2018-01-01")
	q.Set("metricnames", strings.Join(metricNames, ","))
	q.Set("aggregation", "Average,Minimum,Maximum")
	q.Set("interval", "PT1H")
	q.Set("timespan", start.Format(time.RFC3339)+"/"+end.Format(time.RFC3339))
	body, err := c.Get(ctx, armBaseURL+resourceID+"/providers/microsoft.insights/metrics?"+q.Encode())
	if err != nil {
		return nil, err
	}
	return parseMetricSummaries(body)
}

func parseMetricSummaries(body []byte) (map[string]MetricSummary, error) {
	var resp struct {
		Value []struct {
			Name struct {
				Value string `json:"value"`
			} `json:"name"`
			Timeseries []struct {
				Data []struct {
					Average *float64 `json:"average"`
					Minimum *float64 `json:"minimum"`
					Maximum *float64 `json:"maximum"`
				} `json:"data"`
			} `json:"timeseries"`
		} `json:"value"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse métricas: %w", err)
	}
	out := make(map[string]MetricSummary, len(resp.Value))
	for _, m := range resp.Value {
		var avgs []float64
		var max float64
		min := math.MaxFloat64
		for _, ts := range m.Timeseries {
			for _, d := range ts.Data {
				if d.Average != nil {
					avgs = append(avgs, *d.Average)
				}
				if d.Maximum != nil && *d.Maximum > max {
					max = *d.Maximum
				}
				if d.Minimum != nil && *d.Minimum < min {
					min = *d.Minimum
				}
			}
		}
		if len(avgs) == 0 {
			continue
		}
		var sum float64
		for _, v := range avgs {
			sum += v
		}
		sort.Float64s(avgs)
		p95 := avgs[int(float64(len(avgs)-1)*0.95)]
		p5 := avgs[int(float64(len(avgs)-1)*0.05)]
		if min == math.MaxFloat64 {
			min = avgs[0]
		}
		out[m.Name.Value] = MetricSummary{Avg: sum / float64(len(avgs)), Min: min, Max: max, P5: p5, P95: p95, Points: len(avgs)}
	}
	return out, nil
}
