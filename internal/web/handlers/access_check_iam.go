package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"k8s-hpa-manager/internal/config"
)

// iamAdminRoles são as roles nativas do Azure que concedem `listClusterAdminCredential` —
// ou seja, permitem buscar o kubeconfig ADMIN (certificado `system:masters`), que IGNORA
// completamente o RBAC do Kubernetes (nativo ou impersonado). Acesso concedido por essas
// roles é estruturalmente invisível a qualquer checagem via SelfSubjectRulesReview/
// SelfSubjectAccessReview — inclusive `kubectl auth can-i --as` sofre da mesma limitação.
var iamAdminRoles = map[string]bool{
	"Azure Kubernetes Service Cluster Admin Role": true,
	"Azure Kubernetes Service RBAC Cluster Admin": true, // só relevante se enableAzureRbac=true no cluster
}

// azRoleAssignment é o formato retornado por `az role assignment list` que nos interessa.
type azRoleAssignment struct {
	PrincipalName      string `json:"principalName"`
	PrincipalType      string `json:"principalType"`
	RoleDefinitionName string `json:"roleDefinitionName"`
}

type iamCacheEntry struct {
	assignments []azRoleAssignment
	expiresAt   time.Time
}

var (
	iamCacheMu sync.RWMutex
	iamCache   = make(map[string]iamCacheEntry)
)

const iamCacheTTL = 45 * time.Minute

// getAKSResourceRoleAssignments lista os Role Assignments (IAM) do recurso AKS no Azure —
// escopo diferente do RBAC do Kubernetes (RoleBinding/ClusterRoleBinding). Só se aplica a
// clusters AKS; para GKE/EKS retorna nil sem erro (não há equivalente implementado ainda).
// Resultado cacheado por cluster (não depende do e-mail consultado).
func getAKSResourceRoleAssignments(ctx context.Context, kubeManager *config.KubeConfigManager, clusterCtx string) ([]azRoleAssignment, error) {
	if detectSNATProvider(kubeManager.GetServerURL(clusterCtx), clusterCtx) != "aks" {
		return nil, nil
	}

	cacheKey := strings.TrimSuffix(clusterCtx, "-admin")
	iamCacheMu.RLock()
	if entry, ok := iamCache[cacheKey]; ok && time.Now().Before(entry.expiresAt) {
		iamCacheMu.RUnlock()
		return entry.assignments, nil
	}
	iamCacheMu.RUnlock()

	cfg, err := findClusterInConfig(clusterCtx)
	if err != nil {
		return nil, fmt.Errorf("cluster não encontrado na config AKS: %w", err)
	}

	idCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	idOut, err := exec.CommandContext(idCtx, "az", "aks", "show",
		"--name", strings.TrimSuffix(cfg.ClusterName, "-admin"),
		"--resource-group", cfg.ResourceGroup,
		"--subscription", cfg.Subscription,
		"--query", "id", "-o", "tsv").Output()
	if err != nil {
		return nil, fmt.Errorf("az aks show (resource id) falhou: %w", err)
	}
	resourceID := strings.TrimSpace(string(idOut))
	if resourceID == "" {
		return nil, fmt.Errorf("resource id vazio para o cluster %s", clusterCtx)
	}

	raCtx, cancel2 := context.WithTimeout(ctx, 20*time.Second)
	defer cancel2()
	raOut, err := exec.CommandContext(raCtx, "az", "role", "assignment", "list",
		"--scope", resourceID,
		"--query", "[?principalType=='Group'].{principalName:principalName,principalType:principalType,roleDefinitionName:roleDefinitionName}",
		"-o", "json").Output()
	if err != nil {
		return nil, fmt.Errorf("az role assignment list falhou: %w", err)
	}

	var assignments []azRoleAssignment
	if err := json.Unmarshal(raOut, &assignments); err != nil {
		return nil, fmt.Errorf("falha ao parsear role assignments: %w", err)
	}

	iamCacheMu.Lock()
	iamCache[cacheKey] = iamCacheEntry{assignments: assignments, expiresAt: time.Now().Add(iamCacheTTL)}
	iamCacheMu.Unlock()

	return assignments, nil
}

// iamAdminMatchDTO é exposto ao frontend quando um grupo do analista tem uma role de IAM
// que concede acesso admin bypassando o RBAC do Kubernetes.
type iamAdminMatchDTO struct {
	GroupName string `json:"groupName"`
	Role      string `json:"role"`
}

// findIAMAdminBypass cruza os grupos do analista com os Role Assignments do recurso AKS,
// retornando os que concedem acesso admin via IAM (fora do alcance do impersonation/RBAC).
func findIAMAdminBypass(ctx context.Context, kubeManager *config.KubeConfigManager, clusterCtx string, groups []matchedGroupDTO) ([]iamAdminMatchDTO, error) {
	// Nunca retornar nil aqui — um slice nil vira `null` no JSON, e um `null` engana checks
	// como `campo && campo.length` no frontend (null é falsy, mas `!== undefined` não pega
	// null — já causou crash real quando esse campo vinha vazio).
	matches := []iamAdminMatchDTO{}

	assignments, err := getAKSResourceRoleAssignments(ctx, kubeManager, clusterCtx)
	if err != nil || len(assignments) == 0 {
		return matches, err
	}

	groupNames := make(map[string]bool, len(groups))
	for _, g := range groups {
		groupNames[g.DisplayName] = true
	}

	for _, ra := range assignments {
		if iamAdminRoles[ra.RoleDefinitionName] && groupNames[ra.PrincipalName] {
			matches = append(matches, iamAdminMatchDTO{GroupName: ra.PrincipalName, Role: ra.RoleDefinitionName})
		}
	}
	return matches, nil
}
