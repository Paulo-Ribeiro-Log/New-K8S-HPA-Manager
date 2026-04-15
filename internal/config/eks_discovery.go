package config

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// regiões AWS padrão a escanear durante o autodiscover EKS.
// Cobre as regiões mais comuns para workloads brasileiros/globais.
var defaultEKSRegions = []string{
	"us-east-1",
	"us-east-2",
	"us-west-2",
	"sa-east-1",
}

// AutoDiscoverEKSClusters descobre automaticamente todos os clusters EKS acessíveis
// nos perfis AWS configurados localmente, varrendo as regiões padrão em paralelo.
//
// Fluxo:
//  1. aws configure list-profiles → lista perfis disponíveis
//  2. Para cada profile × região (paralelo, semáforo cap. 5):
//     aws eks list-clusters → lista clusters
//     aws eks describe-cluster → extrai região, VPC, nodegroups, accountId
//  3. Retorna []EKSClusterConfig com todos os clusters encontrados
func (k *KubeConfigManager) AutoDiscoverEKSClusters(logFunc func(string)) ([]EKSClusterConfig, []error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if logFunc != nil {
		logFunc("[EKS] 🔍 Listando perfis AWS configurados...")
	}

	profiles, err := listEKSProfiles(ctx)
	if err != nil {
		return nil, []error{fmt.Errorf("falha ao listar perfis AWS: %w", err)}
	}
	if len(profiles) == 0 {
		if logFunc != nil {
			logFunc("[EKS] ⚠️  Nenhum perfil AWS encontrado (aws configure list-profiles)")
		}
		return nil, nil
	}

	if logFunc != nil {
		logFunc(fmt.Sprintf("[EKS] 📋 %d perfil(s) encontrado(s): %s", len(profiles), strings.Join(profiles, ", ")))
	}

	// Disparar descoberta profile × região em paralelo
	type result struct {
		configs []EKSClusterConfig
		err     error
		label   string // "profile/region" para logging
	}

	combinations := len(profiles) * len(defaultEKSRegions)
	resultChan := make(chan result, combinations)
	semaphore := make(chan struct{}, 5)
	var wg sync.WaitGroup

	for _, profile := range profiles {
		for _, region := range defaultEKSRegions {
			wg.Add(1)
			go func(prof, reg string) {
				defer wg.Done()

				select {
				case semaphore <- struct{}{}:
					defer func() { <-semaphore }()
				case <-ctx.Done():
					resultChan <- result{label: prof + "/" + reg, err: ctx.Err()}
					return
				}

				configs, err := discoverEKSClustersInRegion(ctx, prof, reg)
				resultChan <- result{configs: configs, err: err, label: prof + "/" + reg}
			}(profile, region)
		}
	}

	go func() {
		wg.Wait()
		close(resultChan)
	}()

	var allConfigs []EKSClusterConfig
	var allErrors []error
	seen := make(map[string]bool) // evita duplicatas (mesmo cluster em múltiplos perfis)

	for res := range resultChan {
		if res.err != nil {
			// Erros de "sem clusters nesta região" são normais — logar apenas em debug
			if !isNoClusterFoundError(res.err) {
				allErrors = append(allErrors, fmt.Errorf("%s: %w", res.label, res.err))
				if logFunc != nil {
					logFunc(fmt.Sprintf("[EKS] ⚠️  %s: %v", res.label, res.err))
				}
			}
			continue
		}
		for _, cfg := range res.configs {
			if !seen[cfg.Name] {
				seen[cfg.Name] = true
				allConfigs = append(allConfigs, cfg)
				if logFunc != nil {
					logFunc(fmt.Sprintf("[EKS] ✅ %s — região: %s, perfil: %s", cfg.Name, cfg.AwsRegion, cfg.AwsProfile))
				}
			}
		}
	}

	if logFunc != nil {
		logFunc(fmt.Sprintf("[EKS] 📊 Resumo: ✅ %d cluster(s) | ❌ %d erro(s)", len(allConfigs), len(allErrors)))
	}

	return allConfigs, allErrors
}

// listEKSProfiles retorna os perfis AWS CLI configurados localmente.
func listEKSProfiles(ctx context.Context) ([]string, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	out, err := exec.CommandContext(cmdCtx, "aws", "configure", "list-profiles").Output()
	if err != nil {
		return nil, fmt.Errorf("aws configure list-profiles falhou: %w", err)
	}

	var profiles []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		p := strings.TrimSpace(line)
		if p != "" {
			profiles = append(profiles, p)
		}
	}
	return profiles, nil
}

