import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "@/lib/api/client";
import type {
  GitHubReposConfig,
  GitHubReleasesResponse,
  GitHubComparison,
  DeploymentSearchResponse,
  ProductionDeploymentResponse,
  AllVersionsResponse,
  TokenStatusResponse,
  SaveTokenResponse,
} from "@/lib/api/types";

/**
 * Hook para obter lista de repositórios GitHub configurados
 */
export const useGitHubRepos = () => {
  return useQuery<GitHubReposConfig>({
    queryKey: ["github-repos"],
    queryFn: () => apiClient.getGitHubRepos(),
    staleTime: 300000, // 5 minutos
  });
};

/**
 * Hook para obter releases de um repositório GitHub
 */
export const useGitHubReleases = (owner: string, repo: string) => {
  return useQuery<GitHubReleasesResponse>({
    queryKey: ["github-releases", owner, repo],
    queryFn: () => apiClient.getGitHubReleases(owner, repo),
    enabled: !!owner && !!repo,
    staleTime: 60000, // 1 minuto
  });
};

/**
 * Hook para comparar duas releases (tags)
 */
export const useGitHubComparison = (
  owner: string,
  repo: string,
  base: string,
  head: string
) => {
  return useQuery<GitHubComparison>({
    queryKey: ["github-comparison", owner, repo, base, head],
    queryFn: () => apiClient.compareGitHubReleases(owner, repo, base, head),
    enabled: !!owner && !!repo && !!base && !!head,
    staleTime: 300000, // 5 minutos (resultado não muda)
  });
};

/**
 * Hook para buscar deployments por app name
 */
export const useDeploymentSearch = (appName: string) => {
  return useQuery<DeploymentSearchResponse>({
    queryKey: ["deployment-search", appName],
    queryFn: () => apiClient.searchDeployments(appName),
    enabled: !!appName && appName.length >= 3, // Mínimo 3 caracteres
    staleTime: 60000, // 1 minuto
  });
};

/**
 * Hook para obter versão em produção de um app
 */
export const useProductionDeployment = (appName: string) => {
  return useQuery<ProductionDeploymentResponse>({
    queryKey: ["production-deployment", appName],
    queryFn: () => apiClient.getProductionDeployment(appName),
    enabled: !!appName,
    staleTime: 30000, // 30 segundos
    retry: 1, // Tentar apenas 1 vez (pode não ter versão em produção)
  });
};

/**
 * Hook para obter todas as versões de um app
 */
export const useAllVersions = (appName: string) => {
  return useQuery<AllVersionsResponse>({
    queryKey: ["all-versions", appName],
    queryFn: () => apiClient.getAllVersions(appName),
    enabled: !!appName,
    staleTime: 60000, // 1 minuto
  });
};

/**
 * Hook para obter status do token GitHub do usuário
 */
export const useGitHubTokenStatus = () => {
  return useQuery<TokenStatusResponse>({
    queryKey: ["github-token-status"],
    queryFn: () => apiClient.getGitHubTokenStatus(),
    staleTime: 300000, // 5 minutos
    retry: 1, // Apenas 1 tentativa (pode não ter token configurado)
  });
};

/**
 * Hook para salvar token GitHub do usuário
 */
export const useSaveGitHubToken = () => {
  const queryClient = useQueryClient();

  return useMutation<SaveTokenResponse, Error, { token: string; email: string }>({
    mutationFn: ({ token, email }) => apiClient.saveGitHubToken(token, email),
    onSuccess: () => {
      // Invalidar cache do status do token
      queryClient.invalidateQueries({ queryKey: ["github-token-status"] });
      // Invalidar cache de releases (agora com token válido pode ter mais permissões)
      queryClient.invalidateQueries({ queryKey: ["github-releases"] });
    },
  });
};

/**
 * Hook para deletar token GitHub do usuário
 */
export const useDeleteGitHubToken = () => {
  const queryClient = useQueryClient();

  return useMutation<{ success: boolean; message: string }, Error>({
    mutationFn: () => apiClient.deleteGitHubToken(),
    onSuccess: () => {
      // Invalidar cache do status do token
      queryClient.invalidateQueries({ queryKey: ["github-token-status"] });
      // Invalidar cache de releases (agora sem token pode ter limitações)
      queryClient.invalidateQueries({ queryKey: ["github-releases"] });
    },
  });
};
