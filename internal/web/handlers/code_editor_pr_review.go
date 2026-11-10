package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// Revisão do PR (aprovar, pedir alterações ou comentar) a partir do modal do diff.
//
// É uma escrita no GitHub EM NOME DO USUÁRIO (o PAT do perfil), por isso:
//   - a revisão vai presa ao commit que o usuário viu no diff (head_sha): se o PR recebeu commits
//     novos desde então, o envio é recusado (409) — nunca se aprova código que não foi visto;
//   - PR fechado/mergeado não recebe revisão;
//   - o GitHub não deixa aprovar o próprio PR; a tela e o servidor avisam antes;
//   - o texto do comentário não vai para o Change History (só o evento, o PR e o commit).

const (
	reviewEventApprove = "APPROVE"
	reviewEventChanges = "REQUEST_CHANGES"
	reviewEventComment = "COMMENT"

	reviewBodyMax  = 20000
	reviewMaxPages = 5
)

// ReviewSummary é a revisão mais recente de cada revisor (o estado que vale hoje).
type ReviewSummary struct {
	User        string `json:"user"`
	State       string `json:"state"` // APPROVED | CHANGES_REQUESTED | DISMISSED | COMMENTED
	SubmittedAt string `json:"submitted_at"`
	CommitID    string `json:"commit_id"`
	Stale       bool   `json:"stale"` // foi feita num commit anterior ao atual do PR
}

