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

const NODE_SPACING = 160;
const ORIGIN_NODE_ID = "__origin__";
const HOP_NODE_SIZE = 72;
const TARGET_NODE_SIZE = 92;
// INFO_LABEL_GAP — distância entre a borda do círculo e o texto flutuante abaixo dele (hostname +
// recurso K8s, ver INFO_LABEL_KIND). Pedido explícito do usuário: o texto extra estourava a
// largura fixa do círculo e ficava ilegível — corrigido tirando ele de dentro do nó.
const INFO_LABEL_GAP = 10;
const INFO_LABEL_KIND = "info-label";

// truncateHostname evita que um FQDN muito longo (comum em PTR corporativo/nuvem, ex:
// "ord38s33-in-f14.1e100.net") deixe o texto flutuante abaixo do nó largo demais — o nome
// completo continua disponível na tabela de saltos (NetDiscoveryTab.tsx), este é só um resumo
// visual no grafo.
const HOSTNAME_LABEL_MAX = 28;
function truncateHostname(host: string): string {
  return host.length > HOSTNAME_LABEL_MAX ? `${host.slice(0, HOSTNAME_LABEL_MAX - 1)}…` : host;
}

// buildCenterLabel monta só o texto DENTRO do círculo do nó — pedido explícito do usuário:
// "coloque APENAS os IPs descobertos dentro" (mais nada). Índice do salto, hostname e recurso K8s
// vivem fora do círculo (rótulo da seta e texto flutuante abaixo, ver buildInfoLabel).
//
// 3ª rodada de correção, achado real via print do usuário: a v2 desviou dessa instrução original
// adicionando o emoji do SO (Fase 2) dentro do círculo pro nó de destino — que quebrava de duas
// formas: (1) texto maior demais forçando wrap de novo (2ª rodada), e (2) mesmo depois de resolver
// a largura, o glifo do emoji renderizava CORROMPIDO (não como 🌐/🐧/🪟 de verdade, um ícone
// ilegível qualquer) — <canvas> (como o Cytoscape desenha texto) não tem o mesmo suporte a emoji
// colorido que renderização DOM normal tem; dependendo da fonte disponível no SO/browser, o
// fillText() do canvas cai num fallback tosco pra glifos fora do alfabeto básico. Removido de vez:
// o dado de SO/fingerprint já está disponível no painel de detalhe da aba (badges Linux/Windows/
// Web), não precisa ser duplicado — e principalmente incorretamente — dentro do grafo.
function buildCenterLabel(hop: NetDiscoveryHop): string {
  if (hop.timed_out) return "?";
  return hop.ip ?? "";
}

