/**
 * DynatraceGitHubSection — correlaciona problems Dynatrace com GitHub Releases.
 * Usa a base de dados da aba GitHub Releases para mostrar:
 *  - Versão deployada (via DTLabels/AppVersion ou registry)
 *  - Repositório configurado
 *  - Releases após a versão deployada ("candidatos à causa raiz")
 *  - Botão de comparação direto
 *
 * Fallback em 3 níveis para quando OneAgent não injeta AppName/AppVersion:
 *  1. k8sWorkloads[].AppName (OneAgent DTLabels) — mais preciso
 *  2. k8sWorkloads[].Workload sem AppName — busca no registry por nome do deployment
 *  3. affectedEntities[].k8sWorkload — entidades impactadas com info K8s
 */

import { useQuery } from "@tanstack/react-query";
import { apiClient } from "@/lib/api/client";
import { GitHubReposConfig, GitHubRelease, GitHubRepoInfo, DeploymentConfig } from "@/lib/api/types";
import { DynatraceProblem } from "@/types/healthcheck";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  GitBranch,
  Tag,
  ArrowRight,
  AlertTriangle,
  CheckCircle2,
  ExternalLink,
  Package,
  Clock,
  ChevronDown,
  ChevronRight,
  Info,
} from "lucide-react";
import { useState } from "react";

// ─── Tipos internos ────────────────────────────────────────────────────────────

type WorkloadSource = "dt-label" | "dt-workload" | "dt-entity";

