import { useEffect, useState } from "react";
import { AlertCircle, ArrowRight, CheckCircle2, GitBranch, GitMerge, Loader2 } from "lucide-react";
import { apiClient, type CodeEditorPRMergeStatus, type CodeEditorPRPreview } from "@/lib/api/client";

// Peças do modal de criação de PR: de onde sai a branch e para onde ela será mergeada, a prévia
// local de conflito (antes de criar) e o status de merge que o GitHub calcula (depois de criar).

type Tone = "ok" | "warn" | "bad" | "info";

const TONE_CLASS: Record<Tone, string> = {
  ok: "text-emerald-400 border-emerald-500/40 bg-emerald-500/10",
  warn: "text-amber-300 border-amber-500/40 bg-amber-500/10",
  bad: "text-red-400 border-red-500/40 bg-red-500/10",
  info: "text-muted-foreground border-border/50 bg-muted/40",
};

function StatusBox({ tone, icon, title, detail }: { tone: Tone; icon: React.ReactNode; title: string; detail?: React.ReactNode }) {
  return (
    <div className={`flex items-start gap-2 rounded border px-2.5 py-2 text-xs ${TONE_CLASS[tone]}`}>
      <span className="mt-0.5 flex-shrink-0">{icon}</span>
      <div className="min-w-0 space-y-0.5">
        <p className="font-semibold">{title}</p>
        {detail && <div className="text-[11px] opacity-90 [overflow-wrap:anywhere]">{detail}</div>}
      </div>
    </div>
  );
}

function plural(n: number, one: string, many: string): string {
  return `${n} ${n === 1 ? one : many}`;
}

interface PRBranchFlowProps {
  repoId: string;
  head: string;
  base: string;
  bases: string[];
  onBaseChange: (b: string) => void;
  disabled?: boolean;
}

// Origem → destino em dois blocos rotulados, uma frase explicando o merge e a prévia de conflito.
export function PRBranchFlow({ repoId, head, base, bases, onBaseChange, disabled }: PRBranchFlowProps) {
  const [preview, setPreview] = useState<CodeEditorPRPreview | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!base || !head) return;
    let cancelled = false;
    setLoading(true);
    setError("");
    setPreview(null);
    apiClient
      .codeEditorPRPreview(repoId, head, base)
      .then((p) => { if (!cancelled) setPreview(p); })
      .catch((e) => { if (!cancelled) setError(e instanceof Error ? e.message : "Erro ao verificar o merge"); })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [repoId, head, base]);

  return (
    <div className="space-y-2">
      <div className="grid grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-end gap-2">
        <div className="min-w-0">
          <p className="text-[10px] uppercase tracking-wider text-muted-foreground mb-1">Branch (origem)</p>
          <div className="flex items-center gap-1.5 rounded border border-blue-500/40 bg-blue-500/10 px-2 py-1.5 text-xs font-mono text-blue-300" title={head}>
            <GitBranch className="w-3 h-3 flex-shrink-0" />
            <span className="truncate">{head}</span>
          </div>
        </div>
        <ArrowRight className="w-4 h-4 mb-2 text-muted-foreground flex-shrink-0" />
        <div className="min-w-0">
          <p className="text-[10px] uppercase tracking-wider text-muted-foreground mb-1">Será mergeada em (destino)</p>
          <div className="flex items-center gap-1.5 rounded border border-emerald-500/40 bg-emerald-500/10 px-2 py-0.5 text-xs font-mono text-emerald-300">
            <GitMerge className="w-3 h-3 flex-shrink-0" />
            <select
              className="border-none outline-none flex-1 min-w-0 py-1 text-xs font-mono"
              style={{ background: "transparent", color: "inherit" }}
              value={base}
              onChange={(e) => onBaseChange(e.target.value)}
              disabled={disabled}
            >
              {bases.map((b) => (
                <option key={b} value={b} style={{ background: "#1e1e2e", color: "#cdd6f4" }}>{b}</option>
              ))}
            </select>
          </div>
        </div>
      </div>

      <p className="text-xs text-muted-foreground [overflow-wrap:anywhere]">
        As alterações de <span className="font-mono text-blue-300">{head}</span> serão mergeadas em{" "}
        <span className="font-mono text-emerald-300">{base}</span>.
      </p>

      {loading ? (
        <StatusBox tone="info" icon={<Loader2 className="w-3.5 h-3.5 animate-spin" />} title="Verificando se pode ser mergeado…" />
      ) : error ? (
        <StatusBox tone="warn" icon={<AlertCircle className="w-3.5 h-3.5" />} title="Não foi possível verificar o merge" detail={error} />
      ) : preview ? (
        <PreviewStatus preview={preview} head={head} base={base} />
      ) : null}
    </div>
  );
}

