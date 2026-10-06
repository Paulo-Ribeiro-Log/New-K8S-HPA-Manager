import { useEffect, useMemo, useRef, useState } from "react";
import { AlertCircle, Copy, ExternalLink, FileDiff, Loader2, RefreshCw, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { apiClient, type CodeEditorPRPlanResponse } from "@/lib/api/client";

// View Plan — mostra (somente leitura) o último plan que o Atlantis comentou no PR da branch
// atual. Não usa o Dialog do shadcn: o modal é flutuante (arrastável pelo cabeçalho) e tem
// `resize: both` nativo, então a posição é top/left em px controlados aqui — com o Dialog
// centralizado por translate, arrastar a alça faz a caixa crescer pros dois lados e a alça
// "foge" do mouse.

interface ViewPlanModalProps {
  repoId: string;
  profileId?: string;
  onClose: () => void;
}

// Cor da linha pelo símbolo do diff do Terraform. O Atlantis move o símbolo para a coluna 0
// (pro realce de diff do GitHub), mas versões antigas mantêm a indentação — por isso o trim.
function lineStyle(line: string): string {
  const t = line.trimStart();
  if (t.startsWith("-/+") || t.startsWith("+/-")) return "text-fuchsia-400";
  if (t.startsWith("#")) {
    if (/destroyed|replaced/.test(t)) return "text-red-300 font-semibold";
    if (/created/.test(t)) return "text-emerald-300 font-semibold";
    return "text-sky-300 font-semibold";
  }
  if (t.startsWith("Plan:") || t.startsWith("No changes.")) return "text-white font-bold";
  if (t.startsWith("Error:") || t.startsWith("│ Error:")) return "text-red-400 font-semibold";
  if (t.startsWith("Warning:") || t.startsWith("│ Warning:")) return "text-amber-300";
  switch (t[0]) {
    case "+": return "text-emerald-400";
    case "-": return "text-red-400";
    case "~": return "text-amber-300";
    default: return "text-zinc-300";
  }
}

// Mesmo resumo em texto puro, para as <option> do select (que não aceitam cor por trecho).
function summaryText(summary: string, error: boolean): string {
  if (error) return "erro";
  const m = summary.match(/(\d+) to add, (\d+) to change, (\d+) to destroy/);
  if (!m) return summary.startsWith("No changes") ? "sem mudanças" : "—";
  return `+${m[1]} ~${m[2]} -${m[3]}`;
}

// "Plan: 1 to add, 2 to change, 3 to destroy." → contadores coloridos do projeto selecionado.
function SummaryBadge({ summary, error }: { summary: string; error: boolean }) {
  if (error) return <span className="text-red-400">erro</span>;
  const m = summary.match(/(\d+) to add, (\d+) to change, (\d+) to destroy/);
  if (!m) return <span className="text-zinc-400">{summary.startsWith("No changes") ? "sem mudanças" : "—"}</span>;
  return (
    <span className="font-mono">
      <span className="text-emerald-400">+{m[1]}</span>{" "}
      <span className="text-amber-300">~{m[2]}</span>{" "}
      <span className={Number(m[3]) > 0 ? "text-red-400 font-bold" : "text-red-400/60"}>-{m[3]}</span>
    </span>
  );
}

export function ViewPlanModal({ repoId, profileId, onClose }: ViewPlanModalProps) {
  const [data, setData] = useState<CodeEditorPRPlanResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [prNumber, setPrNumber] = useState<number | undefined>(undefined);
  const [projectIdx, setProjectIdx] = useState(0);
  const [copied, setCopied] = useState(false);
  const boxRef = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState(() => ({ x: Math.round(window.innerWidth * 0.48), y: Math.round(window.innerHeight * 0.08) }));
  const dragRef = useRef<{ dx: number; dy: number } | null>(null);

  // Arrastar pelo cabeçalho (exceto pelos botões/select dele). Pointer capture mantém o
  // arrasto mesmo com o mouse saindo da caixa. A posição é limitada para sempre sobrar
  // um pedaço do cabeçalho visível na tela — senão o modal podia sumir e não voltar.
  function onDragStart(e: React.PointerEvent<HTMLDivElement>) {
    if (e.button !== 0 || (e.target as HTMLElement).closest("button,select,a,input")) return;
    dragRef.current = { dx: e.clientX - pos.x, dy: e.clientY - pos.y };
    e.currentTarget.setPointerCapture(e.pointerId);
    e.preventDefault();
  }
  function onDragMove(e: React.PointerEvent<HTMLDivElement>) {
    if (!dragRef.current) return;
    const w = boxRef.current?.offsetWidth ?? 480;
    const minVisible = 120;
    setPos({
      x: Math.min(Math.max(e.clientX - dragRef.current.dx, minVisible - w), window.innerWidth - minVisible),
      y: Math.min(Math.max(e.clientY - dragRef.current.dy, 0), window.innerHeight - 40),
    });
  }
  function onDragEnd(e: React.PointerEvent<HTMLDivElement>) {
    if (!dragRef.current) return;
    dragRef.current = null;
    e.currentTarget.releasePointerCapture(e.pointerId);
  }

  async function load(pr?: number) {
    setLoading(true); setError("");
    try {
      const r = await apiClient.codeEditorGetPRPlan(repoId, pr, profileId);
      setData(r);
      setProjectIdx(0);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Erro ao buscar o plan");
    } finally {
      setLoading(false);
    }
  }

  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => { load(prNumber); }, [repoId, prNumber]);

  const projects = data?.plan?.projects ?? [];
  const project = projects[projectIdx];
  const lines = useMemo(() => (project ? project.content.split("\n") : []), [project]);
  const gutter = String(lines.length).length;

  function copy() {
    if (!project) return;
    navigator.clipboard.writeText(project.content).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    }).catch(() => {});
  }

  return (
    // Janela flutuante sem fundo/overlay: o editor por trás continua utilizável. Por isso
    // também não fecha com Esc (o Esc é usado dentro do editor) — só pelo X.
    <div
      ref={boxRef}
      className="fixed z-50 flex flex-col rounded-lg border border-zinc-700 bg-[#1e1e1e] text-zinc-200 shadow-2xl overflow-hidden"
      style={{ top: pos.y, left: pos.x, width: "50vw", height: "80vh", minWidth: 480, minHeight: 280, maxWidth: "98vw", maxHeight: "96vh", resize: "both" }}
    >
      {/* Cabeçalho = alça de arrasto */}
      <div
        className="flex items-center gap-2 px-3 py-2 border-b border-zinc-700 flex-shrink-0 text-sm cursor-move select-none touch-none"
        onPointerDown={onDragStart}
        onPointerMove={onDragMove}
        onPointerUp={onDragEnd}
        onPointerCancel={onDragEnd}
        title="Arraste para mover"
      >
        <FileDiff className="w-4 h-4 text-sky-400" />
        <span className="font-semibold">View Plan</span>
        {data?.pr && (
          <span className="text-xs text-zinc-400 truncate">
            PR #{data.pr.number} · {data.pr.title} · <span className="font-mono">{data.branch} → {data.pr.base}</span>
            {data.pr.state !== "open" && <span className="ml-1 text-amber-300">({data.pr.state})</span>}
          </span>
        )}
        <div className="ml-auto flex items-center gap-1 flex-shrink-0">
          {data && data.prs.length > 1 && (
            <select
              className="h-6 text-xs rounded bg-zinc-800 border border-zinc-600 px-1"
              value={data.pr?.number ?? ""}
              onChange={e => setPrNumber(Number(e.target.value))}
              disabled={loading}
              title="Esta branch tem mais de um PR"
            >
              {data.prs.map(p => (
                <option key={p.number} value={p.number}>#{p.number} → {p.base} ({p.state})</option>
              ))}
            </select>
          )}
          <Button variant="ghost" size="sm" className="h-6 text-xs gap-1" onClick={() => load(prNumber)} disabled={loading} title="Recarregar">
            <RefreshCw className={`w-3 h-3 ${loading ? "animate-spin" : ""}`} />
          </Button>
          <Button variant="ghost" size="sm" className="h-6 text-xs gap-1" onClick={copy} disabled={!project}>
            <Copy className="w-3 h-3" />{copied ? "Copiado" : "Copiar"}
          </Button>
          {data?.plan && (
            <Button variant="ghost" size="sm" className="h-6 text-xs gap-1" onClick={() => window.open(data.plan!.comment_url, "_blank")}>
              <ExternalLink className="w-3 h-3" />GitHub
            </Button>
          )}
          <Button variant="ghost" size="sm" className="h-6 w-6 p-0" onClick={onClose} title="Fechar">
            <X className="w-4 h-4" />
          </Button>
        </div>
      </div>

      {/* Projeto do plan: select (lista numerada com o resumo de cada um) + resumo colorido do escolhido */}
      {projects.length > 0 && (
        <div className="flex items-center gap-2 px-3 py-1.5 border-b border-zinc-700 flex-shrink-0 text-xs">
          <label htmlFor="view-plan-project" className="text-zinc-400 whitespace-nowrap">
            Projeto ({projects.length}):
          </label>
          <select
            id="view-plan-project"
            className="h-7 min-w-0 max-w-[60%] rounded bg-zinc-800 border border-zinc-600 px-2 font-mono text-xs"
            value={projectIdx}
            onChange={e => setProjectIdx(Number(e.target.value))}
          >
            {projects.map((p, i) => (
              <option key={i} value={i}>
                {i + 1}. {p.label || `projeto ${i + 1}`} — {summaryText(p.summary, p.error)}
              </option>
            ))}
          </select>
          {project && (
            <span className="whitespace-nowrap" title={project.summary}>
              <SummaryBadge summary={project.summary} error={project.error} />
            </span>
          )}
          {data?.plan && (
            <span className="ml-auto pl-2 text-[11px] text-zinc-500 whitespace-nowrap">
              por {data.plan.author} em {new Date(data.plan.created_at).toLocaleString("pt-BR")}
              {data.plan.comments > 1 && ` · reunido de ${data.plan.comments} comentários`}
            </span>
          )}
        </div>
      )}

      {/* Conteúdo: sem quebra de linha, rolagem nos dois eixos */}
      <div className="flex-1 min-h-0 overflow-auto">
        {loading && !data ? (
          <div className="flex items-center gap-2 p-4 text-sm text-zinc-400"><Loader2 className="w-4 h-4 animate-spin" />Buscando plan no PR…</div>
        ) : error ? (
          <div className="flex items-start gap-2 p-4 text-sm text-red-400"><AlertCircle className="w-4 h-4 mt-0.5 flex-shrink-0" /><span className="whitespace-pre-wrap">{error}</span></div>
        ) : !data?.pr ? (
          <div className="p-4 text-sm text-zinc-400">Nenhum PR encontrado para a branch <span className="font-mono">{data?.branch}</span>.</div>
        ) : !data.plan ? (
          <div className="p-4 text-sm text-zinc-400">
            Nenhum plan do Atlantis nos comentários do PR #{data.pr.number}.{" "}
            <a className="text-sky-400 underline" href={data.pr.url} target="_blank" rel="noreferrer">Abrir PR</a>
          </div>
        ) : !project ? (
          <div className="p-4 text-sm text-zinc-400">O último comentário do Atlantis não tem saída de plan.</div>
        ) : (
          <pre className="font-mono text-[12.5px] leading-[1.45] py-2 min-w-max">
            {lines.map((line, i) => (
              <div key={i} className="flex hover:bg-white/5">
                <span
                  className="sticky left-0 select-none text-right pr-3 pl-2 text-zinc-600 bg-[#1e1e1e] flex-shrink-0"
                  style={{ width: `${gutter + 2}ch` }}
                >{i + 1}</span>
                <span className={`whitespace-pre pr-6 ${lineStyle(line)}`}>{line || " "}</span>
              </div>
            ))}
          </pre>
        )}
      </div>
    </div>
  );
}
