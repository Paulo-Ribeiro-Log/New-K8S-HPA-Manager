package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	"k8s-hpa-manager/internal/config"
)

// clustersTestKubeconfigYAML declara 2 clusters AKS (.azmk8s.io) e 1 EKS (.eks.amazonaws.com) —
// o bastante pra exercitar o cross-reference com clusters-config.json feito em ClusterHandler.List
// sem depender de nenhuma chamada real ao Azure/AWS CLI.
const clustersTestKubeconfigYAML = `apiVersion: v1
kind: Config
clusters:
- name: aks-backoffice-admin
  cluster:
    server: https://aks-backoffice-admin.hcp.brazilsouth.azmk8s.io
- name: aks-sem-tag-admin
  cluster:
    server: https://aks-sem-tag-admin.hcp.brazilsouth.azmk8s.io
- name: arn:aws:eks:us-east-1:123456789012:cluster/eks-cluster
  cluster:
    server: https://ABC.gr7.us-east-1.eks.amazonaws.com
contexts:
- name: aks-backoffice-admin
  context:
    cluster: aks-backoffice-admin
- name: aks-sem-tag-admin
  context:
    cluster: aks-sem-tag-admin
- name: eks-context
  context:
    cluster: arn:aws:eks:us-east-1:123456789012:cluster/eks-cluster
current-context: aks-backoffice-admin
`

// TestClusterHandler_List_PopulatesJourneyForAKS cobre a mudança real deste PR: o cross-reference
// entre DiscoverClusters() (puramente kubeconfig) e clusters-config.json (onde as tags do Azure
// ficam persistidas pelo autodiscover) precisa casar o nome do cluster mesmo quando um lado tem o
// sufixo -admin (kubeconfig) e o outro não (clusters-config.json costuma ser salvo sem, mesmo
// padrão já documentado em StagingContext.tsx/ensureAdminSuffix). EKS nunca deveria ser
// consultado nesse cross-reference (a tag é exclusivamente Azure).
func TestClusterHandler_List_PopulatesJourneyForAKS(t *testing.T) {
	gin.SetMode(gin.TestMode)

	home := t.TempDir()
	t.Setenv("HOME", home)

	kubeconfigPath := filepath.Join(home, "kubeconfig.yaml")
	if err := os.WriteFile(kubeconfigPath, []byte(clustersTestKubeconfigYAML), 0600); err != nil {
		t.Fatalf("WriteFile(kubeconfig) error = %v", err)
	}

	appDir := filepath.Join(home, ".k8s-hpa-manager")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	fixture := []config.ClusterConfig{
		// Salvo sem o sufixo -admin — precisa casar mesmo assim com o context
		// "aks-backoffice-admin" do kubeconfig.
		{Name: "aks-backoffice", ResourceGroup: "rg-bo", Tags: map[string]string{"jornada": "backoffice"}},
		{Name: "aks-sem-tag", ResourceGroup: "rg-x"},
	}
	data, err := json.Marshal(fixture)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "clusters-config.json"), data, 0644); err != nil {
		t.Fatalf("WriteFile(clusters-config.json) error = %v", err)
	}

	km, err := config.NewKubeConfigManager(kubeconfigPath)
	if err != nil {
		t.Fatalf("NewKubeConfigManager() error = %v", err)
	}

	handler := NewClusterHandler(km)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/clusters", nil)

	handler.List(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d — body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		Data []struct {
			Name          string `json:"name"`
			CloudProvider string `json:"cloud_provider"`
			Journey       string `json:"journey"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal(response) error = %v — body: %s", err, w.Body.String())
	}

	byName := make(map[string]string) // name -> journey
	providerByName := make(map[string]string)
	for _, c := range resp.Data {
		byName[c.Name] = c.Journey
		providerByName[c.Name] = c.CloudProvider
	}

	if providerByName["aks-backoffice-admin"] != "aks" {
		t.Fatalf("cloud_provider de aks-backoffice-admin = %q, want aks", providerByName["aks-backoffice-admin"])
	}
	if got := byName["aks-backoffice-admin"]; got != "backoffice" {
		t.Errorf("journey de aks-backoffice-admin = %q, want %q (deve casar com clusters-config.json apesar do sufixo -admin)", got, "backoffice")
	}
	if got := byName["aks-sem-tag-admin"]; got != "" {
		t.Errorf("journey de aks-sem-tag-admin = %q, want vazio (cluster sem tag jornada)", got)
	}
	// DiscoverClusters() normaliza o Name de clusters EKS pro segmento depois da última "/" do ARN.
	if _, ok := providerByName["eks-cluster"]; !ok {
		t.Fatalf("cluster EKS não apareceu na resposta — clusters recebidos: %+v", resp.Data)
	}
	// EKS nunca é cruzado contra clusters-config.json (tag é exclusiva do Azure).
	if got := byName["eks-cluster"]; got != "" {
		t.Errorf("journey de cluster EKS = %q, want vazio (tag é exclusiva AKS)", got)
	}
}
