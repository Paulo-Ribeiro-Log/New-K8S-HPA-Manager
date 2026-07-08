package config

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// regiões AWS padrão a escanear no autodiscover EKS
var eksDefaultRegions = []string{
	"us-east-1",
	"us-east-2",
	"us-west-2",
	"sa-east-1",
}

// AutoDiscoverEKSClusters descobre automaticamente todos os clusters EKS acessíveis
// usando AWS CLI: lista profiles → para cada profile×região chama aws eks list-clusters.
//
// Fluxo:
//  1. aws configure list-profiles → lista todos os profiles configurados (filtra ARNs)
//  2. Para cada profile × região (paralelo, semáforo cap. 2 — conservador para WSL2):
//     aws eks list-clusters → lista clusters
//     aws eks describe-cluster → extrai ARN, accountId, VPC
//  3. Retorna []EKSClusterConfig deduplicado por (profile, region, name)
func (k *KubeConfigManager) AutoDiscoverEKSClusters(logFunc func(string)) ([]EKSClusterConfig, []error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if logFunc != nil {
		logFunc("[EKS] 🔍 Listando profiles AWS configurados...")
	}

	// Verificar se aws CLI está instalado
	if _, err := exec.LookPath("aws"); err != nil {
		if logFunc != nil {
			logFunc("[EKS] ⚠️  aws CLI não encontrado — pulando discovery EKS")
		}
		return nil, nil
	}

	profiles, err := listAWSProfiles(ctx)
	if err != nil {
		return nil, []error{fmt.Errorf("erro ao listar profiles AWS: %w", err)}
	}
	if len(profiles) == 0 {
		if logFunc != nil {
			logFunc("[EKS] ⚠️  Nenhum profile AWS encontrado (~/.aws/config)")
		}
		return nil, nil
	}

	if logFunc != nil {
		logFunc(fmt.Sprintf("[EKS] 📋 %d profile(s) encontrado(s): %s", len(profiles), strings.Join(profiles, ", ")))
	}

	// Verificar e renovar credenciais SSO para cada profile antes do discovery paralelo
	for _, profile := range profiles {
		if err := ensureAWSSSOAuth(profile, logFunc); err != nil {
			if logFunc != nil {
				logFunc(fmt.Sprintf("[EKS] ⚠️  Autenticação falhou para profile %s: %v", profile, err))
			}
		}
	}

	type result struct {
		configs []EKSClusterConfig
		errs    []error
	}

	sem := make(chan struct{}, 5) // máx 5 goroutines profile×região simultâneas
	resultCh := make(chan result, len(profiles)*len(eksDefaultRegions))

	// warnedProfiles evita repetir "aws sso login --profile X" para cada região do mesmo profile
	warnedProfiles := make(map[string]bool)
	var warnMu sync.Mutex
	var wg sync.WaitGroup

	for _, profile := range profiles {
		for _, region := range eksDefaultRegions {
			wg.Add(1)
			go func(profile, region string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				cfgs, errs := discoverEKSClustersInRegion(ctx, profile, region, logFunc, func() bool {
					warnMu.Lock()
					defer warnMu.Unlock()
					if warnedProfiles[profile] {
						return false
					}
					warnedProfiles[profile] = true
					return true
				})
				resultCh <- result{configs: cfgs, errs: errs}
			}(profile, region)
		}
	}

	wg.Wait()
	close(resultCh)

	seen := make(map[string]bool)
	var allConfigs []EKSClusterConfig
	var allErrors []error

	for r := range resultCh {
		allErrors = append(allErrors, r.errs...)
		for _, c := range r.configs {
			// Dedup por accountID quando disponível: dois profiles com acesso ao mesmo
			// cluster (mesma conta) produzem o mesmo key e não entram duplicados.
			var key string
			if c.AccountID != "" {
				key = c.AccountID + "|" + c.AwsRegion + "|" + c.Name
			} else {
				key = c.AwsProfile + "|" + c.AwsRegion + "|" + c.Name
			}
			if !seen[key] {
				seen[key] = true
				allConfigs = append(allConfigs, c)
			}
		}
	}

	if logFunc != nil {
		logFunc(fmt.Sprintf("[EKS] 📊 Resumo: ✅ %d cluster(s) encontrado(s)", len(allConfigs)))
	}

	return allConfigs, allErrors
}

// ensureAWSSSOAuth verifica se as credenciais do profile AWS estão válidas.
// Se o profile não for SSO (sem sso_start_url), ignora silenciosamente.
// Se as credenciais estiverem expiradas e for SSO, abre aws sso login interativamente.
func ensureAWSSSOAuth(profile string, logFunc func(string)) error {
	if checkAWSCredentials(profile) {
		return nil
	}
	if !isAWSSSOProfile(profile) {
		// Profile sem SSO (credenciais estáticas, env vars, etc.) — não há como renovar
		return nil
	}
	if logFunc != nil {
		logFunc(fmt.Sprintf("[EKS] 🔑 Credenciais expiradas para profile %q — iniciando aws sso login...", profile))
	}
	loginCmd := exec.CommandContext(context.Background(), "aws", "sso", "login", "--profile", profile)
	loginCmd.Stdin = os.Stdin
	loginCmd.Stdout = os.Stdout
	loginCmd.Stderr = os.Stderr
	if err := loginCmd.Run(); err != nil {
		return fmt.Errorf("aws sso login --profile %s falhou: %w", profile, err)
	}
	if logFunc != nil {
		logFunc(fmt.Sprintf("[EKS] ✅ Autenticação SSO concluída para profile %q", profile))
	}
	return nil
}

