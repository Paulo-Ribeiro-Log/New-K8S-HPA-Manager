/**
 * DynatraceGitHubSection — correlaciona problems Dynatrace com GitHub Releases.
 * Usa a base de dados da aba GitHub Releases para mostrar:
 *  - Versão deployada (via DTLabels/AppVersion)
 *  - Repositório configurado
 *  - Releases após a versão deployada ("candidatos à causa raiz")
 *  - Botão de comparação direto
 */

import { useQuery } from "@tanstack/react-query";
import { apiClient } from "@/lib/api/client";
import { GitHubReposConfig, GitHubRelease, GitHubRepoInfo } from "@/lib/api/types";
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
} from "lucide-react";
import { useState } from "react";

// ─── Helpers ──────────────────────────────────────────────────────────────────

/** Encontra owner/repo no github-repos.yaml por app_name */
function findRepoForApp(config: GitHubReposConfig | undefined, appName: string): GitHubRepoInfo | null {
  if (!config?.clusters) return null;
  const normalized = appName.toLowerCase();
  for (const cluster of config.clusters) {
    for (const ns of cluster.namespaces ?? []) {
      for (const dep of ns.deployments ?? []) {
        if (
          dep.app_name?.toLowerCase() === normalized ||
          dep.name?.toLowerCase() === normalized
        ) {
          return dep.github_repo;
        }
      }
    }
  }
  return null;
}

/** Normaliza versão para comparação: "147-206-7-1" → "147.206.7.1" / "v147.206.7.1" */
function normalizeVersion(v: string): string {
  return v.replace(/^v/, "").replace(/-/g, ".");
}

/** Verifica se tag é "mais nova" que a versão deployada por data */
function isNewerRelease(release: GitHubRelease, deployedTag: string): boolean {
  const normDeployed = normalizeVersion(deployedTag);
  const normRelease = normalizeVersion(release.tag_name);
  return normRelease !== normDeployed;
}

function fmtDate(iso: string) {
  return new Date(iso).toLocaleDateString("pt-BR", { day: "2-digit", month: "short", year: "numeric" });
}

// ─── Sub-componente por workload ──────────────────────────────────────────────

function WorkloadReleaseCard({
  appName,
  deployedVersion,
  reposConfig,
  problemStart,
}: {
  appName: string;
  deployedVersion: string;
  reposConfig: GitHubReposConfig | undefined;
  problemStart: string;
}) {
  const [showAll, setShowAll] = useState(false);
  const [showCompare, setShowCompare] = useState(false);

  const repo = findRepoForApp(reposConfig, appName);

  const { data: releasesData, isLoading } = useQuery({
    queryKey: ["github-releases", repo?.owner, repo?.repo],
    queryFn: () => apiClient.getGitHubReleases(repo!.owner, repo!.repo),
    enabled: !!repo,
    staleTime: 5 * 60_000,
  });

  const releases = releasesData?.releases ?? [];

  // Encontrar índice da versão deployada na lista (por tag normalizada)
  const deployedIdx = releases.findIndex(
    r => normalizeVersion(r.tag_name) === normalizeVersion(deployedVersion),
  );

  // Releases mais novas = publicadas antes na lista (lista vem em ordem decrescente por data)
  const newerReleases = deployedIdx >= 0
    ? releases.slice(0, deployedIdx)           // releases antes do deployado = mais novas
    : releases.filter(r => {                    // fallback: publicadas após o início do problem
        return new Date(r.published_at) > new Date(problemStart) && isNewerRelease(r, deployedVersion);
      });

  const latestRelease = releases[0];
  const isOutdated = deployedIdx > 0 || (deployedIdx === -1 && newerReleases.length > 0);
  const displayReleases = showAll ? (deployedIdx >= 0 ? releases.slice(0, deployedIdx + 3) : releases.slice(0, 8)) : newerReleases.slice(0, 5);

  return (
    <div className="border border-white/10 rounded-lg overflow-hidden">
      {/* Header do workload */}
      <div className="flex items-center justify-between px-3 py-2 bg-white/5 border-b border-white/10">
        <div className="flex items-center gap-2 min-w-0">
          <Package className="h-3.5 w-3.5 text-blue-400 shrink-0" />
          <span className="text-xs font-semibold text-white truncate">{appName}</span>
          {deployedVersion && (
            <Badge variant="outline" className="text-[10px] px-1.5 py-0 h-4 border-gray-600 text-gray-300 font-mono shrink-0">
              v{deployedVersion}
            </Badge>
          )}
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
          <div className="text-xs text-yellow-500/80 flex items-center gap-1.5">
            <AlertTriangle className="h-3 w-3 shrink-0" />
            Repositório não configurado em <code className="bg-gray-800 px-1 rounded">github-repos.yaml</code>
            <span className="text-gray-500">— escaneie o cluster na aba GitHub Releases</span>
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
                const isNewer = i < newerReleases.length || (deployedIdx === -1 && new Date(r.published_at) > new Date(problemStart));
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

            {/* Ver mais */}
            {(releases.length > displayReleases.length || (deployedIdx >= 0 && deployedIdx > 5)) && (
              <button
                onClick={() => setShowAll(!showAll)}
                className="flex items-center gap-1 text-[10px] text-gray-500 hover:text-gray-300 transition-colors mt-1 px-1"
              >
                {showAll ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
                {showAll ? "Mostrar menos" : `Ver todas as releases`}
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

  // Coletar workloads únicos com AppName e AppVersion
  const workloads = Object.values(
    (problem.k8sWorkloads ?? [])
      .filter(w => w.AppName)
      .reduce((acc, w) => {
        if (!acc[w.AppName!]) {
          acc[w.AppName!] = { appName: w.AppName!, version: w.AppVersion ?? "" };
        }
        return acc;
      }, {} as Record<string, { appName: string; version: string }>),
  );

  if (workloads.length === 0) {
    return (
      <div className="flex items-center gap-2 text-gray-500 text-xs py-6 justify-center">
        <GitBranch className="h-4 w-4" />
        Nenhum workload K8s com AppName identificado neste problem.
        <span className="text-gray-600">Configure as tags OneAgent para habilitar a correlação.</span>
      </div>
    );
  }

  return (
    <div className="space-y-3">
      <div className="text-[10px] text-gray-500 px-1 flex items-center gap-1.5">
        <GitBranch className="h-3 w-3" />
        Correlaciona versões deployadas (via DTLabels) com releases do GitHub para identificar mudanças recentes
      </div>

      {reposLoading ? (
        <div className="flex items-center gap-2 text-xs text-gray-500 py-2">
          <div className="w-3 h-3 border border-current border-t-transparent rounded-full animate-spin" />
          Carregando configuração de repositórios...
        </div>
      ) : (
        workloads.map(w => (
          <WorkloadReleaseCard
            key={w.appName}
            appName={w.appName}
            deployedVersion={w.version}
            reposConfig={reposConfig}
            problemStart={problem.startTime}
          />
        ))
      )}
    </div>
  );
}
