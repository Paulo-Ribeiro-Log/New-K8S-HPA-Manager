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

func TestExtractK8sCorrelation_NoData_ReturnsNil(t *testing.T) {
	e := &Entity{DisplayName: "", Tags: []Tag{}, Properties: map[string]interface{}{}}
	if corr := e.ExtractK8sCorrelation(); corr != nil {
		t.Errorf("esperava nil pra entidade sem nenhum dado correlacionável, got %+v", corr)
	}
}
