import { useEffect, useRef, useState } from "react";
import cytoscape, { Core } from "cytoscape";
import { useQuery } from "@tanstack/react-query";
import { apiClient } from "@/lib/api/client";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Loader2, ZoomIn, ZoomOut, Maximize2, RefreshCw, Network } from "lucide-react";
import type { LatencyTopologyEdge } from "@/lib/api/types";

// Mesma família de cor já usada nos badges AKS/EKS/GKE (cloudProviderBadge em useCloudProvider.ts)
// — azul=Azure, laranja=AWS, verde=GCP, independente de ser nó de cluster K8s ou alvo de região
// de nuvem testado (Fase 6.2), pra manter a linguagem visual consistente com o resto da UI.
const PROVIDER_COLORS: Record<string, string> = {
  aks: "#3b82f6",
  azure: "#3b82f6",
  eks: "#f97316",
  aws: "#f97316",
  gke: "#10b981",
  gcp: "#10b981",
};
const DEFAULT_NODE_COLOR = "#6b7280"; // cinza — provider desconhecido

// Thresholds de latência (ms) pra cor da aresta — sugestão inicial do plano, ajustar depois de
// ver dado real de produção.
const LATENCY_YELLOW_MS = 100;
const LATENCY_RED_MS = 300;

function edgeColor(p95: number): string {
  if (p95 >= LATENCY_RED_MS) return "#ef4444"; // red-500
  if (p95 >= LATENCY_YELLOW_MS) return "#eab308"; // yellow-500
  return "#10b981"; // green-500
}

// Rótulo abreviado pro nó — o `label` que a API devolve é o context completo do cluster (ex:
// "akspriv-abastecimento-hlg-admin") ou o host completo do alvo (ex:
// "supply-api.abastecimento-hlg.svc.cluster.local"), longos demais pra caber legível num nó de
// grafo. `id` continua sendo o valor completo (usado pras arestas baterem); isso é só exibição.
function shortNodeLabel(node: { label: string; kind: string }): string {
  if (node.kind === "cluster") {
    return node.label.replace(/-admin$/, "");
  }
  return node.label.split(".")[0];
}