// isAWSSSOProfile retorna true se o profile tiver sso_start_url em ~/.aws/config.
func isAWSSSOProfile(profile string) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	data, err := os.ReadFile(filepath.Join(home, ".aws", "config"))
	if err != nil {
		return false
	}
	targetSection := "[profile " + profile + "]"
	if profile == "default" {
		targetSection = "[default]"
	}
	inSection := false
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			inSection = strings.EqualFold(line, targetSection)
			continue
		}
		if inSection && strings.HasPrefix(strings.ToLower(line), "sso_start_url") {
			return true
		}
	}
	return false
}

// checkAWSCredentials testa se as credenciais do profile estão válidas com um
// aws sts get-caller-identity --profile <profile> com timeout curto.
// Retorna true se as credenciais são válidas, false caso contrário.
func checkAWSCredentials(profile string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	err := exec.CommandContext(ctx, "aws", "sts", "get-caller-identity",
		"--profile", profile,
		"--output", "json",
	).Run()
	return err == nil
}

// listAWSProfiles lista todos os profiles configurados via aws configure list-profiles.
// Filtra entradas que parecem ARNs (arn:aws:...) — geradas pelo aws eks update-kubeconfig
// e inválidas como nome de profile.
func listAWSProfiles(ctx context.Context) ([]string, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	out, err := exec.CommandContext(cmdCtx, "aws", "configure", "list-profiles").Output()
	if err != nil {
		return nil, fmt.Errorf("aws configure list-profiles: %w", err)
	}

	var profiles []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		p := strings.TrimSpace(line)
		if p == "" || strings.HasPrefix(p, "arn:") {
			// ARNs (ex: arn:aws:eks:...) não são nomes de profile válidos —
			// são artefatos de `aws eks update-kubeconfig` gravados no ~/.aws/config
			continue
		}
		profiles = append(profiles, p)
	}
	return profiles, nil
}

// discoverEKSClustersInRegion lista e descreve os clusters EKS de um profile+região.
// canWarn é chamado para controlar se a sugestão de auth deve ser emitida (evita repetição por região).
func discoverEKSClustersInRegion(ctx context.Context, profile, region string, logFunc func(string), canWarn func() bool) ([]EKSClusterConfig, []error) {
	listCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	args := []string{"eks", "list-clusters",
		"--region", region,
		"--profile", profile,
		"--output", "json",
	}
	out, err := exec.CommandContext(listCtx, "aws", args...).CombinedOutput()
	if err != nil {
		errMsg := strings.TrimSpace(string(out))
		if logFunc != nil && errMsg != "" && !strings.Contains(errMsg, "NotFoundException") && canWarn() {
			logFunc(fmt.Sprintf("[EKS] ⚠️  profile %s: %s", profile, firstLine(errMsg)))
			if strings.Contains(errMsg, "SSO") || strings.Contains(errMsg, "sso") {
				logFunc(fmt.Sprintf("[EKS] 💡 Execute: aws sso login --profile %s", profile))
			} else if strings.Contains(errMsg, "NoCredentials") || strings.Contains(errMsg, "Unable to locate credentials") {
				logFunc(fmt.Sprintf("[EKS] 💡 Configure credenciais: aws configure --profile %s", profile))
			}
		}
		return nil, nil
	}

	var resp struct {
		Clusters []string `json:"clusters"`
	}
	if err := json.Unmarshal(out, &resp); err != nil || len(resp.Clusters) == 0 {
		return nil, nil
	}

	var configs []EKSClusterConfig
	for _, name := range resp.Clusters {
		cfg, err := describeEKSCluster(ctx, profile, region, name)
		if err != nil {
			if logFunc != nil {
				logFunc(fmt.Sprintf("[EKS] ⚠️  describe-cluster %s (%s/%s): %v", name, profile, region, err))
			}
			// Inclui mesmo sem detalhes
			configs = append(configs, EKSClusterConfig{
				Name:       name,
				AwsRegion:  region,
				AwsProfile: profile,
			})
			continue
		}
		if logFunc != nil {
			logFunc(fmt.Sprintf("[EKS] ✅ %s — região: %s, perfil: %s, account: %s", cfg.Name, cfg.AwsRegion, cfg.AwsProfile, cfg.AccountID))
		}
		configs = append(configs, *cfg)
	}

	return configs, nil
}

// describeEKSCluster obtém detalhes de um cluster EKS (ARN, accountId, VPC).
func describeEKSCluster(ctx context.Context, profile, region, name string) (*EKSClusterConfig, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	args := []string{"eks", "describe-cluster",
		"--name", name,
		"--region", region,
		"--profile", profile,
		"--output", "json",
	}
	out, err := exec.CommandContext(cmdCtx, "aws", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("aws eks describe-cluster: %w", err)
	}

	var resp struct {
		Cluster struct {
			Arn                string `json:"arn"`
			ResourcesVpcConfig struct {
				VpcId string `json:"vpcId"`
			} `json:"resourcesVpcConfig"`
		} `json:"cluster"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("parse describe-cluster: %w", err)
	}

	return &EKSClusterConfig{
		Name:       name,
		AwsRegion:  region,
		AwsProfile: profile,
		AccountID:  extractAccountIDFromARN(resp.Cluster.Arn),
		VpcID:      resp.Cluster.ResourcesVpcConfig.VpcId,
	}, nil
}

// firstLine retorna apenas a primeira linha de uma string multiline.
func firstLine(s string) string {
	if i := strings.Index(s, "\n"); i >= 0 {
		return s[:i]
	}
	return s
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
