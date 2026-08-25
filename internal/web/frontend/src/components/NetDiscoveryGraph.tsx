import { useEffect, useRef } from "react";
import cytoscape, { Core } from "cytoscape";
import { ZoomIn, ZoomOut, Maximize2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import type { NetDiscoveryFingerprint, NetDiscoveryHop } from "@/lib/api/types";

// Especialização LINEAR de LatencyTopologyGraph.tsx (cadeia Origem → Hop 1 → ... → Destino, não
// um grafo geral) — pedido explícito do usuário: "desenhar na tela um fluxo da rota... ficaria
// lindo o acompanhar do processo". Diferente do grafo de latência (que recebe `data` pronto de
// uma query e desenha tudo de uma vez), este componente é ALIMENTADO INCREMENTALMENTE — a
// instância do Cytoscape nasce uma única vez e cada salto novo em `hops` vira um `cy.add()` (não
// um rebuild completo), preservando zoom/pan entre saltos e permitindo uma pequena animação de
// pan/fit a cada salto novo — é isso que dá a sensação de "desenhando ao vivo".

const NODE_SPACING = 170; // nós um pouco maiores desde a v2 (84-100px) — espaçamento ajustado junto
const ORIGIN_NODE_ID = "__origin__";

// osEmoji — mesmo princípio de fraseologia neutra do resto da app: o próprio ícone já é
// deliberadamente ambíguo ("❓" sem sinal suficiente), nunca afirma com certeza o que é heurística
// (ver NetDiscoveryFingerprint.os_confidence, sempre exibido por extenso no painel de detalhe).
function osEmoji(fp: NetDiscoveryFingerprint | undefined): string {
  if (!fp) return "";
  if (fp.os_guess === "linux") return "🐧";
  if (fp.os_guess === "windows") return "🪟";
  if (fp.is_web_server) return "🌐";
  return "❓";
}

// truncateHostname evita que um FQDN longo (comum em PTR corporativo/nuvem, ex:
// "ord38s33-in-f14.1e100.net") estoure a largura fixa do nó (text-max-width: 80px) — o nome
// completo continua disponível na tabela de saltos (NetDiscoveryTab.tsx), este é só um resumo
// visual dentro do grafo.
const HOSTNAME_LABEL_MAX = 20;
function truncateHostname(host: string): string {
  return host.length > HOSTNAME_LABEL_MAX ? `${host.slice(0, HOSTNAME_LABEL_MAX - 1)}…` : host;
}

// buildHopLabel monta o texto completo do nó — única fonte de verdade do label, usada tanto na
// criação do nó (sem enriquecimento ainda, hop "cru" vindo do evento SSE "hop" ao vivo) quanto na
// atualização pós-enriquecimento (DNS reverso/fingerprint/cross-reference K8s, todos chegando
// juntos no evento "complete"). Existir como uma função só evita o bug de ordenação entre efeitos
// que existiria se cada camada (emoji do fingerprint, hostname da Fase 3, K8s da Fase 4) escrevesse
// o label separadamente — cada write anterior seria perdido pelo próximo.
function buildHopLabel(hop: NetDiscoveryHop, fingerprint: NetDiscoveryFingerprint | undefined): string {
  if (hop.timed_out) return `${hop.index}\n?`;
  if (!hop.ip) return `${hop.index}`;

  const emojiPrefix = hop.is_target ? osEmoji(fingerprint) : "";
  let label = `${hop.index}\n${emojiPrefix ? `${emojiPrefix} ` : ""}${hop.ip}`;
  if (hop.reverse_dns) label += `\n${truncateHostname(hop.reverse_dns)}`;
  if (hop.internal_ref) label += `\n[${hop.internal_ref.name}]`;
  return label;
}

interface NetDiscoveryGraphProps {
  hops: NetDiscoveryHop[];
  running: boolean;
  fingerprint?: NetDiscoveryFingerprint; // Fase 2 — chega DEPOIS do nó de destino já existir no grafo
}

export default function NetDiscoveryGraph({ hops, running, fingerprint }: NetDiscoveryGraphProps) {
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
            // font-size/tamanho reduzidos em relação à v1 (9px/70x70) — o label agora
            // rotineiramente ganha uma 3ª linha (hostname resolvido, Fase 3) e às vezes uma 4ª
            // (recurso K8s, Fase 4); 8px/84x84 dá espaço pras linhas extras sem estourar demais.
            "font-size": "8px",
            "text-wrap": "wrap",
            "text-max-width": "78px",
            width: 84,
            height: 84,
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
          style: { "background-color": "#10b981", width: 100, height: 100, "font-size": "9px", "font-weight": "bold" },
        },
        // Match de nuvem pública (Fase 3, enrichHops) — mesma paleta de cor já usada em
        // PROVIDER_COLORS (LatencyTopologyGraph.tsx: aws=laranja, gcp=verde). Só a BORDA, nunca o
        // background (que já carrega o sentido de "status do salto" — hop/timeout/target) — os
        // dois sinais coexistem sem conflito visual.
        { selector: 'node[cloudMatch = "aws"]', style: { "border-color": "#f97316", "border-width": 4 } },
        { selector: 'node[cloudMatch = "gcp"]', style: { "border-color": "#10b981", "border-width": 4 } },
        // Cross-reference K8s (Fase 4) — borda roxa, mesma cor da Origem (kind="origin"), sinaliza
        // "isto é um recurso da nossa própria frota K8s conhecida". Tem prioridade visual sobre
        // cloudMatch (selector declarado depois vence em empate de especificidade no Cytoscape) —
        // um hop nunca deveria bater nos dois ao mesmo tempo na prática (IP privado K8s vs. faixa
        // pública de nuvem são mutuamente exclusivos), mas se acontecer, K8s é o sinal mais preciso.
        { selector: 'node[internalRefKind]', style: { "border-color": "#8b5cf6", "border-width": 4 } },
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
      // Sem fingerprint/reverse_dns ainda nesta fase (evento "hop" ao vivo chega antes do
      // enriquecimento) — buildHopLabel já lida com isso sozinho (campos ausentes = omitidos).
      const label = buildHopLabel(hop, undefined);

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

  // Enriquecimento por salto (Fases 2-4: fingerprint do destino, DNS reverso/nuvem, cross-reference
  // K8s) chega junto da lista final de `hops`/`fingerprint` no evento "complete" — o efeito de cima
  // (que adiciona NÓS NOVOS) já ignora esse update porque `hops.length` não muda (mesma contagem de
  // saltos, só campos novos preenchidos). Este efeito roda À PARTE, sempre que `hops` OU
  // `fingerprint` mudam, e só ATUALIZA nós já existentes (nunca adiciona/remove).
  //
  // Um único efeito (não um por camada) é deliberado: as três camadas de enriquecimento chegam
  // juntas no mesmo evento SSE, e cada uma contribui pro MESMO label do nó (emoji do SO, hostname
  // resolvido, nome do recurso K8s) — se cada camada escrevesse o label separadamente em efeitos
  // distintos, a ordem de execução entre eles decidiria qual escrita sobrevive, sobrescrevendo as
  // anteriores. `buildHopLabel` monta o texto completo de uma vez, sempre a partir do estado atual
  // (`hops`/`fingerprint`), nunca a partir do label anterior do nó — idempotente e sem essa
  // fragilidade de ordenação.
  useEffect(() => {
    const cy = cyInstance.current;
    if (!cy) return;
    for (const hop of hops) {
      const node = cy.getElementById(`hop-${hop.index}`);
      if (node.empty()) continue;

      const newLabel = buildHopLabel(hop, hop.is_target ? fingerprint : undefined);
      if (node.data("label") !== newLabel) {
        node.data("label", newLabel);
      }
      if (hop.cloud_match && node.data("cloudMatch") !== hop.cloud_match) {
        node.data("cloudMatch", hop.cloud_match);
      }
      if (hop.internal_ref && node.data("internalRefKind") !== hop.internal_ref.kind) {
        node.data("internalRefKind", hop.internal_ref.kind);
      }
    }
  }, [hops, fingerprint]);

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
