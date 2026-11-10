import { RefreshCw } from "lucide-react";
import { useBuildInfo } from "@/hooks/useBuildInfo";
import { buildLabel, describeBuild } from "@/lib/buildInfo";

// Selo discreto no cabeçalho: qual build do servidor está no ar (commit; * = havia mudanças sem
// commit). O tooltip traz commit, hora do binário, início do servidor e os bundles do frontend. Se
// esta página é de um build anterior, vira um botão âmbar para recarregar.
export function BuildBadge() {
  const { build, loadedBundle, stale } = useBuildInfo();
  const label = buildLabel(build);
  if (!label) return null;
  if (stale) {
    return (
      <button
        type="button"
        onClick={() => window.location.reload()}
        className="inline-flex items-center gap-1 rounded border border-amber-500/50 bg-amber-500/10 px-1.5 py-0.5 text-[10px] font-medium text-amber-400 hover:bg-amber-500/20"
        title={`Esta página é de um build anterior ao do servidor: clique para recarregar.\n\n${describeBuild(build, loadedBundle)}`}
      >
        <RefreshCw className="h-3 w-3" />
        página desatualizada
      </button>
    );
  }
  return (
    <span className="select-text rounded bg-muted/40 px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground" title={describeBuild(build, loadedBundle)}>
      {label}
    </span>
  );
}
