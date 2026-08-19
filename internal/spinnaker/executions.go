package spinnaker

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// SearchOptions controla a busca de execuções — espelha os parâmetros reais aceitos por
// GET /applications/{app}/executions/search, confirmados ao vivo (seção 2 do plano).
type SearchOptions struct {
	PipelineName           string // opcional — filtra por nome de pipeline (ex: "rollback-aks-global")
	Limit                  int    // 0 = usa o default do Gate
	TriggerTimeEndBoundary int64  // epoch ms, opcional — usado pra achar execuções antes de um instante
}

// SearchExecutions busca execuções de uma application Spinnaker. Nota confirmada ao vivo: o
// parâmetro "limit" parece se comportar por-pipeline dentro da application, não como um teto
// global — pipelines pouco usados (ex: rollback) podem não aparecer numa busca sem
// PipelineName se pipelines mais ativos (ex: deploy) já preencherem o "limit". Pra achar
// especificamente uma execução de rollback antiga, sempre preferir PipelineName explícito.
func (c *Client) SearchExecutions(ctx context.Context, application string, opts SearchOptions) ([]Execution, error) {
	if application == "" {
		return nil, fmt.Errorf("application vazio")
	}

	q := url.Values{}
	q.Set("expand", "true")
	if opts.Limit > 0 {
		q.Set("limit", strconv.Itoa(opts.Limit))
	} else {
		q.Set("limit", "20")
	}
	if opts.PipelineName != "" {
		q.Set("pipelineName", opts.PipelineName)
	}
	if opts.TriggerTimeEndBoundary > 0 {
		q.Set("triggerTimeEndBoundary", strconv.FormatInt(opts.TriggerTimeEndBoundary, 10))
	}

	path := "/applications/" + url.PathEscape(application) + "/executions/search?" + q.Encode()

	var executions []Execution
	if err := c.doGet(ctx, path, &executions); err != nil {
		return nil, err
	}
	return executions, nil
}

// GetExecution busca uma execução específica por ID — usado pra confirmar/inspecionar um
// executionId conhecido (ex: vindo de uma URL do Deck), sem precisar de application/pipelineName.
func (c *Client) GetExecution(ctx context.Context, executionID string) (*Execution, error) {
	if executionID == "" {
		return nil, fmt.Errorf("executionID vazio")
	}
	var exec Execution
	if err := c.doGet(ctx, "/pipelines/"+url.PathEscape(executionID), &exec); err != nil {
		return nil, err
	}
	return &exec, nil
}

// ListProjects busca os projetos Spinnaker (agrupamento tipo squad) — confirmado ao vivo via
// GET /projects (seção 10.2 do plano). Usado pra alimentar o seletor de projeto no perfil do
// usuário (Fase 2.5) — aqui só o cliente, sem cache/persistência (isso é responsabilidade do
// handler HTTP, Fase 2).
func (c *Client) ListProjects(ctx context.Context) ([]Project, error) {
	var projects []Project
	if err := c.doGet(ctx, "/projects", &projects); err != nil {
		return nil, err
	}
	return projects, nil
}