function PreviewStatus({ preview, head, base }: { preview: CodeEditorPRPreview; head: string; base: string }) {
  const counts = `${plural(preview.ahead, "commit", "commits")} à frente de ${preview.base_ref}${preview.behind > 0 ? ` · ${plural(preview.behind, "commit", "commits")} atrás` : ""}`;
  if (head === base) {
    return <StatusBox tone="bad" icon={<AlertCircle className="w-3.5 h-3.5" />} title="Origem e destino são a mesma branch" />;
  }
  if (!preview.checked) {
    return <StatusBox tone="warn" icon={<AlertCircle className="w-3.5 h-3.5" />} title="Não foi possível simular o merge" detail={`${counts}. O GitHub mostra o resultado depois de criar o PR.`} />;
  }
  if (preview.conflicts) {
    return (
      <StatusBox
        tone="bad"
        icon={<AlertCircle className="w-3.5 h-3.5" />}
        title="Não pode ser mergeado: há conflitos"
        detail={
          <>
            <p>{counts}.</p>
            {preview.conflict_files.length > 0 && (
              <p className="font-mono">{preview.conflict_files.slice(0, 5).join(", ")}{preview.conflict_files.length > 5 ? ` e mais ${preview.conflict_files.length - 5}` : ""}</p>
            )}
            <p>Resolva os conflitos antes (merge de {base} nesta branch) ou o PR sairá com conflito.</p>
          </>
        }
      />
    );
  }
  if (preview.ahead === 0) {
    return <StatusBox tone="warn" icon={<AlertCircle className="w-3.5 h-3.5" />} title="Nada a mergear" detail={`Esta branch não tem commits novos em relação a ${preview.base_ref}.`} />;
  }
  return (
    <StatusBox
      tone={preview.behind > 0 ? "warn" : "ok"}
      icon={<CheckCircle2 className="w-3.5 h-3.5" />}
      title="Pode ser mergeado, sem conflitos"
      detail={preview.behind > 0 ? `${counts}. O destino avançou; considere atualizar a branch.` : counts}
    />
  );
}

// Status que o GitHub calcula para o PR criado. `mergeable` vem nulo logo após criar, então repete
// algumas vezes até o GitHub responder.
const POLL_MS = 2000;
const POLL_MAX = 8;

export function PRMergeStatusLine({ repoId, prNumber, profileId }: { repoId: string; prNumber: number; profileId?: string }) {
  const [status, setStatus] = useState<CodeEditorPRMergeStatus | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout>;
    async function poll(attempt: number) {
      try {
        const s = await apiClient.codeEditorPRMergeStatus(repoId, prNumber, profileId);
        if (cancelled) return;
        setStatus(s);
        if (s.mergeable === null && s.state === "open" && attempt < POLL_MAX) timer = setTimeout(() => poll(attempt + 1), POLL_MS);
      } catch (e) {
        if (!cancelled) setError(e instanceof Error ? e.message : "Erro ao consultar o GitHub");
      }
    }
    poll(1);
    return () => { cancelled = true; clearTimeout(timer); };
  }, [repoId, prNumber, profileId]);

  if (error) return <StatusBox tone="warn" icon={<AlertCircle className="w-3.5 h-3.5" />} title="Não foi possível consultar o status de merge" detail={error} />;
  if (!status || (status.mergeable === null && status.state === "open")) {
    return <StatusBox tone="info" icon={<Loader2 className="w-3.5 h-3.5 animate-spin" />} title="O GitHub está calculando se pode ser mergeado…" />;
  }
  if (status.state === "merged") return <StatusBox tone="info" icon={<GitMerge className="w-3.5 h-3.5" />} title="PR já mergeado" />;
  if (status.state === "closed") return <StatusBox tone="info" icon={<AlertCircle className="w-3.5 h-3.5" />} title="PR fechado" />;
  if (status.mergeable === false || status.mergeable_state === "dirty") {
    return <StatusBox tone="bad" icon={<AlertCircle className="w-3.5 h-3.5" />} title="Não pode ser mergeado: há conflitos" detail="Resolva os conflitos na branch e faça push." />;
  }
  switch (status.mergeable_state) {
    case "clean":
      return <StatusBox tone="ok" icon={<CheckCircle2 className="w-3.5 h-3.5" />} title="Pode ser mergeado" detail="Sem conflitos e com os requisitos atendidos." />;
    case "blocked":
      return <StatusBox tone="warn" icon={<AlertCircle className="w-3.5 h-3.5" />} title="Sem conflitos, mas bloqueado" detail="Faltam aprovações ou checks obrigatórios da proteção da branch." />;
    case "behind":
      return <StatusBox tone="warn" icon={<AlertCircle className="w-3.5 h-3.5" />} title="Sem conflitos, mas desatualizado" detail="A branch está atrás do destino e a proteção exige atualizá-la antes." />;
    case "unstable":
      return <StatusBox tone="warn" icon={<AlertCircle className="w-3.5 h-3.5" />} title="Pode ser mergeado, com checks falhando" />;
    case "draft":
      return <StatusBox tone="warn" icon={<AlertCircle className="w-3.5 h-3.5" />} title="PR em rascunho" detail="Marque como pronto para revisão antes de mergear." />;
    default:
      return <StatusBox tone="ok" icon={<CheckCircle2 className="w-3.5 h-3.5" />} title="Sem conflitos" detail={status.mergeable_state ? `Estado no GitHub: ${status.mergeable_state}` : undefined} />;
  }
}
