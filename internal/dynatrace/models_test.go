package dynatrace

import "testing"

// TestExtractK8sCorrelation_MetadataFallback cobre o bug real corrigido: PROCESS_GROUP_INSTANCE
// (usada por clusters em modo OneAgent classicFullStack — a maioria da frota real, confirmado via
// kubectl contra clusters reais) não carrega tags úteis nem as properties planas usadas por
// CLOUD_APPLICATION/_INSTANCE — os dados K8s vêm de properties.metadata, uma lista de {key,value}.
// Sem esse fallback, ListMonitoredPods nunca via nenhum pod monitorado nesses clusters, mesmo com
// o OneAgent ativo em todos os nós (confirmado contra um tenant real: 0 -> 166 pods).
func TestExtractK8sCorrelation_MetadataFallback(t *testing.T) {
	e := &Entity{
		DisplayName: "calico-node calico-node (calico-node-fsxh5)",
		Tags:        []Tag{}, // PROCESS_GROUP_INSTANCE real não tem tags úteis aqui
		Properties: map[string]interface{}{
			"metadata": []interface{}{
				map[string]interface{}{"key": "KUBERNETES_NAMESPACE", "value": "calico-system"},
				map[string]interface{}{"key": "KUBERNETES_FULL_POD_NAME", "value": "calico-node-fsxh5"},
				map[string]interface{}{"key": "KUBERNETES_BASE_POD_NAME", "value": "calico-node"},
				map[string]interface{}{"key": "EXE_NAME", "value": "calico-node"}, // chave irrelevante, deve ser ignorada
			},
		},
	}

	corr := e.ExtractK8sCorrelation()
	if corr == nil {
		t.Fatal("esperava correlação não-nil")
	}
	if corr.Namespace != "calico-system" {
		t.Errorf("Namespace = %q, want %q", corr.Namespace, "calico-system")
	}
	if corr.PodName != "calico-node-fsxh5" {
		t.Errorf("PodName = %q, want %q", corr.PodName, "calico-node-fsxh5")
	}
	if corr.Workload != "calico-node" {
		t.Errorf("Workload = %q, want %q", corr.Workload, "calico-node")
	}
}

func TestExtractK8sCorrelation_MetadataFallback_StripsWildcardSuffix(t *testing.T) {
	e := &Entity{
		Properties: map[string]interface{}{
			"metadata": []interface{}{
				map[string]interface{}{"key": "KUBERNETES_NAMESPACE", "value": "regionalizacao-prd"},
				map[string]interface{}{"key": "KUBERNETES_FULL_POD_NAME", "value": "vv-regionalizacao-busca-api-544cddb99f-k75w5"},
				map[string]interface{}{"key": "KUBERNETES_BASE_POD_NAME", "value": "vv-regionalizacao-busca-api-*"},
			},
		},
	}

	corr := e.ExtractK8sCorrelation()
	if corr == nil {
		t.Fatal("esperava correlação não-nil")
	}
	if corr.Workload != "vv-regionalizacao-busca-api" {
		t.Errorf("Workload = %q, want sufixo -* removido", corr.Workload)
	}
}

func TestExtractK8sCorrelation_PropertiesFallback_CloudApplication(t *testing.T) {
	e := &Entity{
		Tags: []Tag{}, // CLOUD_APPLICATION/_INSTANCE reais não têm tags neste tenant
		Properties: map[string]interface{}{
			"namespaceName": "onboarding-pf-prd",
			"workloadName":  "onboarding-profile-frontend",
			"detectedName":  "onboarding-profile-frontend-6c578d4f9d-abcde",
		},
	}

	corr := e.ExtractK8sCorrelation()
	if corr == nil {
		t.Fatal("esperava correlação não-nil")
	}
	if corr.Namespace != "onboarding-pf-prd" {
		t.Errorf("Namespace = %q", corr.Namespace)
	}
	if corr.Workload != "onboarding-profile-frontend" {
		t.Errorf("Workload = %q", corr.Workload)
	}
	if corr.PodName != "onboarding-profile-frontend-6c578d4f9d-abcde" {
		t.Errorf("PodName = %q", corr.PodName)
	}
}

// TestExtractK8sCorrelation_CloudApplication_DetectedNameIsWorkload cobre o bug real confirmado
// contra um tenant real (cluster EKS asaplog-production, Cloud Native Full Stack): entidades
// CLOUD_APPLICATION reais NÃO têm a property "workloadName" — só "namespaceName" e "detectedName",
// e detectedName É o nome do Deployment/CronJob (a entidade representa o workload inteiro, não um
// pod). Sem diferenciar por e.Type, detectedName ia pro campo errado (PodName) e corr.Workload
// nunca era preenchido, quebrando ResolveEntityForWorkload (Fase 1 e o overlay de problems da
// Fase 2) para qualquer cluster que use Cloud Native Full Stack.
func TestExtractK8sCorrelation_CloudApplication_DetectedNameIsWorkload(t *testing.T) {
	e := &Entity{
		Type: "CLOUD_APPLICATION",
		Tags: []Tag{}, // confirmado real: CLOUD_APPLICATION não tem tags neste tenant
		Properties: map[string]interface{}{
			"namespaceName": "asaplog",
			"detectedName":  "entregas-service",
		},
	}

	corr := e.ExtractK8sCorrelation()
	if corr == nil {
		t.Fatal("esperava correlação não-nil")
	}
	if corr.Namespace != "asaplog" {
		t.Errorf("Namespace = %q, want %q", corr.Namespace, "asaplog")
	}
	if corr.Workload != "entregas-service" {
		t.Errorf("Workload = %q, want %q (detectedName deveria virar Workload, não PodName, pra CLOUD_APPLICATION)", corr.Workload, "entregas-service")
	}
	if corr.PodName != "" {
		t.Errorf("PodName deveria ficar vazio pra CLOUD_APPLICATION (detectedName não é nome de pod aqui), got %q", corr.PodName)
	}
}

