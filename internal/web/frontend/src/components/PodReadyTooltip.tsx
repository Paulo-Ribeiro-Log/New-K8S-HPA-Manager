import type { ReactNode } from "react";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { ContainerStatus, PodSummary } from "@/lib/api/types";

interface PodReadyTooltipProps {
  pod: Pick<PodSummary, "containers" | "readyContainers" | "totalContainers">;
  /** Texto exibido no lugar de "ready/total" (ex.: "2/2 ready"). */
  children?: ReactNode;
  className?: string;
}

function containerState(c: ContainerStatus): string {
  return c.stateReason ? `${c.state} (${c.stateReason})` : c.state;
}

function ContainerRow({ c }: { c: ContainerStatus }) {
  return (
    <div className="flex items-center gap-2 text-xs">
      <span className={`w-2 h-2 rounded-full shrink-0 ${c.ready ? "bg-green-500" : "bg-orange-500"}`} />
      <span className="font-mono">{c.name}</span>
      <span className="text-muted-foreground">{containerState(c)}</span>
      {c.restartCount > 0 && <span className="text-orange-500">{c.restartCount} restarts</span>}
    </div>
  );
}

/**
 * "ready/total" de um pod com tooltip dos containers. O número do K8s conta só os containers normais (incluindo
 * sidecars como istio-proxy): init e efêmeros aparecem no tooltip, mas separados, porque não entram na conta.
 */
export function PodReadyTooltip({ pod, children, className }: PodReadyTooltipProps) {
  const label = children ?? `${pod.readyContainers}/${pod.totalContainers}`;
  const all = pod.containers ?? [];
  const regular = all.filter((c) => c.type === "container");
  const others = all.filter((c) => c.type !== "container");
  if (all.length === 0) return <span className={className}>{label}</span>;

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className={`cursor-help underline decoration-dotted decoration-muted-foreground/50 underline-offset-2 ${className ?? ""}`}>
          {label}
        </span>
      </TooltipTrigger>
      <TooltipContent className="space-y-1 max-w-sm">
        <div className="text-xs font-semibold">
          Containers ({pod.readyContainers}/{pod.totalContainers} prontos)
        </div>
        {regular.map((c) => (
          <ContainerRow key={`c-${c.name}`} c={c} />
        ))}
        {others.length > 0 && (
          <>
            <div className="pt-1 text-[11px] text-muted-foreground">Init / efêmeros (fora da contagem)</div>
            {others.map((c) => (
              <ContainerRow key={`${c.type}-${c.name}`} c={c} />
            ))}
          </>
        )}
      </TooltipContent>
    </Tooltip>
  );
}