// discoverEKSClustersInRegion lista e descreve todos os clusters EKS de um profile+região.
func discoverEKSClustersInRegion(ctx context.Context, profile, region string) ([]EKSClusterConfig, error) {
	// 1. Listar clusters
	listCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	out, err := exec.CommandContext(listCtx, "aws", "eks", "list-clusters",
		"--profile", profile,
		"--region", region,
		"--output", "json",
	).Output()
	if err != nil {
		if isAccessDeniedError(err, out) {
			return nil, fmt.Errorf("sem permissão (profile=%s, region=%s)", profile, region)
		}
		// Região sem acesso ou sem clusters — não é erro fatal
		return nil, nil
	}

	var listResp struct {
		Clusters []string `json:"clusters"`
	}
	if err := json.Unmarshal(out, &listResp); err != nil {
		return nil, nil
	}
	if len(listResp.Clusters) == 0 {
		return nil, nil
	}

	// 2. Descrever cada cluster em paralelo (cap. 3 dentro da região)
	type descResult struct {
		cfg EKSClusterConfig
		err error
	}
	descChan := make(chan descResult, len(listResp.Clusters))
	descSem := make(chan struct{}, 3)
	var wg sync.WaitGroup

	for _, clusterName := range listResp.Clusters {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			descSem <- struct{}{}
			defer func() { <-descSem }()

			cfg, err := describeEKSCluster(ctx, profile, region, name)
			descChan <- descResult{cfg: cfg, err: err}
		}(clusterName)
	}

	go func() {
		wg.Wait()
		close(descChan)
	}()

	var configs []EKSClusterConfig
	for res := range descChan {
		if res.err != nil {
			log.Debug().Err(res.err).Str("profile", profile).Str("region", region).
				Msg("[EKS] falha ao descrever cluster")
			continue
		}
		configs = append(configs, res.cfg)
	}

	return configs, nil
}

// describeEKSCluster busca os detalhes de um cluster EKS via aws eks describe-cluster.
func describeEKSCluster(ctx context.Context, profile, region, clusterName string) (EKSClusterConfig, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	out, err := exec.CommandContext(cmdCtx, "aws", "eks", "describe-cluster",
		"--name", clusterName,
		"--profile", profile,
		"--region", region,
		"--output", "json",
	).Output()
	if err != nil {
		return EKSClusterConfig{}, fmt.Errorf("describe-cluster %s: %w", clusterName, err)
	}

	var resp struct {
		Cluster struct {
			Name        string `json:"name"`
			Arn         string `json:"arn"`
			ResourcesVpcConfig struct {
				VpcId string `json:"vpcId"`
			} `json:"resourcesVpcConfig"`
		} `json:"cluster"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return EKSClusterConfig{}, fmt.Errorf("parse describe-cluster %s: %w", clusterName, err)
	}

	// Extrair accountId do ARN: arn:aws:eks:REGION:ACCOUNT:cluster/NAME
	accountID := extractAccountIDFromARN(resp.Cluster.Arn)

	return EKSClusterConfig{
		Name:       resp.Cluster.Name,
		AwsRegion:  region,
		AwsProfile: profile,
		AccountID:  accountID,
		VpcID:      resp.Cluster.ResourcesVpcConfig.VpcId,
	}, nil
}

// extractAccountIDFromARN extrai o accountId de um ARN AWS.
// Formato: arn:aws:eks:REGION:ACCOUNT:cluster/NAME → retorna ACCOUNT.
func extractAccountIDFromARN(arn string) string {
	parts := strings.Split(arn, ":")
	if len(parts) >= 5 {
		return parts[4]
	}
	return ""
}

// isNoClusterFoundError retorna true se o erro indica simplesmente ausência de clusters
// (não um problema real de autenticação ou conectividade).
func isNoClusterFoundError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no clusters") || strings.Contains(msg, "not found")
}

// isAccessDeniedError verifica se o output indica erro de permissão AWS.
func isAccessDeniedError(err error, output []byte) bool {
	if err == nil {
		return false
	}
	combined := strings.ToLower(string(output)) + strings.ToLower(err.Error())
	return strings.Contains(combined, "accessdenied") ||
		strings.Contains(combined, "unauthorized") ||
		strings.Contains(combined, "not authorized")
}
