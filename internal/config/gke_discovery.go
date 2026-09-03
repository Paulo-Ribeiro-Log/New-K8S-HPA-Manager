package config

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	gcpprovider "k8s-hpa-manager/internal/cloudprovider/gcp"
)

// AutoDiscoverGKEClusters descobre clusters GKE de duas fontes em ordem de prioridade:
//  1. Contexts do kubeconfig com prefixo gke_ — sem credenciais, sempre disponível
//  2. gcloud CLI — lista projetos e clusters ativos (requer gcloud auth login)
func (k *KubeConfigManager) AutoDiscoverGKEClusters(logFunc func(string)) ([]GKEClusterConfig, []error) {
	if logFunc != nil {
		logFunc("[GKE] Descobrindo clusters GKE...")
	}

	seen := make(map[string]bool)
	var configs []GKEClusterConfig

	// Fonte 1: kubeconfig contexts com prefixo gke_ (sem credenciais)
	for _, c := range k.discoverGKEFromKubeconfig() {
		key := c.ProjectID + "|" + c.Region + "|" + c.Name
		if !seen[key] {
			seen[key] = true
			configs = append(configs, c)
			if logFunc != nil {
				logFunc(fmt.Sprintf("[GKE] ✅ %s — projeto: %s, região: %s (kubeconfig)", c.Name, c.ProjectID, c.Region))
			}
		}
	}
	// Achado via kubeconfig antes da Fonte 2 mexer em `configs` — usado abaixo para decidir se
	// vale a pena pedir `gcloud auth login` interativo (só faz sentido oferecer login quando já
	// sabemos, pelo próprio kubeconfig, que existem clusters GKE a alcançar).
	foundViaKubeconfig := len(configs) > 0
	if logFunc != nil {
		logFunc(fmt.Sprintf("[GKE] Fonte 1 (kubeconfig): %d cluster(s) com contexto gke_* encontrado(s)", len(configs)))
	}

	// Fonte 2: gcloud CLI (requer autenticação)
	if _, err := exec.LookPath("gcloud"); err != nil {
		if logFunc != nil {
			logFunc("[GKE] ⚠️  gcloud CLI não encontrado — usando apenas kubeconfig")
		}
		return configs, nil
	}

	// Garantir que o plugin de autenticação GKE está instalado — best-effort, nunca bloqueia a
	// listagem de projetos/clusters abaixo (só afeta uma conexão de fato contra o cluster
	// depois, via kubectl/client-go — fora do escopo deste discovery). Roda antes só porque é
	// rápido e não depende de nada abaixo, não porque a ordem importe para o login.
	if err := gcpprovider.EnsureGKEAuthPlugin(logFunc); err != nil && logFunc != nil {
		logFunc(fmt.Sprintf("[GKE] ⚠️  %v", err))
	}

	if logFunc != nil {
		logFunc("[GKE] Listando projetos via gcloud...")
	}

	projects, err := listGCPProjectsGcloud()
	if err != nil && foundViaKubeconfig {
		// A 1ª versão desta correção decidia ANTES de listar, via `gcloud auth list`, se valia
		// pedir login — mas esse comando só reflete um estado LOCAL ("qual conta está marcada
		// como ativa" no arquivo de credenciais do gcloud), não se a sessão de fato ainda é
		// válida: uma conta marcada como ativa com refresh token expirado/revogado passa batido
		// por `gcloud auth list` sem erro nenhum, e só a tentativa REAL de listar (`gcloud
		// projects list`, acima) revela isso — foi exatamente esse falso-negativo que fez o
		// login nunca ser disparado mesmo depois da 1ª correção. Corrigido reagindo à falha real
		// da operação que precisamos (mesmo espírito de `checkAWSCredentials` no EKS, que já
		// faz uma chamada de rede de verdade, `aws sts get-caller-identity`, em vez de confiar
		// num estado local) — se listar falhar E já sabemos (Fonte 1) que há clusters GKE a
		// alcançar, abre `gcloud auth login` e tenta listar de novo uma única vez.
		if logFunc != nil {
			logFunc(fmt.Sprintf("[GKE] ℹ️  gcloud projects list falhou (%v) — tentando gcloud auth login...", err))
		}
		if loginErr := ensureGCPAuth(logFunc); loginErr != nil {
			if logFunc != nil {
				logFunc(fmt.Sprintf("[GKE] ⚠️  %v", loginErr))
			}
		} else {
			projects, err = listGCPProjectsGcloud()
		}
	}
	if err != nil {
		if logFunc != nil {
			if len(configs) == 0 {
				logFunc("[GKE] ⚠️  gcloud não autenticado — execute: gcloud auth login")
			} else {
				logFunc("[GKE] ℹ️  gcloud sem autenticação — usando apenas kubeconfig")
			}
		}
		return configs, nil
	}

	if logFunc != nil {
		logFunc(fmt.Sprintf("[GKE] %d projeto(s) encontrado(s)", len(projects)))
	}

	var allErrors []error
	for _, project := range projects {
		clusters, err := listGKEClustersGcloud(project)
		if err != nil {
			allErrors = append(allErrors, fmt.Errorf("projeto %s: %w", project, err))
			continue
		}
		for _, c := range clusters {
			key := c.ProjectID + "|" + c.Region + "|" + c.Name
			if !seen[key] {
				seen[key] = true
				configs = append(configs, c)
				if logFunc != nil {
					logFunc(fmt.Sprintf("[GKE] ✅ %s — projeto: %s, região: %s (gcloud)", c.Name, c.ProjectID, c.Region))
				}
			}
		}
	}

	return configs, allErrors
}