type ghPullDetail struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	State   string `json:"state"`
	Merged  bool   `json:"merged"`
	Draft   bool   `json:"draft"`
	HTMLURL string `json:"html_url"`
	User    struct {
		Login string `json:"login"`
	} `json:"user"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
	Head struct {
		SHA string `json:"sha"`
	} `json:"head"`
}

type ghReview struct {
	ID          int64  `json:"id"`
	State       string `json:"state"`
	SubmittedAt string `json:"submitted_at"`
	CommitID    string `json:"commit_id"`
	User        struct {
		Login string `json:"login"`
	} `json:"user"`
}

// ghAPIError é um erro da API do GitHub com o status HTTP.
type ghAPIError struct {
	Status  int
	Message string
}

func (e *ghAPIError) Error() string { return e.Message }

// githubPostJSON faz um POST autenticado na API do GitHub e devolve a resposta decodificada em out.
func githubPostJSON(ctx context.Context, token, apiURL string, payload, out any) error {
	return githubSendJSON(ctx, http.MethodPost, token, apiURL, payload, out)
}

// githubSendJSON é o githubPostJSON com o método à escolha (POST, PATCH).
func githubSendJSON(ctx context.Context, method, token, apiURL string, payload, out any) error {
	b, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, method, apiURL, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return &ghAPIError{Status: http.StatusBadGateway, Message: "falha ao contatar GitHub API: " + err.Error()}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var ghErr struct {
			Message string `json:"message"`
			Errors  []any  `json:"errors"`
		}
		_ = json.Unmarshal(body, &ghErr)
		msg := ghErr.Message
		if len(ghErr.Errors) > 0 {
			msg += fmt.Sprintf(": %v", ghErr.Errors[0])
		}
		if msg == "" {
			msg = fmt.Sprintf("GitHub API retornou %d", resp.StatusCode)
		}
		return &ghAPIError{Status: resp.StatusCode, Message: msg}
	}
	if out != nil {
		return json.Unmarshal(body, out)
	}
	return nil
}

// summarizeReviews fica com a última revisão de cada revisor que mude o estado (aprovar, pedir
// alterações ou descartar); comentário solto só conta se o revisor não tem nada além disso. A saída
// é ordenada por usuário.
func summarizeReviews(reviews []ghReview, headSHA string) []ReviewSummary {
	latest := map[string]ghReview{}
	for _, r := range reviews { // a API devolve do mais antigo ao mais novo
		if r.User.Login == "" || r.State == "PENDING" {
			continue
		}
		prev, seen := latest[r.User.Login]
		if r.State == "COMMENTED" && seen && prev.State != "COMMENTED" {
			continue // comentar depois de aprovar não desfaz a aprovação
		}
		latest[r.User.Login] = r
	}
	out := make([]ReviewSummary, 0, len(latest))
	for u, r := range latest {
		out = append(out, ReviewSummary{User: u, State: r.State, SubmittedAt: r.SubmittedAt, CommitID: r.CommitID,
			Stale: headSHA != "" && r.CommitID != "" && r.CommitID != headSHA})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].User < out[j].User })
	return out
}

func listPRReviews(ctx context.Context, token, owner, repo string, number int) ([]ghReview, error) {
	var all []ghReview
	for page := 1; page <= reviewMaxPages; page++ {
		var batch []ghReview
		u := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/reviews?per_page=100&page=%d", githubAPIBase, owner, repo, number, page)
		if err := githubGetJSON(ctx, token, u, &batch); err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if len(batch) < 100 {
			break
		}
	}
	return all, nil
}

func getPRDetail(ctx context.Context, token, owner, repo string, number int) (ghPullDetail, error) {
	var d ghPullDetail
	err := githubGetJSON(ctx, token, fmt.Sprintf("%s/repos/%s/%s/pulls/%d", githubAPIBase, owner, repo, number), &d)
	return d, err
}

func getViewerLogin(ctx context.Context, token string) (string, error) {
	var u struct {
		Login string `json:"login"`
	}
	err := githubGetJSON(ctx, token, githubAPIBase+"/user", &u)
	return u.Login, err
}

// selectReviewPR acha o PR pedido (?pr=) ou o aberto mais recente da branch.
func (h *CodeEditorHandler) selectReviewPR(c *gin.Context, ctx context.Context, t planTarget, requested string) (int, bool) {
	prs, err := listBranchPRs(ctx, t.token, t.owner, t.repo, t.branch)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return 0, false
	}
	if len(prs) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "a branch " + t.branch + " não tem Pull Request"})
		return 0, false
	}
	return pickPR(prs, requested).Number, true
}

// GetPRReviews — GET /api/v1/code-editor/repos/:id/pr/reviews?pr=<n>&branch=<b>&profile_id=<id>
// Só leitura: revisões atuais do PR, o autor, quem está logado (dono do PAT) e se dá para aprovar.
func (h *CodeEditorHandler) GetPRReviews(c *gin.Context) {
	t, ok := h.resolvePlanTarget(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 45*time.Second)
	defer cancel()

	number, ok := h.selectReviewPR(c, ctx, t, c.Query("pr"))
	if !ok {
		return
	}
	pr, err := getPRDetail(ctx, t.token, t.owner, t.repo, number)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	reviews, err := listPRReviews(ctx, t.token, t.owner, t.repo, number)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	viewer, vErr := getViewerLogin(ctx, t.token) // falha aqui não impede de ver as revisões
	resp := gin.H{
		"pr": gin.H{"number": pr.Number, "title": pr.Title, "state": prState(pr), "draft": pr.Draft, "url": pr.HTMLURL,
			"author": pr.User.Login, "base": pr.Base.Ref, "head_sha": pr.Head.SHA},
		"viewer":  viewer,
		"reviews": summarizeReviews(reviews, pr.Head.SHA),
	}
	canComment, canApprove, reason := reviewPermissions(pr, viewer, vErr == nil)
	resp["can_comment"], resp["can_approve"], resp["approve_block_reason"] = canComment, canApprove, reason
	c.JSON(http.StatusOK, resp)
}

func prState(p ghPullDetail) string {
	if p.Merged {
		return "merged"
	}
	return p.State
}

// reviewPermissions diz se o usuário pode comentar e se pode aprovar/pedir alterações, e por que não.
func reviewPermissions(pr ghPullDetail, viewer string, viewerKnown bool) (canComment, canApprove bool, reason string) {
	if st := prState(pr); st != "open" {
		return false, false, "o PR está " + map[string]string{"merged": "mergeado", "closed": "fechado"}[st] + ": não recebe mais revisão"
	}
	if viewerKnown && viewer != "" && strings.EqualFold(viewer, pr.User.Login) {
		return true, false, "você é o autor do PR: o GitHub não permite aprovar nem pedir alterações no próprio PR (só comentar)"
	}
	return true, true, ""
}

type submitReviewRequest struct {
	PR      int    `json:"pr"`
	Event   string `json:"event"`
	Body    string `json:"body"`
	HeadSHA string `json:"head_sha"` // o commit que o usuário viu no diff
}

// SubmitPRReview — POST /api/v1/code-editor/repos/:id/pr/review?branch=<b>&profile_id=<id>
// Body: { "pr", "event": APPROVE|REQUEST_CHANGES|COMMENT, "body", "head_sha" }
func (h *CodeEditorHandler) SubmitPRReview(c *gin.Context) {
	var req submitReviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.Event = strings.ToUpper(strings.TrimSpace(req.Event))
	req.Body = strings.TrimSpace(req.Body)
	switch {
	case req.PR <= 0:
		c.JSON(http.StatusBadRequest, gin.H{"error": "pr é obrigatório"})
		return
	case req.Event != reviewEventApprove && req.Event != reviewEventChanges && req.Event != reviewEventComment:
		c.JSON(http.StatusBadRequest, gin.H{"error": "event deve ser APPROVE, REQUEST_CHANGES ou COMMENT"})
		return
	case req.HeadSHA == "":
		c.JSON(http.StatusBadRequest, gin.H{"error": "head_sha é obrigatório: a revisão precisa apontar o commit que foi visto no diff"})
		return
	case len(req.Body) > reviewBodyMax:
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("comentário longo demais (máximo %d caracteres)", reviewBodyMax)})
		return
	case req.Event != reviewEventApprove && req.Body == "":
		c.JSON(http.StatusBadRequest, gin.H{"error": "pedir alterações e comentar exigem um texto"})
		return
	}
	t, ok := h.resolvePlanTarget(c)
	if !ok {
		return
	}
	start := time.Now()
	ctx, cancel := context.WithTimeout(c.Request.Context(), 45*time.Second)
	defer cancel()

	fail := func(status int, msg string) {
		logOperation(c, h.historyTracker, "code_editor_pr_review", fmt.Sprintf("%s/%s#%d", t.owner, t.repo, req.PR), "", nil,
			map[string]interface{}{"repo_id": c.Param("id"), "number": req.PR, "event": req.Event, "head_sha": req.HeadSHA}, start, fmt.Errorf("%s", msg))
		c.JSON(status, gin.H{"error": msg})
	}

	pr, err := getPRDetail(ctx, t.token, t.owner, t.repo, req.PR)
	if err != nil {
		fail(http.StatusBadGateway, err.Error())
		return
	}
	viewer, vErr := getViewerLogin(ctx, t.token)
	canComment, canApprove, reason := reviewPermissions(pr, viewer, vErr == nil)
	if !canComment {
		fail(http.StatusConflict, reason)
		return
	}
	if req.Event != reviewEventComment && !canApprove {
		fail(http.StatusUnprocessableEntity, reason)
		return
	}
	if !strings.EqualFold(pr.Head.SHA, req.HeadSHA) {
		fail(http.StatusConflict, fmt.Sprintf("o PR recebeu novos commits desde que você abriu o diff (agora em %s, você viu %s): reabra o diff, revise o que mudou e envie de novo", short(pr.Head.SHA), short(req.HeadSHA)))
		return
	}

	payload := map[string]any{"commit_id": req.HeadSHA, "event": req.Event}
	if req.Body != "" {
		payload["body"] = req.Body
	}
	var out struct {
		ID      int64  `json:"id"`
		State   string `json:"state"`
		HTMLURL string `json:"html_url"`
	}
	if err := githubPostJSON(ctx, t.token, fmt.Sprintf("%s/repos/%s/%s/pulls/%d/reviews", githubAPIBase, t.owner, t.repo, req.PR), payload, &out); err != nil {
		msg := err.Error()
		status := http.StatusBadGateway
		if ge, ok := err.(*ghAPIError); ok {
			status = ge.Status
			if status == http.StatusForbidden && strings.Contains(msg, "Resource not accessible") {
				msg = "O PAT deste perfil não tem permissão para revisar PRs (precisa de 'Pull requests: Read and write'). " + msg
			}
			if status < 400 || status > 599 {
				status = http.StatusBadGateway
			}
		}
		fail(status, msg)
		return
	}
	logOperation(c, h.historyTracker, "code_editor_pr_review", fmt.Sprintf("%s/%s#%d", t.owner, t.repo, req.PR), "", nil,
		map[string]interface{}{"repo_id": c.Param("id"), "number": req.PR, "event": req.Event, "head_sha": req.HeadSHA, "body_len": len(req.Body), "review_id": out.ID}, start, nil)
	c.JSON(http.StatusOK, gin.H{"review": gin.H{"id": out.ID, "state": out.State, "url": out.HTMLURL}})
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
