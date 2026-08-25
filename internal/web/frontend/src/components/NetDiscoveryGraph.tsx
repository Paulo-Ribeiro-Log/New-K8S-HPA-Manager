import { useEffect, useRef } from "react";
import cytoscape, { Core } from "cytoscape";
import { ZoomIn, ZoomOut, Maximize2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import type { NetDiscoveryHop } from "@/lib/api/types";

// Especialização LINEAR de LatencyTopologyGraph.tsx (cadeia Origem → Hop 1 → ... → Destino, não
// um grafo geral) — pedido explícito do usuário: "desenhar na tela um fluxo da rota... ficaria
// lindo o acompanhar do processo". Diferente do grafo de latência (que recebe `data` pronto de
// uma query e desenha tudo de uma vez), este componente é ALIMENTADO INCREMENTALMENTE — a
// instância do Cytoscape nasce uma única vez e cada salto novo em `hops` vira um `cy.add()` (não
// um rebuild completo), preservando zoom/pan entre saltos e permitindo uma pequena animação de
// pan/fit a cada salto novo — é isso que dá a sensação de "desenhando ao vivo".

const NODE_SPACING = 150;
const ORIGIN_NODE_ID = "__origin__";

interface NetDiscoveryGraphProps {
  hops: NetDiscoveryHop[];
  running: boolean;
}

export default function NetDiscoveryGraph({ hops, running }: NetDiscoveryGraphProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const cyInstance = useRef<Core | null>(null);

  // Cria a instância UMA vez — nunca recriada entre saltos (é isso que preserva zoom/pan e
  // viabiliza a animação incremental, ao contrário de LatencyTopologyGraph que recria em cada
  // mudança de `data`).
  useEffect(() => {
    if (!containerRef.current) return;

    cyInstance.current = cytoscape({
      container: containerRef.current,
      elements: [
        {
          data: { id: ORIGIN_NODE_ID, label: "Origem", kind: "origin" },
          position: { x: 0, y: 0 },
        },
      ],
      style: [
        {
          selector: "node",
          style: {
            label: "data(label)",
            color: "#ffffff",
            "text-valign": "center",
            "text-halign": "center",
            "font-size": "9px",
            "text-wrap": "wrap",
            "text-max-width": "80px",
            width: 70,
            height: 70,
            "border-width": 2,
            "border-color": "#1e293b",
            "background-color": "#6b7280",
          },
        },
        { selector: 'node[kind = "origin"]', style: { "background-color": "#8b5cf6", shape: "round-rectangle" } },
        { selector: 'node[kind = "hop"]', style: { "background-color": "#3b82f6" } },
        { selector: 'node[kind = "timeout"]', style: { "background-color": "#6b7280", "border-style": "dashed" } },
        {
          selector: 'node[kind = "target"]',
          style: { "background-color": "#10b981", width: 90, height: 90, "font-size": "10px", "font-weight": "bold" },
        },
        {
          selector: "edge",
          style: {
            width: 3,
            "curve-style": "bezier",
            "target-arrow-shape": "triangle",
            "arrow-scale": 1.1,
            "line-color": "#64748b",
            "target-arrow-color": "#64748b",
            label: "data(label)",
            "font-size": "9px",
            "text-background-color": "#0f172a",
            "text-background-opacity": 0.7,
            "text-background-padding": "2px",
            color: "#ffffff",
          },
        },
      ],
      layout: { name: "preset" }, // posições explícitas (x = índice × espaçamento) — cadeia linear, sem cose/força
      zoom: 1,
      minZoom: 0.3,
      maxZoom: 2.5,
    });

    return () => {
      cyInstance.current?.destroy();
      cyInstance.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Alimenta a instância incrementalmente — só adiciona os saltos NOVOS desde a última renderização
  // (cy.nodes().length - 1, descontando o nó de origem), nunca recria o grafo inteiro. Quando
  // `hops` volta a ficar vazio (nova descoberta iniciada), reseta pro estado inicial (só a Origem).
  useEffect(() => {
    const cy = cyInstance.current;
    if (!cy) return;

    if (hops.length === 0) {
      cy.elements().remove();
      cy.add({ data: { id: ORIGIN_NODE_ID, label: "Origem", kind: "origin" }, position: { x: 0, y: 0 } });
      cy.fit(undefined, 40);
      return;
    }

    const existingHopNodes = cy.nodes().filter((n) => n.id() !== ORIGIN_NODE_ID).length;
    if (hops.length <= existingHopNodes) return; // nada novo (ex: re-render sem novo salto)

    for (let i = existingHopNodes; i < hops.length; i++) {
      const hop = hops[i];
      const nodeId = `hop-${hop.index}`;
      const prevId = i === 0 ? ORIGIN_NODE_ID : `hop-${hops[i - 1].index}`;

      const kind = hop.timed_out ? "timeout" : hop.is_target ? "target" : "hop";
      const label = hop.timed_out ? `${hop.index}\n?` : hop.ip ? `${hop.index}\n${hop.ip}` : `${hop.index}`;

      cy.add({
        data: { id: nodeId, label, kind },
        position: { x: (i + 1) * NODE_SPACING, y: 0 },
      });
      cy.add({
        data: {
          id: `edge-${nodeId}`,
          source: prevId,
          target: nodeId,
          label: hop.timed_out ? "" : hop.rtt_ms ? `${hop.rtt_ms.toFixed(0)}ms` : "",
        },
      });
    }

    // Pequena animação de pan/fit a cada lote de saltos novos — reforça a sensação de "desenhando
    // ao vivo" sem ser abrupto (duration curta, sempre a mesma).
    cy.animate({ fit: { eles: cy.elements(), padding: 40 } }, { duration: 300 });
  }, [hops]);

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center justify-end gap-1.5">
        <Button size="sm" variant="outline" onClick={() => cyInstance.current?.zoom(cyInstance.current.zoom() * 1.2)}>
          <ZoomIn className="w-3.5 h-3.5" />
        </Button>
        <Button size="sm" variant="outline" onClick={() => cyInstance.current?.zoom(cyInstance.current.zoom() * 0.8)}>
          <ZoomOut className="w-3.5 h-3.5" />
        </Button>
        <Button size="sm" variant="outline" onClick={() => cyInstance.current?.fit(undefined, 40)}>
          <Maximize2 className="w-3.5 h-3.5" />
        </Button>
      </div>
      <div
        ref={containerRef}
        className="w-full bg-slate-50 dark:bg-slate-900 border border-border rounded-md"
        style={{ height: "320px" }}
      />
      {running && (
        <div className="text-xs text-muted-foreground text-center animate-pulse">
          Traçando rota — cada salto aparece assim que responde...
        </div>
      )}
    </div>
  );
}