// ensureGCPAuth abre `gcloud auth login` interativamente (stdin/stdout ligados ao terminal do
// chamador) — mesmo padrão/motivo de `ensureAWSSSOAuth` (EKS): sem essa credencial, o app até
// acha o cluster (via kubeconfig, Fonte 1) mas não consegue de fato ler workloads nele depois —
// `GetRestConfig` depende de `GetFreshGKEToken`, que por sua vez cai no fallback `gcloud auth
// print-access-token` quando não há ADC salvo (ver seção "GKE — Autenticação e Leitura de
// Workloads" no CLAUDE.md). Diferente do AWS (múltiplos profiles possíveis, cada um testado
// individualmente), GCP aqui é tratado como uma única sessão ativa — não itera "profiles"
// (`gcloud config configurations` nomeadas são um caso avançado, fora de escopo).
//
// Chamada só depois que o CHAMADOR já tentou a operação real (`gcloud projects list`) e ela
// falhou — nunca decide por conta própria via `gcloud auth list` (só reflete um estado LOCAL,
// "qual conta está marcada como ativa", sem validar se a sessão de fato ainda funciona — ver
// comentário em `AutoDiscoverGKEClusters`).
func ensureGCPAuth(logFunc func(string)) error {
	if logFunc != nil {
		logFunc("[GKE] 🔑 iniciando gcloud auth login...")
	}
	interactiveCloudLoginMu.Lock()
	defer interactiveCloudLoginMu.Unlock()
	loginCmd := exec.CommandContext(context.Background(), "gcloud", "auth", "login")
	loginCmd.Stdin = os.Stdin
	loginCmd.Stdout = os.Stdout
	loginCmd.Stderr = os.Stderr
	if err := loginCmd.Run(); err != nil {
		return fmt.Errorf("gcloud auth login falhou: %w", err)
	}
	if logFunc != nil {
		logFunc("[GKE] ✅ Autenticação gcloud concluída")
	}
	return nil
}

// discoverGKEFromKubeconfig extrai clusters GKE dos contexts do kubeconfig (sem credenciais).
func (k *KubeConfigManager) discoverGKEFromKubeconfig() []GKEClusterConfig {
	if k.config == nil {
		return nil
	}
	var configs []GKEClusterConfig
	for contextName := range k.config.Contexts {
		project, region, cluster := splitGKEContext(contextName)
		if cluster == "" {
			continue
		}
		configs = append(configs, GKEClusterConfig{
			Name:      cluster,
			ProjectID: project,
			Region:    region,
		})
	}
	return configs
}

// listGCPProjectsGcloud lista projetos GCP ativos via gcloud CLI.
func listGCPProjectsGcloud() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "gcloud", "projects", "list",
		"--filter=lifecycleState:ACTIVE", "--format=json(projectId)")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gcloud projects list: %w", err)
	}

	var result []struct {
		ProjectID string `json:"projectId"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("parse projetos: %w", err)
	}

	ids := make([]string, 0, len(result))
	for _, p := range result {
		if p.ProjectID != "" {
			ids = append(ids, p.ProjectID)
		}
	}
	return ids, nil
}

// listGKEClustersGcloud lista clusters GKE de um projeto via gcloud CLI.
func listGKEClustersGcloud(projectID string) ([]GKEClusterConfig, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "gcloud", "container", "clusters", "list",
		"--project", projectID,
		"--format=json(name,location,network)")
	out, err := cmd.Output()
	if err != nil {
		errMsg := strings.TrimSpace(string(out))
		// API não habilitada ou sem permissão — ignorar silenciosamente
		if strings.Contains(errMsg, "API has not been used") ||
			strings.Contains(errMsg, "not enabled") ||
			strings.Contains(errMsg, "PERMISSION_DENIED") {
			return nil, nil
		}
		return nil, fmt.Errorf("gcloud container clusters list: %w", err)
	}

	var result []struct {
		Name     string `json:"name"`
		Location string `json:"location"`
		Network  string `json:"network"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("parse clusters (projeto %s): %w", projectID, err)
	}

	configs := make([]GKEClusterConfig, 0, len(result))
	for _, c := range result {
		configs = append(configs, GKEClusterConfig{
			Name:      c.Name,
			ProjectID: projectID,
			Region:    c.Location,
			Network:   c.Network,
		})
	}
	return configs, nil
}