export default function LatencyTopologyGraph() {
  const containerRef = useRef<HTMLDivElement>(null);
  const cyInstance = useRef<Core | null>(null);
  const [selectedEdge, setSelectedEdge] = useState<LatencyTopologyEdge | null>(null);

  const { data, isLoading, isFetching, refetch } = useQuery({
    queryKey: ["latency-topology"],
    queryFn: () => apiClient.getLatencyTopology(),
  });

  useEffect(() => {
    if (!containerRef.current || !data) return;

    const elements = [
      ...data.nodes.map((n) => ({
        data: { id: n.id, label: shortNodeLabel(n), fullLabel: n.label, kind: n.kind, provider: n.provider },
      })),
      ...data.edges.map((e) => ({
        data: {
          id: e.id,
          source: e.source,
          target: e.target,
          protocol: e.protocol,
          p95: e.p95_ms,
          p99: e.p99_ms,
          errorCount: e.error_count,
          totalRequests: e.total_requests,
          testedAt: e.tested_at,
        },
      })),
    ];

    cyInstance.current?.destroy();
    cyInstance.current = cytoscape({
      container: containerRef.current,
      elements,
      style: [
        {
          selector: "node",
          style: {
            "background-color": (ele) => PROVIDER_COLORS[ele.data("provider")] || DEFAULT_NODE_COLOR,
            label: "data(label)",
            color: "#ffffff",
            "text-valign": "center",
            "text-halign": "center",
            "font-size": "9px",
            "text-wrap": "wrap",
            "text-max-width": "80px",
            width: 90,
            height: 90,
            "border-width": 2,
            "border-color": "#1e293b",
          },
        },
        { selector: 'node[kind = "cloud_target"]', style: { shape: "round-rectangle" } },
        { selector: 'node[kind = "service_target"]', style: { shape: "diamond" } },
        {
          selector: "edge",
          style: {
            width: 3,
            "curve-style": "bezier",
            "target-arrow-shape": "triangle",
            "arrow-scale": 1.2,
            label: (ele) => `${Number(ele.data("p95")).toFixed(0)}ms`,
            "font-size": "9px",
            "text-background-color": "#0f172a",
            "text-background-opacity": 0.7,
            "text-background-padding": "2px",
            color: "#ffffff",
            "line-color": (ele) => edgeColor(Number(ele.data("p95"))),
            "target-arrow-color": (ele) => edgeColor(Number(ele.data("p95"))),
          },
        },
        {
          selector: "edge:selected",
          style: { width: 5, "line-color": "#fbbf24", "target-arrow-color": "#fbbf24" },
        },
      ],
      layout: {
        name: "cose",
        idealEdgeLength: 130,
        nodeRepulsion: 300000,
        fit: true,
        padding: 30,
        randomize: false,
      },
    });

    cyInstance.current.on("tap", "edge", (evt) => {
      const e = evt.target;
      setSelectedEdge({
        id: e.id(),
        source: e.data("source"),
        target: e.data("target"),
        protocol: e.data("protocol"),
        p95_ms: e.data("p95"),
        p99_ms: e.data("p99"),
        error_count: e.data("errorCount"),
        total_requests: e.data("totalRequests"),
        tested_at: e.data("testedAt"),
      });
    });
    cyInstance.current.on("tap", (evt) => {
      if (evt.target === cyInstance.current) setSelectedEdge(null);
    });

    return () => {
      cyInstance.current?.destroy();
      cyInstance.current = null;
    };
  }, [data]);

  const hasGraph = (data?.nodes.length ?? 0) > 0;

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <div className="text-sm text-muted-foreground flex items-center gap-2">
          <Network className="w-4 h-4" />
          {hasGraph
            ? `${data!.nodes.length} nós, ${data!.edges.length} arestas — resultado mais recente de cada par cluster→alvo já testado`
            : "Nenhum teste persistido ainda — execute testes na aba \"Teste\" pra popular o grafo"}
        </div>
        <div className="flex items-center gap-1.5">
          <Button size="sm" variant="outline" onClick={() => refetch()} disabled={isFetching}>
            {isFetching ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <RefreshCw className="w-3.5 h-3.5" />}
          </Button>
          <Button size="sm" variant="outline" onClick={() => cyInstance.current?.zoom(cyInstance.current.zoom() * 1.2)}>
            <ZoomIn className="w-3.5 h-3.5" />
          </Button>
          <Button size="sm" variant="outline" onClick={() => cyInstance.current?.zoom(cyInstance.current.zoom() * 0.8)}>
            <ZoomOut className="w-3.5 h-3.5" />
          </Button>
          <Button size="sm" variant="outline" onClick={() => cyInstance.current?.fit(undefined, 30)}>
            <Maximize2 className="w-3.5 h-3.5" />
          </Button>
        </div>
      </div>

      {isLoading ? (
        <div className="h-[500px] flex items-center justify-center border border-border rounded-md">
          <Loader2 className="w-5 h-5 animate-spin text-muted-foreground" />
        </div>
      ) : (
        <div
          ref={containerRef}
          className="w-full bg-slate-50 dark:bg-slate-900 border border-border rounded-md"
          style={{ height: "500px" }}
        />
      )}

      {selectedEdge && (
        <div className="rounded-md border border-border p-3 text-sm flex flex-col gap-1.5">
          <div className="font-mono text-xs text-muted-foreground">
            {selectedEdge.source} → {selectedEdge.target}
          </div>
          <div className="flex items-center gap-2 flex-wrap">
            <Badge variant="outline" className="uppercase text-xs">{selectedEdge.protocol}</Badge>
            <span className="font-mono text-xs">P95 {selectedEdge.p95_ms.toFixed(1)}ms</span>
            <span className="font-mono text-xs">P99 {selectedEdge.p99_ms.toFixed(1)}ms</span>
            {selectedEdge.error_count > 0 && (
              <Badge variant="outline" className="bg-red-500/10 text-red-500 border-red-500/30">
                {selectedEdge.error_count}/{selectedEdge.total_requests} erro(s)
              </Badge>
            )}
          </div>
          <div className="text-xs text-muted-foreground">
            testado em {new Date(selectedEdge.tested_at).toLocaleString("pt-BR")}
          </div>
        </div>
      )}
    </div>
  );
}