// TestExtractK8sCorrelation_CloudApplicationInstance_DetectedNameIsPodName garante que a correção
// acima não regrediu o comportamento já validado de CLOUD_APPLICATION_INSTANCE (pod-level) — essa
// continua usando detectedName como PodName.
func TestExtractK8sCorrelation_CloudApplicationInstance_DetectedNameIsPodName(t *testing.T) {
	e := &Entity{
		Type: "CLOUD_APPLICATION_INSTANCE",
		Tags: []Tag{},
		Properties: map[string]interface{}{
			"namespaceName": "onboarding-pf-prd",
			"workloadName":  "onboarding-profile-frontend",
			"detectedName":  "onboarding-profile-frontend-6c578d4f9d-abcde",
		},
	}

	corr := e.ExtractK8sCorrelation()
	if corr == nil {
		t.Fatal("esperava correlação não-nil")
	}
	if corr.PodName != "onboarding-profile-frontend-6c578d4f9d-abcde" {
		t.Errorf("PodName = %q — regressão: CLOUD_APPLICATION_INSTANCE deveria continuar mapeando detectedName pra PodName", corr.PodName)
	}
	if corr.Workload != "onboarding-profile-frontend" {
		t.Errorf("Workload = %q, want %q (via workloadName, não afetado pela correção)", corr.Workload, "onboarding-profile-frontend")
	}
}

func TestExtractK8sCorrelation_NoData_ReturnsNil(t *testing.T) {
	e := &Entity{DisplayName: "", Tags: []Tag{}, Properties: map[string]interface{}{}}
	if corr := e.ExtractK8sCorrelation(); corr != nil {
		t.Errorf("esperava nil pra entidade sem nenhum dado correlacionável, got %+v", corr)
	}
}

// TestExtractK8sCorrelation_MetadataWorkloadBeatsGenericAppNameTag cobre o bug real confirmado
// contra um cluster AKS real (akspriv-adanalytics-prd): a tag "app.kubernetes.io/name" é usada
// nesse tenant como identificador de PRODUTO/grupo (ex: "adanalytics" para TODOS os deployments
// comercial-sortimento-*, não o nome do Deployment específico) — mas o código processava essa tag
// ANTES do bloco de properties.metadata, então KUBERNETES_BASE_POD_NAME (o nome real do
// Deployment) nunca tinha chance de vencer pra PGIs que já tinham essa tag. Resultado real:
// ResolveEntityIDsForWorkload nunca encontrava a entidade certa de nenhum Deployment desse
// cluster (overlay de problems da Fase 2 sempre vazio), mesmo com PGIs reais existindo.
func TestExtractK8sCorrelation_MetadataWorkloadBeatsGenericAppNameTag(t *testing.T) {
	e := &Entity{
		DisplayName: "gunicorn comercial-sortimento-kafka-consumidor-* (comercial-sortimento-kafka-consumidor-79d47b475-7ggqb)",
		Tags: []Tag{
			// tag real confirmada contra o tenant — valor de PRODUTO, não do Deployment
			{Context: "KUBERNETES", Key: "app.kubernetes.io/name", Value: "adanalytics"},
		},
		Properties: map[string]interface{}{
			"metadata": []interface{}{
				map[string]interface{}{"key": "KUBERNETES_NAMESPACE", "value": "adanalytics-prd"},
				map[string]interface{}{"key": "KUBERNETES_FULL_POD_NAME", "value": "comercial-sortimento-kafka-consumidor-79d47b475-7ggqb"},
				map[string]interface{}{"key": "KUBERNETES_BASE_POD_NAME", "value": "comercial-sortimento-kafka-consumidor-*"},
			},
		},
	}

	corr := e.ExtractK8sCorrelation()
	if corr == nil {
		t.Fatal("esperava correlação não-nil")
	}
	if corr.Workload != "comercial-sortimento-kafka-consumidor" {
		t.Errorf("Workload = %q, want %q (metadata deveria vencer sobre a tag app.kubernetes.io/name)", corr.Workload, "comercial-sortimento-kafka-consumidor")
	}
	if corr.AppName != "adanalytics" {
		t.Errorf("AppName = %q, want %q (a tag continua populando AppName normalmente, só não deveria vencer Workload)", corr.AppName, "adanalytics")
	}
}

// TestExtractK8sCorrelation_ExplicitWorkloadTagStillWins garante que a correção acima não regride
// o caso em que a tag "kubernetes.workload.name" (nome inequívoco, não um heurístico como
// app.kubernetes.io/name) realmente existe — essa continua tendo prioridade sobre o metadata.
func TestExtractK8sCorrelation_ExplicitWorkloadTagStillWins(t *testing.T) {
	e := &Entity{
		Tags: []Tag{
			{Key: "kubernetes.workload.name", Value: "workload-explicito"},
		},
		Properties: map[string]interface{}{
			"metadata": []interface{}{
				map[string]interface{}{"key": "KUBERNETES_BASE_POD_NAME", "value": "workload-do-metadata-*"},
			},
		},
	}
	corr := e.ExtractK8sCorrelation()
	if corr == nil {
		t.Fatal("esperava correlação não-nil")
	}
	if corr.Workload != "workload-explicito" {
		t.Errorf("Workload = %q, want %q (tag explícita deveria vencer o metadata)", corr.Workload, "workload-explicito")
	}
}