// buildInfoLabel monta o texto flutuante ABAIXO do nó — hostname resolvido (Fase 3) e/ou nome do
// recurso K8s (Fase 4), cada um em sua própria linha quando presente. Retorna "" quando não há
// nada a mostrar (nó ainda sem enriquecimento, ou salto sem nenhum dos dois).
function buildInfoLabel(hop: NetDiscoveryHop): string {
  const lines: string[] = [];
  if (hop.reverse_dns) lines.push(truncateHostname(hop.reverse_dns));
  if (hop.internal_ref) lines.push(`[${hop.internal_ref.name}]`);
  return lines.join("\n");
}

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
            // 3ª rodada de correção do mesmo bug, achado real via 2º print do usuário: "70px" ainda
            // não bastava — um IPv4 tem até 15 caracteres ("255.255.255.255"), e a métrica real da
            // fonte no canvas do navegador (~5.5-6px por caractere a 9px) passa fácil de 70px,
            // forçando o Cytoscape a quebrar o IP no meio (sem espaço nenhum pra cortar, já que é
            // um token sem espaços). Em vez de tentar acertar o valor exato de novo, usa uma
            // margem generosa (140px, quase o dobro do diâmetro do círculo de 72px) — o IP nunca
            // deveria se aproximar disso, então nunca mais deveria disparar wrap automático. O
            // texto pode ultrapassar visualmente a borda do círculo nos dois lados (Cytoscape não
            // recorta label pela forma do nó) — aceitável, é sempre melhor que um IP partido ao
            // meio. `text-wrap: wrap` continua ligado só pra respeitar a quebra MANUAL (\n) entre
            // emoji e IP no destino — não é isso que causa o wrap indevido, é só o max-width curto.
            "text-max-width": "140px",
            width: HOP_NODE_SIZE,
            height: HOP_NODE_SIZE,
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
          style: {
            "background-color": "#10b981",
            width: TARGET_NODE_SIZE,
            height: TARGET_NODE_SIZE,
            "font-size": "10px",
            "font-weight": "bold",
            "text-max-width": "160px", // mesma margem generosa do node base — nunca deve disparar wrap indevido no IP
          },
        },
        // Texto flutuante ABAIXO do nó (hostname/recurso K8s, ver buildInfoLabel) — nó próprio,
        // sem borda/fundo (só o texto em si), não-interativo (`events: "no"`, `grabbable`/
        // `selectable: false` no elemento — ver criação em cy.add), largura/altura calculadas a
        // partir do próprio conteúdo do label (`width/height: "label"`) em vez de um tamanho fixo,
        // já que o texto varia bastante (de vazio a um FQDN inteiro truncado).
        {
          selector: `node[kind = "${INFO_LABEL_KIND}"]`,
          style: {
            shape: "round-rectangle",
            "background-opacity": 0,
            "border-width": 0,
            label: "data(label)",
            color: "#94a3b8",
            "font-size": "8px",
            "font-weight": "normal",
            "text-valign": "center",
            "text-halign": "center",
            "text-wrap": "wrap",
            "text-max-width": "110px",
            width: "label",
            height: "label",
            padding: "3px",
            events: "no",
          },
        },
        // Match de nuvem pública (Fase 3, enrichHops) — mesma paleta de cor já usada em
        // PROVIDER_COLORS (LatencyTopologyGraph.tsx: aws=laranja, gcp=verde). Só a BORDA, nunca o
        // background (que já carrega o sentido de "status do salto" — hop/timeout/target) — os
        // dois sinais coexistem sem conflito visual.
        { selector: 'node[cloudMatch = "aws"]', style: { "border-color": "#f97316", "border-width": 4 } },
        { selector: 'node[cloudMatch = "gcp"]', style: { "border-color": "#10b981", "border-width": 4 } },
        // Azure adicionado na Fase 5 (item P3 do roadmap) — mesma cor já usada pro provider azure
        // em LatencyTopologyGraph.tsx (PROVIDER_COLORS).
        { selector: 'node[cloudMatch = "azure"]', style: { "border-color": "#3b82f6", "border-width": 4 } },
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

    // Exclui os nós de texto flutuante (INFO_LABEL_KIND) da contagem — cada salto real vira DOIS
    // nós Cytoscape (o círculo + seu companheiro de texto abaixo), então contar os dois juntos
    // faria este efeito pensar que já processou o dobro dos saltos reais e nunca mais adicionar
    // nada a partir do 2º lote.
    const existingHopNodes = cy.nodes().filter((n) => n.id() !== ORIGIN_NODE_ID && n.data("kind") !== INFO_LABEL_KIND).length;
    if (hops.length <= existingHopNodes) return; // nada novo (ex: re-render sem novo salto)

    for (let i = existingHopNodes; i < hops.length; i++) {
      const hop = hops[i];
      const nodeId = `hop-${hop.index}`;
      const prevId = i === 0 ? ORIGIN_NODE_ID : `hop-${hops[i - 1].index}`;
      const x = (i + 1) * NODE_SPACING;

      const kind = hop.timed_out ? "timeout" : hop.is_target ? "target" : "hop";
      const label = buildCenterLabel(hop);

      cy.add({
        data: { id: nodeId, label, kind },
        position: { x, y: 0 },
      });
      // Rótulo da seta carrega o índice do salto (removido de dentro do círculo, ver
      // buildCenterLabel) + a latência quando disponível — "#N" sozinho pra saltos sem resposta
      // (antes ficava sem rótulo nenhum, perdendo até a numeração nesse caso).
      const edgeLabel = hop.rtt_ms ? `#${hop.index} · ${hop.rtt_ms.toFixed(0)}ms` : `#${hop.index}`;
      cy.add({
        data: { id: `edge-${nodeId}`, source: prevId, target: nodeId, label: edgeLabel },
      });

      // Companheiro de texto flutuante (hostname/recurso K8s) — criado vazio de propósito aqui
      // (o enriquecimento chega bem depois, no evento "complete"); o efeito abaixo preenche o
      // texto quando os dados existirem. Nunca criado pra saltos que não responderam (timeout
      // nunca tem IP, então nunca tem hostname/K8s pra mostrar).
      if (kind !== "timeout") {
        const halfHeight = (kind === "target" ? TARGET_NODE_SIZE : HOP_NODE_SIZE) / 2;
        cy.add({
          data: { id: `${nodeId}-info`, label: "", kind: INFO_LABEL_KIND },
          position: { x, y: halfHeight + INFO_LABEL_GAP },
          grabbable: false,
          selectable: false,
        });
      }
    }

    // Pequena animação de pan/fit a cada lote de saltos novos — reforça a sensação de "desenhando
    // ao vivo" sem ser abrupto (duration curta, sempre a mesma).
    cy.animate({ fit: { eles: cy.elements(), padding: 40 } }, { duration: 300 });
  }, [hops]);

  // Enriquecimento por salto (Fases 3-4: DNS reverso/nuvem, cross-reference K8s) chega junto da
  // lista final de `hops` no evento "complete" — o efeito de cima (que adiciona NÓS NOVOS) já
  // ignora esse update porque `hops.length` não muda (mesma contagem de saltos, só campos novos
  // preenchidos). Este efeito roda À PARTE, sempre que `hops` muda, e só ATUALIZA nós já existentes
  // (nunca adiciona/remove).
  //
  // O label do CÍRCULO (buildCenterLabel) não depende mais de nada que chegue por enriquecimento
  // (só IP/timeout, já conhecidos na criação) — o recálculo abaixo é defensivo/idempotente, nunca
  // deveria de fato mudar nada; só o texto flutuante abaixo (buildInfoLabel) realmente muda aqui.
  useEffect(() => {
    const cy = cyInstance.current;
    if (!cy) return;
    let changed = false;

    for (const hop of hops) {
      const node = cy.getElementById(`hop-${hop.index}`);
      if (node.empty()) continue;

      const newCenterLabel = buildCenterLabel(hop);
      if (node.data("label") !== newCenterLabel) {
        node.data("label", newCenterLabel);
        changed = true;
      }
      if (hop.cloud_match && node.data("cloudMatch") !== hop.cloud_match) {
        node.data("cloudMatch", hop.cloud_match);
      }
      if (hop.internal_ref && node.data("internalRefKind") !== hop.internal_ref.kind) {
        node.data("internalRefKind", hop.internal_ref.kind);
      }

      const infoNode = cy.getElementById(`${node.id()}-info`);
      if (!infoNode.empty()) {
        const newInfoLabel = buildInfoLabel(hop);
        if (infoNode.data("label") !== newInfoLabel) {
          infoNode.data("label", newInfoLabel);
          changed = true; // texto flutuante nasce vazio (quase sem largura) e só ganha tamanho
          // real aqui — sem reenquadrar a view, ele cresce fora da área já fixada pelo último
          // cy.fit() (que rodou ANTES do enriquecimento existir, no efeito de cima) e fica
          // cortado na borda do canvas. Achado real, relatado ao vivo: "os nomes ainda estão
          // quebrados" depois da correção anterior (mover o hostname pra fora do círculo) — o
          // texto em si já estava correto, só a viewport nunca acompanhava o novo tamanho.
        }
      }
    }

    // Reenquadra só quando algo de fato mudou (evita um fit "do nada" em re-renders sem
    // enriquecimento novo) — mesma pequena animação já usada no efeito de adicionar saltos, pra
    // não ser abrupto.
    if (changed) {
      cy.animate({ fit: { eles: cy.elements(), padding: 40 } }, { duration: 300 });
    }
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
