package certificates

import (
	"context"
	"fmt"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MutatingWebhookEntry representa uma entrada webhooks[] dentro de um MutatingWebhookConfiguration,
// com o caBundle já parseado (quando possível) pra exibição — mesmo padrão de "melhor esforço" do
// resto deste pacote (parsePEMChain nunca falha "duro", só devolve os campos vazios).
type MutatingWebhookEntry struct {
	Name             string     `json:"name"`
	ServiceName      string     `json:"serviceName,omitempty"` // clientConfig.service.name — quando o webhook aponta pra um Service dentro do próprio cluster (caso comum: Delinea DSV injector, Istio sidecar injector)
	ServiceNamespace string     `json:"serviceNamespace,omitempty"`
	URL              string     `json:"url,omitempty"` // clientConfig.url — quando aponta pra um endpoint externo, sem Service
	CABundleEmpty    bool       `json:"caBundleEmpty"` // caBundle vazio — comum em clusters com CA injector automático (ex: cert-manager), onde essa app não deveria sobrescrever manualmente
	CABundleSubject  string     `json:"caBundleSubject,omitempty"`
	CABundleIssuer   string     `json:"caBundleIssuer,omitempty"`
	CABundleNotAfter *time.Time `json:"caBundleNotAfter,omitempty"`
	CABundleStatus   string     `json:"caBundleStatus,omitempty"` // "valid" | "expiring" | "expired" — classifyExpiry, mesmo limiar do resto do pacote
	CABundleDays     int        `json:"caBundleDays,omitempty"`
}

// MutatingWebhookConfigSummary representa um MutatingWebhookConfiguration inteiro — objeto
// CLUSTER-SCOPED nativo do K8s (admissionregistration.k8s.io/v1), diferente de CRDs: já vem
// vendorizado no client-go desta app, sem precisar do fallback "kubectl shell" documentado no
// CLAUDE.md pra CRDs sem dynamic client.
type MutatingWebhookConfigSummary struct {
	Name     string                 `json:"name"`
	Cluster  string                 `json:"cluster"`
	Webhooks []MutatingWebhookEntry `json:"webhooks"`
}

// ListMutatingWebhookConfigurations lista todos os MutatingWebhookConfigurations de um cluster.
func (s *Scanner) ListMutatingWebhookConfigurations(ctx context.Context, cluster string) ([]MutatingWebhookConfigSummary, error) {
	clientset, err := s.kubeManager.GetClient(cluster)
	if err != nil {
		return nil, fmt.Errorf("erro ao obter client para %s: %w", cluster, err)
	}

	list, err := clientset.AdmissionregistrationV1().MutatingWebhookConfigurations().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("erro ao listar MutatingWebhookConfigurations em %s: %w", cluster, err)
	}

	result := make([]MutatingWebhookConfigSummary, 0, len(list.Items))
	for _, cfg := range list.Items {
		summary := MutatingWebhookConfigSummary{
			Name:     cfg.Name,
			Cluster:  cluster,
			Webhooks: make([]MutatingWebhookEntry, 0, len(cfg.Webhooks)),
		}
		for _, wh := range cfg.Webhooks {
			summary.Webhooks = append(summary.Webhooks, buildMutatingWebhookEntry(wh))
		}
		result = append(result, summary)
	}

	return result, nil
}

// buildMutatingWebhookEntry resume uma entrada webhooks[] — nunca falha "duro": um caBundle
// malformado/vazio só deixa os campos de exibição vazios, sem descartar a entrada inteira (mesmo
// espírito de parsePEMChain, que já tolera blocos individuais malformados).
func buildMutatingWebhookEntry(wh admissionregistrationv1.MutatingWebhook) MutatingWebhookEntry {
	entry := MutatingWebhookEntry{Name: wh.Name}

	if wh.ClientConfig.Service != nil {
		entry.ServiceName = wh.ClientConfig.Service.Name
		entry.ServiceNamespace = wh.ClientConfig.Service.Namespace
	}
	if wh.ClientConfig.URL != nil {
		entry.URL = *wh.ClientConfig.URL
	}

	if len(wh.ClientConfig.CABundle) == 0 {
		entry.CABundleEmpty = true
		return entry
	}

	certs, err := parsePEMChain(wh.ClientConfig.CABundle)
	if err != nil || len(certs) == 0 {
		return entry
	}

	leaf := certs[0]
	entry.CABundleSubject = certSubjectDisplayName(leaf)
	entry.CABundleIssuer = leaf.Issuer.CommonName
	notAfter := leaf.NotAfter
	entry.CABundleNotAfter = &notAfter
	entry.CABundleStatus, entry.CABundleDays = classifyExpiry(leaf.NotAfter)

	return entry
}