interface WorkloadEntry {
  appName: string;
  version: string;
  source: WorkloadSource;
  namespace?: string;
  cluster?: string;
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

/** Encontra o DeploymentConfig completo no registry por app_name ou deployment name */
function findDeploymentForApp(
  config: GitHubReposConfig | undefined,
  appName: string,
): DeploymentConfig | null {
  if (!config?.clusters) return null;
  const normalized = appName.toLowerCase();
  for (const cluster of config.clusters) {
    for (const ns of cluster.namespaces ?? []) {
      for (const dep of ns.deployments ?? []) {
        if (
          dep.app_name?.toLowerCase() === normalized ||
          dep.name?.toLowerCase() === normalized
        ) {
          return dep;
        }
      }
    }
  }
  return null;
}

/** Extrai GitHubRepoInfo do DeploymentConfig */
function repoFromDeployment(dep: DeploymentConfig | null): GitHubRepoInfo | null {
  if (!dep?.github_repo?.owner || !dep?.github_repo?.repo) return null;
  return dep.github_repo;
}

/** Normaliza versão para comparação: "147-206-7-1" → "147.206.7.1" / "v147.206.7.1" */
function normalizeVersion(v: string): string {
  return v.replace(/^v/, "").replace(/-/g, ".");
}

/** Verifica se tag é diferente da versão deployada */
function isNewerRelease(release: GitHubRelease, deployedTag: string): boolean {
  return normalizeVersion(release.tag_name) !== normalizeVersion(deployedTag);
}

function fmtDate(iso: string) {
  return new Date(iso).toLocaleDateString("pt-BR", { day: "2-digit", month: "short", year: "numeric" });
}

const SOURCE_LABELS: Record<WorkloadSource, string> = {
  "dt-label":    "OneAgent",
  "dt-workload": "K8s Workload",
  "dt-entity":   "Entidade DT",
};

// ─── Sub-componente por workload ──────────────────────────────────────────────

function WorkloadReleaseCard({
  workload,
  reposConfig,
  problemStart,
}: {
  workload: WorkloadEntry;
  reposConfig: GitHubReposConfig | undefined;
  problemStart: string;
}) {
  const [showAll, setShowAll] = useState(false);
  const [showCompare, setShowCompare] = useState(false);

  // Busca no registry: combina repo + versão do registro quando DT não tem
  const deployment = findDeploymentForApp(reposConfig, workload.appName);
  const repo = repoFromDeployment(deployment);

  // Versão: prioriza o que DT sabe, senão usa o que o registry tem
  const deployedVersion = workload.version || deployment?.version || "";

  const { data: releasesData, isLoading } = useQuery({
    queryKey: ["github-releases", repo?.owner, repo?.repo],
    queryFn: () => apiClient.getGitHubReleases(repo!.owner, repo!.repo),
    enabled: !!repo,
    staleTime: 5 * 60_000,
  });

  const releases = releasesData?.releases ?? [];

  const deployedIdx = releases.findIndex(
    r => normalizeVersion(r.tag_name) === normalizeVersion(deployedVersion),
  );

  const newerReleases = deployedIdx >= 0
    ? releases.slice(0, deployedIdx)
    : releases.filter(r =>
        new Date(r.published_at) > new Date(problemStart) && isNewerRelease(r, deployedVersion),
      );

  const latestRelease = releases[0];
  const isOutdated = deployedIdx > 0 || (deployedIdx === -1 && newerReleases.length > 0);
  const displayReleases = showAll
    ? (deployedIdx >= 0 ? releases.slice(0, deployedIdx + 3) : releases.slice(0, 8))
    : newerReleases.slice(0, 5);

  const versionFromRegistry = !workload.version && !!deployment?.version;

  return (
    <div className="border border-white/10 rounded-lg overflow-hidden">
      {/* Header do workload */}
      <div className="flex items-center justify-between px-3 py-2 bg-white/5 border-b border-white/10">
        <div className="flex items-center gap-2 min-w-0">
          <Package className="h-3.5 w-3.5 text-blue-400 shrink-0" />
          <span className="text-xs font-semibold text-white truncate">{workload.appName}</span>
          {deployedVersion ? (
            <Badge variant="outline" className="text-[10px] px-1.5 py-0 h-4 border-gray-600 text-gray-300 font-mono shrink-0">
              v{deployedVersion}
              {versionFromRegistry && (
                <span className="ml-1 text-gray-500" title="Versão obtida do registry (scan)">*</span>
              )}
            </Badge>
          ) : null}
          <Badge variant="outline" className="text-[9px] px-1 py-0 h-4 border-white/10 text-gray-600 shrink-0">
            {SOURCE_LABELS[workload.source]}
          </Badge>
        </div>
        {isOutdated ? (
          <Badge className="text-[10px] bg-orange-500/20 text-orange-400 border border-orange-500/30 shrink-0">
            <AlertTriangle className="h-2.5 w-2.5 mr-1" />
            {newerReleases.length} versão(ões) mais nova(s)
          </Badge>
        ) : deployedIdx === 0 ? (
          <Badge className="text-[10px] bg-green-500/20 text-green-400 border border-green-500/30 shrink-0">
            <CheckCircle2 className="h-2.5 w-2.5 mr-1" />
            Versão atual
          </Badge>
        ) : null}
      </div>

      <div className="p-3 space-y-2.5">
        {/* Repositório */}
        {repo ? (
          <div className="flex items-center gap-1.5 text-xs text-gray-400">
            <GitBranch className="h-3 w-3 shrink-0" />
            <span className="font-mono text-gray-300">{repo.owner}/{repo.repo}</span>
          </div>
        ) : (
          <div className="text-xs text-yellow-500/80 flex items-center gap-1.5 flex-wrap">
            <AlertTriangle className="h-3 w-3 shrink-0" />
            Repositório não configurado — execute o scan na aba
            <span className="font-semibold text-yellow-400">GitHub Releases</span>
          </div>
        )}

        {/* Aviso versão do registry */}
        {versionFromRegistry && (
          <div className="flex items-center gap-1.5 text-[10px] text-gray-500">
            <Info className="h-3 w-3 shrink-0" />
            Versão obtida do registry local (scan K8s) — OneAgent não reportou AppVersion
          </div>
        )}

        {/* Loading */}
        {isLoading && (
          <div className="flex items-center gap-2 text-xs text-gray-500 py-1">
            <div className="w-3 h-3 border border-current border-t-transparent rounded-full animate-spin" />
            Carregando releases...
          </div>
        )}

        {/* Lista de releases */}
        {!isLoading && repo && releases.length > 0 && (
          <div className="space-y-1">
            <p className="text-[10px] text-gray-500 uppercase tracking-wider font-semibold">
              {newerReleases.length > 0
                ? `${newerReleases.length} release(s) após a versão deployada`
                : "Últimas releases"}
            </p>
            <div className="space-y-0.5">
              {displayReleases.map((r, i) => {
                const isDeployed = normalizeVersion(r.tag_name) === normalizeVersion(deployedVersion);
                const isNewer = i < newerReleases.length ||
                  (deployedIdx === -1 && new Date(r.published_at) > new Date(problemStart));
                return (
                  <div
                    key={r.tag_name}
                    className={`flex items-center gap-2 px-2 py-1.5 rounded text-xs ${
                      isDeployed
                        ? "bg-green-500/10 border border-green-500/20"
                        : isNewer
                        ? "bg-orange-500/5 border border-orange-500/10"
                        : "bg-white/[0.02]"
                    }`}
                  >
                    <Tag className={`h-3 w-3 shrink-0 ${isDeployed ? "text-green-400" : isNewer ? "text-orange-400" : "text-gray-600"}`} />
                    <span className={`font-mono font-medium ${isDeployed ? "text-green-300" : isNewer ? "text-orange-300" : "text-gray-400"}`}>
                      {r.tag_name}
                    </span>
                    {isDeployed && (
                      <Badge className="text-[9px] px-1 py-0 h-3.5 bg-green-500/20 text-green-400 border-green-500/30">
                        deployada
                      </Badge>
                    )}
                    {r.prerelease && (
                      <Badge className="text-[9px] px-1 py-0 h-3.5 bg-gray-500/20 text-gray-400 border-gray-500/30">
                        pre-release
                      </Badge>
                    )}
                    <span className="ml-auto text-gray-500 flex items-center gap-1 shrink-0">
                      <Clock className="h-2.5 w-2.5" />
                      {fmtDate(r.published_at)}
                    </span>
                  </div>
                );
              })}
            </div>

            {(releases.length > displayReleases.length || (deployedIdx >= 0 && deployedIdx > 5)) && (
              <button
                onClick={() => setShowAll(!showAll)}
                className="flex items-center gap-1 text-[10px] text-gray-500 hover:text-gray-300 transition-colors mt-1 px-1"
              >
                {showAll ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
                {showAll ? "Mostrar menos" : "Ver todas as releases"}
              </button>
            )}
          </div>
        )}

        {/* Sem releases */}
        {!isLoading && repo && releases.length === 0 && (
          <p className="text-xs text-gray-500">Nenhuma release encontrada para este repositório</p>
        )}

        {/* Botões de ação */}
        {repo && latestRelease && (
          <div className="flex items-center gap-2 pt-1 border-t border-white/5 flex-wrap">
            {isOutdated && newerReleases.length > 0 && (
              <Button
                variant="outline"
                size="sm"
                className="h-7 text-xs gap-1.5 border-orange-500/30 text-orange-400 hover:bg-orange-500/10"
                onClick={() => setShowCompare(!showCompare)}
              >
                <ArrowRight className="h-3 w-3" />
                Comparar {deployedVersion || "?"} → {latestRelease.tag_name}
              </Button>
            )}
            <a
              href={`https://github.com/${repo.owner}/${repo.repo}/releases`}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1 text-[10px] text-gray-500 hover:text-gray-300 transition-colors"
            >
              <ExternalLink className="h-3 w-3" />
              Ver no GitHub
            </a>
          </div>
        )}

        {/* Comparação inline: release notes da versão mais nova */}
        {showCompare && newerReleases[0] && (
          <div className="mt-2 border border-orange-500/20 rounded-lg overflow-hidden">
            <div className="px-3 py-1.5 bg-orange-500/10 border-b border-orange-500/10 text-[10px] font-semibold text-orange-400 flex items-center gap-1.5">
              <GitBranch className="h-3 w-3" />
              Release notes — {newerReleases[0].tag_name}
            </div>
            <div className="p-3 text-[11px] text-gray-300 max-h-48 overflow-y-auto whitespace-pre-wrap font-mono leading-relaxed">
              {newerReleases[0].body
                ? newerReleases[0].body.slice(0, 800) + (newerReleases[0].body.length > 800 ? "\n..." : "")
                : "Sem release notes."}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

// ─── Coleta de workloads com fallback em 3 níveis ─────────────────────────────

function collectWorkloads(problem: DynatraceProblem): WorkloadEntry[] {
  const seen = new Set<string>();
  const result: WorkloadEntry[] = [];

  function add(entry: WorkloadEntry) {
    const key = entry.appName.toLowerCase();
    if (!seen.has(key)) {
      seen.add(key);
      result.push(entry);
    }
  }

  // Nível 1: k8sWorkloads com AppName (OneAgent DTLabels) — mais preciso
  for (const w of problem.k8sWorkloads ?? []) {
    if (w.AppName) {
      add({
        appName: w.AppName,
        version: w.AppVersion ?? "",
        source: "dt-label",
        namespace: w.Namespace,
        cluster: w.Cluster,
      });
    }
  }

  // Nível 2: k8sWorkloads com Workload mas sem AppName
  for (const w of problem.k8sWorkloads ?? []) {
    if (!w.AppName && w.Workload) {
      add({
        appName: w.Workload,
        version: w.AppVersion ?? "",
        source: "dt-workload",
        namespace: w.Namespace,
        cluster: w.Cluster,
      });
    }
  }

  // Nível 3: affectedEntities e impactedEntities com k8sWorkload
  const allEntities = [
    ...(problem.affectedEntities ?? []),
    ...(problem.impactedEntities ?? []),
  ];
  for (const e of allEntities) {
    if (e.k8sWorkload) {
      add({
        appName: e.k8sWorkload,
        version: e.labels?.appVersion ?? "",
        source: "dt-entity",
        namespace: e.k8sNamespace,
        cluster: e.k8sCluster,
      });
    }
  }

  return result;
}

// ─── Componente principal ─────────────────────────────────────────────────────

interface DynatraceGitHubSectionProps {
  problem: DynatraceProblem;
}

export function DynatraceGitHubSection({ problem }: DynatraceGitHubSectionProps) {
  const { data: reposConfig, isLoading: reposLoading } = useQuery<GitHubReposConfig>({
    queryKey: ["github-repos"],
    queryFn: () => apiClient.getGitHubRepos(),
    staleTime: 5 * 60_000,
  });

  const workloads = collectWorkloads(problem);

  // Após carregar o registry, filtra apenas workloads que têm repositório configurado
  // (ou mostra todos se ainda carregando)
  const workloadsWithRepo = reposConfig
    ? workloads.filter(w => !!repoFromDeployment(findDeploymentForApp(reposConfig, w.appName)))
    : workloads;

  const workloadsWithoutRepo = reposConfig
    ? workloads.filter(w => !repoFromDeployment(findDeploymentForApp(reposConfig, w.appName)))
    : [];

  if (workloads.length === 0) {
    return (
      <div className="flex flex-col items-center gap-2 text-gray-500 text-xs py-8 text-center">
        <GitBranch className="h-5 w-5 text-gray-600" />
        <p>Nenhum workload K8s identificado neste problem.</p>
        <p className="text-gray-600 text-[10px] max-w-xs">
          Configure as tags OneAgent (AppName/AppVersion) ou verifique se o problem tem entidades K8s associadas.
        </p>
      </div>
    );
  }

  return (
    <div className="space-y-3">
      <div className="text-[10px] text-gray-500 px-1 flex items-center gap-1.5">
        <GitBranch className="h-3 w-3" />
        Correlaciona versões deployadas com releases do GitHub para identificar mudanças recentes
        {workloads.length > 0 && (
          <span className="ml-auto text-gray-600">
            {workloads.length} workload(s) identificado(s)
          </span>
        )}
      </div>

      {reposLoading ? (
        <div className="flex items-center gap-2 text-xs text-gray-500 py-2">
          <div className="w-3 h-3 border border-current border-t-transparent rounded-full animate-spin" />
          Carregando registry de repositórios...
        </div>
      ) : (
        <>
          {/* Workloads com repo configurado */}
          {workloadsWithRepo.map(w => (
            <WorkloadReleaseCard
              key={w.appName}
              workload={w}
              reposConfig={reposConfig}
              problemStart={problem.startTime}
            />
          ))}

          {/* Workloads sem repo — lista compacta com sugestão */}
          {workloadsWithoutRepo.length > 0 && (
            <div className="border border-white/5 rounded-lg p-3 space-y-1.5">
              <p className="text-[10px] text-gray-500 flex items-center gap-1.5">
                <AlertTriangle className="h-3 w-3 text-yellow-600" />
                {workloadsWithoutRepo.length} workload(s) sem repositório no registry
              </p>
              <div className="space-y-1">
                {workloadsWithoutRepo.map(w => (
                  <div key={w.appName} className="flex items-center gap-2 text-xs text-gray-500 px-1">
                    <Package className="h-3 w-3 shrink-0 text-gray-700" />
                    <span className="font-mono text-gray-400">{w.appName}</span>
                    {w.version && (
                      <span className="text-gray-600 font-mono">v{w.version}</span>
                    )}
                    <Badge variant="outline" className="text-[9px] px-1 py-0 h-3.5 border-white/10 text-gray-600 ml-auto shrink-0">
                      {SOURCE_LABELS[w.source]}
                    </Badge>
                  </div>
                ))}
              </div>
              <p className="text-[10px] text-gray-600 pt-1">
                Execute o scan na aba <span className="text-gray-400 font-semibold">GitHub Releases</span> para mapear os repositórios.
              </p>
            </div>
          )}
        </>
      )}
    </div>
  );
}