// UpdateMutatingWebhookCABundleRequest — sobrescreve o clientConfig.caBundle de uma ou mais
// entradas webhooks[] dentro do MESMO MutatingWebhookConfiguration. WebhookNames vazio atualiza
// TODAS as entradas do config (caso comum: um produto registra várias entradas apontando pro
// mesmo Service/CA — ex: Istio sidecar injector tem 2, uma pra injeção automática por namespace e
// outra pra injeção manual via annotation, ambas com o mesmo caBundle).
type UpdateMutatingWebhookCABundleRequest struct {
	Cluster      string   `json:"cluster"`
	ConfigName   string   `json:"configName"`
	WebhookNames []string `json:"webhookNames,omitempty"`
	CABundlePEM  string   `json:"caBundlePEM"`
}

// UpdateMutatingWebhookCABundleResult descreve o resultado — inclui o estado ANTERIOR (before) de
// cada entrada afetada, pra permitir reverter manualmente caso algo dê errado (não existe um
// RollbackStore dedicado pra isso, diferente do fluxo de Secret TLS — essa operação é rara o
// bastante, e o antigo caBundle é sempre recuperável via `kubectl get ... -o yaml` antes de
// aplicar, ou junto do próprio produto que instalou o webhook).
type UpdateMutatingWebhookCABundleResult struct {
	UpdatedCount int                    `json:"updatedCount"`
	UpdatedNames []string               `json:"updatedNames"`
	Before       []MutatingWebhookEntry `json:"before"`
	NewSubject   string                 `json:"newSubject"`
	NewIssuer    string                 `json:"newIssuer"`
	NewNotAfter  *time.Time             `json:"newNotAfter,omitempty"`
}

// UpdateMutatingWebhookCABundle sobrescreve o caBundle de um MutatingWebhookConfiguration —
// usado pra rotacionar o CA que o kube-apiserver confia ao chamar webhooks de terceiro (ex:
// Delinea DSV injector, Istio sidecar injector) quando o certificado de SERVIÇO do webhook é
// renovado (via Secret, fora do controle deste método) mas o objeto
// MutatingWebhookConfiguration em si — normalmente gravado uma única vez pelo instalador do
// produto — nunca é atualizado junto. Sintoma real sem essa correção: a chamada do apiserver ao
// webhook falha por TLS não confiável (x509: certificate signed by unknown authority) mesmo com
// o Secret do lado do servidor já correto.
func (s *Scanner) UpdateMutatingWebhookCABundle(ctx context.Context, req UpdateMutatingWebhookCABundleRequest) (*UpdateMutatingWebhookCABundleResult, error) {
	caBundle := []byte(req.CABundlePEM)
	newCerts, err := parsePEMChain(caBundle)
	if err != nil || len(newCerts) == 0 {
		return nil, fmt.Errorf("CA bundle PEM inválido: nenhum certificado encontrado")
	}

	clientset, err := s.kubeManager.GetClient(req.Cluster)
	if err != nil {
		return nil, fmt.Errorf("erro ao obter client para %s: %w", req.Cluster, err)
	}

	webhooksAPI := clientset.AdmissionregistrationV1().MutatingWebhookConfigurations()

	cfg, err := webhooksAPI.Get(ctx, req.ConfigName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("erro ao obter MutatingWebhookConfiguration %s em %s: %w", req.ConfigName, req.Cluster, err)
	}

	nameFilter := make(map[string]bool, len(req.WebhookNames))
	for _, n := range req.WebhookNames {
		nameFilter[n] = true
	}

	result := &UpdateMutatingWebhookCABundleResult{
		Before: make([]MutatingWebhookEntry, 0, len(cfg.Webhooks)),
	}

	for i := range cfg.Webhooks {
		if len(nameFilter) > 0 && !nameFilter[cfg.Webhooks[i].Name] {
			continue
		}
		result.Before = append(result.Before, buildMutatingWebhookEntry(cfg.Webhooks[i]))
		cfg.Webhooks[i].ClientConfig.CABundle = caBundle
		result.UpdatedNames = append(result.UpdatedNames, cfg.Webhooks[i].Name)
	}
	result.UpdatedCount = len(result.UpdatedNames)

	if result.UpdatedCount == 0 {
		return nil, fmt.Errorf("nenhuma entrada webhooks[] encontrada com os nomes informados em %s", req.ConfigName)
	}

	if _, err := webhooksAPI.Update(ctx, cfg, metav1.UpdateOptions{}); err != nil {
		return nil, fmt.Errorf("erro ao atualizar MutatingWebhookConfiguration %s em %s: %w", req.ConfigName, req.Cluster, err)
	}

	leaf := newCerts[0]
	result.NewSubject = certSubjectDisplayName(leaf)
	result.NewIssuer = leaf.Issuer.CommonName
	notAfter := leaf.NotAfter
	result.NewNotAfter = &notAfter

	return result, nil
}
